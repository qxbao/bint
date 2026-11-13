//go:build windows

package fslock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Khoá một byte ở offset lớn cố định, không phải byte 0: khoá vùng trên Windows là bắt
// buộc (mandatory), khoá chia sẻ ở byte 0 làm chính chủ không ghi được tệp, khoá độc quyền
// làm người khác không đọc được nội dung. Byte ở offset này nằm ngoài nội dung tệp.
const lockOffsetHigh = 0x7fffffff

func tryLock(f *os.File, exclusive bool) (busy bool, err error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return true, nil
	}
	return false, err
}
