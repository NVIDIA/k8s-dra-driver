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
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/term"
	plugin "k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	nvclientset "github.com/NVIDIA/k8s-dra-driver/pkg/nvidia.com/resource/clientset/versioned"
	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1/api"
	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
)

const (
	DriverName     = gpucrd.GroupName
	DriverAPIGroup = gpucrd.GroupName

	PluginRegistrationPath = "/var/lib/kubelet/plugins_registry/" + DriverName + ".sock"
	DriverPluginPath       = "/var/lib/kubelet/plugins/" + DriverName
	DriverPluginSocketPath = DriverPluginPath + "/plugin.sock"
)

type Flags struct {
	kubeconfig   *string
	kubeAPIQPS   *float32
	kubeAPIBurst *int

	cdiRoot *string
}

type Config struct {
	flags    *Flags
	nascrd   *nascrd.NodeAllocationState
	nvclient nvclientset.Interface
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

	flags := AddFlags(cmd)

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Bind an environment variable to each input flag
		v := viper.New()
		v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		v.AutomaticEnv()
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if !f.Changed && v.IsSet(f.Name) {
				val := v.Get(f.Name)
				cmd.Flags().Set(f.Name, fmt.Sprintf("%v", val))
			}
		})
		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		csconfig, err := GetClientsetConfig(flags)
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

		crdconfig := &nascrd.NodeAllocationStateConfig{
			Name:      nodeName,
			Namespace: podNamespace,
			Owner: &metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       nodeName,
				UID:        node.UID,
			},
		}

		nascrd := nascrd.NewNodeAllocationState(crdconfig, nvclient)

		config := &Config{
			flags:    flags,
			nascrd:   nascrd,
			nvclient: nvclient,
		}

		return StartPlugin(config)
	}

	return cmd
}

func AddFlags(cmd *cobra.Command) *Flags {
	flags := &Flags{}
	sharedFlagSets := cliflag.NamedFlagSets{}

	fs := sharedFlagSets.FlagSet("Kubernetes client")
	flags.kubeconfig = fs.String("kubeconfig", "", "Absolute path to the kube.config file. Either this or KUBECONFIG need to be set if the driver is being run out of cluster.")
	flags.kubeAPIQPS = fs.Float32("kube-api-qps", 5, "QPS to use while communicating with the kubernetes apiserver.")
	flags.kubeAPIBurst = fs.Int("kube-api-burst", 10, "Burst to use while communicating with the kubernetes apiserver.")

	fs = sharedFlagSets.FlagSet("CDI")
	flags.cdiRoot = fs.String("cdi-root", "/etc/cdi", "Absolute path to the directory where CDI files will be generated.")

	fs = cmd.PersistentFlags()
	for _, f := range sharedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, sharedFlagSets, cols)

	return flags
}

func GetClientsetConfig(f *Flags) (*rest.Config, error) {
	var csconfig *rest.Config

	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		klog.Infof("Found KUBECONFIG environment variable set, using that..")
		*f.kubeconfig = kubeconfigEnv
	}

	var err error
	if *f.kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", *f.kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = *f.kubeAPIQPS
	csconfig.Burst = *f.kubeAPIBurst

	return csconfig, nil
}

func StartPlugin(config *Config) error {
	err := os.MkdirAll(DriverPluginPath, 0750)
	if err != nil {
		return err
	}

	info, err := os.Stat(*config.flags.cdiRoot)
	if err != nil && os.IsNotExist(err) {
		err := os.MkdirAll(*config.flags.cdiRoot, 0750)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("path for cdi file generation is not a directory: '%v'")
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
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-sigc

	dp.Stop()
	driver.Shutdown()

	return nil
}
