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
	GpuKind = "Gpu"

	GpuStatusReady    = "Ready"
	GpuStatusNotReady = "NotReady"
)

type GpuConfig struct {
	Name      string
	Namespace string
	Owner     *metav1.OwnerReference
}

type Gpu struct {
	*nvcrd.Gpu
	clientset nvclientset.Interface
}
type GpuSpec = nvcrd.GpuSpec
type GpuList = nvcrd.GpuList

func NewGpu(config *GpuConfig, clientset nvclientset.Interface) *Gpu {
	object := &nvcrd.Gpu{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
	}

	if config.Owner != nil {
		object.OwnerReferences = []metav1.OwnerReference{*config.Owner}
	}

	gpucrd := &Gpu{
		object,
		clientset,
	}

	return gpucrd
}

func (g *Gpu) GetOrCreate() error {
	err := g.Get()
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		return g.Create()
	}
	return err
}

func (g *Gpu) Create() error {
	crd := g.Gpu.DeepCopy()
	crd, err := g.clientset.DraV1().Gpus(g.Namespace).Create(context.TODO(), crd, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.Gpu = *crd
	return nil
}

func (g *Gpu) Delete() error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.DraV1().Gpus(g.Namespace).Delete(context.TODO(), g.Gpu.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (g *Gpu) Update(spec *nvcrd.GpuSpec) error {
	crd := g.Gpu.DeepCopy()
	crd.Spec = *spec
	crd, err := g.clientset.DraV1().Gpus(g.Namespace).Update(context.TODO(), crd, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.Gpu = *crd
	return nil
}

func (g *Gpu) UpdateStatus(status string) error {
	crd := g.Gpu.DeepCopy()
	crd.Status = status
	crd, err := g.clientset.DraV1().Gpus(g.Namespace).Update(context.TODO(), crd, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.Gpu = *crd
	return nil
}

func (g *Gpu) Get() error {
	crd, err := g.clientset.DraV1().Gpus(g.Namespace).Get(context.TODO(), g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.Gpu = *crd
	return nil
}
