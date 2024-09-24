package main

import (
	"os"
	"os/exec"
	"path/filepath"

	configapi "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
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

func (vm *VfioPciManager) Configure(vfioPciDevice *VfioPciDeviceInfo, config *configapi.VfioPciConfig) error {
	perGpuLock.Get(vfioPciDevice.parent.pciAddress).Lock()
	defer perGpuLock.Get(vfioPciDevice.parent.pciAddress).Unlock()

	driver, err := getDriver(vm.pciDevicesRoot, vfioPciDevice.parent.pciAddress)
	if err != nil {
		return err
	}
	if driver == vm.driver {
		return nil
	}
	err = changeDriver(vm.pciDevicesRoot, vfioPciDevice.parent.pciAddress, vm.driver)
	if err != nil {
		return err
	}
	return nil
}

func (vm *VfioPciManager) Unconfigure(vfioPciDevice *VfioPciDeviceInfo) error {
	perGpuLock.Get(vfioPciDevice.parent.pciAddress).Lock()
	defer perGpuLock.Get(vfioPciDevice.parent.pciAddress).Unlock()

	driver, err := getDriver(vm.pciDevicesRoot, vfioPciDevice.parent.pciAddress)
	if err != nil {
		return err
	}
	if driver == nvidiaDriver {
		return nil
	}
	err = changeDriver(vm.pciDevicesRoot, vfioPciDevice.parent.pciAddress, nvidiaDriver)
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

func changeDriver(pciDevicesRoot, pciAddress, driver string) error {
	err := unbindFromDriver(pciDevicesRoot, pciAddress, driver)
	if err != nil {
		return err
	}
	err = bindToDriver(pciDevicesRoot, pciAddress, driver)
	if err != nil {
		return err
	}
	return nil
}

func unbindFromDriver(pciDevicesRoot, pciAddress, driverResetRetries string) error {
	cmd := exec.Command(unbindFromDriverScript, pciDevicesRoot, pciAddress, driverResetRetries) //nolint:gosec
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func bindToDriver(pciDevicesRoot, pciAddress, driver string) error {
	cmd := exec.Command(bindToDriverScript, pciDevicesRoot, pciAddress, driver) //nolint:gosec
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

func (vm *VfioPciManager) GetCommonCDIContainerEdits() *cdiapi.ContainerEdits {
	vfioVfioDevicePath := filepath.Join(vm.vfioDevicesRoot, "vfio")
	return &cdiapi.ContainerEdits{
		ContainerEdits: &cdispec.ContainerEdits{
			DeviceNodes: []*cdispec.DeviceNode{
				{
					Path: vfioVfioDevicePath,
				},
			},
			Mounts: []*cdispec.Mount{
				{
					ContainerPath: vfioVfioDevicePath,
					HostPath:      vfioVfioDevicePath,
					Options:       []string{"mrw"},
				},
			},
		},
	}
}

func (vm *VfioPciManager) GetCDIContainerEdits(info *VfioPciDeviceInfo) *cdiapi.ContainerEdits {
	iommuGroup := vm.getIommuGroupForVfioPciDevice(info.parent.pciAddress)
	vfioPciDevicePath := filepath.Join(vm.vfioDevicesRoot, iommuGroup)
	return &cdiapi.ContainerEdits{
		ContainerEdits: &cdispec.ContainerEdits{
			DeviceNodes: []*cdispec.DeviceNode{
				{
					Path: vfioPciDevicePath,
				},
			},
			Mounts: []*cdispec.Mount{
				{
					ContainerPath: vfioPciDevicePath,
					HostPath:      vfioPciDevicePath,
					Options:       []string{"mrw"},
				},
			},
		},
	}
}
