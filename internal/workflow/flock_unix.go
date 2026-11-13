//go:build !windows

package workflow

import "os"

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
