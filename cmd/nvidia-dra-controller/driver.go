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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-helpers/dra/controller"

	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/clientset/versioned"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

const (
	DriverName       = nvcrd.GroupName
	DriverVersion    = nvcrd.Version
	DriverAPIVersion = DriverName + "/" + DriverVersion
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

func (d driver) GetClassParameters(ctx context.Context, class *corev1.ResourceClass) (interface{}, error) {
	if class.Parameters == nil {
		return nvcrd.DefaultDeviceClassParametersSpec(), nil
	}
	if class.Parameters.APIVersion != DriverAPIVersion {
		return nil, fmt.Errorf("incorrect API group and version: %v", class.Parameters.APIVersion)
	}
	dc, err := d.clientset.DraV1().DeviceClassParameters().Get(ctx, class.Parameters.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting DeviceClassParameters called '%v': %v", class.Parameters.Name, err)
	}
	return &dc.Spec, nil
}

func (d driver) GetClaimParameters(ctx context.Context, claim *corev1.ResourceClaim, class *corev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	if claim.Spec.Parameters == nil {
		return nvcrd.DefaultGpuClaimParametersSpec(), nil
	}
	if claim.Spec.Parameters.APIVersion != DriverAPIVersion {
		return nil, fmt.Errorf("incorrect API group and version: %v", claim.Spec.Parameters.APIVersion)
	}
	switch claim.Spec.Parameters.Kind {
	case nvcrd.GpuClaimParametersKind:
		gc, err := d.clientset.DraV1().GpuClaimParameters(claim.Namespace).Get(ctx, claim.Spec.Parameters.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error getting GpuClaimParameters called '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
		}
		err = d.gpu.ValidateClaimParameters(&gc.Spec)
		if err != nil {
			return nil, fmt.Errorf("error validating GpuClaimParameters called '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
		}
		return &gc.Spec, nil
	case nvcrd.MigDeviceClaimParametersKind:
		mc, err := d.clientset.DraV1().MigDeviceClaimParameters(claim.Namespace).Get(ctx, claim.Spec.Parameters.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error getting MigDeviceClaimParameters called '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
		}
		err = d.mig.ValidateClaimParameters(&mc.Spec)
		if err != nil {
			return nil, fmt.Errorf("error validating MigDeviceClaimParameters called '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
		}
		return &mc.Spec, nil
	}
	return nil, fmt.Errorf("unknown ResourceClaim.Parameters.Kind: %v", claim.Spec.Parameters.Kind)
}

func (d driver) Allocate(ctx context.Context, claim *corev1.ResourceClaim, claimParameters interface{}, class *corev1.ResourceClass, classParameters interface{}, selectedNode string) (*corev1.AllocationResult, error) {
	if selectedNode == "" {
		return nil, fmt.Errorf("TODO: immediate allocations not yet supported")
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nvcrd.NodeAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}

	nascrd := nvcrd.NewNodeAllocationState(crdconfig, d.clientset)
	err := nascrd.Get()
	if err != nil {
		return nil, fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if nascrd.Spec.ClaimRequests == nil {
		nascrd.Spec.ClaimRequests = make(map[string]nvcrd.RequestedDevices)
	}

	if _, exists := nascrd.Spec.ClaimRequests[string(claim.UID)]; exists {
		return buildAllocationResult(selectedNode, true), nil
	}

	if nascrd.Status != nvcrd.NodeAllocationStateStatusReady {
		return nil, fmt.Errorf("NodeAllocationStateStatus: %v", nascrd.Status)
	}

	var onSuccess OnSuccessCallback
	classParams := classParameters.(*nvcrd.DeviceClassParametersSpec)
	switch claimParams := claimParameters.(type) {
	case *nvcrd.GpuClaimParametersSpec:
		onSuccess, err = d.gpu.Allocate(nascrd, claim, claimParams, class, classParams, selectedNode)
	case *nvcrd.MigDeviceClaimParametersSpec:
		onSuccess, err = d.mig.Allocate(nascrd, claim, claimParams, class, classParams, selectedNode)
	default:
		err = fmt.Errorf("unknown ResourceClaim.Parameters.Kind: %v", claim.Spec.Parameters.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to allocate devices on node '%v': %v", selectedNode, err)
	}

	err = nascrd.Update(&nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating NodeAllocationState CRD: %v", err)
	}

	onSuccess()

	return buildAllocationResult(selectedNode, true), nil
}

func (d driver) Deallocate(ctx context.Context, claim *corev1.ResourceClaim) error {
	selectedNode := getSelectedNode(claim)
	if selectedNode == "" {
		return nil
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nvcrd.NodeAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}

	nascrd := nvcrd.NewNodeAllocationState(crdconfig, d.clientset)
	err := nascrd.Get()
	if err != nil {
		return fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if nascrd.Spec.ClaimRequests == nil {
		return nil
	}

	if _, exists := nascrd.Spec.ClaimRequests[string(claim.UID)]; !exists {
		return nil
	}

	devices := nascrd.Spec.ClaimRequests[string(claim.UID)]
	switch devices.Type() {
	case nvcrd.GpuDeviceType:
		err = d.gpu.Deallocate(nascrd, claim)
	case nvcrd.MigDeviceType:
		err = d.mig.Deallocate(nascrd, claim)
	default:
		err = fmt.Errorf("unknown RequestedDevices.Type(): %v", devices.Type())
	}
	if err != nil {
		return fmt.Errorf("unable to deallocate devices '%v': %v", devices, err)
	}

	delete(nascrd.Spec.ClaimRequests, string(claim.UID))

	err = nascrd.Update(&nascrd.Spec)
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

	crdconfig := &nvcrd.NodeAllocationStateConfig{
		Name:      potentialNode,
		Namespace: d.namespace,
	}

	nascrd := nvcrd.NewNodeAllocationState(crdconfig, d.clientset)
	err := nascrd.Get()
	if err != nil {
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}

	if nascrd.Status != nvcrd.NodeAllocationStateStatusReady {
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}

	if nascrd.Spec.ClaimRequests == nil {
		nascrd.Spec.ClaimRequests = make(map[string]nvcrd.RequestedDevices)
	}

	perKindCas := make(map[string][]*controller.ClaimAllocation)
	for _, ca := range allcas {
		var kind string
		switch ca.ClaimParameters.(type) {
		case *nvcrd.GpuClaimParametersSpec:
			kind = nvcrd.GpuClaimParametersKind
		case *nvcrd.MigDeviceClaimParametersSpec:
			kind = nvcrd.MigDeviceClaimParametersKind
		}
		perKindCas[kind] = append(perKindCas[kind], ca)
	}
	for _, kind := range []string{nvcrd.GpuClaimParametersKind, nvcrd.MigDeviceClaimParametersKind} {
		var err error
		switch kind {
		case nvcrd.GpuClaimParametersKind:
			err = d.gpu.UnsuitableNode(nascrd, pod, perKindCas[kind], allcas, potentialNode)
		case nvcrd.MigDeviceClaimParametersKind:
			err = d.mig.UnsuitableNode(nascrd, pod, perKindCas[kind], allcas, potentialNode)
		}
		if err != nil {
			return fmt.Errorf("error processing '%v': %v", kind, err)
		}
	}

	return nil
}

func buildAllocationResult(selectedNode string, shared bool) *corev1.AllocationResult {
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
	allocation := &corev1.AllocationResult{
		AvailableOnNodes: nodeSelector,
		SharedResource:   shared,
	}
	return allocation
}

func getSelectedNode(claim *corev1.ResourceClaim) string {
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
