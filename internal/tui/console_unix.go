//go:build !windows

package tui

import (
	"os"

	"golang.org/x/term"
)

// saveConsole chụp trạng thái terminal của đầu vào trước khi mở TUI; hàm trả về đặt lại đúng trạng thái đó (tiến trình con chạy nền
// lúc TUI mở có thể ghi đè sau khi Bubble Tea đã trả lại terminal).
func saveConsole(in, _ *os.File) func() {
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return func() {}
	}
	st, err := term.GetState(int(in.Fd()))
	if err != nil {
		return func() {}
	}
	return func() { _ = term.Restore(int(in.Fd()), st) }
}
