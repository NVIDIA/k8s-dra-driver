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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	resourcelisters "k8s.io/client-go/listers/resource/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	nvapi "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/pkg/flags"
	nvinformers "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/informers/externalversions"
	nvlisters "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/listers/gpu/v1alpha1"
)

const (
	multiNodeEnvironmentFinalizer = "gpu.nvidia.com/finalizer.multiNodeEnvironment"
	imexDeviceClass               = "imex.nvidia.com"

	MultiNodeEnvironmentAddEvent    = "onMultiNodeEnvironmentAddEvent"
	MultiNodeEnvironmentDeleteEvent = "onMultiNodeEnvironmentDeleteEvent"
	ResourceClaimAddEvent           = "ResourceClaimAddEvent"
	DeviceClassAddEvent             = "DeviceClassAddEvent"
)

type WorkItem struct {
	Object    any
	EventType string
}

type MultiNodeEnvironmentManager struct {
	clientsets flags.ClientSets
	waitGroup  sync.WaitGroup
	queue      workqueue.RateLimitingInterface

	multiNodeEnvironmentLister nvlisters.MultiNodeEnvironmentLister
	resourceClaimLister        resourcelisters.ResourceClaimLister
	deviceClassLister          resourcelisters.DeviceClassLister
}

// StartManager starts a MultiNodeEnvironmentManager.
func StartMultiNodeEnvironmentManager(ctx context.Context, config *Config) (*MultiNodeEnvironmentManager, error) {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	nvInformerFactory := nvinformers.NewSharedInformerFactory(config.clientsets.Nvidia, 30*time.Second)
	coreInformerFactory := informers.NewSharedInformerFactory(config.clientsets.Core, 30*time.Second)

	mneInformer := nvInformerFactory.Gpu().V1alpha1().MultiNodeEnvironments().Informer()
	mneLister := nvlisters.NewMultiNodeEnvironmentLister(mneInformer.GetIndexer())

	rcInformer := coreInformerFactory.Resource().V1beta1().ResourceClaims().Informer()
	rcLister := resourcelisters.NewResourceClaimLister(rcInformer.GetIndexer())

	dcInformer := coreInformerFactory.Resource().V1beta1().DeviceClasses().Informer()
	dcLister := resourcelisters.NewDeviceClassLister(dcInformer.GetIndexer())

	m := &MultiNodeEnvironmentManager{
		clientsets:                 config.clientsets,
		queue:                      queue,
		multiNodeEnvironmentLister: mneLister,
		resourceClaimLister:        rcLister,
		deviceClassLister:          dcLister,
	}

	mneInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { m.enqueue(obj, MultiNodeEnvironmentAddEvent) },
		DeleteFunc: func(obj any) { m.enqueue(obj, MultiNodeEnvironmentDeleteEvent) },
	})

	rcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.enqueue(obj, ResourceClaimAddEvent) },
	})

	dcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.enqueue(obj, DeviceClassAddEvent) },
	})

	m.waitGroup.Add(3)
	go func() {
		defer m.waitGroup.Done()
		nvInformerFactory.Start(ctx.Done())
	}()
	go func() {
		defer m.waitGroup.Done()
		coreInformerFactory.Start(ctx.Done())
	}()
	go func() {
		defer m.waitGroup.Done()
		m.run(ctx.Done())
	}()

	if !cache.WaitForCacheSync(ctx.Done(), mneInformer.HasSynced, rcInformer.HasSynced, dcInformer.HasSynced) {
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

func (m *MultiNodeEnvironmentManager) run(done <-chan struct{}) {
	go func() {
		<-done
		m.queue.ShutDown()
	}()
	for {
		select {
		case <-done:
			return
		default:
			m.processNextWorkItem()
		}
	}
}

func (m *MultiNodeEnvironmentManager) enqueue(obj any, eventType string) {
	runtimeObj, ok := obj.(runtime.Object)
	if !ok {
		klog.Warningf("unexpected object type %T", obj)
	}

	workItem := WorkItem{
		Object:    runtimeObj.DeepCopyObject(),
		EventType: eventType,
	}

	m.queue.AddRateLimited(workItem)
}

func (m *MultiNodeEnvironmentManager) processNextWorkItem() {
	item, shutdown := m.queue.Get()
	if shutdown {
		return
	}
	defer m.queue.Done(item)

	workItem, ok := item.(WorkItem)
	if !ok {
		klog.Errorf("Unexpected item in queue: %v", item)
		return
	}

	err := m.reconcile(workItem)
	if err != nil {
		klog.Errorf("Failed to reconcile work item %v: %v", workItem.Object, err)
		m.queue.AddRateLimited(workItem)
	} else {
		m.queue.Forget(workItem)
	}
}

func (m *MultiNodeEnvironmentManager) reconcile(workItem WorkItem) error {
	switch workItem.EventType {
	case MultiNodeEnvironmentAddEvent:
		return m.onMultiNodeEnvironmentAdd(workItem.Object)
	case MultiNodeEnvironmentDeleteEvent:
		return m.onMultiNodeEnvironmentDelete(workItem.Object)
	case ResourceClaimAddEvent:
		return m.onResourceClaimAdd(workItem.Object)
	case DeviceClassAddEvent:
		return m.onDeviceClassAdd(workItem.Object)
	}
	return fmt.Errorf("unknown event type: %s", workItem.EventType)
}

func (m *MultiNodeEnvironmentManager) onMultiNodeEnvironmentAdd(obj any) error {
	mne, ok := obj.(*nvapi.MultiNodeEnvironment)
	if !ok {
		return fmt.Errorf("failed to cast to MultiNodeEnvironment")
	}

	klog.Infof("Processing added MultiNodeEnvironment: %s/%s", mne.Namespace, mne.Name)

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
			return fmt.Errorf("ResourceClaim exists without expected OwnerReference: %s", rc.Name)
		}
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving ResourceClaim '%s': %w", err)
	}

	resourceClaim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            mne.Spec.ResourceClaimName,
			Namespace:       mne.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerReference},
			Finalizers:      []string{multiNodeEnvironmentFinalizer},
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{{
					Name: "imex", DeviceClassName: imexDeviceClass,
				}},
			},
		},
	}

	_, err = m.clientsets.Core.ResourceV1beta1().ResourceClaims(resourceClaim.Namespace).Create(context.Background(), resourceClaim, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create ResourceClaim '%s': %w", resourceClaim.Name, err)
	}

	return nil
}

func (m *MultiNodeEnvironmentManager) onMultiNodeEnvironmentDelete(obj any) error {
	mne, ok := obj.(*nvapi.MultiNodeEnvironment)
	if !ok {
		return fmt.Errorf("failed to cast to MultiNodeEnvironment")
	}

	klog.Infof("Processing deleted MultiNodeEnvironment: %s/%s", mne.Namespace, mne.Name)

	if err := m.removeResourceClaimFinalizer(mne.Namespace, mne.Spec.ResourceClaimName); err != nil {
		return fmt.Errorf("error removing finalizer on ResourceClaim '%s': %w", mne.Spec.ResourceClaimName, err)
	}

	return nil
}

func (m *MultiNodeEnvironmentManager) onResourceClaimAdd(obj any) error {
	rc, ok := obj.(*resourceapi.ResourceClaim)
	if !ok {
		return fmt.Errorf("failed to cast to ResourceClaim")
	}

	klog.Infof("Processing added ResourceClaim: %s/%s", rc.Namespace, rc.Name)

	if len(rc.OwnerReferences) != 1 {
		return nil
	}

	if rc.OwnerReferences[0].Kind != nvapi.MultiNodeEnvironmentKind {
		return nil
	}

	_, err := m.multiNodeEnvironmentLister.MultiNodeEnvironments(rc.Namespace).Get(rc.OwnerReferences[0].Name)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving ResourceClaim's OwnerReference '%s': %w", rc.OwnerReferences[0].Name, err)
	}

	if err := m.removeResourceClaimFinalizer(rc.Namespace, rc.Name); err != nil {
		return fmt.Errorf("error removing finalizer on ResourceClaim '%s': %w", rc.Name, err)
	}

	return nil
}

func (m *MultiNodeEnvironmentManager) onDeviceClassAdd(obj interface{}) error {
	dc, ok := obj.(*resourceapi.DeviceClass)
	if !ok {
		return fmt.Errorf("failed to cast to DeviceClass")
	}

	klog.Infof("Processing added DeviceClass: %s/%s", dc.Namespace, dc.Name)

	if len(dc.OwnerReferences) != 1 {
		return nil
	}

	if dc.OwnerReferences[0].Kind != nvapi.MultiNodeEnvironmentKind {
		return nil
	}

	_, err := m.multiNodeEnvironmentLister.MultiNodeEnvironments(dc.Namespace).Get(dc.OwnerReferences[0].Name)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("error retrieving DeviceClass's OwnerReference '%s': %w", dc.OwnerReferences[0].Name, err)
	}

	if err := m.removeDeviceClassFinalizer(dc.Name); err != nil {
		return fmt.Errorf("error removing finalizer on DeviceClass '%s': %w", dc.Name, err)
	}

	return nil
}

func (m *MultiNodeEnvironmentManager) removeResourceClaimFinalizer(namespace, name string) error {
	rc, err := m.resourceClaimLister.ResourceClaims(namespace).Get(name)
	if err != nil && errors.IsNotFound(err) {
		return fmt.Errorf("ResourceClaim not found")
	}
	if err != nil {
		return fmt.Errorf("error retrieving ResourceClaim: %w", err)
	}

	newRC := rc.DeepCopy()

	newRC.Finalizers = []string{}
	for _, f := range rc.Finalizers {
		if f != multiNodeEnvironmentFinalizer {
			newRC.Finalizers = append(newRC.Finalizers, f)
		}
	}

	_, err = m.clientsets.Core.ResourceV1beta1().ResourceClaims(namespace).Update(context.Background(), newRC, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ResourceClaim: %w", err)
	}

	return nil
}

func (m *MultiNodeEnvironmentManager) removeDeviceClassFinalizer(name string) error {
	dc, err := m.deviceClassLister.Get(name)
	if err != nil && errors.IsNotFound(err) {
		return fmt.Errorf("DeviceClass not found")
	}
	if err != nil {
		return fmt.Errorf("error retrieving DeviceClass: %w", err)
	}

	newDC := dc.DeepCopy()

	newDC.Finalizers = []string{}
	for _, f := range dc.Finalizers {
		if f != multiNodeEnvironmentFinalizer {
			newDC.Finalizers = append(newDC.Finalizers, f)
		}
	}

	_, err = m.clientsets.Core.ResourceV1beta1().DeviceClasses().Update(context.Background(), newDC, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update DeviceClass: %w", err)
	}

	return nil
}
