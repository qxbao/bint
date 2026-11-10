//go:build !windows

package workflow

import (
	"errors"
	"os"
	"syscall"
)

// tryFlock xin khoá độc quyền không chờ trên tệp; busy=true nếu đang có chủ khác giữ.
// Khoá gắn với mô tả tệp đang mở nên hệ điều hành tự nhả khi tiến trình chết.
func tryFlock(f *os.File) (busy bool, err error) {
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

// sameFile cho biết đường dẫn p vẫn là đúng tệp f đang mở (chưa bị xoá/thay).
func sameFile(f *os.File, p string) bool {
	a, err := f.Stat()
	if err != nil {
		return false
	}
	b, err := os.Stat(p)
	return err == nil && os.SameFile(a, b)
}

// removeLockFile xoá tệp khoá khi còn giữ khoá (trên Unix được phép xoá tệp đang mở).
func removeLockFile(path string) { os.Remove(path) }
