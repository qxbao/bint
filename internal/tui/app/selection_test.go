package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/clipboard"
)

func drag(t *testing.T, m *Model, x0, y0, x1, y1 int) tea.Cmd {
	t.Helper()
	send(t, m, tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	send(t, m, tea.MouseMotionMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	return cmd
}

func TestDragSelectsAndCopiesPlainText(t *testing.T) {
	fake := &clipboard.Fake{}
	m := mouseModel(t, 140, 30, "3")
	m.clip = fake
	x, y := cellOf(t, m, "Thêm màn Tác vụ")
	w := ansi.StringWidth("Thêm màn Tác vụ")
	cmd := drag(t, m, x, y, x+w-1, y)
	if cmd == nil {
		t.Fatal("thả sau khi kéo phải trả Cmd chép")
	}
	drive(t, m, cmd)
	if fake.Text != "Thêm màn Tác vụ" {
		t.Fatalf("clipboard = %q", fake.Text)
	}
	if strings.Contains(fake.Text, "\x1b") {
		t.Fatal("văn bản chép không được có ANSI")
	}
	if m.toastErr || m.toast == "" {
		t.Fatalf("toast = %q err=%v", m.toast, m.toastErr)
	}
	// vùng còn tô khi vẽ ra màn hình, nhưng Render giữ nguyên
	if !strings.Contains(m.highlight(m.Render()), "\x1b[7m") {
		t.Fatal("View phải tô đảo màu vùng chọn")
	}
	if strings.Contains(m.Render(), "\x1b[7mThêm màn Tác vụ\x1b[27m") {
		t.Fatal("Render không được chứa đảo màu của vùng chọn")
	}
	// nhấn phím xoá vùng chọn
	press(t, m, "j")
	if strings.Contains(m.highlight(m.Render()), "\x1b[7m"+"Thêm") {
		t.Fatal("vùng chọn phải bị xoá khi nhấn phím")
	}
}

func TestDragMultiLineAndReverseTrimsTrailingSpaces(t *testing.T) {
	fake := &clipboard.Fake{}
	m := mouseModel(t, 140, 30, "3")
	m.clip = fake
	x, y := cellOf(t, m, "Thêm màn Tác vụ")
	// kéo ngược từ dòng dưới lên
	cmd := drag(t, m, 20, y+2, x, y)
	drive(t, m, cmd)
	lines := strings.Split(fake.Text, "\n")
	if len(lines) != 3 {
		t.Fatalf("muốn 3 dòng, có %d: %q", len(lines), fake.Text)
	}
	for _, l := range lines {
		if l != strings.TrimRight(l, " ") {
			t.Fatalf("dòng còn khoảng trắng cuối: %q", l)
		}
	}
	if !strings.HasPrefix(lines[0], "Thêm màn Tác vụ") {
		t.Fatalf("dòng đầu = %q", lines[0])
	}
}

func TestClickWithoutDragDoesNotCopy(t *testing.T) {
	fake := &clipboard.Fake{}
	m := mouseModel(t, 140, 30, "3")
	m.clip = fake
	x, y := cellOf(t, m, "Thêm màn Tác vụ")
	send(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatal("nhấn rồi thả không kéo thì không chép")
	}
	if fake.Writes != 0 {
		t.Fatal("clipboard bị ghi")
	}
}

func TestDragIgnoredWhenMouseOff(t *testing.T) {
	fake := &clipboard.Fake{}
	m := mouseModel(t, 140, 30, "3")
	m.clip = fake
	m.mouseOff = true
	if cmd := drag(t, m, 5, 5, 30, 6); cmd != nil {
		t.Fatal("chuột tắt thì không chép")
	}
	if fake.Writes != 0 {
		t.Fatal("clipboard bị ghi")
	}
}
