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

	"github.com/Masterminds/semver"
	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	resourceapi "k8s.io/api/resource/v1alpha3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type PreparedClaims map[string]*PreparedDevices

type GpuInfo struct {
	UUID                  string `json:"uuid"`
	index                 int
	minor                 int
	migEnabled            bool
	memoryBytes           uint64
	productName           string
	brand                 string
	architecture          string
	cudaComputeCapability string
	driverVersion         string
	cudaDriverVersion     string
}

type MigDeviceInfo struct {
	UUID    string `json:"uuid"`
	index   int
	parent  *GpuInfo
	profile nvdev.MigProfile
	giInfo  *nvml.GpuInstanceInfo
	ciInfo  *nvml.ComputeInstanceInfo
}

type AllocatedGpus struct {
	Devices []string `json:"devices"`
}

type AllocatedDevices struct {
	Gpu *AllocatedGpus `json:"gpu"`
}

type PreparedGpus struct {
	Devices []*PreparedGpu `json:"devices"`
}

type PreparedGpu struct {
	Info   *GpuInfo        `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

type PreparedMigDevices struct {
	Devices []*PreparedMig `json:"devices"`
}

type PreparedMig struct {
	Info   *MigDeviceInfo  `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

type PreparedDevices struct {
	Gpu              *PreparedGpus       `json:"gpu"`
	Mig              *PreparedMigDevices `json:"mig"`
	MpsControlDaemon *MpsControlDaemon
}

func (d AllocatedDevices) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	return types.UnknownDeviceType
}

func (d PreparedDevices) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	if d.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
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
	case types.GpuDeviceType:
		for _, device := range d.Gpu.Devices {
			deviceStrings = append(deviceStrings, device.Info.UUID)
		}
	case types.MigDeviceType:
		for _, device := range d.Mig.Devices {
			deviceStrings = append(deviceStrings, device.Info.UUID)
		}
	}
	return deviceStrings
}

func (d *PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	switch d.Type() {
	case types.GpuDeviceType:
		for _, gpu := range d.Gpu.Devices {
			devices = append(devices, gpu.Device)
		}
	case types.MigDeviceType:
		for _, mig := range d.Mig.Devices {
			devices = append(devices, mig.Device)
		}
	}
	return devices
}

type MigProfileInfo struct {
	profile    nvdev.MigProfile
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
	config      *Config

	nvdevlib          *deviceLib
	checkpointManager checkpointmanager.CheckpointManager
}

func NewDeviceState(ctx context.Context, config *Config) (*DeviceState, error) {
	containerDriverRoot := root(config.flags.containerDriverRoot)
	nvdevlib, err := newDeviceLib(containerDriverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create device library: %w", err)
	}

	allocatable, err := nvdevlib.enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %w", err)
	}

	devRoot := containerDriverRoot.getDevRoot()
	klog.Infof("using devRoot=%v", devRoot)

	hostDriverRoot := config.flags.hostDriverRoot
	cdi, err := NewCDIHandler(
		WithNvml(nvdevlib.nvmllib),
		WithDeviceLib(nvdevlib),
		WithDriverRoot(string(containerDriverRoot)),
		WithDevRoot(devRoot),
		WithTargetDriverRoot(hostDriverRoot),
		WithNvidiaCTKPath(config.flags.nvidiaCTKPath),
		WithCDIRoot(config.flags.cdiRoot),
		WithVendor(cdiVendor),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %w", err)
	}

	tsManager := NewTimeSlicingManager(nvdevlib)
	mpsManager := NewMpsManager(config, nvdevlib, MpsRoot, hostDriverRoot, MpsControlDaemonTemplatePath)

	if err := cdi.CreateStandardDeviceSpecFile(allocatable); err != nil {
		return nil, fmt.Errorf("unable to create base CDI spec file: %v", err)
	}

	checkpointManager, err := checkpointmanager.NewCheckpointManager(DriverPluginPath)
	if err != nil {
		return nil, fmt.Errorf("unable to create checkpoint manager: %v", err)
	}

	state := &DeviceState{
		cdi:               cdi,
		tsManager:         tsManager,
		mpsManager:        mpsManager,
		allocatable:       allocatable,
		config:            config,
		nvdevlib:          nvdevlib,
		checkpointManager: checkpointManager,
	}

	checkpoints, err := state.checkpointManager.ListCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("unable to list checkpoints: %v", err)
	}

	for _, c := range checkpoints {
		if c == DriverPluginCheckpointFile {
			return state, nil
		}
	}

	checkpoint := newCheckpoint()
	if err := state.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Prepare(claim *resourceapi.ResourceClaim) ([]*drapbv1.Device, error) {
	s.Lock()
	defer s.Unlock()

	claimUID := string(claim.UID)

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] != nil {
		return preparedClaims[claimUID].GetDevices(), nil
	}

	preparedDevices := &PreparedDevices{}

	var err error
	preparedDevices.Gpu, err = s.prepareGpus(claim)
	if err != nil {
		return nil, fmt.Errorf("GPU prepare failed: %w", err)
	}
	// TODO: Defer enabling sharing with structured parameters until we
	//       update to the APIs for Kubernetes 1.31.
	//
	// err := s.setupSharing(ctx, allocated.Gpu.Sharing, allocated.ClaimInfo, prepared)
	// if err != nil {
	// 	return nil, fmt.Errorf("error setting up sharing: %w", err)
	// }

	// TODO: Dynamic MIG is not yet supported with structured parameters.
	//      Refactor this to allow for the allocation of statically
	//      partitioned MIG devices in 1.31.
	//
	// prepared.Mig, err = s.prepareMigDevices(claimUID, allocated.Mig)
	// if err != nil {
	// 	return nil, fmt.Errorf("MIG device allocation failed: %w", err)
	// }
	// err = s.setupSharing(ctx, allocated.Mig.Sharing, allocated.ClaimInfo, prepared)
	// if err != nil {
	// 	return nil, fmt.Errorf("error setting up sharing: %w", err)
	// }

	if err := s.cdi.CreateClaimSpecFile(claimUID, preparedDevices); err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %w", err)
	}

	preparedClaims[claimUID] = preparedDevices
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return preparedClaims[claimUID].GetDevices(), nil
}

func (s *DeviceState) Unprepare(ctx context.Context, claimUID string) error {
	s.Lock()
	defer s.Unlock()

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] == nil {
		return nil
	}

	if preparedClaims[claimUID].MpsControlDaemon != nil {
		err := preparedClaims[claimUID].MpsControlDaemon.Stop(ctx)
		if err != nil {
			return fmt.Errorf("error stopping MPS control daemon: %w", err)
		}
	}

	switch preparedClaims[claimUID].Type() {
	case types.GpuDeviceType:
		err := s.unprepareGpus(claimUID, preparedClaims[claimUID])
		if err != nil {
			return fmt.Errorf("GPU unprepare failed: %w", err)
		}
	case types.MigDeviceType:
		// TODO: Dynamic MIG is not yet supported with structured parameters.
		//      Refactor this to allow for the allocation of statically
		//      partitioned MIG devices in 1.31.
		//
		// err := s.unprepareMigDevices(claimUID, s.prepared[claimUID])
		// if err != nil {
		// 	return fmt.Errorf("unprepare failed: %w", err)
		// }
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %w", err)
	}

	delete(preparedClaims, claimUID)
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return nil
}

func (s *DeviceState) prepareGpus(claim *resourceapi.ResourceClaim) (*PreparedGpus, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim not yet allocated")
	}

	var prepared PreparedGpus
	for _, result := range claim.Status.Allocation.Devices.Results {
		if _, exists := s.allocatable[result.Device]; !exists {
			return nil, fmt.Errorf("requested GPU is not allocatable: %v", result.Device)
		}

		cdiDevices := s.cdi.GetStandardDevices([]string{result.Device})
		cdiDevices = append(cdiDevices, s.cdi.GetClaimDevice(string(claim.UID)))

		device := &PreparedGpu{
			Info: s.allocatable[result.Device].GpuInfo,
			Device: &drapbv1.Device{
				RequestNames: []string{result.Request},
				PoolName:     result.Pool,
				DeviceName:   result.Device,
				CDIDeviceIDs: cdiDevices,
			},
		}

		prepared.Devices = append(prepared.Devices, device)
	}

	return &prepared, nil
}

func (s *DeviceState) unprepareGpus(claimUID string, devices *PreparedDevices) error {
	err := s.tsManager.SetTimeSlice(devices, nil)
	if err != nil {
		return fmt.Errorf("error setting timeslice for devices: %w", err)
	}
	return nil
}

// TODO: Dynamic MIG is not yet supported with structured parameters.
// Refactor this to allow for the allocation of statically partitioned MIG
// devices.
//
// func (s *DeviceState) prepareMigDevices(claimUID string, allocated *nascrd.AllocatedMigDevices) (*PreparedMigDevices, error) {
// 	prepared := &PreparedMigDevices{}
// 
// 	for _, device := range allocated.Devices {
// 		if _, exists := s.allocatable[device.ParentUUID]; !exists {
// 			return nil, fmt.Errorf("allocated GPU does not exist: %v", device.ParentUUID)
// 		}
// 
// 		parent := s.allocatable[device.ParentUUID]
// 
// 		if !parent.migEnabled {
// 			return nil, fmt.Errorf("cannot prepare a GPU with MIG mode disabled: %v", device.ParentUUID)
// 		}
// 
// 		if _, exists := parent.migProfiles[device.Profile]; !exists {
// 			return nil, fmt.Errorf("MIG profile %v does not exist on GPU: %v", device.Profile, device.ParentUUID)
// 		}
// 
// 		placement := nvml.GpuInstancePlacement{
// 			Start: uint32(device.Placement.Start),
// 			Size:  uint32(device.Placement.Size),
// 		}
// 
// 		migInfo, err := s.nvdevlib.createMigDevice(parent.GpuInfo, parent.migProfiles[device.Profile].profile, &placement)
// 		if err != nil {
// 			return nil, fmt.Errorf("error creating MIG device: %w", err)
// 		}
// 
// 		prepared.Devices = append(prepared.Devices, migInfo)
// 	}
// 
// 	return prepared, nil
// }
// 
// func (s *DeviceState) unprepareMigDevices(claimUID string, devices *PreparedDevices) error {
// 	for _, device := range devices.Mig.Devices {
// 		err := s.nvdevlib.deleteMigDevice(device)
// 		if err != nil {
// 			return fmt.Errorf("error deleting MIG device for %v: %w", device.uuid, err)
// 		}
// 	}
// 	return nil
//}

// TODO: Defer enabling sharing with structured parameters until we update to
//       the APIs for Kubrnetes 1.31
//
// func (s *DeviceState) setupSharing(ctx context.Context, sharing sharing.Interface, claim *types.ClaimInfo, devices *PreparedDevices) error {
// 	if sharing.IsTimeSlicing() {
// 		config, err := sharing.GetTimeSlicingConfig()
// 		if err != nil {
// 			return fmt.Errorf("error getting timeslice for %v: %w", claim.UID, err)
// 		}
// 		err = s.tsManager.SetTimeSlice(devices, config)
// 		if err != nil {
// 			return fmt.Errorf("error setting timeslice for %v: %w", claim.UID, err)
// 		}
// 	}
// 
// 	if sharing.IsMps() {
// 		config, err := sharing.GetMpsConfig()
// 		if err != nil {
// 			return fmt.Errorf("error getting MPS configuration: %w", err)
// 		}
// 		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(claim, devices, config)
// 		err = mpsControlDaemon.Start(ctx)
// 		if err != nil {
// 			return fmt.Errorf("error starting MPS control daemon: %w", err)
// 		}
// 		err = mpsControlDaemon.AssertReady(ctx)
// 		if err != nil {
// 			return fmt.Errorf("MPS control daemon is not yet ready: %w", err)
// 		}
// 		devices.MpsControlDaemon = mpsControlDaemon
// 	}
// 
// 	return nil
// }

func (d *AllocatableDeviceInfo) GetDevice() resourceapi.Device {
	device := resourceapi.Device{
		Name: fmt.Sprintf("gpu-%d", d.index),
		Basic: &resourceapi.BasicDevice{
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"uuid": {
					StringValue: &d.UUID,
				},
				"minor": {
					IntValue: ptr.To(int64(d.minor)),
				},
				"index": {
					IntValue: ptr.To(int64(d.index)),
				},
				"migEnabled": {
					BoolValue: &d.migEnabled,
				},
				"productName": {
					StringValue: &d.productName,
				},
				"brand": {
					StringValue: &d.brand,
				},
				"architecture": {
					StringValue: &d.architecture,
				},
				"cudaComputeCapability": {
					VersionValue: ptr.To(semver.MustParse(d.cudaComputeCapability).String()),
				},
				"driverVersion": {
					VersionValue: ptr.To(semver.MustParse(d.driverVersion).String()),
				},
				"cudaDriverVersion": {
					VersionValue: ptr.To(semver.MustParse(d.cudaDriverVersion).String()),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resource.Quantity{
				"memory": *resource.NewQuantity(int64(d.memoryBytes), resource.BinarySI),
			},
		},
	}
	return device
}
