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
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/log"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
)

var _ = Describe("Azure Provider", Label("userdata", "azure"), func() {
	var (
		system  *sys.System
		cleanup func()
	)

	BeforeEach(func() {
		fs, c, err := sysmock.TestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		cleanup = c

		system, err = sys.NewSystem(
			sys.WithFS(fs),
			sys.WithLogger(log.New()),
		)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Name", func() {
		It("returns azure", func() {
			provider := NewAzureProvider(system)
			Expect(provider.Name()).To(Equal("azure"))
		})
	})

	Describe("Available", func() {
		It("returns false when IMDS not reachable", func() {
			provider := NewAzureProvider(system)
			Expect(provider.Available(context.Background())).To(BeFalse())
		})

		It("returns true when IMDS responds with Metadata header", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"compute":{}}`))
			}))
			defer server.Close()

			provider := createTestAzureProvider(system, server.URL)
			Expect(provider.Available(context.Background())).To(BeTrue())
		})
	})

	Describe("Fetch with mock server", func() {
		It("fetches and decodes base64 user data", func() {
			userDataContent := `kubernetes:
  token: azure-k8s-token
  server: https://aks.example.com:6443`
			encoded := base64.StdEncoding.EncodeToString([]byte(userDataContent))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				if strings.Contains(r.URL.Path, "userData") {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(encoded))
					return
				}

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			provider := createTestAzureProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("azure"))

			k8s, ok := data.Data["kubernetes"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(k8s["token"]).To(Equal("azure-k8s-token"))
		})

		It("handles 404 as empty user data", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				if strings.Contains(r.URL.Path, "userData") {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestAzureProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data).To(BeEmpty())
		})

		It("handles empty response as empty user data", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(""))
			}))
			defer server.Close()

			provider := createTestAzureProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data).To(BeEmpty())
		})

		It("returns error for invalid base64", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				if strings.Contains(r.URL.Path, "userData") {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("not-valid-base64!!!"))
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestAzureProvider(system, server.URL)

			_, err := provider.Fetch(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base64"))
		})

		It("requires Metadata header", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata") != "true" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			// Test that the provider sets the header correctly
			provider := createTestAzureProvider(system, server.URL)
			Expect(provider.Available(context.Background())).To(BeTrue())
		})
	})
})

// createTestAzureProvider creates an Azure provider for testing with custom endpoints.
func createTestAzureProvider(s *sys.System, baseURL string) *testAzureProvider {
	return &testAzureProvider{
		system:           s,
		client:           &http.Client{},
		userDataEndpoint: baseURL + "/metadata/instance/compute/userData?api-version=2021-01-01&format=text",
		checkEndpoint:    baseURL + "/metadata/instance?api-version=2021-01-01",
	}
}

// testAzureProvider is a testable version of AzureProvider with configurable endpoints.
type testAzureProvider struct {
	system           *sys.System
	client           *http.Client
	userDataEndpoint string
	checkEndpoint    string
}

func (p *testAzureProvider) Name() string {
	return ProviderAzure
}

func (p *testAzureProvider) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.checkEndpoint, nil)
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

func (p *testAzureProvider) Fetch(ctx context.Context) (*UserData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userDataEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata", "true")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ParseUserData(nil, p.Name())
	}

	buf := make([]byte, 1024*1024)
	n, _ := resp.Body.Read(buf)
	encoded := buf[:n]

	if len(encoded) == 0 {
		return ParseUserData(nil, p.Name())
	}

	decoded, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return nil, err
	}

	return ParseUserData(decoded, p.Name())
}
