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

// ValidRepro cho biết repro đủ làm bằng chứng cho phát hiện mức chặn: có lệnh trong dấu backtick, hoặc có tham chiếu tệp:dòng
// trỏ vào tệp có thật trong root (dòng không vượt số dòng của tệp). Lập luận thuần không hợp lệ.
func ValidRepro(repro, root string) bool {
	if reproCmdRe.MatchString(repro) {
		return true
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
