//go:build !windows

package workflow

import (
	"errors"
	"io/fs"
	"os"
)

// removeHeldLockFile xoá tệp khoá p khi l vẫn đang giữ nó (Unix xoá được tệp còn mở); l do người gọi nhả.
func removeHeldLockFile(l *Lock, p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
