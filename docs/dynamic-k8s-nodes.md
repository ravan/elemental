# Dynamic Kubernetes Node Configuration

This guide explains how to use user data to dynamically configure Kubernetes (RKE2) node types at boot time, enabling a single image to be deployed as different node types (init server, joining server, or agent).

## Overview

Traditional multi-node cluster deployment requires building separate images for each node type or pre-defining node hostnames in the image configuration. Dynamic node configuration removes this limitation by determining the node's role at boot time based on user data provided by cloud providers or local filesystems.

**Benefits:**
- Single image for all cluster nodes
- Node type determined at deployment time
- Supports cloud and on-premises deployments
- Flexible cluster topology

## Static vs Dynamic Configuration

Elemental supports two approaches for configuring RKE2 clusters:

### Static Configuration (Hostname-Based)

**Best for:** Bare-metal servers or VMs with known, fixed hostnames.

With static configuration, node roles are assigned at **build time** based on hostname mappings in `kubernetes.yaml`:

```yaml
nodes:
- hostname: node1.example
  type: server
  init: true
- hostname: node2.example
  type: server
- hostname: node3.example
  type: agent
network:
  apiVIP: 192.168.122.100
  apiHost: api.example.com
```

The cluster token is stored in `kubernetes/config/server.yaml`. At boot time, the node looks up its hostname to determine its role.

**Characteristics:**
- Roles determined by hostname at build time
- Token baked into the image
- Must rebuild image to change topology
- See `examples/elemental/customize/multi-node/`

### Dynamic Configuration (User Data-Based)

**Best for:** Cloud environments (AWS, Azure, GCP) with dynamic instances.

With dynamic configuration, node roles are assigned at **boot time** based on user data from cloud provider metadata services:

```yaml
# User data provided at instance launch
rke2:
  type: server
  init: true
  token: my-cluster-token
```

No `nodes:` list is required in `kubernetes.yaml`. The token and role are passed via cloud instance user data.

**Characteristics:**
- Roles determined by user data at boot time
- Token passed at deployment time (more secure)
- Same image for all node types
- Supports auto-scaling and infrastructure-as-code
- See `examples/elemental/customize/dynamic-node/`

### When to Choose

| Scenario | Recommended |
|----------|-------------|
| Bare-metal with fixed hostnames | Static |
| Cloud deployment (AWS, Azure, GCP) | Dynamic |
| Using Terraform/OpenTofu | Dynamic |
| Auto-scaling groups | Dynamic |
| Air-gapped environment | Static |
| Frequently changing topology | Dynamic |

## User Data Format

The node configuration is provided via user data under the `rke2` key:

### Init Node (Cluster Bootstrap)

The init node is the first server that bootstraps the cluster:

```yaml
rke2:
  type: server
  init: true
  token: my-cluster-token
  tls_san:                         # Optional: additional TLS SANs
    - 192.168.122.100
    - api.example.com
```

### Joining Server (Additional Control Plane)

Additional server nodes join the existing cluster:

```yaml
rke2:
  type: server
  server: https://192.168.122.100:9345
  token: my-cluster-token
  api_vip4: 192.168.122.100        # Optional: for /etc/hosts entry
  api_host: api.example.com        # Optional: for /etc/hosts entry
```

### Agent (Worker Node)

Agent nodes join as workers:

```yaml
rke2:
  type: agent
  server: https://192.168.122.100:9345
  token: my-cluster-token
```

## Configuration Fields

### Core Fields (Built-in Support)

These fields are processed directly by the k8s-dynamic service:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `type` | No | `server` | Node type: `server` or `agent` |
| `init` | No | `false` | Set to `true` for the cluster bootstrap node |
| `token` | Conditional | - | Cluster token. Required for joining nodes |
| `server` | Conditional | - | API server URL. Required for joining servers and agents |
| `tls_san` | No | - | Additional TLS Subject Alternative Names (init server only) |
| `api_vip4` | No | - | API VIP (IPv4) for /etc/hosts entry |
| `api_vip6` | No | - | API VIP (IPv6) for /etc/hosts entry |
| `api_host` | No | - | API hostname for /etc/hosts entry |

### Config File Output

The k8s-dynamic service generates the RKE2 config file based on node type:

| Node Type | `init` Flag | Config File |
|-----------|-------------|-------------|
| server | true | `init.yaml` |
| server | false | `server.yaml` |
| agent | false | `agent.yaml` |

## Example Configuration

### Building the Image

Create a configuration directory with user data enabled:

```
dynamic-node/
├── install.yaml      # Boot/install configuration
├── butane.yaml       # System configuration (users, etc.)
├── userdata.yaml     # Enable dynamic configuration
└── kubernetes.yaml   # Helm charts and manifests (no nodes: list)
```

**userdata.yaml:**
```yaml
enabled: true
providers: ["auto"]   # Auto-detect: aws, azure, gcp, filesystem
timeout: 120
retries: 5
```

**kubernetes.yaml:**
```yaml
# No nodes: list required - roles determined at boot from user data
# Helm charts and manifests are deployed by the init node
manifests:
  - https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.31/deploy/local-path-storage.yaml
helm:
  charts:
    - name: "neuvector-crd"
      version: "106.0.0+up2.8.5"
      targetNamespace: "neuvector-system"
      repositoryName: "rancher-charts"
  repositories:
    - name: "rancher-charts"
      url: "https://charts.rancher.io"
```

**install.yaml:**
```yaml
bootloader: grub
kernelCmdLine: "console=ttyS0 quiet loglevel=3"
raw:
  diskSize: 35G
```

Build the image:
```shell
elemental3 build --config ./dynamic-node
```

### Deploying Nodes

#### AWS

Set user data when launching EC2 instances:

**Init node:**
```shell
aws ec2 run-instances \
  --image-id ami-xxxxx \
  --user-data file://init-node-userdata.yaml \
  --instance-type t3.large \
  ...
```

**init-node-userdata.yaml:**
```yaml
rke2:
  type: server
  init: true
  token: my-cluster-token-12345
  tls_san:
    - 10.0.0.100
    - k8s.example.com
```

**Joining server:**
```yaml
rke2:
  type: server
  server: https://10.0.0.100:9345
  token: my-cluster-token-12345
```

**Agent:**
```yaml
rke2:
  type: agent
  server: https://10.0.0.100:9345
  token: my-cluster-token-12345
```

#### On-Premises (cidata ISO)

Create a cidata ISO with the user data:

```shell
# Create user-data file for init node
cat > user-data <<EOF
rke2:
  type: server
  init: true
  token: my-cluster-token
  tls_san:
    - 192.168.122.100
EOF

# Create ISO
mkisofs -output cidata.iso -volid cidata -joliet -rock user-data

# Boot VM with ISO attached
qemu-system-x86_64 \
  -drive file=node.qcow2,format=qcow2 \
  -cdrom cidata.iso \
  ...
```

## HA Cluster Setup Workflow

1. **Deploy Init Node:**
   - Set `init: true` and define the token
   - Include API VIP and hostname in `tls_san`
   - Wait for the node to become ready

2. **Deploy Additional Servers:**
   - Use same token as init node
   - Set `server` to the API VIP URL
   - Deploy one at a time, waiting for each to join

3. **Deploy Agents:**
   - Use same token as servers
   - Set `server` to the API VIP URL
   - Can deploy multiple agents in parallel

## Boot Sequence

1. **Network Online:** System waits for network connectivity
2. **User Data Fetch:** `elemental-k8s-dynamic.service` fetches user data from cloud provider (AWS IMDS, etc.)
3. **Config Generation:** RKE2 config file (`init.yaml`, `server.yaml`, or `agent.yaml`) is generated from user data
4. **Deploy Script:** Dynamic deployment script is generated with correct node type and config filename
5. **Config Installation:** `k8s-config-installer.service` runs the deployment script
6. **RKE2 Start:** The appropriate RKE2 service (`rke2-server` or `rke2-agent`) is enabled and started

## Troubleshooting

### Check K8s Dynamic Service

```shell
systemctl status elemental-k8s-dynamic.service
journalctl -u elemental-k8s-dynamic.service
```

### Check K8s Config Installer

```shell
systemctl status k8s-config-installer.service
journalctl -u k8s-config-installer.service
```

### View Generated Configs

```shell
# Final RKE2 config (copied by deploy script)
cat /etc/rancher/rke2/config.yaml

# Generated config files (before copy)
ls -la /var/lib/elemental/kubernetes/
cat /var/lib/elemental/kubernetes/init.yaml    # or server.yaml, agent.yaml

# Generated deploy script
cat /var/lib/elemental/kubernetes/k8s_conf_deploy.sh
```

### Common Issues

**Node type not detected:**
- Verify user data format (YAML)
- Check `rke2.type` field is set correctly
- Default is `server` if not specified

**Token mismatch:**
- Ensure all nodes use the same cluster token
- Token must be provided for joining nodes

**Server URL unreachable:**
- Verify network connectivity to the init node
- Check firewall rules allow port 9345
- Ensure the init node is ready before joining

## See Also

- [User Data Support](user-data.md) - General user data configuration
- [Configuration Directory Guide](configuration-directory.md) - Build configuration
