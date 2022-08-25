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

package api

import (
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1"
)

type DeviceSelector = nvcrd.DeviceSelector
type DeviceClassSpec = nvcrd.DeviceClassSpec
type DeviceClass = nvcrd.DeviceClass
type DeviceClassList = nvcrd.DeviceClassList

func DefaultDeviceClassSpec() DeviceClassSpec {
	return DeviceClassSpec{
		DeviceSelector: []DeviceSelector{
			{
				Type: GpuDeviceType,
				Name: "*",
			},
		},
	}
}
