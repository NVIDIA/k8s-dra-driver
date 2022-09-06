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

type migdriver struct {
	PendingClaimRequests *PerNodeClaimRequests
}

type ClaimInfo struct {
	UID  string
	Name string
}

type MigDevicePlacement struct {
	ParentUUID string
	Placement  nvcrd.MigDevicePlacement
}

type MigDevicePlacements map[string][]MigDevicePlacement

func NewMigDriver() *migdriver {
	return &migdriver{
		PendingClaimRequests: NewPerNodeClaimRequests(),
	}
}

func (m migdriver) ValidateClaimParameters(claimParams *nvcrd.MigDeviceClaimParametersSpec) error {
	return nil
}

func (m *migdriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimParams *nvcrd.MigDeviceClaimParametersSpec, class *corev1.ResourceClass, classParams *nvcrd.DeviceClassParametersSpec, selectedNode string) (OnSuccessCallback, error) {
	claimUID := string(claim.UID)

	if !m.PendingClaimRequests.Exists(claimUID, selectedNode) {
		return nil, fmt.Errorf("no allocation requests generated for claim '%v' on node '%v' yet", claim.UID, selectedNode)
	}

	request := m.PendingClaimRequests.Get(claimUID, selectedNode).Mig
	if request.Spec.GpuClaimParametersName != "" {
		for _, device := range request.Devices {
			if !m.gpuIsAllocated(crd, device.ParentUUID) {
				return nil, fmt.Errorf("dependent claim for parent GPU not yet allocated: %v", device.ParentUUID)
			}
		}
	}

	crd.Spec.ClaimRequests[claimUID] = m.PendingClaimRequests.Get(claimUID, selectedNode)
	onSuccess := func() {
		m.PendingClaimRequests.Remove(claimUID)
	}

	return onSuccess, nil
}

func (m *migdriver) Deallocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim) error {
	m.PendingClaimRequests.Remove(string(claim.UID))
	return nil
}

func (m *migdriver) UnsuitableNode(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, migcas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, potentialNode string) error {
	m.PendingClaimRequests.VisitNode(potentialNode, func(claimUID string, request nvcrd.RequestedDevices) {
		if _, exists := crd.Spec.ClaimRequests[claimUID]; exists {
			m.PendingClaimRequests.Remove(claimUID)
		} else {
			crd.Spec.ClaimRequests[claimUID] = request
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
		claimParams := ca.ClaimParameters.(*nvcrd.MigDeviceClaimParametersSpec)

		devices := []nvcrd.RequestedMigDevice{
			{
				Profile:    claimParams.Profile,
				ParentUUID: placements[ca].ParentUUID,
				Placement:  placements[ca].Placement,
			},
		}

		requestedDevices := nvcrd.RequestedDevices{
			Mig: &nvcrd.RequestedMigDevices{
				Spec:    *claimParams,
				Devices: devices,
			},
		}

		m.PendingClaimRequests.Set(claimUID, potentialNode, requestedDevices)
		crd.Spec.ClaimRequests[claimUID] = requestedDevices
	}

	return nil
}

func (m *migdriver) available(crd *nvcrd.NodeAllocationState) MigDevicePlacements {
	parents := make(map[string][]string)
	placements := make(MigDevicePlacements)

	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			if !device.Gpu.MigEnabled {
				continue
			}
			parents[device.Gpu.Model] = append(parents[device.Gpu.Model], device.Gpu.UUID)
		}
	}

	for _, device := range crd.Spec.AllocatableDevices {
		switch device.Type() {
		case nvcrd.MigDeviceType:
			var pps []MigDevicePlacement
			for _, parentUUID := range parents[device.Mig.ParentModel] {
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
	}

	for _, request := range crd.Spec.ClaimRequests {
		switch request.Type() {
		case nvcrd.MigDeviceType:
			for _, device := range request.Mig.Devices {
				p := MigDevicePlacement{
					ParentUUID: device.ParentUUID,
					Placement:  device.Placement,
				}
				placements.removeOverlapping(&p)
			}
		}
	}

	return placements
}

func (m *migdriver) allocate(crd *nvcrd.NodeAllocationState, pod *corev1.Pod, migcas []*controller.ClaimAllocation, allcas []*controller.ClaimAllocation, node string) map[*controller.ClaimAllocation]MigDevicePlacement {
	available := m.available(crd)
	gpuClaimInfo := m.gpuClaimInfo(crd, allcas)

	possiblePlacements := make(map[*controller.ClaimAllocation][]MigDevicePlacement)
	for _, ca := range migcas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.ClaimRequests[claimUID]; exists {
			device := crd.Spec.ClaimRequests[claimUID].Mig.Devices[0]
			possiblePlacements[ca] = []MigDevicePlacement{
				{
					ParentUUID: device.ParentUUID,
					Placement:  device.Placement,
				},
			}
			continue
		}

		claimParams := ca.ClaimParameters.(*nvcrd.MigDeviceClaimParametersSpec)
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

func (m *migdriver) gpuIsAllocated(crd *nvcrd.NodeAllocationState, uuid string) bool {
	for _, request := range crd.Spec.ClaimRequests {
		switch request.Type() {
		case nvcrd.GpuDeviceType:
			for _, device := range request.Gpu.Devices {
				if device.UUID == uuid {
					return true
				}
			}
		}
	}
	return false
}

func (m *migdriver) gpuClaimInfo(crd *nvcrd.NodeAllocationState, cas []*controller.ClaimAllocation) map[string]ClaimInfo {
	info := make(map[string]ClaimInfo)

	for _, ca := range cas {
		claimUID := string(ca.Claim.UID)
		if _, exists := crd.Spec.ClaimRequests[claimUID]; !exists {
			continue
		}

		request := crd.Spec.ClaimRequests[claimUID]

		switch request.Type() {
		case nvcrd.GpuDeviceType:
			for _, device := range request.Gpu.Devices {
				info[device.UUID] = ClaimInfo{
					UID:  claimUID,
					Name: ca.Claim.Name,
				}
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
