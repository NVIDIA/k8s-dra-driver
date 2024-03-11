/*
 * Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
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
	"github.com/NVIDIA/k8s-dra-driver/api/utils/selector"
)

// GpuSelector defines the set of conditions that can be used to select a
// specific GPU. This condition can either be a single property from the set
// of available GpuSelectorProperties or a nested list of GpuSelectors that are
// 'anded' or 'ored' together.
//
// NOTE: Because CRDs do not allow recursively defined structs, we explicitly
// define up to 3 levels of nesting. This is done by defining new types with
// the same fields as this struct, up to 3 levels deep.
// +kubebuilder:validation:MaxProperties=1
type GpuSelector struct {
	*GpuSelectorProperties `json:",omitempty"`
	AndExpression          []GpuSelector1 `json:"andExpression,omitempty"`
	OrExpression           []GpuSelector1 `json:"orExpression,omitempty"`
}

// GpuSelector1 is a copy of GpuSelector to enable the first level of nesting
// +kubebuilder:validation:MaxProperties=1
type GpuSelector1 struct {
	*GpuSelectorProperties `json:",omitempty"`
	AndExpression          []GpuSelector2 `json:"andExpression,omitempty"`
	OrExpression           []GpuSelector2 `json:"orExpression,omitempty"`
}

// GpuSelector2 is a copy of GpuSelector to enable the second level of nesting
// +kubebuilder:validation:MaxProperties=1
type GpuSelector2 struct {
	*GpuSelectorProperties `json:",omitempty"`
	AndExpression          []GpuSelector3 `json:"andExpression,omitempty"`
	OrExpression           []GpuSelector3 `json:"orExpression,omitempty"`
}

// GpuSelector3 is a copy of GpuSelector to enable the third level of nesting
// +kubebuilder:validation:MaxProperties=1
type GpuSelector3 struct {
	*GpuSelectorProperties `json:",omitempty"`
}

// GpuSelectorProperties defines the full set of GPU properties that can be selected upon.
// +kubebuilder:validation:MaxProperties=1
type GpuSelectorProperties struct {
	Index                 *selector.IntProperty        `json:"index,omitempty"`
	UUID                  *selector.StringProperty     `json:"uuid,omitempty"`
	MigEnabled            *selector.BoolProperty       `json:"migEnabled,omitempty"`
	Memory                *selector.QuantityComparator `json:"memory,omitempty"`
	ProductName           *selector.GlobProperty       `json:"productName,omitempty"`
	Brand                 *selector.GlobProperty       `json:"brand,omitempty"`
	Architecture          *selector.GlobProperty       `json:"architecture,omitempty"`
	CUDAComputeCapability *selector.VersionComparator  `json:"cudaComputeCapability,omitempty"`
	DriverVersion         *selector.VersionComparator  `json:"driverVersion,omitempty"`
	CUDADriverVersion     *selector.VersionComparator  `json:"cudaDriverVersion,omitempty"`
}

// Matches evaluates a GpuSelector to see if it matches the boolean expression it represents
// Each individual Properties object is passed to the caller via a callback to
// compare it in isolation before combining the results.
func (s GpuSelector) Matches(compare func(*GpuSelectorProperties) bool) bool {
	return s.convert().Matches(compare)
}

// ToNamedResourcesSelector converts a GpuSelector into a selector for use with
// the NamedResources structured model
func (s GpuSelector) ToNamedResourcesSelector() string {
	return s.convert().ToNamedResourcesSelector()
}

// convert converts a GpuSelector into a generic Selector.
func (s GpuSelector) convert() selector.Selector[GpuSelectorProperties] {
	properties := s.GpuSelectorProperties
	if (properties != nil) && (*properties == GpuSelectorProperties{}) {
		properties = nil
	}
	ns := selector.Selector[GpuSelectorProperties]{
		Properties: properties,
	}
	for _, e := range s.AndExpression {
		ns.AndExpression = append(ns.AndExpression, e.convert())
	}
	for _, e := range s.OrExpression {
		ns.OrExpression = append(ns.OrExpression, e.convert())
	}
	return ns
}

// convert converts a GpuSelector1 into a generic Selector.
func (s GpuSelector1) convert() selector.Selector[GpuSelectorProperties] {
	properties := s.GpuSelectorProperties
	if (properties != nil) && (*properties == GpuSelectorProperties{}) {
		properties = nil
	}
	ns := selector.Selector[GpuSelectorProperties]{
		Properties: properties,
	}
	for _, e := range s.AndExpression {
		ns.AndExpression = append(ns.AndExpression, e.convert())
	}
	for _, e := range s.OrExpression {
		ns.OrExpression = append(ns.OrExpression, e.convert())
	}
	return ns
}

// convert converts a GpuSelector2 into a generic Selector.
func (s GpuSelector2) convert() selector.Selector[GpuSelectorProperties] {
	properties := s.GpuSelectorProperties
	if (properties != nil) && (*properties == GpuSelectorProperties{}) {
		properties = nil
	}
	ns := selector.Selector[GpuSelectorProperties]{
		Properties: properties,
	}
	for _, e := range s.AndExpression {
		ns.AndExpression = append(ns.AndExpression, e.convert())
	}
	for _, e := range s.OrExpression {
		ns.OrExpression = append(ns.OrExpression, e.convert())
	}
	return ns
}

// convert converts a GpuSelector3 into a generic Selector.
func (s GpuSelector3) convert() selector.Selector[GpuSelectorProperties] {
	properties := s.GpuSelectorProperties
	if (properties != nil) && (*properties == GpuSelectorProperties{}) {
		properties = nil
	}
	ns := selector.Selector[GpuSelectorProperties]{
		Properties: properties,
	}
	return ns
}

// ToNamedResourcesSelector defines the process of converting
// GpuSelectorProperties into a selector for use with the NamedResources
// structured model
func (p GpuSelectorProperties) ToNamedResourcesSelector() string {
	if p.Index != nil {
		return p.Index.ToNamedResourcesSelector("index")
	}
	if p.UUID != nil {
		return p.UUID.ToNamedResourcesSelector("uuid")
	}
	if p.MigEnabled != nil {
		return p.MigEnabled.ToNamedResourcesSelector("mig-enabled")
	}
	if p.Memory != nil {
		return p.Memory.ToNamedResourcesSelector("memory")
	}
	if p.ProductName != nil {
		return p.ProductName.ToNamedResourcesSelector("product-name")
	}
	if p.Brand != nil {
		return p.Brand.ToNamedResourcesSelector("brand")
	}
	if p.Architecture != nil {
		return p.Architecture.ToNamedResourcesSelector("architecture")
	}
	if p.CUDAComputeCapability != nil {
		return p.CUDAComputeCapability.ToNamedResourcesSelector("cuda-compute-capability")
	}
	if p.DriverVersion != nil {
		return p.DriverVersion.ToNamedResourcesSelector("driver-version")
	}
	if p.CUDADriverVersion != nil {
		return p.CUDADriverVersion.ToNamedResourcesSelector("cuda-driver-version")
	}
	return "()"
}
