package platformresolver_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runResolver(t *testing.T, root string, env map[string]string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("bash", "elemental-platform-resolver.sh")
	cmd.Dir = "."
	cmd.Env = append(os.Environ(),
		"ELEMENTAL_PLATFORM_RESOLVER_LOG_TO_STDERR=1",
		"ELEMENTAL_PLATFORM_RESOLVER_IGNITION_ENV="+filepath.Join(root, "run", "ignition.env"),
		"ELEMENTAL_PLATFORM_RESOLVER_KERNEL_CMDLINE="+filepath.Join(root, "proc", "cmdline"),
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS="+filepath.Join(root, "hints"),
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_WAIT_SECONDS=0",
		"ELEMENTAL_PLATFORM_RESOLVER_SYS_ROOT="+filepath.Join(root, "sys"),
	)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	out, err := cmd.CombinedOutput()
	data, readErr := os.ReadFile(filepath.Join(root, "run", "ignition.env"))
	if readErr != nil {
		return string(out), "", err
	}

	return string(out), string(data), err
}

func writeIgnitionEnv(t *testing.T, root string, content string) {
	t.Helper()

	path := filepath.Join(root, "run")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "ignition.env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHint(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, "hints", name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "grubenv"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCmdline(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, "proc")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "cmdline"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDMI(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, "sys", "class", "dmi", "id")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExistingPlatformIDWins(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\nPLATFORM_ID=openstack\n")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "amazon",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=openstack\n") {
		t.Fatalf("expected existing platform to remain, got:\n%s", env)
	}
	if strings.Contains(env, "PLATFORM_ID=proxmoxve\n") || strings.Contains(env, "PLATFORM_ID=aws\n") {
		t.Fatalf("expected existing platform to win, got:\n%s", env)
	}
}

func TestBootMediaPlatformHintSelectsProxmoxVE(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\n")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=proxmoxve\n") {
		t.Fatalf("expected proxmoxve, got:\n%s", env)
	}
}

func TestBootMediaOverridesGeneratedGenericQEMUPlatform(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\nPLATFORM_ID=qemu\n")
	writeCmdline(t, root, "quiet systemd.show_status=yes\n")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=proxmoxve\n") {
		t.Fatalf("expected proxmoxve, got:\n%s", env)
	}
	if !strings.Contains(out, "platform set by generic qemu detection") {
		t.Fatalf("expected generated qemu diagnostic, got:\n%s", out)
	}
}

func TestBootMediaOverridesExplicitGenericQEMUPlatform(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\nPLATFORM_ID=qemu\n")
	writeCmdline(t, root, "quiet ignition.platform.id=qemu\n")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=proxmoxve\n") {
		t.Fatalf("expected proxmoxve, got:\n%s", env)
	}
	if !strings.Contains(out, "platform explicitly set to generic qemu") {
		t.Fatalf("expected explicit generic qemu diagnostic, got:\n%s", out)
	}
}

func TestElementalPlatformLabelIsNotAlias(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeHint(t, root, "ELEMENTAL_PLATFORM", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS": "",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=") {
		t.Fatalf("expected no platform selection, got:\n%s", env)
	}
}

func TestInvalidBootMediaNoOps(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\n")
	writeHint(t, root, "media0", "platform_id=proxmoxve quiet splash\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=") {
		t.Fatalf("expected invalid platform no-op, got:\n%s", env)
	}
	if !strings.Contains(out, "ignored invalid platform_id") {
		t.Fatalf("expected invalid diagnostic, got:\n%s", out)
	}
}

func TestMaliciousBootMediaIsNotExecuted(t *testing.T) {
	root := t.TempDir()
	owned := filepath.Join(t.TempDir(), "elemental-platform-owned")
	writeIgnitionEnv(t, root, "")
	writeHint(t, root, "media0", "platform_id=$(touch "+owned+")\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=") {
		t.Fatalf("expected malicious value no-op, got:\n%s", env)
	}
	if _, statErr := os.Stat(owned); !os.IsNotExist(statErr) {
		t.Fatalf("resolver executed grubenv content; stat err=%v", statErr)
	}
}

func TestAmbiguousBootMediaNoOps(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")
	writeHint(t, root, "media1", "platform_id=openstack\n")

	out, env, err := runResolver(t, root, nil)
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=") {
		t.Fatalf("expected ambiguous media no-op, got:\n%s", env)
	}
	if !strings.Contains(out, "ambiguous platform hint media") {
		t.Fatalf("expected ambiguity diagnostic, got:\n%s", out)
	}
}

func TestBootMediaPrecedesLocalDetection(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeHint(t, root, "media0", "platform_id=proxmoxve\n")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "amazon",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=proxmoxve\n") {
		t.Fatalf("expected boot media to win over local detection, got:\n%s", env)
	}
}

func TestLocalAWSDetection(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "amazon",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=aws\n") {
		t.Fatalf("expected aws, got:\n%s", env)
	}
}

func TestGeneratedGenericMetalPlatformIsRefinedToAWS(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "IGNITION_ARGS=--log-to-stdout\nPLATFORM_ID=metal\n")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "amazon",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=aws\n") {
		t.Fatalf("expected generic metal platform to be refined to aws, got:\n%s", env)
	}
	if strings.Contains(env, "PLATFORM_ID=metal\n") {
		t.Fatalf("expected generic metal platform to be replaced, got:\n%s", env)
	}
}

func TestLocalAWSDetectionFromDMI(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeDMI(t, root, "product_name", "Amazon EC2")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS": "",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=aws\n") {
		t.Fatalf("expected aws, got:\n%s", env)
	}
}

func TestLocalGCPDetectionFromDetectVirt(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "google",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=gcp\n") {
		t.Fatalf("expected gcp, got:\n%s", env)
	}
}

func TestLocalGCPDetectionFromDMI(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeDMI(t, root, "product_name", "Google Compute Engine")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS": "",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=gcp\n") {
		t.Fatalf("expected gcp, got:\n%s", env)
	}
}

func TestGenericHyperVDoesNotSelectAzure(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeDMI(t, root, "sys_vendor", "Microsoft Corporation")
	writeDMI(t, root, "product_name", "Virtual Machine")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "microsoft",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=azure") {
		t.Fatalf("expected generic Hyper-V to no-op, got:\n%s", env)
	}
	if !strings.Contains(out, "microsoft detected without Azure-specific evidence") {
		t.Fatalf("expected strict Azure diagnostic, got:\n%s", out)
	}
}

func TestAzureSpecificDMISelectsAzure(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")
	writeDMI(t, root, "sys_vendor", "Microsoft Corporation")
	writeDMI(t, root, "product_name", "Virtual Machine")
	writeDMI(t, root, "chassis_asset_tag", "7783-7084-3265-9085-8269-3286-77")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "microsoft",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if !strings.Contains(env, "PLATFORM_ID=azure\n") {
		t.Fatalf("expected azure, got:\n%s", env)
	}
}

func TestGenericQEMUDoesNotSelectQEMU(t *testing.T) {
	root := t.TempDir()
	writeIgnitionEnv(t, root, "")

	out, env, err := runResolver(t, root, map[string]string{
		"ELEMENTAL_PLATFORM_RESOLVER_HINT_ROOTS":         "",
		"ELEMENTAL_PLATFORM_RESOLVER_DETECT_VIRT_RESULT": "qemu",
	})
	if err != nil {
		t.Fatalf("resolver failed: %v\n%s", err, out)
	}
	if strings.Contains(env, "PLATFORM_ID=qemu") {
		t.Fatalf("expected generic qemu detection to no-op, got:\n%s", env)
	}
}

func TestPatchIgnitionGeneratorPreservesResolvedPlatformOnRerun(t *testing.T) {
	root := t.TempDir()
	generator := filepath.Join(root, "ignition-generator")
	original := `#!/bin/bash
set -e
cmdline_arg() {
	echo ""
}
echo "PLATFORM_ID=$(cmdline_arg ignition.platform.id)" > /run/ignition.env
. /run/ignition.env
if [ -z "${PLATFORM_ID}" ]; then
	platform="metal"
	detectedvirt="qemu"
	case "${detectedvirt}" in
		*kvm*|*qemu*)
			platform="qemu"
			;;
	esac
	echo "PLATFORM_ID=${platform}" > /run/ignition.env
fi
`
	if err := os.WriteFile(generator, []byte(original), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "patch-ignition-generator.sh", generator)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("patch failed: %v\n%s", err, out)
	}

	patchedBytes, err := os.ReadFile(generator)
	if err != nil {
		t.Fatal(err)
	}
	patched := string(patchedBytes)
	if !strings.Contains(patched, `if [ -f /run/ignition.env ]; then`) {
		t.Fatalf("expected patched generator to source existing env, got:\n%s", patched)
	}
	if !strings.Contains(patched, `if [ -z "${PLATFORM_ID:-}" ]; then`) {
		t.Fatalf("expected patched generator to guard platform generation, got:\n%s", patched)
	}
	if !strings.Contains(patched, "elemental-platform-resolver preserve resolved PLATFORM_ID") {
		t.Fatalf("expected patch marker, got:\n%s", patched)
	}

	cmd = exec.Command("bash", "-n", generator)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("patched generator is not valid shell: %v\n%s", err, out)
	}

	cmd = exec.Command("bash", "patch-ignition-generator.sh", generator)
	cmd.Dir = "."
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("second patch failed: %v\n%s", err, out)
	}
	patchedAgain, err := os.ReadFile(generator)
	if err != nil {
		t.Fatal(err)
	}
	if string(patchedAgain) != patched {
		t.Fatalf("patch should be idempotent\nfirst:\n%s\nsecond:\n%s", patched, string(patchedAgain))
	}
}
