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
	"errors"
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

var (
	// ErrNoProviderAvailable is returned when no provider is available.
	ErrNoProviderAvailable = errors.New("no user data provider available")
	// ErrNoUserData is returned when no user data could be fetched.
	ErrNoUserData = errors.New("no user data found")
)

// Fetch attempts to get user data from the given providers in order,
// returning the first successful result.
func Fetch(ctx context.Context, providers ...Provider) (*UserData, error) {
	if len(providers) == 0 {
		return nil, ErrNoProviderAvailable
	}

	var lastErr error
	for _, p := range providers {
		if !p.Available(ctx) {
			continue
		}

		data, err := p.Fetch(ctx)
		if err != nil {
			lastErr = err
			continue
		}

		return data, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("fetching user data: %w", lastErr)
	}

	return nil, ErrNoProviderAvailable
}

// FetchWithRetry attempts to fetch user data with retry logic and exponential backoff.
func FetchWithRetry(ctx context.Context, maxRetries int, providers ...Provider) (*UserData, error) {
	if maxRetries < 1 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := range maxRetries {
		data, err := Fetch(ctx, providers...)
		if err == nil {
			return data, nil
		}

		lastErr = err

		// Don't sleep on the last attempt
		if attempt < maxRetries-1 {
			// Exponential backoff: 1s, 2s, 4s, 8s, ...
			backoff := time.Duration(1<<attempt) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

// ParseUserData parses raw bytes into a UserData struct.
func ParseUserData(raw []byte, providerName string) (*UserData, error) {
	data := make(map[string]any)

	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("parsing user data as YAML: %w", err)
		}
	}

	return &UserData{
		Raw:      raw,
		Data:     data,
		Provider: providerName,
	}, nil
}
