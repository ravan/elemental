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
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/suse/elemental/v3/pkg/sys"
)

const (
	azureUserDataEndpoint = "http://169.254.169.254/metadata/instance/compute/userData?api-version=2021-01-01&format=text"
	azureCheckEndpoint    = "http://169.254.169.254/metadata/instance?api-version=2021-01-01"
)

// AzureProvider implements the Provider interface for Azure IMDS.
type AzureProvider struct {
	system *sys.System
	client *http.Client
}

// NewAzureProvider creates a new Azure IMDS provider.
func NewAzureProvider(s *sys.System) Provider {
	return &AzureProvider{
		system: s,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Name returns the provider name.
func (p *AzureProvider) Name() string {
	return ProviderAzure
}

// Available checks if Azure IMDS is accessible.
func (p *AzureProvider) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, azureCheckEndpoint, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Metadata", "true")

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// Fetch retrieves user data from Azure IMDS.
func (p *AzureProvider) Fetch(ctx context.Context) (*UserData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, azureUserDataEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Metadata", "true")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching user data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 404 means no user data configured, which is valid
	if resp.StatusCode == http.StatusNotFound {
		return ParseUserData(nil, p.Name())
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	encoded, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Azure returns user data as base64-encoded
	if len(encoded) == 0 {
		return ParseUserData(nil, p.Name())
	}

	data, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return nil, fmt.Errorf("decoding base64 user data: %w", err)
	}

	return ParseUserData(data, p.Name())
}
