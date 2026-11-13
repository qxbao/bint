package cli

import (
	"os"
	"strings"
	"testing"

	"bint/internal/tui/actions"
)

// docs/usage.md có mục "Giao diện terminal (TUI)" và mọi phím của hành động toàn cục có Key trong sổ đều xuất hiện trong mục đó.
func TestUsageDocumentsTUIGlobalKeys(t *testing.T) {
	b, err := os.ReadFile("../../docs/usage.md")
	if err != nil {
		t.Fatal(err)
	}
	const head = "## Giao diện terminal (TUI)"
	s := string(b)
	i := strings.Index(s, head)
	if i < 0 {
		t.Fatalf("docs/usage.md thiếu mục %q", head)
	}
	sec := s[i+len(head):]
	if j := strings.Index(sec, "\n## "); j >= 0 {
		sec = sec[:j]
	}
	for _, w := range []string{"bint tui", "--lang", "--read-only", "tui-plan.md", "NO_COLOR", "Tám tab", "chỉ đọc", "Chuột", "Ngôn ngữ", "`Ctrl+K`", "không phải terminal",
		"Tổng quan", "Tri thức", "Tác vụ", "Lượt chạy", "Đánh giá", "Tiến hoá", "Hệ thống", "MCP"} {
		if !strings.Contains(sec, w) {
			t.Errorf("mục TUI của docs/usage.md thiếu %q", w)
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
			want = "`1`-`8`"
		}
		if !strings.Contains(sec, want) {
			t.Errorf("phím %s của hành động toàn cục %s không có trong mục TUI của docs/usage.md (tìm %s)", a.Key, a.ID, want)
		}
	}
	if n < 10 {
		t.Fatalf("quét thiếu hành động toàn cục trong docs/usage.md: %d", n)
	}
	// Phím làm mới `r` là phím của từng màn (không Global trong sổ) nhưng là phím toàn cục theo mô tả của docs/usage.md.
	if !strings.Contains(sec, "`r`") {
		t.Error("docs/usage.md phải nêu phím r")
	}
}
