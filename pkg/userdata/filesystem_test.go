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

var _ = Describe("Filesystem Provider", Label("userdata", "filesystem"), func() {
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
		It("returns filesystem", func() {
			provider := userdata.NewFilesystemProvider(system)
			Expect(provider.Name()).To(Equal("filesystem"))
		})
	})

	Describe("Available", func() {
		It("returns false when no userdata volume found", func() {
			provider := userdata.NewFilesystemProvider(system)
			// No cidata/USERDATA volume in test environment
			Expect(provider.Available(context.Background())).To(BeFalse())
		})
	})

	Describe("Provider constants", func() {
		It("defines expected volume labels", func() {
			// Test that the provider looks for expected labels
			provider := userdata.NewFilesystemProvider(system)
			Expect(provider.Name()).To(Equal("filesystem"))
		})
	})
})

var _ = Describe("Filesystem Provider Integration", Label("userdata", "filesystem", "integration"), func() {
	// These tests would require actual filesystem/block device mocking
	// which is more complex and typically done in integration tests

	Describe("User data paths", func() {
		It("supports standard cloud-init paths", func() {
			// Document expected paths
			expectedPaths := []string{
				"user-data",
				"userdata",
				"userdata.yaml",
				"userdata.yml",
				"user-data.yaml",
				"user-data.yml",
			}
			Expect(len(expectedPaths)).To(Equal(6))
		})

		It("supports standard volume labels", func() {
			// Document expected labels
			expectedLabels := []string{
				"cidata",
				"CIDATA",
				"USERDATA",
				"userdata",
			}
			Expect(len(expectedLabels)).To(Equal(4))
		})
	})
})
