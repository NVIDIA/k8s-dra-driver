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
	"k8s.io/dynamic-resource-allocation/controller"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"
)

type migdriver struct {
	PendingAllocatedClaims *PerNodeAllocatedClaims
}

type ClaimInfo struct {
	UID  string
	Name string
}

type MigDevicePlacement struct {
	ParentUUID string
	Placement  nascrd.MigDevicePlacement
}

type MigDevicePlacements map[string][]MigDevicePlacement

func NewMigDriver() *migdriver {
	return &migdriver{
		PendingAllocatedClaims: NewPerNodeAllocatedClaims(),
	}
}

func (m migdriver) ValidateClaimParameters(claimParams *gpucrd.MigDeviceClaimParametersSpec) error {
	return nil
}

func (m *migdriver) Allocate(crd *nascrd.NodeAllocationState, claim *resourcev1.ResourceClaim, claimParams *gpucrd.MigDeviceClaimParametersSpec, class *resourcev1.ResourceClass, classParams *gpucrd.DeviceClassParametersSpec, selectedNode string) (OnSuccessCallback, error) {
	claimUID := string(claim.UID)

	if !m.PendingAllocatedClaims.Exists(claimUID, selectedNode) {
		return nil, fmt.Errorf("no allocations generated for claim '%v' on node '%v' yet", claim.UID, selectedNode)
	}

	crd.Spec.AllocatedClaims[claimUID] = m.PendingAllocatedClaims.Get(claimUID, selectedNode)
	onSuccess := func() {
		m.PendingAllocatedClaims.Remove(claimUID)
	}

	return onSuccess, nil
}

func (m *migdriver) Deallocate(crd *nascrd.NodeAllocationState, claim *resourcev1.ResourceClaim) error {
	m.PendingAllocatedClaims.Remove(string(claim.UID))
	return nil
}

func (m *migdriver) UnsuitableNode(crd *nascrd.NodeAllocationState, pod *corev1.Pod, migcas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, potentialNode string) error {
	m.PendingAllocatedClaims.VisitNode(potentialNode, func(claimUID string, allocated nascrd.AllocatedDevices) {
		if _, exists := crd.Spec.AllocatedClaims[claimUID]; exists {
			m.PendingAllocatedClaims.Remove(claimUID)
		} else {
			crd.Spec.AllocatedClaims[claimUID] = allocated
		}
	})

	placements := m.allocate(crd, pod, migcas, allcas, potentialNode)
	if len(placements) != len(migcas) {
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}

	for _, ca := range migcas {
		claimUID := string(ca.Claim.UID)
		claimParams, ok := ca.ClaimParameters.(*gpucrd.MigDeviceClaimParametersSpec)
		if !ok {
			return fmt.Errorf("invalid claim parameters type: %T", ca.ClaimParameters)
		}

		devices := []nascrd.AllocatedMigDevice{
			{
				Profile:    claimParams.Profile,
				ParentUUID: placements[ca].ParentUUID,
				Placement:  placements[ca].Placement,
			},
		}

		AllocatedDevices := nascrd.AllocatedDevices{
			Mig: &nascrd.AllocatedMigDevices{
				Devices: devices,
				Sharing: claimParams.Sharing.DeepCopy(),
			},
		}

		m.PendingAllocatedClaims.Set(claimUID, potentialNode, AllocatedDevices)
		crd.Spec.AllocatedClaims[claimUID] = AllocatedDevices
	}

	return nil
}

func (m *migdriver) available(crd *nascrd.NodeAllocationState) MigDevicePlacements {
	parents := make(map[string][]string)
	placements := make(MigDevicePlacements)

	for _, device := range crd.Spec.AllocatableDevices {
		if device.Type() != types.GpuDeviceType {
			continue
		}
		if !device.Gpu.MigEnabled {
			continue
		}
		parents[device.Gpu.ProductName] = append(parents[device.Gpu.ProductName], device.Gpu.UUID)
	}

	for _, device := range crd.Spec.AllocatableDevices {
		if device.Type() != types.MigDeviceType {
			continue
		}
		var pps []MigDevicePlacement
		for _, parentUUID := range parents[device.Mig.ParentProductName] {
			var mps []MigDevicePlacement
			for _, p := range device.Mig.Placements {
				mp := MigDevicePlacement{
					ParentUUID: parentUUID,
					Placement:  p,
				}
				mps = append(mps, mp)
			}
			pps = append(pps, mps...)
		}
		placements[device.Mig.Profile] = pps
	}

	for _, allocated := range crd.Spec.AllocatedClaims {
		if allocated.Type() != types.MigDeviceType {
			continue
		}
		for _, device := range allocated.Mig.Devices {
			p := MigDevicePlacement{
				ParentUUID: device.ParentUUID,
				Placement:  device.Placement,
			}
			placements.removeOverlapping(&p)
		}
	}

	return placements
}

func (m *migdriver) allocate(crd *nascrd.NodeAllocationState, pod *corev1.Pod, migcas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, node string) map[*controller.ClaimAllocation]MigDevicePlacement {
	available := m.available(crd)
	gpuClaimInfo := m.gpuClaimInfo(crd, allcas)

	possiblePlacements := make(map[*controller.ClaimAllocation][]MigDevicePlacement)
	for _, ca := range migcas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.AllocatedClaims[claimUID]; exists {
			device := crd.Spec.AllocatedClaims[claimUID].Mig.Devices[0]
			possiblePlacements[ca] = []MigDevicePlacement{
				{
					ParentUUID: device.ParentUUID,
					Placement:  device.Placement,
				},
			}
			continue
		}

		//nolint:forcetypeassert  // TODO: What is the correct behaviour if the type assertion fails?
		claimParams := ca.ClaimParameters.(*gpucrd.MigDeviceClaimParametersSpec)
		if _, exists := available[claimParams.Profile]; !exists {
			possiblePlacements[ca] = nil
			continue
		}

		var filtered []MigDevicePlacement
		for _, p := range available[claimParams.Profile] {
			if claimInfo, exists := gpuClaimInfo[p.ParentUUID]; exists {
				if claimInfo.Name == pod.Name+"-"+claimParams.GpuClaimParametersName {
					filtered = append(filtered, p)
				} else if claimInfo.Name == claimParams.GpuClaimParametersName {
					filtered = append(filtered, p)
				}
				continue
			}
			if claimParams.GpuClaimParametersName == "" {
				filtered = append(filtered, p)
			}
		}
		possiblePlacements[ca] = filtered
	}

	if len(possiblePlacements) == 0 {
		return nil
	}

	for _, placements := range possiblePlacements {
		if len(placements) == 0 {
			return nil
		}
	}

	if len(possiblePlacements) == 1 {
		for ca, placements := range possiblePlacements {
			return map[*controller.ClaimAllocation]MigDevicePlacement{
				ca: placements[0],
			}
		}
	}

	var iterate func(i int, combo map[*controller.ClaimAllocation]MigDevicePlacement) map[*controller.ClaimAllocation]MigDevicePlacement
	iterate = func(i int, combo map[*controller.ClaimAllocation]MigDevicePlacement) map[*controller.ClaimAllocation]MigDevicePlacement {
		if len(combo) == len(possiblePlacements) {
			var placements []MigDevicePlacement
			for _, placement := range combo {
				placements = append(placements, placement)
			}
			for i := range placements {
				if !placements[i].NoOverlap(placements[i+1:]...) {
					return nil
				}
			}
			return combo
		}

		for _, newplacement := range possiblePlacements[migcas[i]] {
			newcombo := make(map[*controller.ClaimAllocation]MigDevicePlacement)
			for claimParams, placement := range combo {
				newcombo[claimParams] = placement
			}
			newcombo[migcas[i]] = newplacement

			result := iterate(i+1, newcombo)
			if len(result) != 0 {
				return result
			}
		}

		return nil
	}

	return iterate(0, map[*controller.ClaimAllocation]MigDevicePlacement{})
}

func (m *migdriver) gpuClaimInfo(crd *nascrd.NodeAllocationState, cas []*controller.ClaimAllocation) map[string]ClaimInfo {
	info := make(map[string]ClaimInfo)

	for _, ca := range cas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.AllocatedClaims[claimUID]; !exists {
			continue
		}

		allocated := crd.Spec.AllocatedClaims[claimUID]

		if allocated.Type() != types.GpuDeviceType {
			continue
		}
		for _, device := range allocated.Gpu.Devices {
			info[device.UUID] = ClaimInfo{
				UID:  claimUID,
				Name: ca.Claim.Name,
			}
		}
	}

	return info
}

func (m *MigDevicePlacement) NoOverlap(m2s ...MigDevicePlacement) bool {
	mFirst := m.Placement.Start
	mLast := m.Placement.Start + m.Placement.Size - 1

	for _, m2 := range m2s {
		m2First := m2.Placement.Start
		m2Last := (m2.Placement.Start + m2.Placement.Size - 1)

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

func (mps MigDevicePlacements) removeOverlapping(p *MigDevicePlacement) {
	for profile := range mps {
		var newps []MigDevicePlacement
		for _, placement := range mps[profile] {
			if !placement.NoOverlap(*p) {
				continue
			}
			newps = append(newps, placement)
		}
		mps[profile] = newps
	}
}
