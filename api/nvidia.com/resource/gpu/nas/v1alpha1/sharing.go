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
)

// These constants represent the different Sharing strategies
const (
	TimeSlicingStrategy GpuSharingStrategy = "TimeSlicing"
)

// These constants represent the different TimeSlicing configurations
const (
	DefaultTimeSlice TimeSliceDuration = "Default"
	ShortTimeSlice   TimeSliceDuration = "Short"
	MediumTimeSlice  TimeSliceDuration = "Medium"
	LongTimeSlice    TimeSliceDuration = "Long"
)

// GpuSharingStrategy encodes the valid Sharing strategies as a string
// +kubebuilder:validation:Enum=TimeSlicing
type GpuSharingStrategy string

// TimeSliceDuration encodes the valid timeslice duration as a string
// +kubebuilder:validation:Enum=Default;Short;Medium;Long
type TimeSliceDuration string

// GpuSharing holds the current sharing strategy for GPUs and its settings
// +kubebuilder:validation:MaxProperties=2
type GpuSharing struct {
	// +kubebuilder:default=TimeSlicing
	// +kubebuilder:validation:Required
	Strategy          GpuSharingStrategy `json:"strategy"`
	TimeSlicingConfig *TimeSlicingConfig `json:"timeSlicingConfig,omitempty"`
}

// TimeSlicingSettings provides the settings for CUDA time-slicing.
type TimeSlicingConfig struct {
	// +kubebuilder:default=Default
	TimeSlice *TimeSliceDuration `json:"timeSlice,omitempty"`
}

// IsTimeSlicing checks if the TimeSlicing strategy is applied
func (s *GpuSharing) IsTimeSlicing() bool {
	if s == nil {
		// TimeSlicing is the default strategy
		return true
	}
	return s.Strategy == TimeSlicingStrategy
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
