package app

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/components/input"
	"bint/internal/tui/ui"
)

// Dán (tea.PasteMsg và Ctrl+V). Chữ dán đi đường riêng tới ô nhập (ui.Paster, Paste của component), không giả lập thành
// phím: nếu không, chuỗi dán đúng bằng "q", "esc", "enter" sẽ bị hiểu thành phím tắt. Không có ô nhập nào nhận phím thì
// chữ dán bị bỏ.

// clipMsg là kết quả đọc clipboard hệ thống sau Ctrl+V.
type clipMsg struct {
	text string
	err  error
}

// pasteScreen trả màn hình hiện tại nếu nó đang nhận phím ở ô nhập và nhận được chữ dán.
func (m *Model) pasteScreen() (ui.Paster, bool) {
	sc := m.screens[m.Screen()]
	if sc == nil || !sc.Capturing() {
		return nil, false
	}
	p, ok := sc.(ui.Paster)
	return p, ok
}

// pasteTarget cho biết có ô nhập đang nhận phím (cùng thứ tự ưu tiên với key).
func (m *Model) pasteTarget() bool {
	switch {
	case m.notes.dlg != nil, m.exiting, m.exitDlg != nil:
		return false
	case m.prompt != nil:
		return m.prompt.dlg.CanPaste()
	case m.pal != nil:
		return true
	case m.showHelp:
		return false
	}
	_, ok := m.pasteScreen()
	return ok
}

// paste chèn chữ dán vào ô nhập đang nhận phím; không có thì không làm gì.
func (m *Model) paste(s string) (tea.Model, tea.Cmd) {
	switch {
	case m.notes.dlg != nil, m.exiting, m.exitDlg != nil:
		return m, nil
	case m.prompt != nil:
		m.prompt.dlg.Paste(s)
		return m, nil
	case m.pal != nil:
		if s = input.Sanitize(s, false); s != "" {
			m.pal.Type(s)
		}
		return m, nil
	case m.showHelp:
		return m, nil
	}
	if p, ok := m.pasteScreen(); ok {
		return m, tea.Batch(p.Paste(s), m.tickIfBusy())
	}
	return m, nil
}

// readClipboard đọc clipboard hệ thống (ngoài vòng cập nhật) rồi trả clipMsg.
func (m *Model) readClipboard() tea.Cmd {
	clip := m.clip
	return func() tea.Msg {
		t, err := clip.Read()
		return clipMsg{text: t, err: err}
	}
}

// onClip xử lý kết quả Ctrl+V: lỗi thì toast lỗi (ô giữ nguyên), còn lại dán như tea.PasteMsg.
func (m *Model) onClip(msg clipMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setToast(m.cat.T("ui.paste.err", i18n.A{"err": msg.err.Error()}), true)
		return m, nil
	}
	return m.paste(msg.text)
}
