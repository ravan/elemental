/*
Copyright © 2025-2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

// K8sDynamicFlags contains the flags for the k8s-dynamic command.
type K8sDynamicFlags struct {
	ConfigPath string
	Provider   string
	Timeout    int
	Retries    int
}

// K8sDynamicArgs holds the parsed k8s-dynamic command flags.
var K8sDynamicArgs K8sDynamicFlags

// NewK8sDynamicCommand creates the k8s-dynamic command with its subcommands.
func NewK8sDynamicCommand(appName string, applyAction func(*cli.Context) error) *cli.Command {
	return &cli.Command{
		Name:  "k8s-dynamic",
		Usage: "Manage dynamic Kubernetes configuration from cloud userdata",
		Subcommands: []*cli.Command{
			{
				Name:      "apply",
				Usage:     "Fetch user data and render Kubernetes config templates",
				UsageText: fmt.Sprintf("%s k8s-dynamic apply [OPTIONS]", appName),
				Action:    applyAction,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "config",
						Usage:       "Path to the configuration directory (optional, userdata is auto-detected from cloud metadata)",
						Destination: &K8sDynamicArgs.ConfigPath,
					},
					&cli.StringFlag{
						Name:        "provider",
						Usage:       "Override provider selection (auto, aws, azure, gcp, filesystem)",
						Destination: &K8sDynamicArgs.Provider,
					},
					&cli.IntFlag{
						Name:        "timeout",
						Usage:       "Timeout in seconds for fetching user data",
						Destination: &K8sDynamicArgs.Timeout,
					},
					&cli.IntFlag{
						Name:        "retries",
						Usage:       "Number of retry attempts",
						Destination: &K8sDynamicArgs.Retries,
					},
				},
			},
		},
	}
}
