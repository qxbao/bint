package state

import (
	"path/filepath"
	"runtime"
	"strings"
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
	base := filepath.Base(abs)
	cur, rest := filepath.Dir(abs), ""
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			cur = r
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
	out := filepath.ToSlash(filepath.Clean(filepath.Join(cur, rest, base)))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		out = strings.ToLower(out)
	}
	return out
}
