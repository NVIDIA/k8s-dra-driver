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
	"os"

	"github.com/sirupsen/logrus"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	cdispec "github.com/container-orchestrated-devices/container-device-interface/specs-go"
	nvdevice "gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/device"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"
)

const (
	cdiVendor = "k8s." + DriverName
	cdiClass  = "claim"
	cdiKind   = cdiVendor + "/" + cdiClass

	cdiCommonDeviceName = "common"
)

type CDIHandler struct {
	logger   *logrus.Logger
	nvml     nvml.Interface
	nvdevice nvdevice.Interface
	nvcdi    nvcdi.Interface
	registry cdiapi.Registry
}

func NewCDIHandler(config *Config) (*CDIHandler, error) {
	registry := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(*config.flags.cdiRoot),
	)

	err := registry.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	logger := logrus.New()
	logger.Out = os.Stdout
	logger.Level = logrus.DebugLevel

	nvmllib := nvml.New()
	nvdevicelib := nvdevice.New(
		nvdevice.WithNvml(nvmllib),
	)
	nvcdilib := nvcdi.New(
		nvcdi.WithDeviceLib(nvdevicelib),
		nvcdi.WithDriverRoot("/run/nvidia/driver"),
		nvcdi.WithLogger(logger),
		nvcdi.WithNvmlLib(nvmllib),
		nvcdi.WithMode("nvml"),
	)

	handler := &CDIHandler{
		logger:   logger,
		nvml:     nvmllib,
		nvdevice: nvdevicelib,
		nvcdi:    nvcdilib,
		registry: registry,
	}

	return handler, nil
}

func (cdi *CDIHandler) GetDevice(device string) *cdiapi.Device {
	return cdi.registry.DeviceDB().GetDevice(device)
}

func (cdi *CDIHandler) CreateCommonSpecFile() error {
	ret := cdi.nvml.Init()
	if ret != nvml.SUCCESS {
		return ret
	}
	defer cdi.nvml.Shutdown()

	commonEdits, err := cdi.nvcdi.GetCommonEdits()
	if err != nil {
		return fmt.Errorf("failed to get common CDI spec edits: %v", err)
	}

	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				Name:           cdiCommonDeviceName,
				ContainerEdits: *commonEdits.ContainerEdits,
			},
		},
	}

	minVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = minVersion

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, cdiCommonDeviceName)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.registry.SpecDB().WriteSpec(spec, specName)
}

func (cdi *CDIHandler) CreateClaimSpecFile(claimUid string, devices AllocatedDevices) error {
	ret := cdi.nvml.Init()
	if ret != nvml.SUCCESS {
		return ret
	}
	defer cdi.nvml.Shutdown()

	claimEdits := cdiapi.ContainerEdits{}

	for _, device := range devices {
		switch device.Type() {
		case nascrd.GpuDeviceType:
			nvmlDevice, ret := cdi.nvml.DeviceGetHandleByUUID(device.gpu.uuid)
			if ret != nvml.SUCCESS {
				return fmt.Errorf("unable to get nvml GPU device for UUID '%v': %v", device.gpu.uuid, ret)
			}
			nvlibDevice, err := cdi.nvdevice.NewDevice(nvmlDevice)
			if err != nil {
				return fmt.Errorf("unable to get nvlib GPU device for UUID '%v': %v", device.gpu.uuid, ret)
			}
			gpuEdits, err := cdi.nvcdi.GetGPUDeviceEdits(nvlibDevice)
			if err != nil {
				return fmt.Errorf("unable to get CDI spec edits for GPU: %v", device.gpu)
			}
			claimEdits.Append(gpuEdits)
		case nascrd.MigDeviceType:
			nvmlParentDevice, ret := cdi.nvml.DeviceGetHandleByUUID(device.mig.parent.uuid)
			if ret != nvml.SUCCESS {
				return fmt.Errorf("unable to get nvml GPU parent device for MIG UUID '%v': %v", device.mig.uuid, ret)
			}
			nvlibParentDevice, err := cdi.nvdevice.NewDevice(nvmlParentDevice)
			if err != nil {
				return fmt.Errorf("unable to get nvlib GPU parent device for MIG UUID '%v': %v", device.mig.uuid, ret)
			}
			var nvlibMigDevice nvdevice.MigDevice
			migs, err := nvlibParentDevice.GetMigDevices()
			if err != nil {
				return fmt.Errorf("unable to get MIG devices on GPU '%v': %v", device.mig.parent.uuid, err)
			}
			for _, mig := range migs {
				uuid, ret := mig.GetUUID()
				if err != nil {
					return fmt.Errorf("unable to get MIG UUID: %v", ret)
				}
				if uuid == device.mig.uuid {
					nvlibMigDevice = mig
					break
				}
			}
			if nvlibMigDevice == nil {
				return fmt.Errorf("unable to find MIG device '%v' on parent GPU '%v'", device.mig.uuid, device.mig.parent.uuid)
			}
			migEdits, err := cdi.nvcdi.GetMIGDeviceEdits(nvlibParentDevice, nvlibMigDevice)
			if err != nil {
				return fmt.Errorf("unable to get CDI spec edits for MIG device: %v", device.mig)
			}
			claimEdits.Append(migEdits)
		}
	}

	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				Name:           claimUid,
				ContainerEdits: *claimEdits.ContainerEdits,
			},
		},
	}

	minVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = minVersion

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, claimUid)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.registry.SpecDB().WriteSpec(spec, specName)
}

func (cdi *CDIHandler) DeleteClaimSpecFile(claimUid string) error {
	spec := &cdispec.Spec{
		Kind: cdiKind,
	}

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, claimUid)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.registry.SpecDB().RemoveSpec(specName)
}

func (cdi *CDIHandler) GetClaimDevices(claimUid string) []string {
	devices := []string{
		cdiapi.QualifiedName(cdiVendor, cdiClass, cdiCommonDeviceName),
		cdiapi.QualifiedName(cdiVendor, cdiClass, claimUid),
	}
	return devices
}
