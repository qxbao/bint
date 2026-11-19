//go:build !windows

package fsafe

import "os"

// rename là os.Rename: ngoài Windows, đổi tên đè lên tệp đang mở không lỗi (xem rename_windows.go).
func rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
