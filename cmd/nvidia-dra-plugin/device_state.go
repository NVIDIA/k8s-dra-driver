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

type AllocatableDevices map[string]*AllocatableDevice
type PreparedDevices []*PreparedDeviceGroup
type PreparedClaims map[string]PreparedDevices

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
	migProfiles           []*MigProfileInfo
}

type MigDeviceInfo struct {
	UUID          string `json:"uuid"`
	index         int
	profile       string
	parent        *GpuInfo
	placement     *MigDevicePlacement
	giProfileInfo *nvml.GpuInstanceProfileInfo
	giInfo        *nvml.GpuInstanceInfo
	ciProfileInfo *nvml.ComputeInstanceProfileInfo
	ciInfo        *nvml.ComputeInstanceInfo
}

type MigProfileInfo struct {
	profile    nvdev.MigProfile
	placements []*MigDevicePlacement
}

type MigDevicePlacement struct {
	nvml.GpuInstancePlacement
}

type AllocatableDevice struct {
	Gpu *GpuInfo
	Mig *MigDeviceInfo
}

type PreparedDeviceGroup struct {
	Devices          []*PreparedDevice `json:"devices"`
	MpsControlDaemon *MpsControlDaemon `json:"mpsControlDaemon"`
}

type PreparedDevice struct {
	Gpu *PreparedGpu       `json:"gpu"`
	Mig *PreparedMigDevice `json:"mig"`
}

type PreparedGpu struct {
	Info   *GpuInfo        `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

type PreparedMigDevice struct {
	Info   *MigDeviceInfo  `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

func (p MigProfileInfo) String() string {
	return p.profile.String()
}

func (d AllocatableDevice) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	if d.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
}

func (d PreparedDevice) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	if d.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
}

func (d *PreparedDeviceGroup) GpuUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == types.GpuDeviceType {
			uuids = append(uuids, device.Gpu.Info.UUID)
		}
	}
	return uuids
}

func (d *PreparedDeviceGroup) MigDeviceUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == types.MigDeviceType {
			uuids = append(uuids, device.Mig.Info.UUID)
		}
	}
	return uuids
}

func (d *PreparedDeviceGroup) UUIDs() []string {
	return append(d.GpuUUIDs(), d.MigDeviceUUIDs()...)
}

func (d PreparedDevices) GpuUUIDs() []string {
	var uuids []string
	for _, group := range d {
		uuids = append(uuids, group.GpuUUIDs()...)
	}
	return uuids
}

func (d PreparedDevices) MigDeviceUUIDs() []string {
	var uuids []string
	for _, group := range d {
		uuids = append(uuids, group.MigDeviceUUIDs()...)
	}
	return uuids
}

func (d PreparedDevices) UUIDs() []string {
	var uuids []string
	for _, group := range d {
		uuids = append(uuids, group.UUIDs()...)
	}
	return uuids
}

func (d PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, group := range d {
		devices = append(devices, group.GetDevices()...)
	}
	return devices
}

func (d *PreparedDeviceGroup) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, device := range d.Devices {
		switch device.Type() {
		case types.GpuDeviceType:
			devices = append(devices, device.Gpu.Device)
		case types.MigDeviceType:
			devices = append(devices, device.Mig.Device)
		}
	}
	return devices
}

func (d *AllocatableDevice) GetDevice() resourceapi.Device {
	switch d.Type() {
	case types.GpuDeviceType:
		return d.Gpu.GetDevice()
	case types.MigDeviceType:
		return d.Mig.GetDevice()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *AllocatableDevice) CanonicalName() string {
	switch d.Type() {
	case types.GpuDeviceType:
		return d.Gpu.CanonicalName()
	case types.MigDeviceType:
		return d.Mig.CanonicalName()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *AllocatableDevice) CanonicalIndex() string {
	switch d.Type() {
	case types.GpuDeviceType:
		return d.Gpu.CanonicalIndex()
	case types.MigDeviceType:
		return d.Mig.CanonicalIndex()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *PreparedDevice) CanonicalName() string {
	switch d.Type() {
	case types.GpuDeviceType:
		return d.Gpu.Info.CanonicalName()
	case types.MigDeviceType:
		return d.Mig.Info.CanonicalName()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *PreparedDevice) CanonicalIndex() string {
	switch d.Type() {
	case types.GpuDeviceType:
		return d.Gpu.Info.CanonicalIndex()
	case types.MigDeviceType:
		return d.Mig.Info.CanonicalIndex()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *GpuInfo) CanonicalName() string {
	return fmt.Sprintf("gpu-%d", d.index)
}

func (d *MigDeviceInfo) CanonicalName() string {
	return fmt.Sprintf("gpu-%d-mig-%d-%d-%d", d.parent.index, d.giInfo.ProfileId, d.placement.Start, d.placement.Size)
}

func (d *GpuInfo) CanonicalIndex() string {
	return fmt.Sprintf("%d", d.index)
}

func (d *MigDeviceInfo) CanonicalIndex() string {
	return fmt.Sprintf("%d:%d", d.parent.index, d.index)
}

func (d *GpuInfo) GetDevice() resourceapi.Device {
	device := resourceapi.Device{
		Name: d.CanonicalName(),
		Basic: &resourceapi.BasicDevice{
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"type": {
					StringValue: ptr.To(types.GpuDeviceType),
				},
				"uuid": {
					StringValue: &d.UUID,
				},
				"minor": {
					IntValue: ptr.To(int64(d.minor)),
				},
				"index": {
					IntValue: ptr.To(int64(d.index)),
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

func (d *MigDeviceInfo) GetDevice() resourceapi.Device {
	device := resourceapi.Device{
		Name: d.CanonicalName(),
		Basic: &resourceapi.BasicDevice{
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"type": {
					StringValue: ptr.To(types.MigDeviceType),
				},
				"uuid": {
					StringValue: &d.UUID,
				},
				"parentUUID": {
					StringValue: &d.parent.UUID,
				},
				"index": {
					IntValue: ptr.To(int64(d.index)),
				},
				"parentIndex": {
					IntValue: ptr.To(int64(d.parent.index)),
				},
				"profile": {
					StringValue: &d.profile,
				},
				"productName": {
					StringValue: &d.parent.productName,
				},
				"brand": {
					StringValue: &d.parent.brand,
				},
				"architecture": {
					StringValue: &d.parent.architecture,
				},
				"cudaComputeCapability": {
					VersionValue: ptr.To(semver.MustParse(d.parent.cudaComputeCapability).String()),
				},
				"driverVersion": {
					VersionValue: ptr.To(semver.MustParse(d.parent.driverVersion).String()),
				},
				"cudaDriverVersion": {
					VersionValue: ptr.To(semver.MustParse(d.parent.cudaDriverVersion).String()),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resource.Quantity{
				"multiprocessors": *resource.NewQuantity(int64(d.giProfileInfo.MultiprocessorCount), resource.BinarySI),
				"copyEngines":     *resource.NewQuantity(int64(d.giProfileInfo.CopyEngineCount), resource.BinarySI),
				"decoders":        *resource.NewQuantity(int64(d.giProfileInfo.DecoderCount), resource.BinarySI),
				"encoders":        *resource.NewQuantity(int64(d.giProfileInfo.EncoderCount), resource.BinarySI),
				"jpegEngines":     *resource.NewQuantity(int64(d.giProfileInfo.JpegCount), resource.BinarySI),
				"ofaEngines":      *resource.NewQuantity(int64(d.giProfileInfo.OfaCount), resource.BinarySI),
				"memory":          *resource.NewQuantity(int64(d.giProfileInfo.MemorySizeMB*1024*1024), resource.BinarySI),
			},
		},
	}
	for i := d.placement.Start; i < d.placement.Start+d.placement.Size; i++ {
		capacity := resourceapi.QualifiedName(fmt.Sprintf("memorySlice%d", i))
		device.Basic.Capacity[capacity] = *resource.NewQuantity(1, resource.BinarySI)
	}
	return device
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

func (s *DeviceState) Prepare(ctx context.Context, claim *resourceapi.ResourceClaim) ([]*drapbv1.Device, error) {
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

	preparedDevices, err := s.prepareDevices(ctx, claim)
	if err != nil {
		return nil, fmt.Errorf("prepare devices failed: %w", err)
	}

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

	if err := s.unprepareDevices(ctx, claimUID, preparedClaims[claimUID]); err != nil {
		return fmt.Errorf("unprepare devices failed: %w", err)
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

func (s *DeviceState) prepareDevices(ctx context.Context, claim *resourceapi.ResourceClaim) (PreparedDevices, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim not yet allocated")
	}

	// Track the set of preparedDevice groups. For now there is just one group
	// to hold all prepared devices. In the future, each group will represent
	// the set of devices that have the same configuration applied to them.
	//
	// TODO: Apply sharing settings via opaque device configs.
	preparedDevices := PreparedDevices{
		&PreparedDeviceGroup{},
	}

	for _, result := range claim.Status.Allocation.Devices.Results {
		if _, exists := s.allocatable[result.Device]; !exists {
			return nil, fmt.Errorf("requested GPU is not allocatable: %v", result.Device)
		}

		cdiDevices := s.cdi.GetStandardDevices([]string{result.Device})
		cdiDevices = append(cdiDevices, s.cdi.GetClaimDevice(string(claim.UID), result.Device))

		device := &drapbv1.Device{
			RequestNames: []string{result.Request},
			PoolName:     result.Pool,
			DeviceName:   result.Device,
			CDIDeviceIDs: cdiDevices,
		}

		var preparedDevice PreparedDevice
		switch s.allocatable[result.Device].Type() {
		case types.GpuDeviceType:
			preparedDevice.Gpu = &PreparedGpu{
				Info:   s.allocatable[result.Device].Gpu,
				Device: device,
			}
		case types.MigDeviceType:
			preparedDevice.Mig = &PreparedMigDevice{
				Info:   s.allocatable[result.Device].Mig,
				Device: device,
			}
		}

		preparedDevices[0].Devices = append(preparedDevices[0].Devices, &preparedDevice)
	}

	return preparedDevices, nil
}

func (s *DeviceState) unprepareDevices(ctx context.Context, claimUID string, devices PreparedDevices) error {
	for _, group := range devices {
		if group.MpsControlDaemon != nil {
			if err := group.MpsControlDaemon.Stop(ctx); err != nil {
				return fmt.Errorf("error stopping MPS control daemon: %w", err)
			}
		}
		if err := s.tsManager.SetTimeSlice(group, nil); err != nil {
			return fmt.Errorf("error setting timeslice for devices: %w", err)
		}
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
