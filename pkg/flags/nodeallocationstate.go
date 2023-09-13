/*
 * Copyright 2023 The Kubernetes Authors.
 * Copyright 2023 NVIDIA CORPORATION.
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

package flags

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

type NasConfig struct {
	NodeName  string
	Namespace string

	HideNodeName bool
}

func (n *NasConfig) Flags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "namespace",
			Usage:       "The namespace used for the custom resources.",
			Value:       "default",
			Destination: &n.Namespace,
			EnvVars:     []string{"NAMESPACE"},
		},
	}
	if !n.HideNodeName {
		flags = append(flags, &cli.StringFlag{
			Name:        "node-name",
			Usage:       "The name of the node to be worked on.",
			Required:    true,
			Destination: &n.NodeName,
			EnvVars:     []string{"NODE_NAME"},
		})
	}

	return flags
}

func (n *NasConfig) NewNodeAllocationState(ctx context.Context, core coreclientset.Interface) (*nascrd.NodeAllocationState, error) {
	node, err := core.CoreV1().Nodes().Get(ctx, n.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get node object: %v", err)
	}

	crdconfig := &nascrd.NodeAllocationStateConfig{
		Name:      n.NodeName,
		Namespace: n.Namespace,
		Owner: &metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       n.NodeName,
			UID:        node.UID,
		},
	}
	nascr := nascrd.NewNodeAllocationState(crdconfig)
	return nascr, nil
}
