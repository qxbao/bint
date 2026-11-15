package system

import (
	"bint/internal/tui/components/input"
	tea "charm.land/bubbletea/v2"
)

// Paste chèn chữ dán vào ô đang nhận phím, theo cùng thứ tự với Key: hộp thoại, form (bỏ qua khi đang gửi), ô lọc.
func (m *Model) Paste(s string) tea.Cmd {
	switch {
	case m.dlg != nil:
		m.dlg.c.Paste(s)
	case m.frm != nil:
		if !m.frmBusy {
			m.frm.Paste(s)
		}
	case m.textIn:
		in := input.New(m.filter)
		if in.Paste(s) {
			m.filter = in.Text
		}
		m.rebuildComponents(true)
		return nil
	}
	return nil
}
