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

package main

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha1"

	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"

	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

type driver struct {
	config *Config
	gpucrd *nvcrd.Gpu
	state  *DeviceState
}

func NewDriver(config *Config) (*driver, error) {
	gpucrd := nvcrd.NewGpu(config.crdconfig, config.clientset.nvidia)
	err := gpucrd.GetOrCreate()
	if err != nil {
		return nil, err
	}

	state, err := NewDeviceState(config, gpucrd)
	if err != nil {
		return nil, err
	}

	err = gpucrd.Update(state.GetUpdatedSpec(&gpucrd.Spec))
	if err != nil {
		return nil, err
	}

	err = gpucrd.UpdateStatus(nvcrd.GpuStatusReady)
	if err != nil {
		return nil, err
	}

	d := &driver{
		config: config,
		gpucrd: gpucrd,
		state:  state,
	}

	return d, nil
}

func (d *driver) NodePrepareResource(ctx context.Context, req *drapbv1.NodePrepareResourceRequest) (*drapbv1.NodePrepareResourceResponse, error) {
	klog.Infof("NodePrepareResource is called: request: %+v", req)

	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)
	err := cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	allocated := d.state.GetAllocated(req.ClaimUid)
	if allocated == nil {
		allocated, err = d.Allocate(req.ClaimUid)
		if err != nil {
			return nil, fmt.Errorf("error allocating devices for claim '%v': %v", req.ClaimUid, err)
		}
	}

	err = d.gpucrd.Update(d.state.GetUpdatedSpec(&d.gpucrd.Spec))
	if err != nil {
		d.state.Free(req.ClaimUid)
		return nil, fmt.Errorf("error updating Gpu CRD: %v", err)
	}

	var devs []string
	for _, i := range allocated.List() {
		devs = append(devs, cdi.DeviceDB().GetDevice(fmt.Sprintf("%s=gpu%d", cdiKind, i)).GetQualifiedName())
	}

	klog.Infof("Allocated devices for claim '%v': %s", req.ClaimUid, devs)
	return &drapbv1.NodePrepareResourceResponse{CdiDevice: devs}, nil
}

func (d *driver) NodeUnprepareResource(ctx context.Context, req *drapbv1.NodeUnprepareResourceRequest) (*drapbv1.NodeUnprepareResourceResponse, error) {
	klog.Infof("NodeUnprepareResource is called: request: %+v", req)

	err := d.Free(req.ClaimUid)
	if err != nil {
		return nil, fmt.Errorf("error freeing devices for claim '%v': %v", req.ClaimUid, err)
	}

	err = d.gpucrd.Update(d.state.GetUpdatedSpec(&d.gpucrd.Spec))
	if err != nil {
		return nil, fmt.Errorf("error updating Gpu CRD: %v", err)
	}

	klog.Infof("Freed devices for claim '%v'", req.ClaimUid)
	return &drapbv1.NodeUnprepareResourceResponse{}, nil
}

func (d *driver) Allocate(claimUid string) (sets.Int, error) {
	err := d.gpucrd.Get()
	if err != nil {
		return nil, err
	}
	allocated, err := d.state.Allocate(claimUid, d.gpucrd.Spec.ClaimRequirements[claimUid])
	if err != nil {
		return nil, err
	}
	err = d.gpucrd.Update(d.state.GetUpdatedSpec(&d.gpucrd.Spec))
	if err != nil {
		return nil, fmt.Errorf("error updating Gpu CRD: %v", err)
	}
	return allocated, nil
}

func (d *driver) Free(claimUid string) error {
	err := d.gpucrd.Get()
	if err != nil {
		return err
	}
	allocated := d.state.GetAllocated(claimUid)
	if allocated == nil {
		return nil
	}
	d.state.Free(claimUid)
	return nil
}
