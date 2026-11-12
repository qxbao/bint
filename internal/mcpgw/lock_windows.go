//go:build windows

package mcpgw

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Khoá một byte ở offset lớn cố định, không phải byte 0 (cùng lý do với internal/workflow/flock_windows.go: khoá
// vùng trên Windows là bắt buộc nên không đặt ở nội dung tệp).
const lockOffsetHigh = 0x7fffffff

// tryLock xin khoá độc quyền không chờ (LockFileEx); busy=true nếu daemon khác đang giữ. Hệ điều hành nhả khoá khi
// tiến trình chết.
func tryLock(f *os.File) (busy bool, err error) {
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_FAIL_IMMEDIATELY|windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return true, nil
	}
	return false, err
}
