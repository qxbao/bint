//go:build !windows

package config

import (
	"fmt"
	"os"
)

// SecretsPermProblem báo khi tệp credential cho phép nhóm hoặc người khác truy cập (rộng hơn 0600); rỗng nếu ổn.
func SecretsPermProblem(fi os.FileInfo) string {
	if m := fi.Mode().Perm(); m&0o077 != 0 {
		return fmt.Sprintf("quyền %04o rộng hơn 0600", uint32(m))
	}
	return ""
}
