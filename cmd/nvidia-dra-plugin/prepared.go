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

package main

import (
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
)

type PreparedDevices []*PreparedDeviceGroup
type PreparedClaims map[string]PreparedDevices

type PreparedDevice struct {
	Gpu *PreparedGpu       `json:"gpu"`
	Mig *PreparedMigDevice `json:"mig"`
}

type PreparedGpu struct {
	Info   *GpuInfo        `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

type PreparedMigDevice struct {
	Info   *MigDeviceInfo  `json:"info"`
	Device *drapbv1.Device `json:"device"`
}

type PreparedDeviceGroup struct {
	Devices     []*PreparedDevice `json:"devices"`
	ConfigState DeviceConfigState `json:"configState"`
}

func (d PreparedDevice) Type() string {
	if d.Gpu != nil {
		return GpuDeviceType
	}
	if d.Mig != nil {
		return MigDeviceType
	}
	return UnknownDeviceType
}

func (d *PreparedDevice) CanonicalName() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.Info.CanonicalName()
	case MigDeviceType:
		return d.Mig.Info.CanonicalName()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d *PreparedDevice) CanonicalIndex() string {
	switch d.Type() {
	case GpuDeviceType:
		return d.Gpu.Info.CanonicalIndex()
	case MigDeviceType:
		return d.Mig.Info.CanonicalIndex()
	}
	panic("unexpected type for AllocatableDevice")
}

func (d PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, group := range d {
		devices = append(devices, group.GetDevices()...)
	}
	return devices
}

func (d *PreparedDeviceGroup) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, device := range d.Devices {
		switch device.Type() {
		case GpuDeviceType:
			devices = append(devices, device.Gpu.Device)
		case MigDeviceType:
			devices = append(devices, device.Mig.Device)
		}
	}
	return devices
}

func (d PreparedDevices) GpuUUIDs() []string {
	var uuids []string
	for _, group := range d {
		uuids = append(uuids, group.GpuUUIDs()...)
	}
	return uuids
}

func (d *PreparedDeviceGroup) GpuUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == GpuDeviceType {
			uuids = append(uuids, device.Gpu.Info.UUID)
		}
	}
	return uuids
}

func (d PreparedDevices) MigDeviceUUIDs() []string {
	var uuids []string
	for _, group := range d {
		uuids = append(uuids, group.MigDeviceUUIDs()...)
	}
	return uuids
}

func (d *PreparedDeviceGroup) MigDeviceUUIDs() []string {
	var uuids []string
	for _, device := range d.Devices {
		if device.Type() == MigDeviceType {
			uuids = append(uuids, device.Mig.Info.UUID)
		}
	}
	return uuids
}

func (d *PreparedDeviceGroup) UUIDs() []string {
	return append(d.GpuUUIDs(), d.MigDeviceUUIDs()...)
}

func (d PreparedDevices) UUIDs() []string {
	return append(d.GpuUUIDs(), d.MigDeviceUUIDs()...)
}
