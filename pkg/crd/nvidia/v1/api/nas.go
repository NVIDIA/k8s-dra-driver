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

package api

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/clientset/versioned"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1"
)

const (
	NodeAllocationStateStatusReady    = "Ready"
	NodeAllocationStateStatusNotReady = "NotReady"
)

type NodeAllocationStateConfig struct {
	Name      string
	Namespace string
	Owner     *metav1.OwnerReference
}

type MigDevicePlacement = nvcrd.MigDevicePlacement
type AllocatableGpu = nvcrd.AllocatableGpu
type AllocatableMigDevice = nvcrd.AllocatableMigDevice
type AllocatableDevice = nvcrd.AllocatableDevice
type AllocatedGpu = nvcrd.AllocatedGpu
type AllocatedMigDevice = nvcrd.AllocatedMigDevice
type AllocatedDevice = nvcrd.AllocatedDevice
type AllocatedDevices = nvcrd.AllocatedDevices
type RequestedGpu = nvcrd.RequestedGpu
type RequestedMigDevice = nvcrd.RequestedMigDevice
type RequestedDevice = nvcrd.RequestedDevice
type RequestedDevicesSpec = nvcrd.RequestedDevicesSpec
type RequestedDevices = nvcrd.RequestedDevices
type NodeAllocationStateSpec = nvcrd.NodeAllocationStateSpec
type NodeAllocationStateList = nvcrd.NodeAllocationStateList

type NodeAllocationState struct {
	*nvcrd.NodeAllocationState
	clientset nvclientset.Interface
}

func NewNodeAllocationState(config *NodeAllocationStateConfig, clientset nvclientset.Interface) *NodeAllocationState {
	object := &nvcrd.NodeAllocationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
	}

	if config.Owner != nil {
		object.OwnerReferences = []metav1.OwnerReference{*config.Owner}
	}

	nascrd := &NodeAllocationState{
		object,
		clientset,
	}

	return nascrd
}

func (g *NodeAllocationState) GetOrCreate() error {
	err := g.Get()
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		return g.Create()
	}
	return err
}

func (g *NodeAllocationState) Create() error {
	crd := g.NodeAllocationState.DeepCopy()
	crd, err := g.clientset.DraV1().NodeAllocationStates(g.Namespace).Create(context.TODO(), crd, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.NodeAllocationState = *crd
	return nil
}

func (g *NodeAllocationState) Delete() error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.DraV1().NodeAllocationStates(g.Namespace).Delete(context.TODO(), g.NodeAllocationState.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (g *NodeAllocationState) Update(spec *nvcrd.NodeAllocationStateSpec) error {
	crd := g.NodeAllocationState.DeepCopy()
	crd.Spec = *spec
	crd, err := g.clientset.DraV1().NodeAllocationStates(g.Namespace).Update(context.TODO(), crd, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.NodeAllocationState = *crd
	return nil
}

func (g *NodeAllocationState) UpdateStatus(status string) error {
	crd := g.NodeAllocationState.DeepCopy()
	crd.Status = status
	crd, err := g.clientset.DraV1().NodeAllocationStates(g.Namespace).Update(context.TODO(), crd, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.NodeAllocationState = *crd
	return nil
}

func (g *NodeAllocationState) Get() error {
	crd, err := g.clientset.DraV1().NodeAllocationStates(g.Namespace).Get(context.TODO(), g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.NodeAllocationState = *crd
	return nil
}
