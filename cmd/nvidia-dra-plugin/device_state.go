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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
)

type UnallocatedDevices []UnallocatedDeviceInfo
type AllocatedDevices map[string]AllocatedDeviceInfo
type ClaimAllocations map[string]AllocatedDevices

type GpuInfo struct {
	uuid       string
	name       string
	minor      int
	migEnabled bool
}

func (g GpuInfo) CDIDevice() string {
	return fmt.Sprintf("%s=gpu%d", cdiKind, g.minor)
}

type MigDeviceInfo struct {
	uuid    string
	parent  *GpuInfo
	profile *MigProfile
	giInfo  *nvml.GpuInstanceInfo
	ciInfo  *nvml.ComputeInstanceInfo
}

func (m MigDeviceInfo) CDIDevice() string {
	return fmt.Sprintf("%s=mig-gpu%d-gi%d-ci%d", cdiKind, m.parent.minor, m.giInfo.Id, m.ciInfo.Id)
}

type AllocatedDeviceInfo struct {
	gpu *GpuInfo
	mig *MigDeviceInfo
}

func (i AllocatedDeviceInfo) Type() string {
	if i.gpu != nil {
		return nvcrd.GpuDeviceType
	}
	if i.mig != nil {
		return nvcrd.MigDeviceType
	}
	return nvcrd.UnknownDeviceType
}

func (i AllocatedDeviceInfo) CDIDevice() string {
	switch i.Type() {
	case nvcrd.GpuDeviceType:
		return i.gpu.CDIDevice()
	case nvcrd.MigDeviceType:
		return i.mig.CDIDevice()
	}
	return ""
}

type MigProfileInfo struct {
	profile    *MigProfile
	available  int
	placements []*MigDevicePlacement
}

type MigDevicePlacement struct {
	nvml.GpuInstancePlacement
	blockedBy int
}

type UnallocatedDeviceInfo struct {
	*GpuInfo
	migProfiles map[string]*MigProfileInfo
}

type DeviceState struct {
	sync.Mutex
	cdi        cdiapi.Registry
	alldevices UnallocatedDevices
	available  UnallocatedDevices
	allocated  ClaimAllocations
}

func NewDeviceState(config *Config, nascrd *nvcrd.NodeAllocationState) (*DeviceState, error) {
	alldevices, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)

	err = cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	state := &DeviceState{
		cdi:        cdi,
		alldevices: alldevices,
		available:  alldevices.DeepCopy(),
		allocated:  make(ClaimAllocations),
	}

	err = state.syncAllocatedDevicesFromCRDSpec(&nascrd.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from CRD: %v", err)
	}

	for claimUid := range state.allocated {
		state.removeFromAvailable(state.allocated[claimUid])
	}

	return state, nil
}

func (s *DeviceState) Allocate(claimUid string, requirements nvcrd.DeviceRequirements) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] != nil {
		return s.getAllocatedAsCDIDevices(claimUid), nil
	}

	var err error
	switch requirements.Type() {
	case nvcrd.GpuDeviceType:
		err = s.allocateGpus(claimUid, requirements.Gpu)
	case nvcrd.MigDeviceType:
		err = s.allocateMigDevices(claimUid, requirements.Mig)
	}
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %v", err)
	}

	return s.getAllocatedAsCDIDevices(claimUid), nil
}

func (s *DeviceState) Free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.allocated[claimUid] == nil {
		return nil
	}

	for _, device := range s.allocated[claimUid] {
		var err error
		switch device.Type() {
		case nvcrd.GpuDeviceType:
			err = s.freeGpu(device.gpu)
		case nvcrd.MigDeviceType:
			err = s.freeMigDevice(device.mig)
		}
		if err != nil {
			return fmt.Errorf("free failed: %v", err)
		}
	}

	s.addtoAvailable(s.allocated[claimUid])
	delete(s.allocated, claimUid)
	return nil
}

func (s *DeviceState) GetUpdatedSpec(inspec *nvcrd.NodeAllocationStateSpec) *nvcrd.NodeAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToCRDSpec(outspec)
	s.syncAllocatedDevicesToCRDSpec(outspec)
	return outspec
}

func (s *DeviceState) getAllocatedAsCDIDevices(claimUid string) []string {
	var devs []string
	for _, device := range s.allocated[claimUid] {
		devs = append(devs, s.cdi.DeviceDB().GetDevice(device.CDIDevice()).GetQualifiedName())
	}
	return devs
}

func (s *DeviceState) allocateGpus(claimUid string, requirements *nvcrd.GpuClaimSpec) error {
	var available []*GpuInfo
	for _, d := range s.available {
		if !d.migEnabled {
			available = append(available, d.GpuInfo)
		}
	}

	if len(available) < requirements.Count {
		return fmt.Errorf("not enough GPUs available for allocation (available: %v, required: %v)", len(available), requirements.Count)
	}

	allocated := make(AllocatedDevices)
	for _, gpu := range available[:requirements.Count] {
		allocated[gpu.uuid] = AllocatedDeviceInfo{
			gpu: gpu,
		}
	}

	s.allocated[claimUid] = allocated
	s.removeFromAvailable(allocated)

	return nil
}

func (s *DeviceState) allocateMigDevices(claimUid string, requirements *nvcrd.MigDeviceClaimSpec) error {
	var gpus UnallocatedDevices
	for _, d := range s.available {
		if d.migEnabled {
			gpus = append(gpus, d)
		}
	}

	allocated := make(AllocatedDevices)
	for _, gpu := range gpus {
		if _, exists := gpu.migProfiles[requirements.Profile]; !exists {
			continue
		}

		if gpu.migProfiles[requirements.Profile].available == 0 {
			continue
		}

		migInfo, err := createMigDevice(gpu.GpuInfo, gpu.migProfiles[requirements.Profile].profile, nil)
		if err != nil {
			return fmt.Errorf("error creating MIG device: %v", err)
		}

		allocated[migInfo.uuid] = AllocatedDeviceInfo{
			mig: migInfo,
		}

		break
	}

	if len(allocated) == 0 {
		return fmt.Errorf("no MIG Devices with provided profile available on any GPUs: %v", requirements.Profile)
	}

	s.allocated[claimUid] = allocated
	s.removeFromAvailable(allocated)

	return nil
}

func (s *DeviceState) freeGpu(gpu *GpuInfo) error {
	return nil
}

func (s *DeviceState) freeMigDevice(mig *MigDeviceInfo) error {
	return deleteMigDevice(mig)
}

func (s *DeviceState) removeFromAvailable(ads AllocatedDevices) {
	newav := s.available.DeepCopy()
	(&newav).removeGpus(ads)
	(&newav).removeMigDevices(ads)
	s.available = newav
}

func (s *DeviceState) addtoAvailable(ads AllocatedDevices) {
	newav := s.available.DeepCopy()
	(&newav).addGpus(ads)
	(&newav).addMigDevices(ads)
	s.available = newav
}

func (s *DeviceState) syncAllocatableDevicesToCRDSpec(spec *nvcrd.NodeAllocationStateSpec) {
	gpus := make(map[string]nvcrd.AllocatableDevice)
	migs := make(map[string]map[string]nvcrd.AllocatableDevice)
	for _, device := range s.alldevices {
		if _, exists := gpus[device.name]; !exists {
			gpus[device.name] = nvcrd.AllocatableDevice{
				Gpu: &nvcrd.AllocatableGpu{
					Name:  device.name,
					Count: 0,
				},
			}
		}

		gpus[device.name].Gpu.Count += 1
		if !device.migEnabled {
			gpus[device.name].Gpu.MigDisabled = append(gpus[device.name].Gpu.MigDisabled, device.uuid)
			continue
		}
		gpus[device.name].Gpu.MigEnabled = append(gpus[device.name].Gpu.MigEnabled, device.uuid)

		for _, mig := range device.migProfiles {
			if _, exists := migs[device.name]; !exists {
				migs[device.name] = make(map[string]nvcrd.AllocatableDevice)
			}

			if _, exists := migs[device.name][mig.profile.String()]; exists {
				continue
			}

			var placements []nvcrd.MigDevicePlacement
			for _, p := range mig.placements {
				placements = append(placements, nvcrd.MigDevicePlacement(p.Start))
			}

			ad := nvcrd.AllocatableDevice{
				Mig: &nvcrd.AllocatableMigDevice{
					Profile:    mig.profile.String(),
					Slices:     mig.profile.G,
					ParentName: device.name,
					Placements: placements,
				},
			}

			migs[device.name][mig.profile.String()] = ad
		}
	}

	var allocatable []nvcrd.AllocatableDevice
	for _, device := range gpus {
		if device.Gpu.Count > 0 {
			allocatable = append(allocatable, device)
		}
	}
	for _, devices := range migs {
		for _, device := range devices {
			allocatable = append(allocatable, device)
		}
	}

	spec.AllocatableDevices = allocatable
}

func (s *DeviceState) syncAllocatedDevicesFromCRDSpec(spec *nvcrd.NodeAllocationStateSpec) error {
	gpus, err := getGpus()
	if err != nil {
		return fmt.Errorf("error getting GPUs: %v", err)
	}

	migs := make(map[string]map[string]*MigDeviceInfo)
	for uuid, gpu := range gpus {
		ms, err := getMigDevices(gpu)
		if err != nil {
			return fmt.Errorf("error getting MIG devices for GPU '%v': %v", uuid, err)
		}
		if len(ms) != 0 {
			migs[uuid] = ms
		}
	}

	allocated := make(ClaimAllocations)
	for claim, devices := range spec.ClaimAllocations {
		allocated[claim] = make(AllocatedDevices)
		for _, d := range devices {
			switch d.Type() {
			case nvcrd.GpuDeviceType:
				gpuInfo, err := getGpu(d.Gpu.UUID)
				if err != nil {
					return fmt.Errorf("error getting GPU info for %+v: %v", d.Gpu, err)
				}
				allocated[claim][gpuInfo.uuid] = AllocatedDeviceInfo{
					gpu: gpuInfo,
				}
			case nvcrd.MigDeviceType:
				migInfo := migs[d.Mig.ParentUUID][d.Mig.UUID]
				if migInfo == nil {
					profile, err := ParseMigProfile(d.Mig.Profile)
					if err != nil {
						return fmt.Errorf("error parsing MIG profile for '%v': %v", d.Mig.Profile, err)
					}
					placement := &nvml.GpuInstancePlacement{
						Start: uint32(d.Mig.Placement),
						Size:  uint32(profile.G),
					}
					migInfo, err = createMigDevice(gpus[d.Mig.ParentUUID], profile, placement)
					if err != nil {
						return fmt.Errorf("error creating MIG device info for '%v' on GPU '%v': %v", d.Mig.Profile, d.Mig.ParentUUID, err)
					}
				} else {
					delete(migs[d.Mig.ParentUUID], d.Mig.UUID)
					if len(migs[d.Mig.ParentUUID]) == 0 {
						delete(migs, d.Mig.ParentUUID)
					}
				}
				allocated[claim][migInfo.uuid] = AllocatedDeviceInfo{
					mig: migInfo,
				}
			}
		}
	}

	if len(migs) != 0 {
		return fmt.Errorf("MIG devices found that aren't allocated to any claim: %+v", migs)
	}

	s.allocated = allocated
	return nil
}

func (s *DeviceState) syncAllocatedDevicesToCRDSpec(spec *nvcrd.NodeAllocationStateSpec) {
	outcas := make(map[string][]nvcrd.AllocatedDevice)
	for claim, devices := range s.allocated {
		var allocated []nvcrd.AllocatedDevice
		for uuid, device := range devices {
			outdevice := nvcrd.AllocatedDevice{}
			switch device.Type() {
			case nvcrd.GpuDeviceType:
				outdevice.Gpu = &nvcrd.AllocatedGpu{
					UUID:      uuid,
					Name:      device.gpu.name,
					CDIDevice: device.gpu.CDIDevice(),
				}
			case nvcrd.MigDeviceType:
				outdevice.Mig = &nvcrd.AllocatedMigDevice{
					UUID:       uuid,
					Profile:    device.mig.profile.String(),
					ParentUUID: device.mig.parent.uuid,
					ParentName: device.mig.parent.name,
					CDIDevice:  device.mig.CDIDevice(),
					Placement:  nvcrd.MigDevicePlacement(device.mig.giInfo.Placement.Start),
				}
			}
			allocated = append(allocated, outdevice)
		}
		outcas[claim] = allocated
	}
	spec.ClaimAllocations = outcas
}

func (ds UnallocatedDevices) DeepCopy() UnallocatedDevices {
	var newds UnallocatedDevices
	for _, d := range ds {
		gpuInfo := *d.GpuInfo

		migProfiles := make(map[string]*MigProfileInfo)
		for _, p := range d.migProfiles {
			newp := &MigProfileInfo{
				profile:    p.profile,
				available:  p.available,
				placements: make([]*MigDevicePlacement, len(p.placements)),
			}
			for i := range p.placements {
				p := *p.placements[i]
				newp.placements[i] = &p
			}
			migProfiles[p.profile.String()] = newp
		}

		deviceInfo := UnallocatedDeviceInfo{
			GpuInfo:     &gpuInfo,
			migProfiles: migProfiles,
		}

		newds = append(newds, deviceInfo)
	}
	return newds
}

func (uds *UnallocatedDevices) removeGpus(ads AllocatedDevices) {
	var newuds UnallocatedDevices
	for _, ud := range *uds {
		if _, exists := ads[ud.uuid]; !exists {
			newuds = append(newuds, ud)
		}
	}
	*uds = newuds
}

func (uds *UnallocatedDevices) removeMigDevices(ads AllocatedDevices) {
	for _, ud := range *uds {
		for _, ad := range ads {
			if ad.Type() == nvcrd.MigDeviceType {
				if ud.uuid == ad.mig.parent.uuid {
					for _, profile := range ud.migProfiles {
						profile.removeOverlappingPlacements(&ad.mig.giInfo.Placement)
					}
				}
			}
		}
	}
}

func (uds *UnallocatedDevices) addGpus(ads AllocatedDevices) {
	for _, ad := range ads {
		if ad.Type() == nvcrd.GpuDeviceType {
			ud := UnallocatedDeviceInfo{
				GpuInfo: ad.gpu,
			}
			*uds = append(*uds, ud)
		}
	}
}

func (uds *UnallocatedDevices) addMigDevices(ads AllocatedDevices) {
	for _, ad := range ads {
		if ad.Type() == nvcrd.MigDeviceType {
			for _, ud := range *uds {
				if ud.uuid == ad.mig.parent.uuid {
					for _, profile := range ud.migProfiles {
						profile.addOverlappingPlacements(&ad.mig.giInfo.Placement)
					}
				}
			}
		}
	}
}

func (m *MigProfileInfo) removeOverlappingPlacements(p *nvml.GpuInstancePlacement) {
	pFirst := p.Start
	pLast := p.Start + p.Size - 1

	for _, mpp := range m.placements {
		mppFirst := mpp.Start
		mppLast := (mpp.Start + mpp.Size - 1)

		if mppLast < pFirst {
			continue
		}
		if mppFirst > pLast {
			continue
		}
		if mpp.blockedBy == 0 {
			m.available -= 1
		}
		mpp.blockedBy += 1
	}
}

func (m *MigProfileInfo) addOverlappingPlacements(p *nvml.GpuInstancePlacement) {
	pFirst := p.Start
	pLast := p.Start + p.Size - 1

	for _, mpp := range m.placements {
		mppFirst := mpp.Start
		mppLast := (mpp.Start + mpp.Size - 1)

		if mppLast < pFirst {
			continue
		}
		if mppFirst > pLast {
			continue
		}
		mpp.blockedBy -= 1
		if mpp.blockedBy == 0 {
			m.available += 1
		}
	}
}
