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
	"slices"
	"sync"

	"github.com/Masterminds/semver"
	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	resourceapi "k8s.io/api/resource/v1alpha3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	configapi "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
)

type AllocatableDevices map[string]*AllocatableDevice
type PreparedDevices []*PreparedDeviceGroup
type PreparedClaims map[string]PreparedDevices

type OpaqueDeviceConfig struct {
	Requests []string
	Config   runtime.Object
}

type DeviceConfigState struct {
	MpsControlDaemonID string `json:"mpsControlDaemonID"`
	containerEdits     *cdiapi.ContainerEdits
}

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
	Devices     []*PreparedDevice `json:"devices"`
	ConfigState DeviceConfigState `json:"configState"`
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
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

func (d PreparedDevice) Type() string {
	if d.Gpu != nil {
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

func (d AllocatableDevices) GpuUUIDs() []string {
	var uuids []string
	for _, device := range d {
		if device.Type() == GpuDeviceType {
			uuids = append(uuids, device.Gpu.UUID)
		}
	}
	return uuids
}

func (d AllocatableDevices) MigDeviceUUIDs() []string {
	var uuids []string
	for _, device := range d {
		if device.Type() == MigDeviceType {
			uuids = append(uuids, device.Mig.UUID)
		}
	}
	return uuids
}

func (d AllocatableDevices) UUIDs() []string {
	return append(d.GpuUUIDs(), d.MigDeviceUUIDs()...)
}

func (d *PreparedDeviceGroup) GpuUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == GpuDeviceType {
			uuids = append(uuids, device.Gpu.Info.UUID)
		}
	}
	return uuids
}

func (d *PreparedDeviceGroup) MigDeviceUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == MigDeviceType {
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
		case GpuDeviceType:
			devices = append(devices, device.Gpu.Device)
		case MigDeviceType:
			devices = append(devices, device.Mig.Device)
		}
	}
	return devices
}

func (d *AllocatableDevice) GetDevice() resourceapi.Device {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.GetDevice()
	case MigDeviceType:
		return d.Mig.GetDevice()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *AllocatableDevice) CanonicalName() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.CanonicalName()
	case MigDeviceType:
		return d.Mig.CanonicalName()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *AllocatableDevice) CanonicalIndex() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.CanonicalIndex()
	case MigDeviceType:
		return d.Mig.CanonicalIndex()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *PreparedDevice) CanonicalName() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.Info.CanonicalName()
	case MigDeviceType:
		return d.Mig.Info.CanonicalName()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *PreparedDevice) CanonicalIndex() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.Info.CanonicalIndex()
	case MigDeviceType:
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
					StringValue: ptr.To(GpuDeviceType),
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
					StringValue: ptr.To(MigDeviceType),
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

	// Retrieve the full set of device configs for the driver.
	configs, err := GetOpaqueDeviceConfigs(
		configapi.Decoder,
		DriverName,
		claim.Status.Allocation.Devices.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting opaque device configs: %v", err)
	}

	// Add the default GPU and MIG device Configs to the front of the config
	// list with the lowest precedence. This guarantees there will be at least
	// one of each config in the list with len(Requests) == 0 for the lookup below.
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{
		Requests: []string{},
		Config:   configapi.DefaultGpuConfig(),
	})
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{
		Requests: []string{},
		Config:   configapi.DefaultMigDeviceConfig(),
	})

	// Look through the configs and figure out which one will be applied to
	// each device allocation result based on their order of precedence and type.
	configResultsMap := make(map[runtime.Object][]*resourceapi.DeviceRequestAllocationResult)
	for _, result := range claim.Status.Allocation.Devices.Results {
		device, exists := s.allocatable[result.Device]
		if !exists {
			return nil, fmt.Errorf("requested device is not allocatable: %v", result.Device)
		}
		for _, c := range slices.Backward(configs) {
			if slices.Contains(c.Requests, result.Request) {
				if _, ok := c.Config.(*configapi.GpuConfig); ok && device.Type() != GpuDeviceType {
					return nil, fmt.Errorf("cannot apply GPU config to MIG device from request: %v", result.Request)
				}
				if _, ok := c.Config.(*configapi.MigDeviceConfig); ok && device.Type() == MigDeviceType {
					return nil, fmt.Errorf("cannot apply MIG device config to GPU from request: %v", result.Request)
				}
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
			if len(c.Requests) == 0 {
				if _, ok := c.Config.(*configapi.GpuConfig); ok && device.Type() != GpuDeviceType {
					continue
				}
				if _, ok := c.Config.(*configapi.MigDeviceConfig); ok && device.Type() == MigDeviceType {
					continue
				}
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
		}
	}

	// Normalize, validate, and apply all configs associated with devices that
	// need to be prepared. Track device group configs generated from applying the
	// config to the set of device allocation results.
	preparedDeviceGroupConfigState := make(map[runtime.Object]*DeviceConfigState)
	for c, results := range configResultsMap {
		// Cast the opaque config to a configapi.Interface type
		var config configapi.Interface
		switch castConfig := c.(type) {
		case *configapi.GpuConfig:
			config = castConfig
		case *configapi.MigDeviceConfig:
			config = castConfig
		default:
			return nil, fmt.Errorf("runtime object is not a recognized configuration")
		}

		// Normalize the config to set any implied defaults.
		if err := config.Normalize(); err != nil {
			return nil, fmt.Errorf("error normalizing GPU config: %w", err)
		}

		// Validate the config to ensure its integrity.
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("error validating GPU config: %w", err)
		}

		// Apply the config to the list of results associated with it.
		configState, err := s.applyConfig(ctx, config, claim, results)
		if err != nil {
			return nil, fmt.Errorf("error applying GPU config: %w", err)
		}

		// Capture the prepared device group config in the map.
		preparedDeviceGroupConfigState[c] = configState
	}

	// Walk through each config and its associated device allocation results
	// and construct the list of prepared devices to return.
	var preparedDevices PreparedDevices
	for c, results := range configResultsMap {
		preparedDeviceGroup := PreparedDeviceGroup{
			ConfigState: *preparedDeviceGroupConfigState[c],
		}

		for _, result := range results {
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
			case GpuDeviceType:
				preparedDevice.Gpu = &PreparedGpu{
					Info:   s.allocatable[result.Device].Gpu,
					Device: device,
				}
			case MigDeviceType:
				preparedDevice.Mig = &PreparedMigDevice{
					Info:   s.allocatable[result.Device].Mig,
					Device: device,
				}
			}

			preparedDeviceGroup.Devices = append(preparedDeviceGroup.Devices, &preparedDevice)
		}

		preparedDevices = append(preparedDevices, &preparedDeviceGroup)
	}
	return preparedDevices, nil
}

func (s *DeviceState) unprepareDevices(ctx context.Context, claimUID string, devices PreparedDevices) error {
	for _, group := range devices {
		// Stop any MPS control daemons started for each group of prepared devices.
		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(claimUID, group)
		if err := mpsControlDaemon.Stop(ctx); err != nil {
			return fmt.Errorf("error stopping MPS control daemon: %w", err)
		}

		// Go back to default time-slicing for all full GPUs.
		tsc := configapi.DefaultGpuConfig().Sharing.TimeSlicingConfig
		if err := s.tsManager.SetTimeSlice(group, tsc); err != nil {
			return fmt.Errorf("error setting timeslice for devices: %w", err)
		}
	}
	return nil
}

func (s *DeviceState) applyConfig(ctx context.Context, config configapi.Interface, claim *resourceapi.ResourceClaim, results []*resourceapi.DeviceRequestAllocationResult) (*DeviceConfigState, error) {
	// Get the sharing interface from the config interface based on its type.
	var sharing configapi.Sharing
	switch castConfig := config.(type) {
	case *configapi.GpuConfig:
		sharing = castConfig.Sharing
	case *configapi.MigDeviceConfig:
		sharing = castConfig.Sharing
	}

	// Get the list of claim requests this config is being applied over.
	var requests []string
	for _, r := range results {
		requests = append(requests, r.Request)
	}

	// Get the list of allocatable devices this config is being applied over.
	allocatableDevices := make(AllocatableDevices)
	for _, r := range results {
		allocatableDevices[r.Device] = s.allocatable[r.Device]
	}

	// Declare a device group atate object to populate.
	var configState DeviceConfigState

	// Apply time-slicing settings.
	if sharing.IsTimeSlicing() {
		tsc, err := sharing.GetTimeSlicingConfig()
		if err != nil {
			return nil, fmt.Errorf("error getting timeslice config for requests '%v' in claim '%v': %w", requests, claim.UID, err)
		}
		err = s.tsManager.SetTimeSlice(allocatableDevices, tsc)
		if err != nil {
			return nil, fmt.Errorf("error setting timeslice config for requests '%v' in claim '%v': %w", requests, claim.UID, err)
		}
	}

	// Apply MPS settings.
	if sharing.IsMps() {
		mpsc, err := sharing.GetMpsConfig()
		if err != nil {
			return nil, fmt.Errorf("error getting MPS configuration: %w", err)
		}
		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(string(claim.UID), allocatableDevices)
		if err := mpsControlDaemon.Start(ctx, mpsc); err != nil {
			return nil, fmt.Errorf("error starting MPS control daemon: %w", err)
		}
		if err := mpsControlDaemon.AssertReady(ctx); err != nil {
			return nil, fmt.Errorf("MPS control daemon is not yet ready: %w", err)
		}
		configState.MpsControlDaemonID = mpsControlDaemon.GetID()
		configState.containerEdits = mpsControlDaemon.GetCDIContainerEdits()
	}

	return &configState, nil
}

// GetOpaqueDeviceConfigs returns an ordered list of the configs contained in possibleConfigs for this driver.
//
// Configs can either come from the resource claim itself or from the device
// class associated with the request. Configs coming directly from the resource
// claim take precedence over configs coming from the device class. Moreover,
// configs found later in the list of configs attached to its source take
// precedence over configs found earlier in the list for that source.
//
// All of the configs relevant to the driver from the list of possibleConfigs
// will be returned in order of precedence (from lowest to highest). If no
// configs are found, nil is returned.
func GetOpaqueDeviceConfigs(
	decoder runtime.Decoder,
	driverName string,
	possibleConfigs []resourceapi.DeviceAllocationConfiguration,
) ([]*OpaqueDeviceConfig, error) {
	// Collect all configs in order of reverse precedence.
	var classConfigs []resourceapi.DeviceAllocationConfiguration
	var claimConfigs []resourceapi.DeviceAllocationConfiguration
	var candidateConfigs []resourceapi.DeviceAllocationConfiguration
	for _, config := range possibleConfigs {
		switch config.Source {
		case resourceapi.AllocationConfigSourceClass:
			classConfigs = append(classConfigs, config)
		case resourceapi.AllocationConfigSourceClaim:
			claimConfigs = append(claimConfigs, config)
		default:
			return nil, fmt.Errorf("invalid config source: %v", config.Source)
		}
	}
	candidateConfigs = append(candidateConfigs, classConfigs...)
	candidateConfigs = append(candidateConfigs, claimConfigs...)

	// Decode all configs that are relevant for the driver.
	var resultConfigs []*OpaqueDeviceConfig
	for _, config := range candidateConfigs {
		// If this is nil, the driver doesn't support some future API extension
		// and needs to be updated.
		if config.DeviceConfiguration.Opaque == nil {
			return nil, fmt.Errorf("only opaque parameters are supported by this driver")
		}

		// Configs for different drivers may have been specified because a
		// single request can be satisfied by different drivers. This is not
		// an error -- drivers must skip over other driver's configs in order
		// to support this.
		if config.DeviceConfiguration.Opaque.Driver != driverName {
			continue
		}

		decodedConfig, err := runtime.Decode(decoder, config.DeviceConfiguration.Opaque.Parameters.Raw)
		if err != nil {
			return nil, fmt.Errorf("error decoding config parameters: %w", err)
		}

		resultConfig := &OpaqueDeviceConfig{
			Requests: config.Requests,
			Config:   decodedConfig,
		}

		resultConfigs = append(resultConfigs, resultConfig)
	}

	return resultConfigs, nil
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
