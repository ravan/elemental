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

package config

import (
	_ "embed"
	"fmt"
	"path/filepath"

	"github.com/coreos/butane/base/v0_6"
	"github.com/coreos/ignition/v2/config/util"
	"go.yaml.in/yaml/v3"

	"github.com/suse/elemental/v3/internal/butane"
	"github.com/suse/elemental/v3/internal/image"
	"github.com/suse/elemental/v3/internal/image/kubernetes"
	"github.com/suse/elemental/v3/internal/template"
	"github.com/suse/elemental/v3/pkg/extensions"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/sys"
)

const (
	ensureSysextUnitName        = "ensure-sysext.service"
	reloadKernelModulesUnitName = "reload-kernel-modules.service"
	updateLinkerCacheUnitName   = "update-linker-cache.service"
	k8sResourcesUnitName        = "k8s-resource-installer.service"
	k8sConfigUnitName           = "k8s-config-installer.service"
	k8sDynamicUnitName          = "elemental-k8s-dynamic.service"
)

var (
	//go:embed templates/ensure-sysext.service
	ensureSysextUnit string

	//go:embed templates/reload-kernel-modules.service
	reloadKernelModulesUnit string

	//go:embed templates/update-linker-cache.service
	updateLinkerCacheUnit string

	//go:embed templates/k8s-resource-installer.service.tpl
	k8sResourceUnitTpl string

	//go:embed templates/k8s-config-installer.service.tpl
	k8sConfigUnitTpl string

	//go:embed templates/k8s-vip.yaml.tpl
	k8sVIPManifestTpl string

	//go:embed templates/elemental-k8s-dynamic.service.tpl
	k8sDynamicUnitTpl string
)

// configureIgnition writes the Ignition configuration file including:
// * Predefined Butane configuration
// * Kubernetes configuration and deployment files
// * Systemd extensions
// * K8s dynamic service (when user data is enabled)
func (m *Manager) configureIgnition(conf *image.Configuration, output Output, k8sScript, k8sConfScript string, ext []api.SystemdExtension) error {
	if len(conf.ButaneConfig) == 0 &&
		!conf.UserData.Enabled &&
		k8sScript == "" &&
		k8sConfScript == "" &&
		len(ext) == 0 {
		m.system.Logger().Info("No ignition configuration required")
		return nil
	}

	const (
		variant = "fcos"
		version = "1.6.0"
	)
	var config butane.Config

	config.Variant = variant
	config.Version = version

	// Handle butane configuration - always render statically at build time
	if len(conf.ButaneConfig) > 0 {
		m.system.Logger().Info("Translating butane configuration to Ignition syntax")

		ignitionBytes, err := butane.TranslateBytes(m.system, conf.ButaneConfig)
		if err != nil {
			return fmt.Errorf("failed translating butane configuration: %w", err)
		}
		config.MergeInlineIgnition(string(ignitionBytes))
	} else {
		m.system.Logger().Info("No butane configuration to translate into Ignition syntax")
	}

	// When user data is enabled, add k8s-dynamic service for post-boot K8s config rendering
	if conf.UserData.Enabled {
		m.system.Logger().Info("User data enabled: configuring k8s-dynamic service for boot-time K8s config rendering")

		if err := m.configureK8sDynamic(conf, &config); err != nil {
			return fmt.Errorf("configuring k8s-dynamic: %w", err)
		}
	}

	if k8sScript != "" {
		initHostname := "*"
		if len(conf.Kubernetes.Nodes) > 0 {
			initNode, err := kubernetes.FindInitNode(conf.Kubernetes.Nodes)
			if err != nil {
				return err
			}

			if initNode != nil {
				initHostname = initNode.Hostname
			}
		}

		k8sResourcesUnit, err := generateK8sResourcesUnit(k8sScript, initHostname)
		if err != nil {
			return err
		}

		config.AddSystemdUnit(k8sResourcesUnitName, k8sResourcesUnit, true)
	}

	if k8sConfScript != "" {
		err := appendRke2Configuration(m.system, &config, &conf.Kubernetes, k8sConfScript)
		if err != nil {
			return fmt.Errorf("failed appending rke2 configuration: %w", err)
		}
	}

	if len(ext) > 0 {
		data, err := extensions.Serialize(ext)
		if err != nil {
			return fmt.Errorf("serializing extensions: %w", err)
		}

		config.Storage.Files = append(config.Storage.Files, v0_6.File{
			Path:     extensions.File,
			Contents: v0_6.Resource{Inline: util.StrToPtr(data)},
		})

		config.AddSystemdUnit(ensureSysextUnitName, ensureSysextUnit, true)
		config.AddSystemdUnit(reloadKernelModulesUnitName, reloadKernelModulesUnit, true)
		config.AddSystemdUnit(updateLinkerCacheUnitName, updateLinkerCacheUnit, true)
	}

	ignitionFile := filepath.Join(output.FirstbootConfigDir(), image.IgnitionFilePath())
	return butane.WriteIgnitionFile(m.system, config, ignitionFile)
}

func generateK8sResourcesUnit(deployScript, initHostname string) (string, error) {
	values := struct {
		KubernetesDir        string
		ManifestDeployScript string
		InitHostname         string
	}{
		KubernetesDir:        filepath.Dir(deployScript),
		ManifestDeployScript: deployScript,
		InitHostname:         initHostname,
	}

	data, err := template.Parse(k8sResourcesUnitName, k8sResourceUnitTpl, &values)
	if err != nil {
		return "", fmt.Errorf("parsing config script template: %w", err)
	}
	return data, nil
}

func generateK8sConfigUnit(deployScript string) (string, error) {
	values := struct {
		ConfigDeployScript string
	}{
		ConfigDeployScript: deployScript,
	}

	data, err := template.Parse(k8sConfigUnitName, k8sConfigUnitTpl, &values)
	if err != nil {
		return "", fmt.Errorf("parsing config script template: %w", err)
	}
	return data, nil
}

func kubernetesVIPManifest(k *kubernetes.Kubernetes) (string, error) {
	vars := struct {
		APIAddress4 string
		APIAddress6 string
	}{
		APIAddress4: k.Network.APIVIP4,
		APIAddress6: k.Network.APIVIP6,
	}

	return template.Parse("k8s-vip", k8sVIPManifestTpl, &vars)
}

func appendRke2Configuration(s *sys.System, config *butane.Config, k *kubernetes.Kubernetes, configScript string) error {
	c, err := kubernetes.NewCluster(s, k)
	if err != nil {
		return fmt.Errorf("failed parsing cluster: %w", err)
	}

	k8sConfigUnit, err := generateK8sConfigUnit(configScript)
	if err != nil {
		return fmt.Errorf("failed generating k8s config unit: %w", err)
	}

	config.AddSystemdUnit(k8sConfigUnitName, k8sConfigUnit, true)

	k8sPath := filepath.Join("/", image.KubernetesPath())

	serverBytes, err := marshalConfig(c.ServerConfig)
	if err != nil {
		return fmt.Errorf("failed marshaling server config: %w", err)
	}

	config.Storage.Files = append(config.Storage.Files, v0_6.File{
		Path:     filepath.Join(k8sPath, "server.yaml"),
		Contents: v0_6.Resource{Inline: util.StrToPtr(string(serverBytes))},
	})

	if c.InitServerConfig != nil {
		initServerBytes, err := marshalConfig(c.InitServerConfig)
		if err != nil {
			return fmt.Errorf("failed marshaling init-server config: %w", err)
		}

		config.Storage.Files = append(config.Storage.Files, v0_6.File{
			Path:     filepath.Join(k8sPath, "init.yaml"),
			Contents: v0_6.Resource{Inline: util.StrToPtr(string(initServerBytes))},
		})
	}

	if c.AgentConfig != nil {
		agentBytes, err := marshalConfig(c.AgentConfig)
		if err != nil {
			return fmt.Errorf("failed marshaling agent config: %w", err)
		}

		config.Storage.Files = append(config.Storage.Files, v0_6.File{
			Path:     filepath.Join(k8sPath, "agent.yaml"),
			Contents: v0_6.Resource{Inline: util.StrToPtr(string(agentBytes))},
		})
	}

	if k.Network.APIVIP4 != "" || k.Network.APIVIP6 != "" {
		manifestsPath := filepath.Join("/", image.KubernetesManifestsPath())

		vip, err := kubernetesVIPManifest(k)
		if err != nil {
			return fmt.Errorf("failed marshaling agent config: %w", err)
		}

		config.Storage.Files = append(config.Storage.Files, v0_6.File{
			Path:     filepath.Join(manifestsPath, "k8s-vip.yaml"),
			Contents: v0_6.Resource{Inline: util.StrToPtr(string(vip))},
		})
	}

	return nil
}

func marshalConfig(config map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("serializing kubernetes config: %w", err)
	}

	return data, nil
}

// configureK8sDynamic sets up the k8s-dynamic service for post-boot K8s config rendering.
// The service will auto-detect the cloud environment and fetch userdata at boot time.
func (m *Manager) configureK8sDynamic(conf *image.Configuration, config *butane.Config) error {
	k8sDynamicUnit, err := generateK8sDynamicUnit(conf.UserData.Timeout)
	if err != nil {
		return fmt.Errorf("generating k8s-dynamic unit: %w", err)
	}

	config.AddSystemdUnit(k8sDynamicUnitName, k8sDynamicUnit, true)

	return nil
}

func generateK8sDynamicUnit(timeout int) (string, error) {
	if timeout <= 0 {
		timeout = 120
	}

	values := struct {
		Timeout int
	}{
		Timeout: timeout,
	}

	data, err := template.Parse(k8sDynamicUnitName, k8sDynamicUnitTpl, &values)
	if err != nil {
		return "", fmt.Errorf("parsing k8s-dynamic service template: %w", err)
	}
	return data, nil
}
