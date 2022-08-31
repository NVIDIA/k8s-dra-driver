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

type gpudriver struct {
	PendingClaimRequests *PerNodeClaimRequests
}

func NewGpuDriver() *gpudriver {
	return &gpudriver{
		PendingClaimRequests: NewPerNodeClaimRequests(),
	}
}

func (g *gpudriver) ValidateClaimSpec(claimSpec *nvcrd.GpuClaimSpec) error {
	if claimSpec.Count < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", claimSpec.Count)
	}
	return nil
}

func (g *gpudriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimSpec *nvcrd.GpuClaimSpec, class *corev1.ResourceClass, classSpec *nvcrd.DeviceClassSpec, selectedNode string) (OnSuccessCallback, error) {
	claimUID := string(claim.UID)

	if !g.PendingClaimRequests.Exists(claimUID, selectedNode) {
		return nil, fmt.Errorf("no allocation requests generated for claim '%v' on node '%v' yet", claim.UID, selectedNode)
	}

	crd.Spec.ClaimRequests[claimUID] = g.PendingClaimRequests.Get(claimUID, selectedNode)
	onSuccess := func() {
		g.PendingClaimRequests.Remove(claimUID)
	}

	return onSuccess, nil
}

func (g *gpudriver) Deallocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim) error {
	g.PendingClaimRequests.Remove(string(claim.UID))
	return nil
}

func (g *gpudriver) UnsuitableNode(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, gpucas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, potentialNode string) error {
	g.PendingClaimRequests.VisitNode(potentialNode, func(claimUID string, request nvcrd.RequestedDevices) {
		if _, exists := crd.Spec.ClaimRequests[claimUID]; exists {
			g.PendingClaimRequests.Remove(claimUID)
		} else {
			crd.Spec.ClaimRequests[claimUID] = request
		}
	})

	allocated := g.allocate(crd, pod, gpucas, allcas, potentialNode)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		claimSpec := ca.ClaimParameters.(*nvcrd.GpuClaimSpec)

		if claimSpec.Count != len(allocated[claimUID]) {
			for _, ca := range allcas {
				ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
			}
			return nil
		}

		var devices []nvcrd.RequestedGpu
		for _, gpu := range allocated[claimUID] {
			device := nvcrd.RequestedGpu{
				UUID: gpu,
			}
			devices = append(devices, device)
		}

		requestedDevices := nvcrd.RequestedDevices{
			Gpu: &nvcrd.RequestedGpus{
				Spec:    *claimSpec,
				Devices: devices,
			},
		}

		g.PendingClaimRequests.Set(claimUID, potentialNode, requestedDevices)
		crd.Spec.ClaimRequests[claimUID] = requestedDevices
	}

	return nil
}

func (g *gpudriver) allocate(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, gpucas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, node string) map[string][]string {
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
		case nvcrd.MigDeviceType:
			for _, device := range request.Mig.Devices {
				delete(available, device.ParentUUID)
			}
		}
	}

	allocated := make(map[string][]string)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.ClaimRequests[claimUID]; exists {
			devices := crd.Spec.ClaimRequests[claimUID].Gpu.Devices
			for _, device := range devices {
				allocated[claimUID] = append(allocated[claimUID], device.UUID)
			}
			continue
		}

		claimSpec := ca.ClaimParameters.(*nvcrd.GpuClaimSpec)
		var devices []string
		for i := 0; i < claimSpec.Count; i++ {
			for _, device := range available {
				if device.MigEnabled == claimSpec.MigEnabled {
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
