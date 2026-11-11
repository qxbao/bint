package tui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// vhsCommands là các lệnh VHS được dùng trong kịch bản (kiểm tra cú pháp sơ bộ: không có VHS trong CI).
var vhsCommands = map[string]bool{
	"Output": true, "Set": true, "Hide": true, "Show": true, "Type": true, "Enter": true, "Sleep": true,
	"Screenshot": true, "Escape": true, "Tab": true, "Down": true, "Up": true, "Ctrl+C": true, "Require": true, "Env": true,
}

var screenshotRE = regexp.MustCompile(`^Screenshot (internal/tui/testdata/vhs/out/[a-z0-9-]+\.png)$`)

// Mỗi màn hình có kịch bản VHS: có Output, ít nhất một Screenshot vào thư mục out/, mọi lệnh hợp lệ, và
// các tham số Set đặt trước lệnh đầu tiên không phải Set (VHS không cho đổi cỡ cửa sổ giữa chừng).
func TestVHSTapesAreWellFormed(t *testing.T) {
	tapes, _ := filepath.Glob(filepath.Join("testdata", "vhs", "*.tape"))
	have := map[string]bool{}
	for _, p := range tapes {
		have[filepath.Base(p)] = true
	}
	for _, want := range []string{"overview.tape", "init.tape", "gallery.tape"} {
		if !have[want] {
			t.Errorf("thiếu kịch bản %s", want)
		}
	}
	for _, p := range tapes {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		hasOutput, shots, sawAction := false, 0, false
		for n, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cmd := strings.Fields(line)[0]
			if !vhsCommands[cmd] {
				t.Errorf("%s:%d: lệnh VHS lạ %q", p, n+1, cmd)
			}
			switch cmd {
			case "Output":
				hasOutput = true
				if !strings.HasPrefix(line, "Output internal/tui/testdata/vhs/out/") {
					t.Errorf("%s:%d: Output phải nằm trong internal/tui/testdata/vhs/out/", p, n+1)
				}
			case "Screenshot":
				shots++
				if !screenshotRE.MatchString(line) {
					t.Errorf("%s:%d: Screenshot sai khuôn dạng: %s", p, n+1, line)
				}
			case "Set":
				if sawAction && (strings.Contains(line, "Width") || strings.Contains(line, "Height") || strings.Contains(line, "FontSize")) {
					t.Errorf("%s:%d: VHS không đổi kích thước/phông giữa chừng: %s", p, n+1, line)
				}
			default:
				if cmd != "Output" {
					sawAction = true
				}
			}
		}
		if !hasOutput || shots == 0 {
			t.Errorf("%s: cần Output và ít nhất một Screenshot (có %v, %d)", p, hasOutput, shots)
		}
	}
}
