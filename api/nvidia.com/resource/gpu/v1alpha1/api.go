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

const (
	GroupName = "gpu.resource.nvidia.com"
	Version   = "v1alpha1"

	GpuClaimParametersKind       = "GpuClaimParameters"
	MigDeviceClaimParametersKind = "MigDeviceClaimParameters"
)

func UpdateDeviceClassParametersSpecWithDefaults(oldspec *DeviceClassParametersSpec) *DeviceClassParametersSpec {
	newspec := &DeviceClassParametersSpec{}
	if oldspec != nil {
		newspec = oldspec.DeepCopy()
	}
	if newspec.Shareable == nil {
		shareable := true
		newspec.Shareable = &shareable
	}
	return newspec
}

func UpdateGpuClaimParametersSpecWithDefaults(oldspec *GpuClaimParametersSpec) *GpuClaimParametersSpec {
	newspec := &GpuClaimParametersSpec{}
	if oldspec != nil {
		newspec = oldspec.DeepCopy()
	}
	if newspec.Count == nil {
		count := 1
		newspec.Count = &count
	}
	return newspec
}

func UpdateMigDeviceClaimParametersSpecWithDefaults(oldspec *MigDeviceClaimParametersSpec) *MigDeviceClaimParametersSpec {
	newspec := &MigDeviceClaimParametersSpec{}
	if oldspec != nil {
		newspec = oldspec.DeepCopy()
	}
	return newspec
}
