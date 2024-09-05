/*
 * Copyright (c) 2022-2024, NVIDIA CORPORATION.  All rights reserved.
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
	"sync"

	resourceapi "k8s.io/api/resource/v1alpha2"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"
)

type driver struct {
	sync.Mutex
	doneCh chan struct{}
	state  *DeviceState
}

func NewDriver(ctx context.Context, config *Config) (*driver, error) {
	state, err := NewDeviceState(ctx, config)
	if err != nil {
		return nil, err
	}

	d := &driver{
		state: state,
	}

	return d, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	close(d.doneCh)
	return nil
}

func (d *driver) NodeListAndWatchResources(req *drapbv1.NodeListAndWatchResourcesRequest, stream drapbv1.Node_NodeListAndWatchResourcesServer) error {
	model := d.state.getResourceModelFromAllocatableDevices()
	resp := &drapbv1.NodeListAndWatchResourcesResponse{
		Resources: []*resourceapi.ResourceModel{&model},
	}

	if err := stream.Send(resp); err != nil {
		return err
	}

	for {
		select {
		case <-d.doneCh:
			return nil
		}
		// TODO: Update with case for when GPUs go unhealthy
	}
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drapbv1.NodePrepareResourcesRequest) (*drapbv1.NodePrepareResourcesResponse, error) {
	klog.Infof("NodePrepareResource is called: number of claims: %d", len(req.Claims))
	preparedResources := &drapbv1.NodePrepareResourcesResponse{Claims: map[string]*drapbv1.NodePrepareResourceResponse{}}

	// In production version some common operations of d.nodePrepareResources
	// could be done outside of this loop, for instance updating the CR could
	// be done once after all HW was prepared.
	for _, claim := range req.Claims {
		preparedResources.Claims[claim.Uid] = d.nodePrepareResource(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drapbv1.NodeUnprepareResourcesRequest) (*drapbv1.NodeUnprepareResourcesResponse, error) {
	klog.Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drapbv1.NodeUnprepareResourcesResponse{Claims: map[string]*drapbv1.NodeUnprepareResourceResponse{}}

	// In production version some common operations of d.nodeUnprepareResources
	// could be done outside of this loop, for instance updating the CR could
	// be done once after all HW was prepared.
	for _, claim := range req.Claims {
		unpreparedResources.Claims[claim.Uid] = d.nodeUnprepareResource(ctx, claim)
	}

	return unpreparedResources, nil
}

func (d *driver) nodePrepareResource(ctx context.Context, claim *drapbv1.Claim) *drapbv1.NodePrepareResourceResponse {
	d.Lock()
	defer d.Unlock()

	if len(claim.StructuredResourceHandle) == 0 {
		return &drapbv1.NodePrepareResourceResponse{
			Error: "driver only supports structured parameters",
		}
	}

	allocated, err := d.getAllocatedDevices(ctx, claim)
	if err != nil {
		return &drapbv1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("error allocating devices for claim %v: %v", claim.Uid, err),
		}
	}

	prepared, err := d.state.Prepare(ctx, claim.Uid, allocated)
	if err != nil {
		return &drapbv1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("error preparing devices for claim %v: %v", claim.Uid, err),
		}
	}

	klog.Infof("Returning newly prepared devices for claim '%v': %s", claim.Uid, prepared)
	return &drapbv1.NodePrepareResourceResponse{CDIDevices: prepared}
}

func (d *driver) nodeUnprepareResource(ctx context.Context, claim *drapbv1.Claim) *drapbv1.NodeUnprepareResourceResponse {
	d.Lock()
	defer d.Unlock()

	if len(claim.StructuredResourceHandle) == 0 {
		return &drapbv1.NodeUnprepareResourceResponse{
			Error: "driver only supports structured parameters",
		}
	}

	if err := d.state.Unprepare(ctx, claim.Uid); err != nil {
		return &drapbv1.NodeUnprepareResourceResponse{
			Error: fmt.Sprintf("error unpreparing devices for claim %v: %v", claim.Uid, err),
		}
	}

	return &drapbv1.NodeUnprepareResourceResponse{}
}

func (d *driver) getAllocatedDevices(ctx context.Context, claim *drapbv1.Claim) (AllocatedDevices, error) {
	allocated := AllocatedDevices{
		Gpu: &AllocatedGpus{},
	}

	// TODO: Defer enabling sharing with structured parameters until we update
	//       to the APIs for Kubernetes 1.31.
	//
	//vendorClaimParameters := claim.StructuredResourceHandle[0].VendorClaimParameters
	//if len(vendorClaimParameters.Raw) > 0 {
	//	var claimParams gpucrd.GpuClaimParametersSpec
	//	if err := json.Unmarshal(vendorClaimParameters.Raw, &claimParams); err != nil {
	//		return fmt.Errorf("decoding claim parameters: %v", err)
	//	}
	//	allocated.Gpu.Sharing = claimParams.Sharing
	//}

	for _, r := range claim.StructuredResourceHandle[0].Results {
		name := r.AllocationResultModel.NamedResources.Name
		gpu := fmt.Sprintf("GPU-%s", name[4:])
		allocated.Gpu.Devices = append(allocated.Gpu.Devices, gpu)
	}

	return allocated, nil
}

func (d *driver) cleanupCDIFiles(wg *sync.WaitGroup) chan error {
	// TODO: implement loop to remove CDI files from the CDI path for claimUIDs
	// that have been removed from the AllocatedClaims map.
	errors := make(chan error)
	return errors
}

func (d *driver) cleanupMpsControlDaemonArtifacts(wg *sync.WaitGroup) chan error {
	// TODO: implement loop to remove mpsControlDaemon folders from the mps
	// path for claimUIDs that have been removed from the AllocatedClaims map.
	errors := make(chan error)
	return errors
}
