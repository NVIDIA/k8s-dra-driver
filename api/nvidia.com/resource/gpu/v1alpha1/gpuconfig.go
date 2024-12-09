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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GpuConfig holds the set of parameters for configuring a GPU.
type GpuConfig struct {
	metav1.TypeMeta `json:",inline"`
	Sharing         *GpuSharing      `json:"sharing,omitempty"`
	DriverConfig    *GpuDriverConfig `json:"driverConfig,omitempty"`
}

// DefaultGpuConfig provides the default GPU configuration.
func DefaultGpuConfig() *GpuConfig {
	return &GpuConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       GpuConfigKind,
		},
		Sharing: &GpuSharing{
			Strategy: TimeSlicingStrategy,
			TimeSlicingConfig: &TimeSlicingConfig{
				Interval: ptr.To(DefaultTimeSlice),
			},
		},
		DriverConfig: &GpuDriverConfig{
			Driver: NvidiaDriver,
		},
	}
}

// Normalize updates a GpuConfig config with implied default values based on other settings.
func (c *GpuConfig) Normalize() error {
	if c.DriverConfig == nil {
		c.DriverConfig = DefaultGpuDriverConfig()
	}

	if err := c.DriverConfig.Normalize(); err != nil {
		return err
	}

	// If sharing is not supported, don't proceed with normalizing its configuration.
	if !c.SupportsSharing() {
		return nil
	}

	if c.Sharing == nil {
		c.Sharing = &GpuSharing{
			Strategy: TimeSlicingStrategy,
		}
	}
	if c.Sharing.Strategy == TimeSlicingStrategy && c.Sharing.TimeSlicingConfig == nil {
		c.Sharing.TimeSlicingConfig = &TimeSlicingConfig{
			Interval: ptr.To(DefaultTimeSlice),
		}
	}
	if c.Sharing.Strategy == MpsStrategy && c.Sharing.MpsConfig == nil {
		c.Sharing.MpsConfig = &MpsConfig{}
	}
	return nil
}

// Validate ensures that GpuConfig has a valid set of values.
func (c *GpuConfig) Validate() error {
	if err := c.DriverConfig.Validate(); err != nil {
		return err
	}

	if c.SupportsSharing() {
		if c.Sharing == nil {
			return fmt.Errorf("no sharing strategy set")
		}
		if err := c.Sharing.Validate(); err != nil {
			return err
		}
	} else if c.Sharing != nil {
		return fmt.Errorf("sharing strategy cannot be provided while using non-nvidia driver")
	}
	return nil
}

func (c *GpuConfig) SupportsSharing() bool {
	return c.DriverConfig.Driver == NvidiaDriver
}
