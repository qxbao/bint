//go:build !windows

package workflow

import (
	"errors"
	"os"
	"syscall"
)

// tryFlock xin khoá không chờ trên tệp (chia sẻ hoặc độc quyền); busy=true nếu đang có chủ
// khác giữ khoá không tương thích. Khoá gắn với mô tả tệp đang mở nên hệ điều hành tự nhả
// khi tiến trình chết.
func tryFlock(f *os.File, mode LockMode) (busy bool, err error) {
	how := syscall.LOCK_EX
	if mode == LockShared {
		how = syscall.LOCK_SH
	}
	for {
		err = syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return true, nil
		}
		return false, err
	}
}

// sameFile cho biết đường dẫn p vẫn là đúng tệp f đang mở (chưa bị xoá/thay). Bản hiện tại
// không xoá tệp khoá, nhưng bản cũ (khoá độc quyền) có xoá khi trả nên vẫn kiểm.
func sameFile(f *os.File, p string) bool {
	a, err := f.Stat()
	if err != nil {
		return false
	}
	b, err := os.Stat(p)
	return err == nil && os.SameFile(a, b)
}
