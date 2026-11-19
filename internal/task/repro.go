package task

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	reproCmdRe  = regexp.MustCompile("`[^`\\n]*[^`\\s][^`\\n]*`")
	reproFileRe = regexp.MustCompile("([^\\s:`'\"()]+):([1-9][0-9]*)(?:-([1-9][0-9]*))?")
)

// reproPrograms: tên chương trình đủ để một đoạn backtick một từ (vd `make`) được coi là lệnh tái hiện.
var reproPrograms = []string{"go", "sh", "bash", "zsh", "pwsh", "powershell", "cmd", "bint", "git", "npm", "npx", "pnpm", "yarn",
	"make", "python", "python3", "pytest", "curl", "node", "cargo", "dotnet", "mvn", "gradle", "staticcheck", "docker"}

// isReproCommand: đoạn trong backtick là lệnh khi có ít nhất hai từ hoặc bắt đầu bằng tên chương trình quen thuộc;
// định danh/tệp đơn (`foo.go`, `blockFinalize`) chỉ là tên, không phải lệnh.
func isReproCommand(s string) bool {
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	if len(f) >= 2 {
		return true
	}
	return slices.Contains(reproPrograms, strings.TrimPrefix(strings.ToLower(f[0]), "./"))
}

// ValidRepro cho biết repro đủ làm bằng chứng cho phát hiện mức chặn: có lệnh trong dấu backtick (>= hai từ hoặc bắt đầu bằng tên chương trình), hoặc có tham chiếu tệp:dòng
// trỏ vào tệp có thật trong root (dòng không vượt số dòng của tệp). Lập luận thuần không hợp lệ.
func ValidRepro(repro, root string) bool {
	for _, m := range reproCmdRe.FindAllString(repro, -1) {
		if isReproCommand(strings.Trim(m, "`")) {
			return true
		}
	}
	for _, m := range reproFileRe.FindAllStringSubmatch(repro, -1) {
		p := strings.TrimPrefix(filepath.ToSlash(m[1]), "./")
		if filepath.IsAbs(p) || slices.Contains(strings.Split(p, "/"), "..") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		n := bytes.Count(data, []byte("\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			n++
		}
		last := m[2]
		if m[3] != "" {
			last = m[3]
		}
		if l, _ := strconv.Atoi(last); l <= n {
			return true
		}
	}
	return false
}

// downgradeUnproven hạ phát hiện bug/risk mức chặn thiếu repro hợp lệ xuống medium, ghi mức gốc vào DowngradedFrom.
func downgradeUnproven(fs []Finding, root string) {
	for i := range fs {
		if f := &fs[i]; f.Blocking() && !ValidRepro(f.Repro, root) {
			f.DowngradedFrom, f.Severity = f.Severity, "medium"
		}
	}
}
