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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClaimInfo holds the identifying information about a claim
type ClaimInfo struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

// MigDevicePlacement represents the placement of a MIG device within a GPU
type MigDevicePlacement struct {
	Start int `json:"start"`
	Size  int `json:"size"`
}

// AllocatableGpu represents an allocatable GPU on a node
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

// AllocatableMigDevice represents an allocatable MIG device (and its possible placements) on a given type of GPU
type AllocatableMigDevice struct {
	Profile           string               `json:"profile"`
	ParentProductName string               `json:"parentProductName"`
	Placements        []MigDevicePlacement `json:"placements"`
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
	UUID string `json:"uuid"`
}

// AllocatedMigDevice represents an allocated MIG device on a node
type AllocatedMigDevice struct {
	UUID       string             `json:"uuid"`
	Profile    string             `json:"profile"`
	ParentUUID string             `json:"parentUUID"`
	Placement  MigDevicePlacement `json:"placement"`
}

// AllocatedGpus represents a set of allocated GPUs
type AllocatedGpus struct {
	Devices []AllocatedGpu `json:"devices"`
}

// AllocatedMigDevices represents a set of allocated MIG devices
type AllocatedMigDevices struct {
	Devices []AllocatedMigDevice `json:"devices"`
}

// AllocatedDevices represents an allocated device on a node
type AllocatedDevices struct {
	Gpu *AllocatedGpus       `json:"gpu,omitempty"`
	Mig *AllocatedMigDevices `json:"mig,omitempty"`
}

// Type returns the type of AllocatedDevice this represents
func (d AllocatedDevices) Type() string {
	if d.Gpu != nil {
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
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
	Devices []RequestedGpu `json:"devices"`
	Sharing *GpuSharing    `json:"sharing,omitempty"`
}

// RequestedMigDevices represents a set of MIG device being requested for allocation
type RequestedMigDevices struct {
	Devices []RequestedMigDevice `json:"devices"`
	Sharing *MigDeviceSharing    `json:"sharing,omitempty"`
}

// RequestedDevices represents a list of requests for devices to be allocated
type RequestedDevices struct {
	ClaimInfo *ClaimInfo           `json:"claimInfo"`
	Gpu       *RequestedGpus       `json:"gpu,omitempty"`
	Mig       *RequestedMigDevices `json:"mig,omitempty"`
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
	AllocatableDevices []AllocatableDevice         `json:"allocatableDevices,omitempty"`
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
