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
	DriverName                    = "gpu.nvidia.com"
	ImexDomainLabel               = "nvidia.com/gpu.imex-domain"
	ResourceSliceImexChannelLimit = 128
	DriverImexChannelLimit        = 2048
)

type ImexManager struct {
	waitGroup sync.WaitGroup
	clientset kubernetes.Interface
}

// imexDomainOffsets represents the offset for assigning IMEX channels
// to ResourceSlices for each <imex-domain, cliqueid> combination.
type imexDomainOffsets map[string]map[string]int

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
	driverResources := &resourceslice.DriverResources{}
	controller, err := resourceslice.StartController(ctx, m.clientset, DriverName, owner, driverResources)
	if err != nil {
		return fmt.Errorf("error starting resource slice controller: %w", err)
	}

	imexDomainOffsets := new(imexDomainOffsets)
	m.waitGroup.Add(1)
	go func() {
		defer m.waitGroup.Done()
		for {
			select {
			case addedDomain := <-addedDomainsCh:
				offset, err := imexDomainOffsets.add(addedDomain, ResourceSliceImexChannelLimit, DriverImexChannelLimit)
				if err != nil {
					klog.Errorf("Error calculating channel offset for IMEX domain %s: %v", addedDomain, err)
					return
				}
				klog.Infof("Adding channels for new IMEX domain: %v", addedDomain)
				driverResources := driverResources.DeepCopy()
				driverResources.Pools[addedDomain] = generateImexChannelPool(addedDomain, offset, ResourceSliceImexChannelLimit)
				controller.Update(driverResources)
			case removedDomain := <-removedDomainsCh:
				klog.Infof("Removing channels for removed IMEX domain: %v", removedDomain)
				driverResources := driverResources.DeepCopy()
				delete(driverResources.Pools, removedDomain)
				imexDomainOffsets.remove(removedDomain)
				controller.Update(driverResources)
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
func generateImexChannelPool(imexDomain string, startChannel int, numChannels int) resourceslice.Pool {
	// Generate channels from startChannel to offset+numChannels
	var devices []resourceapi.Device
	for i := startChannel; i < (startChannel + numChannels); i++ {
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
								imexDomain,
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

// add sets the offset where an IMEX domain's channels should start counting from.
func (offsets imexDomainOffsets) add(imexDomain string, resourceSliceImexChannelLimit, driverImexChannelLimit int) (int, error) {
	// Split the incoming imexDomain to split off its cliqueID
	id := strings.SplitN(imexDomain, ".", 2)
	if len(id) != 2 {
		return -1, fmt.Errorf("error adding IMEX domain %s: invalid format", imexDomain)
	}
	imexDomain = id[0]
	cliqueID := id[1]

	// Check if the IMEX domain is already in the map
	if _, ok := offsets[imexDomain]; !ok {
		offsets[imexDomain] = make(map[string]int)
	}

	// Return early if the clique is already in the map
	if offset, exists := offsets[imexDomain][cliqueID]; exists {
		return offset, nil
	}

	// Track used offsets for the current imexDomain
	usedOffsets := make(map[int]struct{})
	for _, v := range offsets[imexDomain] {
		usedOffsets[v] = struct{}{}
	}

	// Look for the first unused offset, stepping by resourceSliceImexChannelLimit
	var offset int
	for offset = 0; offset < driverImexChannelLimit; offset += resourceSliceImexChannelLimit {
		if _, exists := usedOffsets[offset]; !exists {
			break
		}
	}

	// If we reach the limit, return an error
	if offset == driverImexChannelLimit {
		return -1, fmt.Errorf("error adding IMEX domain %s: channel limit reached", imexDomain)
	}
	offsets[imexDomain][cliqueID] = offset

	return offset, nil
}

func (offsets imexDomainOffsets) remove(imexDomain string) {
	id := strings.SplitN(imexDomain, ".", 2)
	if len(id) != 2 {
		return
	}
	imexDomain = id[0]
	cliqueID := id[1]

	delete(offsets[imexDomain], cliqueID)
	if len(offsets[imexDomain]) == 0 {
		delete(offsets, imexDomain)
	}
}
