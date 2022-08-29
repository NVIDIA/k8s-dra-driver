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

	"github.com/NVIDIA/k8s-dra-driver/pkg/controller"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

type gpudriver struct{}

func NewGpuDriver() *gpudriver {
	return &gpudriver{}
}

func (g gpudriver) ValidateClaimSpec(claimSpec *nvcrd.GpuClaimSpec) error {
	if claimSpec.Count < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", claimSpec.Count)
	}
	return nil
}

func (g gpudriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimSpec *nvcrd.GpuClaimSpec, class *corev1.ResourceClass, classSpec *nvcrd.DeviceClassSpec, selectedNode string) error {
	allocated := g.allocate(crd, claimSpec)
	if claimSpec.Count > len(allocated) {
		return fmt.Errorf("not enough devices to satisfy allocation: (available: %v, requested: %v)", len(allocated), claimSpec.Count)
	}

	var devices []nvcrd.RequestedGpu
	for _, gpu := range allocated {
		device := nvcrd.RequestedGpu{
			UUID: gpu,
		}
		devices = append(devices, device)
	}

	crd.Spec.ClaimRequests[string(claim.UID)] = nvcrd.RequestedDevices{
		Gpu: &nvcrd.RequestedGpus{
			Spec:    *claimSpec,
			Devices: devices,
		},
	}

	return nil
}

func (m gpudriver) Deallocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim) error {
	return nil
}

func (g gpudriver) UnsuitableNode(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, cas []*controller.ClaimAllocation, potentialNode string) error {
	totalRequested := 0
	var claimSpecs []*nvcrd.GpuClaimSpec
	for _, ca := range cas {
		if ca.Claim.Spec.Parameters.Kind != nvcrd.GpuClaimKind {
			continue
		}
		claimSpec := ca.ClaimParameters.(*nvcrd.GpuClaimSpec)
		claimSpecs = append(claimSpecs, claimSpec)
		totalRequested += claimSpec.Count
	}
	if totalRequested != len(g.allocate(crd, claimSpecs...)) {
		for _, ca := range cas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
	}
	return nil
}

func (g gpudriver) allocate(crd *nvcrd.NodeAllocationState, claimSpecs ...*nvcrd.GpuClaimSpec) []string {
	available := make(map[string]*nvcrd.AllocatableGpu)

	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			available[device.Gpu.UUID] = device.Gpu
		}
	}

	for _, request := range crd.Spec.ClaimRequests {
		switch request.Type() {
		case nvcrd.GpuDeviceType:
			for _, device := range request.Gpu.Devices {
				delete(available, device.UUID)
			}
		}
	}

	var allocated []string
	for _, claimSpec := range claimSpecs {
		for i := 0; i < claimSpec.Count; i++ {
			for _, device := range available {
				if device.MigEnabled == claimSpec.MigEnabled {
					allocated = append(allocated, device.UUID)
					delete(available, device.UUID)
					break
				}
			}
		}
	}

	return allocated
}
