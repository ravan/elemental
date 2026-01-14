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

package userdata

import (
	"context"
)

// Provider defines the interface for fetching user data from various sources.
type Provider interface {
	// Name returns the provider name (e.g., "aws", "azure", "gcp", "filesystem").
	Name() string
	// Fetch retrieves user data and returns structured data.
	Fetch(ctx context.Context) (*UserData, error)
	// Available checks if this provider is accessible in the current environment.
	Available(ctx context.Context) bool
}

// UserData holds the parsed user data in a structured format.
type UserData struct {
	// Raw contains the raw user data bytes as received from the provider.
	Raw []byte
	// Data contains the parsed YAML/JSON data as a nested map structure.
	Data map[string]any
	// Provider identifies which provider supplied this data.
	Provider string
}

// Config holds the configuration for user data fetching.
type Config struct {
	// Enabled indicates whether user data fetching is enabled.
	Enabled bool `yaml:"enabled"`
	// Providers lists the providers to try, in order. Use ["auto"] for auto-detection.
	Providers []string `yaml:"providers"`
	// Timeout is the timeout in seconds for fetching user data from each provider.
	Timeout int `yaml:"timeout"`
	// Retries is the number of retry attempts for transient failures.
	Retries int `yaml:"retries"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:   false,
		Providers: []string{"auto"},
		Timeout:   60,
		Retries:   3,
	}
}

const (
	// ProviderAuto indicates auto-detection of cloud provider.
	ProviderAuto = "auto"
	// ProviderAWS is the AWS IMDSv2 provider name.
	ProviderAWS = "aws"
	// ProviderAzure is the Azure IMDS provider name.
	ProviderAzure = "azure"
	// ProviderGCP is the GCP Metadata Server provider name.
	ProviderGCP = "gcp"
	// ProviderFilesystem is the filesystem provider name.
	ProviderFilesystem = "filesystem"
)
