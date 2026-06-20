#!/usr/bin/env bash

check() {
	return 0
}

depends() {
	echo ignition
	return 0
}

install() {
	inst_multiple -o \
		bash \
		sgdisk \
		blockdev \
		udevadm \
		findmnt \
		blkid \
		btrfs \
		mount \
		umount \
		lsblk \
		awk \
		readlink \
		mkdir \
		rmdir \
		tr \
		sleep

	inst_simple "$moddir/elemental-system-autogrow.sh" "/usr/lib/elemental/elemental-system-autogrow.sh"
	inst_simple "$moddir/elemental-system-autogrow.service" "$systemdsystemunitdir/elemental-system-autogrow.service"
	mkdir -p "$initdir/$systemdsystemunitdir/initrd.target.wants"
	ln_r "$systemdsystemunitdir/elemental-system-autogrow.service" "$systemdsystemunitdir/initrd.target.wants/elemental-system-autogrow.service"
}
