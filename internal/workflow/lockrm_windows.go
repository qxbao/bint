//go:build windows

package workflow

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// removeHeldLockFile: Windows không xoá được tệp còn handle mở, nên nhả khoá rồi mới xoá. Nếu người khác kịp
// lấy khoá giữa hai bước thì handle của họ làm Remove lỗi chia sẻ (ERROR_SHARING_VIOLATION): để lại tệp (doctor
// dọn sau), không phải lỗi. Mọi lỗi khác (quyền, antivirus, đường dẫn là thư mục không rỗng...) được trả về.
func removeHeldLockFile(l *Lock, p string) error {
	l.Release()
	err := os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, errSharingViolation) {
		return nil
	}
	return err
}

// errSharingViolation là ERROR_SHARING_VIOLATION (32) của Windows: tệp đang được tiến trình khác mở.
const errSharingViolation = syscall.Errno(32)
