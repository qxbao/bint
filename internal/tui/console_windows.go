//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// saveConsole chụp chế độ console của đầu vào và đầu ra trước khi mở TUI; hàm trả về đặt lại đúng chế độ đó. Bubble Tea tự trả lại
// chế độ khi thoát, nhưng tiến trình con chạy nền lúc TUI mở (doctor gọi claude/codex/git…) có thể ghi đè sau đó: thiếu bước này
// terminal bị kẹt ở chế độ của TUI (vd DISABLE_NEWLINE_AUTO_RETURN: xuống dòng không về đầu dòng, phải gõ reset).
func saveConsole(in, out *os.File) func() {
	type saved struct {
		h    windows.Handle
		mode uint32
	}
	var all []saved
	for _, f := range []*os.File{in, out} {
		if f == nil {
			continue
		}
		h := windows.Handle(f.Fd())
		var mode uint32
		if windows.GetConsoleMode(h, &mode) == nil {
			all = append(all, saved{h, mode})
		}
	}
	return func() {
		for _, s := range all {
			_ = windows.SetConsoleMode(s.h, s.mode)
		}
	}
}
