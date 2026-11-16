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

// ensureTempDir tạo <parent>/<StoreKey>/<profile> với parent là bint-iso-<uid>; từng cấp được tạo bằng
// os.Mkdir 0700 và kiểm bằng Lstat (không theo symlink, đúng chủ, không quyền nhóm/người khác) — xem EnsureTemp.
func ensureTempDir(dir string) error {
	store := filepath.Dir(dir)
	parent := filepath.Dir(store)
	if err := os.MkdirAll(filepath.Dir(parent), 0o700); err != nil {
		return err
	}
	for _, d := range []string{parent, store, dir} {
		if err := os.Mkdir(d, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := checkPrivateDir(d); err != nil {
			return err
		}
	}
	return nil
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
