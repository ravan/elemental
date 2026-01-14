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
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/log"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
)

var _ = Describe("GCP Provider", Label("userdata", "gcp"), func() {
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
		It("returns gcp", func() {
			provider := NewGCPProvider(system)
			Expect(provider.Name()).To(Equal("gcp"))
		})
	})

	Describe("Available", func() {
		It("returns false when metadata server not reachable", func() {
			provider := NewGCPProvider(system)
			Expect(provider.Available(context.Background())).To(BeFalse())
		})

		It("returns true when metadata server responds with correct header", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Metadata-Flavor", "Google")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestGCPProvider(system, server.URL)
			Expect(provider.Available(context.Background())).To(BeTrue())
		})

		It("returns false when server doesn't return Google header", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Don't set the Metadata-Flavor response header
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestGCPProvider(system, server.URL)
			Expect(provider.Available(context.Background())).To(BeFalse())
		})
	})

	Describe("Fetch with mock server", func() {
		It("fetches user data with correct header", func() {
			userDataContent := `kubernetes:
  token: gcp-k8s-token
  server: https://gke.example.com:6443`

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.Header().Set("Metadata-Flavor", "Google")

				if strings.Contains(r.URL.Path, "user-data") {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(userDataContent))
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestGCPProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("gcp"))

			k8s, ok := data.Data["kubernetes"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(k8s["token"]).To(Equal("gcp-k8s-token"))
		})

		It("handles 404 as empty user data", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				w.Header().Set("Metadata-Flavor", "Google")

				if strings.Contains(r.URL.Path, "user-data") {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			provider := createTestGCPProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data).To(BeEmpty())
		})

		It("returns error for non-200/404 status", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Metadata-Flavor", "Google")
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()

			provider := createTestGCPProviderWithError(system, server.URL)

			_, err := provider.Fetch(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unexpected status"))
		})

		It("parses complex nested YAML", func() {
			userDataContent := `
kubernetes:
  token: nested-token
  server: https://gke.example.com:6443
  options:
    tls_san:
      - 10.0.0.1
      - k8s.local
users:
  - name: admin
    groups:
      - sudo
      - docker
    ssh_authorized_keys:
      - ssh-rsa AAAA...
network:
  interfaces:
    eth0:
      dhcp: true
`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Metadata-Flavor", "Google")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(userDataContent))
			}))
			defer server.Close()

			provider := createTestGCPProvider(system, server.URL)

			data, err := provider.Fetch(context.Background())
			Expect(err).NotTo(HaveOccurred())

			// Verify kubernetes section
			k8s, ok := data.Data["kubernetes"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(k8s["token"]).To(Equal("nested-token"))

			// Verify users section
			users, ok := data.Data["users"].([]any)
			Expect(ok).To(BeTrue())
			Expect(len(users)).To(Equal(1))

			// Verify network section
			network, ok := data.Data["network"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(network).To(HaveKey("interfaces"))
		})
	})
})

// createTestGCPProvider creates a GCP provider for testing with custom endpoints.
func createTestGCPProvider(s *sys.System, baseURL string) *testGCPProvider {
	return &testGCPProvider{
		system:           s,
		client:           &http.Client{},
		userDataEndpoint: baseURL + "/computeMetadata/v1/instance/attributes/user-data",
		checkEndpoint:    baseURL + "/computeMetadata/v1/",
	}
}

// testGCPProvider is a testable version of GCPProvider with configurable endpoints.
type testGCPProvider struct {
	system           *sys.System
	client           *http.Client
	userDataEndpoint string
	checkEndpoint    string
}

func (p *testGCPProvider) Name() string {
	return ProviderGCP
}

func (p *testGCPProvider) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.checkEndpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.Header.Get("Metadata-Flavor") == "Google"
}

func (p *testGCPProvider) Fetch(ctx context.Context) (*UserData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userDataEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ParseUserData(nil, p.Name())
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	buf := make([]byte, 1024*1024)
	n, _ := resp.Body.Read(buf)

	return ParseUserData(buf[:n], p.Name())
}

// createTestGCPProviderWithError is an alias for createTestGCPProvider for clarity in error tests.
func createTestGCPProviderWithError(s *sys.System, baseURL string) *testGCPProvider {
	return createTestGCPProvider(s, baseURL)
}
