//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuConfig) DeepCopyInto(out *GpuConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.Sharing != nil {
		in, out := &in.Sharing, &out.Sharing
		*out = new(GpuSharing)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuConfig.
func (in *GpuConfig) DeepCopy() *GpuConfig {
	if in == nil {
		return nil
	}
	out := new(GpuConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GpuConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuSharing) DeepCopyInto(out *GpuSharing) {
	*out = *in
	if in.TimeSlicingConfig != nil {
		in, out := &in.TimeSlicingConfig, &out.TimeSlicingConfig
		*out = new(TimeSlicingConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.MpsConfig != nil {
		in, out := &in.MpsConfig, &out.MpsConfig
		*out = new(MpsConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuSharing.
func (in *GpuSharing) DeepCopy() *GpuSharing {
	if in == nil {
		return nil
	}
	out := new(GpuSharing)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ImexChannelConfig) DeepCopyInto(out *ImexChannelConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ImexChannelConfig.
func (in *ImexChannelConfig) DeepCopy() *ImexChannelConfig {
	if in == nil {
		return nil
	}
	out := new(ImexChannelConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ImexChannelConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ImexDaemonConfig) DeepCopyInto(out *ImexDaemonConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ImexDaemonConfig.
func (in *ImexDaemonConfig) DeepCopy() *ImexDaemonConfig {
	if in == nil {
		return nil
	}
	out := new(ImexDaemonConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ImexDaemonConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MigDeviceConfig) DeepCopyInto(out *MigDeviceConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.Sharing != nil {
		in, out := &in.Sharing, &out.Sharing
		*out = new(MigDeviceSharing)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MigDeviceConfig.
func (in *MigDeviceConfig) DeepCopy() *MigDeviceConfig {
	if in == nil {
		return nil
	}
	out := new(MigDeviceConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MigDeviceConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MigDeviceSharing) DeepCopyInto(out *MigDeviceSharing) {
	*out = *in
	if in.MpsConfig != nil {
		in, out := &in.MpsConfig, &out.MpsConfig
		*out = new(MpsConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MigDeviceSharing.
func (in *MigDeviceSharing) DeepCopy() *MigDeviceSharing {
	if in == nil {
		return nil
	}
	out := new(MigDeviceSharing)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MpsConfig) DeepCopyInto(out *MpsConfig) {
	*out = *in
	if in.DefaultActiveThreadPercentage != nil {
		in, out := &in.DefaultActiveThreadPercentage, &out.DefaultActiveThreadPercentage
		*out = new(int)
		**out = **in
	}
	if in.DefaultPinnedDeviceMemoryLimit != nil {
		in, out := &in.DefaultPinnedDeviceMemoryLimit, &out.DefaultPinnedDeviceMemoryLimit
		x := (*in).DeepCopy()
		*out = &x
	}
	if in.DefaultPerDevicePinnedMemoryLimit != nil {
		in, out := &in.DefaultPerDevicePinnedMemoryLimit, &out.DefaultPerDevicePinnedMemoryLimit
		*out = make(MpsPerDevicePinnedMemoryLimit, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MpsConfig.
func (in *MpsConfig) DeepCopy() *MpsConfig {
	if in == nil {
		return nil
	}
	out := new(MpsConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in MpsPerDevicePinnedMemoryLimit) DeepCopyInto(out *MpsPerDevicePinnedMemoryLimit) {
	{
		in := &in
		*out = make(MpsPerDevicePinnedMemoryLimit, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MpsPerDevicePinnedMemoryLimit.
func (in MpsPerDevicePinnedMemoryLimit) DeepCopy() MpsPerDevicePinnedMemoryLimit {
	if in == nil {
		return nil
	}
	out := new(MpsPerDevicePinnedMemoryLimit)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultiNodeEnvironment) DeepCopyInto(out *MultiNodeEnvironment) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultiNodeEnvironment.
func (in *MultiNodeEnvironment) DeepCopy() *MultiNodeEnvironment {
	if in == nil {
		return nil
	}
	out := new(MultiNodeEnvironment)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MultiNodeEnvironment) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultiNodeEnvironmentList) DeepCopyInto(out *MultiNodeEnvironmentList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MultiNodeEnvironment, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultiNodeEnvironmentList.
func (in *MultiNodeEnvironmentList) DeepCopy() *MultiNodeEnvironmentList {
	if in == nil {
		return nil
	}
	out := new(MultiNodeEnvironmentList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MultiNodeEnvironmentList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultiNodeEnvironmentSpec) DeepCopyInto(out *MultiNodeEnvironmentSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultiNodeEnvironmentSpec.
func (in *MultiNodeEnvironmentSpec) DeepCopy() *MultiNodeEnvironmentSpec {
	if in == nil {
		return nil
	}
	out := new(MultiNodeEnvironmentSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TimeSlicingConfig) DeepCopyInto(out *TimeSlicingConfig) {
	*out = *in
	if in.Interval != nil {
		in, out := &in.Interval, &out.Interval
		*out = new(TimeSliceInterval)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TimeSlicingConfig.
func (in *TimeSlicingConfig) DeepCopy() *TimeSlicingConfig {
	if in == nil {
		return nil
	}
	out := new(TimeSlicingConfig)
	in.DeepCopyInto(out)
	return out
}
