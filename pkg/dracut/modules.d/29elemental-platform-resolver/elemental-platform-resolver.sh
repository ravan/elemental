#!/usr/bin/env bash
set -eu

IGNITION_ENV="${ELEMENTAL_PLATFORM_RESOLVER_IGNITION_ENV:-/run/ignition.env}"
KERNEL_CMDLINE="${ELEMENTAL_PLATFORM_RESOLVER_KERNEL_CMDLINE:-/proc/cmdline}"
BY_LABEL_DIR="${ELEMENTAL_PLATFORM_RESOLVER_BY_LABEL_DIR:-/dev/disk/by-label}"
MOUNT_BASE="${ELEMENTAL_PLATFORM_RESOLVER_MOUNT_BASE:-/run/elemental/platform-hint}"
SYS_ROOT="${ELEMENTAL_PLATFORM_RESOLVER_SYS_ROOT:-/sys}"
LOG_TO_STDERR="${ELEMENTAL_PLATFORM_RESOLVER_LOG_TO_STDERR:-}"
HINT_WAIT_SECONDS="${ELEMENTAL_PLATFORM_RESOLVER_HINT_WAIT_SECONDS:-10}"

log() {
	local msg="elemental-platform-resolver: $*"
	if [ -n "$LOG_TO_STDERR" ]; then
		printf '%s\n' "$msg" >&2
	else
		printf '%s\n' "$msg" >/dev/kmsg 2>/dev/null || true
	fi
}

env_value() {
	local key="$1"

	[ -f "$IGNITION_ENV" ] || return 1
	awk -F= -v key="$key" '$1 == key { print substr($0, length(key) + 2); found=1 } END { exit found ? 0 : 1 }' "$IGNITION_ENV"
}

cmdline_platform_id() {
	[ -r "$KERNEL_CMDLINE" ] || return 1

	local arg value
	value=""
	for arg in $(cat "$KERNEL_CMDLINE"); do
		case "$arg" in
			ignition.platform.id=*) value="${arg#*=}" ;;
		esac
	done

	[ -n "$value" ] || return 1
	printf '%s\n' "$value"
}

write_platform_id() {
	local platform="$1"
	local dir tmp

	dir="${IGNITION_ENV%/*}"
	[ "$dir" != "$IGNITION_ENV" ] || dir="."
	mkdir -p "$dir"
	tmp="${IGNITION_ENV}.tmp.$$"
	if [ -f "$IGNITION_ENV" ]; then
		awk -F= '$1 != "PLATFORM_ID" { print }' "$IGNITION_ENV" >"$tmp"
	else
		: >"$tmp"
	fi
	printf 'PLATFORM_ID=%s\n' "$platform" >>"$tmp"
	mv "$tmp" "$IGNITION_ENV"
}

valid_media_platform() {
	case "$1" in
		proxmoxve | kubevirt | openstack | metal | qemu) return 0 ;;
		*) return 1 ;;
	esac
}

valid_local_platform() {
	case "$1" in
		aws | gcp | azure) return 0 ;;
		*) return 1 ;;
	esac
}

strict_platform_line() {
	local file="$1"

	awk '/^platform_id=/ { print substr($0, 13); found=1 } END { exit found ? 0 : 1 }' "$file"
}

read_platform_from_grubenv() {
	local file="$1"
	local value

	[ -f "$file" ] || return 1

	if command -v grub2-editenv >/dev/null 2>&1 && [ -z "${ELEMENTAL_PLATFORM_RESOLVER_FORCE_PARSE:-}" ]; then
		if value="$(grub2-editenv "$file" list 2>/dev/null | awk -F= '$1 == "platform_id" && $2 ~ /^[A-Za-z0-9_.-]+$/ { print $2; found=1 } END { exit found ? 0 : 1 }')"; then
			printf '%s\n' "$value"
			return 0
		fi
	fi

	strict_platform_line "$file"
}

hint_roots_from_test_hook() {
	[ "${ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS+x}" = "x" ] || return 1
	[ -n "$ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS" ] || return 0

	local oldifs root
	oldifs="$IFS"
	IFS=:
	for root in $ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS; do
		[ -n "$root" ] && [ -d "$root" ] || continue
		if [ -f "$root/grubenv" ]; then
			printf '%s\n' "$root"
		else
			find "$root" -mindepth 1 -maxdepth 1 -type d
		fi
	done
	IFS="$oldifs"
	return 0
}

mount_hint_media() {
	mkdir -p "$MOUNT_BASE"

	wait_for_hint_media

	local index=0
	local found=0
	local dev target
	for dev in "$BY_LABEL_DIR"/PLATFORM_HINT "$BY_LABEL_DIR"/PLATFORM_HINT-*; do
		[ -e "$dev" ] || continue

		target="$MOUNT_BASE/$index"
		mkdir -p "$target"
		if mount -o ro "$dev" "$target" 2>/dev/null; then
			printf '%s\n' "$target"
			found=1
			index=$((index + 1))
		else
			log "could not mount platform hint media $dev"
		fi
	done

	[ "$found" -eq 1 ]
}

hint_media_present() {
	local dev
	for dev in "$BY_LABEL_DIR"/PLATFORM_HINT "$BY_LABEL_DIR"/PLATFORM_HINT-*; do
		[ -e "$dev" ] && return 0
	done
	return 1
}

wait_for_hint_media() {
	local remaining
	remaining="$HINT_WAIT_SECONDS"

	while ! hint_media_present; do
		[ "$remaining" -gt 0 ] || return 0
		sleep 1
		remaining=$((remaining - 1))
	done
}

cleanup_mounts() {
	[ -d "$MOUNT_BASE" ] || return 0

	local mountpoint
	for mountpoint in "$MOUNT_BASE"/*; do
		[ -d "$mountpoint" ] || continue
		umount "$mountpoint" >/dev/null 2>&1 || true
	done
}

resolve_from_hint_media() {
	local roots root value

	roots="$(hint_roots_from_test_hook || mount_hint_media || true)"
	[ -n "$roots" ] || return 1

	local values=""
	while IFS= read -r root; do
		[ -n "$root" ] || continue

		if value="$(read_platform_from_grubenv "$root/grubenv" 2>/dev/null)"; then
			if valid_media_platform "$value"; then
				values="${values}${value}"$'\n'
			else
				log "ignored invalid platform_id $root/grubenv: $value"
			fi
		else
			log "ignored unreadable or malformed platform hint $root/grubenv"
		fi
	done <<EOF
$roots
EOF

	local unique count
	unique="$(printf '%s' "$values" | awk 'NF { seen[$0]=1 } END { for (v in seen) print v }')"
	count="$(printf '%s\n' "$unique" | awk 'NF { n++ } END { print n + 0 }')"
	case "$count" in
		0) return 1 ;;
		1)
			printf '%s\n' "$unique" | awk 'NF { print; exit }'
			return 0
			;;
		*)
			log "ambiguous platform hint media: $(printf '%s' "$unique" | tr '\n' ' ')"
			return 1
			;;
	esac
}

dmi_value() {
	local name="$1"
	local path="$SYS_ROOT/class/dmi/id/$name"

	[ -r "$path" ] || return 1
	tr -d '\000' <"$path" | tr '\n' ' '
}

detect_virt() {
	if [ -n "${ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT:-}" ]; then
		printf '%s\n' "$ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT"
		return 0
	fi

	command -v systemd-detect-virt >/dev/null 2>&1 || return 1
	systemd-detect-virt --vm 2>/dev/null || return 1
}

dmi_contains() {
	local needle="$1"
	shift

	local file value
	for file in "$@"; do
		value="$(dmi_value "$file" 2>/dev/null || true)"
		case "$value" in
			*"$needle"*) return 0 ;;
		esac
	done
	return 1
}

azure_specific_dmi() {
	local asset

	asset="$(dmi_value chassis_asset_tag 2>/dev/null || true)"
	[ "$asset" = "7783-7084-3265-9085-8269-3286-77 " ] || [ "$asset" = "7783-7084-3265-9085-8269-3286-77" ]
}

resolve_from_local_detection() {
	local virt

	virt="$(detect_virt 2>/dev/null || true)"
	case "$virt" in
		amazon)
			printf 'aws\n'
			return 0
			;;
		google)
			printf 'gcp\n'
			return 0
			;;
		microsoft)
			if azure_specific_dmi; then
				printf 'azure\n'
				return 0
			fi
			log "microsoft detected without Azure-specific evidence"
			return 1
			;;
	esac

	if dmi_contains "Amazon EC2" product_name sys_vendor board_vendor; then
		printf 'aws\n'
		return 0
	fi
	if dmi_contains "Google Compute Engine" product_name sys_vendor board_vendor; then
		printf 'gcp\n'
		return 0
	fi
	if azure_specific_dmi; then
		printf 'azure\n'
		return 0
	fi

	return 1
}

main() {
	if [ ! -f "$IGNITION_ENV" ]; then
		log "ignition env not present; no platform resolution needed"
		return 0
	fi

	local explicit existing platform
	existing="$(env_value PLATFORM_ID 2>/dev/null || true)"
	if [ -n "$existing" ]; then
		explicit="$(cmdline_platform_id 2>/dev/null || true)"
		case "$existing" in
			qemu | metal) ;;
			*)
				log "platform already set: $existing"
				return 0
				;;
		esac
		if [ -n "$explicit" ]; then
			log "platform explicitly set to generic $existing; checking platform hint"
		else
			log "platform set by generic $existing detection; checking platform hint"
		fi
	fi

	trap cleanup_mounts EXIT

	platform="$(resolve_from_hint_media || true)"
	if [ -n "$platform" ]; then
		write_platform_id "$platform"
		log "resolved platform PLATFORM_HINT: $platform"
		return 0
	fi

	platform="$(resolve_from_local_detection || true)"
	if [ -n "$platform" ] && valid_local_platform "$platform"; then
		write_platform_id "$platform"
		log "resolved platform local detection: $platform"
		return 0
	fi

	log "no platform resolved"
	return 0
}

main "$@"
