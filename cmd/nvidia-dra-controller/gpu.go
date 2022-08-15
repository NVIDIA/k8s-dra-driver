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

	"github.com/NVIDIA/k8s-dra-driver/pkg/controller"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

type gpudriver struct {
}

func NewGpuDriver() *gpudriver {
	return &gpudriver{}
}

func (g gpudriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimSpec *nvcrd.GpuClaimSpec, class *corev1.ResourceClass, classSpec *nvcrd.DeviceClassSpec, selectedNode string) error {
	requested := claimSpec.Count
	if requested < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", requested)
	}

	allocatable := 0
	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			allocatable += device.Gpu.Count
		}
	}

	allocated := 0
	for _, requirements := range crd.Spec.ClaimRequirements {
		switch requirements.Type() {
		case nvcrd.GpuDeviceType:
			allocated += requirements.Gpu.Count
		}
	}

	available := allocatable - allocated
	if requested > available {
		return fmt.Errorf("not enough devices to satisfy allocation: (available: %v, requested: %v)", available, requested)
	}

	crd.Spec.ClaimRequirements[string(claim.UID)] = nvcrd.DeviceRequirements{
		Gpu: claimSpec,
	}

	return nil
}

func (g gpudriver) UnsuitableNodes(ctx context.Context, pod *v1.Pod, claims []*controller.ClaimAllocation, potentialNodes []string) error {
	return nil
}
