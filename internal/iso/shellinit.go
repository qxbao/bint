package iso

import (
	"fmt"
	"regexp"
	"strings"
)

// Shells là các shell mà ShellInit hỗ trợ.
var Shells = []string{"bash", "zsh", "fish", "pwsh"}

var shellFuncRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ShellInit trả đoạn mã khai báo một hàm `<tool>-<name>` cho mỗi profile, chạy `bint iso run <tool> --name
// <name> -- "$@"` (docs/iso-plan.md D8: người dùng tự `eval "$(bint iso shell-init zsh)"`, bint không sửa RC).
// Tên có ký tự mà shell không chấp nhận làm tên hàm (vd dấu chấm) được bỏ qua kèm dòng chú thích.
func ShellInit(shell string, profiles []Profile) (string, error) {
	var tmpl string
	switch shell {
	case "bash", "zsh":
		tmpl = "%[1]s() { command bint iso run %[2]s --name %[3]s -- \"$@\"; }\n"
	case "fish":
		tmpl = "function %[1]s; command bint iso run %[2]s --name %[3]s -- $argv; end\n"
	case "pwsh":
		tmpl = "function %[1]s { & bint iso run %[2]s --name %[3]s -- @args }\n"
	default:
		return "", fmt.Errorf("shell không hỗ trợ %q (hỗ trợ: %s)", shell, strings.Join(Shells, ", "))
	}
	var b strings.Builder
	b.WriteString("# bint iso shell-init " + shell + ": mỗi profile một hàm <tool>-<name> (không ghi tệp nào)\n")
	n := 0
	for _, p := range profiles {
		fn := string(p.Tool) + "-" + p.Name
		if !shellFuncRE.MatchString(fn) {
			fmt.Fprintf(&b, "# bỏ qua %s: %q không dùng được làm tên hàm\n", p.ID(), fn)
			continue
		}
		fmt.Fprintf(&b, tmpl, fn, p.Tool, p.Name)
		n++
	}
	if n == 0 {
		b.WriteString("# chưa có profile nào dùng được (bint iso create <claude|codex> --name N)\n")
	}
	return b.String(), nil
}
