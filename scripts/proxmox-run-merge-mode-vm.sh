#!/usr/bin/env bash
set -euo pipefail

# Clone a merge-mode template and attach the two provider media needed for the
# Proxmox happy path:
#   - cidata ISO: Provider Ignition Config payload
#   - PLATFORM_HINT ISO: platform_id=proxmoxve grubenv

PVE_HOST="${PVE_HOST:-root@10.0.0.101}"
TEMPLATE_ID="${TEMPLATE_ID:-9012}"
VMID="${VMID:-2610}"
VM_NAME="${VM_NAME:-elemental-merge-mode-${VMID}}"
STORAGE="${STORAGE:-local-lvm}"
ISO_STORAGE="${ISO_STORAGE:-local}"
BRIDGE="${BRIDGE:-vmbr0}"
MEMORY="${MEMORY:-4096}"
CORES="${CORES:-2}"
REMOTE_DIR="${REMOTE_DIR:-/var/lib/vz/template/iso/elemental-merge-mode-${VMID}}"
PLATFORM_ID="${PLATFORM_ID:-proxmoxve}"
START_VM="${START_VM:-1}"
FORCE="${FORCE:-0}"
DYNAMIC_USERDATA_FILE="${DYNAMIC_USERDATA_FILE:-}"
PROVIDER_IGNITION_FILE="${PROVIDER_IGNITION_FILE:-}"
SSH_PUBLIC_KEY="${SSH_PUBLIC_KEY:-}"
SSH_PUBLIC_KEY_FILE="${SSH_PUBLIC_KEY_FILE:-${HOME}/.ssh/id_ed25519.pub}"
GUEST_SSH="${GUEST_SSH:-}"

usage() {
  cat <<'USAGE'
Usage: proxmox-run-merge-mode-vm.sh [options]

Clones a template VM, creates Provider Ignition cidata media and PLATFORM_HINT
media on Proxmox, attaches them as SCSI CD-ROMs, and starts the VM.

Options:
  --host HOST                  Proxmox SSH host (default: root@10.0.0.101)
  --template-id ID             Source template VMID (default: 9012)
  --vmid ID                    New VMID (default: 2610)
  --name NAME                  VM name (default: elemental-merge-mode-<vmid>)
  --storage STORAGE            Proxmox VM disk storage (default: local-lvm)
  --iso-storage STORAGE        Proxmox ISO storage (default: local)
  --bridge BRIDGE              Proxmox bridge for net0 (default: vmbr0)
  --memory MB                  VM memory (default: 4096)
  --cores N                    VM cores (default: 2)
  --platform-id ID             Platform hint value (default: proxmoxve)
  --dynamic-userdata PATH      Runtime k8s dynamic userdata YAML
  --provider-ignition PATH     Full provider Ignition JSON payload to use instead
  --ssh-key KEY                SSH public key to inject for root
  --ssh-key-file PATH          SSH public key file (default: ~/.ssh/id_ed25519.pub)
  --guest-ssh TARGET           Optional guest SSH target, for example root@192.168.122.50
  --no-start                   Create/configure VM but do not start it
  --force                      Destroy an existing VM with the same VMID first
  -h, --help                   Show this help

Environment variables with the same uppercase names are also accepted.
USAGE
}

log() {
  printf '[proxmox-merge-vm] %s\n' "$*"
}

die() {
  printf '[proxmox-merge-vm] ERROR: %s\n' "$*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --host) PVE_HOST="$2"; shift 2 ;;
    --template-id) TEMPLATE_ID="$2"; shift 2 ;;
    --vmid) VMID="$2"; VM_NAME="elemental-merge-mode-$2"; shift 2 ;;
    --name) VM_NAME="$2"; shift 2 ;;
    --storage) STORAGE="$2"; shift 2 ;;
    --iso-storage) ISO_STORAGE="$2"; shift 2 ;;
    --bridge) BRIDGE="$2"; shift 2 ;;
    --memory) MEMORY="$2"; shift 2 ;;
    --cores) CORES="$2"; shift 2 ;;
    --platform-id) PLATFORM_ID="$2"; shift 2 ;;
    --dynamic-userdata) DYNAMIC_USERDATA_FILE="$2"; shift 2 ;;
    --provider-ignition) PROVIDER_IGNITION_FILE="$2"; shift 2 ;;
		--ssh-key) SSH_PUBLIC_KEY="$2"; shift 2 ;;
		--ssh-key-file) SSH_PUBLIC_KEY_FILE="$2"; shift 2 ;;
		--guest-ssh) GUEST_SSH="$2"; shift 2 ;;
		--no-start) START_VM=0; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

if [ -z "$SSH_PUBLIC_KEY" ] && [ -f "$SSH_PUBLIC_KEY_FILE" ]; then
  SSH_PUBLIC_KEY="$(tr -d '\n' < "$SSH_PUBLIC_KEY_FILE")"
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

dynamic_userdata="${tmpdir}/dynamic-userdata.yaml"
provider_ignition="${tmpdir}/provider.ign"

if [ -n "$DYNAMIC_USERDATA_FILE" ]; then
  [ -f "$DYNAMIC_USERDATA_FILE" ] || die "dynamic userdata file not found: $DYNAMIC_USERDATA_FILE"
  cp "$DYNAMIC_USERDATA_FILE" "$dynamic_userdata"
else
  cat > "$dynamic_userdata" <<'YAML'
hostname: merge-node.example.com
rke2:
  type: server
  init: true
  token: merge-token
elemental:
  kubernetes:
    deployResources: true
YAML
fi

if [ -n "$PROVIDER_IGNITION_FILE" ]; then
  [ -f "$PROVIDER_IGNITION_FILE" ] || die "provider ignition file not found: $PROVIDER_IGNITION_FILE"
  cp "$PROVIDER_IGNITION_FILE" "$provider_ignition"
else
  python3 - "$dynamic_userdata" "$SSH_PUBLIC_KEY" > "$provider_ignition" <<'PY'
import json
import pathlib
import sys
import urllib.parse

dynamic_path = pathlib.Path(sys.argv[1])
ssh_key = sys.argv[2].strip()
dynamic = dynamic_path.read_text()

payload = {
    "ignition": {"version": "3.5.0"},
    "storage": {
        "files": [
            {
                "path": "/var/lib/elemental/provider-ignition-marker",
                "mode": 420,
                "overwrite": True,
                "contents": {"source": "data:,provider-ok"},
            },
            {
                "path": "/var/lib/elemental/k8s-dynamic/userdata.yaml",
                "mode": 420,
                "overwrite": True,
                "contents": {"source": "data:," + urllib.parse.quote(dynamic)},
            },
        ]
    },
}

if ssh_key:
    payload["passwd"] = {
        "users": [
            {
                "name": "root",
                "sshAuthorizedKeys": [ssh_key],
            }
        ]
    }

json.dump(payload, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")
PY
fi

python3 -m json.tool "$provider_ignition" >/dev/null || die "provider ignition is not valid JSON: $provider_ignition"

log "uploading provider Ignition payload"
ssh "$PVE_HOST" "rm -rf '$REMOTE_DIR' && mkdir -p '$REMOTE_DIR'"
scp "$provider_ignition" "${PVE_HOST}:${REMOTE_DIR}/provider.ign"

log "creating VM ${VMID} from template ${TEMPLATE_ID}"
ssh "$PVE_HOST" bash -s -- \
  "$TEMPLATE_ID" "$VMID" "$VM_NAME" "$STORAGE" "$ISO_STORAGE" "$BRIDGE" "$MEMORY" "$CORES" "$REMOTE_DIR" "$PLATFORM_ID" "$START_VM" "$FORCE" <<'REMOTE'
set -euo pipefail

template_id="$1"
vmid="$2"
vm_name="$3"
storage="$4"
iso_storage="$5"
bridge="$6"
memory="$7"
cores="$8"
remote_dir="$9"
platform_id="${10}"
start_vm="${11}"
force="${12}"

die() {
  printf '[proxmox-merge-vm] ERROR: %s\n' "$*" >&2
  exit 1
}

command -v qm >/dev/null 2>&1 || die "qm not found on Proxmox host"
qm status "$template_id" >/dev/null 2>&1 || die "template VM $template_id does not exist"

cidata_iso="/var/lib/vz/template/iso/vm-${vmid}-cidata.iso"
platform_iso="/var/lib/vz/template/iso/elemental-merge-mode-${vmid}-platform.iso"

make_iso() {
  local label="$1"
  local output="$2"
  local source="$3"

  if command -v xorriso >/dev/null 2>&1; then
    xorriso -as mkisofs -volid "$label" -joliet -rock -output "$output" "$source" >/dev/null
  elif command -v genisoimage >/dev/null 2>&1; then
    genisoimage -volid "$label" -joliet -rock -output "$output" "$source" >/dev/null
  elif command -v mkisofs >/dev/null 2>&1; then
    mkisofs -volid "$label" -joliet -rock -output "$output" "$source" >/dev/null
  else
    die "xorriso, genisoimage, or mkisofs is required on Proxmox host"
  fi
}

if qm status "$vmid" >/dev/null 2>&1; then
  if [ "$force" != "1" ]; then
    die "VM $vmid already exists; rerun with --force to replace it"
  fi
  qm stop "$vmid" >/dev/null 2>&1 || true
  qm destroy "$vmid" --purge >/dev/null 2>&1 || true
fi

rm -rf "${remote_dir}/cidata" "${remote_dir}/platform"
mkdir -p "${remote_dir}/cidata" "${remote_dir}/platform"

cp "${remote_dir}/provider.ign" "${remote_dir}/cidata/user-data"
cat > "${remote_dir}/cidata/meta-data" <<META
instance-id: vm-${vmid}
local-hostname: ${vm_name}
META

if command -v grub2-editenv >/dev/null 2>&1; then
  grub2-editenv "${remote_dir}/platform/grubenv" create
  grub2-editenv "${remote_dir}/platform/grubenv" set "platform_id=${platform_id}"
else
  printf 'platform_id=%s\n' "$platform_id" > "${remote_dir}/platform/grubenv"
fi

make_iso cidata "$cidata_iso" "${remote_dir}/cidata"
make_iso PLATFORM_HINT "$platform_iso" "${remote_dir}/platform"

qm clone "$template_id" "$vmid" --name "$vm_name" --full 1 --storage "$storage" >/dev/null
qm set "$vmid" \
  --memory "$memory" \
  --cores "$cores" \
  --cpu host \
  --net0 "virtio,bridge=${bridge}" \
  --serial0 socket \
  --vga serial0 \
  --bios ovmf \
  --scsihw virtio-scsi-pci \
  --boot order=scsi0 >/dev/null

qm set "$vmid" --scsi1 "${iso_storage}:iso/$(basename "$cidata_iso"),media=cdrom" >/dev/null
qm set "$vmid" --scsi2 "${iso_storage}:iso/$(basename "$platform_iso"),media=cdrom" >/dev/null

qm config "$vmid"

if [ "$start_vm" = "1" ]; then
  qm start "$vmid" >/dev/null
  printf '\nVM started.\n'
else
  printf '\nVM configured but not started.\n'
fi
REMOTE

cat <<EOF

Next commands:
  ssh ${PVE_HOST} "qm terminal ${VMID}"
  ssh ${PVE_HOST} "qm config ${VMID}"
  ssh ${PVE_HOST} "qm stop ${VMID}"

Expected guest checks after first boot:
cat /run/ignition.env
cat /var/lib/elemental/provider-ignition-marker
cat /var/lib/elemental/k8s-dynamic/userdata.yaml
systemctl status elemental-k8s-dynamic.service --no-pager
cat /var/lib/elemental/kubernetes/init.yaml

Guest autogrow checks:
journalctl -b -u elemental-system-autogrow.service --no-pager
lsblk -o NAME,SIZE,FSTYPE,LABEL,MOUNTPOINTS
findmnt /
btrfs filesystem usage /
EOF

if [ -n "$GUEST_SSH" ]; then
	log "running guest autogrow checks via ssh ${GUEST_SSH}"
	ssh "$GUEST_SSH" \
		"systemctl is-failed elemental-system-autogrow.service >/tmp/elemental-autogrow.failed && exit 1 || true; journalctl -b -u elemental-system-autogrow.service --no-pager; lsblk -o NAME,SIZE,FSTYPE,LABEL,MOUNTPOINTS; findmnt /; btrfs filesystem usage /"
fi
