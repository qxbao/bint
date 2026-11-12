//go:build !windows

package mcpgw

import (
	"errors"
	"os"
	"syscall"
)

// tryLock xin khoá độc quyền không chờ trên tệp; busy=true nếu daemon khác đang giữ. Khoá gắn với mô tả tệp đang mở
// nên hệ điều hành tự nhả khi tiến trình chết (mẫu của internal/workflow/flock_unix.go, không chép kiểu PID-file).
func tryLock(f *os.File) (busy bool, err error) {
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
}
