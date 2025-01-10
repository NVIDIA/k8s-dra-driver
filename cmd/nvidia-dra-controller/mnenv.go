/*
 * Copyright (c) 2025 NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	resourcelisters "k8s.io/client-go/listers/resource/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	nvapi "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/pkg/flags"
	nvinformers "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/informers/externalversions"
	nvlisters "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/listers/gpu/v1alpha1"
)

const (
	resourceClaimFinalizer = "gpu.nvidia.com/finalizer.multiNodeEnvironment"
	imexDeviceClass        = "imex.nvidia.com"
)

type MultiNodeEnvironmentManager struct {
	clientsets flags.ClientSets
	waitGroup  sync.WaitGroup

	multiNodeEnvironmentLister nvlisters.MultiNodeEnvironmentLister
	resourceClaimLister        resourcelisters.ResourceClaimLister
}

// StartManager starts a MultiNodeEnvironmentManager.
func StartMultiNodeEnvironmentManager(ctx context.Context, config *Config) (*MultiNodeEnvironmentManager, error) {
	mneInformerFactory := nvinformers.NewSharedInformerFactory(config.clientsets.Nvidia, 30*time.Second)
	mneInformer := mneInformerFactory.Gpu().V1alpha1().MultiNodeEnvironments().Informer()
	mneLister := nvlisters.NewMultiNodeEnvironmentLister(mneInformer.GetIndexer())

	rcInformerFactory := informers.NewSharedInformerFactory(config.clientsets.Core, 30*time.Second)
	rcInformer := rcInformerFactory.Resource().V1beta1().ResourceClaims().Informer()
	rcLister := resourcelisters.NewResourceClaimLister(rcInformer.GetIndexer())

	m := &MultiNodeEnvironmentManager{
		clientsets:                 config.clientsets,
		multiNodeEnvironmentLister: mneLister,
		resourceClaimLister:        rcLister,
	}

	mneInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    m.onMultiNodeEnvironmentAdd,
		DeleteFunc: m.onMultiNodeEnvironmentDelete,
	})

	rcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: m.onResourceClaimAdd,
	})

	m.waitGroup.Add(2)
	go func() {
		defer m.waitGroup.Done()
		rcInformerFactory.Start(ctx.Done())
	}()
	go func() {
		defer m.waitGroup.Done()
		mneInformerFactory.Start(ctx.Done())
	}()

	if !cache.WaitForCacheSync(ctx.Done(), mneInformer.HasSynced, rcInformer.HasSynced) {
		klog.Warning("Cache sync failed; retrying in 5 seconds")
		time.Sleep(5 * time.Second)
		if !cache.WaitForCacheSync(ctx.Done(), mneInformer.HasSynced, rcInformer.HasSynced) {
			return nil, fmt.Errorf("informer cache sync failed twice")
		}
	}

	return m, nil
}

// Stop stops a running MultiNodeEnvironmentManager.
func (m *MultiNodeEnvironmentManager) Stop() error {
	if m == nil {
		return nil
	}
	m.waitGroup.Wait()
	return nil
}

func (m *MultiNodeEnvironmentManager) onMultiNodeEnvironmentAdd(obj interface{}) {
	mne, ok := obj.(*nvapi.MultiNodeEnvironment)
	if !ok {
		klog.Warning("Failed to cast to MultiNodeEnvironment")
		return
	}

	klog.Infof("MultiNodeEnvironment added: %s", mne.Name)

	gvk := nvapi.SchemeGroupVersion.WithKind("MultiNodeEnvironment")
	mne.APIVersion = gvk.GroupVersion().String()
	mne.Kind = gvk.Kind

	ownerReference := metav1.OwnerReference{
		APIVersion: mne.APIVersion,
		Kind:       mne.Kind,
		Name:       mne.Name,
		UID:        mne.UID,
		Controller: ptr.To(true),
	}

	rc, err := m.resourceClaimLister.ResourceClaims(mne.Namespace).Get(mne.Spec.ResourceClaimName)
	if err == nil {
		if len(rc.OwnerReferences) != 1 && rc.OwnerReferences[0] != ownerReference {
			klog.Warningf("ResourceClaim exists without expected OwnerReference: %s", rc.Name)
		}
		return
	}
	if !errors.IsNotFound(err) {
		klog.Warningf("Error retrieving ResourceClaim '%s': %v", err)
		return
	}

	resourceClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            mne.Spec.ResourceClaimName,
			Namespace:       mne.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerReference},
			Finalizers:      []string{resourceClaimFinalizer},
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{{
					Name: "imex", DeviceClassName: imexDeviceClass,
				}},
			},
		},
	}

	_, err = m.clientsets.Core.ResourceV1beta1().ResourceClaims(mne.Namespace).Create(context.Background(), resourceClaim, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("Failed to create ResourceClaim '%s': %v", mne.Spec.ResourceClaimName, err)
	}
}

func (m *MultiNodeEnvironmentManager) onMultiNodeEnvironmentDelete(obj interface{}) {
	mne, ok := obj.(*nvapi.MultiNodeEnvironment)
	if !ok {
		klog.Warning("Failed to cast to MultiNodeEnvironment")
		return
	}

	klog.Infof("MultiNodeEnvironment deleted: %s", mne.Name)

	if err := m.removeResourceClaimFinalizer(mne.Namespace, mne.Spec.ResourceClaimName); err != nil {
		klog.Infof("Error removing finalizer on Resource Claim '%s'", mne.Spec.ResourceClaimName, err)
	}
}

func (m *MultiNodeEnvironmentManager) onResourceClaimAdd(obj interface{}) {
	rc, ok := obj.(*resourceapi.ResourceClaim)
	if !ok {
		klog.Warning("Failed to cast to ResourceClaim")
		return
	}

	if len(rc.OwnerReferences) != 1 {
		return
	}

	if rc.OwnerReferences[0].Kind != nvapi.MultiNodeEnvironmentKind {
		return
	}

	_, err := m.multiNodeEnvironmentLister.MultiNodeEnvironments(rc.Namespace).Get(rc.OwnerReferences[0].Name)
	if err == nil {
		return
	}
	if !errors.IsNotFound(err) {
		klog.Warningf("Error retrieving ResourceClaim's OwnerReference '%s': %v", rc.OwnerReferences[0].Name, err)
		return
	}

	if err := m.removeResourceClaimFinalizer(rc.Namespace, rc.Name); err != nil {
		klog.Warningf("Error removing finalizer on ResourceClaim '%s': %v", rc.Name, err)
	}
}

func (m *MultiNodeEnvironmentManager) removeResourceClaimFinalizer(namespace, name string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		rc, err := m.resourceClaimLister.ResourceClaims(namespace).Get(name)
		if err != nil && errors.IsNotFound(err) {
			return fmt.Errorf("ResourceClaim not found")
		}
		if err != nil {
			return fmt.Errorf("error retrieving ResourceClaim", err)
		}

		newRC := rc.DeepCopy()

		newRC.Finalizers = []string{}
		for _, f := range rc.Finalizers {
			if f != resourceClaimFinalizer {
				newRC.Finalizers = append(newRC.Finalizers, f)
			}
		}

		_, err = m.clientsets.Core.ResourceV1beta1().ResourceClaims(namespace).Update(context.Background(), newRC, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update ResourceClaim: %v", err)
	}

	return nil
}
