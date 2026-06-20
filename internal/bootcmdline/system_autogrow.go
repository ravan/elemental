package bootcmdline

import "strings"

const SystemAutogrowKernelFlag = "elemental.system_autogrow=1"
const SystemAutogrowTargetKernelArg = "elemental.system_autogrow_target="

func AppendSystemAutogrow(cmdline string, target ...string) string {
	fields := strings.Fields(cmdline)
	wantTarget := ""
	if len(target) > 0 {
		wantTarget = strings.TrimSpace(target[0])
	}

	filtered := fields[:0]
	hasFlag := false
	for _, field := range fields {
		switch {
		case field == SystemAutogrowKernelFlag:
			hasFlag = true
			filtered = append(filtered, field)
		case strings.HasPrefix(field, SystemAutogrowTargetKernelArg):
			continue
		default:
			filtered = append(filtered, field)
		}
	}
	fields = filtered

	if !hasFlag {
		fields = append(fields, SystemAutogrowKernelFlag)
	}
	if wantTarget != "" {
		fields = append(fields, SystemAutogrowTargetKernelArg+wantTarget)
	}

	return strings.Join(fields, " ")
}

func HasSystemAutogrow(cmdline string) bool {
	for _, field := range strings.Fields(cmdline) {
		if field == SystemAutogrowKernelFlag {
			return true
		}
	}
	return false
}
