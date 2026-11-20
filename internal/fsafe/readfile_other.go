//go:build !windows

package fsafe

import "os"

// ReadFile là os.ReadFile: ngoài Windows, đọc không vướng lần ghi nguyên tử đang thay tệp (xem readfile_windows.go).
func ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
