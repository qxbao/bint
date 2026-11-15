package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/actions"
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
	drive(t, m, cmd) // thả không kéo chỉ chạy hành động của vùng bấm, không chép
	if m.sel.shown {
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

// Nhấn chuột chỉ ghi điểm neo; hành động của vùng bấm chạy đúng một lần lúc thả cùng ô.
func TestClickRunsOnReleaseOnly(t *testing.T) {
	m := mouseModel(t, 140, 40)
	x, y := cellOf(t, m, "Tri thức")
	before := m.Screen()
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.Screen() != before {
		t.Fatal("nhấn chưa được đổi tab")
	}
	if !m.sel.pressed {
		t.Fatal("nhấn phải ghi điểm neo")
	}
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, m, cmd)
	if m.Screen() != actions.Knowledge {
		t.Fatalf("thả cùng ô phải chuyển tab: %s", m.Screen())
	}
	// Thả lần nữa không có nhấn trước: không làm gì thêm.
	m.run(actions.NavID(actions.Overview))
	if _, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}); cmd != nil || m.Screen() != actions.Overview {
		t.Fatalf("thả không có nhấn không được chạy hành động (màn %s)", m.Screen())
	}
}

// Kéo bắt đầu trên tab hoặc nút chân phím rồi thả chỉ chép vùng chọn, không chạy hành động.
func TestDragStartedOnClickableOnlyCopies(t *testing.T) {
	for _, tc := range []struct {
		name, needle string
		check        func(m *Model) bool
	}{
		{"tab", "Tri thức", func(m *Model) bool { return m.Screen() == actions.Overview }},
		{"chân phím ?", "[?→", func(m *Model) bool { return !m.showHelp }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &clipboard.Fake{}
			m := mouseModel(t, 140, 40)
			m.clip = fake
			x, y := cellOf(t, m, tc.needle)
			cmd := drag(t, m, x, y, x+3, y)
			drive(t, m, cmd)
			if !tc.check(m) {
				t.Fatalf("kéo bắt đầu trên %s không được chạy hành động của nó", tc.name)
			}
			if fake.Writes == 0 {
				t.Fatal("kéo phải chép vùng chọn")
			}
		})
	}
	// Thả ở ô khác dù không có sự kiện di chuyển cũng là kéo.
	m := mouseModel(t, 140, 40)
	x, y := cellOf(t, m, "Tri thức")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x + 3, Y: y, Button: tea.MouseLeft})
	drive(t, m, cmd)
	if m.Screen() != actions.Overview {
		t.Fatalf("thả ở ô khác không được đổi tab: %s", m.Screen())
	}
}

// Sau khi chép bằng kéo chuột, cuộn bánh xe hoặc đổi kích thước xoá vùng tô đảo màu.
func TestWheelAndResizeClearCopiedHighlight(t *testing.T) {
	for _, name := range []string{"wheel", "resize"} {
		t.Run(name, func(t *testing.T) {
			m := mouseModel(t, 140, 30, "3")
			m.clip = &clipboard.Fake{}
			x, y := cellOf(t, m, "Thêm màn Tác vụ")
			drive(t, m, drag(t, m, x, y, x+10, y))
			if !strings.Contains(m.highlight(m.Render()), "\x1b[7m") {
				t.Fatal("phải đang tô vùng đã chép")
			}
			if name == "wheel" {
				m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
			} else {
				m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
			}
			if m.sel.shown || m.sel.dragging {
				t.Fatalf("%s phải xoá vùng tô", name)
			}
		})
	}
}
