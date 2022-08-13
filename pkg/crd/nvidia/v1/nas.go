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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Types of Devices that can be allocated
const (
	GpuDeviceType     = "gpu"
	MigDeviceType     = "mig"
	UnknownDeviceType = "unknown"
)

// AllocatableGpu represents an allocatable GPU on a node
type AllocatableGpu struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AllocatableMigDevice represents an allocatable MIG device on a node
type AllocatableMigDevice struct {
	Profile string `json:"profile"`
	Count   int    `json:"count"`
	Size    int    `json:"size"`
}

// AllocatableDevice represents an allocatable device on a node
type AllocatableDevice struct {
	Gpu *AllocatableGpu       `json:"gpu,omitempty"`
	Mig *AllocatableMigDevice `json:"mig,omitempty"`
}

// Type returns the type of AllocatableDevice this represents
func (d AllocatableDevice) Type() string {
	if d.Gpu != nil {
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

// AllocatedGpu represents an allocated GPU on a node
type AllocatedGpu struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// AllocatedMigDevice represents an allocated MIG device on a node
type AllocatedMigDevice struct {
	Name      string             `json:"name"`
	UUID      string             `json:"uuid"`
	Placement MigDevicePlacement `json:"placement"`
}

// MigDevicePlacement represents the physical placement of a MIG device relative to others
type MigDevicePlacement struct {
	GpuUUID string `json:"gpu"`
	Start   int    `json:"start"`
	Size    int    `json:"size"`
}

// AllocatedDevice represents an allocated device on a node
type AllocatedDevice struct {
	Gpu *AllocatedGpu       `json:"gpu,omitempty"`
	Mig *AllocatedMigDevice `json:"mig,omitempty"`
}

// Type returns the type of AllocatedDevice this represents
func (d AllocatedDevice) Type() string {
	if d.Gpu != nil {
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

// DeviceRequirements represents the set of rquirements for the device to be allocated
type DeviceRequirements struct {
	Gpu *GpuClaimSpec `json:"gpu,omitempty"`
}

// Type returns the type of DeviceRequirements this represents
func (r DeviceRequirements) Type() string {
	if r.Gpu != nil {
		return GpuDeviceType
	}
	return UnknownDeviceType
}

// NodeAllocationStateSpec is the spec for the NodeAllocationState CRD
type NodeAllocationStateSpec struct {
	AllocatableDevices []AllocatableDevice           `json:"allocatableDevices,omitempty"`
	ClaimRequirements  map[string]DeviceRequirements `json:"claimRequirements,omitempty"`
	ClaimAllocations   map[string][]AllocatedDevice  `json:"claimAllocations,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:singular=nas

// NodeAllocationState holds the state required for allocation on a node
type NodeAllocationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeAllocationStateSpec `json:"spec,omitempty"`
	Status string                  `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeAllocationStateList represents the "plural" of a NodeAllocationState CRD object
type NodeAllocationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeAllocationState `json:"items"`
}
