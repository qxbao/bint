//go:build !windows

package eval

import "syscall"

// freeBytes trả về số byte trống (cho người dùng thường) của ổ chứa path.
func freeBytes(path string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
