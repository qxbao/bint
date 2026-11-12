package system

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/list"
	"bint/internal/tui/ui"
)

// Toast trả nguyên văn thông báo hiện tại (ui.Toaster).
func (m *Model) Toast() (string, bool) { return m.toast, m.toastErr }

// Click xử lý bấm chuột (ui.Pointer): thẻ, hàng (bấm lại hàng đang chọn = Enter), focus khung.
func (m *Model) Click(id string) tea.Cmd {
	if m.Capturing() {
		return nil
	}
	m.toast = ""
	if i, ok := ui.ParseSubZone(id); ok && i >= 0 && i < numTabs {
		return m.setTab(i)
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
	if i, ok := list.ParseRowZone(id); ok {
		m.pane = paneList
		lst := &m.lst[m.tab]
		return ui.RowClick(lst, i, func() tea.Cmd { m.nav(0); return nil }, func() tea.Cmd { cmd, _ := m.Key(ui.EnterKey()); return cmd })
	}
	return nil
}

// Wheel cuộn khung dưới con trỏ chuột.
func (m *Model) Wheel(pane string, dy int) tea.Cmd {
	if m.Capturing() {
		return nil
	}
	switch pane {
	case "list":
		m.pane = paneList
		m.nav(dy)
	case "detail":
		m.det.Scroll(dy)
	}
	return nil
}

// SubTab trả tên thẻ con đang chọn (ui.SubTabber).
func (m *Model) SubTab() string { return m.t("sy.tab." + TabNames[m.tab]) }

// HelpTab trả mã thẻ phụ đang xem (ui.Helper).
func (m *Model) HelpTab() string { return TabNames[m.tab] }
