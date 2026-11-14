//go:build windows

package workflow

import (
	"errors"
	"io/fs"
	"os"
)

// removeHeldLockFile: Windows không xoá được tệp còn handle mở, nên nhả khoá rồi mới xoá. Nếu người khác kịp
// lấy khoá giữa hai bước thì handle của họ làm Remove lỗi chia sẻ: để lại tệp (doctor dọn sau), không phải lỗi.
func removeHeldLockFile(l *Lock, p string) error {
	l.Release()
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		if _, serr := os.Stat(p); serr == nil {
			return nil // còn tệp vì đang có người giữ
		}
		return err
	}
	return nil
}
