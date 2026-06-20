# System Autogrow

## Purpose

Elemental RAW images often move through providers with different disk-size semantics. Transfer pipelines want small RAW artifacts. Infrastructure providers often import artifact into larger virtual disk. Kubernetes and system components need usable root filesystem capacity after guest boots.

System Autogrow provides guest-side contract for this case. Product Image declares expected provider-expanded system disk size through `raw.systemDiskSize`. Elemental embeds internal kernel command line activation. On initramfs boot, Elemental grows `SYSTEM` partition and btrfs filesystem to available block device capacity before installed root filesystem mounts.

This keeps RAW Artifact Size and System Disk Target Size separate. `raw.diskSize` controls produced RAW file size. `raw.systemDiskSize` documents expected provider disk size and enables Elemental autogrow. Elemental grows to actual block device capacity and does not enforce exact target size after provider import.

## Terminology

- **RAW Artifact Size**: size of generated RAW image file, configured by `raw.diskSize`.
- **System Disk Target Size**: expected provider-expanded guest disk size, configured by `raw.systemDiskSize`.
- **SYSTEM Partition**: Elemental installed root filesystem partition, discovered by filesystem label `SYSTEM`.
- **Autogrow Activation**: internal Elemental kernel command line flag `elemental.system_autogrow=1`.
- **Autogrow Target**: internal Elemental kernel command line argument `elemental.system_autogrow_target=<size>` derived from `raw.systemDiskSize`.
- **Structural Resize Error**: boot-time problem where Elemental cannot safely identify, validate, resize, reread, mount, or grow `SYSTEM`.

important ownership boundary provider controls virtual disk expansion. Elemental controls guest partition and filesystem growth. `raw.systemDiskSize` is not provider API call, not quota, and not exact-size assertion.

## Design Overview

System autogrow relies on build-time activation and initramfs execution:

1. Product Image configuration sets `raw.diskSize` and optional `raw.systemDiskSize`.
2. Elemental build creates RAW artifact sized by `raw.diskSize`.
3. When `raw.systemDiskSize` set, Elemental appends `elemental.system_autogrow=1` and `elemental.system_autogrow_target=<size>` to installer and installed kernel command lines.
4. Provider imports RAW artifact and expands guest disk according provider workflow.
5. Elemental initramfs starts `elemental-system-autogrow.service` when kernel command line contains activation flag.
6. service finds btrfs filesystem labeled `SYSTEM`, expands GPT partition to target or disk capacity, rereads partition table, mounts partition, runs `btrfs filesystem resize max`, then continues boot.

solves three implementation problems:

- RAW artifact can stay small for transfer while deployed guest receives larger usable root filesystem.
- Provider-specific disk expansion remains outside Elemental. Elemental only consumes block device state visible inside guest.
- Resize failure appears during initramfs boot, before Kubernetes or application startup obscures disk-capacity root cause.

## Build-Time Contract

`install.yaml` declares RAW artifact size and expected system disk target size:

```yaml
bootloader: grub
kernelCmdLine: "console=ttyS0"
raw:
  diskSize: 8G
  systemDiskSize: 80G
```

contract intentionally narrow:

- `raw.diskSize` is required for RAW output and defines generated RAW artifact size.
- `raw.systemDiskSize` is optional and enables System Autogrow when present.
- size values use positive integer plus binary suffix `K`, `M`, `G`, or `T`, example `80G`.
- Elemental adds internal kernel command line values derived from `raw.systemDiskSize`.
- provider or import workflow remains responsible expanding virtual disk beyond RAW artifact size.
- Elemental grows to actual block device capacity visible in guest. It does not verify provider disk is exactly `raw.systemDiskSize`.

The generated kernel command line includes:

```text
elemental.system_autogrow=1 elemental.system_autogrow_target=80G
```

these are implementation details. Users should configure `raw.systemDiskSize`, not hand-author internal autogrow flags, unless debugging low-level boot behavior.

## Boot Mechanism

System Autogrow is implemented by dracut module:

```text
pkg/dracut/modules.d/32elemental-system-autogrow/
```

module installs:

- `elemental-system-autogrow.service`
- `/usr/lib/elemental/elemental-system-autogrow.sh`
- required initramfs tools such as `sgdisk`, `blockdev`, `udevadm`, `findmnt`, `blkid`, `btrfs`, `mount`, `umount`, and `lsblk`

The systemd unit runs only when activation flag present:

```ini
ConditionKernelCommandLine=elemental.system_autogrow=1
```

service ordering is initramfs-only. It runs after udev settle and before root filesystem mount and Ignition fetch/disk/file completion units:

```text
After=systemd-udev-settle.service ignition-setup-user.service
Before=initrd-root-fs.target ignition-fetch-offline.service ignition-fetch.service ignition-disks.service ignition-files.service ignition-complete.target
```

this makes filesystem capacity available before installed root mounted and before later first-boot configuration expects final disk shape.

## Resize Contract

Autogrow script performs conservative block-device resize:

1. wait for filesystem label `SYSTEM`;
2. verify `SYSTEM` filesystem type is `btrfs`;
3. derive parent disk, partition number, start sector, sector size, partition size, and disk size;
4. compute target end sector from `elemental.system_autogrow_target=<size>` when present, capped at disk last usable sector;
5. treat already-large-enough partition as successful no-op;
6. read and preserve partition type GUID and unique GUID;
7. run `sgdisk -e` to relocate backup GPT metadata when provider expanded disk;
8. delete and recreate same partition number at same start sector with new end sector, name `SYSTEM`, same type GUID, same unique GUID;
9. reread partition table and settle udev;
10. mount `SYSTEM` under `/run/elemental-system-autogrow`;
11. run `btrfs filesystem resize max`;
12. unmount and continue boot.

Preserving partition identity matters because later boot and upgrade logic may rely on stable partition role and filesystem label. The script changes partition extent, not partition purpose.

When target size smaller than current partition, service logs that `SYSTEM` already satisfies target and still resizes btrfs max. When no extra disk capacity exists, service is successful no-op.

## Installer Reset Contract

Installer reset path also consumes `elemental.system_autogrow_target=<size>` from deployment kernel command lines. Before reconciling partitions, Elemental adjusts configured `SYSTEM` partition size to target minus preceding partition sizes. This keeps reset/reinstall layout consistent with target disk shape when autogrow target is present.

If target smaller than preceding partitions, reset fails with configuration error instead of producing impossible layout. Invalid target syntax also fails early.

## Failure Semantics

System Autogrow failure is intentionally not recoverable by continuing normal boot. Structural or resize errors fail initramfs service and block boot:

- required command missing;
- `SYSTEM` label cannot be found;
- `SYSTEM` is not btrfs;
- parent disk, partition number, start sector, sector size, disk size, type GUID, or unique GUID cannot be derived;
- `sgdisk`, partition reread, mount, btrfs resize, or unmount fails.

This is stricter than dynamic runtime configuration errors. Disk layout failure means installed root capacity and partition identity are uncertain. failing early preserves clear diagnosis through initramfs logs and avoids later Kubernetes failures with misleading symptoms.

## Boundaries And Rejected Approaches

Rejected approaches:

- Provider-specific disk expansion in Elemental. Provider import and virtual disk sizing remain deployment integration responsibility.
- Exact-size enforcement against `raw.systemDiskSize`. Elemental caps target at actual disk capacity and grows to available capacity.
- Filesystem-only resize. Provider-expanded RAW disks need GPT partition expansion before btrfs can use extra space.
- Kubernetes-level remediation. root filesystem capacity must exist before Kubernetes starts.
- Runtime metadata parsing. autogrow reads kernel command line and local block devices only.

boundaries keep image build declarative, provider expansion external, and guest resize provider-agnostic.

## Verification

Automated coverage includes:

- `internal/bootcmdline/system_autogrow_test.go` for kernel command line activation and target handling;
- `internal/build/build_test.go` for `raw.systemDiskSize` to deployment kernel command line mapping;
- `pkg/install/system_autogrow_target_test.go` for reset target sizing;
- `pkg/dracut/modules.d/32elemental-system-autogrow/elemental_system_autogrow_test.go` for initramfs autogrow script behavior.

Manual guest checks after provider import:

```shell
journalctl -b -u elemental-system-autogrow.service --no-pager
lsblk -o NAME,SIZE,FSTYPE,LABEL,MOUNTPOINTS
findmnt /
btrfs filesystem usage /
```

successful boot should show service completed, `SYSTEM` partition using provider-expanded disk capacity, and mounted btrfs filesystem with expanded size.
