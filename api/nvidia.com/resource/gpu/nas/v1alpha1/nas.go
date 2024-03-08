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

package v1alpha1

import (
	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MigDevicePlacement represents the placement of a MIG device within a GPU.
type MigDevicePlacement struct {
	Start int `json:"start"`
	Size  int `json:"size"`
}

// AllocatableGpu represents an allocatable GPU on a node.
type AllocatableGpu struct {
	Index                 int    `json:"index"`
	UUID                  string `json:"uuid"`
	MigEnabled            bool   `json:"migEnabled"`
	MemoryBytes           uint64 `json:"memoryBytes"`
	ProductName           string `json:"productName"`
	Brand                 string `json:"brand"`
	Architecture          string `json:"architecture"`
	CUDAComputeCapability string `json:"cudaComputeCapability"`
}

// AllocatableMigDevice represents an allocatable MIG device (and its possible placements) on a given type of GPU.
type AllocatableMigDevice struct {
	Profile           string               `json:"profile"`
	ParentProductName string               `json:"parentProductName"`
	Placements        []MigDevicePlacement `json:"placements"`
}

// AllocatableDevice represents an allocatable device on a node.
type AllocatableDevice struct {
	Gpu *AllocatableGpu       `json:"gpu,omitempty"`
	Mig *AllocatableMigDevice `json:"mig,omitempty"`
}

// Type returns the type of AllocatableDevice this represents.
func (d AllocatableDevice) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	if d.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
}

// AllocatedGpu represents an allocated GPU.
type AllocatedGpu struct {
	UUID string `json:"uuid,omitempty"`
}

// AllocatedMigDevice represents an allocated MIG device.
type AllocatedMigDevice struct {
	Profile    string             `json:"profile"`
	ParentUUID string             `json:"parentUUID"`
	Placement  MigDevicePlacement `json:"placement"`
}

// AllocatedGpus represents a set of allocated GPUs.
type AllocatedGpus struct {
	Devices []AllocatedGpu `json:"devices"`
	Sharing *GpuSharing    `json:"sharing,omitempty"`
}

// AllocatedMigDevices represents a set of allocated MIG devices.
type AllocatedMigDevices struct {
	Devices []AllocatedMigDevice `json:"devices"`
	Sharing *MigDeviceSharing    `json:"sharing,omitempty"`
}

// AllocatedDevices represents a set of allocated devices.
type AllocatedDevices struct {
	ClaimInfo *types.ClaimInfo     `json:"claimInfo"`
	Gpu       *AllocatedGpus       `json:"gpu,omitempty"`
	Mig       *AllocatedMigDevices `json:"mig,omitempty"`
}

// Type returns the type of AllocatedDevices this represents.
func (r AllocatedDevices) Type() string {
	if r.Gpu != nil {
		return types.GpuDeviceType
	}
	if r.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
}

// PreparedGpu represents a prepared GPU on a node.
type PreparedGpu struct {
	UUID string `json:"uuid"`
}

// PreparedMigDevice represents a prepared MIG device on a node.
type PreparedMigDevice struct {
	UUID       string             `json:"uuid"`
	Profile    string             `json:"profile"`
	ParentUUID string             `json:"parentUUID"`
	Placement  MigDevicePlacement `json:"placement"`
}

// PreparedGpus represents a set of prepared GPUs.
type PreparedGpus struct {
	Devices []PreparedGpu `json:"devices"`
}

// PreparedMigDevices represents a set of prepared MIG devices on a node.
type PreparedMigDevices struct {
	Devices []PreparedMigDevice `json:"devices"`
}

// PreparedDevices represents a set of prepared devices on a node.
type PreparedDevices struct {
	Gpu *PreparedGpus       `json:"gpu,omitempty"`
	Mig *PreparedMigDevices `json:"mig,omitempty"`
}

// Type returns the type of PreparedDevices this represents.
func (d PreparedDevices) Type() string {
	if d.Gpu != nil {
		return types.GpuDeviceType
	}
	if d.Mig != nil {
		return types.MigDeviceType
	}
	return types.UnknownDeviceType
}

// NodeAllocationStateSpec is the spec for the NodeAllocationState CRD.
type NodeAllocationStateSpec struct {
	AllocatableDevices []AllocatableDevice         `json:"allocatableDevices,omitempty"`
	AllocatedClaims    map[string]AllocatedDevices `json:"allocatedClaims,omitempty"`
	PreparedClaims     map[string]PreparedDevices  `json:"preparedClaims,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:singular=nas

// NodeAllocationState holds the state required for allocation on a node.
type NodeAllocationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeAllocationStateSpec `json:"spec,omitempty"`
	Status string                  `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeAllocationStateList represents the "plural" of a NodeAllocationState CRD object.
type NodeAllocationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeAllocationState `json:"items"`
}
