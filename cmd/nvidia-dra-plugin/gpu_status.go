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
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

type intSet sets.Int
type ClaimAllocation map[string]sets.Int

type GpuStatus struct {
	sync.Mutex
	healthy   sets.Int
	unhealthy sets.Int
	available sets.Int
	allocated ClaimAllocation
}

func tryNvmlShutdown() {
	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		klog.Warningf("error shutting down NVML: %v", nvml.ErrorString(ret))
	}
}

func NewGpuStatus(config *Config, gpucrd *nvcrd.Gpu) (*GpuStatus, error) {
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

	gpus := &GpuStatus{
		healthy:   sets.NewInt().Union(gpuset),
		unhealthy: sets.NewInt(),
		available: sets.NewInt().Union(gpuset),
		allocated: make(ClaimAllocation),
	}

	gpus.allocated.FromStringsMap(gpucrd.Spec.ClaimAllocations)
	for claimUid := range gpus.allocated {
		gpus.available = gpus.available.Difference(gpus.allocated[claimUid])
	}

	return gpus, nil
}

func (g *GpuStatus) Allocate(claimUid string, parameters nvcrd.GpuParameterSetSpec) (sets.Int, error) {
	g.Lock()
	defer g.Unlock()

	if g.allocated[claimUid] != nil {
		return nil, fmt.Errorf("allocation already exists")
	}

	if g.available.Len() < parameters.Count {
		return nil, fmt.Errorf("unable to satisfy allocation (available: %v)", g.available.Len())
	}

	g.allocated[claimUid] = sets.NewInt(g.available.List()[:parameters.Count]...)
	g.available = g.available.Difference(g.allocated[claimUid])
	return g.allocated[claimUid], nil
}

func (g *GpuStatus) Free(claimUid string) error {
	g.Lock()
	defer g.Unlock()

	g.available = g.available.Union(g.allocated[claimUid])
	delete(g.allocated, claimUid)
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
	outspec.ClaimAllocations = g.allocated.ToStringsMap()
	return outspec
}

func (is intSet) FromStrings(instrings []string) error {
	newis := sets.NewInt()
	for _, s := range instrings {
		i, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("unable to convert '%s' to integer: %v", s)
		}
		newis.Insert(i)
	}
	sets.Int(is).Delete(sets.Int(is).List()...)
	sets.Int(is).Insert(newis.List()...)
	return nil
}

func (is intSet) ToStrings() []string {
	var outstrings []string
	for _, i := range sets.Int(is).List() {
		outstrings = append(outstrings, fmt.Sprintf("%d", i))
	}
	return outstrings
}

func (ca ClaimAllocation) FromStringsMap(m map[string][]string) error {
	newca := make(ClaimAllocation)
	for claim, strs := range m {
		is := sets.NewInt()
		err := intSet(is).FromStrings(strs)
		if err != nil {
			return fmt.Errorf("error converting from strings for claim '%s': %v", claim, err)
		}
		newca[claim] = is
	}
	for claim := range ca {
		delete(ca, claim)
	}
	for claim := range newca {
		ca[claim] = newca[claim]
	}
	return nil
}

func (ca ClaimAllocation) ToStringsMap() map[string][]string {
	strsmap := make(map[string][]string)
	for claim, allocations := range ca {
		strsmap[claim] = intSet(allocations).ToStrings()
	}
	return strsmap
}

func (ca ClaimAllocation) DeepCopy() ClaimAllocation {
	newca := make(ClaimAllocation)
	for claim, devices := range ca {
		newca[claim] = sets.NewInt().Union(devices)
	}
	return newca
}
