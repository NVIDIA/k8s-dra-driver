/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/dynamic-resource-allocation/controller"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	nasclient "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1/client"
	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/clientset/versioned"
)

const (
	DriverName     = gpucrd.GroupName
	DriverAPIGroup = gpucrd.GroupName
)

type OnSuccessCallback func()

type driver struct {
	lock      *PerNodeMutex
	namespace string
	clientset nvclientset.Interface
	gpu       *gpudriver
	mig       *migdriver
}

var _ controller.Driver = (*driver)(nil)

func NewDriver(config *Config) *driver {
	return &driver{
		lock:      NewPerNodeMutex(),
		namespace: config.namespace,
		clientset: config.clientset.nvidia,
		gpu:       NewGpuDriver(),
		mig:       NewMigDriver(),
	}
}

func (d driver) GetClassParameters(ctx context.Context, class *resourcev1.ResourceClass) (interface{}, error) {
	if class.ParametersRef == nil {
		return gpucrd.DefaultDeviceClassParametersSpec(), nil
	}
	if class.ParametersRef.APIGroup != DriverAPIGroup {
		return nil, fmt.Errorf("incorrect API group: %v", class.ParametersRef.APIGroup)
	}
	dc, err := d.clientset.GpuV1alpha1().DeviceClassParameters().Get(ctx, class.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting DeviceClassParameters called '%v': %v", class.ParametersRef.Name, err)
	}
	return &dc.Spec, nil
}

func (d driver) GetClaimParameters(ctx context.Context, claim *resourcev1.ResourceClaim, class *resourcev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	if claim.Spec.ParametersRef == nil {
		return gpucrd.DefaultGpuClaimParametersSpec(), nil
	}
	if claim.Spec.ParametersRef.APIGroup != DriverAPIGroup {
		return nil, fmt.Errorf("incorrect API group: %v", claim.Spec.ParametersRef.APIGroup)
	}
	switch claim.Spec.ParametersRef.Kind {
	case gpucrd.GpuClaimParametersKind:
		gc, err := d.clientset.GpuV1alpha1().GpuClaimParameters(claim.Namespace).Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error getting GpuClaimParameters called '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		err = d.gpu.ValidateClaimParameters(&gc.Spec)
		if err != nil {
			return nil, fmt.Errorf("error validating GpuClaimParameters called '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		return &gc.Spec, nil
	case gpucrd.MigDeviceClaimParametersKind:
		mc, err := d.clientset.GpuV1alpha1().MigDeviceClaimParameters(claim.Namespace).Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error getting MigDeviceClaimParameters called '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		err = d.mig.ValidateClaimParameters(&mc.Spec)
		if err != nil {
			return nil, fmt.Errorf("error validating MigDeviceClaimParameters called '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		return &mc.Spec, nil
	}
	return nil, fmt.Errorf("unknown ResourceClaim.ParametersRef.Kind: %v", claim.Spec.ParametersRef.Kind)
}

func (d driver) Allocate(ctx context.Context, claim *resourcev1.ResourceClaim, claimParameters interface{}, class *resourcev1.ResourceClass, classParameters interface{}, selectedNode string) (*resourcev1.AllocationResult, error) {
	if selectedNode == "" {
		return nil, fmt.Errorf("TODO: immediate allocations not yet supported")
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nascrd.NodeAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}
	crd := nascrd.NewNodeAllocationState(crdconfig)

	client := nasclient.New(crd, d.clientset.NasV1alpha1())
	err := client.Get()
	if err != nil {
		return nil, fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if crd.Spec.ClaimRequests == nil {
		crd.Spec.ClaimRequests = make(map[string]nascrd.RequestedDevices)
	}

	if _, exists := crd.Spec.ClaimRequests[string(claim.UID)]; exists {
		return buildAllocationResult(selectedNode, true), nil
	}

	if crd.Status != nascrd.NodeAllocationStateStatusReady {
		return nil, fmt.Errorf("NodeAllocationStateStatus: %v", crd.Status)
	}

	var onSuccess OnSuccessCallback
	classParams := classParameters.(*gpucrd.DeviceClassParametersSpec)
	switch claimParams := claimParameters.(type) {
	case *gpucrd.GpuClaimParametersSpec:
		onSuccess, err = d.gpu.Allocate(crd, claim, claimParams, class, classParams, selectedNode)
	case *gpucrd.MigDeviceClaimParametersSpec:
		onSuccess, err = d.mig.Allocate(crd, claim, claimParams, class, classParams, selectedNode)
	default:
		err = fmt.Errorf("unknown ResourceClaim.ParametersRef.Kind: %v", claim.Spec.ParametersRef.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to allocate devices on node '%v': %v", selectedNode, err)
	}

	requested := crd.Spec.ClaimRequests[string(claim.UID)]
	requested.ClaimInfo = &nascrd.ClaimInfo{
		Namespace: claim.Namespace,
		Name:      claim.Name,
		UID:       string(claim.UID),
	}
	crd.Spec.ClaimRequests[string(claim.UID)] = requested

	err = client.Update(&crd.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating NodeAllocationState CRD: %v", err)
	}

	onSuccess()

	return buildAllocationResult(selectedNode, true), nil
}

func (d driver) Deallocate(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	selectedNode := getSelectedNode(claim)
	if selectedNode == "" {
		return nil
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nascrd.NodeAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}
	crd := nascrd.NewNodeAllocationState(crdconfig)

	client := nasclient.New(crd, d.clientset.NasV1alpha1())
	err := client.Get()
	if err != nil {
		return fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if crd.Spec.ClaimRequests == nil {
		return nil
	}

	if _, exists := crd.Spec.ClaimRequests[string(claim.UID)]; !exists {
		return nil
	}

	devices := crd.Spec.ClaimRequests[string(claim.UID)]
	switch devices.Type() {
	case nascrd.GpuDeviceType:
		err = d.gpu.Deallocate(crd, claim)
	case nascrd.MigDeviceType:
		err = d.mig.Deallocate(crd, claim)
	default:
		err = fmt.Errorf("unknown RequestedDevices.Type(): %v", devices.Type())
	}
	if err != nil {
		return fmt.Errorf("unable to deallocate devices '%v': %v", devices, err)
	}

	delete(crd.Spec.ClaimRequests, string(claim.UID))

	err = client.Update(&crd.Spec)
	if err != nil {
		return fmt.Errorf("error updating NodeAllocationState CRD: %v", err)
	}

	return nil
}

func (d driver) UnsuitableNodes(ctx context.Context, pod *corev1.Pod, cas []*controller.ClaimAllocation, potentialNodes []string) error {
	for _, node := range potentialNodes {
		err := d.unsuitableNode(ctx, pod, cas, node)
		if err != nil {
			return fmt.Errorf("error processing node '%v': %v", node, err)
		}
	}

	for _, ca := range cas {
		ca.UnsuitableNodes = unique(ca.UnsuitableNodes)
	}

	return nil
}

func (d driver) unsuitableNode(ctx context.Context, pod *corev1.Pod, allcas []*controller.ClaimAllocation, potentialNode string) error {
	d.lock.Get(potentialNode).Lock()
	defer d.lock.Get(potentialNode).Unlock()

	crdconfig := &nascrd.NodeAllocationStateConfig{
		Name:      potentialNode,
		Namespace: d.namespace,
	}
	crd := nascrd.NewNodeAllocationState(crdconfig)

	client := nasclient.New(crd, d.clientset.NasV1alpha1())
	err := client.Get()
	if err != nil {
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}

	if crd.Status != nascrd.NodeAllocationStateStatusReady {
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}

	if crd.Spec.ClaimRequests == nil {
		crd.Spec.ClaimRequests = make(map[string]nascrd.RequestedDevices)
	}

	perKindCas := make(map[string][]*controller.ClaimAllocation)
	for _, ca := range allcas {
		var kind string
		switch ca.ClaimParameters.(type) {
		case *gpucrd.GpuClaimParametersSpec:
			kind = gpucrd.GpuClaimParametersKind
		case *gpucrd.MigDeviceClaimParametersSpec:
			kind = gpucrd.MigDeviceClaimParametersKind
		}
		perKindCas[kind] = append(perKindCas[kind], ca)
	}
	for _, kind := range []string{gpucrd.GpuClaimParametersKind, gpucrd.MigDeviceClaimParametersKind} {
		var err error
		switch kind {
		case gpucrd.GpuClaimParametersKind:
			err = d.gpu.UnsuitableNode(crd, pod, perKindCas[kind], allcas, potentialNode)
		case gpucrd.MigDeviceClaimParametersKind:
			err = d.mig.UnsuitableNode(crd, pod, perKindCas[kind], allcas, potentialNode)
		}
		if err != nil {
			return fmt.Errorf("error processing '%v': %v", kind, err)
		}
	}

	return nil
}

func buildAllocationResult(selectedNode string, shareable bool) *resourcev1.AllocationResult {
	nodeSelector := &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchFields: []corev1.NodeSelectorRequirement{
					{
						Key:      "metadata.name",
						Operator: "In",
						Values:   []string{selectedNode},
					},
				},
			},
		},
	}
	allocation := &resourcev1.AllocationResult{
		AvailableOnNodes: nodeSelector,
		Shareable:        shareable,
	}
	return allocation
}

func getSelectedNode(claim *resourcev1.ResourceClaim) string {
	if claim.Status.Allocation == nil {
		return ""
	}
	if claim.Status.Allocation.AvailableOnNodes == nil {
		return ""
	}
	return claim.Status.Allocation.AvailableOnNodes.NodeSelectorTerms[0].MatchFields[0].Values[0]
}

func unique(s []string) []string {
	set := make(map[string]struct{})
	var news []string
	for _, str := range s {
		if _, exists := set[str]; !exists {
			set[str] = struct{}{}
			news = append(news, str)
		}
	}
	return news
}
