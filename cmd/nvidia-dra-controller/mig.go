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

type migdriver struct{}

type MigDevicePlacement struct {
	ParentUUID string
	Start      int
	Size       int
}

type MigDevicePlacements map[string][]MigDevicePlacement

func NewMigDriver() *migdriver {
	return &migdriver{}
}

func (m migdriver) ValidateClaimSpec(claimSpec *nvcrd.MigDeviceClaimSpec) error {
	return nil
}

func (m *migdriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimSpec *nvcrd.MigDeviceClaimSpec, class *corev1.ResourceClass, classSpec *nvcrd.DeviceClassSpec, selectedNode string) error {
	claimSpecs := []*nvcrd.MigDeviceClaimSpec{claimSpec}

	for rclaim, r := range crd.Spec.ClaimRequirements {
		if r.Type() != nvcrd.MigDeviceType {
			continue
		}
		if _, exists := crd.Spec.ClaimAllocations[rclaim]; exists {
			continue
		}
		claimSpecs = append(claimSpecs, r.Mig)
	}

	available := m.available(crd)
	if !m.allocatable(available, claimSpecs...) {
		return fmt.Errorf("no %v MIG devices available", claimSpec.Profile)
	}

	crd.Spec.ClaimRequirements[string(claim.UID)] = nvcrd.DeviceRequirements{
		Mig: claimSpec,
	}

	return nil
}

func (m *migdriver) Deallocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim) error {
	return nil
}

func (m *migdriver) UnsuitableNode(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, cas []*controller.ClaimAllocation, potentialNode string) error {
	claimSpecs := []*nvcrd.MigDeviceClaimSpec{}

	for _, ca := range cas {
		if ca.Claim.Spec.Parameters.Kind != nvcrd.MigDeviceClaimKind {
			continue
		}
		claimSpecs = append(claimSpecs, ca.ClaimParameters.(*nvcrd.MigDeviceClaimSpec))
	}

	for rclaim, r := range crd.Spec.ClaimRequirements {
		if r.Type() != nvcrd.MigDeviceType {
			continue
		}
		if _, exists := crd.Spec.ClaimAllocations[rclaim]; exists {
			continue
		}
		claimSpecs = append(claimSpecs, r.Mig)
	}

	available := m.available(crd)
	if !m.allocatable(available, claimSpecs...) {
		for _, ca := range cas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
	}

	return nil
}

func (m *migdriver) available(crd *nvcrd.NodeAllocationState) MigDevicePlacements {
	parents := make(map[string][]string)
	placements := make(MigDevicePlacements)

	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			for _, uuid := range device.Gpu.MigEnabled {
				parents[device.Gpu.Name] = append(parents[device.Gpu.Name], uuid)
			}
		}
	}

	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.MigDeviceType:
			for _, parentUUID := range parents[device.Mig.ParentName] {
				var mps []MigDevicePlacement
				for _, p := range device.Mig.Placements {
					mp := MigDevicePlacement{
						ParentUUID: parentUUID,
						Start:      int(p),
						Size:       device.Mig.Slices,
					}
					mps = append(mps, mp)
				}
				placements[device.Mig.Profile] = mps
			}
		}
	}

	for _, devices := range crd.Spec.ClaimAllocations {
		for _, device := range devices {
			switch device.Type() {
			case nvcrd.MigDeviceType:
				placements.removePlacements(device.Mig)
			}
		}
	}

	return placements
}

func (m *migdriver) allocatable(mps MigDevicePlacements, claimSpecs ...*nvcrd.MigDeviceClaimSpec) bool {
	var allPlacements [][]MigDevicePlacement
	for _, spec := range claimSpecs {
		for profile, placements := range mps {
			if spec.Profile == profile {
				allPlacements = append(allPlacements, placements)
			}
		}
	}

	if len(allPlacements) == 0 {
		return false
	}

	for _, placements := range allPlacements {
		if len(placements) == 0 {
			return false
		}
	}

	if len(allPlacements) == 1 {
		return true
	}

	var iterate func(int, []MigDevicePlacement) bool
	iterate = func(i int, combos []MigDevicePlacement) bool {
		if i == len(allPlacements) {
			for j := range combos {
				if combos[j].NoOverlap(combos[j+1:]...) {
					return true
				}
			}
			return false
		}
		for j := range allPlacements[i] {
			allocatable := iterate(i+1, append(combos, allPlacements[i][j]))
			if allocatable {
				return true
			}
		}
		return false
	}

	return iterate(0, []MigDevicePlacement{})
}

func (m *MigDevicePlacement) NoOverlap(m2s ...MigDevicePlacement) bool {
	mFirst := m.Start
	mLast := m.Start + m.Size - 1

	for _, m2 := range m2s {
		m2First := m2.Start
		m2Last := (m2.Start + m2.Size - 1)

		if m.ParentUUID != m2.ParentUUID {
			continue
		}
		if mLast < m2First {
			continue
		}
		if mFirst > m2Last {
			continue
		}

		return false
	}

	return true
}

func (mps MigDevicePlacements) removePlacements(m *nvcrd.AllocatedMigDevice) {
	allocatedPlacement := MigDevicePlacement{
		ParentUUID: m.ParentUUID,
		Start:      int(m.Placement),
		Size:       m.Slices,
	}

	for profile := range mps {
		var newps []MigDevicePlacement
		for _, placement := range mps[profile] {
			if !placement.NoOverlap(allocatedPlacement) {
				continue
			}
			newps = append(newps, placement)
		}
		mps[profile] = newps
	}
}
