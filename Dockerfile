ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM registry.opensuse.org/opensuse/bci/golang:${GO_VERSION} AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /work

# Add specific dirs to the image so cache is not invalidated when modifying non go files
ADD go.mod .
ADD go.sum .
RUN go mod download
ADD cmd cmd
ADD internal internal
ADD pkg pkg
ADD Makefile .
ADD .git .
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH make all

FROM registry.opensuse.org/opensuse/bci/golang:${GO_VERSION} AS compat-builder

RUN printf '%s\n' \
        '#define _GNU_SOURCE' \
        '#include <dlfcn.h>' \
        '#include <errno.h>' \
        '#include <fcntl.h>' \
        '#include <stdio.h>' \
        '#include <sys/syscall.h>' \
        '#include <unistd.h>' \
        '#ifndef CLOSE_RANGE_CLOEXEC' \
        '#define CLOSE_RANGE_CLOEXEC (1U << 2)' \
        '#endif' \
        'typedef int (*faccessat_func_t)(int, const char *, int, int);' \
        'int faccessat(int dirfd, const char *pathname, int mode, int flags) {' \
        '    static faccessat_func_t real_faccessat;' \
        '    if (!real_faccessat)' \
        '        real_faccessat = (faccessat_func_t) dlsym(RTLD_NEXT, "faccessat");' \
        '    if (pathname && pathname[0] == 0 && (flags & AT_EMPTY_PATH)) {' \
        '        char path[64];' \
        '        snprintf(path, sizeof(path), "/proc/self/fd/%d", dirfd);' \
        '        return access(path, mode);' \
        '    }' \
        '    return real_faccessat(dirfd, pathname, mode, flags);' \
        '}' \
        'int close_range(unsigned int first, unsigned int last, int flags) {' \
        '    long max_fd = sysconf(_SC_OPEN_MAX);' \
        '    if (max_fd < 0)' \
        '        max_fd = 1024;' \
        '    if (last == ~0U || last >= (unsigned int) max_fd)' \
        '        last = (unsigned int) max_fd - 1;' \
        '    if (first > last)' \
        '        return 0;' \
        '    for (unsigned int fd = first; fd <= last; fd++) {' \
        '        if (flags & CLOSE_RANGE_CLOEXEC)' \
        '            (void) fcntl((int) fd, F_SETFD, FD_CLOEXEC);' \
        '        else' \
        '            (void) close((int) fd);' \
        '    }' \
        '    return 0;' \
        '}' \
        > /tmp/faccessat-empty-path.c && \
    gcc -shared -fPIC -O2 -o /tmp/faccessat-empty-path.so /tmp/faccessat-empty-path.c -ldl

FROM registry.opensuse.org/opensuse/tumbleweed:latest AS runner-base

ARG TARGETARCH
RUN ARCH=$(uname -m); \
    [[ "${ARCH}" == "aarch64" ]] && ARCH="arm64"; \
    zypper --non-interactive removerepo repo-update || true; \
    zypper --non-interactive install --no-recommends xfsprogs \
        util-linux-systemd \
        e2fsprogs \
        udev \
        rsync \
        grub2 \
        dosfstools \
        grub2-${ARCH}-efi \
        mtools \
        gptfdisk \
        patterns-microos-selinux \
        btrfsprogs \
        btrfsmaintenance \
        snapper \
        lvm2 && \
    zypper clean --all && \
    for tool in mkfs.vfat mkfs.fat mkfs.ext2 mkfs.ext3 mkfs.ext4 mkfs.xfs mkfs.btrfs mkswap; do \
        if command -v "$tool" >/dev/null 2>&1; then \
            src="$(readlink -f "$(command -v "$tool")")"; \
            for dir in /usr/bin /usr/sbin; do \
                dst="$dir/$tool"; \
                if [ -L "$dst" ] || [ "$(readlink -f "$dst" 2>/dev/null || true)" != "$src" ]; then \
                    ln -f "$src" "$dst"; \
                fi; \
            done; \
        fi; \
    done
COPY --from=compat-builder /tmp/faccessat-empty-path.so /usr/lib64/elemental-faccessat-empty-path.so
RUN mv /usr/bin/systemd-repart /usr/bin/systemd-repart.bin && \
    printf '%s\n' \
        '#!/bin/bash' \
        'set -u' \
        'export LD_PRELOAD=/usr/lib64/elemental-faccessat-empty-path.so${LD_PRELOAD:+:$LD_PRELOAD}' \
        'target="${@: -1}"' \
        'if [[ "$target" == /config/* && ! -b "$target" ]]; then' \
        '    status=1' \
        '    for attempt in 1 2 3 4 5; do' \
        '        for idx in 0 1 2 3 4 5 6 7; do' \
        '            [[ -e "/dev/loop${idx}" ]] || mknod "/dev/loop${idx}" b 7 "$idx" 2>/dev/null || true' \
        '        done' \
        '        losetup -D 2>/dev/null || true' \
        '        tmp="$(mktemp -p /tmp "systemd-repart.XXXXXX.raw")"' \
        '        rm -f "$tmp"' \
        '        args=("$@")' \
        '        args[$((${#args[@]} - 1))]="$tmp"' \
        '        /usr/bin/systemd-repart.bin "${args[@]}"' \
        '        status="$?"' \
        '        if [[ "$status" -eq 0 ]]; then' \
        '            cp --sparse=always "$tmp" "$target"' \
        '            status="$?"' \
        '            rm -f "$tmp"' \
        '            exit "$status"' \
        '        fi' \
        '        rm -f "$tmp"' \
        '        sleep 1' \
        '    done' \
        '    exit "$status"' \
        'fi' \
        'exec /usr/bin/systemd-repart.bin "$@"' \
        > /usr/bin/systemd-repart && \
    chmod 0755 /usr/bin/systemd-repart

COPY --from=builder /work/build/elemental3ctl /usr/bin/elemental3ctl
COPY --from=builder /work/build/elemental3 /usr/bin/elemental3

FROM runner-base AS runner-elemental3ctl
ENTRYPOINT ["/usr/bin/elemental3ctl"]

FROM runner-base AS runner-elemental3

RUN zypper --non-interactive removerepo repo-update || true; \
    zypper --non-interactive install --no-recommends xorriso && \
    zypper clean --all

ENTRYPOINT ["/usr/bin/elemental3"]
