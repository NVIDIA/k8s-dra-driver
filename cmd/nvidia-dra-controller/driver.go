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
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

const (
	DriverName       = nvcrd.GroupName
	DriverVersion    = nvcrd.Version
	DriverAPIVersion = DriverName + "/" + DriverVersion
)

type driver struct {
	lock   *PerNodeMutex
	config *Config
}

var _ controller.Driver = (*driver)(nil)

func NewDriver(config *Config) *driver {
	return &driver{
		lock:   NewPerNodeMutex(),
		config: config,
	}
}

func (d driver) GetClassParameters(ctx context.Context, class *corev1.ResourceClass) (interface{}, error) {
	return nil, nil
}

func (d driver) GetClaimParameters(ctx context.Context, claim *corev1.ResourceClaim, class *corev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	if claim.Spec.Parameters == nil {
		return nil, fmt.Errorf("missing claim parameters")
	}
	if claim.Spec.Parameters.APIVersion != DriverAPIVersion {
		return nil, fmt.Errorf("incorrect API group and version: %v", claim.Spec.Parameters.APIVersion)
	}
	if claim.Spec.Parameters.Kind != nvcrd.GpuParameterSetKind {
		return nil, fmt.Errorf("incorrect Kind: %v", claim.Spec.Parameters.Kind)
	}
	gcps, err := d.config.clientset.nvidia.DraV1().GpuParameterSets(claim.Namespace).Get(ctx, claim.Spec.Parameters.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting GpuParameterSet for '%v' in namespace '%v': %v", claim.Spec.Parameters.Name, claim.Namespace, err)
	}
	if gcps.Spec.Count <= 0 {
		return nil, fmt.Errorf("expected Count > 0")
	}
	return gcps.Spec, nil
}

func (d driver) Allocate(ctx context.Context, claim *corev1.ResourceClaim, claimParameters interface{}, class *corev1.ResourceClass, classParameters interface{}, selectedNode string) (*corev1.AllocationResult, error) {
	if selectedNode == "" {
		return nil, fmt.Errorf("TODO: immediate allocations not yet supported")
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nvcrd.GpuConfig{
		Name:      selectedNode,
		Namespace: d.config.namespace,
	}

	gpucrd := nvcrd.NewGpu(crdconfig, d.config.clientset.nvidia)
	err := gpucrd.Get()
	if err != nil {
		return nil, fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if gpucrd.Spec.ClaimRequirements == nil {
		gpucrd.Spec.ClaimRequirements = make(map[string]nvcrd.GpuParameterSetSpec)
	}

	if _, exists := gpucrd.Spec.ClaimRequirements[string(claim.UID)]; exists {
		return buildAllocationResult(selectedNode), nil
	}

	if gpucrd.Status != nvcrd.GpuStatusReady {
		return nil, fmt.Errorf("Gpu CRD status: %v", gpucrd.Status)
	}

	allocated := 0
	for _, parameters := range gpucrd.Spec.ClaimRequirements {
		allocated += parameters.Count
	}

	available := gpucrd.Spec.Allocatable - allocated
	requested := claimParameters.(nvcrd.GpuParameterSetSpec).Count
	if requested > available {
		return nil, fmt.Errorf("not enough devices to satisfy allocation: (available: %v, requested: %v)", available, requested)
	}

	gpucrd.Spec.ClaimRequirements[string(claim.UID)] = claimParameters.(nvcrd.GpuParameterSetSpec)

	err = gpucrd.Update(&gpucrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating Gpu CRD: %v", err)
	}

	return buildAllocationResult(selectedNode), nil
}

func (d driver) Deallocate(ctx context.Context, claim *corev1.ResourceClaim) error {
	selectedNode := getSelectedNode(claim)
	if selectedNode == "" {
		return nil
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &nvcrd.GpuConfig{
		Name:      selectedNode,
		Namespace: d.config.namespace,
	}

	gpucrd := nvcrd.NewGpu(crdconfig, d.config.clientset.nvidia)
	err := gpucrd.Get()
	if err != nil {
		return fmt.Errorf("error retrieving node specific Gpu CRD: %v", err)
	}

	if gpucrd.Spec.ClaimRequirements == nil {
		return nil
	}

	if _, exists := gpucrd.Spec.ClaimRequirements[string(claim.UID)]; !exists {
		return nil
	}

	delete(gpucrd.Spec.ClaimRequirements, string(claim.UID))

	err = gpucrd.Update(&gpucrd.Spec)
	if err != nil {
		return fmt.Errorf("error updating Gpu CRD: %v", err)
	}

	return nil
}

func (d driver) StopAllocation(ctx context.Context, claim *corev1.ResourceClaim) error {
	return nil
}

func (d driver) UnsuitableNodes(ctx context.Context, pod *v1.Pod, claims []*controller.ClaimAllocation, potentialNodes []string) error {
	return nil
}

func buildAllocationResult(selectedNode string) *corev1.AllocationResult {
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
		SharedResource:   true,
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
