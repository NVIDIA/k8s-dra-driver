/*
 * Copyright (c) 2024 NVIDIA CORPORATION.  All rights reserved.
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
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	DriverName       = "gpu.nvidia.com"
	ImexDomainLabel  = "nvidia.com/gpu.imex-domain"
	ImexChannelLimit = 128
)

type ImexManager struct {
	waitGroup sync.WaitGroup
	clientset kubernetes.Interface
}

type DriverResources resourceslice.DriverResources

func StartIMEXManager(ctx context.Context, config *Config) (*ImexManager, error) {
	// Build a client set config
	csconfig, err := config.flags.kubeClientConfig.NewClientSetConfig()
	if err != nil {
		return nil, fmt.Errorf("error creating client set config: %w", err)
	}

	// Create a new clientset
	clientset, err := kubernetes.NewForConfig(csconfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic client: %w", err)
	}

	// Fetch the current Pod object
	pod, err := clientset.CoreV1().Pods(config.flags.namespace).Get(ctx, config.flags.podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod: %w", err)
	}

	// Set the owner of the ResourceSlices we will create
	owner := resourceslice.Owner{
		APIVersion: "v1",
		Kind:       "Pod",
		Name:       pod.Name,
		UID:        pod.UID,
	}

	// Create the manager itself
	m := &ImexManager{
		clientset: clientset,
	}

	// Stream added/removed IMEX domains from nodes over time
	klog.Info("Start streaming IMEX domains from nodes...")
	addedDomainsCh, removedDomainsCh, err := m.streamImexDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("error streaming IMEX domains: %w", err)
	}

	// Add/Remove resource slices from IMEX domains as they come and go
	klog.Info("Start publishing IMEX channels to ResourceSlices...")
	err = m.manageResourceSlices(ctx, owner, addedDomainsCh, removedDomainsCh)
	if err != nil {
		return nil, fmt.Errorf("error managing resource slices: %w", err)
	}

	return m, nil
}

// manageResourceSlices reacts to added and removed IMEX domains and triggers the creation / removal of resource slices accordingly.
func (m *ImexManager) manageResourceSlices(ctx context.Context, owner resourceslice.Owner, addedDomainsCh <-chan string, removedDomainsCh <-chan string) error {
	driverResources := resourceslice.DriverResources{}
	controller, err := resourceslice.StartController(ctx, m.clientset, DriverName, owner, &driverResources)
	if err != nil {
		return fmt.Errorf("error starting resource slice controller: %w", err)
	}

	imexChannelPool := make(map[string]int)

	m.waitGroup.Add(1)
	go func() {
		defer m.waitGroup.Done()
		for {
			select {
			case addedDomain := <-addedDomainsCh:
				klog.Infof("Adding channels for new IMEX domain: %v", addedDomain)
				id := strings.Split(addedDomain, ".")
				clique, err := strconv.Atoi(id[1])
				if err != nil {
					klog.Errorf("Error converting string to int: %v", err)
				}
				imexChannelPool[id[0]] = clique
				newDriverResources := DriverResources(driverResources).DeepCopy()
				newDriverResources.Pools[addedDomain] = generateImexChannelPool(id[0], clique)
				controller.Update(&newDriverResources)
				driverResources = newDriverResources
			case removedDomain := <-removedDomainsCh:
				klog.Infof("Removing channels for removed IMEX domain: %v", removedDomain)
				id := strings.Split(removedDomain, ".")
				delete(imexChannelPool, id[0])
				newDriverResources := DriverResources(driverResources).DeepCopy()
				delete(newDriverResources.Pools, removedDomain)
				controller.Update(&newDriverResources)
				driverResources = newDriverResources
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Stop stops a running ImexManager.
func (m *ImexManager) Stop() error {
	if m == nil {
		return nil
	}

	m.waitGroup.Wait()
	klog.Info("Cleaning up all resourceSlices")
	if err := m.cleanupResourceSlices(); err != nil {
		return fmt.Errorf("error cleaning up resource slices: %w", err)
	}

	return nil
}

// DeepCopy will perform a deep copy of the provided DriverResources.
func (d DriverResources) DeepCopy() resourceslice.DriverResources {
	driverResources := resourceslice.DriverResources{
		Pools: make(map[string]resourceslice.Pool),
	}
	for p := range d.Pools {
		id := strings.Split(p, ".")
		clique, err := strconv.Atoi(id[1])
		if err != nil {
			klog.Errorf("Error converting string to int: %v", err)
		}
		driverResources.Pools[p] = generateImexChannelPool(id[0], clique)
	}
	return driverResources
}

// streamImexDomains returns two channels that streams imexDomans that are added and removed from nodes over time.
func (m *ImexManager) streamImexDomains(ctx context.Context) (<-chan string, <-chan string, error) {
	// Create channels to stream IMEX domain ids that are added / removed
	addedDomainCh := make(chan string)
	removedDomainCh := make(chan string)

	// Use a map to track how many nodes are part of a given IMEX domain
	nodesPerImexDomain := make(map[string]int)

	// Build a label selector to get all nodes with ImexDomainLabel set
	requirement, err := labels.NewRequirement(ImexDomainLabel, selection.Exists, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error building label selector requirement: %w", err)
	}
	labelSelector := labels.NewSelector().Add(*requirement).String()

	// Create a shared informer factory for nodes
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		m.clientset,
		time.Minute*10, // Resync period
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = labelSelector
		}),
	)
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()

	// Set up event handlers for node events
	_, err = nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*v1.Node) // nolint:forcetypeassert
			imexDomain := node.Labels[ImexDomainLabel]
			if imexDomain != "" {
				nodesPerImexDomain[imexDomain]++
				if nodesPerImexDomain[imexDomain] == 1 {
					addedDomainCh <- imexDomain
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			node := obj.(*v1.Node) // nolint:forcetypeassert
			imexDomain := node.Labels[ImexDomainLabel]
			if imexDomain != "" {
				nodesPerImexDomain[imexDomain]--
				if nodesPerImexDomain[imexDomain] == 0 {
					removedDomainCh <- imexDomain
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNode := oldObj.(*v1.Node) // nolint:forcetypeassert
			newNode := newObj.(*v1.Node) // nolint:forcetypeassert

			oldImexDomain := oldNode.Labels[ImexDomainLabel]
			newImexDomain := newNode.Labels[ImexDomainLabel]

			if oldImexDomain == newImexDomain {
				return
			}
			if oldImexDomain != "" {
				nodesPerImexDomain[oldImexDomain]--
				if nodesPerImexDomain[oldImexDomain] == 0 {
					removedDomainCh <- oldImexDomain
				}
			}
			if newImexDomain != "" {
				nodesPerImexDomain[newImexDomain]++
				if nodesPerImexDomain[newImexDomain] == 1 {
					addedDomainCh <- newImexDomain
				}
			}
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create node informer: %w", err)
	}

	// Start the informer and wait for it to sync
	m.waitGroup.Add(1)
	go func() {
		defer m.waitGroup.Done()
		informerFactory.Start(ctx.Done())
	}()

	// Wait for the informer caches to sync
	if !cache.WaitForCacheSync(ctx.Done(), nodeInformer.HasSynced) {
		return nil, nil, fmt.Errorf("failed to sync informer caches")
	}

	return addedDomainCh, removedDomainCh, nil
}

// generateImexChannelPool generates the contents of a ResourceSlice pool for a given IMEX domain.
func generateImexChannelPool(imexDomain string, cliqueid int) resourceslice.Pool {
	// Generate dchannels from ImexChannelLimit*(cliqueid-1) to ImexChannelLimit*(cliqueid)
	var devices []resourceapi.Device
	for i := (ImexChannelLimit * (cliqueid - 1)); i < ImexChannelLimit*(cliqueid); i++ {
		d := resourceapi.Device{
			Name: fmt.Sprintf("imex-channel-%d", i),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"type": {
						StringValue: ptr.To("imex-channel"),
					},
					"channel": {
						IntValue: ptr.To(int64(i)),
					},
				},
			},
		}
		devices = append(devices, d)
	}

	// Put them in a pool named after the IMEX domain with the IMEX domain label as a node selector
	pool := resourceslice.Pool{
		NodeSelector: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{
				{
					MatchExpressions: []v1.NodeSelectorRequirement{
						{
							Key:      ImexDomainLabel,
							Operator: v1.NodeSelectorOpIn,
							Values: []string{
								strings.Join([]string{imexDomain, strconv.Itoa(cliqueid)}, "."),
							},
						},
					},
				},
			},
		},
		Devices: devices,
	}

	return pool
}

// cleanupResourceSlices removes all resource slices created by the IMEX manager.
func (m *ImexManager) cleanupResourceSlices() error {
	// Delete all resource slices created by the IMEX manager
	ops := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("%s=%s", resourceapi.ResourceSliceSelectorDriver, DriverName),
	}
	l, err := m.clientset.ResourceV1alpha3().ResourceSlices().List(context.Background(), ops)
	if err != nil {
		return fmt.Errorf("error listing resource slices: %w", err)
	}

	for _, rs := range l.Items {
		err := m.clientset.ResourceV1alpha3().ResourceSlices().Delete(context.Background(), rs.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("error deleting resource slice %s: %w", rs.Name, err)
		}
	}

	return nil
}
