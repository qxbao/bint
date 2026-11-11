package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/tui/theme"
)

var update = flag.Bool("update", false, "ghi lại tệp golden")

// checkGolden so sánh got với testdata/golden/<name>; -update để ghi lại. Golden chỉ chống thụt lùi;
// thẩm mỹ do chủ dự án duyệt bằng ảnh VHS (internal/tui/testdata/vhs).
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("thiếu golden %s (chạy go test ./internal/tui/app -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("golden %s khác kết quả hiện tại (chạy với -update nếu thay đổi là chủ ý):\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// Ảnh chụp văn bản (đã bỏ mã màu) của từng màn hình ở 80×24 và 140×40, vi/en/locale giả.
func TestGoldenScreens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			screens := map[string]func() *Model{
				"overview": func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1]) },
				"init":     func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), sz[0], sz[1]) },
				"knowledge": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2")
					return m
				},
				"palette": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, ":")
					return m
				},
				"help": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "?")
					return m
				},
			}
			for name, build := range screens {
				checkGolden(t, fmt.Sprintf("%s_%dx%d_%s.txt", name, sz[0], sz[1], lang), plain(build().Render())+"\n")
			}
		}
	}
}

// Ảnh chụp có mã màu của Tổng quan ở từng chế độ màu (bắt lỗi đổi vai trò màu).
func TestGoldenColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 80, 24)
		out := strings.ReplaceAll(m.Render(), "\x1b", "␛")
		checkGolden(t, fmt.Sprintf("overview_80x24_vi_%s.ansi.txt", mode), out+"\n")
	}
}
