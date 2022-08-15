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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
)

type ClaimAllocations map[string]sets.String

type GpuInfo struct {
	uuid       string
	minor      int
	name       string
	migEnabled bool
	migs       []MigProfileInfo
}

type MigProfileInfo struct {
	profile *MigProfile
	count   int
}

func (g GpuInfo) CDIDevice() string {
	return fmt.Sprintf("%s=gpu%d", cdiKind, g.minor)
}

type DeviceState struct {
	sync.Mutex
	cdi       cdiapi.Registry
	devices   map[string]*GpuInfo
	available sets.String
	allocated ClaimAllocations
}

func tryNvmlShutdown() {
	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		klog.Warningf("error shutting down NVML: %v", nvml.ErrorString(ret))
	}
}

func NewDeviceState(config *Config, nascrd *nvcrd.NodeAllocationState) (*DeviceState, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device count: %v", nvml.ErrorString(ret))
	}

	uuids := sets.NewString()
	devices := make(map[string]*GpuInfo)
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

		devices[uuid] = gpuInfo
		uuids.Insert(uuid)

		if !gpuInfo.migEnabled {
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
				return nil, fmt.Errorf("error retrieving GPUInstanceProfileInfo for profile %d on GPU %v", j, i)
			}
			profileInfo := MigProfileInfo{
				profile: NewMigProfile(giProfileInfo.SliceCount, giProfileInfo.SliceCount, GetMigProfileAttributes(j), giProfileInfo.MemorySizeMB, memory.Total),
				count:   int(giProfileInfo.InstanceCount),
			}
			gpuInfo.migs = append(gpuInfo.migs, profileInfo)
		}
	}

	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)

	err := cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	state := &DeviceState{
		cdi:       cdi,
		devices:   devices,
		available: sets.NewString().Union(uuids),
		allocated: make(ClaimAllocations),
	}

	state.allocated.From(nascrd.Spec.ClaimAllocations)
	for claimUid := range state.allocated {
		state.available = state.available.Difference(state.allocated[claimUid])
	}

	return state, nil
}

func (s *DeviceState) Allocate(claimUid string, requirements nvcrd.DeviceRequirements) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] != nil {
		return s.getAllocatedAsCDIDevices(claimUid), nil
	}

	if requirements.Type() != nvcrd.GpuDeviceType {
		return nil, fmt.Errorf("unsupported device type: %v", requirements.Type)
	}

	if s.available.Len() < requirements.Gpu.Count {
		return nil, fmt.Errorf("unable to satisfy allocation (available: %v)", s.available.Len())
	}

	s.allocated[claimUid] = sets.NewString(s.available.List()[:requirements.Gpu.Count]...)
	s.available = s.available.Difference(s.allocated[claimUid])
	return s.getAllocatedAsCDIDevices(claimUid), nil
}

func (s *DeviceState) getAllocatedAsCDIDevices(claimUid string) []string {
	var devs []string
	for _, uuid := range s.allocated[claimUid].List() {
		devs = append(devs, s.cdi.DeviceDB().GetDevice(s.devices[uuid].CDIDevice()).GetQualifiedName())
	}
	return devs
}

func (s *DeviceState) Free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	s.available = s.available.Union(s.allocated[claimUid])
	delete(s.allocated, claimUid)
	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nvcrd.NodeAllocationStateSpec) *nvcrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	gpus := make(map[string]nvcrd.AllocatableDevice)
	migs := make(map[string]nvcrd.AllocatableDevice)
	for _, device := range s.devices {
		if _, exists := gpus[device.name]; !exists {
			gpus[device.name] = nvcrd.AllocatableDevice{
				Gpu: &nvcrd.AllocatableGpu{
					Name:  device.name,
					Count: 0,
				},
			}
		}

		if !device.migEnabled {
			gpus[device.name].Gpu.Count += 1
			continue
		}

		for _, mig := range device.migs {
			if _, exists := migs[mig.profile.String()]; !exists {
				migs[mig.profile.String()] = nvcrd.AllocatableDevice{
					Mig: &nvcrd.AllocatableMigDevice{
						Profile: mig.profile.String(),
						Count:   0,
						Slices:  mig.profile.G,
					},
				}
			}
			migs[mig.profile.String()].Mig.Count += mig.count
		}
	}

	allocatable := []nvcrd.AllocatableDevice{}
	for _, device := range gpus {
		if device.Gpu.Count > 0 {
			allocatable = append(allocatable, device)
		}
	}
	for _, device := range migs {
		if device.Mig.Count > 0 {
			allocatable = append(allocatable, device)
		}
	}

	outspec := inspec.DeepCopy()
	outspec.AllocatableDevices = allocatable
	outspec.ClaimAllocations = s.allocated.To(s.devices)

	return outspec
}

func (cas ClaimAllocations) From(incas map[string][]nvcrd.AllocatedDevice) {
	outcas := make(ClaimAllocations)
	for claim, devices := range incas {
		outcas[claim] = sets.NewString()
		for _, d := range devices {
			if d.Type() != nvcrd.GpuDeviceType {
				continue
			}
			outcas[claim].Insert(d.Gpu.CDIDeviceName)
		}
	}
	for claim := range cas {
		delete(cas, claim)
	}
	for claim := range outcas {
		cas[claim] = outcas[claim]
	}
}

func (cas ClaimAllocations) To(info map[string]*GpuInfo) map[string][]nvcrd.AllocatedDevice {
	outcas := make(map[string][]nvcrd.AllocatedDevice)
	for claim, devices := range cas {
		outcas[claim] = []nvcrd.AllocatedDevice{}
		for _, uuid := range devices.List() {
			device := nvcrd.AllocatedDevice{
				Gpu: &nvcrd.AllocatedGpu{
					UUID:          uuid,
					Name:          info[uuid].name,
					CDIDeviceName: info[uuid].CDIDevice(),
				},
			}
			outcas[claim] = append(outcas[claim], device)
		}
	}
	return outcas
}

func (cas ClaimAllocations) DeepCopy() ClaimAllocations {
	newcas := make(ClaimAllocations)
	for claim, devices := range cas {
		newcas[claim] = sets.NewString().Union(devices)
	}
	return newcas
}
