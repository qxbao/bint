//go:build windows

package iso

import "os"

// %TEMP% trên Windows đã riêng theo người dùng nên không cần uid trong tên.
func tempParentName() string { return "bint-iso" }

func ensureTempDir(dir string) error { return os.MkdirAll(dir, 0o700) }
