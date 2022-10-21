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
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	plugin "k8s.io/dynamic-resource-allocation/kubeletplugin"

	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/clientset/versioned"
	nvcrd "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia/v1/api"
)

const (
	DriverName     = nvcrd.GroupName
	DriverAPIGroup = nvcrd.GroupName

	PluginRegistrationPath = "/var/lib/kubelet/plugins_registry/" + DriverName + ".sock"
	DriverPluginPath       = "/var/lib/kubelet/plugins/" + DriverName
	DriverPluginSocketPath = DriverPluginPath + "/plugin.sock"

	cdiVersion = "0.4.0"
	cdiRoot    = "/etc/cdi"
	cdiVendor  = "nvidia.com"
	cdiKind    = cdiVendor + "/gpu"

	KubeApiQps   = 5
	KubeApiBurst = 10
)

type Clientset struct {
	core   coreclientset.Interface
	nvidia nvclientset.Interface
}

type Config struct {
	crdconfig *nvcrd.NodeAllocationStateConfig
	clientset *Clientset
}

func main() {
	command := NewCommand()
	err := command.Execute()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

// NewCommand creates a *cobra.Command object with default parameters.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "nvidia-dra-plugin",
		Long: "nvidia-dra-plugin implements a DRA driver plugin.",
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		csconfig, err := GetClientsetConfig()
		if err != nil {
			return fmt.Errorf("create client configuration: %v", err)
		}

		coreclient, err := coreclientset.NewForConfig(csconfig)
		if err != nil {
			return fmt.Errorf("create core client: %v", err)
		}

		nvclient, err := nvclientset.NewForConfig(csconfig)
		if err != nil {
			return fmt.Errorf("create nvidia client: %v", err)
		}

		nodeName := os.Getenv("NODE_NAME")
		podNamespace := os.Getenv("POD_NAMESPACE")

		node, err := coreclient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get node object: %v", err)
		}

		config := &Config{
			crdconfig: &nvcrd.NodeAllocationStateConfig{
				Name:      nodeName,
				Namespace: podNamespace,
				Owner: &metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Node",
					Name:       nodeName,
					UID:        node.UID,
				},
			},
			clientset: &Clientset{
				coreclient,
				nvclient,
			},
		}

		return StartPlugin(config)
	}

	return cmd
}

func GetClientsetConfig() (*rest.Config, error) {
	var csconfig *rest.Config
	kubeconfig := os.Getenv("KUBECONFIG")

	var err error
	if kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = KubeApiQps
	csconfig.Burst = KubeApiBurst

	return csconfig, nil
}

func StartPlugin(config *Config) error {
	err := os.MkdirAll(DriverPluginPath, 0750)
	if err != nil {
		return err
	}

	err = os.MkdirAll(cdiRoot, 0750)
	if err != nil {
		return err
	}

	driver, err := NewDriver(config)
	if err != nil {
		return err
	}

	dp, err := plugin.Start(
		driver,
		plugin.DriverName(DriverName),
		plugin.RegistrarSocketPath(PluginRegistrationPath),
		plugin.PluginSocketPath(DriverPluginSocketPath),
		plugin.KubeletPluginSocketPath(DriverPluginSocketPath))
	if err != nil {
		return err
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	<-sigc
	dp.Stop()

	return nil
}
