/*
 * Copyright (c) 2022-2023 NVIDIA CORPORATION.  All rights reserved.
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

	"github.com/urfave/cli/v2"

	plugin "k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
	"github.com/NVIDIA/k8s-dra-driver/pkg/flags"
)

const (
	DriverName = gpucrd.GroupName

	PluginRegistrationPath = "/var/lib/kubelet/plugins_registry/" + DriverName + ".sock"
	DriverPluginPath       = "/var/lib/kubelet/plugins/" + DriverName
	DriverPluginSocketPath = DriverPluginPath + "/plugin.sock"
)

type Flags struct {
	kubeClientConfig flags.KubeClientConfig
	nasConfig        flags.NasConfig
	loggingConfig    *flags.LoggingConfig

	cdiRoot string
}

type Config struct {
	flags *Flags
	// TODO: Rename to nascr
	nascrd *nascrd.NodeAllocationState
	// TODO: Rename to clientsets
	clientset flags.ClientSets
}

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	flags := &Flags{
		loggingConfig: flags.NewLoggingConfig(),
	}
	cliFlags := []cli.Flag{
		&cli.StringFlag{
			Name:        "cdi-root",
			Usage:       "Absolute path to the directory where CDI files will be generated.",
			Value:       "/etc/cdi",
			Destination: &flags.cdiRoot,
			EnvVars:     []string{"CDI_ROOT"},
		},
	}
	cliFlags = append(cliFlags, flags.kubeClientConfig.Flags()...)
	cliFlags = append(cliFlags, flags.nasConfig.Flags()...)
	cliFlags = append(cliFlags, flags.loggingConfig.Flags()...)

	app := &cli.App{
		Name:            "nvidia-dra-plugin",
		Usage:           "nvidia-dra-plugin implements a DRA driver plugin for NVIDIA GPUs.",
		ArgsUsage:       " ",
		HideHelpCommand: true,
		Flags:           cliFlags,
		Before: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", c.Args().Slice())
			}
			return flags.loggingConfig.Apply()
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			clientSets, err := flags.kubeClientConfig.NewClientSets()
			if err != nil {
				return fmt.Errorf("create client: %v", err)
			}

			nascr, err := flags.nasConfig.NewNodeAllocationState(ctx, clientSets.Core)
			if err != nil {
				return fmt.Errorf("create NodeAllocationState CR: %v", err)
			}

			config := &Config{
				flags:     flags,
				nascrd:    nascr,
				clientset: clientSets,
			}

			return StartPlugin(ctx, config)
		},
	}

	return app
}

func StartPlugin(ctx context.Context, config *Config) error {
	err := os.MkdirAll(DriverPluginPath, 0750)
	if err != nil {
		return err
	}

	info, err := os.Stat(config.flags.cdiRoot)
	switch {
	case err != nil && os.IsNotExist(err):
		err := os.MkdirAll(config.flags.cdiRoot, 0750)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("path for cdi file generation is not a directory: '%v'", config.flags.cdiRoot)
	}

	driver, err := NewDriver(ctx, config)
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

	err = driver.Shutdown(ctx)
	if err != nil {
		klog.Errorf("Unable to cleanly shutdown driver: %v", err)
	}

	return nil
}
