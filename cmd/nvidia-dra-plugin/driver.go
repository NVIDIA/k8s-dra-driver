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

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha2"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	nasclient "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1/client"
)

type driver struct {
	nascrd    *nascrd.NodeAllocationState
	nasclient *nasclient.Client
	state     *DeviceState
}

func NewDriver(config *Config) (*driver, error) {
	var d *driver
	client := nasclient.New(config.nascrd, config.nvclient.NasV1alpha1())
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := client.GetOrCreate()
		if err != nil {
			return err
		}

		err = client.UpdateStatus(nascrd.NodeAllocationStateStatusNotReady)
		if err != nil {
			return err
		}

		state, err := NewDeviceState(config)
		if err != nil {
			return err
		}

		err = client.Update(state.GetUpdatedSpec(&config.nascrd.Spec))
		if err != nil {
			return err
		}

		err = client.UpdateStatus(nascrd.NodeAllocationStateStatusReady)
		if err != nil {
			return err
		}

		d = &driver{
			nascrd:    config.nascrd,
			nasclient: client,
			state:     state,
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (d *driver) Shutdown() error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.nasclient.Get()
		if err != nil {
			return err
		}
		return d.nasclient.UpdateStatus(nascrd.NodeAllocationStateStatusNotReady)
	})
}

func (d *driver) NodePrepareResource(ctx context.Context, req *drapbv1.NodePrepareResourceRequest) (*drapbv1.NodePrepareResourceResponse, error) {
	klog.Infof("NodePrepareResource is called: request: %+v", req)

	var err error
	var allocated []string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		allocated, err = d.Allocate(req.ClaimUid)
		if err != nil {
			return fmt.Errorf("error allocating devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.nasclient.Update(d.state.GetUpdatedSpec(&d.nascrd.Spec))
		if err != nil {
			d.state.Free(req.ClaimUid)
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error preparing resource: %v", err)
	}

	klog.Infof("Allocated devices for claim '%v': %s", req.ClaimUid, allocated)
	return &drapbv1.NodePrepareResourceResponse{CdiDevices: allocated}, nil
}

func (d *driver) NodeUnprepareResource(ctx context.Context, req *drapbv1.NodeUnprepareResourceRequest) (*drapbv1.NodeUnprepareResourceResponse, error) {
	klog.Infof("NodeUnprepareResource is called: request: %+v", req)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.Free(req.ClaimUid)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.nasclient.Update(d.state.GetUpdatedSpec(&d.nascrd.Spec))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error unpreparing resource: %v", err)
	}

	klog.Infof("Freed devices for claim '%v'", req.ClaimUid)
	return &drapbv1.NodeUnprepareResourceResponse{}, nil
}

func (d *driver) Allocate(claimUid string) ([]string, error) {
	err := d.nasclient.Get()
	if err != nil {
		return nil, err
	}
	allocated, err := d.state.Allocate(claimUid, d.nascrd.Spec.ClaimRequests[claimUid])
	if err != nil {
		return nil, err
	}
	return allocated, nil
}

func (d *driver) Free(claimUid string) error {
	err := d.nasclient.Get()
	if err != nil {
		return err
	}
	err = d.state.Free(claimUid)
	if err != nil {
		return err
	}
	return nil
}
