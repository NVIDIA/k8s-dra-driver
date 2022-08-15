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

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/k8s-dra-driver/pkg/controller"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

type migdriver struct {
}

func NewMigDriver() *migdriver {
	return &migdriver{}
}

func (g migdriver) Allocate(crd *nvcrd.NodeAllocationState, claim *corev1.ResourceClaim, claimSpec *nvcrd.MigDeviceClaimSpec, class *corev1.ResourceClass, classSpec *nvcrd.DeviceClassSpec, selectedNode string) error {
	return fmt.Errorf("MIG devices are not yet supported")
}

func (g migdriver) UnsuitableNodes(ctx context.Context, pod *v1.Pod, claims []*controller.ClaimAllocation, potentialNodes []string) error {
	return nil
}
