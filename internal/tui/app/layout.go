package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/split"
	"bint/internal/tui/ui"
)

// Tỉ lệ pane của các màn hai pane: phím < > (đổi 5 điểm %), z (phóng to pane đang chọn), = (về mặc định), kéo đường chia bằng
// chuột. Tỉ lệ khác mặc định được lưu vào ui.layout của .bint/local.yml (trễ layoutSaveDelay để gom nhiều lần đổi liên tiếp) và
// nạp lại khi mở TUI. Chế độ chỉ đọc và phiên agent không ghi.

// layoutSaveDelay là độ trễ gom các lần đổi tỉ lệ trước khi ghi cấu hình.
const layoutSaveDelay = 500 * time.Millisecond

// layoutLoadedMsg mang ui.layout hiệu lực đọc lúc mở TUI.
type layoutLoadedMsg struct{ layout map[string]int }

// layoutSaveMsg kích lần ghi đã hẹn (seq cũ hơn lần đổi gần nhất thì bỏ qua).
type layoutSaveMsg struct{ seq int }

// layoutSavedMsg là kết quả ghi (lỗi hiện thành thông báo).
type layoutSavedMsg struct{ err error }

// splitter trả trạng thái chia của màn hiện tại (nil nếu màn không có hai pane).
func (m *Model) splitter() *split.State {
	if sp, ok := m.screens[m.Screen()].(ui.Splitter); ok {
		return sp.Splitter()
	}
	return nil
}

// loadLayout đọc ui.layout ở nền.
func (m *Model) loadLayout() tea.Cmd {
	dir := m.opts.Dir
	return func() tea.Msg {
		l, err := bintapp.UILayout(context.Background(), dir)
		if err != nil {
			l = nil // repo chưa init hoặc cấu hình lỗi: giữ tỉ lệ mặc định (lỗi cấu hình đã hiện ở Tổng quan/Hệ thống)
		}
		return layoutLoadedMsg{layout: l}
	}
}

// applyLayout đặt tỉ lệ đã lưu cho từng màn (layoutReady: đã đọc cấu hình, kể cả khi không có tỉ lệ nào).
func (m *Model) applyLayout(l map[string]int) {
	m.layoutReady = true
	for s, sc := range m.screens {
		sp, ok := sc.(ui.Splitter)
		if !ok {
			continue
		}
		if p, ok := l[string(s)]; ok {
			sp.Splitter().Set(p)
		}
	}
}

// splitKey xử lý phím chia pane của màn hiện tại; false nếu màn không có hai pane.
func (m *Model) splitKey(s string) (tea.Cmd, bool) {
	sp := m.splitter()
	if sp == nil {
		return nil, false
	}
	changed := false
	switch s {
	case ">":
		sp.Zoom = false
		changed = sp.Grow()
	case "<":
		sp.Zoom = false
		changed = sp.Shrink()
	case "=":
		sp.Reset()
		changed = true
	case "z":
		sp.ToggleZoom()
		return nil, true
	}
	if !changed {
		return nil, true
	}
	return m.scheduleLayoutSave(), true
}

// scheduleLayoutSave hẹn ghi tỉ lệ của màn hiện tại sau layoutSaveDelay.
func (m *Model) scheduleLayoutSave() tea.Cmd {
	if m.opts.ReadOnly || m.opts.Agent {
		return nil
	}
	m.layoutSeq++
	if m.layoutDirty == nil {
		m.layoutDirty = map[string]bool{}
	}
	m.layoutDirty[string(m.Screen())] = true
	seq := m.layoutSeq
	return tea.Tick(layoutSaveDelay, func(time.Time) tea.Msg { return layoutSaveMsg{seq: seq} })
}

// saveLayout ghi tỉ lệ của các màn đã đổi (lệnh nền).
func (m *Model) saveLayout(seq int) tea.Cmd {
	if seq != m.layoutSeq || len(m.layoutDirty) == 0 {
		return nil
	}
	want := map[string]int{}
	for s := range m.layoutDirty {
		if sp, ok := m.screens[actionsScreen(s)].(ui.Splitter); ok {
			want[s] = sp.Splitter().Percent()
		}
	}
	m.layoutDirty = nil
	dir := m.opts.Dir
	return func() tea.Msg {
		for s, p := range want {
			if _, err := bintapp.SetUILayout(context.Background(), dir, s, p); err != nil {
				return layoutSavedMsg{err: err}
			}
		}
		return layoutSavedMsg{}
	}
}

// splitPress bắt đầu kéo đường chia khi nhấn trúng nó (cột viền phải pane trái hoặc viền trái pane phải, trong vùng nội dung).
func (m *Model) splitPress(mo tea.Mouse) bool {
	sp := m.splitter()
	if sp == nil || m.modalOpen() {
		return false
	}
	col, ok := sp.Divider()
	_, bh := m.bodySize()
	if !ok || mo.Y < 1 || mo.Y > bh || (mo.X != col && mo.X != col+1) {
		return false
	}
	m.dragSplit = true
	return true
}

// splitMotion dời đường chia theo chuột khi đang kéo.
func (m *Model) splitMotion(mo tea.Mouse) bool {
	if !m.dragSplit {
		return false
	}
	if sp := m.splitter(); sp != nil {
		sp.DragTo(mo.X)
	}
	return true
}

// splitRelease kết thúc kéo và hẹn ghi tỉ lệ.
func (m *Model) splitRelease(mo tea.Mouse) (tea.Cmd, bool) {
	if !m.dragSplit {
		return nil, false
	}
	m.dragSplit = false
	if sp := m.splitter(); sp != nil {
		sp.DragTo(mo.X)
	}
	return m.scheduleLayoutSave(), true
}

// actionsScreen đổi tên màn (khoá ui.layout) sang actions.Screen.
func actionsScreen(s string) actions.Screen { return actions.Screen(s) }
