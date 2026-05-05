/*
Copyright © 2025-2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0
*/

package action

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/suse/elemental/v3/pkg/log"
	"github.com/suse/elemental/v3/pkg/sys"
	sysmock "github.com/suse/elemental/v3/pkg/sys/mock"
	"github.com/suse/elemental/v3/pkg/sys/vfs"
	"github.com/suse/elemental/v3/pkg/userdata"
)

var _ = Describe("writeHostnameFromUserData", Label("k8s-dynamic", "hostname"), func() {
	var (
		system  *sys.System
		runner  *sysmock.Runner
		cleanup func()
	)

	BeforeEach(func() {
		fs, c, err := sysmock.TestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		cleanup = c
		runner = sysmock.NewRunner()

		system, err = sys.NewSystem(
			sys.WithFS(fs),
			sys.WithLogger(log.New()),
			sys.WithRunner(runner),
		)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanup()
	})

	It("writes hostname to /etc/hostname and applies it via hostnamectl", func() {
		ud := &userdata.UserData{
			Data:     map[string]any{"hostname": "node1.example.com"},
			Provider: "test",
		}

		err := writeHostnameFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())

		content, err := system.FS().ReadFile("/etc/hostname")
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("node1.example.com\n"))

		Expect(runner.IncludesCmds([][]string{
			{"hostnamectl", "set-hostname", "node1.example.com"},
		})).To(Succeed())
	})

	It("does nothing when hostname is not set", func() {
		ud := &userdata.UserData{
			Data:     map[string]any{"rke2": map[string]any{"type": "server"}},
			Provider: "test",
		}

		err := writeHostnameFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())
	})

	It("does nothing when hostname is empty", func() {
		ud := &userdata.UserData{
			Data:     map[string]any{"hostname": ""},
			Provider: "test",
		}

		err := writeHostnameFromUserData(system, ud)
		Expect(err).NotTo(HaveOccurred())
	})

	It("does nothing when user data is nil", func() {
		err := writeHostnameFromUserData(system, nil)
		Expect(err).NotTo(HaveOccurred())
	})
})

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

var _ = Describe("writeK8sDynamicDeployScript", Label("k8s-dynamic", "deploy-script"), func() {
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

	It("installs embedded RKE2 artifacts before enabling the node service", func() {
		ud := &userdata.UserData{
			Data: map[string]any{
				"rke2": map[string]any{
					"type":  "server",
					"init":  true,
					"token": "test-token",
				},
			},
			Provider: "test",
		}

		err := writeK8sDynamicDeployScript(system, "/var/lib/elemental/kubernetes", ud)
		Expect(err).NotTo(HaveOccurred())

		content, err := system.FS().ReadFile("/var/lib/elemental/kubernetes/k8s_conf_deploy.sh")
		Expect(err).NotTo(HaveOccurred())
		script := string(content)

		Expect(script).To(ContainSubstring("export INSTALL_RKE2_ARTIFACT_PATH=\"/opt/k8s/install\""))
		Expect(script).To(ContainSubstring("sh \"/opt/k8s/install/install.sh\""))
		Expect(script).To(ContainSubstring("NODETYPE=\"server\""))
		Expect(script).To(ContainSubstring("systemctl enable --now rke2-${NODETYPE}.service"))
		Expect(script).To(MatchRegexp(`(?s)sh "/opt/k8s/install/install\.sh".*systemctl enable --now rke2-\$\{NODETYPE\}\.service`))
	})
})
