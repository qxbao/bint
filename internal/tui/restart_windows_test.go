//go:build windows

package tui

import "testing"

// Windows không exec: restartSelf chỉ trả lỗi và RestartSupported = false (TUI hiện hộp "đóng và mở lại bint").
func TestRestartNotSupportedOnWindows(t *testing.T) {
	if RestartSupported {
		t.Fatal("Windows không hỗ trợ khởi động lại tự động")
	}
	if err := restartSelf(`C:\bin\bint.exe`, []string{"bint"}, nil); err == nil {
		t.Fatal("restartSelf phải trả lỗi trên Windows")
	}
}
