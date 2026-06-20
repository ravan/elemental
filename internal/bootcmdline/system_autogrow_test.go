package bootcmdline

import "testing"

func TestAppendSystemAutogrow(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		target  string
		want    string
	}{
		{name: "empty", cmdline: "", want: SystemAutogrowKernelFlag},
		{name: "preserves existing", cmdline: "console=ttyS0 quiet", want: "console=ttyS0 quiet " + SystemAutogrowKernelFlag},
		{name: "trims whitespace", cmdline: " console=ttyS0 ", want: "console=ttyS0 " + SystemAutogrowKernelFlag},
		{name: "deduplicates exact token", cmdline: "console=ttyS0 " + SystemAutogrowKernelFlag, want: "console=ttyS0 " + SystemAutogrowKernelFlag},
		{name: "does not treat prefix duplicate", cmdline: "xelemental.system_autogrow=1", want: "xelemental.system_autogrow=1 " + SystemAutogrowKernelFlag},
		{name: "appends target", cmdline: "console=ttyS0", target: "10G", want: "console=ttyS0 " + SystemAutogrowKernelFlag + " " + SystemAutogrowTargetKernelArg + "10G"},
		{name: "replaces target", cmdline: "console=ttyS0 " + SystemAutogrowKernelFlag + " " + SystemAutogrowTargetKernelArg + "8G", target: "10G", want: "console=ttyS0 " + SystemAutogrowKernelFlag + " " + SystemAutogrowTargetKernelArg + "10G"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppendSystemAutogrow(tt.cmdline, tt.target); got != tt.want {
				t.Fatalf("AppendSystemAutogrow(%q) = %q, want %q", tt.cmdline, got, tt.want)
			}
		})
	}
}

func TestHasSystemAutogrow(t *testing.T) {
	if !HasSystemAutogrow("console=ttyS0 " + SystemAutogrowKernelFlag) {
		t.Fatal("expected exact autogrow token to be detected")
	}
	if HasSystemAutogrow("console=ttyS0 xelemental.system_autogrow=1") {
		t.Fatal("prefix match must not count as autogrow token")
	}
}
