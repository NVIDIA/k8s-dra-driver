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
	"sync"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

type PerNodeAllocatedClaims struct {
	sync.RWMutex
	allocations map[string]map[string]nascrd.AllocatedDevices
}

func NewPerNodeAllocatedClaims() *PerNodeAllocatedClaims {
	return &PerNodeAllocatedClaims{
		allocations: make(map[string]map[string]nascrd.AllocatedDevices),
	}
}

func (p *PerNodeAllocatedClaims) Exists(claimUID, node string) bool {
	p.RLock()
	defer p.RUnlock()

	claimAllocation, exists := p.allocations[claimUID]
	if !exists {
		return false
	}

	_, exists = claimAllocation[node]

	return exists
}

func (p *PerNodeAllocatedClaims) Get(claimUID, node string) nascrd.AllocatedDevices {
	p.RLock()
	defer p.RUnlock()

	if !p.Exists(claimUID, node) {
		return nascrd.AllocatedDevices{}
	}
	return p.allocations[claimUID][node]
}

func (p *PerNodeAllocatedClaims) VisitNode(node string, visitor func(claimUID string, allocation nascrd.AllocatedDevices)) {
	p.RLock()
	for claimUID := range p.allocations {
		if allocation, exists := p.allocations[claimUID][node]; exists {
			p.RUnlock()
			visitor(claimUID, allocation)
			p.RLock()
		}
	}
	p.RUnlock()
}

func (p *PerNodeAllocatedClaims) Visit(visitor func(claimUID, node string, allocation nascrd.AllocatedDevices)) {
	p.RLock()
	for claimUID := range p.allocations {
		for node, allocation := range p.allocations[claimUID] {
			p.RUnlock()
			visitor(claimUID, node, allocation)
			p.RLock()
		}
	}
	p.RUnlock()
}

func (p *PerNodeAllocatedClaims) Set(claimUID, node string, devices nascrd.AllocatedDevices) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.allocations[claimUID]
	if !exists {
		p.allocations[claimUID] = make(map[string]nascrd.AllocatedDevices)
	}

	p.allocations[claimUID][node] = devices
}

func (p *PerNodeAllocatedClaims) RemoveNode(claimUID, node string) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.allocations[claimUID]
	if !exists {
		return
	}

	delete(p.allocations[claimUID], node)
	if len(p.allocations[claimUID]) == 0 {
		delete(p.allocations, claimUID)
	}
}

func (p *PerNodeAllocatedClaims) Remove(claimUID string) {
	p.Lock()
	defer p.Unlock()

	delete(p.allocations, claimUID)
}
