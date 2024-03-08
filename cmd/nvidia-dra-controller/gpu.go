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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/dynamic-resource-allocation/controller"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"
)

type gpudriver struct {
	PendingAllocatedClaims *PerNodeAllocatedClaims
}

func NewGpuDriver() *gpudriver {
	return &gpudriver{
		PendingAllocatedClaims: NewPerNodeAllocatedClaims(),
	}
}

func (g *gpudriver) ValidateClaimParameters(claimParams *gpucrd.GpuClaimParametersSpec) error {
	if *claimParams.Count < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", claimParams.Count)
	}
	return nil
}

func (g *gpudriver) Allocate(crd *nascrd.NodeAllocationState, claim *resourcev1.ResourceClaim, claimParams *gpucrd.GpuClaimParametersSpec, class *resourcev1.ResourceClass, classParams *gpucrd.DeviceClassParametersSpec, selectedNode string) (OnSuccessCallback, error) {
	claimUID := string(claim.UID)

	if !g.PendingAllocatedClaims.Exists(claimUID, selectedNode) {
		return nil, fmt.Errorf("no allocations generated for claim '%v' on node '%v' yet", claim.UID, selectedNode)
	}

	crd.Spec.AllocatedClaims[claimUID] = g.PendingAllocatedClaims.Get(claimUID, selectedNode)
	onSuccess := func() {
		g.PendingAllocatedClaims.Remove(claimUID)
	}

	return onSuccess, nil
}

func (g *gpudriver) Deallocate(crd *nascrd.NodeAllocationState, claim *resourcev1.ResourceClaim) error {
	g.PendingAllocatedClaims.Remove(string(claim.UID))
	return nil
}

func (g *gpudriver) UnsuitableNode(crd *nascrd.NodeAllocationState, pod *corev1.Pod, gpucas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, potentialNode string) error {
	g.PendingAllocatedClaims.VisitNode(potentialNode, func(claimUID string, allocated nascrd.AllocatedDevices) {
		if _, exists := crd.Spec.AllocatedClaims[claimUID]; exists {
			g.PendingAllocatedClaims.Remove(claimUID)
		} else {
			crd.Spec.AllocatedClaims[claimUID] = allocated
		}
	})

	allocated := g.allocate(crd, pod, gpucas, allcas, potentialNode)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		claimParams, ok := ca.ClaimParameters.(*gpucrd.GpuClaimParametersSpec)
		if !ok {
			return fmt.Errorf("invalid claim parameters type: %T", ca.ClaimParameters)
		}

		if *claimParams.Count != len(allocated[claimUID]) {
			for _, ca := range allcas {
				ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
			}
			return nil
		}

		var devices []nascrd.AllocatedGpu
		for _, gpu := range allocated[claimUID] {
			device := nascrd.AllocatedGpu{
				UUID: gpu,
			}
			devices = append(devices, device)
		}

		AllocatedDevices := nascrd.AllocatedDevices{
			Gpu: &nascrd.AllocatedGpus{
				Devices: devices,
				Sharing: claimParams.Sharing.DeepCopy(),
			},
		}

		g.PendingAllocatedClaims.Set(claimUID, potentialNode, AllocatedDevices)
		crd.Spec.AllocatedClaims[claimUID] = AllocatedDevices
	}

	return nil
}

func (g *gpudriver) allocate(crd *nascrd.NodeAllocationState, pod *corev1.Pod, gpucas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, node string) map[string][]string {
	available := make(map[string]*nascrd.AllocatableGpu)

	for _, device := range crd.Spec.AllocatableDevices {
		if device.Type() != types.GpuDeviceType {
			continue
		}
		available[device.Gpu.UUID] = device.Gpu
	}

	for _, allocated := range crd.Spec.AllocatedClaims {
		switch allocated.Type() {
		case types.GpuDeviceType:
			for _, device := range allocated.Gpu.Devices {
				delete(available, device.UUID)
			}
		case types.MigDeviceType:
			for _, device := range allocated.Mig.Devices {
				delete(available, device.ParentUUID)
			}
		}
	}

	allocated := make(map[string][]string)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.AllocatedClaims[claimUID]; exists {
			devices := crd.Spec.AllocatedClaims[claimUID].Gpu.Devices
			for _, device := range devices {
				allocated[claimUID] = append(allocated[claimUID], device.UUID)
			}
			continue
		}

		//nolint:forcetypeassert  // TODO: What is the correct behaviour if the type assertion fails?
		claimParams := ca.ClaimParameters.(*gpucrd.GpuClaimParametersSpec)
		var devices []string
		for i := 0; i < *claimParams.Count; i++ {
			for _, device := range available {
				if selectorMatchesGpu(claimParams.Selector, device) {
					devices = append(devices, device.UUID)
					delete(available, device.UUID)
					break
				}
			}
		}
		allocated[claimUID] = devices
	}

	return allocated
}

func selectorMatchesGpu(selector *gpucrd.GpuSelector, gpu *nascrd.AllocatableGpu) bool {
	checkedMigEnabled := false
	if selector == nil {
		return !gpu.MigEnabled
	}
	matches := selector.Matches(func(p *gpucrd.GpuSelectorProperties) bool {
		if p.Index != nil {
			return p.Index.Matches(gpu.Index)
		}
		if p.UUID != nil {
			return p.UUID.Matches(gpu.UUID)
		}
		if p.MigEnabled != nil {
			checkedMigEnabled = true
			return p.MigEnabled.Matches(gpu.MigEnabled)
		}
		if p.Memory != nil {
			quantity := resource.NewQuantity(int64(gpu.MemoryBytes), resource.BinarySI)
			return p.Memory.Matches(quantity)
		}
		if p.ProductName != nil {
			return p.ProductName.Matches(gpu.ProductName)
		}
		if p.Brand != nil {
			return p.Brand.Matches(gpu.Brand)
		}
		if p.Architecture != nil {
			return p.Architecture.Matches(gpu.Architecture)
		}
		if p.CUDAComputeCapability != nil {
			return p.CUDAComputeCapability.Matches(gpu.CUDAComputeCapability)
		}
		return false
	})
	if matches && !checkedMigEnabled {
		return !gpu.MigEnabled
	}
	return matches
}
