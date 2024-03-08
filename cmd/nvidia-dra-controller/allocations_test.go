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
	"testing"

	"github.com/stretchr/testify/assert"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/api/utils/types"
)

func Test_PerNodeAllocatedClaims(t *testing.T) {
	allocationClaims := &PerNodeAllocatedClaims{
		allocations: make(map[string]map[string]nascrd.AllocatedDevices),
	}

	// Test Exists()
	exists := allocationClaims.Exists("claim-not-exist", "fake-node")
	assert.Equal(t, false, exists)

	// Test Set()
	device1 := nascrd.AllocatedDevices{ClaimInfo: &types.ClaimInfo{Namespace: "default", Name: "device1"}}
	device2 := nascrd.AllocatedDevices{ClaimInfo: &types.ClaimInfo{Namespace: "default", Name: "device2"}}
	allocationClaims.Set("fake-claim", "fake-node", device1)
	allocationClaims.Set("fake-claim", "fake-node", device2)

	// Test Get()
	exists = allocationClaims.Exists("fake-claim", "fake-node")
	assert.Equal(t, true, exists)
	wantDevice := allocationClaims.Get("fake-claim", "fake-node")
	assert.Equal(t, device2, wantDevice)

	// Test Remove()
	allocationClaims.Remove("fake-claim")
	assert.Equal(t, allocationClaims.allocations, map[string]map[string]nascrd.AllocatedDevices{})

	// Test RemoveNode()
	allocationClaims.Set("fake-claim", "fake-node-1", device1)
	allocationClaims.Set("fake-claim", "fake-node-2", device2)
	allocationClaims.RemoveNode("fake-claim", "fake-node-1")
	assert.Equal(t, allocationClaims.allocations, map[string]map[string]nascrd.AllocatedDevices{"fake-claim": {"fake-node-2": device2}})
	allocationClaims.RemoveNode("fake-claim", "fake-node-2")
	assert.Equal(t, allocationClaims.allocations, map[string]map[string]nascrd.AllocatedDevices{})
}
