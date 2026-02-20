/*
Copyright © 2025-2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0
*/

package action

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/log"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
	"github.com/suse/elemental/v3/pkg/sys/vfs"
	"github.com/suse/elemental/v3/pkg/userdata"
)

func TestK8sDynamic(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8sDynamic Suite")
}

var _ = Describe("writeSSHKeysFromUserData", Label("k8s-dynamic", "ssh"), func() {
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

	It("writes SSH keys to /root/.ssh/authorized_keys for root user", func() {
		ud := &userdata.UserData{
			Data: map[string]any{
				"users": []any{
					map[string]any{
						"name": "root",
						"ssh_authorized_keys": []any{
							"ssh-ed25519 AAAA-test-key user@host",
						},
					},
				},
			},
			Provider: "test",
		}

		err := writeSSHKeysFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())

		content, err := system.FS().ReadFile("/root/.ssh/authorized_keys")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("ssh-ed25519 AAAA-test-key user@host"))
	})

	It("appends to existing authorized_keys", func() {
		// Pre-existing key (e.g., set by Ignition)
		Expect(vfs.MkdirAll(system.FS(), "/root/.ssh", 0o700)).To(Succeed())
		Expect(system.FS().WriteFile("/root/.ssh/authorized_keys", []byte("ssh-rsa existing-key\n"), 0o600)).To(Succeed())

		ud := &userdata.UserData{
			Data: map[string]any{
				"users": []any{
					map[string]any{
						"name": "root",
						"ssh_authorized_keys": []any{
							"ssh-ed25519 new-key",
						},
					},
				},
			},
			Provider: "test",
		}

		err := writeSSHKeysFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())

		content, err := system.FS().ReadFile("/root/.ssh/authorized_keys")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("ssh-rsa existing-key"))
		Expect(string(content)).To(ContainSubstring("ssh-ed25519 new-key"))
	})

	It("handles multiple users with multiple keys", func() {
		ud := &userdata.UserData{
			Data: map[string]any{
				"users": []any{
					map[string]any{
						"name": "root",
						"ssh_authorized_keys": []any{
							"ssh-ed25519 key1",
							"ssh-ed25519 key2",
						},
					},
				},
			},
			Provider: "test",
		}

		err := writeSSHKeysFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())

		content, err := system.FS().ReadFile("/root/.ssh/authorized_keys")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("ssh-ed25519 key1"))
		Expect(string(content)).To(ContainSubstring("ssh-ed25519 key2"))
	})

	It("does nothing when no users section exists", func() {
		ud := &userdata.UserData{
			Data: map[string]any{
				"rke2": map[string]any{"type": "server"},
			},
			Provider: "test",
		}

		err := writeSSHKeysFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())
		// No file should be created
		_, err = system.FS().ReadFile("/root/.ssh/authorized_keys")
		Expect(err).To(HaveOccurred())
	})

	It("does nothing when user data is nil", func() {
		err := writeSSHKeysFromUserData(system, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	It("skips users without ssh_authorized_keys", func() {
		ud := &userdata.UserData{
			Data: map[string]any{
				"users": []any{
					map[string]any{
						"name": "root",
					},
				},
			},
			Provider: "test",
		}

		err := writeSSHKeysFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())
	})
})
