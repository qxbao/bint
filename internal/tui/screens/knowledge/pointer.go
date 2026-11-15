package knowledge

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/form"
	"bint/internal/tui/components/list"
	"bint/internal/tui/ui"
)

// Toast trả nguyên văn thông báo hiện tại (ui.Toaster).
func (m *Model) Toast() (string, bool) { return m.toast, m.toastErr }

// Click xử lý bấm chuột (ui.Pointer): thẻ phụ, hàng danh sách (bấm lại hàng đang chọn = Enter), focus khung.
func (m *Model) Click(id string) tea.Cmd {
	if m.dlg != nil { // hộp xác nhận: bấm ô chọn/ô nhập (nút [Enter]/[Esc] là vùng key:)
		m.dlg.c.Click(id)
		return nil
	}
	if m.frm != nil { // biểu mẫu: tiêu điểm, ô Bool, lựa chọn (chỉ đổi trạng thái, không gửi)
		if !m.frmBusy {
			m.frm.Click(id)
		}
		return nil
	}
	if m.modalCapture() {
		return nil
	}
	if p := m.cur(); p.form != nil && form.IsZone(id) { // form nhúng trong thẻ phụ
		if !p.running {
			p.form.Click(id)
			p.inForm = true
		}
		return nil
	}
	m.toast = ""
	if i, ok := ui.ParseSubZone(id); ok && i >= 0 && i < numSubs {
		m.setSub(i)
		return m.autoRun()
	}
	if pane, ok := ui.ParsePane(id); ok {
		switch pane {
		case "list":
			m.pane = paneList
		case "detail":
			m.pane = paneDetail
		}
		return nil
	}
	if i, ok := list.ParseRowZone(id); ok && m.sub == subList {
		m.pane = paneList
		return ui.RowClick(&m.lst, i, m.wantDetail, func() tea.Cmd { cmd, _ := m.Key(ui.EnterKey()); return cmd })
	}
	return nil
}

// Wheel cuộn khung dưới con trỏ chuột: danh sách dời con trỏ, chi tiết/kết quả cuộn.
func (m *Model) Wheel(pane string, dy int) tea.Cmd {
	if m.modalCapture() {
		return nil
	}
	switch pane {
	case "list":
		m.lst.Move(dy)
		return m.wantDetail()
	case "detail":
		m.detail.Scroll(dy)
	case "view":
		m.cur().view.Scroll(dy)
	}
	return nil
}

// modalCapture: hộp thoại, form hoặc ô lọc đang mở (chuột bị bỏ qua). Ô nhập của thẻ phụ (inForm) không chặn chuột: bấm thẻ phụ khác là rời nó.
func (m *Model) modalCapture() bool { return m.dlg != nil || m.frm != nil || m.textIn }

// SubTab trả tên thẻ phụ đang chọn (ui.SubTabber).
func (m *Model) SubTab() string { return m.t("kn.sub." + subNames[m.sub]) }

// HelpTab trả mã thẻ phụ đang xem (ui.Helper).
func (m *Model) HelpTab() string { return subNames[m.sub] }
