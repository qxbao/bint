//go:build windows

package workflow

import "os"

// Windows không xoá được tệp đang mở và không bao giờ xoá tệp khoá, nên đường dẫn
// luôn là đúng tệp.
func sameFile(*os.File, string) bool { return true }
