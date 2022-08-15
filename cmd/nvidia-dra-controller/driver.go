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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/k8s-dra-driver/pkg/controller"
	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/clientset/versioned"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

const (
	DriverName       = nvcrd.GroupName
	DriverVersion    = nvcrd.Version
	DriverAPIVersion = DriverName + "/" + DriverVersion
)

type driver struct {
	lock      *PerNodeMutex
	namespace string
	clientset nvclientset.Interface
	gpu       *gpudriver
}

var _ controller.Driver = (*driver)(nil)

func NewDriver(config *Config) *driver {
	return &driver{
		lock:      NewPerNodeMutex(),
		namespace: config.namespace,
		clientset: config.clientset.nvidia,
		gpu:       NewGpuDriver(),
	}
}

func (d driver) GetClassParameters(ctx context.Context, class *corev1.ResourceClass) (interface{}, error) {
	if class.Parameters == nil {
		return nvcrd.DefaultDeviceClassSpec(), nil
	}
	if class.Parameters.APIVersion != DriverAPIVersion {
		return nil, fmt.Errorf("incorrect API group and version: %v", class.Parameters.APIVersion)
	}
	dc, err := d.clientset.DraV1().DeviceClasses().Get(ctx, class.Parameters.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting DeviceClass called '%v': %v", class.Parameters.Name, err)
	}
	return &dc.Spec, nil
}

func (d driver) GetClaimParameters(ctx context.Context, claim *corev1.ResourceClaim, class *corev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	if claim.Spec.Parameters == nil {
		return nil, fmt.Errorf("missing claim parameters")
	}
	if claim.Spec.Parameters.APIVersion != DriverAPIVersion {
		return nil, fmt.Errorf("incorrect API group and version: %v", claim.Spec.Parameters.APIVersion)
	}
	switch claim.Spec.Parameters.Kind {
	case nvcrd.GpuClaimKind:
		gc, err := d.clientset.DraV1().GpuClaims(claim.Namespace).Get(ctx, claim.Spec.Parameters.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error getting GpuClaim called '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
		}
		return &gc.Spec, nil
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

	if nascrd.Spec.ClaimRequirements == nil {
		nascrd.Spec.ClaimRequirements = make(map[string]nvcrd.DeviceRequirements)
	}

	if _, exists := nascrd.Spec.ClaimRequirements[string(claim.UID)]; exists {
		return buildAllocationResult(selectedNode, true), nil
	}

	if nascrd.Status != nvcrd.NodeAllocationStateStatusReady {
		return nil, fmt.Errorf("NodeAllocationStateStatus: %v", nascrd.Status)
	}

	classSpec := classParameters.(*nvcrd.DeviceClassSpec)
	switch claim.Spec.Parameters.Kind {
	case nvcrd.GpuClaimKind:
		claimSpec := claimParameters.(*nvcrd.GpuClaimSpec)
		err = d.gpu.Allocate(nascrd, claim, claimSpec, class, classSpec, selectedNode)
	default:
		err = fmt.Errorf("unknown ResourceClaim.Parameters.Kind: %v", claim.Spec.Parameters.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to allocate devices: %v", err)
	}

	err = nascrd.Update(&nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating Gpu CRD: %v", err)
	}

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

	if nascrd.Spec.ClaimRequirements == nil {
		return nil
	}

	if _, exists := nascrd.Spec.ClaimRequirements[string(claim.UID)]; !exists {
		return nil
	}

	delete(nascrd.Spec.ClaimRequirements, string(claim.UID))

	err = nascrd.Update(&nascrd.Spec)
	if err != nil {
		return fmt.Errorf("error updating Gpu CRD: %v", err)
	}

	return nil
}

func (d driver) UnsuitableNodes(ctx context.Context, pod *v1.Pod, cas []*controller.ClaimAllocation, potentialNodes []string) error {
	perKindCas := make(map[string][]*controller.ClaimAllocation)
	for _, ca := range cas {
		kind := ca.Claim.Spec.Parameters.Kind
		perKindCas[kind] = append(perKindCas[kind], ca)
	}
	for kind, cas := range perKindCas {
		var err error
		switch kind {
		case nvcrd.GpuClaimKind:
			err = d.gpu.UnsuitableNodes(ctx, pod, cas, potentialNodes)
		}
		if err != nil {
			return fmt.Errorf("error calling UnsuitableNodes for '%v': %v", kind, err)
		}
	}
	return nil
}

func (d driver) StopAllocation(ctx context.Context, claim *corev1.ResourceClaim) error {
	return d.Deallocate(ctx, claim)
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
