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

package action

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
	"go.yaml.in/yaml/v3"

	"github.com/suse/elemental/v3/internal/cli/cmd"
	"github.com/suse/elemental/v3/internal/config"
	"github.com/suse/elemental/v3/internal/image"
	"github.com/suse/elemental/v3/internal/template"
	"github.com/suse/elemental/v3/pkg/sys"
	"github.com/suse/elemental/v3/pkg/sys/vfs"
	"github.com/suse/elemental/v3/pkg/userdata"
)

const (
	k8sConfDeployDynamicScriptName = "k8s_conf_deploy.sh"
)

// K8sDynamicApply fetches user data and renders Kubernetes config templates.
// This runs AFTER ignition-firstboot, before k8s-config-installer.
func K8sDynamicApply(ctx *cli.Context) error {
	var s *sys.System
	args := &cmd.K8sDynamicArgs
	if ctx.App.Metadata == nil || ctx.App.Metadata["system"] == nil {
		return fmt.Errorf("error setting up initial configuration")
	}
	s = ctx.App.Metadata["system"].(*sys.System)

	s.Logger().Info("Starting k8s-dynamic apply action")

	// Use default config - no config file needed
	// The userdata provider will auto-detect the cloud environment
	cfg := userdata.DefaultConfig()
	cfg.Enabled = true

	// Apply flag overrides
	if args.Provider != "" {
		cfg.Providers = []string{args.Provider}
	}
	if args.Timeout > 0 {
		cfg.Timeout = args.Timeout
	}
	if args.Retries > 0 {
		cfg.Retries = args.Retries
	}

	s.Logger().Info("Fetching user data with config: providers=%v, timeout=%d, retries=%d",
		cfg.Providers, cfg.Timeout, cfg.Retries)

	// Fetch user data from cloud provider
	userData, err := userdata.FetchUserData(ctx.Context, s, cfg)
	if err != nil {
		return fmt.Errorf("fetching user data: %w", err)
	}

	s.Logger().Info("User data fetched from provider: %s", userData.Provider)

	// Find and render all .tpl files in kubernetes config directory
	// Use absolute path - kubernetes configs are on the system partition, not the config partition
	k8sConfigDir := filepath.Join("/", image.KubernetesPath())
	templatesRendered, err := renderK8sTemplates(s, k8sConfigDir, userData)
	if err != nil {
		return fmt.Errorf("rendering kubernetes templates: %w", err)
	}

	if templatesRendered == 0 {
		s.Logger().Info("No kubernetes config templates found in %s", k8sConfigDir)
	} else {
		s.Logger().Info("Successfully rendered %d kubernetes config template(s)", templatesRendered)
	}

	// Generate RKE2 config from user data
	if err := writeRKE2ConfigFromUserData(s, k8sConfigDir, userData); err != nil {
		return fmt.Errorf("writing RKE2 config: %w", err)
	}

	// Generate deployment script with node type from user data
	if err := writeK8sDynamicDeployScript(s, k8sConfigDir, userData); err != nil {
		return fmt.Errorf("writing dynamic deployment script: %w", err)
	}

	return nil
}

// writeRKE2ConfigFromUserData generates RKE2 config YAML from user data.
// The filename is determined by node type to match k8s-config-installer expectations:
// - init.yaml: for init server (rke2.init: true)
// - server.yaml: for joining servers (rke2.type: server, default)
// - agent.yaml: for agents (rke2.type: agent)
func writeRKE2ConfigFromUserData(s *sys.System, k8sConfigDir string, userData *userdata.UserData) error {
	if userData == nil || userData.Data == nil {
		s.Logger().Info("No user data available for RKE2 config generation")
		return nil
	}

	rke2Data, ok := userData.Data["rke2"].(map[string]any)
	if !ok {
		s.Logger().Info("No rke2 section in user data")
		return nil
	}

	// Determine config filename based on node type
	// This must match what k8s-config-installer's deploy script expects
	nodeType := "server" // default
	isInit := false
	if t, ok := rke2Data["type"].(string); ok && t != "" {
		nodeType = t
	}
	if init, ok := rke2Data["init"].(bool); ok && init {
		isInit = true
	}

	// Build RKE2 config from userdata
	rke2Config := make(map[string]any)

	// Server URL (required for joining nodes, not for init server)
	if server, ok := rke2Data["server"].(string); ok && server != "" {
		rke2Config["server"] = server
	}

	// Token (required for all nodes)
	if token, ok := rke2Data["token"].(string); ok && token != "" {
		rke2Config["token"] = token
	}

	// TLS SANs for API server certificate
	if tlsSan, ok := rke2Data["tls_san"].([]any); ok && len(tlsSan) > 0 {
		var sans []string
		for _, san := range tlsSan {
			if s, ok := san.(string); ok && s != "" {
				sans = append(sans, s)
			}
		}
		if len(sans) > 0 {
			rke2Config["tls-san"] = sans
		}
	}

	// If no config to generate, skip
	if len(rke2Config) == 0 {
		s.Logger().Info("No RKE2 config data in user data - skipping config generation")
		return nil
	}

	// Determine filename based on node type
	// k8s-config-installer expects: init.yaml, server.yaml, or agent.yaml
	configFilename := nodeType + ".yaml"
	if isInit {
		configFilename = "init.yaml"
	}

	s.Logger().Info("Generating RKE2 config (type=%s, init=%v) with server=%v, token=%v",
		nodeType, isInit, rke2Config["server"], rke2Config["token"] != nil)

	// Ensure directory exists
	if err := vfs.MkdirAll(s.FS(), k8sConfigDir, vfs.DirPerm); err != nil {
		return fmt.Errorf("creating kubernetes config directory: %w", err)
	}

	// Marshal to YAML
	configBytes, err := yaml.Marshal(rke2Config)
	if err != nil {
		return fmt.Errorf("marshaling RKE2 config: %w", err)
	}

	configPath := filepath.Join(k8sConfigDir, configFilename)
	if err := s.FS().WriteFile(configPath, configBytes, 0o600); err != nil {
		return fmt.Errorf("writing RKE2 config: %w", err)
	}

	s.Logger().Info("RKE2 config written to %s", configPath)
	return nil
}

// renderK8sTemplates finds all .tpl files in the directory and renders them.
func renderK8sTemplates(s *sys.System, dir string, userData *userdata.UserData) (int, error) {
	// Check if directory exists
	isDir, err := vfs.IsDir(s.FS(), dir)
	if err != nil {
		// Directory doesn't exist - that's OK, just means no templates to render
		s.Logger().Debug("Kubernetes config directory does not exist: %s", dir)
		return 0, nil
	}
	if !isDir {
		return 0, fmt.Errorf("kubernetes config path is not a directory: %s", dir)
	}

	// Read directory entries
	entries, err := s.FS().ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("reading kubernetes config directory: %w", err)
	}

	rendered := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".tpl") {
			continue
		}

		templatePath := filepath.Join(dir, name)
		outputPath := filepath.Join(dir, strings.TrimSuffix(name, ".tpl"))

		s.Logger().Info("Rendering template: %s -> %s", templatePath, outputPath)

		if err := renderTemplateFile(s, templatePath, outputPath, userData); err != nil {
			return rendered, fmt.Errorf("rendering template %s: %w", name, err)
		}

		rendered++
	}

	return rendered, nil
}

// renderTemplateFile reads a template file, renders it with userdata, and writes the output.
func renderTemplateFile(s *sys.System, templatePath, outputPath string, userData *userdata.UserData) error {
	// Read template file
	templateBytes, err := s.FS().ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading template file: %w", err)
	}

	// Render template with user data
	rendered, err := config.RenderButaneTemplate(string(templateBytes), userData)
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	s.Logger().Debug("Rendered content:\n%s", rendered)

	// Write rendered output
	if err := s.FS().WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("writing rendered config: %w", err)
	}

	return nil
}

// writeK8sDynamicDeployScript generates the deployment script with node type from user data.
func writeK8sDynamicDeployScript(s *sys.System, k8sConfigDir string, userData *userdata.UserData) error {
	// Ensure kubernetes config directory exists
	if err := vfs.MkdirAll(s.FS(), k8sConfigDir, vfs.DirPerm); err != nil {
		return fmt.Errorf("creating kubernetes config directory: %w", err)
	}
	// Extract node type from user data (default: server)
	nodeType := "server"
	isInit := false
	var apiVIP4, apiVIP6, apiHost string

	if userData != nil && userData.Data != nil {
		if rke2Data, ok := userData.Data["rke2"].(map[string]any); ok {
			if t, ok := rke2Data["type"].(string); ok && t != "" {
				nodeType = t
			}
			if init, ok := rke2Data["init"].(bool); ok && init {
				isInit = true
			}
			if v, ok := rke2Data["api_vip4"].(string); ok {
				apiVIP4 = v
			}
			if v, ok := rke2Data["api_vip6"].(string); ok {
				apiVIP6 = v
			}
			if v, ok := rke2Data["api_host"].(string); ok {
				apiHost = v
			}
		}
	}

	// Determine config filename - must match what writeRKE2ConfigFromUserData writes
	configFilename := nodeType + ".yaml"
	if isInit {
		configFilename = "init.yaml"
	}

	s.Logger().Info("Generating deployment script for node type: %s (init=%v, config=%s)", nodeType, isInit, configFilename)

	values := struct {
		NodeType       string
		KubernetesDir  string
		ConfigFilename string
		APIVIP4        string
		APIVIP6        string
		APIHost        string
	}{
		NodeType:       nodeType,
		KubernetesDir:  k8sConfigDir,
		ConfigFilename: configFilename,
		APIVIP4:        apiVIP4,
		APIVIP6:        apiVIP6,
		APIHost:        apiHost,
	}

	data, err := template.Parse(k8sConfDeployDynamicScriptName, config.K8sConfDeployDynamicScriptTpl, &values)
	if err != nil {
		return fmt.Errorf("parsing deployment template: %w", err)
	}

	scriptPath := filepath.Join(k8sConfigDir, k8sConfDeployDynamicScriptName)
	if err := s.FS().WriteFile(scriptPath, []byte(data), 0o744); err != nil {
		return fmt.Errorf("writing deployment script: %w", err)
	}

	s.Logger().Info("Deployment script written to %s", scriptPath)

	return nil
}
