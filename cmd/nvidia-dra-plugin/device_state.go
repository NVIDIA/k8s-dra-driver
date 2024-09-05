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
	"strings"
	"sync"

	"github.com/Masterminds/semver"
	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	resourceapi "k8s.io/api/resource/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"
)

type AllocatableDevices map[string]*AllocatableDeviceInfo
type PreparedClaims map[string]*PreparedDevices

type GpuInfo struct {
	UUID                  string `json:"uuid"`
	minor                 int
	index                 int
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
	Devices []*GpuInfo `json:"devices"`
}

type PreparedMigDevices struct {
	Devices []*MigDeviceInfo `json:"devices"`
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
			deviceStrings = append(deviceStrings, device.UUID)
		}
	case types.MigDeviceType:
		for _, device := range d.Mig.Devices {
			deviceStrings = append(deviceStrings, device.UUID)
		}
	}
	return deviceStrings
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

func (s *DeviceState) Prepare(ctx context.Context, claimUID string, allocated AllocatedDevices) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] != nil {
		return s.cdi.GetClaimDevices(claimUID), nil
	}

	preparedDevices := &PreparedDevices{}

	var err error
	switch allocated.Type() {
	case types.GpuDeviceType:
		preparedDevices.Gpu, err = s.prepareGpus(claimUID, allocated.Gpu)
		if err != nil {
			return nil, fmt.Errorf("GPU allocation failed: %w", err)
		}
		// TODO: Defer enabling sharing with structured parameters until we
		//       update to the APIs for Kubernetes 1.31.
		//
		//err := s.setupSharing(ctx, allocated.Gpu.Sharing, allocated.ClaimInfo, prepared)
		//if err != nil {
		//	return nil, fmt.Errorf("error setting up sharing: %w", err)
		//}
	case types.MigDeviceType:
		// TODO: Dynamic MIG is not yet supported with structured parameters.
		//      Refactor this to allow for the allocation of statically
		//      partitioned MIG devices in 1.31.
		//
		//prepared.Mig, err = s.prepareMigDevices(claimUID, allocated.Mig)
		//if err != nil {
		//	return nil, fmt.Errorf("MIG device allocation failed: %w", err)
		//}
		//err = s.setupSharing(ctx, allocated.Mig.Sharing, allocated.ClaimInfo, prepared)
		//if err != nil {
		//	return nil, fmt.Errorf("error setting up sharing: %w", err)
		//}
	}

	err = s.cdi.CreateClaimSpecFile(claimUID, preparedDevices)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %w", err)
	}

	preparedClaims[claimUID] = preparedDevices
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return s.cdi.GetClaimDevices(claimUID), nil
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
			return fmt.Errorf("unprepare failed: %w", err)
		}
	case types.MigDeviceType:
		// TODO: Dynamic MIG is not yet supported with structured parameters.
		//      Refactor this to allow for the allocation of statically
		//      partitioned MIG devices in 1.31.
		//
		//err := s.unprepareMigDevices(claimUID, s.prepared[claimUID])
		//if err != nil {
		//	return fmt.Errorf("unprepare failed: %w", err)
		//}
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

func (s *DeviceState) prepareGpus(claimUID string, allocated *AllocatedGpus) (*PreparedGpus, error) {
	prepared := &PreparedGpus{}

	for _, device := range allocated.Devices {
		gpuInfo := s.allocatable[device].GpuInfo

		if _, exists := s.allocatable[device]; !exists {
			return nil, fmt.Errorf("allocated GPU does not exist: %v", device)
		}

		prepared.Devices = append(prepared.Devices, gpuInfo)
	}

	return prepared, nil
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
//func (s *DeviceState) prepareMigDevices(claimUID string, allocated *nascrd.AllocatedMigDevices) (*PreparedMigDevices, error) {
//	prepared := &PreparedMigDevices{}
//
//	for _, device := range allocated.Devices {
//		if _, exists := s.allocatable[device.ParentUUID]; !exists {
//			return nil, fmt.Errorf("allocated GPU does not exist: %v", device.ParentUUID)
//		}
//
//		parent := s.allocatable[device.ParentUUID]
//
//		if !parent.migEnabled {
//			return nil, fmt.Errorf("cannot prepare a GPU with MIG mode disabled: %v", device.ParentUUID)
//		}
//
//		if _, exists := parent.migProfiles[device.Profile]; !exists {
//			return nil, fmt.Errorf("MIG profile %v does not exist on GPU: %v", device.Profile, device.ParentUUID)
//		}
//
//		placement := nvml.GpuInstancePlacement{
//			Start: uint32(device.Placement.Start),
//			Size:  uint32(device.Placement.Size),
//		}
//
//		migInfo, err := s.nvdevlib.createMigDevice(parent.GpuInfo, parent.migProfiles[device.Profile].profile, &placement)
//		if err != nil {
//			return nil, fmt.Errorf("error creating MIG device: %w", err)
//		}
//
//		prepared.Devices = append(prepared.Devices, migInfo)
//	}
//
//	return prepared, nil
//}
//
//func (s *DeviceState) unprepareMigDevices(claimUID string, devices *PreparedDevices) error {
//	for _, device := range devices.Mig.Devices {
//		err := s.nvdevlib.deleteMigDevice(device)
//		if err != nil {
//			return fmt.Errorf("error deleting MIG device for %v: %w", device.uuid, err)
//		}
//	}
//	return nil
//}

// TODO: Defer enabling sharing with structured parameters until we update to
//       the APIs for Kubrnetes 1.31
//
//func (s *DeviceState) setupSharing(ctx context.Context, sharing sharing.Interface, claim *types.ClaimInfo, devices *PreparedDevices) error {
//	if sharing.IsTimeSlicing() {
//		config, err := sharing.GetTimeSlicingConfig()
//		if err != nil {
//			return fmt.Errorf("error getting timeslice for %v: %w", claim.UID, err)
//		}
//		err = s.tsManager.SetTimeSlice(devices, config)
//		if err != nil {
//			return fmt.Errorf("error setting timeslice for %v: %w", claim.UID, err)
//		}
//	}
//
//	if sharing.IsMps() {
//		config, err := sharing.GetMpsConfig()
//		if err != nil {
//			return fmt.Errorf("error getting MPS configuration: %w", err)
//		}
//		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(claim, devices, config)
//		err = mpsControlDaemon.Start(ctx)
//		if err != nil {
//			return fmt.Errorf("error starting MPS control daemon: %w", err)
//		}
//		err = mpsControlDaemon.AssertReady(ctx)
//		if err != nil {
//			return fmt.Errorf("MPS control daemon is not yet ready: %w", err)
//		}
//		devices.MpsControlDaemon = mpsControlDaemon
//	}
//
//	return nil
//}

func (s *DeviceState) getResourceModelFromAllocatableDevices() resourceapi.ResourceModel {
	var instances []resourceapi.NamedResourcesInstance
	for _, device := range s.allocatable {
		instance := resourceapi.NamedResourcesInstance{
			Name: strings.ToLower(device.UUID),
			Attributes: []resourceapi.NamedResourcesAttribute{
				{
					Name: "index",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						IntValue: ptr.To(int64(device.index)),
					},
				},
				{
					Name: "uuid",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						StringValue: &device.UUID,
					},
				},
				{
					Name: "mig-enabled",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						BoolValue: &device.migEnabled,
					},
				},
				{
					Name: "memory",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						QuantityValue: resource.NewQuantity(int64(device.memoryBytes), resource.BinarySI),
					},
				},
				{
					Name: "product-name",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						StringValue: &device.productName,
					},
				},
				{
					Name: "brand",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						StringValue: &device.brand,
					},
				},
				{
					Name: "architecture",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						StringValue: &device.architecture,
					},
				},
				{
					Name: "cuda-compute-capability",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						VersionValue: ptr.To(semver.MustParse(device.cudaComputeCapability).String()),
					},
				},
				{
					Name: "driver-version",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						VersionValue: ptr.To(semver.MustParse(device.driverVersion).String()),
					},
				},
				{
					Name: "cuda-driver-version",
					NamedResourcesAttributeValue: resourceapi.NamedResourcesAttributeValue{
						VersionValue: ptr.To(semver.MustParse(device.cudaDriverVersion).String()),
					},
				},
			},
		}
		instances = append(instances, instance)
	}

	model := resourceapi.ResourceModel{
		NamedResources: &resourceapi.NamedResourcesResources{instances},
	}

	return model
}
