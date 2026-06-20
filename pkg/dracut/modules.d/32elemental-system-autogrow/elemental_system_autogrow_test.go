package systemautogrow_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type autogrowFixture struct {
	root string
	bin  string
	log  string
}

func newFixture(t *testing.T) autogrowFixture {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{
		bin,
		filepath.Join(root, "dev", "disk", "by-label"),
		filepath.Join(root, "run"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return autogrowFixture{root: root, bin: bin, log: filepath.Join(root, "calls.log")}
}

func (f autogrowFixture) writeCmd(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.Join(f.bin, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f autogrowFixture) run(t *testing.T, cmdline string, extraEnv ...string) (string, error) {
	t.Helper()
	cmdlinePath := filepath.Join(f.root, "proc-cmdline")
	if err := os.WriteFile(cmdlinePath, []byte(cmdline), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "elemental-system-autogrow.sh")
	cmd.Dir = "."
	cmd.Env = append(os.Environ(),
		"PATH="+f.bin+":"+os.Getenv("PATH"),
		"ELEMENTAL_SYSTEM_AUTOGROW_LOG_TO_STDERR=1",
		"ELEMENTAL_SYSTEM_AUTOGROW_CMDLINE="+cmdlinePath,
		"ELEMENTAL_SYSTEM_AUTOGROW_DEV_ROOT="+filepath.Join(f.root, "dev"),
		"ELEMENTAL_SYSTEM_AUTOGROW_RUN_ROOT="+filepath.Join(f.root, "run"),
		"ELEMENTAL_SYSTEM_AUTOGROW_CALL_LOG="+f.log,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f autogrowFixture) calls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func installHappyPathCommands(t *testing.T, f autogrowFixture, systemPart, parentDisk, partNum string) {
	t.Helper()
	systemBase := filepath.Base(systemPart)
	parentBase := filepath.Base(parentDisk)
	for _, dev := range []string{systemPart, parentDisk} {
		if err := os.MkdirAll(filepath.Dir(dev), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dev, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	systemLink := filepath.Join(f.root, "dev", "disk", "by-label", "SYSTEM")
	if err := os.Symlink(systemPart, systemLink); err != nil {
		t.Fatal(err)
	}

	f.writeCmd(t, "readlink", `if [ "$1" = "-f" ]; then echo "$ELEMENTAL_TEST_SYSTEM_PART"; exit 0; fi
exit 1
`)
	f.writeCmd(t, "blkid", `ready() {
	want="${ELEMENTAL_TEST_SYSTEM_PART_READY_AFTER:-0}"
	[ "$want" = "0" ] && return 0
	got="$(cat "$ELEMENTAL_SYSTEM_AUTOGROW_RUN_ROOT/wait-count" 2>/dev/null || echo 0)"
	[ "$got" -ge "$want" ]
}
if [ "$1 $2 $3" = "-o value -s" ] && [ "$4" = "TYPE" ]; then echo "${ELEMENTAL_TEST_FS_TYPE:-btrfs}"; exit 0; fi
if [ "$1 $2 $3" = "-o value -s" ] && [ "$4" = "LABEL" ]; then ready && [ "$5" = "${ELEMENTAL_TEST_SYSTEM_PART:-}" ] && echo SYSTEM && exit 0; exit 1; fi
if [ "$1 $2" = "-L SYSTEM" ]; then ready || exit 1; [ "${ELEMENTAL_TEST_BLKID_L_MISSING:-0}" = "1" ] && exit 1; [ -n "${ELEMENTAL_TEST_SYSTEM_PART:-}" ] && echo "$ELEMENTAL_TEST_SYSTEM_PART" && exit 0; exit 1; fi
if [ "$1 $2 $3 $4" = "-o device -t LABEL=SYSTEM" ]; then ready || exit 1; [ "${ELEMENTAL_TEST_BLKID_DEVICE_MISSING:-0}" = "1" ] && exit 1; [ -n "${ELEMENTAL_TEST_SYSTEM_PART:-}" ] && echo "$ELEMENTAL_TEST_SYSTEM_PART" && exit 0; exit 1; fi
exit 1
`)
	f.writeCmd(t, "lsblk", `case "$*" in
"-ndo PKNAME "* ) [ "${ELEMENTAL_TEST_PARENT_MISSING:-0}" = "1" ] && exit 0; echo "`+parentBase+`" ;;
"-ndo PARTN "* ) [ "${ELEMENTAL_TEST_PARTNUM_MISSING:-0}" = "1" ] && exit 0; echo "`+partNum+`" ;;
"-ndo START "* ) echo "2048" ;;
"-bndo SIZE "*`+systemBase+`* ) echo "${ELEMENTAL_TEST_SYSTEM_SIZE_BYTES:-8589934592}" ;;
"-nrpo NAME,LABEL" ) want="${ELEMENTAL_TEST_SYSTEM_PART_READY_AFTER:-0}"; got="$(cat "$ELEMENTAL_SYSTEM_AUTOGROW_RUN_ROOT/wait-count" 2>/dev/null || echo 0)"; [ "$want" = "0" ] || [ "$got" -ge "$want" ] || exit 0; [ -n "${ELEMENTAL_TEST_SYSTEM_PART:-}" ] && printf "%s SYSTEM\n" "$ELEMENTAL_TEST_SYSTEM_PART" ;;
"-nrpo NAME,TYPE,START "* ) printf "%s part 2048\n" "$ELEMENTAL_TEST_SYSTEM_PART"; [ "${ELEMENTAL_TEST_HAS_LATER_PART:-0}" = "1" ] && printf "%s part 20000000\n" "$ELEMENTAL_SYSTEM_AUTOGROW_DEV_ROOT/later"; ;;
* ) echo "unexpected lsblk $*" >&2; exit 1 ;;
esac
`)
	f.writeCmd(t, "blockdev", `case "$*" in
  --getss\ * ) echo "${ELEMENTAL_TEST_SECTOR_SIZE:-512}" ;;
  --getsize64\ * ) echo "${ELEMENTAL_TEST_DISK_SIZE_BYTES:-19327352832}" ;;
  --rereadpt\ * ) printf "blockdev %s\n" "$*" >> "$ELEMENTAL_SYSTEM_AUTOGROW_CALL_LOG"; if [ "${ELEMENTAL_TEST_FAIL:-}" = "blockdev" ]; then exit 1; fi ;;
  * ) exit 1 ;;
esac
`)
	f.writeCmd(t, "sgdisk", `printf "sgdisk %s\n" "$*" >> "$ELEMENTAL_SYSTEM_AUTOGROW_CALL_LOG"
if [ "${ELEMENTAL_TEST_FAIL:-}" = "sgdisk" ]; then exit 1; fi
if [ "$1" = "-i" ]; then
  echo "Partition GUID code: 0FC63DAF-8483-4772-8E79-3D69D8477DE4 (Linux filesystem)"
  echo "Partition unique GUID: 11111111-2222-3333-4444-555555555555"
fi
exit 0
`)
	for _, name := range []string{"udevadm", "mount", "btrfs", "umount"} {
		failName := name
		if name == "btrfs" {
			failName = "btrfs"
		}
		f.writeCmd(t, name, `printf "`+name+` %s\n" "$*" >> "$ELEMENTAL_SYSTEM_AUTOGROW_CALL_LOG"
if [ "${ELEMENTAL_TEST_FAIL:-}" = "`+failName+`" ]; then exit 1; fi
exit 0
`)
	}
	f.writeCmd(t, "findmnt", `exit 0
`)
	f.writeCmd(t, "sleep", `count_file="$ELEMENTAL_SYSTEM_AUTOGROW_RUN_ROOT/wait-count"
count="$(cat "$count_file" 2>/dev/null || echo 0)"
printf "%s\n" "$((count + 1))" > "$count_file"
printf "sleep %s\n" "$*" >> "$ELEMENTAL_SYSTEM_AUTOGROW_CALL_LOG"
exit 0
`)
	t.Setenv("ELEMENTAL_TEST_SYSTEM_PART", systemPart)
}

func TestDisabledWithoutKernelFlag(t *testing.T) {
	f := newFixture(t)
	out, err := f.run(t, "console=ttyS0 quiet")
	if err != nil {
		t.Fatalf("helper failed while disabled: %v\n%s", err, out)
	}
	if !strings.Contains(out, "kernel flag not present") {
		t.Fatalf("expected disabled diagnostic, got:\n%s", out)
	}
	if got := f.calls(t); got != "" {
		t.Fatalf("disabled helper should not call tools, got:\n%s", got)
	}
}

func TestStructuralFailures(t *testing.T) {
	tests := []struct {
		name    string
		env     []string
		wantErr string
	}{
		{name: "missing system label", env: []string{"ELEMENTAL_TEST_SYSTEM_PART="}, wantErr: "SYSTEM filesystem label not found"},
		{name: "parent disk missing", env: []string{"ELEMENTAL_TEST_PARENT_MISSING=1"}, wantErr: "could not derive parent disk"},
		{name: "partition number missing", env: []string{"ELEMENTAL_TEST_PARTNUM_MISSING=1"}, wantErr: "could not derive partition number"},
		{name: "not btrfs", env: []string{"ELEMENTAL_TEST_FS_TYPE=xfs"}, wantErr: "SYSTEM filesystem must be btrfs"},
		{name: "not last partition", env: []string{"ELEMENTAL_TEST_HAS_LATER_PART=1"}, wantErr: "SYSTEM partition must be the last partition"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			systemPart := filepath.Join(f.root, "dev", "sda4")
			installHappyPathCommands(t, f, systemPart, filepath.Join(f.root, "dev", "sda"), "4")
			env := append([]string{"ELEMENTAL_TEST_SYSTEM_PART=" + systemPart}, tt.env...)
			out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", env...)
			if err == nil {
				t.Fatalf("expected failure, got success:\n%s", out)
			}
			if !strings.Contains(out, tt.wantErr) {
				t.Fatalf("expected %q, got:\n%s", tt.wantErr, out)
			}
		})
	}
}

func TestRequiredCommandMissing(t *testing.T) {
	f := newFixture(t)
	systemPart := filepath.Join(f.root, "dev", "sda4")
	installHappyPathCommands(t, f, systemPart, filepath.Join(f.root, "dev", "sda"), "4")
	if err := os.Remove(filepath.Join(f.bin, "sgdisk")); err != nil {
		t.Fatal(err)
	}
	out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", "ELEMENTAL_TEST_SYSTEM_PART="+systemPart)
	if err == nil {
		t.Fatalf("expected failure, got success:\n%s", out)
	}
	if !strings.Contains(out, "required command missing: sgdisk") {
		t.Fatalf("expected missing command diagnostic, got:\n%s", out)
	}
}

func TestNoExtraCapacityNoops(t *testing.T) {
	tests := []struct {
		name string
		env  []string
	}{
		{name: "disk capacity equals current system end", env: []string{"ELEMENTAL_TEST_DISK_SIZE_BYTES=8589934592"}},
		{name: "system reaches last usable gpt sector", env: []string{"ELEMENTAL_TEST_SYSTEM_SIZE_BYTES=19326287360"}},
		{name: "repeated execution after growth", env: []string{"ELEMENTAL_TEST_SYSTEM_SIZE_BYTES=19326287360"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			systemPart := filepath.Join(f.root, "dev", "vda4")
			installHappyPathCommands(t, f, systemPart, filepath.Join(f.root, "dev", "vda"), "4")
			env := append([]string{"ELEMENTAL_TEST_SYSTEM_PART=" + systemPart}, tt.env...)
			out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", env...)
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "already consumes target disk capacity") {
				t.Fatalf("expected filesystem-only resize diagnostic, got:\n%s", out)
			}
			if strings.Contains(f.calls(t), "sgdisk -d") {
				t.Fatalf("no-op should not rewrite partition, got:\n%s", f.calls(t))
			}
			if !strings.Contains(f.calls(t), "btrfs filesystem resize") {
				t.Fatalf("no-op should still resize filesystem, got:\n%s", f.calls(t))
			}
		})
	}
}

func TestHappyPaths(t *testing.T) {
	tests := []struct {
		systemPart string
		parentDisk string
		partNum    string
	}{
		{systemPart: "sda4", parentDisk: "sda", partNum: "4"},
		{systemPart: "vda4", parentDisk: "vda", partNum: "4"},
		{systemPart: "nvme0n1p4", parentDisk: "nvme0n1", partNum: "4"},
	}
	for _, tt := range tests {
		t.Run(tt.systemPart, func(t *testing.T) {
			f := newFixture(t)
			systemPart := filepath.Join(f.root, "dev", tt.systemPart)
			parentDisk := filepath.Join(f.root, "dev", tt.parentDisk)
			installHappyPathCommands(t, f, systemPart, parentDisk, tt.partNum)

			out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", "ELEMENTAL_TEST_SYSTEM_PART="+systemPart)
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, out)
			}
			calls := f.calls(t)
			for _, want := range []string{
				"sgdisk -i " + tt.partNum + " " + parentDisk,
				"sgdisk -e " + parentDisk,
				"sgdisk -d " + tt.partNum + " -n " + tt.partNum + ":2048:37748702 -c " + tt.partNum + ":SYSTEM -t " + tt.partNum + ":0FC63DAF-8483-4772-8E79-3D69D8477DE4 -u " + tt.partNum + ":11111111-2222-3333-4444-555555555555 " + parentDisk,
				"blockdev --rereadpt " + parentDisk,
				"udevadm settle",
				"mount " + systemPart + " ",
				"btrfs filesystem resize max ",
				"umount ",
			} {
				if !strings.Contains(calls, want) {
					t.Fatalf("expected call %q, got:\n%s", want, calls)
				}
			}
		})
	}
}

func TestSystemLabelDiscoveryFallbacks(t *testing.T) {
	tests := []struct {
		name string
		env  []string
	}{
		{
			name: "blkid device query",
			env:  []string{"ELEMENTAL_TEST_BLKID_L_MISSING=1"},
		},
		{
			name: "lsblk label output",
			env: []string{
				"ELEMENTAL_TEST_BLKID_L_MISSING=1",
				"ELEMENTAL_TEST_BLKID_DEVICE_MISSING=1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			systemPart := filepath.Join(f.root, "dev", "sda4")
			parentDisk := filepath.Join(f.root, "dev", "sda")
			installHappyPathCommands(t, f, systemPart, parentDisk, "4")
			if err := os.Remove(filepath.Join(f.root, "dev", "disk", "by-label", "SYSTEM")); err != nil {
				t.Fatal(err)
			}

			env := append([]string{"ELEMENTAL_TEST_SYSTEM_PART=" + systemPart}, tt.env...)
			out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", env...)
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, out)
			}

			calls := f.calls(t)
			if !strings.Contains(calls, "mount "+systemPart+" ") {
				t.Fatalf("expected discovered SYSTEM partition to be mounted, got:\n%s", calls)
			}
		})
	}
}

func TestSystemLabelDiscoveryWaitsForUdev(t *testing.T) {
	f := newFixture(t)
	systemPart := filepath.Join(f.root, "dev", "sda4")
	parentDisk := filepath.Join(f.root, "dev", "sda")
	installHappyPathCommands(t, f, systemPart, parentDisk, "4")
	if err := os.Remove(filepath.Join(f.root, "dev", "disk", "by-label", "SYSTEM")); err != nil {
		t.Fatal(err)
	}

	out, err := f.run(
		t,
		"console=ttyS0 elemental.system_autogrow=1",
		"ELEMENTAL_TEST_SYSTEM_PART="+systemPart,
		"ELEMENTAL_TEST_SYSTEM_PART_READY_AFTER=2",
	)
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	calls := f.calls(t)
	if !strings.Contains(calls, "sleep 1") {
		t.Fatalf("expected helper to wait for SYSTEM label, got:\n%s", calls)
	}
	if !strings.Contains(calls, "mount "+systemPart+" ") {
		t.Fatalf("expected discovered SYSTEM partition to be mounted, got:\n%s", calls)
	}
}

func TestTargetSizeCapsGrowth(t *testing.T) {
	f := newFixture(t)
	systemPart := filepath.Join(f.root, "dev", "sda4")
	parentDisk := filepath.Join(f.root, "dev", "sda")
	installHappyPathCommands(t, f, systemPart, parentDisk, "4")

	out, err := f.run(t,
		"console=ttyS0 elemental.system_autogrow=1 elemental.system_autogrow_target=10G",
		"ELEMENTAL_TEST_SYSTEM_PART="+systemPart,
	)
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}

	want := "sgdisk -d 4 -n 4:2048:20971519 -c 4:SYSTEM -t 4:0FC63DAF-8483-4772-8E79-3D69D8477DE4 -u 4:11111111-2222-3333-4444-555555555555 " + parentDisk
	if calls := f.calls(t); !strings.Contains(calls, want) {
		t.Fatalf("expected call %q, got:\n%s", want, calls)
	}
}

func TestFailurePropagation(t *testing.T) {
	tests := []struct {
		fail    string
		wantErr string
	}{
		{fail: "sgdisk", wantErr: "command failed: sgdisk -i"},
		{fail: "blockdev", wantErr: "command failed: blockdev --rereadpt"},
		{fail: "mount", wantErr: "command failed: mount"},
		{fail: "btrfs", wantErr: "command failed: btrfs filesystem resize max"},
		{fail: "umount", wantErr: "command failed: umount"},
	}
	for _, tt := range tests {
		t.Run(tt.fail, func(t *testing.T) {
			f := newFixture(t)
			systemPart := filepath.Join(f.root, "dev", "sda4")
			installHappyPathCommands(t, f, systemPart, filepath.Join(f.root, "dev", "sda"), "4")
			out, err := f.run(t, "console=ttyS0 elemental.system_autogrow=1", "ELEMENTAL_TEST_SYSTEM_PART="+systemPart, "ELEMENTAL_TEST_FAIL="+tt.fail)
			if err == nil {
				t.Fatalf("expected failure, got success:\n%s", out)
			}
			if !strings.Contains(out, tt.wantErr) {
				t.Fatalf("expected %q, got:\n%s", tt.wantErr, out)
			}
		})
	}
}
