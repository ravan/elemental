# UC Base OS Patch

Builds a patched Elemental CLI image plus temporary UC base OS and installer ISO
images that replace the published `/usr/bin/elemental3ctl` with the locally
built Elemental binary.

This works around published UC images that still miss the snapshotted `/etc`
SELinux relabel pass.

Source baseline: `registry.suse.com/elemental/base-os-kernel-default:16.0`

Required initrd tools for SYSTEM autogrow: `sgdisk`, `blockdev`, `udevadm`, `findmnt`, `blkid`, `btrfs`, `mount`, `umount`, `lsblk`.

Unsupported tools intentionally not used: `growpart`, `partprobe`, `parted`, `sfdisk`.

## Build

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml build
task --taskfile uc-base-os-patch/Taskfile.yaml verify
```

For a clean rebuild, clear Docker buildx cache first:

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml clean-docker-cache
```

## Publish

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml publish
```

This publishes a patched replacement for users who normally consume the
official Elemental CLI image:

```text
registry.suse.com/elemental/elemental:3.0
```

The default patched image is:

```text
docker.io/ravan/elemental:3.0-elemental-userdata
```

To build, verify, and publish the patched Elemental CLI, patched UC images, and
patched core release manifest images in one step:

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml build-publish
```

Publish tasks print the patched image references and YAML-ready
`corePlatform.image` values at the end.
The standalone `publish` task runs verification before pushing, including an
ISO live-root SELinux label check for critical paths.

Defaults:

- `ELEMENTAL_VERSION=3.0`
- `BASE_OS_VERSION=16.0`
- `PATCH_VERSION=elemental-userdata`
- `SOURCE_IMAGE=registry.suse.com/elemental/base-os-kernel-default:${BASE_OS_VERSION}`
- `SOURCE_ISO_IMAGE=registry.suse.com/elemental/base-os-kernel-default-iso:${BASE_OS_VERSION}`
- `ELEMENTAL_IMAGE=elemental-image:latest` local-only helper image for OS/ISO patching
- `TARGET_ELEMENTAL_IMAGE=docker.io/ravan/elemental:${ELEMENTAL_VERSION}-${PATCH_VERSION}`
- `TARGET_IMAGE=docker.io/ravan/base-os-kernel-default:${BASE_OS_VERSION}-${PATCH_VERSION}`
- `TARGET_ISO_IMAGE=docker.io/ravan/base-os-kernel-default-iso:${BASE_OS_VERSION}-${PATCH_VERSION}`
- `TARGET_IMAGE_IN_CORE_MANIFEST=index.docker.io/ravan/base-os-kernel-default:${BASE_OS_VERSION}-${PATCH_VERSION}`
- `TARGET_ISO_IMAGE_IN_CORE_MANIFEST=index.docker.io/ravan/base-os-kernel-default-iso:${BASE_OS_VERSION}-${PATCH_VERSION}`
- `SOURCE_CORE_MANIFEST_REPO=elemental/rke2/rke2-manifest`
- `TARGET_CORE_MANIFEST_REPO=docker.io/ravan/release-manifest`
- `CORE_MANIFEST_TAGS="1.35.5"`
- `CORE_MANIFEST_SUFFIX=${PATCH_VERSION}`
- `PLATFORM=linux/amd64`

Override any value at invocation time:

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml build-publish \
  ELEMENTAL_VERSION=3.0 \
  BASE_OS_VERSION=16.0 \
CORE_MANIFEST_TAGS=1.35.5
```

Use `PATCH_VERSION` for patch subversions while keeping upstream versions unchanged:

```bash
task --taskfile uc-base-os-patch/Taskfile.yaml build-publish \
PATCH_VERSION=elemental-userdata.1
```

That publishes image references like:

```text
docker.io/ravan/elemental:3.0-elemental-userdata.1
docker.io/ravan/base-os-kernel-default:16.0-elemental-userdata.1
docker.io/ravan/base-os-kernel-default-iso:16.0-elemental-userdata.1
docker.io/ravan/release-manifest:1.35.5-elemental-userdata.1
```

The published Elemental CLI replacement is:

```yaml
image: docker.io/ravan/elemental:3.0-elemental-userdata
```

The patched core release manifest references the patched image under:

```yaml
components:
  operatingSystem:
    image:
    base: docker.io/ravan/base-os-kernel-default:16.0-elemental-userdata
    iso: docker.io/ravan/base-os-kernel-default-iso:16.0-elemental-userdata
```

Use the canonical `index.docker.io/...` form in the core manifest when the
published image lives on Docker Hub. ELM canonicalizes `docker.io` to
`index.docker.io` in the generated System Upgrade Controller script, and the
`upgrader --local` path expects that exact reference to be present in node
containerd.

The base image covers installed OS and upgrade paths. The ISO image covers
seed RAW first boot and recovery/reset paths, because `elemental3 customize
--type raw` copies the ISO live environment into the RAW `RECOVERY` partition.

For older core release manifests that still contain a required `elemental3ctl`
systemd extension, the patch step also removes that extension. Latest upstream
manifests provide `elemental3ctl` from the base OS and ISO images instead;
leaving the old sysext in place can mask the patched binary at boot.

Appliance `application.yaml` files should reference the patched core release
manifest image, for example:

```yaml
corePlatform:
  image: docker.io/ravan/release-manifest:1.35.5-elemental-userdata
```

ELM does not need to be rebuilt for this workaround. It reads the OS and ISO
images from the resolved core release manifest.
