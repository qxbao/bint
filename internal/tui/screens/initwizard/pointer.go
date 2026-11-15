package initwizard

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/list"
	"bint/internal/tui/ui"
)

// Click xử lý bấm chuột (ui.Pointer): bấm một hàng của bước hỏi đặt con trỏ vào hàng đó và chọn/đảo như Space. Dòng phím
// (Enter/Esc...) là vùng key: do mô hình gốc xử lý.
func (m *Model) Click(id string) tea.Cmd {
	if m.o.ReadOnly || m.running || m.done || m.planning || m.step == stepPreview {
		return nil
	}
	i, ok := list.ParseRowZone(id)
	if !ok || i < 0 || i >= len(m.rows()) {
		return nil
	}
	m.cur = i
	m.pick(i)
	return nil
}

// Wheel: màn khởi tạo không cuộn bằng chuột.
func (m *Model) Wheel(pane string, dy int) tea.Cmd { return nil }

var _ ui.Pointer = (*Model)(nil)
