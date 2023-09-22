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

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"k8s.io/client-go/util/retry"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	nasclient "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1/client"

	"github.com/NVIDIA/k8s-dra-driver/pkg/flags"
)

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	var (
		status string

		kubeClientConfig flags.KubeClientConfig
		loggingConfig    *flags.LoggingConfig
		nasConfig        flags.NasConfig
	)

	loggingConfig = flags.NewLoggingConfig()

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:     "status",
			Usage:    "The status to set [Ready | NotReady].",
			Required: true,
			Action: func(_ *cli.Context, value string) error {
				switch strings.ToLower(value) {
				case strings.ToLower(nascrd.NodeAllocationStateStatusReady):
					status = nascrd.NodeAllocationStateStatusReady
				case strings.ToLower(nascrd.NodeAllocationStateStatusNotReady):
					status = nascrd.NodeAllocationStateStatusNotReady
				default:
					return fmt.Errorf("unknown status: %s", value)
				}
				return nil
			},
			EnvVars: []string{"STATUS"},
		},
	}

	flags = append(flags, kubeClientConfig.Flags()...)
	flags = append(flags, loggingConfig.Flags()...)
	flags = append(flags, nasConfig.Flags()...)

	app := &cli.App{
		Name:            "set-nas-status",
		Usage:           "set-nas-status sets the status of the NodeAllocationState CRD managed by the DRA driver for GPUs.",
		ArgsUsage:       " ",
		HideHelpCommand: true,
		Flags:           flags,
		Before: func(ctx *cli.Context) error {
			if ctx.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", ctx.Args().Slice())
			}
			return loggingConfig.Apply()
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			clientSets, err := kubeClientConfig.NewClientSets()
			if err != nil {
				return fmt.Errorf("create client: %v", err)
			}

			nascr, err := nasConfig.NewNodeAllocationState(ctx, clientSets.Core)
			if err != nil {
				return fmt.Errorf("create NodeAllocationState CR: %v", err)
			}

			client := nasclient.New(nascr, clientSets.Nvidia.NasV1alpha1())
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// TODO: Should we pass the context here and for UpdateStatus?
				err := client.GetOrCreate()
				if err != nil {
					return err
				}
				return client.UpdateStatus(status)
			}); err != nil {
				return err
			}
			return nil
		},
	}

	return app
}
