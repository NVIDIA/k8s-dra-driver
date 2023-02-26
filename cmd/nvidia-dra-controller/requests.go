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

type PerNodeClaimRequests struct {
	sync.RWMutex
	requests map[string]map[string]nascrd.RequestedDevices
}

func NewPerNodeClaimRequests() *PerNodeClaimRequests {
	return &PerNodeClaimRequests{
		requests: make(map[string]map[string]nascrd.RequestedDevices),
	}
}

func (p *PerNodeClaimRequests) Exists(claimUID, node string) bool {
	p.RLock()
	defer p.RUnlock()

	_, exists := p.requests[claimUID]
	if !exists {
		return false
	}

	_, exists = p.requests[claimUID][node]
	if !exists {
		return false
	}

	return true
}

func (p *PerNodeClaimRequests) Get(claimUID, node string) nascrd.RequestedDevices {
	p.RLock()
	defer p.RUnlock()

	if !p.Exists(claimUID, node) {
		return nascrd.RequestedDevices{}
	}
	return p.requests[claimUID][node]
}

func (p *PerNodeClaimRequests) VisitNode(node string, visitor func(claimUID string, request nascrd.RequestedDevices)) {
	p.RLock()
	for claimUID := range p.requests {
		if request, exists := p.requests[claimUID][node]; exists {
			p.RUnlock()
			visitor(claimUID, request)
			p.RLock()
		}
	}
	p.RUnlock()
}

func (p *PerNodeClaimRequests) Visit(visitor func(claimUID, node string, request nascrd.RequestedDevices)) {
	p.RLock()
	for claimUID := range p.requests {
		for node, request := range p.requests[claimUID] {
			p.RUnlock()
			visitor(claimUID, node, request)
			p.RLock()
		}
	}
	p.RUnlock()
}

func (p *PerNodeClaimRequests) Set(claimUID, node string, devices nascrd.RequestedDevices) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.requests[claimUID]
	if !exists {
		p.requests[claimUID] = make(map[string]nascrd.RequestedDevices)
	}

	p.requests[claimUID][node] = devices
}

func (p *PerNodeClaimRequests) RemoveNode(claimUID, node string) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.requests[claimUID]
	if !exists {
		return
	}

	delete(p.requests[claimUID], node)
}

func (p *PerNodeClaimRequests) Remove(claimUID string) {
	p.Lock()
	defer p.Unlock()

	delete(p.requests, claimUID)
}
