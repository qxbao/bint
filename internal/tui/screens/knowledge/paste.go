package knowledge

import (
	tea "charm.land/bubbletea/v2"
)

// Paste chèn chữ dán vào ô đang nhận phím, theo cùng thứ tự với Key: hộp thoại, form (bỏ qua khi đang gửi), ô lọc và ô nhập của thẻ phụ.
func (m *Model) Paste(s string) tea.Cmd {
	switch {
	case m.dlg != nil:
		m.dlg.c.Paste(s)
	case m.frm != nil:
		if !m.frmBusy {
			m.frm.Paste(s)
		}
	case m.textIn:
		in := inputOf(m.fopts.Text)
		if in.Paste(s) {
			m.fopts.Text = in.Text
		}
		m.refilter()
		return m.wantDetail()
	case m.sub != subList && m.cur().inForm && !m.cur().running && m.cur().form != nil:
		m.cur().form.Paste(s)
	}
	return nil
}
