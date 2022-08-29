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

	"k8s.io/klog/v2"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

func tryNvmlShutdown() {
	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		klog.Warningf("error shutting down NVML: %v", nvml.ErrorString(ret))
	}
}

func enumerateAllPossibleDevices() (UnallocatedDevices, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device count: %v", nvml.ErrorString(ret))
	}

	alldevices := make(UnallocatedDevices)
	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting handle for device %d: %v", i, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting UUID for device %d: %v", i, nvml.ErrorString(ret))
		}
		minor, ret := device.GetMinorNumber()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting minor number for device %d: %v", i, nvml.ErrorString(ret))
		}
		name, ret := device.GetName()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting name for device %d: %v", i, nvml.ErrorString(ret))
		}
		migMode, _, ret := device.GetMigMode()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting MIG mode for device %d: %v", i, nvml.ErrorString(ret))
		}

		gpuInfo := &GpuInfo{
			uuid:       uuid,
			minor:      minor,
			model:      name,
			migEnabled: migMode == nvml.DEVICE_MIG_ENABLE,
		}

		deviceInfo := UnallocatedDeviceInfo{
			GpuInfo:     gpuInfo,
			migProfiles: make(map[string]*MigProfileInfo),
		}

		if !gpuInfo.migEnabled {
			alldevices[uuid] = deviceInfo
			continue
		}

		memory, ret := device.GetMemoryInfo()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting memory info for device %d: %v", i, nvml.ErrorString(ret))
		}

		for j := 0; j < nvml.GPU_INSTANCE_PROFILE_COUNT; j++ {
			giProfileInfo, ret := device.GetGpuInstanceProfileInfo(j)
			if ret == nvml.ERROR_NOT_SUPPORTED {
				continue
			}
			if ret == nvml.ERROR_INVALID_ARGUMENT {
				continue
			}
			if ret != nvml.SUCCESS {
				return nil, fmt.Errorf("error retrieving GpuInstanceProfileInfo for profile %d on GPU %v", j, i)
			}
			giPossiblePlacements, ret := device.GetGpuInstancePossiblePlacements(&giProfileInfo)
			if ret == nvml.ERROR_NOT_SUPPORTED {
				continue
			}
			if ret == nvml.ERROR_INVALID_ARGUMENT {
				continue
			}
			if ret != nvml.SUCCESS {
				return nil, fmt.Errorf("error retrieving GpuInstancePossiblePlacements for profile %d on GPU %v", j, i)
			}
			var migDevicePlacements []*MigDevicePlacement
			for _, p := range giPossiblePlacements {
				mdp := &MigDevicePlacement{
					GpuInstancePlacement: p,
					blockedBy:            0,
				}
				migDevicePlacements = append(migDevicePlacements, mdp)
			}
			profileInfo := &MigProfileInfo{
				profile:    NewMigProfile(j, j, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED, giProfileInfo.SliceCount, giProfileInfo.SliceCount, giProfileInfo.MemorySizeMB, memory.Total),
				available:  len(migDevicePlacements),
				placements: migDevicePlacements,
			}
			deviceInfo.migProfiles[profileInfo.profile.String()] = profileInfo
		}

		alldevices[uuid] = deviceInfo
	}

	return alldevices, nil
}

func getGpus() (map[string]*GpuInfo, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device count: %v", nvml.ErrorString(ret))
	}

	gpuInfos := make(map[string]*GpuInfo)
	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting handle for device %d: %v", i, nvml.ErrorString(ret))
		}
		uuid, ret := device.GetUUID()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting UUID for device %d: %v", i, nvml.ErrorString(ret))
		}
		minor, ret := device.GetMinorNumber()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting minor number for device %d: %v", i, nvml.ErrorString(ret))
		}
		name, ret := device.GetName()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting name for device %d: %v", i, nvml.ErrorString(ret))
		}
		migMode, _, ret := device.GetMigMode()
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting MIG mode for device %d: %v", i, nvml.ErrorString(ret))
		}

		gpuInfo := &GpuInfo{
			uuid:       uuid,
			minor:      minor,
			model:      name,
			migEnabled: migMode == nvml.DEVICE_MIG_ENABLE,
		}

		gpuInfos[uuid] = gpuInfo
	}

	return gpuInfos, nil
}

func getGpu(uuid string) (*GpuInfo, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	device, ret := nvml.DeviceGetHandleByUUID(uuid + string(rune(0)))
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting handle for device %d: %v", uuid, nvml.ErrorString(ret))
	}
	minor, ret := device.GetMinorNumber()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting minor number for device %d: %v", uuid, nvml.ErrorString(ret))
	}
	name, ret := device.GetName()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting name for device %d: %v", uuid, nvml.ErrorString(ret))
	}
	migMode, _, ret := device.GetMigMode()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting MIG mode for device %d: %v", uuid, nvml.ErrorString(ret))
	}

	gpuInfo := &GpuInfo{
		uuid:       uuid,
		minor:      minor,
		model:      name,
		migEnabled: migMode == nvml.DEVICE_MIG_ENABLE,
	}

	return gpuInfo, nil
}

func getMigProfiles(gpuInfo *GpuInfo) (map[int]*MigProfile, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	device, ret := nvml.DeviceGetHandleByUUID(gpuInfo.uuid + string(rune(0)))
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", nvml.ErrorString(ret))
	}

	memory, ret := device.GetMemoryInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting memory info for device %d: %v", gpuInfo.uuid, nvml.ErrorString(ret))
	}

	migProfiles := make(map[int]*MigProfile)
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
		migProfiles[int(giProfileInfo.Id)] = NewMigProfile(i, i, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED, giProfileInfo.SliceCount, giProfileInfo.SliceCount, giProfileInfo.MemorySizeMB, memory.Total)
	}

	return migProfiles, nil
}

func getMigDevices(gpuInfo *GpuInfo) (map[string]*MigDeviceInfo, error) {
	if !gpuInfo.migEnabled {
		return nil, nil
	}

	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	device, ret := nvml.DeviceGetHandleByUUID(gpuInfo.uuid + string(rune(0)))
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", nvml.ErrorString(ret))
	}

	migProfiles, err := getMigProfiles(gpuInfo)
	if err != nil {
		return nil, fmt.Errorf("error getting GPU instance profile infos for '%v': %v", gpuInfo.uuid, err)
	}

	migInfos := make(map[string]*MigDeviceInfo)
	err = walkMigDevices(device, func(i int, migDevice nvml.Device) error {
		giID, ret := migDevice.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance ID for MIG device: %v", nvml.ErrorString(ret))
		}
		gi, ret := device.GetGpuInstanceById(giID)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance for '%v': %v", giID, nvml.ErrorString(ret))
		}
		giInfo, ret := gi.GetInfo()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance info for '%v': %v", giID, nvml.ErrorString(ret))
		}
		ciID, ret := migDevice.GetComputeInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance ID for MIG device: %v", nvml.ErrorString(ret))
		}
		ci, ret := gi.GetComputeInstanceById(ciID)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance for '%v': %v", ciID, nvml.ErrorString(ret))
		}
		ciInfo, ret := ci.GetInfo()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance info for '%v': %v", ciID, nvml.ErrorString(ret))
		}
		uuid, ret := migDevice.GetUUID()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting UUID for MIG device: %v", nvml.ErrorString(ret))
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
		return nil, fmt.Errorf("error enumerating MIG devices: %v", err)
	}

	if len(migInfos) == 0 {
		return nil, nil
	}

	return migInfos, nil
}

func createMigDevice(gpu *GpuInfo, profile *MigProfile, placement *nvml.GpuInstancePlacement) (*MigDeviceInfo, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	device, ret := nvml.DeviceGetHandleByUUID(gpu.uuid + string(rune(0)))
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", nvml.ErrorString(ret))
	}

	giProfileInfo, ret := device.GetGpuInstanceProfileInfo(profile.GIProfileID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance profile info for '%v': %v", profile, nvml.ErrorString(ret))
	}

	gi, ret := device.CreateGpuInstanceWithPlacement(&giProfileInfo, placement)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error creating GPU instance for '%v': %v", profile, nvml.ErrorString(ret))
	}

	giInfo, ret := gi.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, nvml.ErrorString(ret))
	}

	ciProfileInfo, ret := gi.GetComputeInstanceProfileInfo(profile.CIProfileID, profile.CIEngProfileID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting Compute instance profile info for '%v': %v", profile, nvml.ErrorString(ret))
	}

	ci, ret := gi.CreateComputeInstance(&ciProfileInfo)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error creating Compute instance for '%v': %v, %+v", profile, nvml.ErrorString(ret), ciProfileInfo)
	}

	ciInfo, ret := ci.GetInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, nvml.ErrorString(ret))
	}

	uuid := ""
	err := walkMigDevices(device, func(i int, migDevice nvml.Device) error {
		giID, ret := migDevice.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting GPU instance ID for MIG device: %v", nvml.ErrorString(ret))
		}
		ciID, ret := migDevice.GetComputeInstanceId()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting Compute instance ID for MIG device: %v", nvml.ErrorString(ret))
		}
		if giID != int(giInfo.Id) || ciID != int(ciInfo.Id) {
			return nil
		}
		uuid, ret = migDevice.GetUUID()
		if ret != nvml.SUCCESS {
			return fmt.Errorf("error getting UUID for MIG device: %v", nvml.ErrorString(ret))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error processing MIG device for GI and CI just created: %v", nvml.ErrorString(ret))
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

func deleteMigDevice(mig *MigDeviceInfo) error {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	parent, ret := nvml.DeviceGetHandleByUUID(mig.parent.uuid + string(rune(0)))
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting device from UUID '%v': %v", mig.parent.uuid, nvml.ErrorString(ret))
	}
	gi, ret := parent.GetGpuInstanceById(int(mig.giInfo.Id))
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting GPU instance ID for MIG device: %v", nvml.ErrorString(ret))
	}
	ci, ret := gi.GetComputeInstanceById(int(mig.ciInfo.Id))
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting Compute instance ID for MIG device: %v", nvml.ErrorString(ret))
	}
	ret = ci.Destroy()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error destroying Compute Instance: %v", nvml.ErrorString(ret))
	}
	ret = gi.Destroy()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error destroying GPU Instance: %v", nvml.ErrorString(ret))
	}
	return nil
}

func walkMigDevices(d nvml.Device, f func(i int, d nvml.Device) error) error {
	count, ret := nvml.Device(d).GetMaxMigDeviceCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("error getting max MIG device count: %v", nvml.ErrorString(ret))
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
			return fmt.Errorf("error getting MIG device handle at index '%v': %v", i, nvml.ErrorString(ret))
		}
		err := f(i, device)
		if err != nil {
			return err
		}
	}
	return nil
}
