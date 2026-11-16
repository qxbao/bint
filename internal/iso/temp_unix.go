//go:build !windows

package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func tempParentName() string { return "bint-iso-" + strconv.Itoa(os.Getuid()) }

// ensureTempDir tạo <parent>/<profile> với parent là bint-iso-<uid> đã được kiểm (xem EnsureTemp).
func ensureTempDir(dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(filepath.Dir(parent), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(parent, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := checkPrivateDir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	// Mục con nằm trong thư mục riêng đã kiểm, vẫn Lstat để không đi theo symlink đặt sẵn.
	return checkPrivateDir(dir)
}

// checkPrivateDir: thư mục thật (Lstat, không theo symlink), của người dùng hiện tại, mode không có bit
// nhóm/người khác.
func checkPrivateDir(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s không phải thư mục thường (symlink hoặc tệp)", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: không đọc được chủ sở hữu", path)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s thuộc uid %d, không phải uid %d của bạn", path, st.Uid, os.Getuid())
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s có quyền %o, cần 0700", path, fi.Mode().Perm())
	}
	return nil
}
