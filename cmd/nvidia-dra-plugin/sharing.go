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

package main

import (
	"fmt"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

type TimeSlicingManager struct {
	nvidiaDriverRoot string
}

func NewTimeSlicingManager(nvidiaDriverRoot string) *TimeSlicingManager {
	return &TimeSlicingManager{
		nvidiaDriverRoot: nvidiaDriverRoot,
	}
}

func (t *TimeSlicingManager) SetTimeSlice(devices *AllocatedDevices, config *nascrd.TimeSlicingConfig) error {
	if devices.Mig != nil {
		return fmt.Errorf("setting a TimeSlice duration on MIG devices is unsupported")
	}

	timeSlice := nascrd.DefaultTimeSlice
	if config != nil && config.TimeSlice != nil {
		timeSlice = *config.TimeSlice
	}

	err := setTimeSlice(t.nvidiaDriverRoot, devices.UUIDs(), timeSlice.Int())
	if err != nil {
		return fmt.Errorf("error setting time slice: %v", err)
	}

	return nil
}
