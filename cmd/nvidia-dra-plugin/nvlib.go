/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"

	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type deviceLib struct {
	nvdev.Interface
	nvmllib       nvml.Interface
	nvidiaSMIPath string
}

func newDeviceLib(driverRoot root) (*deviceLib, error) {
	driverLibraryPath, err := driverRoot.getDriverLibraryPath()
	if err != nil {
		return nil, fmt.Errorf("failed to locate driver libraries: %w", err)
	}

	nvidiaSMIPath, err := driverRoot.getNvidiaSMIPath()
	if err != nil {
		return nil, fmt.Errorf("failed to locate nvidia-smi: %w", err)
	}

	// In order for nvidia-smi to run, we need to set the PATH to the parent of
	// the nvidia-smi executable and update LD_PRELOAD to include the path to
	// libnvidia-ml.so.1
	updatePathListEnvvar("LD_PRELOAD", driverLibraryPath)
	updatePathListEnvvar("PATH", filepath.Dir(nvidiaSMIPath))

	// We construct an NVML library specifying the path to libnvidia-ml.so.1
	// explicitly so that we don't have to rely on the library path.
	nvmllib := nvml.New(
		nvml.WithLibraryPath(driverLibraryPath),
	)
	d := deviceLib{
		Interface:     nvdev.New(nvdev.WithNvml(nvmllib)),
		nvmllib:       nvmllib,
		nvidiaSMIPath: nvidiaSMIPath,
	}
	return &d, nil
}

// updatePathListEnvvar prepends a specified list of strings to a specified envvar.
func updatePathListEnvvar(envvar string, prepend ...string) {
	if len(prepend) == 0 {
		return
	}
	current := filepath.SplitList(os.Getenv(envvar))
	os.Setenv(envvar, strings.Join(append(prepend, current...), string(filepath.ListSeparator)))
}

func (l deviceLib) Init() error {
	ret := l.nvmllib.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error initializing NVML: %v", ret)
	}
	return nil
}

func (l deviceLib) alwaysShutdown() {
	ret := l.nvmllib.Shutdown()
	if ret != nvml.SUCCESS {
		klog.Warningf("error shutting down NVML: %v", ret)
	}
}

func (l deviceLib) enumerateAllPossibleDevices() (AllocatableDevices, error) {
	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	alldevices := make(AllocatableDevices)
	err := l.VisitDevices(func(i int, d nvdev.Device) error {
		gpuInfo, err := l.getGpuInfo(i, d)
		if err != nil {
			return fmt.Errorf("error getting info for GPU %d: %w", i, err)
		}

		migProfileInfos, err := l.getMigProfileInfos(gpuInfo)
		if err != nil {
			return fmt.Errorf("error getting MIG profile info for GPU %v: %w", i, err)
		}

		deviceInfo := &AllocatableDeviceInfo{
			GpuInfo:     gpuInfo,
			migProfiles: migProfileInfos,
		}

		alldevices[gpuInfo.uuid] = deviceInfo

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error visiting devices: %w", err)
	}

	return alldevices, nil
}

func (l deviceLib) getGpuInfo(index int, device nvdev.Device) (*GpuInfo, error) {
	minor, ret := device.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting minor number for device %d: %v", index, ret)
	}
	uuid, ret := device.GetUUID()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting UUID for device %d: %v", index, ret)
	}
	migEnabled, err := device.IsMigEnabled()
	if err != nil {
		return nil, fmt.Errorf("error checking if MIG mode enabled for device %d: %w", index, err)
	}
	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting memory info for device %d: %v", index, ret)
	}
	productName, ret := device.GetName()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting product name for device %d: %v", index, ret)
	}
	architecture, err := device.GetArchitectureAsString()
	if err != nil {
		return nil, fmt.Errorf("error getting architecture for device %d: %w", index, err)
	}
	brand, err := device.GetBrandAsString()
	if err != nil {
		return nil, fmt.Errorf("error getting brand for device %d: %w", index, err)
	}
	cudaComputeCapability, err := device.GetCudaComputeCapabilityAsString()
	if err != nil {
		return nil, fmt.Errorf("error getting CUDA compute capability for device %d: %w", index, err)
	}
	driverVersion, ret := l.nvmllib.SystemGetDriverVersion()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting driver version: %w", err)
	}
	cudaDriverVersion, ret := l.nvmllib.SystemGetCudaDriverVersion()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting CUDA driver version: %w", err)
	}

	gpuInfo := &GpuInfo{
		minor:                 minor,
		index:                 index,
		uuid:                  uuid,
		migEnabled:            migEnabled,
		memoryBytes:           memory.Total,
		productName:           productName,
		brand:                 brand,
		architecture:          architecture,
		cudaComputeCapability: cudaComputeCapability,
		driverVersion:         driverVersion,
		cudaDriverVersion:     fmt.Sprintf("%v.%v", cudaDriverVersion/1000, (cudaDriverVersion%1000)/10),
	}

	return gpuInfo, nil
}

func (l deviceLib) getMigProfileInfos(gpuInfo *GpuInfo) (map[string]*MigProfileInfo, error) {
	if !gpuInfo.migEnabled {
		return nil, nil
	}

	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpuInfo.uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting handle for device %v: %v", gpuInfo.uuid, ret)
	}

	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting memory info for device %v: %v", gpuInfo.uuid, ret)
	}

	migProfiles := make(map[string]*MigProfileInfo)
	for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
		giProfileInfo, ret := device.GetGpuInstanceProfileInfo(i)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error retrieving GpuInstanceProfileInfo for profile %d on GPU %v", i, gpuInfo.uuid)
		}
		giPossiblePlacements, ret := device.GetGpuInstancePossiblePlacements(&giProfileInfo)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error retrieving GpuInstancePossiblePlacements for profile %d on GPU %v", i, gpuInfo.uuid)
		}
		var migDevicePlacements []*MigDevicePlacement
		for _, p := range giPossiblePlacements {
			mdp := &MigDevicePlacement{
				GpuInstancePlacement: p,
				blockedBy:            0,
			}
			migDevicePlacements = append(migDevicePlacements, mdp)
		}
		migProfile, err := l.NewMigProfile(i, i, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED, giProfileInfo.MemorySizeMB, memory.Total)
		if err != nil {
			return nil, fmt.Errorf("error building MIG profile from GpuInstanceProfileInfo for profile %d on GPU %v", i, gpuInfo.uuid)
		}
		profileInfo := &MigProfileInfo{
			profile:    migProfile,
			placements: migDevicePlacements,
		}
		migProfiles[profileInfo.profile.String()] = profileInfo
	}

	return migProfiles, nil
}

func (l deviceLib) getMigProfiles(gpuInfo *GpuInfo) (map[int]nvdev.MigProfile, error) {
	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpuInfo.uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
	}

	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting memory info for device %v: %v", gpuInfo.uuid, ret)
	}

	migProfiles := make(map[int]nvdev.MigProfile)
	for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
		giProfileInfo, ret := device.GetGpuInstanceProfileInfo(i)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error retrieving GpuInstanceProfileInfo for profile %d on GPU %v", i, gpuInfo.uuid)
		}
		migProfile, err := l.NewMigProfile(i, i, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED, giProfileInfo.MemorySizeMB, memory.Total)
		if err != nil {
			return nil, fmt.Errorf("error building MIG profile from GpuInstanceProfileInfo for profile %d on GPU %v", i, gpuInfo.uuid)
		}
		migProfiles[int(giProfileInfo.Id)] = migProfile
	}

	return migProfiles, nil
}

func (l deviceLib) getMigDevices(gpuInfo *GpuInfo) (map[string]*MigDeviceInfo, error) {
	if !gpuInfo.migEnabled {
		return nil, nil
	}

	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpuInfo.uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
	}

	migProfiles, err := l.getMigProfiles(gpuInfo)
	if err != nil {
		return nil, fmt.Errorf("error getting GPU instance profile infos for '%v': %w", gpuInfo.uuid, err)
	}

	migInfos := make(map[string]*MigDeviceInfo)
	err = walkMigDevices(device, func(i int, migDevice nvml.Device) error {
		giID, ret := migDevice.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance ID for MIG device: %v", ret)
		}
		gi, ret := device.GetGpuInstanceById(giID)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance for '%v': %v", giID, ret)
		}
		giInfo, ret := gi.GetInfo()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance info for '%v': %v", giID, ret)
		}
		ciID, ret := migDevice.GetComputeInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance ID for MIG device: %v", ret)
		}
		ci, ret := gi.GetComputeInstanceById(ciID)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance for '%v': %v", ciID, ret)
		}
		ciInfo, ret := ci.GetInfo()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance info for '%v': %v", ciID, ret)
		}
		uuid, ret := migDevice.GetUUID()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting UUID for MIG device: %v", ret)
		}
		migInfos[uuid] = &MigDeviceInfo{
			uuid:    uuid,
			parent:  gpuInfo,
			profile: migProfiles[int(giInfo.ProfileId)],
			giInfo:  &giInfo,
			ciInfo:  &ciInfo,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error enumerating MIG devices: %w", err)
	}

	if len(migInfos) == 0 {
		return nil, nil
	}

	return migInfos, nil
}

func (l deviceLib) createMigDevice(gpu *GpuInfo, profile nvdev.MigProfile, placement *nvml.GpuInstancePlacement) (*MigDeviceInfo, error) {
	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	profileInfo := profile.GetInfo()

	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpu.uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
	}

	giProfileInfo, ret := device.GetGpuInstanceProfileInfo(profileInfo.GIProfileID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance profile info for '%v': %v", profile, ret)
	}

	gi, ret := device.CreateGpuInstanceWithPlacement(&giProfileInfo, placement)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error creating GPU instance for '%v': %v", profile, ret)
	}

	giInfo, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, ret)
	}

	ciProfileInfo, ret := gi.GetComputeInstanceProfileInfo(profileInfo.CIProfileID, profileInfo.CIEngProfileID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute instance profile info for '%v': %v", profile, ret)
	}

	ci, ret := gi.CreateComputeInstance(&ciProfileInfo)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error creating Compute instance for '%v': %v", profile, ret)
	}

	ciInfo, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, ret)
	}

	uuid := ""
	err := walkMigDevices(device, func(i int, migDevice nvml.Device) error {
		giID, ret := migDevice.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance ID for MIG device: %v", ret)
		}
		ciID, ret := migDevice.GetComputeInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance ID for MIG device: %v", ret)
		}
		if giID != int(giInfo.Id) || ciID != int(ciInfo.Id) {
			return nil
		}
		uuid, ret = migDevice.GetUUID()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting UUID for MIG device: %v", ret)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error processing MIG device for GI and CI just created: %w", err)
	}
	if uuid == "" {
		return nil, fmt.Errorf("unable to find MIG device for GI and CI just created")
	}

	migInfo := &MigDeviceInfo{
		uuid:    uuid,
		parent:  gpu,
		profile: profile,
		giInfo:  &giInfo,
		ciInfo:  &ciInfo,
	}

	return migInfo, nil
}

func (l deviceLib) deleteMigDevice(mig *MigDeviceInfo) error {
	if err := l.Init(); err != nil {
		return err
	}
	defer l.alwaysShutdown()

	parent, ret := l.nvmllib.DeviceGetHandleByUUID(mig.parent.uuid)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device from UUID '%v': %v", mig.parent.uuid, ret)
	}
	gi, ret := parent.GetGpuInstanceById(int(mig.giInfo.Id))
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting GPU instance ID for MIG device: %v", ret)
	}
	ci, ret := gi.GetComputeInstanceById(int(mig.ciInfo.Id))
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting Compute instance ID for MIG device: %v", ret)
	}
	ret = ci.Destroy()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error destroying Compute Instance: %v", ret)
	}
	ret = gi.Destroy()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error destroying GPU Instance: %v", ret)
	}
	return nil
}

func walkMigDevices(d nvml.Device, f func(i int, d nvml.Device) error) error {
	count, ret := nvml.Device(d).GetMaxMigDeviceCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting max MIG device count: %v", ret)
	}

	for i := 0; i < count; i++ {
		device, ret := d.GetMigDeviceHandleByIndex(i)
		if ret == nvml.ERROR_NOT_FOUND {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting MIG device handle at index '%v': %v", i, ret)
		}
		err := f(i, device)
		if err != nil {
			return err
		}
	}
	return nil
}

func (l deviceLib) setTimeSlice(uuids []string, timeSlice int) error {
	for _, uuid := range uuids {
		cmd := exec.Command(
			"nvidia-smi",
			"compute-policy",
			"-i", uuid,
			"--set-timeslice", fmt.Sprintf("%d", timeSlice))
		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("\n%v", string(output))
			return fmt.Errorf("error running nvidia-smi: %w", err)
		}
	}
	return nil
}

func (l deviceLib) setComputeMode(uuids []string, mode string) error {
	for _, uuid := range uuids {
		cmd := exec.Command(
			"nvidia-smi",
			"-i", uuid,
			"-c", mode)
		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("\n%v", string(output))
			return fmt.Errorf("error running nvidia-smi: %w", err)
		}
	}
	return nil
}
