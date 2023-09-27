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
	"sync"

	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type PreparedClaims map[string]*PreparedDevices

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

type PreparedGpus struct {
	Devices []*GpuInfo
}

type PreparedMigDevices struct {
	Devices []*MigDeviceInfo
}

type PreparedDevices struct {
	Gpu              *PreparedGpus
	Mig              *PreparedMigDevices
	MpsControlDaemon *MpsControlDaemon
}

func (d PreparedDevices) Type() string {
	if d.Gpu != nil {
		return nascrd.GpuDeviceType
	}
	if d.Mig != nil {
		return nascrd.MigDeviceType
	}
	return nascrd.UnknownDeviceType
}

func (d PreparedDevices) Len() int {
	if d.Gpu != nil {
		return len(d.Gpu.Devices)
	}
	if d.Mig != nil {
		return len(d.Mig.Devices)
	}
	return 0
}

func (d *PreparedDevices) UUIDs() []string {
	var deviceStrings []string
	switch d.Type() {
	case nascrd.GpuDeviceType:
		for _, device := range d.Gpu.Devices {
			deviceStrings = append(deviceStrings, device.uuid)
		}
	case nascrd.MigDeviceType:
		for _, device := range d.Mig.Devices {
			deviceStrings = append(deviceStrings, device.uuid)
		}
	}
	return deviceStrings
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
	tsManager   *TimeSlicingManager
	mpsManager  *MpsManager
	allocatable AllocatableDevices
	prepared    PreparedClaims
	config      *Config
}

func NewDeviceState(ctx context.Context, config *Config) (*DeviceState, error) {
	nvdevlib := newDeviceLib()

	allocatable, err := nvdevlib.enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	containerDriverRoot := config.flags.containerDriverRoot
	cdi, err := NewCDIHandler(
		WithNvml(nvdevlib.nvmllib),
		WithDeviceLib(nvdevlib),
		WithDriverRoot(containerDriverRoot),
		WithTargetDriverRoot(config.flags.hostDriverRoot),
		WithNvidiaCTKPath(config.flags.nvidiaCTKPath),
		WithCDIRoot(config.flags.cdiRoot),
		WithVendor(cdiVendor),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %v", err)
	}

	tsManager := NewTimeSlicingManager(containerDriverRoot)
	mpsManager := NewMpsManager(config, MpsRoot, containerDriverRoot, MpsControlDaemonTemplatePath)

	state := &DeviceState{
		cdi:         cdi,
		tsManager:   tsManager,
		mpsManager:  mpsManager,
		allocatable: allocatable,
		prepared:    make(PreparedClaims),
		config:      config,
	}

	err = state.syncPreparedDevicesFromCRDSpec(ctx, &config.nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync prepared devices from CRD: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Prepare(ctx context.Context, claimUID string, allocated nascrd.AllocatedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] != nil {
		return s.cdi.GetClaimDevices(claimUID), nil
	}

	prepared := &PreparedDevices{}

	var err error
	switch allocated.Type() {
	case nascrd.GpuDeviceType:
		prepared.Gpu, err = s.prepareGpus(claimUID, allocated.Gpu)
		if err != nil {
			return nil, fmt.Errorf("GPU allocation failed: %v", err)
		}
		err = s.setupSharing(ctx, allocated.Gpu.Sharing, allocated.ClaimInfo, prepared)
		if err != nil {
			return nil, fmt.Errorf("error setting up sharing: %v", err)
		}
	case nascrd.MigDeviceType:
		prepared.Mig, err = s.prepareMigDevices(claimUID, allocated.Mig)
		if err != nil {
			return nil, fmt.Errorf("MIG device allocation failed: %v", err)
		}
		err = s.setupSharing(ctx, allocated.Mig.Sharing, allocated.ClaimInfo, prepared)
		if err != nil {
			return nil, fmt.Errorf("error setting up sharing: %v", err)
		}
	}

	err = s.cdi.CreateClaimSpecFile(claimUID, prepared)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	s.prepared[claimUID] = prepared

	return s.cdi.GetClaimDevices(claimUID), nil
}

func (s *DeviceState) Unprepare(ctx context.Context, claimUID string) error {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] == nil {
		return nil
	}

	if s.prepared[claimUID].MpsControlDaemon != nil {
		err := s.prepared[claimUID].MpsControlDaemon.Stop(ctx)
		if err != nil {
			return fmt.Errorf("error stopping MPS control daemon: %v", err)
		}
	}

	switch s.prepared[claimUID].Type() {
	case nascrd.GpuDeviceType:
		err := s.unprepareGpus(claimUID, s.prepared[claimUID])
		if err != nil {
			return fmt.Errorf("unprepare failed: %v", err)
		}
	case nascrd.MigDeviceType:
		err := s.unprepareMigDevices(claimUID, s.prepared[claimUID])
		if err != nil {
			return fmt.Errorf("unprepare failed: %v", err)
		}
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	delete(s.prepared, claimUID)

	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nascrd.NodeAllocationStateSpec) *nascrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToCRDSpec(outspec)
	s.syncPreparedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) prepareGpus(claimUID string, allocated *nascrd.AllocatedGpus) (*PreparedGpus, error) {
	prepared := &PreparedGpus{}

	for _, device := range allocated.Devices {
		gpuInfo := s.allocatable[device.UUID].GpuInfo

		if _, exists := s.allocatable[device.UUID]; !exists {
			return nil, fmt.Errorf("allocated GPU does not exist: %v", device.UUID)
		}

		prepared.Devices = append(prepared.Devices, gpuInfo)
	}

	return prepared, nil
}

func (s *DeviceState) prepareMigDevices(claimUID string, allocated *nascrd.AllocatedMigDevices) (*PreparedMigDevices, error) {
	l := newDeviceLib()

	prepared := &PreparedMigDevices{}

	for _, device := range allocated.Devices {
		if _, exists := s.allocatable[device.ParentUUID]; !exists {
			return nil, fmt.Errorf("allocated GPU does not exist: %v", device.ParentUUID)
		}

		parent := s.allocatable[device.ParentUUID]

		if !parent.migEnabled {
			return nil, fmt.Errorf("cannot prepare a GPU with MIG mode disabled: %v", device.ParentUUID)
		}

		if _, exists := parent.migProfiles[device.Profile]; !exists {
			return nil, fmt.Errorf("MIG profile %v does not exist on GPU: %v", device.Profile, device.ParentUUID)
		}

		placement := nvml.GpuInstancePlacement{
			Start: uint32(device.Placement.Start),
			Size:  uint32(device.Placement.Size),
		}

		migInfo, err := l.createMigDevice(parent.GpuInfo, parent.migProfiles[device.Profile].profile, &placement)
		if err != nil {
			return nil, fmt.Errorf("error creating MIG device: %v", err)
		}

		prepared.Devices = append(prepared.Devices, migInfo)
	}

	return prepared, nil
}

func (s *DeviceState) unprepareGpus(claimUID string, devices *PreparedDevices) error {
	err := s.tsManager.SetTimeSlice(devices, nil)
	if err != nil {
		return fmt.Errorf("error setting timeslice for devices: %v", err)
	}
	return nil
}

func (s *DeviceState) unprepareMigDevices(claimUID string, devices *PreparedDevices) error {
	l := newDeviceLib()
	for _, device := range devices.Mig.Devices {
		err := l.deleteMigDevice(device)
		if err != nil {
			return fmt.Errorf("error deleting MIG device for %v: %v", device.uuid, err)
		}
	}
	return nil
}

func (s *DeviceState) setupSharing(ctx context.Context, sharing nascrd.Sharing, claim *nascrd.ClaimInfo, devices *PreparedDevices) error {
	if sharing.IsTimeSlicing() {
		config, err := sharing.GetTimeSlicingConfig()
		if err != nil {
			return fmt.Errorf("error getting timeslice for %v: %v", claim.UID, err)
		}
		err = s.tsManager.SetTimeSlice(devices, config)
		if err != nil {
			return fmt.Errorf("error setting timeslice for %v: %v", claim.UID, err)
		}
	}

	if sharing.IsMps() {
		config, err := sharing.GetMpsConfig()
		if err != nil {
			return fmt.Errorf("error getting MPS configuration: %v", err)
		}
		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(claim, devices, config)
		err = mpsControlDaemon.Start(ctx)
		if err != nil {
			return fmt.Errorf("error starting MPS control daemon: %v", err)
		}
		err = mpsControlDaemon.AssertReady(ctx)
		if err != nil {
			return fmt.Errorf("MPS control daemon is not yet ready: %v", err)
		}
		devices.MpsControlDaemon = mpsControlDaemon
	}

	return nil
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

func (s *DeviceState) syncPreparedDevicesFromCRDSpec(ctx context.Context, spec *nascrd.NodeAllocationStateSpec) error {
	gpus := s.allocatable
	migs := make(map[string]map[string]*MigDeviceInfo)

	l := newDeviceLib()
	for uuid, gpu := range gpus {
		ms, err := l.getMigDevices(gpu.GpuInfo)
		if err != nil {
			return fmt.Errorf("error getting MIG devices for GPU '%v': %v", uuid, err)
		}
		if len(ms) != 0 {
			migs[uuid] = ms
		}
	}

	prepared := make(PreparedClaims)
	for claim, devices := range spec.PreparedClaims {
		if _, exists := spec.AllocatedClaims[claim]; !exists {
			continue
		}
		allocated := spec.AllocatedClaims[claim]
		prepared[claim] = &PreparedDevices{}
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			prepared[claim].Gpu = &PreparedGpus{}
			for _, d := range devices.Gpu.Devices {
				prepared[claim].Gpu.Devices = append(prepared[claim].Gpu.Devices, gpus[d.UUID].GpuInfo)
			}
			err := s.setupSharing(ctx, allocated.Gpu.Sharing, allocated.ClaimInfo, prepared[claim])
			if err != nil {
				return fmt.Errorf("error setting up sharing: %v", err)
			}
		case nascrd.MigDeviceType:
			prepared[claim].Mig = &PreparedMigDevices{}
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
					migInfo, err = l.createMigDevice(gpus[d.ParentUUID].GpuInfo, profile, placement)
					if err != nil {
						return fmt.Errorf("error creating MIG device info for '%v' on GPU '%v': %v", d.Profile, d.ParentUUID, err)
					}
				} else {
					delete(migs[d.ParentUUID], d.UUID)
					if len(migs[d.ParentUUID]) == 0 {
						delete(migs, d.ParentUUID)
					}
				}
				prepared[claim].Mig.Devices = append(prepared[claim].Mig.Devices, migInfo)
			}
			err := s.setupSharing(ctx, allocated.Mig.Sharing, allocated.ClaimInfo, prepared[claim])
			if err != nil {
				return fmt.Errorf("error setting up sharing: %v", err)
			}
		}
	}

	if len(migs) != 0 {
		return fmt.Errorf("MIG devices found that aren't prepared to any claim: %+v", migs)
	}

	s.prepared = prepared
	return nil
}

func (s *DeviceState) syncPreparedDevicesToCRDSpec(spec *nascrd.NodeAllocationStateSpec) {
	outcas := make(map[string]nascrd.PreparedDevices)
	for claim, devices := range s.prepared {
		var prepared nascrd.PreparedDevices
		switch devices.Type() {
		case nascrd.GpuDeviceType:
			prepared.Gpu = &nascrd.PreparedGpus{}
			for _, device := range devices.Gpu.Devices {
				outdevice := nascrd.PreparedGpu{
					UUID: device.uuid,
				}
				prepared.Gpu.Devices = append(prepared.Gpu.Devices, outdevice)
			}
		case nascrd.MigDeviceType:
			prepared.Mig = &nascrd.PreparedMigDevices{}
			for _, device := range devices.Mig.Devices {
				placement := nascrd.MigDevicePlacement{
					Start: int(device.giInfo.Placement.Start),
					Size:  int(device.giInfo.Placement.Size),
				}
				outdevice := nascrd.PreparedMigDevice{
					UUID:       device.uuid,
					Profile:    device.profile.String(),
					ParentUUID: device.parent.uuid,
					Placement:  placement,
				}
				prepared.Mig.Devices = append(prepared.Mig.Devices, outdevice)
			}
		}
		outcas[claim] = prepared
	}
	spec.PreparedClaims = outcas
}
