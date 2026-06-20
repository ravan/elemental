#!/usr/bin/env bash
set -euo pipefail

CMDLINE_PATH="${ELEMENTAL_SYSTEM_AUTOGROW_CMDLINE:-/proc/cmdline}"
DEV_ROOT="${ELEMENTAL_SYSTEM_AUTOGROW_DEV_ROOT:-/dev}"
RUN_ROOT="${ELEMENTAL_SYSTEM_AUTOGROW_RUN_ROOT:-/run}"
LOG_TO_STDERR="${ELEMENTAL_SYSTEM_AUTOGROW_LOG_TO_STDERR:-0}"
SYSTEM_PART_WAIT_SECONDS="${ELEMENTAL_SYSTEM_AUTOGROW_WAIT_SECONDS:-10}"

log() {
	if [ "$LOG_TO_STDERR" = "1" ]; then
		printf '[elemental-system-autogrow] %s\n' "$*" >&2
	else
		printf '[elemental-system-autogrow] %s\n' "$*"
	fi
}

die() {
	log "ERROR: $*"
	exit 1
}

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command missing: $1"
}

run_or_die() {
	"$@" || die "command failed: $*"
}

size_to_bytes() {
	case "$1" in
	*[Kk]) echo $((${1%[Kk]} * 1024)) ;;
		*[Mm]) echo $((${1%[Mm]} * 1024 * 1024)) ;;
		*[Gg]) echo $((${1%[Gg]} * 1024 * 1024 * 1024)) ;;
		*[Tt]) echo $((${1%[Tt]} * 1024 * 1024 * 1024 * 1024)) ;;
		*) die "invalid elemental.system_autogrow_target value: $1" ;;
	esac
}

find_system_part() {
	local system_link="$DEV_ROOT/disk/by-label/SYSTEM"
	local candidate=""

	udevadm settle || true

	if [ -e "$system_link" ]; then
		readlink -f "$system_link"
		return 0
	fi

	candidate="$(blkid -L SYSTEM 2>/dev/null || true)"
	if [ -n "$candidate" ]; then
		printf '%s\n' "$candidate"
		return 0
	fi

	candidate="$(blkid -o device -t LABEL=SYSTEM 2>/dev/null | awk 'NR == 1 {print; exit}' || true)"
	if [ -n "$candidate" ]; then
		printf '%s\n' "$candidate"
		return 0
	fi

	candidate="$(lsblk -nrpo NAME,LABEL 2>/dev/null | awk '$2 == "SYSTEM" {print $1; exit}' || true)"
	if [ -n "$candidate" ]; then
		printf '%s\n' "$candidate"
		return 0
	fi

	for candidate in "$DEV_ROOT"/sd*[0-9] "$DEV_ROOT"/vd*[0-9] "$DEV_ROOT"/xvd*[0-9] "$DEV_ROOT"/nvme*n*p*; do
		[ -b "$candidate" ] || continue
		if [ "$(blkid -o value -s LABEL "$candidate" 2>/dev/null || true)" = "SYSTEM" ]; then
			printf '%s\n' "$candidate"
			return 0
		fi
	done

	return 1
}

wait_for_system_part() {
	local attempt=0
	local system_part=""

	while [ "$attempt" -le "$SYSTEM_PART_WAIT_SECONDS" ]; do
		system_part="$(find_system_part || true)"
		if [ -n "$system_part" ]; then
			printf '%s\n' "$system_part"
			return 0
		fi

		attempt=$((attempt + 1))
		[ "$attempt" -le "$SYSTEM_PART_WAIT_SECONDS" ] || break
		log "waiting for SYSTEM filesystem label (${attempt}/${SYSTEM_PART_WAIT_SECONDS})"
		sleep 1
	done

	return 1
}

autogrow_enabled=0
autogrow_target=""
for field in $(tr ' ' '\n' < "$CMDLINE_PATH"); do
	case "$field" in
		elemental.system_autogrow=1)
			autogrow_enabled=1
			;;
		elemental.system_autogrow_target=*)
			autogrow_target="${field#elemental.system_autogrow_target=}"
			;;
	esac
done

if [ "$autogrow_enabled" != "1" ]; then
	log "kernel flag not present; skipping"
	exit 0
fi

for cmd in sgdisk blockdev udevadm findmnt blkid btrfs mount umount lsblk sleep; do
	require_cmd "$cmd"
done

system_part="$(wait_for_system_part || true)"
[ -n "$system_part" ] || die "SYSTEM filesystem label not found"

fs_type="$(blkid -o value -s TYPE "$system_part" 2>/dev/null || true)"
[ "$fs_type" = "btrfs" ] || die "SYSTEM filesystem must be btrfs, got ${fs_type:-unknown}"

parent_name="$(lsblk -ndo PKNAME "$system_part" | tr -d '[:space:]')"
[ -n "$parent_name" ] || die "could not derive parent disk for $system_part"
case "$parent_name" in
	/dev/*) disk="$parent_name" ;;
	*) disk="$DEV_ROOT/$parent_name" ;;
esac

part_num="$(lsblk -ndo PARTN "$system_part" | tr -d '[:space:]')"
[ -n "$part_num" ] || die "could not derive partition number for $system_part"

start_sector="$(lsblk -ndo START "$system_part" | tr -d '[:space:]')"
[ -n "$start_sector" ] || die "could not derive SYSTEM partition start sector"

system_size_bytes="$(lsblk -bndo SIZE "$system_part" | tr -d '[:space:]')"
sector_size="$(blockdev --getss "$disk" | tr -d '[:space:]')"
disk_size_bytes="$(blockdev --getsize64 "$disk" | tr -d '[:space:]')"
[ -n "$system_size_bytes" ] || die "could not derive SYSTEM partition size"
[ -n "$sector_size" ] || die "could not derive disk sector size"
[ -n "$disk_size_bytes" ] || die "could not derive disk size"

system_end_sector=$((start_sector + (system_size_bytes / sector_size) - 1))
disk_last_usable_sector=$(((disk_size_bytes / sector_size) - 34))
target_end_sector="$disk_last_usable_sector"
grow_partition=1
if [ -n "$autogrow_target" ]; then
	target_size_bytes="$(size_to_bytes "$autogrow_target")"
	target_end_sector=$(((target_size_bytes / sector_size) - 1))
	if [ "$target_end_sector" -gt "$disk_last_usable_sector" ]; then
		target_end_sector="$disk_last_usable_sector"
	fi
fi

while read -r name type start; do
	[ "$type" = "part" ] || continue
	[ "$name" != "$system_part" ] || continue
	if [ "$start" -gt "$start_sector" ]; then
		die "SYSTEM partition must be the last partition on $disk"
	fi
done < <(lsblk -nrpo NAME,TYPE,START "$disk")

if [ "$system_end_sector" -ge "$target_end_sector" ]; then
	log "SYSTEM partition already consumes target disk capacity; resizing filesystem only"
	grow_partition=0
fi

if [ "$grow_partition" = "1" ]; then
	part_info="$(sgdisk -i "$part_num" "$disk")" || die "command failed: sgdisk -i $part_num $disk"
	type_guid="$(printf '%s\n' "$part_info" | awk -F: '/Partition GUID code/ {gsub(/^[ \t]+/, "", $2); sub(/[ \t]+\(.*$/, "", $2); print $2}')"
	unique_guid="$(printf '%s\n' "$part_info" | awk -F: '/Partition unique GUID/ {gsub(/^[ \t]+/, "", $2); print $2}')"
	[ -n "$type_guid" ] || die "could not read SYSTEM partition type GUID"
	[ -n "$unique_guid" ] || die "could not read SYSTEM partition unique GUID"

	run_or_die sgdisk -e "$disk"
	run_or_die sgdisk -d "$part_num" \
		-n "${part_num}:${start_sector}:${target_end_sector}" \
		-c "${part_num}:SYSTEM" \
		-t "${part_num}:${type_guid}" \
		-u "${part_num}:${unique_guid}" \
		"$disk"
	run_or_die blockdev --rereadpt "$disk"
	run_or_die udevadm settle
fi

mount_dir="$RUN_ROOT/elemental-system-autogrow"
mounted=0
cleanup() {
	if [ "$mounted" = "1" ]; then
		umount "$mount_dir" 2>/dev/null || true
	fi
	rmdir "$mount_dir" 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p "$mount_dir"
run_or_die mount "$system_part" "$mount_dir"
mounted=1
run_or_die btrfs filesystem resize max "$mount_dir"
run_or_die umount "$mount_dir"
mounted=0
rmdir "$mount_dir" 2>/dev/null || true
trap - EXIT

log "SYSTEM partition filesystem autogrow complete"
