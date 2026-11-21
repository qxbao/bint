//go:build !windows

package tui

import "os"

// saveCursor: ngoài Windows, chế độ alt screen 1049 của terminal tự khôi phục con trỏ; không cần làm gì.
func saveCursor(_, _ *os.File) func() { return func() {} }
