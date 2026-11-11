//go:build windows

package tui

import "errors"

// RestartSupported: Windows không có exec thay tiến trình; TUI chỉ yêu cầu người dùng đóng và mở lại bint.
const RestartSupported = false

// restartSelf không hỗ trợ trên Windows (cùng chữ ký với bản Unix).
func restartSelf(path string, args, env []string) error {
	return errors.New("automatic restart is not available on Windows: close and reopen bint")
}
