package viewer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func TestViewerScrollAndPosition(t *testing.T) {
	var m Model
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("dòng %d", i))
	}
	m.SetLines(lines)
	out := m.Render(20, 10)
	if got := strings.Split(out, "\n"); len(got) != 10 || !strings.HasPrefix(got[0], "dòng 0") {
		t.Fatalf("%q", out)
	}
	if m.Position() != "1-10/30" || !m.Overflow() {
		t.Fatalf("%q", m.Position())
	}
	m.Scroll(100)
	m.Render(20, 10)
	if m.Top != 20 || m.Position() != "21-30/30" {
		t.Fatalf("top=%d %q", m.Top, m.Position())
	}
	m.Page(-1)
	m.Render(20, 10)
	if m.Top != 11 {
		t.Fatalf("Page: %d", m.Top)
	}
	m.Home()
	m.End()
	if m.Top != 20 {
		t.Fatalf("End: %d", m.Top)
	}
	m.SetLines(lines[:3])
	if m.Top != 0 || m.Position() != "" {
		t.Fatalf("co lại: top=%d", m.Top)
	}
}

// Tab được đổi thành khoảng trắng để độ rộng theo ô ổn định; không sửa slice của người gọi.
func TestViewerExpandsTabsWithoutMutatingInput(t *testing.T) {
	in := []string{"a\tb", "không tab"}
	var m Model
	m.SetLines(in)
	if in[0] != "a\tb" {
		t.Fatal("slice đầu vào bị sửa")
	}
	if strings.Contains(m.Lines[0], "\t") || text.Width(m.Lines[0]) != len("a    b") {
		t.Fatalf("%q", m.Lines[0])
	}
}

func TestViewerRenderFitsWidth(t *testing.T) {
	var m Model
	m.SetLines([]string{"Ắ ề ộ ữ ợ — dòng rất dài vượt quá chiều rộng của vùng nhìn để thử cắt"})
	for _, l := range strings.Split(m.Render(20, 3), "\n") {
		if text.Width(l) != 20 {
			t.Fatalf("%q rộng %d", l, text.Width(l))
		}
	}
}

func TestColorDiffRoles(t *testing.T) {
	diff := "--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -1,2 +1,3 @@\n giữ\n-bỏ\n+thêm\n"
	want := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	col := ColorDiff(theme.New(theme.Dark), diff)
	if len(col) != len(want) {
		t.Fatalf("%d dòng, muốn %d", len(col), len(want))
	}
	for i, l := range col {
		if got := ansi.Strip(l); got != want[i] {
			t.Errorf("dòng %d: %q, muốn %q (mã màu không được đổi chữ)", i, got, want[i])
		}
		changed := l != want[i]
		if wantColor := i != 3; changed != wantColor { // chỉ dòng giữ nguyên là không tô
			t.Errorf("dòng %d (%q): tô màu=%v, muốn %v", i, want[i], changed, wantColor)
		}
	}
	if col[4] == col[5] {
		t.Error("dòng thêm và dòng xoá phải khác màu")
	}
}
