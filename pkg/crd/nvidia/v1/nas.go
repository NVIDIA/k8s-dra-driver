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

// MigDevicePlacement represents the placement of a MIG device within a GPU
type MigDevicePlacement struct {
	Start int `json:"start"`
	Size  int `json:"size"`
}

// AllocatableGpu represents an allocatable GPU on a node
type AllocatableGpu struct {
	Model      string `json:"model"`
	UUID       string `json:"uuid"`
	MigEnabled bool   `json:"migEnabled"`
}

// AllocatableMigDevices represents a set of allocatable MIG devices on a given type of GPU
type AllocatableMigDevices struct {
	Profile     string               `json:"profile"`
	ParentModel string               `json:"parentModel"`
	Placements  []MigDevicePlacement `json:"placements"`
}

// AllocatableDevices represents a set of allocatable devices on a node
type AllocatableDevices struct {
	Gpu *AllocatableGpu        `json:"gpu,omitempty"`
	Mig *AllocatableMigDevices `json:"mig,omitempty"`
}

// Type returns the type of AllocatableDevices this represents
func (d AllocatableDevices) Type() string {
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
	Model     string `json:"model"`
	UUID      string `json:"uuid"`
	CDIDevice string `json:"cdiDevice"`
}

// AllocatedMigDevice represents an allocated MIG device on a node
type AllocatedMigDevice struct {
	UUID        string             `json:"uuid"`
	Profile     string             `json:"profile"`
	ParentUUID  string             `json:"parentUUID"`
	ParentModel string             `json:"parentModel"`
	CDIDevice   string             `json:"cdiDevice"`
	Placement   MigDevicePlacement `json:"placement"`
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

// AllocatedDevices represents a list of allocated devices on a node
type AllocatedDevices []AllocatedDevice

// Type returns the type of AllocatedDevices this represents
func (d AllocatedDevices) Type() string {
	if len(d) == 0 {
		return UnknownDeviceType
	}
	return d[0].Type()
}

// RequestedGpu represents a GPU being requested for allocation
type RequestedGpu struct {
	UUID string `json:"uuid,omitempty"`
}

// RequestedMigDevice represents a MIG device being requested for allocation
type RequestedMigDevice struct {
	Profile    string             `json:"profile"`
	ParentUUID string             `json:"parentUUID"`
	Placement  MigDevicePlacement `json:"placement"`
}

// RequestedGpus represents a set of GPUs being requested for allocation
type RequestedGpus struct {
	Spec    GpuClaimParametersSpec `json:"spec"`
	Devices []RequestedGpu         `json:"devices"`
}

// RequestedMigDevices represents a set of MIG device being requested for allocation
type RequestedMigDevices struct {
	Spec    MigDeviceClaimParametersSpec `json:"spec"`
	Devices []RequestedMigDevice         `json:"devices"`
}

// RequestedDevices represents a list of requests for devices to be allocated
type RequestedDevices struct {
	Gpu *RequestedGpus       `json:"gpu,omitempty"`
	Mig *RequestedMigDevices `json:"mig,omitempty"`
}

// Type returns the type of RequestedDevices this represents
func (r RequestedDevices) Type() string {
	if r.Gpu != nil {
		return GpuDeviceType
	}
	if r.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

// NodeAllocationStateSpec is the spec for the NodeAllocationState CRD
type NodeAllocationStateSpec struct {
	AllocatableDevices []AllocatableDevices        `json:"allocatableDevices,omitempty"`
	ClaimRequests      map[string]RequestedDevices `json:"claimRequests,omitempty"`
	ClaimAllocations   map[string]AllocatedDevices `json:"claimAllocations,omitempty"`
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
