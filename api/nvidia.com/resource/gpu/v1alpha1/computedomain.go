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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced

// ComputeDomain prepares a set of nodes to run a multi-node workload in.
type ComputeDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ComputeDomainSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ComputeDomainList provides a list of ComputeDomains.
type ComputeDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ComputeDomain `json:"items"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.resourceClaimName) ? !has(self.deviceClassName) : has(self.deviceClassName))",message="Exactly one of 'resourceClaimName' or 'deviceClassName' must be set."

// ComputeDomainSpec provides the spec for a ComputeDomain.
type ComputeDomainSpec struct {
	NumNodes          int    `json:"numNodes"`
	ResourceClaimName string `json:"resourceClaimName,omitempty"`
	DeviceClassName   string `json:"deviceClassName,omitempty"`
}
