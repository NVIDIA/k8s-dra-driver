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
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	cm "k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	cmerrors "k8s.io/kubernetes/pkg/kubelet/checkpointmanager/errors"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1"
)

type ClaimAllocation map[string]sets.Int

type GpuStatus struct {
	sync.Mutex
	healthy      sets.Int
	unhealthy    sets.Int
	available    sets.Int
	allocated    ClaimAllocation
	checkpointer cm.CheckpointManager
}

func tryNvmlShutdown() {
	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		klog.Warningf("error shutting down NVML: %v", nvml.ErrorString(ret))
	}
}

func NewGpuStatus(config *Config) (*GpuStatus, error) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error initializing NVML: %v", nvml.ErrorString(ret))
	}
	defer tryNvmlShutdown()

	numGPUs, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device count: %v", nvml.ErrorString(ret))
	}

	gpuset := sets.NewInt()
	for i := 0; i < numGPUs; i++ {
		gpuset.Insert(i)
	}

	checkpointer, err := cm.NewCheckpointManager(DriverPluginPath)
	if err != nil {
		return nil, fmt.Errorf("error creating checkpoint manager: %v", err)
	}

	gpus := &GpuStatus{
		healthy:      sets.NewInt().Union(gpuset),
		unhealthy:    sets.NewInt(),
		available:    sets.NewInt().Union(gpuset),
		allocated:    make(ClaimAllocation),
		checkpointer: checkpointer,
	}

	checkpoint := NewCheckpoint()
	err = gpus.checkpointer.GetCheckpoint(DriverPluginCheckpointFile, checkpoint)
	if err != nil && err != cmerrors.ErrCheckpointNotFound {
		return nil, fmt.Errorf("error getting checkpoint file: %v", err)
	}

	if err == nil {
		gpus.allocated = checkpoint.Allocations
		for claimUid := range gpus.allocated {
			gpus.available = gpus.available.Difference(gpus.allocated[claimUid])
		}
		return gpus, nil
	}

	checkpoint = NewCheckpoint()
	checkpoint.Allocations = gpus.allocated
	err = gpus.checkpointer.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint)
	if err != nil {
		return nil, fmt.Errorf("error creating checkpoint file: %v", err)
	}

	return gpus, nil
}

func (g *GpuStatus) Allocate(claimUid string, count int) (sets.Int, error) {
	g.Lock()
	defer g.Unlock()

	if g.allocated[claimUid] != nil {
		return nil, fmt.Errorf("allocation already exists")
	}

	if g.available.Len() < count {
		return nil, fmt.Errorf("unable to satisfy allocation (available: %v)", g.available.Len())
	}

	checkpoint := NewCheckpoint()
	checkpoint.Allocations = g.allocated.DeepCopy()
	checkpoint.Allocations[claimUid] = sets.NewInt(g.available.List()[:count]...)

	err := g.checkpointer.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint)
	if err != nil {
		return nil, fmt.Errorf("error updating checkpoint file: %v", err)
	}

	g.allocated = checkpoint.Allocations
	g.available = g.available.Difference(g.allocated[claimUid])
	return g.allocated[claimUid], nil
}

func (g *GpuStatus) Free(claimUid string) error {
	g.Lock()
	defer g.Unlock()

	checkpoint := NewCheckpoint()
	checkpoint.Allocations = g.allocated.DeepCopy()
	delete(checkpoint.Allocations, claimUid)

	err := g.checkpointer.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint)
	if err != nil {
		return fmt.Errorf("error updating checkpoint file: %v", err)
	}

	g.available = g.available.Union(g.allocated[claimUid])
	g.allocated = checkpoint.Allocations
	return nil
}

func (g *GpuStatus) GetAvailable() sets.Int {
	g.Lock()
	defer g.Unlock()
	return g.available
}

func (g *GpuStatus) GetAllocated(claimUid string) sets.Int {
	g.Lock()
	defer g.Unlock()
	return g.allocated[claimUid]
}

func (g *GpuStatus) GetUpdatedSpec(inspec *nvcrd.GpuSpec) *nvcrd.GpuSpec {
	g.Lock()
	defer g.Unlock()
	outspec := inspec.DeepCopy()
	outspec.Capacity = g.healthy.Union(g.unhealthy).Len()
	outspec.Allocatable = g.healthy.Len()
	return outspec
}

func (ca ClaimAllocation) DeepCopy() ClaimAllocation {
	newca := make(ClaimAllocation)
	for claim, devices := range ca {
		newca[claim] = sets.NewInt().Union(devices)
	}
	return newca
}
