#!/bin/bash
# Dynamic K8s configuration deployment script
# This script is generated at boot time based on user data

set -euo pipefail

NODETYPE="{{ .NodeType }}"
CONFIGFILE="{{ .KubernetesDir }}/{{ .ConfigFilename }}"

echo "Configuring node as ${NODETYPE}"

mkdir -p /etc/rancher/rke2
echo "Copying RKE2 config file ${CONFIGFILE}"
cp "${CONFIGFILE}" /etc/rancher/rke2/config.yaml

{{- if and .APIVIP4 .APIHost }}
grep -q "{{ .APIVIP4 }} {{ .APIHost }}" /etc/hosts \
  || echo "{{ .APIVIP4 }} {{ .APIHost }}" >> /etc/hosts
{{- end }}

{{- if and .APIVIP6 .APIHost }}
grep -q "{{ .APIVIP6 }} {{ .APIHost }}" /etc/hosts \
  || echo "{{ .APIVIP6 }} {{ .APIHost }}" >> /etc/hosts
{{- end }}

echo "Installing RKE2 from embedded artifacts..."

export INSTALL_RKE2_ARTIFACT_PATH="{{ .InstallPath }}"
export INSTALL_RKE2_TAR_PREFIX=/opt/rke2

if ! sh "{{ .InstallScript }}"; then
  echo "Error: RKE2 installation failed" >&2
  exit 1
fi

echo "Enabling RKE2 ${NODETYPE} service"
systemctl enable --now rke2-${NODETYPE}.service
