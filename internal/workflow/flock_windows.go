//go:build windows

package workflow

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryFlock xin khoá độc quyền không chờ (LockFileEx) trên tệp; busy=true nếu đang có
// chủ khác giữ. Khoá được hệ điều hành nhả khi tiến trình chết.
func tryFlock(f *os.File) (busy bool, err error) {
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return true, nil
	}
	return false, err
}

// Windows không xoá được tệp đang mở và không bao giờ xoá tệp khoá, nên đường dẫn
// luôn là đúng tệp.
func sameFile(*os.File, string) bool { return true }

// removeLockFile không làm gì trên Windows: tệp khoá được giữ lại, chỉ nhả khoá.
func removeLockFile(string) {}
