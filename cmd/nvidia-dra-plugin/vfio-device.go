package main

import (
	"os"
	"os/exec"
	"path/filepath"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

const (
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
	cmd := exec.Command("modprobe", vm.vfioPciModule) //nolint:gosec
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	return nil
}

func init() {
}

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
	cmd := exec.Command(unbindFromDriverScript, pciAddress, driverResetRetries) //nolint:gosec
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func bindToDriver(pciAddress, driver string) error {
	cmd := exec.Command(bindToDriverScript, pciAddress, driver) //nolint:gosec
	_, err := cmd.CombinedOutput()
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
