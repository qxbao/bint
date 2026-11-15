package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/i18n"
)

// Chọn và chép bằng chuột: kéo chuột trái (nhấn, di, thả) bôi đen một vùng trên bản vẽ; khi thả, chữ thuần của vùng
// (không mã ANSI, bỏ khoảng trắng cuối dòng) được chép vào clipboard qua OSC52 (tea.SetClipboard) và clipboard hệ thống.
// Nhấn không kéo vẫn là bấm, nhưng hành động chạy lúc thả (nhấn chỉ ghi điểm neo). Vùng chọn là dòng chảy: dòng đầu từ cột neo, dòng giữa trọn dòng, dòng cuối tới cột thả.

type cell struct{ x, y int }

// selection là trạng thái vùng chọn.
type selection struct {
	anchor, end cell
	pressed     bool // đang giữ nút trái (từ lúc nhấn tới lúc thả)
	dragging    bool // đã di sang ô khác điểm neo
	shown       bool // vùng vừa chép còn được tô cho tới lần nhấn phím hoặc nhấn chuột kế tiếp
}

func (m *Model) selectable() bool { return !m.mouseOff && m.w >= MinW && m.h >= MinH }

// clearSelection xoá vùng chọn (nhấn phím hoặc nhấn chuột mới).
func (m *Model) clearSelection() { m.sel = selection{} }

// selPress ghi điểm neo sau khi bấm trái đã được xử lý như cũ.
func (m *Model) selPress(mo tea.Mouse) {
	m.clearSelection()
	if mo.Button != tea.MouseLeft || !m.selectable() {
		return
	}
	m.sel = selection{anchor: cell{mo.X, mo.Y}, end: cell{mo.X, mo.Y}, pressed: true}
}

// selMotion cập nhật điểm cuối khi kéo.
func (m *Model) selMotion(mo tea.Mouse) {
	if !m.sel.pressed || mo.Button != tea.MouseLeft || !m.selectable() {
		return
	}
	c := cell{mo.X, mo.Y}
	if !m.sel.dragging && c == m.sel.anchor {
		return
	}
	m.sel.dragging, m.sel.end = true, c
}

// selRelease kết thúc kéo: chép chữ của vùng. Không kéo thì không làm gì.
func (m *Model) selRelease(mo tea.Mouse) tea.Cmd {
	if !m.sel.pressed {
		return nil
	}
	m.sel.pressed = false
	if !m.sel.dragging && (mo.X != m.sel.anchor.x || mo.Y != m.sel.anchor.y) {
		m.sel.dragging, m.sel.end = true, cell{mo.X, mo.Y} // thả ở ô khác dù không có sự kiện di chuyển: vẫn là kéo
	}
	if !m.sel.dragging && m.selectable() {
		// Bấm thật (nhấn và thả cùng ô): chạy hành động của vùng tại điểm nhấn, đúng một lần.
		a := m.sel.anchor
		m.clearSelection()
		_, cmd := m.click(tea.Mouse{X: a.x, Y: a.y, Button: tea.MouseLeft})
		return cmd
	}
	if !m.sel.dragging || !m.selectable() {
		m.clearSelection()
		return nil
	}
	if mo.X != m.sel.end.x || mo.Y != m.sel.end.y {
		m.sel.end = cell{mo.X, mo.Y}
	}
	txt := m.selectedText(strings.Split(m.Render(), "\n"))
	if strings.TrimSpace(txt) == "" {
		m.clearSelection()
		return nil
	}
	m.sel.shown = true
	m.setToast(m.cat.T("ui.copied", i18n.A{"n": len([]rune(txt))}), false)
	clip := m.clip
	return tea.Batch(tea.SetClipboard(txt), func() tea.Msg {
		_ = clip.Write(txt) // OSC52 đã gửi: lỗi clipboard hệ thống (vd. Linux thiếu xclip) không báo để khỏi nhiễu
		return nil
	})
}

// span trả vùng chọn theo thứ tự đọc (đầu <= cuối).
func (s selection) span() (a, b cell) {
	a, b = s.anchor, s.end
	if b.y < a.y || b.y == a.y && b.x < a.x {
		a, b = b, a
	}
	return a, b
}

// cols trả khoảng cột [l, r) của dòng y trong vùng chọn; ok = false nếu dòng nằm ngoài vùng.
func (s selection) cols(y, width int) (l, r int, ok bool) {
	a, b := s.span()
	if y < a.y || y > b.y {
		return 0, 0, false
	}
	l, r = 0, width
	if y == a.y {
		l = a.x
	}
	if y == b.y {
		r = b.x + 1
	}
	if r <= l {
		return 0, 0, false
	}
	return l, r, true
}

// selectedText lấy chữ thuần của vùng chọn từ các dòng đã vẽ (mỗi dòng có thể mang mã ANSI).
func (m *Model) selectedText(lines []string) string {
	a, b := m.sel.span()
	var out []string
	for y := a.y; y <= b.y; y++ {
		if y < 0 || y >= len(lines) {
			out = append(out, "")
			continue
		}
		w := ansi.StringWidth(lines[y])
		l, r, ok := m.sel.cols(y, w)
		if !ok {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(lines[y], l, r)), " \t"))
	}
	return strings.Join(out, "\n")
}

// highlight tô đảo màu vùng chọn trên bản vẽ đưa ra màn hình (không dùng cho Render để golden và test không đổi).
func (m *Model) highlight(frame string) string {
	if !m.sel.dragging && !m.sel.shown {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for y, line := range lines {
		w := ansi.StringWidth(line)
		l, r, ok := m.sel.cols(y, w)
		if !ok {
			continue
		}
		r = min(r, w)
		if l >= r {
			continue
		}
		mid := ansi.Strip(ansi.Cut(line, l, r))
		lines[y] = ansi.Cut(line, 0, l) + "\x1b[7m" + mid + "\x1b[27m" + ansi.Cut(line, r, w)
	}
	return strings.Join(lines, "\n")
}
