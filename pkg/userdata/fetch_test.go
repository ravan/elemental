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

package userdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/userdata"
)

// mockProvider is a test provider implementation.
type mockProvider struct {
	name      string
	available bool
	data      *userdata.UserData
	err       error
	fetchCnt  int
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Available(_ context.Context) bool {
	return m.available
}

func (m *mockProvider) Fetch(_ context.Context) (*userdata.UserData, error) {
	m.fetchCnt++
	if m.err != nil {
		return nil, m.err
	}
	return m.data, nil
}

var _ = Describe("Fetch", Label("userdata", "fetch"), func() {
	Describe("Fetch function", func() {
		It("returns error when no providers given", func() {
			_, err := userdata.Fetch(context.Background())
			Expect(err).To(MatchError(userdata.ErrNoProviderAvailable))
		})

		It("skips unavailable providers", func() {
			unavailable := &mockProvider{name: "unavailable", available: false}
			available := &mockProvider{
				name:      "available",
				available: true,
				data:      &userdata.UserData{Provider: "available"},
			}

			data, err := userdata.Fetch(context.Background(), unavailable, available)
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("available"))
			Expect(unavailable.fetchCnt).To(Equal(0))
			Expect(available.fetchCnt).To(Equal(1))
		})

		It("returns first successful result", func() {
			first := &mockProvider{
				name:      "first",
				available: true,
				data:      &userdata.UserData{Provider: "first"},
			}
			second := &mockProvider{
				name:      "second",
				available: true,
				data:      &userdata.UserData{Provider: "second"},
			}

			data, err := userdata.Fetch(context.Background(), first, second)
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("first"))
			Expect(second.fetchCnt).To(Equal(0))
		})

		It("tries next provider on error", func() {
			failing := &mockProvider{
				name:      "failing",
				available: true,
				err:       errors.New("fetch failed"),
			}
			working := &mockProvider{
				name:      "working",
				available: true,
				data:      &userdata.UserData{Provider: "working"},
			}

			data, err := userdata.Fetch(context.Background(), failing, working)
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("working"))
		})

		It("returns last error when all providers fail", func() {
			failing1 := &mockProvider{
				name:      "failing1",
				available: true,
				err:       errors.New("first error"),
			}
			failing2 := &mockProvider{
				name:      "failing2",
				available: true,
				err:       errors.New("second error"),
			}

			_, err := userdata.Fetch(context.Background(), failing1, failing2)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("second error"))
		})

		It("returns ErrNoProviderAvailable when all unavailable", func() {
			unavailable1 := &mockProvider{name: "u1", available: false}
			unavailable2 := &mockProvider{name: "u2", available: false}

			_, err := userdata.Fetch(context.Background(), unavailable1, unavailable2)
			Expect(err).To(MatchError(userdata.ErrNoProviderAvailable))
		})
	})

	Describe("FetchWithRetry function", func() {
		It("succeeds on first try", func() {
			provider := &mockProvider{
				name:      "test",
				available: true,
				data:      &userdata.UserData{Provider: "test"},
			}

			data, err := userdata.FetchWithRetry(context.Background(), 3, provider)
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("test"))
			Expect(provider.fetchCnt).To(Equal(1))
		})

		It("retries on failure", func() {
			// Create a custom flaky provider that fails twice then succeeds
			flakyProvider := &flakyMockProvider{
				name:           "flaky",
				available:      true,
				failCount:      2,
				successData:    &userdata.UserData{Provider: "flaky"},
				currentFetches: 0,
			}

			data, err := userdata.FetchWithRetry(context.Background(), 3, flakyProvider)
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("flaky"))
			Expect(flakyProvider.currentFetches).To(Equal(3))
		})

		It("fails after max retries", func() {
			provider := &mockProvider{
				name:      "failing",
				available: true,
				err:       errors.New("persistent failure"),
			}

			_, err := userdata.FetchWithRetry(context.Background(), 2, provider)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("after 2 attempts"))
			Expect(provider.fetchCnt).To(Equal(2))
		})

		It("respects context cancellation", func() {
			provider := &mockProvider{
				name:      "slow",
				available: true,
				err:       errors.New("failure"),
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			_, err := userdata.FetchWithRetry(ctx, 5, provider)
			Expect(err).To(HaveOccurred())
			// Should fail quickly due to context cancellation
		})
	})

	Describe("ParseUserData function", func() {
		It("parses valid YAML", func() {
			raw := []byte(`
kubernetes:
  token: my-token
  server: https://k8s.example.com:6443
users:
  - name: admin
    ssh_keys:
      - ssh-rsa AAAA...
`)
			data, err := userdata.ParseUserData(raw, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Provider).To(Equal("test"))
			Expect(data.Raw).To(Equal(raw))

			k8s, ok := data.Data["kubernetes"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(k8s["token"]).To(Equal("my-token"))
		})

		It("handles empty data", func() {
			data, err := userdata.ParseUserData(nil, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data).To(BeEmpty())
		})

		It("handles empty byte slice", func() {
			data, err := userdata.ParseUserData([]byte{}, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data).To(BeEmpty())
		})

		It("returns error for invalid YAML", func() {
			raw := []byte(`invalid: yaml: content: [`)
			_, err := userdata.ParseUserData(raw, "test")
			Expect(err).To(HaveOccurred())
		})

		It("parses JSON as YAML", func() {
			raw := []byte(`{"key": "value", "nested": {"inner": 123}}`)
			data, err := userdata.ParseUserData(raw, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(data.Data["key"]).To(Equal("value"))

			nested, ok := data.Data["nested"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(nested["inner"]).To(Equal(123))
		})
	})
})

// flakyMockProvider fails a specified number of times before succeeding.
type flakyMockProvider struct {
	name           string
	available      bool
	failCount      int
	successData    *userdata.UserData
	currentFetches int
}

func (f *flakyMockProvider) Name() string {
	return f.name
}

func (f *flakyMockProvider) Available(_ context.Context) bool {
	return f.available
}

func (f *flakyMockProvider) Fetch(_ context.Context) (*userdata.UserData, error) {
	f.currentFetches++
	if f.currentFetches <= f.failCount {
		return nil, errors.New("temporary failure")
	}
	return f.successData, nil
}
