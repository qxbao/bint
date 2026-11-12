package tasks

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/list"
	"bint/internal/tui/ui"
)

// Toast trả nguyên văn thông báo hiện tại (ui.Toaster).
func (m *Model) Toast() (string, bool) { return m.toast, m.toastErr }

// Click xử lý bấm chuột (ui.Pointer): thẻ con, hàng tác vụ (bỏ qua tiêu đề nhóm; bấm lại hàng đang chọn = Enter), focus khung.
func (m *Model) Click(id string) tea.Cmd {
	if m.Capturing() {
		return nil
	}
	m.toast = ""
	if s, ok := ui.ParseSubZone(id); ok {
		if !slices.Contains(m.visibleSubs(), s) {
			return nil
		}
		m.setSub(s)
		return m.wantDetail()
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
	if i, ok := list.ParseRowZone(id); ok && i < len(m.rows) && m.rows[i].t != nil {
		m.pane = paneList
		return ui.RowClick(&m.lst, i, m.wantDetail, func() tea.Cmd { cmd, _ := m.Key(ui.EnterKey()); return cmd })
	}
	return nil
}

// Wheel cuộn khung dưới con trỏ chuột: danh sách dời con trỏ (nhảy qua tiêu đề nhóm), chi tiết cuộn.
func (m *Model) Wheel(pane string, dy int) tea.Cmd {
	if m.Capturing() {
		return nil
	}
	switch pane {
	case "list":
		m.move(dy)
		return m.wantDetail()
	case "detail":
		if m.sub == subLog {
			m.scrollLog(dy)
		} else {
			m.detail.Scroll(dy)
		}
	}
	return nil
}

// SubTab trả tên thẻ con đang chọn (ui.SubTabber).
func (m *Model) SubTab() string { return m.t("tk.sub." + subNames[m.sub]) }

// HelpTab trả mã thẻ phụ đang xem (ui.Helper).
func (m *Model) HelpTab() string { return subNames[m.sub] }
