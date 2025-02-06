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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

const (
	procDevicesPath                  = "/proc/devices"
	nvidiaCapsImexChannelsDeviceName = "nvidia-caps-imex-channels"
)

type deviceLib struct {
	nvdev.Interface
	nvmllib           nvml.Interface
	driverLibraryPath string
	devRoot           string
	nvidiaSMIPath     string
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

	// We construct an NVML library specifying the path to libnvidia-ml.so.1
	// explicitly so that we don't have to rely on the library path.
	nvmllib := nvml.New(
		nvml.WithLibraryPath(driverLibraryPath),
	)
	d := deviceLib{
		Interface:         nvdev.New(nvmllib),
		nvmllib:           nvmllib,
		driverLibraryPath: driverLibraryPath,
		devRoot:           driverRoot.getDevRoot(),
		nvidiaSMIPath:     nvidiaSMIPath,
	}
	return &d, nil
}

// prependPathListEnvvar prepends a specified list of strings to a specified envvar and returns its value.
func prependPathListEnvvar(envvar string, prepend ...string) string {
	if len(prepend) == 0 {
		return os.Getenv(envvar)
	}
	current := filepath.SplitList(os.Getenv(envvar))
	return strings.Join(append(prepend, current...), string(filepath.ListSeparator))
}

// setOrOverrideEnvvar adds or updates an envar to the list of specified envvars and returns it.
func setOrOverrideEnvvar(envvars []string, key, value string) []string {
	var updated []string
	for _, envvar := range envvars {
		pair := strings.SplitN(envvar, "=", 2)
		if pair[0] == key {
			continue
		}
		updated = append(updated, envvar)
	}
	return append(updated, fmt.Sprintf("%s=%s", key, value))
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

func (l deviceLib) enumerateAllPossibleDevices(config *Config) (AllocatableDevices, error) {
	alldevices := make(AllocatableDevices)
	deviceClasses := config.flags.deviceClasses

	if deviceClasses.Has(GpuDeviceType) || deviceClasses.Has(MigDeviceType) {
		gms, err := l.enumerateGpusAndMigDevices(config)
		if err != nil {
			return nil, fmt.Errorf("error enumerating GPUs and MIG devices: %w", err)
		}
		for k, v := range gms {
			alldevices[k] = v
		}
	}

	if deviceClasses.Has(ImexChannelType) {
		imex, err := l.enumerateImexChannels(config)
		if err != nil {
			return nil, fmt.Errorf("error enumerating IMEX devices: %w", err)
		}
		for k, v := range imex {
			alldevices[k] = v
		}
	}

	return alldevices, nil
}

func (l deviceLib) enumerateGpusAndMigDevices(config *Config) (AllocatableDevices, error) {
	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	devices := make(AllocatableDevices)
	deviceClasses := config.flags.deviceClasses
	err := l.VisitDevices(func(i int, d nvdev.Device) error {
		gpuInfo, err := l.getGpuInfo(i, d)
		if err != nil {
			return fmt.Errorf("error getting info for GPU %d: %w", i, err)
		}

		if deviceClasses.Has(GpuDeviceType) && !gpuInfo.migEnabled {
			deviceInfo := &AllocatableDevice{
				Gpu: gpuInfo,
			}
			devices[gpuInfo.CanonicalName()] = deviceInfo
		}

		if deviceClasses.Has(MigDeviceType) {
			migs, err := l.getMigDevices(gpuInfo)
			if err != nil {
				return fmt.Errorf("error getting MIG devices for GPU %d: %w", i, err)
			}

			for _, migDeviceInfo := range migs {
				deviceInfo := &AllocatableDevice{
					Mig: migDeviceInfo,
				}
				devices[migDeviceInfo.CanonicalName()] = deviceInfo
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error visiting devices: %w", err)
	}

	return devices, nil
}

func (l deviceLib) enumerateImexChannels(config *Config) (AllocatableDevices, error) {
	devices := make(AllocatableDevices)

	imexChannelCount, err := l.getImexChannelCount()
	if err != nil {
		return nil, fmt.Errorf("error getting IMEX channel count: %w", err)
	}
	for i := 0; i < imexChannelCount; i++ {
		imexChannelInfo := &ImexChannelInfo{
			Channel: i,
		}
		deviceInfo := &AllocatableDevice{
			ImexChannel: imexChannelInfo,
		}
		devices[imexChannelInfo.CanonicalName()] = deviceInfo
	}

	return devices, nil
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

	var migProfiles []*MigProfileInfo
	for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
		giProfileInfo, ret := device.GetGpuInstanceProfileInfo(i)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error retrieving GpuInstanceProfileInfo for profile %d on GPU %v", i, uuid)
		}

		giPossiblePlacements, ret := device.GetGpuInstancePossiblePlacements(&giProfileInfo)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			continue
		}
		if ret == nvml.ERROR_INVALID_ARGUMENT {
			continue
		}
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error retrieving GpuInstancePossiblePlacements for profile %d on GPU %v", i, uuid)
		}

		var migDevicePlacements []*MigDevicePlacement
		for _, p := range giPossiblePlacements {
			mdp := &MigDevicePlacement{
				GpuInstancePlacement: p,
			}
			migDevicePlacements = append(migDevicePlacements, mdp)
		}

		for j := 0; j < nvml.COMPUTE_INSTANCE_PROFILE_COUNT; j++ {
			for k := 0; k < nvml.COMPUTE_INSTANCE_ENGINE_PROFILE_COUNT; k++ {
				migProfile, err := l.NewMigProfile(i, j, k, giProfileInfo.MemorySizeMB, memory.Total)
				if err != nil {
					return nil, fmt.Errorf("error building MIG profile from GpuInstanceProfileInfo for profile %d on GPU %v", i, uuid)
				}

				if migProfile.GetInfo().G != migProfile.GetInfo().C {
					continue
				}

				profileInfo := &MigProfileInfo{
					profile:    migProfile,
					placements: migDevicePlacements,
				}

				migProfiles = append(migProfiles, profileInfo)
			}
		}
	}

	gpuInfo := &GpuInfo{
		UUID:                  uuid,
		minor:                 minor,
		index:                 index,
		migEnabled:            migEnabled,
		memoryBytes:           memory.Total,
		productName:           productName,
		brand:                 brand,
		architecture:          architecture,
		cudaComputeCapability: cudaComputeCapability,
		driverVersion:         driverVersion,
		cudaDriverVersion:     fmt.Sprintf("%v.%v", cudaDriverVersion/1000, (cudaDriverVersion%1000)/10),
		migProfiles:           migProfiles,
	}

	return gpuInfo, nil
}

func (l deviceLib) getMigDevices(gpuInfo *GpuInfo) (map[string]*MigDeviceInfo, error) {
	if !gpuInfo.migEnabled {
		return nil, nil
	}

	if err := l.Init(); err != nil {
		return nil, err
	}
	defer l.alwaysShutdown()

	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpuInfo.UUID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
	}

	migInfos := make(map[string]*MigDeviceInfo)
	err := walkMigDevices(device, func(i int, migDevice nvml.Device) error {
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

		var migProfile *MigProfileInfo
		var giProfileInfo *nvml.GpuInstanceProfileInfo
		var ciProfileInfo *nvml.ComputeInstanceProfileInfo
		for _, profile := range gpuInfo.migProfiles {
			profileInfo := profile.profile.GetInfo()
			gipInfo, ret := device.GetGpuInstanceProfileInfo(profileInfo.GIProfileID)
			if ret != nvml.SUCCESS {
				continue
			}
			if giInfo.ProfileId != gipInfo.Id {
				continue
			}
			cipInfo, ret := gi.GetComputeInstanceProfileInfo(profileInfo.CIProfileID, profileInfo.CIEngProfileID)
			if ret != nvml.SUCCESS {
				continue
			}
			if ciInfo.ProfileId != cipInfo.Id {
				continue
			}
			migProfile = profile
			giProfileInfo = &gipInfo
			ciProfileInfo = &cipInfo
		}
		if migProfile == nil {
			return fmt.Errorf("error getting profile info for MIG device: %v", uuid)
		}

		placement := MigDevicePlacement{
			GpuInstancePlacement: giInfo.Placement,
		}

		migInfos[uuid] = &MigDeviceInfo{
			UUID:          uuid,
			index:         i,
			profile:       migProfile.String(),
			parent:        gpuInfo,
			placement:     &placement,
			giProfileInfo: giProfileInfo,
			giInfo:        &giInfo,
			ciProfileInfo: ciProfileInfo,
			ciInfo:        &ciInfo,
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

func (l deviceLib) getImexChannelCount() (int, error) {
	// TODO: Pull this value from /proc/driver/nvidia/params
	return 2048, nil
}

func (l deviceLib) getImexChannelMajor() (int, error) {
	file, err := os.Open(procDevicesPath)
	if err != nil {
		return -1, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	foundCharDevices := false

	for scanner.Scan() {
		line := scanner.Text()

		// Ignore empty lines
		if line == "" {
			continue
		}

		// Check for any line with text followed by a colon (header)
		if strings.Contains(line, ":") {
			// Stop if we've already found the character devices section and reached another section
			if foundCharDevices {
				break
			}
			// Check if we entered the character devices section
			if strings.HasSuffix(line, ":") && strings.HasPrefix(line, "Character") {
				foundCharDevices = true
			}
			// Continue to the next line, regardless
			continue
		}

		// If we've passed the character devices section, check for nvidiaCapsImexChannelsDeviceName
		if foundCharDevices {
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[1] == nvidiaCapsImexChannelsDeviceName {
				return strconv.Atoi(parts[0])
			}
		}
	}

	return -1, scanner.Err()
}

func (l deviceLib) createImexChannelDevice(channel int) error {
	// Construct the properties of the device node to create.
	path := fmt.Sprintf("/dev/nvidia-caps-imex-channels/channel%d", channel)
	path = filepath.Join(l.devRoot, path)
	mode := uint32(unix.S_IFCHR | 0666)

	// Get the IMEX channel major and build a /dev device from it
	major, err := l.getImexChannelMajor()
	if err != nil {
		return fmt.Errorf("error getting IMEX channel major: %w", err)
	}
	dev := unix.Mkdev(uint32(major), uint32(channel))

	// Recursively create any parent directories of the channel.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("error creating directory for IMEX channel device nodes: %w", err)
	}

	// Remove the channel if it already exists.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error removing existing IMEX channel device node: %w", err)
	}

	// Create the device node using syscall.Mknod
	if err := unix.Mknod(path, mode, int(dev)); err != nil {
		return fmt.Errorf("mknod of IMEX channel failed: %w", err)
	}

	return nil
}

func (l deviceLib) setTimeSlice(uuids []string, timeSlice int) error {
	for _, uuid := range uuids {
		cmd := exec.Command(
			l.nvidiaSMIPath,
			"compute-policy",
			"-i", uuid,
			"--set-timeslice", fmt.Sprintf("%d", timeSlice))

		// In order for nvidia-smi to run, we need update LD_PRELOAD to include the path to libnvidia-ml.so.1.
		cmd.Env = setOrOverrideEnvvar(os.Environ(), "LD_PRELOAD", prependPathListEnvvar("LD_PRELOAD", l.driverLibraryPath))

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
			l.nvidiaSMIPath,
			"-i", uuid,
			"-c", mode)

		// In order for nvidia-smi to run, we need update LD_PRELOAD to include the path to libnvidia-ml.so.1.
		cmd.Env = setOrOverrideEnvvar(os.Environ(), "LD_PRELOAD", prependPathListEnvvar("LD_PRELOAD", l.driverLibraryPath))

		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("\n%v", string(output))
			return fmt.Errorf("error running nvidia-smi: %w", err)
		}
	}
	return nil
}

// TODO: Reenable dynamic MIG functionality once it is supported in Kubernetes 1.32
//
// func (l deviceLib) createMigDevice(gpu *GpuInfo, profile nvdev.MigProfile, placement *nvml.GpuInstancePlacement) (*MigDeviceInfo, error) {
// 	if err := l.Init(); err != nil {
// 		return nil, err
// 	}
// 	defer l.alwaysShutdown()
//
// 	profileInfo := profile.GetInfo()
//
// 	device, ret := l.nvmllib.DeviceGetHandleByUUID(gpu.UUID)
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
// 	}
//
// 	giProfileInfo, ret := device.GetGpuInstanceProfileInfo(profileInfo.GIProfileID)
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error getting GPU instance profile info for '%v': %v", profile, ret)
// 	}
//
// 	gi, ret := device.CreateGpuInstanceWithPlacement(&giProfileInfo, placement)
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error creating GPU instance for '%v': %v", profile, ret)
// 	}
//
// 	giInfo, ret := gi.GetInfo()
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, ret)
// 	}
//
// 	ciProfileInfo, ret := gi.GetComputeInstanceProfileInfo(profileInfo.CIProfileID, profileInfo.CIEngProfileID)
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error getting Compute instance profile info for '%v': %v", profile, ret)
// 	}
//
// 	ci, ret := gi.CreateComputeInstance(&ciProfileInfo)
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error creating Compute instance for '%v': %v", profile, ret)
// 	}
//
// 	ciInfo, ret := ci.GetInfo()
// 	if ret != nvml.SUCCESS {
// 		return nil, fmt.Errorf("error getting GPU instance info for '%v': %v", profile, ret)
// 	}
//
// 	uuid := ""
// 	err := walkMigDevices(device, func(i int, migDevice nvml.Device) error {
// 		giID, ret := migDevice.GetGpuInstanceId()
// 		if ret != nvml.SUCCESS {
// 			return fmt.Errorf("error getting GPU instance ID for MIG device: %v", ret)
// 		}
// 		ciID, ret := migDevice.GetComputeInstanceId()
// 		if ret != nvml.SUCCESS {
// 			return fmt.Errorf("error getting Compute instance ID for MIG device: %v", ret)
// 		}
// 		if giID != int(giInfo.Id) || ciID != int(ciInfo.Id) {
// 			return nil
// 		}
// 		uuid, ret = migDevice.GetUUID()
// 		if ret != nvml.SUCCESS {
// 			return fmt.Errorf("error getting UUID for MIG device: %v", ret)
// 		}
// 		return nil
// 	})
// 	if err != nil {
// 		return nil, fmt.Errorf("error processing MIG device for GI and CI just created: %w", err)
// 	}
// 	if uuid == "" {
// 		return nil, fmt.Errorf("unable to find MIG device for GI and CI just created")
// 	}
//
// 	migInfo := &MigDeviceInfo{
// 		UUID:    uuid,
// 		parent:  gpu,
// 		profile: profile,
// 		giInfo:  &giInfo,
// 		ciInfo:  &ciInfo,
// 	}
//
// 	return migInfo, nil
// }
//
// func (l deviceLib) deleteMigDevice(mig *MigDeviceInfo) error {
// 	if err := l.Init(); err != nil {
// 		return err
// 	}
// 	defer l.alwaysShutdown()
//
// 	parent, ret := l.nvmllib.DeviceGetHandleByUUID(mig.parent.UUID)
// 	if ret != nvml.SUCCESS {
// 		return fmt.Errorf("error getting device from UUID '%v': %v", mig.parent.UUID, ret)
// 	}
// 	gi, ret := parent.GetGpuInstanceById(int(mig.giInfo.Id))
// 	if ret != nvml.SUCCESS {
// 		return fmt.Errorf("error getting GPU instance ID for MIG device: %v", ret)
// 	}
// 	ci, ret := gi.GetComputeInstanceById(int(mig.ciInfo.Id))
// 	if ret != nvml.SUCCESS {
// 		return fmt.Errorf("error getting Compute instance ID for MIG device: %v", ret)
// 	}
// 	ret = ci.Destroy()
// 	if ret != nvml.SUCCESS {
// 		return fmt.Errorf("error destroying Compute Instance: %v", ret)
// 	}
// 	ret = gi.Destroy()
// 	if ret != nvml.SUCCESS {
// 		return fmt.Errorf("error destroying GPU Instance: %v", ret)
// 	}
// 	return nil
// }
