package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/list"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
)

// ToastLabel dựng nhãn thông báo cho thanh phụ: " nội dung " tô màu (ok, hoặc warn khi isErr). Rộng hơn room thì rút gọn bằng "…"
// kèm dấu "[!]" (phím ! hoặc bấm vào đó mở hộp Thông báo hiện nguyên văn): thông báo bị cắt luôn có dấu báo và có cách đọc đủ.
func ToastLabel(th theme.Theme, toast string, isErr bool, room int) string {
	if toast == "" || room <= 0 {
		return ""
	}
	role := theme.OK
	if isErr {
		role = theme.Warn
		if !th.Colored() {
			toast = "✗ " + toast // NO_COLOR: lỗi khác thông báo thường bằng chữ, không chỉ bằng màu
		}
	}
	full := " " + toast + " "
	if text.Width(full) <= room {
		return th.S(role, full)
	}
	const tail = " [!] "
	tw := text.Width(tail)
	if room <= tw+2 {
		return zone.Mark("key:!", th.S(theme.Key, text.Truncate(tail, room)))
	}
	return zone.Mark("key:!", th.S(role, " "+text.Truncate(toast, room-tw-1))+th.S(theme.Key, tail))
}

// SubZone là id vùng bấm của thẻ phụ thứ i ("t:<i>"); ParseSubZone là phép ngược.
func SubZone(i int) string { return "t:" + strconv.Itoa(i) }

// ParseSubZone trả chỉ số thẻ phụ của id vùng "t:<i>".
func ParseSubZone(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "t:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	return i, err == nil && i >= 0
}

// PaneZone: tên khung của id vùng "p:<pane>".
func ParsePane(id string) (string, bool) { return strings.CutPrefix(id, "p:") }

// MarkSub bọc nhãn thẻ phụ thứ i bằng vùng bấm.
func MarkSub(i int, s string) string { return zone.Mark(SubZone(i), s) }

// RowClick xử lý bấm hàng i của danh sách l: hàng khác con trỏ thì dời con trỏ tới đó (onMove trả việc kế tiếp, ví dụ nạp chi
// tiết); đúng hàng đang chọn thì làm như Enter (enter).
func RowClick(l *list.Model, i int, onMove func() tea.Cmd, enter func() tea.Cmd) tea.Cmd {
	if i < 0 || i >= l.N {
		return nil
	}
	if i == l.Cursor {
		return enter()
	}
	l.MoveTo(i)
	return onMove()
}

// EnterKey là phím Enter.
func EnterKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
