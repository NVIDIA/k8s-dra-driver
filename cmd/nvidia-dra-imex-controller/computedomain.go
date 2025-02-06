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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	nvapi "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/v1beta1"
	nvinformers "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/informers/externalversions"
)

type GetComputeDomainFunc func(uid string) (*nvapi.ComputeDomain, error)

const (
	informerResyncPeriod = 10 * time.Minute

	computeDomainLabelKey  = "resource.nvidia.com/computeDomain"
	computeDomainFinalizer = computeDomainLabelKey
)

type ComputeDomainManager struct {
	config        *ManagerConfig
	waitGroup     sync.WaitGroup
	cancelContext context.CancelFunc

	factory  nvinformers.SharedInformerFactory
	informer cache.SharedIndexInformer

	deploymentManager    *DeploymentManager
	deviceClassManager   *DeviceClassManager
	resourceClaimManager *ResourceClaimManager
	imexChannelManager   *ImexChannelManager
}

// NewComputeDomainManager creates a new ComputeDomainManager.
func NewComputeDomainManager(config *ManagerConfig) *ComputeDomainManager {
	factory := nvinformers.NewSharedInformerFactory(config.clientsets.Nvidia, informerResyncPeriod)
	informer := factory.Resource().V1beta1().ComputeDomains().Informer()

	m := &ComputeDomainManager{
		config:   config,
		factory:  factory,
		informer: informer,
	}
	m.deploymentManager = NewDeploymentManager(config, m.Get)
	m.deviceClassManager = NewDeviceClassManager(config)
	m.resourceClaimManager = NewResourceClaimManager(config)
	m.imexChannelManager = NewImexChannelManager(config)

	return m
}

// Start starts a ComputeDomainManager.
func (m *ComputeDomainManager) Start(ctx context.Context) (rerr error) {
	ctx, cancel := context.WithCancel(ctx)
	m.cancelContext = cancel

	defer func() {
		if rerr != nil {
			if err := m.Stop(); err != nil {
				klog.Errorf("error stopping ComputeDomain manager: %v", err)
			}
		}
	}()

	err := m.informer.AddIndexers(cache.Indexers{
		"uid": uidIndexer[*nvapi.ComputeDomain],
	})
	if err != nil {
		return fmt.Errorf("error adding indexer for UIDs: %w", err)
	}

	_, err = m.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			m.config.workQueue.Enqueue(obj, m.onAddOrUpdate)
		},
		UpdateFunc: func(oldObj, newObj any) {
			m.config.workQueue.Enqueue(newObj, m.onAddOrUpdate)
		},
	})
	if err != nil {
		return fmt.Errorf("error adding event handlers for ComputeDomain informer: %w", err)
	}

	m.waitGroup.Add(1)
	go func() {
		defer m.waitGroup.Done()
		m.factory.Start(ctx.Done())
	}()

	if !cache.WaitForCacheSync(ctx.Done(), m.informer.HasSynced) {
		return fmt.Errorf("informer cache sync for ComputeDomains failed")
	}

	if err := m.deploymentManager.Start(ctx); err != nil {
		return fmt.Errorf("error starting Deployment manager: %w", err)
	}

	if err := m.deviceClassManager.Start(ctx); err != nil {
		return fmt.Errorf("error creating DeviceClass manager: %w", err)
	}

	if err := m.resourceClaimManager.Start(ctx); err != nil {
		return fmt.Errorf("error creating ResourceClaim manager: %w", err)
	}

	if err := m.imexChannelManager.Start(ctx); err != nil {
		return fmt.Errorf("error starting IMEX channel manager: %w", err)
	}

	return nil
}

func (m *ComputeDomainManager) Stop() error {
	if err := m.deploymentManager.Stop(); err != nil {
		return fmt.Errorf("error stopping Deployment manager: %w", err)
	}
	if err := m.resourceClaimManager.Stop(); err != nil {
		return fmt.Errorf("error stopping ResourceClaim manager: %w", err)
	}
	if err := m.deviceClassManager.Stop(); err != nil {
		return fmt.Errorf("error stopping DeviceClass manager: %w", err)
	}
	if err := m.imexChannelManager.Stop(); err != nil {
		return fmt.Errorf("error stopping IMEX channel manager: %w", err)
	}
	m.cancelContext()
	m.waitGroup.Wait()
	return nil
}

// Get gets a ComputeDomain with a specific UID.
func (m *ComputeDomainManager) Get(uid string) (*nvapi.ComputeDomain, error) {
	cds, err := m.informer.GetIndexer().ByIndex("uid", uid)
	if err != nil {
		return nil, fmt.Errorf("error retrieving ComputeDomain by UID: %w", err)
	}
	if len(cds) == 0 {
		return nil, nil
	}
	if len(cds) != 1 {
		return nil, fmt.Errorf("multiple ComputeDomains with the same UID")
	}
	cd, ok := cds[0].(*nvapi.ComputeDomain)
	if !ok {
		return nil, fmt.Errorf("failed to cast to ComputeDomain")
	}
	return cd, nil
}

// RemoveFinalizer removes the finalizer from a ComputeDomain.
func (m *ComputeDomainManager) RemoveFinalizer(ctx context.Context, uid string) error {
	cd, err := m.Get(uid)
	if err != nil {
		return fmt.Errorf("error retrieving ComputeDomain: %w", err)
	}

	if cd.GetDeletionTimestamp() == nil {
		return fmt.Errorf("attempting to remove finalizer before ComputeDomain marked for deletion")
	}

	newCD := cd.DeepCopy()
	newCD.Finalizers = []string{}
	for _, f := range cd.Finalizers {
		if f != computeDomainFinalizer {
			newCD.Finalizers = append(newCD.Finalizers, f)
		}
	}
	if len(cd.Finalizers) == len(newCD.Finalizers) {
		return nil
	}

	if _, err = m.config.clientsets.Nvidia.ResourceV1beta1().ComputeDomains(cd.Namespace).Update(ctx, newCD, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error updating ComputeDomain: %w", err)
	}

	return nil
}

func (m *ComputeDomainManager) addFinalizer(ctx context.Context, cd *nvapi.ComputeDomain) error {
	for _, f := range cd.Finalizers {
		if f == computeDomainFinalizer {
			return nil
		}
	}

	newCD := cd.DeepCopy()
	newCD.Finalizers = append(newCD.Finalizers, computeDomainFinalizer)
	if _, err := m.config.clientsets.Nvidia.ResourceV1beta1().ComputeDomains(cd.Namespace).Update(ctx, newCD, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error updating ComputeDomain: %w", err)
	}

	return nil
}

func (m *ComputeDomainManager) onAddOrUpdate(ctx context.Context, obj any) error {
	cd, ok := obj.(*nvapi.ComputeDomain)
	if !ok {
		return fmt.Errorf("failed to cast to ComputeDomain")
	}

	klog.Infof("Processing added or updated ComputeDomain: %s/%s", cd.Namespace, cd.Name)

	if cd.GetDeletionTimestamp() != nil {
		if err := m.imexChannelManager.DeletePool(string(cd.UID)); err != nil {
			return fmt.Errorf("error deleting IMEX channel pool: %w", err)
		}

		if err := m.resourceClaimManager.Delete(ctx, string(cd.UID)); err != nil {
			return fmt.Errorf("error deleting ResourceClaim: %w", err)
		}

		if err := m.deviceClassManager.Delete(ctx, string(cd.UID)); err != nil {
			return fmt.Errorf("error deleting DeviceClass: %w", err)
		}

		if err := m.deploymentManager.Delete(ctx, string(cd.UID)); err != nil {
			return fmt.Errorf("error deleting Deployment: %w", err)
		}

		// TODO: Condition the removal of these finalizers on there being no
		// workloads running in the compute domain. One idea to do this is to
		// (1) ensure that the ResourceSlice associated with the ComputeDomain
		// has been deleted, and (2) track the allocation of channels in the
		// ComputeDomain status and wait for that list to become empty.
		if true {
			if err := m.resourceClaimManager.RemoveFinalizer(ctx, string(cd.UID)); err != nil {
				return fmt.Errorf("error deleting ResourceClaim: %w", err)
			}

			if err := m.deviceClassManager.RemoveFinalizer(ctx, string(cd.UID)); err != nil {
				return fmt.Errorf("error deleting DeviceClass: %w", err)
			}

			if err := m.deploymentManager.RemoveFinalizer(ctx, string(cd.UID)); err != nil {
				return fmt.Errorf("error deleting Deployment: %w", err)
			}

			if err := m.RemoveFinalizer(ctx, string(cd.UID)); err != nil {
				return fmt.Errorf("error removing finalizer: %w", err)
			}
		}

		return nil
	}

	if err := m.addFinalizer(ctx, cd); err != nil {
		return fmt.Errorf("error adding finalizer: %w", err)
	}

	if _, err := m.deploymentManager.Create(ctx, m.config.driverNamespace, cd.Spec.NumNodes, cd); err != nil {
		return fmt.Errorf("error creating Deployment: %w", err)
	}

	dc, err := m.deviceClassManager.Create(ctx, cd.Spec.DeviceClassName, cd)
	if err != nil {
		return fmt.Errorf("error creating DeviceClass: %w", err)
	}

	for _, name := range cd.Spec.ResourceClaimNames {
		if _, err := m.resourceClaimManager.Create(ctx, cd.Namespace, name, dc.Name, cd); err != nil {
			return fmt.Errorf("error creating ResourceClaim '%s/%s': %w", cd.Namespace, name, err)
		}
	}

	if cd.Status.Status != nvapi.ComputeDomainStatusReady {
		return nil
	}

	if err := m.createOrUpdatePool(ctx, cd); err != nil {
		return fmt.Errorf("error creating or updating pool: %w", err)
	}

	return nil
}

func (m *ComputeDomainManager) createOrUpdatePool(ctx context.Context, cd *nvapi.ComputeDomain) error {
	var nodeNames []string
	for _, node := range cd.Status.Nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	nodeSelector := corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "kubernetes.io/hostname",
						Operator: corev1.NodeSelectorOpIn,
						Values:   nodeNames,
					},
				},
			},
		},
	}

	if err := m.imexChannelManager.CreateOrUpdatePool(string(cd.UID), &nodeSelector); err != nil {
		return fmt.Errorf("failed to create or update IMEX channel pool: %w", err)
	}

	return nil
}
