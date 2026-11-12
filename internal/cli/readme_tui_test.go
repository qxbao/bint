package cli

import (
	"os"
	"strings"
	"testing"

	"bint/internal/tui/actions"
)

// README có mục "Giao diện terminal (TUI)" và mọi phím của hành động toàn cục có Key trong sổ đều xuất hiện trong mục đó.
func TestREADMEDocumentsTUIGlobalKeys(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	const head = "## Giao diện terminal (TUI)"
	s := string(b)
	i := strings.Index(s, head)
	if i < 0 {
		t.Fatalf("README thiếu mục %q", head)
	}
	sec := s[i+len(head):]
	if j := strings.Index(sec, "\n## "); j >= 0 {
		sec = sec[:j]
	}
	for _, w := range []string{"bint tui", "--lang", "--read-only", "docs/tui-plan.md", "NO_COLOR", "Bảy tab", "chỉ đọc", "Chuột", "Ngôn ngữ", "`Ctrl+K`", "không phải terminal",
		"Tổng quan", "Tri thức", "Tác vụ", "Lượt chạy", "Đánh giá", "Tiến hoá", "Hệ thống"} {
		if !strings.Contains(sec, w) {
			t.Errorf("mục TUI của README thiếu %q", w)
		}
	}
	n := 0
	for _, a := range actions.Default().All() {
		if a.Screen != actions.Global || a.Key == "" {
			continue
		}
		n++
		want := "`" + a.Key + "`"
		if strings.HasPrefix(a.ID, "nav.") {
			want = "`1`-`7`"
		}
		if !strings.Contains(sec, want) {
			t.Errorf("phím %s của hành động toàn cục %s không có trong mục TUI của README (tìm %s)", a.Key, a.ID, want)
		}
	}
	if n < 10 {
		t.Fatalf("quét thiếu hành động toàn cục: %d", n)
	}
	// Phím làm mới `r` là phím của từng màn (không Global trong sổ) nhưng là phím toàn cục theo mô tả của README.
	if !strings.Contains(sec, "`r`") {
		t.Error("README phải nêu phím r")
	}
}
