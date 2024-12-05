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

import "fmt"

// GpuDriver encodes the gpu driver as a string.
type GpuDriver string

const (
	NvidiaDriver  GpuDriver = "nvidia"
	VfioPciDriver GpuDriver = "vfio-pci"
)

// GpuDriverConfig holds the set of parameters for configuring a GPU with a driver.
type GpuDriverConfig struct {
	Driver GpuDriver `json:"driver"`
}

// DefaultGpuDriverConfig provides the default configuration of a GPU with a driver.
func DefaultGpuDriverConfig() *GpuDriverConfig {
	return &GpuDriverConfig{
		Driver: NvidiaDriver,
	}
}

// Normalize updates a GpuDriverConfig config with implied default values based on other settings.
func (c *GpuDriverConfig) Normalize() error {
	if c.Driver == "" {
		c.Driver = NvidiaDriver
	}
	return nil
}

// Validate ensures that GpuDriverConfig has a valid set of values.
func (c *GpuDriverConfig) Validate() error {
	switch c.Driver {
	case NvidiaDriver:
		fallthrough
	case VfioPciDriver:
		break
	default:
		return fmt.Errorf("invalid driver '%s' specified in gpu driver configuration", c.Driver)
	}
	return nil
}
