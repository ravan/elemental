package install

import (
	"strings"
	"testing"

	"github.com/suse/elemental/v3/pkg/deployment"
)

func TestApplySystemAutogrowTarget(t *testing.T) {
	dep := deployment.DefaultDeployment()
	dep.Disks[0].Partitions = []*deployment.Partition{
		{Role: deployment.EFI, Size: 1024},
		{Role: deployment.Recovery, Size: 1152},
		{Role: deployment.Config, Size: 256},
		{Role: deployment.System},
	}
	dep.BootConfig.KernelCmdline = "console=ttyS0 elemental.system_autogrow=1 elemental.system_autogrow_target=10G"

	if err := applySystemAutogrowTarget(dep); err != nil {
		t.Fatalf("applySystemAutogrowTarget() error = %v", err)
	}

	if got, want := dep.Disks[0].Partitions[3].Size, deployment.MiB(7808); got != want {
		t.Fatalf("SYSTEM size = %dMiB, want %dMiB", got, want)
	}
}

func TestApplySystemAutogrowTargetUsesInstallerCmdline(t *testing.T) {
	dep := deployment.DefaultDeployment()
	dep.Disks[0].Partitions = []*deployment.Partition{
		{Role: deployment.EFI, Size: 1024},
		{Role: deployment.System},
	}
	dep.Installer.KernelCmdline = "elemental.system_autogrow_target=8G"

	if err := applySystemAutogrowTarget(dep); err != nil {
		t.Fatalf("applySystemAutogrowTarget() error = %v", err)
	}

	if got, want := dep.Disks[0].Partitions[1].Size, deployment.MiB(7168); got != want {
		t.Fatalf("SYSTEM size = %dMiB, want %dMiB", got, want)
	}
}

func TestApplySystemAutogrowTargetRejectsTooSmallTarget(t *testing.T) {
	dep := deployment.DefaultDeployment()
	dep.Disks[0].Partitions = []*deployment.Partition{
		{Role: deployment.EFI, Size: 1024},
		{Role: deployment.Recovery, Size: 1152},
		{Role: deployment.System},
	}
	dep.BootConfig.KernelCmdline = "elemental.system_autogrow_target=1G"

	err := applySystemAutogrowTarget(dep)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "smaller than preceding partitions") {
		t.Fatalf("unexpected error: %v", err)
	}
}
