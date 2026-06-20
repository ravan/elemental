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

package build

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/suse/elemental/v3/internal/config"
	"github.com/suse/elemental/v3/internal/image"
	imginstall "github.com/suse/elemental/v3/internal/image/install"
	"github.com/suse/elemental/v3/pkg/crypto"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
	"github.com/suse/elemental/v3/pkg/sys/vfs"
)

func TestBuildSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Build test suite")
}

var _ = Describe("newDeployment system autogrow", func() {
	It("does not append flag when raw.systemDiskSize is empty", func() {
		s, err := sys.NewSystem(sys.WithFS(vfs.New()), sys.WithRunner(sysmock.NewRunner()))
		Expect(err).ToNot(HaveOccurred())

		dep, err := newDeployment(
			s,
			"/dev/null",
			"registry.example.com/os:1",
			&imginstall.Installation{
				Bootloader:    "grub",
				KernelCmdLine: "console=ttyS0",
				CryptoPolicy:  crypto.DefaultPolicy,
			},
			config.Output{RootPath: "/tmp/elemental-build-test"},
		)

		Expect(err).ToNot(HaveOccurred())
		Expect(dep.BootConfig.KernelCmdline).To(Equal("console=ttyS0"))
	})

	It("appends and deduplicates the internal flag when raw.systemDiskSize is set", func() {
		s, err := sys.NewSystem(sys.WithFS(vfs.New()), sys.WithRunner(sysmock.NewRunner()))
		Expect(err).ToNot(HaveOccurred())

		dep, err := newDeployment(
			s,
			"/dev/null",
			"registry.example.com/os:1",
			&imginstall.Installation{
				Bootloader:    "grub",
				KernelCmdLine: "console=ttyS0 elemental.system_autogrow=1",
				CryptoPolicy:  crypto.DefaultPolicy,
				RAW: imginstall.RAW{
					SystemDiskSize: imginstall.DiskSize("80G"),
				},
			},
			config.Output{RootPath: "/tmp/elemental-build-test"},
		)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Fields(dep.BootConfig.KernelCmdline)).To(ContainElement("elemental.system_autogrow=1"))
		Expect(strings.Count(dep.BootConfig.KernelCmdline, "elemental.system_autogrow=1")).To(Equal(1))
		Expect(strings.Fields(dep.Installer.KernelCmdline)).To(ContainElement("console=ttyS0"))
		Expect(strings.Fields(dep.Installer.KernelCmdline)).To(ContainElement("elemental.system_autogrow=1"))
		Expect(strings.Count(dep.Installer.KernelCmdline, "elemental.system_autogrow=1")).To(Equal(1))
		Expect(strings.Fields(dep.BootConfig.KernelCmdline)).To(ContainElement("elemental.system_autogrow_target=80G"))
		Expect(strings.Fields(dep.Installer.KernelCmdline)).To(ContainElement("elemental.system_autogrow_target=80G"))
	})
})

var _ = Describe("createDisk raw sizing", func() {
	It("uses raw.diskSize for the artifact size", func() {
		runner := sysmock.NewRunner()
		img := image.Image{OutputImageName: "elemental.raw"}

		err := createDisk(runner, img, imginstall.DiskSize("8G"))

		Expect(err).ToNot(HaveOccurred())
		Expect(runner.IncludesCmds([][]string{
			{"truncate", "-s", "8G", "elemental.raw"},
		})).To(Succeed())
	})
})
