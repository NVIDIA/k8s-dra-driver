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
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// These constants represent the different Sharing strategies
const (
	TimeSlicingStrategy GpuSharingStrategy = "TimeSlicing"
	MpsStrategy         GpuSharingStrategy = "MPS"
)

// These constants represent the different TimeSlicing configurations
const (
	DefaultTimeSlice TimeSliceDuration = "Default"
	ShortTimeSlice   TimeSliceDuration = "Short"
	MediumTimeSlice  TimeSliceDuration = "Medium"
	LongTimeSlice    TimeSliceDuration = "Long"
)

// Sharing provides methods to check if a given sharing strategy is selected and grab its configuration
// +k8s:deepcopy-gen=false
type Sharing interface {
	IsTimeSlicing() bool
	IsMps() bool
	GetTimeSlicingConfig() (*TimeSlicingConfig, error)
	GetMpsConfig() (*MpsConfig, error)
}

// GpuSharingStrategy encodes the valid Sharing strategies as a string
// +kubebuilder:validation:Enum=TimeSlicing;MPS
type GpuSharingStrategy string

// MigDeviceSharingStrategy encodes the valid Sharing strategies as a string
// +kubebuilder:validation:Enum=MPS
type MigDeviceSharingStrategy string

// TimeSliceDuration encodes the valid timeslice duration as a string
// +kubebuilder:validation:Enum=Default;Short;Medium;Long
type TimeSliceDuration string

// MpsPinnedDeviceMemoryLimit holds the string representation of the limits across multiple devices
type MpsPinnedDeviceMemoryLimit map[string]resource.Quantity

// GpuSharing holds the current sharing strategy for GPUs and its settings
// +kubebuilder:validation:MaxProperties=2
type GpuSharing struct {
	// +kubebuilder:default=TimeSlicing
	// +kubebuilder:validation:Required
	Strategy          GpuSharingStrategy `json:"strategy"`
	TimeSlicingConfig *TimeSlicingConfig `json:"timeSlicingConfig,omitempty"`
	MpsConfig         *MpsConfig         `json:"mpsConfig,omitempty"`
}

// MigDeviceSharing holds the current sharing strategy for MIG Devices and its settings
// +kubebuilder:validation:MaxProperties=2
type MigDeviceSharing struct {
	// +kubebuilder:default=TimeSlicing
	// +kubebuilder:validation:Required
	Strategy  GpuSharingStrategy `json:"strategy"`
	MpsConfig *MpsConfig         `json:"mpsConfig,omitempty"`
}

// TimeSlicingSettings provides the settings for CUDA time-slicing.
type TimeSlicingConfig struct {
	// +kubebuilder:default=Default
	TimeSlice *TimeSliceDuration `json:"timeSlice,omitempty"`
}

// MpsConfig provides the configuring for an MPS control daemon.
type MpsConfig struct {
	MaxConnections          *int                       `json:"maxConnections,omitempty"`
	ActiveThreadPercentage  *int                       `json:"activeThreadPercentage,omitempty"`
	PinnedDeviceMemoryLimit MpsPinnedDeviceMemoryLimit `json:"pinnedDeviceMemoryLimit,omitempty"`
}

// IsTimeSlicing checks if the TimeSlicing strategy is applied
func (s *GpuSharing) IsTimeSlicing() bool {
	if s == nil {
		// TimeSlicing is the default strategy
		return true
	}
	return s.Strategy == TimeSlicingStrategy
}

// IsMps checks if the MPS strategy is applied
func (s *GpuSharing) IsMps() bool {
	if s == nil {
		return false
	}
	return s.Strategy == MpsStrategy
}

// IsTimeSlicing checks if the TimeSlicing strategy is applied
func (s *MigDeviceSharing) IsTimeSlicing() bool {
	return false
}

// IsMps checks if the MPS strategy is applied
func (s *MigDeviceSharing) IsMps() bool {
	if s == nil {
		return false
	}
	return s.Strategy == MpsStrategy
}

// GetTimeSlicingConfig returns the timeslicing config that applies to the given strategy
func (s *GpuSharing) GetTimeSlicingConfig() (*TimeSlicingConfig, error) {
	if s == nil {
		// TimeSlicing is the default strategy
		dts := DefaultTimeSlice
		return &TimeSlicingConfig{&dts}, nil
	}
	if s.Strategy != TimeSlicingStrategy {
		return nil, fmt.Errorf("strategy is not set to '%v'", TimeSlicingStrategy)
	}
	return s.TimeSlicingConfig, nil
}

// GetTimeSlicingConfig returns the timeslicing config that applies to the given strategy
func (s *MigDeviceSharing) GetTimeSlicingConfig() (*TimeSlicingConfig, error) {
	return nil, nil
}

// GetMpsConfig returns the MPS config that applies to the given strategy
func (s *GpuSharing) GetMpsConfig() (*MpsConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("no sharing set to get config from")
	}
	if s.Strategy != MpsStrategy {
		return nil, fmt.Errorf("strategy is not set to '%v'", MpsStrategy)
	}
	if s.TimeSlicingConfig != nil {
		return nil, fmt.Errorf("cannot use TimeSlicingConfig with the '%v' strategy", MpsStrategy)
	}
	return s.MpsConfig, nil
}

// GetMpsConfig returns the MPS config that applies to the given strategy
func (s *MigDeviceSharing) GetMpsConfig() (*MpsConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("no sharing set to get config from")
	}
	if s.Strategy != MpsStrategy {
		return nil, fmt.Errorf("strategy is not set to '%v'", MpsStrategy)
	}
	return s.MpsConfig, nil
}

// Int returns the integer representations of a timeslice duration
func (c TimeSliceDuration) Int() int {
	switch c {
	case DefaultTimeSlice:
		return 0
	case ShortTimeSlice:
		return 1
	case MediumTimeSlice:
		return 2
	case LongTimeSlice:
		return 3
	}
	return -1
}

// String formats MpsPinnedDeviceMemoryLimit for passing as an envvar
func (m MpsPinnedDeviceMemoryLimit) String() (string, error) {
	var limits []string
	for k, v := range m {
		_, err := strconv.Atoi(k)
		if err != nil {
			return "", fmt.Errorf("unable to parse key as an integer: %v", k)
		}

		value := v.Value() / 1024 / 1024
		if value == 0 {
			return "", fmt.Errorf("value set too low: %v", v)
		}

		limits = append(limits, fmt.Sprintf("%v=%vM", k, value))
	}
	return strings.Join(limits, ","), nil
}
