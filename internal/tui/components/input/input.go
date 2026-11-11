// Package input là ô nhập chữ tối giản (một dòng hoặc nhiều dòng) dùng chung cho form, hộp xác nhận và
// ô lọc: chèn chữ, xoá, di chuyển con trỏ, gợi ý mờ chấp nhận bằng phím phải. Không phụ thuộc
// textinput của Bubbles để việc đo độ rộng chữ có dấu thống nhất (text.Width, NFC).
package input

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Cursor là ký hiệu con trỏ (rộng 1 ô, thấy được cả khi đơn sắc).
const Cursor = "▏"

// Model là trạng thái ô nhập.
type Model struct {
	Text string
	// Cur là vị trí con trỏ tính theo rune trong Text.
	Cur       int
	Multiline bool
	// Ghost trả phần gợi ý mờ nối sau văn bản hiện tại (rỗng = không có); nil = không gợi ý.
	Ghost func(text string) string
}

// New dựng ô nhập với giá trị ban đầu, con trỏ ở cuối.
func New(s string) Model { return Model{Text: s, Cur: len([]rune(s))} }

// Set thay giá trị, con trỏ về cuối.
func (m *Model) Set(s string) { m.Text, m.Cur = s, len([]rune(s)) }

// Update xử lý một phím; trả true nếu ô nhập đã dùng phím đó.
func (m *Model) Update(msg tea.KeyPressMsg) bool {
	r := []rune(m.Text)
	m.Cur = max(0, min(m.Cur, len(r)))
	switch s := msg.String(); s {
	case "backspace":
		if m.Cur > 0 {
			m.Text, m.Cur = string(r[:m.Cur-1])+string(r[m.Cur:]), m.Cur-1
		}
	case "delete":
		if m.Cur < len(r) {
			m.Text = string(r[:m.Cur]) + string(r[m.Cur+1:])
		}
	case "left":
		m.Cur = max(m.Cur-1, 0)
	case "right":
		if g := m.ghost(); g != "" && m.Cur == len(r) {
			m.Text += g
			m.Cur = len([]rune(m.Text))
			return true
		}
		m.Cur = min(m.Cur+1, len(r))
	case "home":
		m.Cur = 0
	case "end":
		if g := m.ghost(); g != "" && m.Cur == len(r) {
			m.Text += g
		}
		m.Cur = len([]rune(m.Text))
	case "ctrl+u":
		m.Text, m.Cur = string(r[m.Cur:]), 0
	case "ctrl+w":
		i := m.Cur
		for i > 0 && r[i-1] == ' ' {
			i--
		}
		for i > 0 && r[i-1] != ' ' {
			i--
		}
		m.Text, m.Cur = string(r[:i])+string(r[m.Cur:]), i
	case "enter":
		if !m.Multiline {
			return false
		}
		m.insert("\n")
	default:
		if msg.Text == "" || msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 {
			return false
		}
		m.insert(msg.Text)
	}
	return true
}

func (m *Model) insert(s string) {
	r := []rune(m.Text)
	m.Cur = max(0, min(m.Cur, len(r)))
	m.Text = string(r[:m.Cur]) + s + string(r[m.Cur:])
	m.Cur += len([]rune(s))
}

func (m Model) ghost() string {
	if m.Ghost == nil || m.Text == "" {
		return ""
	}
	return m.Ghost(m.Text)
}

// Line vẽ ô nhập một dòng rộng đúng w ô: văn bản (kèm con trỏ khi focused) và gợi ý mờ. Văn bản dài
// hơn w được cuộn để con trỏ luôn thấy được.
func (m Model) Line(th theme.Theme, w int, focused bool) string {
	r := []rune(strings.ReplaceAll(m.Text, "\n", "⏎"))
	cur := max(0, min(m.Cur, len(r)))
	before, after := string(r[:cur]), string(r[cur:])
	mark := ""
	if focused {
		mark = Cursor
	}
	if text.Width(before) > w-2 {
		before = text.TruncateHead(before, max(w-2, 1))
	}
	main := before + mark + after
	room := w - text.Width(main)
	if room < 0 {
		main, room = text.Truncate(main, w), 0
	}
	if g := m.ghost(); focused && cur == len(r) && g != "" && room > 0 {
		main += th.S(theme.Muted, text.Truncate(g, room))
	}
	return text.Pad(main, w)
}

// Lines vẽ ô nhiều dòng: tối đa h dòng (cuộn theo con trỏ), mỗi dòng rộng w ô.
func (m Model) Lines(th theme.Theme, w, h int, focused bool) []string {
	r := []rune(m.Text)
	cur := max(0, min(m.Cur, len(r)))
	s := string(r[:cur])
	if focused {
		s += Cursor
	}
	s += string(r[cur:])
	all := strings.Split(s, "\n")
	line := strings.Count(string(r[:cur]), "\n")
	top := max(0, min(line-h+1, len(all)-h))
	if line < top {
		top = line
	}
	var out []string
	for i := top; i < len(all) && len(out) < h; i++ {
		out = append(out, text.Fit(all[i], w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}
