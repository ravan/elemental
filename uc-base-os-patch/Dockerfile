ARG ELEMENTAL_IMAGE=elemental-image:latest
ARG SOURCE_IMAGE=registry.suse.com/elemental/base-os-kernel-default:16.0

FROM ${ELEMENTAL_IMAGE} AS elemental

FROM ${SOURCE_IMAGE}

COPY --from=elemental /usr/bin/elemental3ctl /usr/bin/elemental3ctl
COPY pkg/dracut/modules.d/29elemental-platform-resolver /usr/lib/dracut/modules.d/31elemental-platform-resolver
COPY pkg/dracut/modules.d/30elemental-ignition-merge /usr/lib/dracut/modules.d/30elemental-ignition-merge
COPY pkg/dracut/modules.d/32elemental-system-autogrow /usr/lib/dracut/modules.d/32elemental-system-autogrow

RUN chmod 0755 \
	/usr/lib/dracut/modules.d/31elemental-platform-resolver/module-setup.sh \
	/usr/lib/dracut/modules.d/31elemental-platform-resolver/elemental-platform-resolver.sh \
	/usr/lib/dracut/modules.d/31elemental-platform-resolver/patch-ignition-generator.sh \
	/usr/lib/dracut/modules.d/30elemental-ignition-merge/module-setup.sh \
	/usr/lib/dracut/modules.d/30elemental-ignition-merge/elemental-ignition-merge.sh \
	/usr/lib/dracut/modules.d/32elemental-system-autogrow/module-setup.sh \
	/usr/lib/dracut/modules.d/32elemental-system-autogrow/elemental-system-autogrow.sh && \
	command -v dracut >/dev/null 2>&1 && \
	command -v lsinitrd >/dev/null 2>&1 && \
	dracut -f --regenerate-all --add-drivers "btrfs" && \
    for module_dir in /usr/lib/modules/*; do \
        [ -d "${module_dir}" ] || continue; \
        kver="$(basename "${module_dir}")"; \
        if [ -f "/boot/initrd-${kver}" ]; then \
          cp "/boot/initrd-${kver}" "${module_dir}/initrd"; \
        fi; \
        [ -f "${module_dir}/initrd" ] || { echo "missing initrd for ${kver}" >&2; exit 1; }; \
        lsinitrd "${module_dir}/initrd" | grep -F "elemental-platform-resolver.service" >/dev/null || { echo "missing elemental-platform-resolver.service in ${module_dir}/initrd" >&2; exit 1; }; \
        lsinitrd -f etc/systemd/system-generators/ignition-generator "${module_dir}/initrd" | grep -F "elemental-platform-resolver preserve resolved PLATFORM_ID" >/dev/null || { echo "ignition-generator platform preservation patch missing in ${module_dir}/initrd" >&2; exit 1; }; \
        lsinitrd "${module_dir}/initrd" | grep -F "elemental-ignition-merge.service" >/dev/null || { echo "missing elemental-ignition-merge.service in ${module_dir}/initrd" >&2; exit 1; }; \
        lsinitrd "${module_dir}/initrd" | grep -F "kernel/fs/btrfs/btrfs.ko.zst" >/dev/null || { echo "missing btrfs driver in ${module_dir}/initrd" >&2; exit 1; }; \
    done
