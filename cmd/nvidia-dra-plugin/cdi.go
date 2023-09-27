/*
 * Copyright (c) 2022-2023, NVIDIA CORPORATION.  All rights reserved.
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
	"io"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	cdiparser "github.com/container-orchestrated-devices/container-device-interface/pkg/parser"
	cdispec "github.com/container-orchestrated-devices/container-device-interface/specs-go"
	nvdevice "gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvlib/device"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvml"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

const (
	cdiVendor = "k8s." + DriverName
	cdiClass  = "claim"
	cdiKind   = cdiVendor + "/" + cdiClass

	defaultCDIRoot = "/var/run/cdi"
)

type CDIHandler struct {
	logger           *logrus.Logger
	nvml             nvml.Interface
	nvdevice         nvdevice.Interface
	nvcdi            nvcdi.Interface
	registry         cdiapi.Registry
	driverRoot       string
	targetDriverRoot string
	nvidiaCTKPath    string

	cdiRoot string
	vendor  string
	class   string
}

func NewCDIHandler(opts ...cdiOption) (*CDIHandler, error) {
	h := &CDIHandler{}
	for _, opt := range opts {
		opt(h)
	}

	if h.logger == nil {
		h.logger = logrus.New()
		h.logger.SetOutput(io.Discard)
	}
	if h.nvml == nil {
		h.nvml = nvml.New()
	}
	if h.nvdevice == nil {
		h.nvdevice = nvdevice.New(nvdevice.WithNvml(h.nvml))
	}
	if h.vendor == "" {
		h.vendor = cdiVendor
	}
	if h.class == "" {
		h.class = cdiClass
	}
	if h.nvcdi == nil {
		nvcdilib, err := nvcdi.New(
			nvcdi.WithDeviceLib(h.nvdevice),
			nvcdi.WithDriverRoot(h.driverRoot),
			nvcdi.WithLogger(h.logger),
			nvcdi.WithNvmlLib(h.nvml),
			nvcdi.WithMode("nvml"),
			nvcdi.WithVendor(h.vendor),
			nvcdi.WithClass(h.class),
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create CDI library: %v", err)
		}
		h.nvcdi = nvcdilib
	}
	if h.cdiRoot == "" {
		h.cdiRoot = defaultCDIRoot
	}

	if h.registry == nil {
		// TODO: We should rather construct a cdi.CacheHere directly.
		registry := cdiapi.GetRegistry(
			cdiapi.WithSpecDirs(h.cdiRoot),
		)
		err := registry.Refresh()
		if err != nil {
			return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
		}
		h.registry = registry
	}

	return h, nil
}

func (cdi *CDIHandler) GetDevice(device string) *cdiapi.Device {
	return cdi.registry.DeviceDB().GetDevice(device)
}

func (cdi *CDIHandler) CreateClaimSpecFile(claimUID string, devices *PreparedDevices) error {
	ret := cdi.nvml.Init()
	if ret != nvml.SUCCESS {
		return ret
	}
	defer func() {
		_ = cdi.nvml.Shutdown()
	}()

	claimEdits := cdiapi.ContainerEdits{}

	switch devices.Type() {
	case nascrd.GpuDeviceType:
		for _, device := range devices.Gpu.Devices {
			nvmlDevice, ret := cdi.nvml.DeviceGetHandleByUUID(device.uuid)
			if ret != nvml.SUCCESS {
				return fmt.Errorf("unable to get nvml GPU device for UUID '%v': %v", device.uuid, ret)
			}
			nvlibDevice, err := cdi.nvdevice.NewDevice(nvmlDevice)
			if err != nil {
				return fmt.Errorf("unable to get nvlib GPU device for UUID '%v': %v", device.uuid, ret)
			}
			gpuEdits, err := cdi.nvcdi.GetGPUDeviceEdits(nvlibDevice)
			if err != nil {
				return fmt.Errorf("unable to get CDI spec edits for GPU: %v", device)
			}
			claimEdits.Append(gpuEdits)
		}
	case nascrd.MigDeviceType:
		for _, device := range devices.Mig.Devices {
			nvmlParentDevice, ret := cdi.nvml.DeviceGetHandleByUUID(device.parent.uuid)
			if ret != nvml.SUCCESS {
				return fmt.Errorf("unable to get nvml GPU parent device for MIG UUID '%v': %v", device.uuid, ret)
			}
			nvlibParentDevice, err := cdi.nvdevice.NewDevice(nvmlParentDevice)
			if err != nil {
				return fmt.Errorf("unable to get nvlib GPU parent device for MIG UUID '%v': %v", device.uuid, ret)
			}
			var nvlibMigDevice nvdevice.MigDevice
			migs, err := nvlibParentDevice.GetMigDevices()
			if err != nil {
				return fmt.Errorf("unable to get MIG devices on GPU '%v': %v", device.parent.uuid, err)
			}
			for _, mig := range migs {
				uuid, ret := mig.GetUUID()
				if err != nil {
					return fmt.Errorf("unable to get MIG UUID: %v", ret)
				}
				if uuid == device.uuid {
					nvlibMigDevice = mig
					break
				}
			}
			if nvlibMigDevice == nil {
				return fmt.Errorf("unable to find MIG device '%v' on parent GPU '%v'", device.uuid, device.parent.uuid)
			}
			migEdits, err := cdi.nvcdi.GetMIGDeviceEdits(nvlibParentDevice, nvlibMigDevice)
			if err != nil {
				return fmt.Errorf("unable to get CDI spec edits for MIG device: %v", device)
			}
			claimEdits.Append(migEdits)
		}
	}

	// We construct the common edits that are independent of any specific device
	// in the CDI specification associated with a claim.
	commonEdits, err := cdi.nvcdi.GetCommonEdits()
	if err != nil {
		return fmt.Errorf("failed to get common CDI spec edits: %v", err)
	}

	if devices.MpsControlDaemon != nil {
		commonEdits.Append(devices.MpsControlDaemon.GetCDIContainerEdits())
	}

	spec, err := spec.New(
		spec.WithVendor(cdiVendor),
		spec.WithClass(cdiClass),
		spec.WithDeviceSpecs(
			[]cdispec.Device{
				{
					Name:           claimUID,
					ContainerEdits: *claimEdits.ContainerEdits,
				},
			},
		),
		spec.WithEdits(*commonEdits.ContainerEdits),
	)
	if err != nil {
		return fmt.Errorf("failed to creat CDI spec: %w", err)
	}
	err = transform.NewRootTransformer(cdi.driverRoot, cdi.targetDriverRoot).Transform(spec.Raw())
	if err != nil {
		return fmt.Errorf("failed to transform driver root in CDI spec: %v", err)
	}

	specName, err := cdiapi.GenerateNameForTransientSpec(spec.Raw(), claimUID)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return spec.Save(filepath.Join(cdi.cdiRoot, specName+".json"))
}

func (cdi *CDIHandler) DeleteClaimSpecFile(claimUID string) error {
	spec := &cdispec.Spec{
		Kind: cdiKind,
	}

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, claimUID)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.registry.SpecDB().RemoveSpec(specName + ".json")
}

func (cdi *CDIHandler) GetClaimDevices(claimUID string) []string {
	devices := []string{
		cdiparser.QualifiedName(cdiVendor, cdiClass, claimUID),
	}
	return devices
}
