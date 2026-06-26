# Dynamic Elemental Services

## Purpose

Elemental Product Images define the operating system, Kubernetes distribution, and product components before a node is deployed. Some deployment data is only known at runtime: the infrastructure platform, the node hostname, the RKE2 role, cluster join data, SSH keys, resource deployment authority, and deployment-specific Helm values.

```bash
elemental customize --mode merge
```

Dynamic Elemental Services provide a contract for that runtime-supplied data without rebuilding the Product Image. The Product Image declares which Elemental runtime services are enabled. Provider Ignition Config, delivered through stock Ignition provider support, writes per-node files on first boot. Elemental services then consume those files by explicit path.

The first implementation is `k8s-dynamic`, which reads Dynamic Node User Data and renders Kubernetes node configuration on first boot.

## Terminology

- **Provider Ignition Config**: Ignition configuration supplied by a cloud provider or filesystem source during merge-mode provisioning.
- **Dynamic Node User Data**: Elemental node configuration consumed at boot to select Kubernetes node role, resource deployment authority, and runtime overrides.
- **Dynamic Service Declaration**: Build-time configuration that enables Elemental runtime services that consume files placed by Provider Ignition Config.
- **Platform Hint**: Pre-Ignition deployment data that identifies the infrastructure platform so Ignition can select its native provider.
- **Merge Mode**: Product Image customization mode where Elemental base Ignition and Provider Ignition Config are combined during installed-system first boot.
- **Recoverable Configuration Error**: A boot-time configuration problem that blocks the affected appliance function while preserving VM access for diagnosis and repair.

The important ownership boundary is that Provider Ignition Config is still Ignition data. Dynamic Node User Data is Elemental data. Provider Ignition Config may write Dynamic Node User Data to disk, but Elemental does not reinterpret provider metadata as Elemental configuration.

## Design Overview

Dynamic services rely on this first-boot flow:

1. Elemental initramfs resolves the Ignition platform before Ignition fetches provider data.
2. Merge mode stages the Product Image base Ignition into the SUSE Ignition system config path.
3. Stock Ignition fetches Provider Ignition Config through the selected native provider.
4. Provider Ignition Config writes runtime files, such as `/var/lib/elemental/k8s-dynamic/userdata.yaml`.
5. Elemental-generated systemd units run dynamic services with explicit `--config` paths.
6. Dynamic services validate the runtime file and write derived appliance configuration or a persistent diagnostic.

This solves three implementation problems:

- Platform selection happens before Ignition provider fetch, so generic virtualization evidence does not force the wrong provider in environments such as Proxmox.
- Product Image base Ignition and Provider Ignition Config are combined by stock Ignition behavior, so Elemental does not need a custom metadata fetcher or provider parser.
- Runtime Elemental configuration remains repairable when the provider successfully writes a bad or missing service input file. Invalid Provider Ignition Config itself remains a stock Ignition failure.

## Boot Mechanisms

Dynamic Elemental Services depend on two initramfs mechanisms and one installed-system mechanism.

The initramfs mechanisms are dracut modules that depend on the Ignition dracut module:

- `29elemental-platform-resolver` installs `elemental-platform-resolver.service` and `elemental-platform-resolver.sh` into the initramfs. The service runs after udev has settled and before `ignition-fetch-offline.service`, `ignition-fetch.service`, `ignition-disks.service`, `ignition-files.service`, and `ignition-complete.target`. It updates `/run/ignition.env` before Ignition fetches provider configuration.
- `30elemental-ignition-merge` installs `elemental-ignition-merge.service` and `elemental-ignition-merge.sh` into the initramfs. The service runs after `ignition-setup-user.service` and before the same Ignition fetch, disk, file, and complete units. It stages the Product Image base Ignition before Ignition evaluates system and provider configuration.

The platform resolver also patches Ignition's systemd generator in the target initramfs. The patch preserves an already resolved `PLATFORM_ID` across later generator reruns, so a value selected from Platform Hint media is not replaced by generic virtualization detection before provider fetch.

The merge helper has two roots to keep distinct:

- The embedded Ignition media root is the source. Merge-mode images carry `/ignition/elemental-merge` as a marker and `/ignition/config.ign` as the flattened Product Image base Ignition. When the ignition media is not already mounted, the helper can mount `/dev/disk/by-label/ignition` at `/ignition` and also handles the nested `/ignition/ignition` layout used by some mounted media.
- Ignition's system configuration root is the handoff target. The helper copies the embedded base config into `/usr/lib/ignition/base.d/10-elemental-base.ign`, which is the SUSE Ignition system config path consumed by stock Ignition on the installed system's first boot.

After that handoff, stock Ignition remains responsible for loading runtime provider data. Provider Ignition Config is fetched through Ignition's selected native provider and may write Dynamic Node User Data to the installed root, for example `/var/lib/elemental/k8s-dynamic/userdata.yaml`. Dracut does not parse that runtime file. It only ensures the platform and base Ignition inputs are ready before Ignition runs.

The installed-system mechanism is the generated Elemental service. For `k8s-dynamic`, `elemental-k8s-dynamic.service` runs after Ignition has written files and calls `elemental3ctl k8s-dynamic apply --config <path>`. That command is the first Elemental component that reads Dynamic Node User Data.

## Ignition Platform Resolution

Elemental installs an initramfs platform resolver so the target system can write Ignition's `/run/ignition.env` before Ignition fetches Provider Ignition Config.

The resolver uses this precedence:

1. Existing `PLATFORM_ID` from Ignition's generated `/run/ignition.env`, including explicit kernel command line `ignition.platform.id`.
2. Platform Hint boot media.
3. Local non-network cloud detection for AWS, GCP, and Azure.
4. No change to Ignition behavior when no confident platform can be resolved.

Platform Hint boot media uses this contract:

- filesystem label: `PLATFORM_HINT`
- file path: `/grubenv`
- key: `platform_id`
- accepted values: `proxmoxve`, `kubevirt`, `openstack`, `metal`, `qemu`

The `/grubenv` file must be a GRUB environment file, for example:

```shell
mkdir -p platform-hint
grub2-editenv platform-hint/grubenv create
grub2-editenv platform-hint/grubenv set platform_id=proxmoxve
xorriso -as mkisofs -volid PLATFORM_HINT -joliet -rock -output platform-hint.iso platform-hint
```

The resolver reads only the `platform_id` key, validates the allow-list, and writes the selected value as `PLATFORM_ID` in `/run/ignition.env`. It does not source or execute `/grubenv`. `ELEMENTAL_PLATFORM` is not recognized as an alias.

Deployment integrations own creating and attaching Platform Hint media. Elemental does not provide a first-class CLI generator for the media in the first implementation.

## Merge Mode

`elemental3 customize --mode merge` creates an embedded ignition partition like embedded mode and adds the marker file:

```text
/ignition/elemental-merge
```

The Product Image base Ignition is flattened into:

```text
/ignition/config.ign
```

On installed-system first boot, the Elemental initramfs merge helper stages that base Ignition into the SUSE Ignition system config path:

```text
/usr/lib/ignition/base.d/10-elemental-base.ign
```

Stock Ignition then fetches Provider Ignition Config through the native provider selected by `PLATFORM_ID`. The provider config is responsible for writing runtime files. Elemental runtime services consume those files later.

Merge mode scope is the installed system's first boot. It does not define ISO live-boot merge semantics. For ISO output, merge behavior applies after installation, when the installed system boots for the first time.

## Dynamic Service Contract

A Product Image enables file-driven runtime services with `dynamic_service.yaml`:

```yaml
services:
  k8s-dynamic:
    enabled: true
    config: /var/lib/elemental/k8s-dynamic/userdata.yaml
    timeout: 120
```

The contract is intentionally narrow:

- `services.<serviceName>.enabled` opts in to a generated Elemental runtime service.
- `config` is the explicit runtime file path passed to the service.
- `timeout` is the systemd service timeout in seconds.
- Dynamic services are valid only for merge-mode images.
- Provider Ignition Config writes runtime files.
- Elemental services consume runtime files and write derived appliance configuration.

The generated unit must pass the configured file path explicitly. For `k8s-dynamic`, the unit runs:

```shell
elemental3ctl k8s-dynamic apply --config /var/lib/elemental/k8s-dynamic/userdata.yaml
```

Missing or invalid service input files are Recoverable Configuration Errors for the dynamic service. They should be recorded in persistent diagnostics so the node remains accessible for repair.

## Dynamic Kubernetes Example

`k8s-dynamic` is the first Dynamic Elemental Service. It is enabled through `dynamic_service.yaml` and generated only for merge-mode images.

Defaults:

- config path: `/var/lib/elemental/k8s-dynamic/userdata.yaml`
- status path: `/var/lib/elemental/k8s-dynamic/status.yaml`
- timeout: `120`

Build-time configuration:

```text
dynamic-node/
├── install.yaml
├── release.yaml
├── butane.yaml
├── dynamic_service.yaml
└── kubernetes/
    └── cluster.yaml
```

```shell
elemental3 customize --mode merge --config-dir ./dynamic-node
```

Provider Ignition Config writes Dynamic Node User Data using normal Ignition storage configuration:

```json
{
  "ignition": { "version": "3.5.0" },
  "storage": {
    "files": [
      {
        "path": "/var/lib/elemental/k8s-dynamic/userdata.yaml",
        "mode": 420,
        "overwrite": true,
        "contents": {
          "source": "data:,hostname%3A%20node1.example.com%0Arke2%3A%0A%20%20type%3A%20server%0A%20%20init%3A%20true%0A%20%20token%3A%20my-cluster-token%0A"
        }
      }
    ]
  }
}
```

Dynamic Node User Data is Elemental YAML. Core fields:

```yaml
hostname: node1.example.com
rke2:
  type: server
  init: true
  token: my-cluster-token
elemental:
  kubernetes:
    deployResources: true
helm:
  values:
    rancher:
      hostname: rancher.example.com
users:
  - name: root
    ssh_authorized_keys:
      - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForDynamicNode root@example
```

`elemental3ctl k8s-dynamic apply --config <path>` validates the Dynamic Node User Data before writing derived files. When validation succeeds, it can:

- write `/etc/hostname`;
- write SSH authorized keys;
- render RKE2 `init.yaml`, `server.yaml`, or `agent.yaml`;
- create the resource deployment marker when `elemental.kubernetes.deployResources` is enabled;
- apply runtime Helm value overrides to image-generated HelmChart resources.

`rke2.type` selects `server` or `agent`. `rke2.init: true` writes `init.yaml`. Joining nodes provide `rke2.server` and `rke2.token`.

`elemental.kubernetes.deployResources` controls whether the node may deploy bundled manifests and Helm charts. When omitted, it defaults to `true` for simple deployments. Multi-node lifecycle flows should set it explicitly.

Runtime Helm value overrides may change values only for Helm charts already enabled by the Product Image. Unknown chart names or invalid overrides are Recoverable Configuration Errors recorded in `/var/lib/elemental/k8s-dynamic/status.yaml`. Missing or invalid Dynamic Node User Data is also recorded there and can be repaired by writing a corrected file and rerunning:

```shell
elemental3ctl k8s-dynamic apply --config <path>
```

## Rationale And Boundaries

Rejected approaches:

- Custom provider metadata parsing in Elemental. This would duplicate stock Ignition provider behavior and change failure semantics for invalid Provider Ignition Config.
- GRUB-based platform detection as the primary contract. The resolver reads a GRUB environment file from Platform Hint media, but it runs in initramfs and writes Ignition's environment before provider fetch.
- Nested inline Ignition merge payloads. Merge mode stages flattened Product Image base Ignition into `/usr/lib/ignition/base.d/10-elemental-base.ign`, then lets stock Ignition handle the provider config.
- ISO live-boot merge semantics. Merge mode applies to the installed system's first boot, not to live ISO execution.

These boundaries keep provider-specific data delivery in Ignition and Elemental-specific runtime behavior in explicit Elemental services.

## Verification

Existing coverage is split by contract boundary:

- Platform resolver unit coverage in `pkg/dracut/modules.d/29elemental-platform-resolver/elemental_platform_resolver_test.go`.
- Merge staging unit coverage in `pkg/dracut/modules.d/30elemental-ignition-merge/elemental_ignition_merge_test.go`.
- Dynamic-service merge-mode validation and generated unit coverage in `internal/cli/action/customize_test.go` and `internal/config/ignition_test.go`.
- `k8s-dynamic` runtime validation, status, RKE2 rendering, SSH keys, hostname, deployment marker, and Helm override coverage in `internal/cli/action/k8s_dynamic_test.go`.
- Proxmox merge-mode boot-flow integration coverage in `tests/integration/mergemode/proxmox_merge_mode_test.go`.
