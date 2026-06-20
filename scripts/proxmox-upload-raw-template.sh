#!/usr/bin/env bash
set -euo pipefail

# Upload a locally built Elemental raw disk to Proxmox and turn it into a VM
# template. The script intentionally does not build the raw image; use
# scripts/build-merge-raw-in-docker.sh for the Docker/Linux build loop first.

PVE_HOST="${PVE_HOST:-root@10.0.0.101}"
TEMPLATE_ID="${TEMPLATE_ID:-9012}"
TEMPLATE_NAME="${TEMPLATE_NAME:-elemental-merge-mode-template}"
RAW_IMAGE="${RAW_IMAGE:-/tmp/elemental-merge-raw/out/merge-mode.raw}"
STORAGE="${STORAGE:-local-lvm}"
BRIDGE="${BRIDGE:-vmbr0}"
MEMORY="${MEMORY:-4096}"
CORES="${CORES:-2}"
DISK_SIZE="${DISK_SIZE:-18G}"
SYSTEM_DISK_SIZE="${SYSTEM_DISK_SIZE:-18G}"
REMOTE_DIR="${REMOTE_DIR:-/var/lib/vz/template/iso/elemental-merge-mode-template-${TEMPLATE_ID}}"
REMOTE_RAW_NAME="${REMOTE_RAW_NAME:-$(basename "$RAW_IMAGE")}"
FORCE="${FORCE:-0}"
REQUIRE_LABELS="${REQUIRE_LABELS:-EFI RECOVERY ignition SYSTEM}"
USE_RSYNC="${USE_RSYNC:-0}"

usage() {
  cat <<'USAGE'
Usage: proxmox-upload-raw-template.sh [options]

Uploads a raw Elemental disk image to Proxmox, imports it as scsi0, configures
the VM for serial-console boot, and marks it as a template.

Options:
  --host HOST            Proxmox SSH host (default: root@10.0.0.101)
  --template-id ID       Template VMID (default: 9012)
  --name NAME            Template name (default: elemental-merge-mode-template)
  --raw PATH             Local raw disk path (default: /tmp/elemental-merge-raw/out/merge-mode.raw)
  --storage STORAGE      Proxmox VM disk storage (default: local-lvm)
  --bridge BRIDGE        Proxmox bridge for net0 (default: vmbr0)
  --memory MB            Template memory (default: 4096)
  --cores N              Template cores (default: 2)
  --disk-size SIZE       Proxmox-side scsi0 size after import (default: 18G)
  --system-disk-size SIZE Expected Elemental system disk target size (default: 18G)
  --remote-dir PATH      Remote staging directory
  --force                Destroy an existing VM/template with the same ID first
  -h, --help             Show this help

Environment variables with the same uppercase names are also accepted.
USAGE
}

log() {
  printf '[proxmox-template] %s\n' "$*"
}

die() {
	printf '[proxmox-template] ERROR: %s\n' "$*" >&2
	exit 1
}

size_to_mib() {
	case "$1" in
		*K) echo $((${1%K} / 1024)) ;;
		*M) echo "${1%M}" ;;
		*G) echo $((${1%G} * 1024)) ;;
		*T) echo $((${1%T} * 1024 * 1024)) ;;
		*) die "invalid size: $1" ;;
	esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --host) PVE_HOST="$2"; shift 2 ;;
    --template-id) TEMPLATE_ID="$2"; shift 2 ;;
    --name) TEMPLATE_NAME="$2"; shift 2 ;;
    --raw) RAW_IMAGE="$2"; shift 2 ;;
    --storage) STORAGE="$2"; shift 2 ;;
    --bridge) BRIDGE="$2"; shift 2 ;;
		--memory) MEMORY="$2"; shift 2 ;;
		--cores) CORES="$2"; shift 2 ;;
		--disk-size) DISK_SIZE="$2"; shift 2 ;;
		--system-disk-size) SYSTEM_DISK_SIZE="$2"; shift 2 ;;
		--remote-dir) REMOTE_DIR="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ -f "$RAW_IMAGE" ] || die "raw image not found: $RAW_IMAGE"
[ -s "$RAW_IMAGE" ] || die "raw image is empty: $RAW_IMAGE"

if [ -n "${SYSTEM_DISK_SIZE:-}" ]; then
	[ "$(size_to_mib "$DISK_SIZE")" -ge "$(size_to_mib "$SYSTEM_DISK_SIZE")" ] || die "DISK_SIZE=${DISK_SIZE} is smaller than SYSTEM_DISK_SIZE=${SYSTEM_DISK_SIZE}"
fi

if command -v qemu-img >/dev/null 2>&1; then
  log "local image metadata"
  qemu-img info "$RAW_IMAGE"
else
  log "qemu-img not found locally; skipping local image metadata check"
fi

if command -v parted >/dev/null 2>&1; then
  labels="$(parted -s "$RAW_IMAGE" print 2>/dev/null || true)"
  for label in $REQUIRE_LABELS; do
    printf '%s\n' "$labels" | grep -Eq "[[:space:]]${label}([[:space:]]|$)" || die "raw image missing expected partition label/name: ${label}"
  done
else
  log "parted not found locally; relying on prior raw-builder partition validation"
fi

remote_raw="${REMOTE_DIR}/${REMOTE_RAW_NAME}"
log "creating remote staging directory ${PVE_HOST}:${REMOTE_DIR}"
ssh "$PVE_HOST" "mkdir -p '$REMOTE_DIR'"

log "uploading ${RAW_IMAGE} to ${PVE_HOST}:${remote_raw}"
if [ "$USE_RSYNC" = "1" ]; then
    command -v rsync >/dev/null 2>&1 || die "USE_RSYNC=1 but local rsync not found"
    ssh "$PVE_HOST" "command -v rsync >/dev/null 2>&1" || die "USE_RSYNC=1 but remote rsync not found"
    rsync --sparse "$RAW_IMAGE" "${PVE_HOST}:${remote_raw}"
else
    scp "$RAW_IMAGE" "${PVE_HOST}:${remote_raw}"
fi

log "creating Proxmox template ${TEMPLATE_ID} (${TEMPLATE_NAME})"
ssh "$PVE_HOST" bash -s -- \
  "$TEMPLATE_ID" "$TEMPLATE_NAME" "$remote_raw" "$STORAGE" "$BRIDGE" "$MEMORY" "$CORES" "$FORCE" "$DISK_SIZE" <<'REMOTE'
set -euo pipefail

template_id="$1"
template_name="$2"
raw_image="$3"
storage="$4"
bridge="$5"
memory="$6"
cores="$7"
force="$8"
disk_size="$9"

die() {
  printf '[proxmox-template] ERROR: %s\n' "$*" >&2
  exit 1
}

[ -f "$raw_image" ] || die "remote raw image not found: $raw_image"
command -v qm >/dev/null 2>&1 || die "qm not found on Proxmox host"

if qm status "$template_id" >/dev/null 2>&1; then
  if [ "$force" != "1" ]; then
    die "VM/template $template_id already exists; rerun with --force to replace it"
  fi
  qm stop "$template_id" >/dev/null 2>&1 || true
  qm destroy "$template_id" --purge >/dev/null 2>&1 || true
fi

qm create "$template_id" \
  --name "$template_name" \
  --memory "$memory" \
  --cores "$cores" \
  --cpu host \
  --net0 "virtio,bridge=${bridge}" \
  --serial0 socket \
  --vga serial0 \
  --bios ovmf \
  --ostype l26 \
  --scsihw virtio-scsi-pci \
  --agent 0

qm importdisk "$template_id" "$raw_image" "$storage" >/dev/null
disk_ref="$(qm config "$template_id" | awk -F': ' '/^unused[0-9]+:/{print $2; exit}')"
[ -n "$disk_ref" ] || die "could not find imported disk as unused volume"

qm set "$template_id" --scsi0 "$disk_ref" >/dev/null
qm resize "$template_id" scsi0 "$disk_size" >/dev/null
qm set "$template_id" --efidisk0 "${storage}:1,efitype=4m,pre-enrolled-keys=0" >/dev/null
qm set "$template_id" --boot order=scsi0 >/dev/null
qm template "$template_id"

qm config "$template_id"
REMOTE

log "template ready: ${TEMPLATE_ID}"
