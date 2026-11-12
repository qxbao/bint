//go:build windows

package workflow

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Khoá một byte ở offset lớn cố định, không phải byte 0: khoá vùng trên Windows là bắt
// buộc (mandatory), khoá chia sẻ ở byte 0 làm chính chủ không ghi được tệp, khoá độc quyền
// làm người khác không đọc được nội dung. Byte ở offset này nằm ngoài nội dung tệp.
const lockOffsetHigh = 0x7fffffff

// tryFlock xin khoá không chờ (LockFileEx), chia sẻ hoặc độc quyền; busy=true nếu đang có
// chủ khác giữ khoá không tương thích. Khoá được hệ điều hành nhả khi tiến trình chết.
func tryFlock(f *os.File, mode LockMode) (busy bool, err error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if mode == LockExclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return true, nil
	}
	return false, err
}

// Windows không xoá được tệp đang mở và không bao giờ xoá tệp khoá, nên đường dẫn
// luôn là đúng tệp.
func sameFile(*os.File, string) bool { return true }
