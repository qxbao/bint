package evolve

import (
	tea "charm.land/bubbletea/v2"
)

// Paste chèn chữ dán vào ô đang nhận phím, theo cùng thứ tự với Key: hộp thoại, form (bỏ qua khi đang gửi).
func (m *Model) Paste(s string) tea.Cmd {
	switch {
	case m.dlg != nil:
		m.dlg.c.Paste(s)
	case m.frm != nil:
		if !m.frmBusy {
			m.frm.Paste(s)
		}
	}
	return nil
}
