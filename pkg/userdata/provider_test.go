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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/userdata"
)

var _ = Describe("Provider", Label("userdata", "provider"), func() {
	Describe("Config", func() {
		It("returns sensible defaults", func() {
			cfg := userdata.DefaultConfig()

			Expect(cfg.Enabled).To(BeFalse())
			Expect(cfg.Providers).To(Equal([]string{"auto"}))
			Expect(cfg.Timeout).To(Equal(60))
			Expect(cfg.Retries).To(Equal(3))
		})
	})

	Describe("Provider constants", func() {
		It("defines expected provider names", func() {
			Expect(userdata.ProviderAuto).To(Equal("auto"))
			Expect(userdata.ProviderAWS).To(Equal("aws"))
			Expect(userdata.ProviderAzure).To(Equal("azure"))
			Expect(userdata.ProviderGCP).To(Equal("gcp"))
			Expect(userdata.ProviderFilesystem).To(Equal("filesystem"))
		})
	})

	Describe("UserData struct", func() {
		It("can hold raw and parsed data", func() {
			raw := []byte(`kubernetes:
  token: test-token
  server: https://example.com:6443`)

			data := &userdata.UserData{
				Raw: raw,
				Data: map[string]any{
					"kubernetes": map[string]any{
						"token":  "test-token",
						"server": "https://example.com:6443",
					},
				},
				Provider: "test",
			}

			Expect(data.Raw).To(Equal(raw))
			Expect(data.Provider).To(Equal("test"))

			k8s, ok := data.Data["kubernetes"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(k8s["token"]).To(Equal("test-token"))
			Expect(k8s["server"]).To(Equal("https://example.com:6443"))
		})
	})
})
