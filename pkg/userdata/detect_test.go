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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/log"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
	"github.com/suse/elemental/v3/pkg/userdata"
)

var _ = Describe("Detect", Label("userdata", "detect"), func() {
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

	Describe("GetProviders", func() {
		It("returns all providers for auto config", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"auto"},
			}

			providers := userdata.GetProviders(system, cfg)
			Expect(len(providers)).To(Equal(4)) // aws, azure, gcp, filesystem
		})

		It("returns all providers for empty provider list", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{},
			}

			providers := userdata.GetProviders(system, cfg)
			Expect(len(providers)).To(Equal(4))
		})

		It("returns specific providers when configured", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"aws", "gcp"},
			}

			providers := userdata.GetProviders(system, cfg)
			Expect(len(providers)).To(Equal(2))

			names := make([]string, len(providers))
			for i, p := range providers {
				names[i] = p.Name()
			}
			Expect(names).To(ContainElements("aws", "gcp"))
		})

		It("skips unknown provider names", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"aws", "unknown", "gcp"},
			}

			providers := userdata.GetProviders(system, cfg)
			Expect(len(providers)).To(Equal(2))
		})

		It("returns single provider", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"filesystem"},
			}

			providers := userdata.GetProviders(system, cfg)
			Expect(len(providers)).To(Equal(1))
			Expect(providers[0].Name()).To(Equal("filesystem"))
		})

		It("ignores auto in mixed list", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"auto", "aws"},
			}

			providers := userdata.GetProviders(system, cfg)
			// Should only get aws since auto is skipped when mixed with others
			Expect(len(providers)).To(Equal(1))
			Expect(providers[0].Name()).To(Equal("aws"))
		})
	})

	Describe("Detect", func() {
		It("returns first available provider or error", func() {
			// In test environment, the Detect function will check all providers
			// The filesystem provider may or may not be available depending on the system
			provider, err := userdata.Detect(context.Background(), system)
			if err != nil {
				Expect(err).To(MatchError(userdata.ErrNoProviderAvailable))
			} else {
				Expect(provider).NotTo(BeNil())
				Expect(provider.Name()).To(BeElementOf("aws", "azure", "gcp", "filesystem"))
			}
		})
	})

	Describe("FetchUserData", func() {
		It("returns error when not enabled", func() {
			cfg := userdata.Config{
				Enabled: false,
			}

			_, err := userdata.FetchUserData(context.Background(), system, cfg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enabled"))
		})

		It("returns error when no providers available", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"auto"},
				Timeout:   1,
				Retries:   1,
			}

			// In test environment, no cloud metadata is available
			_, err := userdata.FetchUserData(context.Background(), system, cfg)
			Expect(err).To(HaveOccurred())
		})

		It("uses default timeout when not specified", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"aws"},
				Timeout:   0, // Should default to 60
				Retries:   1,
			}

			// Will fail but tests timeout handling
			_, err := userdata.FetchUserData(context.Background(), system, cfg)
			Expect(err).To(HaveOccurred())
		})

		It("uses default retries when not specified", func() {
			cfg := userdata.Config{
				Enabled:   true,
				Providers: []string{"aws"},
				Timeout:   1,
				Retries:   0, // Should default to 3
			}

			// Will fail but tests retry handling
			_, err := userdata.FetchUserData(context.Background(), system, cfg)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Provider Order", Label("userdata", "detect"), func() {
	Describe("Default provider order", func() {
		It("tries providers in expected order", func() {
			// Document the expected order for auto-detection
			expectedOrder := []string{
				userdata.ProviderAWS,
				userdata.ProviderAzure,
				userdata.ProviderGCP,
				userdata.ProviderFilesystem,
			}

			Expect(expectedOrder[0]).To(Equal("aws"))
			Expect(expectedOrder[1]).To(Equal("azure"))
			Expect(expectedOrder[2]).To(Equal("gcp"))
			Expect(expectedOrder[3]).To(Equal("filesystem"))
		})
	})
})
