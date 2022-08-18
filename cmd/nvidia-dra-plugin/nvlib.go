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

	var alldevices UnallocatedDevices
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
			name:       name,
			migEnabled: migMode == nvml.DEVICE_MIG_ENABLE,
		}

		deviceInfo := UnallocatedDeviceInfo{
			GpuInfo:     gpuInfo,
			migProfiles: make(map[string]*MigProfileInfo),
		}

		if !gpuInfo.migEnabled {
			alldevices = append(alldevices, deviceInfo)
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
			profileInfo := &MigProfileInfo{
				profile:            NewMigProfile(j, j, nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_SHARED, giProfileInfo.SliceCount, giProfileInfo.SliceCount, giProfileInfo.MemorySizeMB, memory.Total),
				count:              int(giProfileInfo.InstanceCount),
				possiblePlacements: giPossiblePlacements,
			}
			deviceInfo.migProfiles[profileInfo.profile.String()] = profileInfo
		}

		alldevices = append(alldevices, deviceInfo)
	}

	return alldevices, nil
}

func createMigDevice(gpu *GpuInfo, profile *MigProfile, placement *nvml.GpuInstancePlacement) (*MigDeviceInfo, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	device, ret := nvml.DeviceGetHandleByUUID(gpu.uuid)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle for '%v': %v", profile, nvml.ErrorString(ret))
	}

	giProfileInfo, ret := device.GetGpuInstanceProfileInfo(profile.GIProfileID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU instance profile info for '%v': %v", profile, nvml.ErrorString(ret))
	}

	var gi nvml.GpuInstance
	if placement == nil {
		gi, ret = device.CreateGpuInstance(&giProfileInfo)
	} else {
		gi, ret = device.CreateGpuInstanceWithPlacement(&giProfileInfo, placement)
	}
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

	parent, ret := nvml.DeviceGetHandleByUUID(mig.parent.uuid)
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
