/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

const (
	hostNamespaceMount     = "/proc/1/ns/mnt"
	vfioPciModule          = "vfio_pci"
	vfioPciDriver          = "vfio-pci"
	nvidiaDriver           = "nvidia"
	unbindFromDriverScript = "/usr/bin/unbind_from_driver.sh"
	bindToDriverScript     = "/usr/bin/bind_to_driver.sh"
	driverResetRetries     = "5"
)

type VfioPciManager struct {
	pciDevicesRoot  string
	vfioDevicesRoot string
	sysModulesRoot  string
	driver          string
	vfioPciModule   string
}

func NewVfioPciManager() *VfioPciManager {
	return &VfioPciManager{
		pciDevicesRoot:  "/sys/bus/pci/devices",
		vfioDevicesRoot: "/dev/vfio",
		sysModulesRoot:  "/sys/module",
		driver:          vfioPciDriver,
		vfioPciModule:   vfioPciModule,
	}
}

// Init ensures the vfio-pci module is loaded on the host.
func (vm *VfioPciManager) Init() error {
	if !vm.isVfioPCIModuleLoaded() {
		err := vm.loadVfioPciModule()
		if err != nil {
			return err
		}
	}
	return nil
}

func (vm *VfioPciManager) isVfioPCIModuleLoaded() bool {
	modules, err := os.ReadDir(vm.sysModulesRoot)
	if err != nil {
		return false
	}

	for _, module := range modules {
		if module.Name() == vm.vfioPciModule {
			return true
		}
	}

	return false

}

func (vm *VfioPciManager) loadVfioPciModule() error {
	_, err := execCommandInHostNamespace("modprobe", []string{vm.vfioPciModule}) //nolint:gosec
	if err != nil {
		return err
	}

	return nil
}

// Configure binds the GPU to the vfio-pci driver.
func (vm *VfioPciManager) Configure(info *GpuInfo) error {
	perGpuLock.Get(info.PciAddress).Lock()
	defer perGpuLock.Get(info.PciAddress).Unlock()

	driver, err := getDriver(vm.pciDevicesRoot, info.PciAddress)
	if err != nil {
		return err
	}
	if driver == vm.driver {
		return nil
	}
	err = changeDriver(info.PciAddress, vm.driver)
	if err != nil {
		return err
	}
	return nil
}

// Unconfigure binds the GPU to the nvidia driver.
func (vm *VfioPciManager) Unconfigure(info *GpuInfo) error {
	perGpuLock.Get(info.PciAddress).Lock()
	defer perGpuLock.Get(info.PciAddress).Unlock()

	driver, err := getDriver(vm.pciDevicesRoot, info.PciAddress)
	if err != nil {
		return err
	}
	if driver == nvidiaDriver {
		return nil
	}
	err = changeDriver(info.PciAddress, nvidiaDriver)
	if err != nil {
		return err
	}
	return nil
}

func getDriver(pciDevicesRoot, pciAddress string) (string, error) {
	driverPath, err := os.Readlink(filepath.Join(pciDevicesRoot, pciAddress, "driver"))
	if err != nil {
		return "", err
	}
	_, driver := filepath.Split(driverPath)
	return driver, nil
}

func changeDriver(pciAddress, driver string) error {
	err := unbindFromDriver(pciAddress, driver)
	if err != nil {
		return err
	}
	err = bindToDriver(pciAddress, driver)
	if err != nil {
		return err
	}
	return nil
}

func unbindFromDriver(pciAddress, driverResetRetries string) error {
	_, err := execCommandInHostNamespace(unbindFromDriverScript, []string{pciAddress, driverResetRetries}) //nolint:gosec
	if err != nil {
		return err
	}
	return nil
}

func bindToDriver(pciAddress, driver string) error {
	_, err := execCommandInHostNamespace(bindToDriverScript, []string{pciAddress, driver}) //nolint:gosec
	if err != nil {
		return err
	}
	return nil
}

func (vm *VfioPciManager) getIommuGroupForVfioPciDevice(pciAddress string) string {
	iommuGroup, err := os.Readlink(filepath.Join(vm.pciDevicesRoot, pciAddress, "iommu_group"))
	if err != nil {
		return ""
	}
	_, file := filepath.Split(iommuGroup)
	return file

}

// GetCDIContainerEdits returns the CDI spec for a container to have access to the GPU while bound on vfio-pci driver.
func (vm *VfioPciManager) GetCDIContainerEdits(info *GpuInfo) *cdiapi.ContainerEdits {
	iommuGroup := vm.getIommuGroupForVfioPciDevice(info.PciAddress)
	vfioDevicePath := filepath.Join(vm.vfioDevicesRoot, iommuGroup)
	return &cdiapi.ContainerEdits{
		ContainerEdits: &cdispec.ContainerEdits{
			DeviceNodes: []*cdispec.DeviceNode{
				{
					Path: vfioDevicePath,
				},
			},
		},
	}
}

func execCommandInHostNamespace(cmd string, args []string) ([]byte, error) {
	nsenterArgs := []string{fmt.Sprintf("--mount=%s", hostNamespaceMount), "--", cmd}
	nsenterArgs = append(nsenterArgs, args...)
	return exec.Command("nsenter", nsenterArgs...).CombinedOutput()
}
