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
	"io"
	"net/http"
	"time"

	"github.com/suse/elemental/v3/pkg/sys"
)

const (
	gcpUserDataEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/attributes/user-data"
	gcpCheckEndpoint    = "http://metadata.google.internal/computeMetadata/v1/"
)

// GCPProvider implements the Provider interface for GCP Metadata Server.
type GCPProvider struct {
	system *sys.System
	client *http.Client
}

// NewGCPProvider creates a new GCP Metadata Server provider.
func NewGCPProvider(s *sys.System) Provider {
	return &GCPProvider{
		system: s,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Name returns the provider name.
func (p *GCPProvider) Name() string {
	return ProviderGCP
}

// Available checks if GCP Metadata Server is accessible.
func (p *GCPProvider) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gcpCheckEndpoint, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	// GCP metadata server returns this header
	return resp.Header.Get("Metadata-Flavor") == "Google"
}

// Fetch retrieves user data from GCP Metadata Server.
func (p *GCPProvider) Fetch(ctx context.Context) (*UserData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gcpUserDataEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Metadata-Flavor", "Google")

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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return ParseUserData(data, p.Name())
}
