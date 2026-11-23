package workflow

import (
	"slices"
	"strings"
)

// goTestCommand tách lệnh `go test` thuần thành cờ và mẫu gói. ok=false (bảo thủ) khi lệnh không phải go test thuần: có
// ký tự shell đặc biệt, cờ có giá trị rời (-run X), hay mẫu gói không bắt đầu bằng "./" hoặc "." (kể cả {test_dirs}).
func goTestCommand(cmd string) (flags, pats []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "&|;<>$`'\"()\\{}*?[]\n") {
		return nil, nil, false
	}
	f := strings.Fields(cmd)
	if len(f) < 3 || f[0] != "go" || f[1] != "test" {
		return nil, nil, false
	}
	for _, a := range f[2:] {
		switch {
		case strings.HasPrefix(a, "-"):
			switch a {
			case "-race", "-v", "-short", "-failfast", "-cover":
			default:
				if !strings.Contains(a, "=") || strings.HasPrefix(a, "-run=") || strings.HasPrefix(a, "-skip=") ||
					strings.HasPrefix(a, "-bench=") || strings.HasPrefix(a, "-fuzz=") || strings.HasPrefix(a, "-tags=") ||
					strings.HasPrefix(a, "-exec=") {
					return nil, nil, false
				}
			}
			flags = append(flags, a)
		case a == "." || strings.HasPrefix(a, "./"):
			pats = append(pats, a)
		default:
			return nil, nil, false
		}
	}
	if len(pats) == 0 {
		return nil, nil, false
	}
	slices.Sort(flags)
	return flags, pats, true
}
