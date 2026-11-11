// Package confirm là hộp xác nhận hoặc nhập lý do: tiêu đề, các dòng nội dung (cuộn được khi dài), tuỳ
// chọn một ô nhập, hai nút Enter/Esc. Hộp được vẽ thành khối dòng để phủ lên màn hình bằng text.Overlay.
package confirm

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/theme"
)

// Result là kết quả xử lý một phím.
type Result int

const (
	Pending Result = iota
	Yes
	No
)

// Model là hộp xác nhận.
type Model struct {
	Title string
	// Body là các dòng nội dung đã dựng sẵn (có thể mang mã màu).
	Body []string
	// Danger: tô nút xác nhận bằng màu lỗi (thao tác khó đảo ngược).
	Danger bool
	// Input: có ô nhập (lý do). Label là nhãn ô; Placeholder hiện mờ khi ô trống; Required bắt buộc không trống
	// (Enter khi trống không xác nhận, hiện RequiredMsg).
	Input       bool
	Label       string
	Placeholder string
	Required    bool
	RequiredMsg string
	// YesLabel/NoLabel là nhãn hai nút (đã qua i18n).
	YesLabel, NoLabel string
	// Checks là các ô chọn (nhãn đã qua i18n), mặc định không chọn: hộp cảnh báo cho điều kiện bị bỏ qua (kiểm chứng,
	// rủi ro, xung đột tệp...) — mỗi ô một cờ, người chọn thì cờ mới được đặt. Có ô chọn thì y/n không xác nhận/từ
	// chối nữa (tránh bấm nhầm); Tab/↑↓ dời giữa các ô và ô nhập, Space hoặc x đảo ô.
	Checks []string
	On     []bool

	In      input.Model
	Scroll  viewer.Model
	showErr bool
	focus   int // 0..len(Checks)-1: ô chọn; len(Checks): ô nhập (khi có)
}

// Checked cho biết ô chọn i đang được chọn.
func (m Model) Checked(i int) bool { return i >= 0 && i < len(m.On) && m.On[i] }

// slots là số đích tiêu điểm: các ô chọn và ô nhập.
func (m Model) slots() int {
	n := len(m.Checks)
	if m.Input {
		n++
	}
	return n
}

// onInput: tiêu điểm đang ở ô nhập (hoặc hộp không có ô chọn).
func (m Model) onInput() bool { return m.Input && m.focus >= len(m.Checks) }

// Value là chữ đã nhập (đã cắt khoảng trắng đầu/cuối).
func (m Model) Value() string { return strings.TrimSpace(m.In.Text) }

// Update xử lý phím: Enter xác nhận (kiểm tra bắt buộc), Esc/Ctrl+C từ chối; khi có ô nhập mọi phím chữ
// vào ô; không có ô nhập thì y/n cũng được và phím cuộn cuộn nội dung.
func (m *Model) Update(msg tea.KeyPressMsg) Result {
	switch s := msg.String(); s {
	case "esc", "ctrl+c":
		return No
	case "enter":
		if m.Input && m.Required && m.Value() == "" {
			m.showErr = true
			return Pending
		}
		return Yes
	case "pgdown":
		m.Scroll.Page(1)
		return Pending
	case "pgup":
		m.Scroll.Page(-1)
		return Pending
	}
	if len(m.Checks) > 0 {
		if len(m.On) < len(m.Checks) {
			m.On = append(m.On, make([]bool, len(m.Checks)-len(m.On))...)
		}
		switch s := msg.String(); s {
		case "tab", "down":
			m.focus = (m.focus + 1) % m.slots()
			return Pending
		case "shift+tab", "up":
			m.focus = (m.focus + m.slots() - 1) % m.slots()
			return Pending
		}
		if !m.onInput() {
			switch msg.String() {
			case " ", "space", "x":
				m.On[m.focus] = !m.On[m.focus]
			}
			return Pending
		}
	}
	if m.Input {
		m.showErr = false
		m.In.Update(msg)
		return Pending
	}
	if len(m.Checks) > 0 {
		return Pending // có ô chọn: y/n không xác nhận hay từ chối
	}
	switch msg.String() {
	case "y", "Y":
		return Yes
	case "n", "N":
		return No
	case "j", "down":
		m.Scroll.Scroll(1)
	case "k", "up":
		m.Scroll.Scroll(-1)
	}
	return Pending
}

// View vẽ hộp rộng w, cao tối đa h (cao vừa nội dung).
func (m *Model) View(th theme.Theme, w, h int) []string {
	iw := max(w-4, 10)
	var extra []string
	if len(m.On) < len(m.Checks) {
		m.On = append(m.On, make([]bool, len(m.Checks)-len(m.On))...)
	}
	for i, c := range m.Checks {
		box := "[ ]"
		if m.On[i] {
			box = "[x]"
		}
		if i == 0 {
			extra = append(extra, "")
		}
		for j, l := range text.WrapHang(box+" "+c, max(iw-2, 8), "", "    ") {
			switch {
			case m.focus == i && j == 0:
				l = th.S(theme.Key, "▸ ") + th.Selected(l)
			case m.focus == i:
				l = "  " + th.Selected(l)
			default:
				l = "  " + l
			}
			extra = append(extra, l)
		}
	}
	if m.Input {
		line := m.In.Line(th, max(iw-text.Width(m.Label)-2, 4), m.onInput())
		if m.In.Text == "" && m.Placeholder != "" {
			line = text.Pad(input.Cursor+th.S(theme.Muted, text.Truncate(m.Placeholder, max(iw-text.Width(m.Label)-4, 1))), max(iw-text.Width(m.Label)-2, 4))
		}
		extra = append(extra, "", th.S(theme.Accent, m.Label)+": "+line)
		if m.showErr && m.RequiredMsg != "" {
			extra = append(extra, th.S(theme.Err, "✗ "+m.RequiredMsg))
		}
	}
	buttons := th.S(theme.Key, "[Enter]") + " " + m.YesLabel + "   " + th.S(theme.Key, "[Esc]") + " " + m.NoLabel
	if m.Danger {
		buttons = th.S(theme.Err, "[Enter]") + " " + m.YesLabel + "   " + th.S(theme.Key, "[Esc]") + " " + m.NoLabel
	}
	chrome := len(extra) + 2 // dòng trống + nút
	room := max(h-2-chrome, 1)
	// Dòng dài được ngắt theo độ rộng hộp (mã màu giữ nguyên) thay vì bị cắt cụt.
	var wrapped []string
	for _, l := range m.Body {
		wrapped = append(wrapped, text.Wrap(l, iw)...)
	}
	vh := min(len(wrapped), room)
	m.Scroll.SetLines(wrapped)
	body := strings.Split(m.Scroll.Render(iw, vh), "\n")
	if len(wrapped) == 0 {
		body = nil
	}
	lines := append(append(append([]string{}, body...), extra...), "", buttons)
	role := theme.Key
	return strings.Split(frame.Box{Title: m.Title, TitleAt: frame.Left, Focused: true, Top: th.S(role, m.Scroll.Position())}.
		Render(th, w, len(lines)+2, strings.Join(lines, "\n")), "\n")
}
