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
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/api/resource/gpu/v1alpha1/api"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type AllocatedDevices map[string]AllocatedDeviceInfo
type ClaimAllocations map[string]AllocatedDevices

type GpuInfo struct {
	uuid       string
	model      string
	minor      int
	migEnabled bool
}

func (g GpuInfo) CDIDevice() string {
	return fmt.Sprintf("%s=gpu%d", cdiKind, g.minor)
}

type MigDeviceInfo struct {
	uuid    string
	parent  *GpuInfo
	profile *MigProfile
	giInfo  *nvml.GpuInstanceInfo
	ciInfo  *nvml.ComputeInstanceInfo
}

func (m MigDeviceInfo) CDIDevice() string {
	return fmt.Sprintf("%s=mig-gpu%d-gi%d-ci%d", cdiKind, m.parent.minor, m.giInfo.Id, m.ciInfo.Id)
}

type AllocatedDeviceInfo struct {
	gpu *GpuInfo
	mig *MigDeviceInfo
}

func (i AllocatedDeviceInfo) Type() string {
	if i.gpu != nil {
		return nvcrd.GpuDeviceType
	}
	if i.mig != nil {
		return nvcrd.MigDeviceType
	}
	return nvcrd.UnknownDeviceType
}

func (i AllocatedDeviceInfo) CDIDevice() string {
	switch i.Type() {
	case nvcrd.GpuDeviceType:
		return i.gpu.CDIDevice()
	case nvcrd.MigDeviceType:
		return i.mig.CDIDevice()
	}
	return ""
}

type MigProfileInfo struct {
	profile    *MigProfile
	placements []*MigDevicePlacement
}

type MigDevicePlacement struct {
	nvml.GpuInstancePlacement
	blockedBy int
}

type AllocatableDeviceInfo struct {
	*GpuInfo
	migProfiles map[string]*MigProfileInfo
}

type DeviceState struct {
	sync.Mutex
	cdi         cdiapi.Registry
	allocatable AllocatableDevices
	allocated   ClaimAllocations
}

func NewDeviceState(config *Config, nascrd *nvcrd.NodeAllocationState) (*DeviceState, error) {
	allocatable, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(*config.flags.cdiRoot),
	)

	err = cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	state := &DeviceState{
		cdi:         cdi,
		allocatable: allocatable,
		allocated:   make(ClaimAllocations),
	}

	err = state.syncAllocatedDevicesFromCRDSpec(&nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from CRD: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Allocate(claimUid string, request nvcrd.RequestedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if len(s.allocated[claimUid]) != 0 {
		return s.getAllocatedAsCDIDevices(claimUid), nil
	}

	s.allocated[claimUid] = make(AllocatedDevices)

	var err error
	switch request.Type() {
	case nvcrd.GpuDeviceType:
		err = s.allocateGpus(claimUid, request.Gpu.Devices)
	case nvcrd.MigDeviceType:
		err = s.allocateMigDevices(claimUid, request.Mig.Devices)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	return s.getAllocatedAsCDIDevices(claimUid), nil
}

func (s *DeviceState) Free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] == nil {
		return nil
	}

	for _, device := range s.allocated[claimUid] {
		var err error
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			err = s.freeGpu(device.gpu)
		case nvcrd.MigDeviceType:
			err = s.freeMigDevice(device.mig)
		}
		if err != nil {
			return fmt.Errorf("free failed: %v", err)
		}
	}

	delete(s.allocated, claimUid)
	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nvcrd.NodeAllocationStateSpec) *nvcrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToCRDSpec(outspec)
	s.syncAllocatedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) getAllocatedAsCDIDevices(claimUid string) []string {
	var devs []string
	for _, device := range s.allocated[claimUid] {
		devs = append(devs, s.cdi.DeviceDB().GetDevice(device.CDIDevice()).GetQualifiedName())
	}
	return devs
}

func (s *DeviceState) allocateGpus(claimUid string, devices []nvcrd.RequestedGpu) error {
	for _, device := range devices {
		if _, exists := s.allocatable[device.UUID]; !exists {
			return fmt.Errorf("requested GPU does not exist: %v", device.UUID)
		}

		allocated := AllocatedDevices{
			device.UUID: AllocatedDeviceInfo{
				gpu: s.allocatable[device.UUID].GpuInfo,
			},
		}

		s.allocated[claimUid][device.UUID] = allocated[device.UUID]
	}

	return nil
}

func (s *DeviceState) allocateMigDevices(claimUid string, devices []nvcrd.RequestedMigDevice) error {
	for _, device := range devices {
		if _, exists := s.allocatable[device.ParentUUID]; !exists {
			return fmt.Errorf("requested GPU does not exist: %v", device.ParentUUID)
		}

		parent := s.allocatable[device.ParentUUID]

		if !parent.migEnabled {
			return fmt.Errorf("cannot allocate a GPU with MIG mode disabled: %v", device.ParentUUID)
		}

		if _, exists := parent.migProfiles[device.Profile]; !exists {
			return fmt.Errorf("MIG profile %v does not exist on GPU: %v", device.Profile, device.ParentUUID)
		}

		placement := nvml.GpuInstancePlacement{
			Start: uint32(device.Placement.Start),
			Size:  uint32(device.Placement.Size),
		}

		migInfo, err := createMigDevice(parent.GpuInfo, parent.migProfiles[device.Profile].profile, &placement)
		if err != nil {
			return fmt.Errorf("error creating MIG device: %v", err)
		}

		allocated := AllocatedDevices{
			migInfo.uuid: AllocatedDeviceInfo{
				mig: migInfo,
			},
		}

		s.allocated[claimUid][migInfo.uuid] = allocated[migInfo.uuid]
	}

	return nil
}

func (s *DeviceState) freeGpu(gpu *GpuInfo) error {
	return nil
}

func (s *DeviceState) freeMigDevice(mig *MigDeviceInfo) error {
	return deleteMigDevice(mig)
}

func (s *DeviceState) syncAllocatableDevicesToCRDSpec(spec *nvcrd.NodeAllocationStateSpec) {
	gpus := make(map[string]nvcrd.AllocatableDevices)
	migs := make(map[string]map[string]nvcrd.AllocatableDevices)
	for _, device := range s.allocatable {
		gpus[device.uuid] = nvcrd.AllocatableDevices{
			Gpu: &nvcrd.AllocatableGpu{
				Model:      device.model,
				UUID:       device.uuid,
				MigEnabled: device.migEnabled,
			},
		}

		if !device.migEnabled {
			continue
		}

		for _, mig := range device.migProfiles {
			if _, exists := migs[device.model]; !exists {
				migs[device.model] = make(map[string]nvcrd.AllocatableDevices)
			}

			if _, exists := migs[device.model][mig.profile.String()]; exists {
				continue
			}

			var placements []nvcrd.MigDevicePlacement
			for _, placement := range mig.placements {
				p := nvcrd.MigDevicePlacement{
					Start: int(placement.Start),
					Size:  int(placement.Size),
				}
				placements = append(placements, p)
			}

			ad := nvcrd.AllocatableDevices{
				Mig: &nvcrd.AllocatableMigDevices{
					Profile:     mig.profile.String(),
					ParentModel: device.model,
					Placements:  placements,
				},
			}

			migs[device.model][mig.profile.String()] = ad
		}
	}

	var allocatable []nvcrd.AllocatableDevices
	for _, device := range gpus {
		allocatable = append(allocatable, device)
	}
	for _, devices := range migs {
		for _, device := range devices {
			allocatable = append(allocatable, device)
		}
	}

	spec.AllocatableDevices = allocatable
}

func (s *DeviceState) syncAllocatedDevicesFromCRDSpec(spec *nvcrd.NodeAllocationStateSpec) error {
	gpus := s.allocatable
	migs := make(map[string]map[string]*MigDeviceInfo)

	for uuid, gpu := range gpus {
		ms, err := getMigDevices(gpu.GpuInfo)
		if err != nil {
			return fmt.Errorf("error getting MIG devices for GPU '%v': %v", uuid, err)
		}
		if len(ms) != 0 {
			migs[uuid] = ms
		}
	}

	allocated := make(ClaimAllocations)
	for claim, devices := range spec.ClaimAllocations {
		allocated[claim] = make(AllocatedDevices)
		for _, d := range devices {
			switch d.Type() {
			case nvcrd.GpuDeviceType:
				allocated[claim][d.Gpu.UUID] = AllocatedDeviceInfo{
					gpu: gpus[d.Gpu.UUID].GpuInfo,
				}
			case nvcrd.MigDeviceType:
				migInfo := migs[d.Mig.ParentUUID][d.Mig.UUID]
				if migInfo == nil {
					profile, err := ParseMigProfile(d.Mig.Profile)
					if err != nil {
						return fmt.Errorf("error parsing MIG profile for '%v': %v", d.Mig.Profile, err)
					}
					placement := &nvml.GpuInstancePlacement{
						Start: uint32(d.Mig.Placement.Start),
						Size:  uint32(d.Mig.Placement.Size),
					}
					migInfo, err = createMigDevice(gpus[d.Mig.ParentUUID].GpuInfo, profile, placement)
					if err != nil {
						return fmt.Errorf("error creating MIG device info for '%v' on GPU '%v': %v", d.Mig.Profile, d.Mig.ParentUUID, err)
					}
				} else {
					delete(migs[d.Mig.ParentUUID], d.Mig.UUID)
					if len(migs[d.Mig.ParentUUID]) == 0 {
						delete(migs, d.Mig.ParentUUID)
					}
				}
				allocated[claim][migInfo.uuid] = AllocatedDeviceInfo{
					mig: migInfo,
				}
			}
		}
	}

	if len(migs) != 0 {
		return fmt.Errorf("MIG devices found that aren't allocated to any claim: %+v", migs)
	}

	s.allocated = allocated
	return nil
}

func (s *DeviceState) syncAllocatedDevicesToCRDSpec(spec *nvcrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nvcrd.AllocatedDevices)
	for claim, devices := range s.allocated {
		var allocated []nvcrd.AllocatedDevice
		for uuid, device := range devices {
			outdevice := nvcrd.AllocatedDevice{}
			switch device.Type() {
			case nvcrd.GpuDeviceType:
				outdevice.Gpu = &nvcrd.AllocatedGpu{
					UUID:      uuid,
					Model:     device.gpu.model,
					CDIDevice: device.gpu.CDIDevice(),
				}
			case nvcrd.MigDeviceType:
				placement := nvcrd.MigDevicePlacement{
					Start: int(device.mig.giInfo.Placement.Start),
					Size:  int(device.mig.giInfo.Placement.Size),
				}
				outdevice.Mig = &nvcrd.AllocatedMigDevice{
					UUID:        uuid,
					Profile:     device.mig.profile.String(),
					ParentUUID:  device.mig.parent.uuid,
					ParentModel: device.mig.parent.model,
					CDIDevice:   device.mig.CDIDevice(),
					Placement:   placement,
				}
			}
			allocated = append(allocated, outdevice)
		}
		outcas[claim] = allocated
	}
	spec.ClaimAllocations = outcas
}
