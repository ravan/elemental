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

echo "Enabling RKE2 ${NODETYPE} service"
systemctl enable --now rke2-${NODETYPE}.service
