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
	"fmt"
	"time"

	"github.com/suse/elemental/v3/pkg/sys"
)

// ProviderFactory creates a Provider instance.
type ProviderFactory func(s *sys.System) Provider

// registry holds the registered provider factories.
var registry = map[string]ProviderFactory{
	ProviderAWS:        NewAWSProvider,
	ProviderAzure:      NewAzureProvider,
	ProviderGCP:        NewGCPProvider,
	ProviderFilesystem: NewFilesystemProvider,
}

// defaultProviderOrder defines the order in which providers are tried during auto-detection.
var defaultProviderOrder = []string{
	ProviderAWS,
	ProviderAzure,
	ProviderGCP,
	ProviderFilesystem,
}

// GetProviders returns a list of Provider instances based on the configuration.
// If the provider list contains "auto", all registered providers are returned in default order.
func GetProviders(s *sys.System, cfg Config) []Provider {
	var providers []Provider

	providerNames := cfg.Providers
	if len(providerNames) == 0 || (len(providerNames) == 1 && providerNames[0] == ProviderAuto) {
		providerNames = defaultProviderOrder
	}

	for _, name := range providerNames {
		if name == ProviderAuto {
			continue
		}

		factory, ok := registry[name]
		if !ok {
			continue
		}

		providers = append(providers, factory(s))
	}

	return providers
}

// Detect auto-detects the current cloud environment and returns the first available provider.
func Detect(ctx context.Context, s *sys.System) (Provider, error) {
	providers := GetProviders(s, Config{Providers: []string{ProviderAuto}})

	for _, p := range providers {
		if p.Available(ctx) {
			return p, nil
		}
	}

	return nil, ErrNoProviderAvailable
}

// FetchUserData fetches user data using the configuration settings.
func FetchUserData(ctx context.Context, s *sys.System, cfg Config) (*UserData, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("user data fetching is not enabled")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	providers := GetProviders(s, cfg)
	if len(providers) == 0 {
		return nil, ErrNoProviderAvailable
	}

	retries := cfg.Retries
	if retries <= 0 {
		retries = 3
	}

	return FetchWithRetry(ctx, retries, providers...)
}
