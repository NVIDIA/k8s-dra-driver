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

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type ClaimAllocations map[string]*AllocatedDevices

type GpuInfo struct {
	minor                 int
	index                 int
	uuid                  string
	migEnabled            bool
	memoryBytes           uint64
	productName           string
	brand                 string
	architecture          string
	cudaComputeCapability string
}

type MigDeviceInfo struct {
	uuid    string
	parent  *GpuInfo
	profile *MigProfile
	giInfo  *nvml.GpuInstanceInfo
	ciInfo  *nvml.ComputeInstanceInfo
}

type AllocatedGpus struct {
	Devices []*GpuInfo
}

type AllocatedMigDevices struct {
	Devices []*MigDeviceInfo
}

type AllocatedDevices struct {
	Gpu *AllocatedGpus
	Mig *AllocatedMigDevices
}

func (d AllocatedDevices) Type() string {
	if d.Gpu != nil {
		return nascrd.GpuDeviceType
	}
	if d.Mig != nil {
		return nascrd.MigDeviceType
	}
	return nascrd.UnknownDeviceType
}

func (d AllocatedDevices) Len() int {
	if d.Gpu != nil {
		return len(d.Gpu.Devices)
	}
	if d.Mig != nil {
		return len(d.Mig.Devices)
	}
	return 0
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
	cdi         *CDIHandler
	allocatable AllocatableDevices
	allocated   ClaimAllocations
}

func NewDeviceState(config *Config) (*DeviceState, error) {
	allocatable, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi, err := NewCDIHandler(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %v", err)
	}

	err = cdi.CreateCommonSpecFile()
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for common edits: %v", err)
	}

	state := &DeviceState{
		cdi:         cdi,
		allocatable: allocatable,
		allocated:   make(ClaimAllocations),
	}

	err = state.syncAllocatedDevicesFromCRDSpec(&config.nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from CRD: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Allocate(claimUid string, request nascrd.RequestedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] != nil && s.allocated[claimUid].Len() != 0 {
		return s.cdi.GetClaimDevices(claimUid), nil
	}

	s.allocated[claimUid] = &AllocatedDevices{}

	var err error
	switch request.Type() {
	case nascrd.GpuDeviceType:
		err = s.allocateGpus(claimUid, request.Gpu)
	case nascrd.MigDeviceType:
		err = s.allocateMigDevices(claimUid, request.Mig)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	err = s.cdi.CreateClaimSpecFile(claimUid, s.allocated[claimUid])
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	return s.cdi.GetClaimDevices(claimUid), nil
}

func (s *DeviceState) Free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] == nil {
		return nil
	}
	switch s.allocated[claimUid].Type() {
	case nascrd.GpuDeviceType:
		for _, device := range s.allocated[claimUid].Gpu.Devices {
			err := s.freeGpu(device)
			if err != nil {
				return fmt.Errorf("free failed: %v", err)
			}
		}
	case nascrd.MigDeviceType:
		for _, device := range s.allocated[claimUid].Mig.Devices {
			err := s.freeMigDevice(device)
			if err != nil {
				return fmt.Errorf("free failed: %v", err)
			}
		}
	}

	delete(s.allocated, claimUid)

	err := s.cdi.DeleteClaimSpecFile(claimUid)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nascrd.NodeAllocationStateSpec) *nascrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToCRDSpec(outspec)
	s.syncAllocatedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) allocateGpus(claimUid string, requested *nascrd.RequestedGpus) error {
	for _, device := range requested.Devices {
		gpuInfo := s.allocatable[device.UUID].GpuInfo

		if _, exists := s.allocatable[device.UUID]; !exists {
			return fmt.Errorf("requested GPU does not exist: %v", device.UUID)
		}

		if requested.Sharing.IsTimeSlicing() {
			nvidiaDriverRoot := "/run/nvidia/driver"
			config, err := requested.Sharing.GetTimeSlicingConfig()
			if err != nil {
				return fmt.Errorf("error getting timeslice config for %v: %v", gpuInfo.uuid, err)
			}
			err = setCudaTimeSlice(gpuInfo, nvidiaDriverRoot, config)
			if err != nil {
				return fmt.Errorf("error setting timeslice for %v: %v", gpuInfo.uuid, err)
			}
		}

		if s.allocated[claimUid].Gpu == nil {
			s.allocated[claimUid].Gpu = &AllocatedGpus{}
		}

		s.allocated[claimUid].Gpu.Devices = append(s.allocated[claimUid].Gpu.Devices, gpuInfo)
	}

	return nil
}

func (s *DeviceState) allocateMigDevices(claimUid string, requested *nascrd.RequestedMigDevices) error {
	for _, device := range requested.Devices {
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

		if s.allocated[claimUid].Mig == nil {
			s.allocated[claimUid].Mig = &AllocatedMigDevices{}
		}

		s.allocated[claimUid].Mig.Devices = append(s.allocated[claimUid].Mig.Devices, migInfo)
	}

	return nil
}

func (s *DeviceState) freeGpu(gpu *GpuInfo) error {
	nvidiaDriverRoot := "/run/nvidia/driver"
	err := setCudaTimeSlice(gpu, nvidiaDriverRoot, nil)
	if err != nil {
		return fmt.Errorf("error setting timeslice for %v: %v", gpu.uuid, err)
	}
	return nil
}

func (s *DeviceState) freeMigDevice(mig *MigDeviceInfo) error {
	return deleteMigDevice(mig)
}

func (s *DeviceState) syncAllocatableDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	gpus := make(map[string]nascrd.AllocatableDevice)
	migs := make(map[string]map[string]nascrd.AllocatableDevice)
	for _, device := range s.allocatable {
		gpus[device.uuid] = nascrd.AllocatableDevice{
			Gpu: &nascrd.AllocatableGpu{
				Index:                 device.index,
				UUID:                  device.uuid,
				MigEnabled:            device.migEnabled,
				MemoryBytes:           device.memoryBytes,
				ProductName:           device.productName,
				Brand:                 device.brand,
				Architecture:          device.architecture,
				CUDAComputeCapability: device.cudaComputeCapability,
			},
		}

		if !device.migEnabled {
			continue
		}

		for _, mig := range device.migProfiles {
			if _, exists := migs[device.productName]; !exists {
				migs[device.productName] = make(map[string]nascrd.AllocatableDevice)
			}

			if _, exists := migs[device.productName][mig.profile.String()]; exists {
				continue
			}

			var placements []nascrd.MigDevicePlacement
			for _, placement := range mig.placements {
				p := nascrd.MigDevicePlacement{
					Start: int(placement.Start),
					Size:  int(placement.Size),
				}
				placements = append(placements, p)
			}

			ad := nascrd.AllocatableDevice{
				Mig: &nascrd.AllocatableMigDevice{
					Profile:           mig.profile.String(),
					ParentProductName: device.productName,
					Placements:        placements,
				},
			}

			migs[device.productName][mig.profile.String()] = ad
		}
	}

	var allocatable []nascrd.AllocatableDevice
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

func (s *DeviceState) syncAllocatedDevicesFromCRDSpec(spec *nascrd.NodeAllocationStateSpec) error {
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
		allocated[claim] = &AllocatedDevices{}
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			allocated[claim].Gpu = &AllocatedGpus{}
			for _, d := range devices.Gpu.Devices {
				gpuInfo := gpus[d.UUID].GpuInfo
				nvidiaDriverRoot := "/run/nvidia/driver"
				requested := spec.ClaimRequests[claim].Gpu
				if requested.Sharing.IsTimeSlicing() {
					config, err := requested.Sharing.GetTimeSlicingConfig()
					if err != nil {
						return fmt.Errorf("error getting timeslice for %v: %v", gpuInfo.uuid, err)
					}
					err = setCudaTimeSlice(gpuInfo, nvidiaDriverRoot, config)
					if err != nil {
						return fmt.Errorf("error setting timeslice for %v: %v", gpuInfo.uuid, err)
					}
				}
				allocated[claim].Gpu.Devices = append(allocated[claim].Gpu.Devices, gpuInfo)
			}
		case nascrd.MigDeviceType:
			allocated[claim].Mig = &AllocatedMigDevices{}
			for _, d := range devices.Mig.Devices {
				migInfo := migs[d.ParentUUID][d.UUID]
				if migInfo == nil {
					profile, err := ParseMigProfile(d.Profile)
					if err != nil {
						return fmt.Errorf("error parsing MIG profile for '%v': %v", d.Profile, err)
					}
					placement := &nvml.GpuInstancePlacement{
						Start: uint32(d.Placement.Start),
						Size:  uint32(d.Placement.Size),
					}
					migInfo, err = createMigDevice(gpus[d.ParentUUID].GpuInfo, profile, placement)
					if err != nil {
						return fmt.Errorf("error creating MIG device info for '%v' on GPU '%v': %v", d.Profile, d.ParentUUID, err)
					}
				} else {
					delete(migs[d.ParentUUID], d.UUID)
					if len(migs[d.ParentUUID]) == 0 {
						delete(migs, d.ParentUUID)
					}
				}
				allocated[claim].Mig.Devices = append(allocated[claim].Mig.Devices, migInfo)
			}
		}
	}

	if len(migs) != 0 {
		return fmt.Errorf("MIG devices found that aren't allocated to any claim: %+v", migs)
	}

	s.allocated = allocated
	return nil
}

func (s *DeviceState) syncAllocatedDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nascrd.AllocatedDevices)
	for claim, devices := range s.allocated {
		var allocated nascrd.AllocatedDevices
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			allocated.Gpu = &nascrd.AllocatedGpus{}
			for _, device := range devices.Gpu.Devices {
				outdevice := nascrd.AllocatedGpu{
					UUID: device.uuid,
				}
				allocated.Gpu.Devices = append(allocated.Gpu.Devices, outdevice)
			}
		case nascrd.MigDeviceType:
			allocated.Mig = &nascrd.AllocatedMigDevices{}
			for _, device := range devices.Mig.Devices {
				placement := nascrd.MigDevicePlacement{
					Start: int(device.giInfo.Placement.Start),
					Size:  int(device.giInfo.Placement.Size),
				}
				outdevice := nascrd.AllocatedMigDevice{
					UUID:       device.uuid,
					Profile:    device.profile.String(),
					ParentUUID: device.parent.uuid,
					Placement:  placement,
				}
				allocated.Mig.Devices = append(allocated.Mig.Devices, outdevice)
			}
		}
		outcas[claim] = allocated
	}
	spec.ClaimAllocations = outcas
}
