package state

import (
	"path/filepath"
	"runtime"
	"strings"

	"bint/internal/pathutil"
)

// NormalizeWritePath chuẩn hoá đường dẫn tệp để so khớp giữa nhật ký và kết quả git diff: tuyệt đối, giải
// symlink của thư mục (tới tổ tiên còn tồn tại gần nhất, nên tệp/thư mục đã xoá vẫn cho cùng kết quả),
// dấu '/', không phân biệt hoa thường trên Windows/macOS. Không giải symlink của chính tệp.
func NormalizeWritePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	dir := pathutil.BestEffort(filepath.Dir(abs))
	out := filepath.ToSlash(filepath.Clean(filepath.Join(dir, filepath.Base(abs))))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		out = strings.ToLower(out)
	}
	return out
}
