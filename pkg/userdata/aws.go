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
	awsTokenEndpoint    = "http://169.254.169.254/latest/api/token" //nolint:gosec // AWS IMDSv2 token endpoint, not credentials
	awsUserDataEndpoint = "http://169.254.169.254/latest/user-data"
	awsTokenTTL         = "21600" // 6 hours in seconds
)

// AWSProvider implements the Provider interface for AWS IMDSv2.
type AWSProvider struct {
	system *sys.System
	client *http.Client
}

// NewAWSProvider creates a new AWS IMDSv2 provider.
func NewAWSProvider(s *sys.System) Provider {
	return &AWSProvider{
		system: s,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Name returns the provider name.
func (p *AWSProvider) Name() string {
	return ProviderAWS
}

// Available checks if AWS IMDSv2 is accessible.
func (p *AWSProvider) Available(ctx context.Context) bool {
	_, err := p.getToken(ctx)
	return err == nil
}

// Fetch retrieves user data from AWS IMDSv2.
func (p *AWSProvider) Fetch(ctx context.Context) (*UserData, error) {
	token, err := p.getToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting IMDSv2 token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsUserDataEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("X-aws-ec2-metadata-token", token)

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

// getToken retrieves an IMDSv2 session token.
func (p *AWSProvider) getToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, awsTokenEndpoint, nil)
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}

	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", awsTokenTTL)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	token, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token: %w", err)
	}

	return string(token), nil
}
