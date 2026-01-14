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
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	"github.com/suse/elemental/v3/pkg/userdata"
)

// ButaneTemplateData holds the data available during butane template rendering.
type ButaneTemplateData struct {
	// UserData contains the user data fetched from cloud provider or filesystem.
	UserData map[string]any
}

// RenderButaneTemplate renders a butane template with user data.
func RenderButaneTemplate(butaneTemplate string, userData *userdata.UserData) (string, error) {
	if butaneTemplate == "" {
		return "", fmt.Errorf("butane template is empty")
	}

	data := ButaneTemplateData{
		UserData: make(map[string]any),
	}

	if userData != nil && userData.Data != nil {
		data.UserData = userData.Data
	}

	funcs := template.FuncMap{
		"join":     strings.Join,
		"default":  defaultValue,
		"required": requiredValue,
		"b64enc":   base64Encode,
		"b64dec":   base64Decode,
		"indent":   indentString,
		"toYaml":   toYaml,
	}

	tmpl, err := template.New("butane").Funcs(funcs).Parse(butaneTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing butane template: %w", err)
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing butane template: %w", err)
	}

	return buf.String(), nil
}

// defaultValue returns the first non-empty value, or the default if all are empty.
func defaultValue(defaultVal any, val any) any {
	if val == nil {
		return defaultVal
	}
	if s, ok := val.(string); ok && s == "" {
		return defaultVal
	}
	return val
}

// requiredValue returns an error if the value is empty.
func requiredValue(name string, val any) (any, error) {
	if val == nil {
		return nil, fmt.Errorf("required value %q is not set", name)
	}
	if s, ok := val.(string); ok && s == "" {
		return nil, fmt.Errorf("required value %q is empty", name)
	}
	return val, nil
}

// base64Encode encodes a string to base64.
func base64Encode(val string) string {
	return base64.StdEncoding.EncodeToString([]byte(val))
}

// base64Decode decodes a base64 string.
func base64Decode(val string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return "", fmt.Errorf("decoding base64: %w", err)
	}
	return string(decoded), nil
}

// indentString indents each line of a string by the given number of spaces.
func indentString(spaces int, val string) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(val, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// toYaml converts a value to YAML-like string representation.
func toYaml(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case []any:
		var lines []string
		for _, item := range v {
			lines = append(lines, fmt.Sprintf("- %v", item))
		}
		return strings.Join(lines, "\n")
	case map[string]any:
		var lines []string
		for key, value := range v {
			lines = append(lines, fmt.Sprintf("%s: %v", key, value))
		}
		return strings.Join(lines, "\n")
	default:
		return fmt.Sprintf("%v", val)
	}
}
