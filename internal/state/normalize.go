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
	return NormalizeWritePaths([]string{p})[0]
}

// NormalizeWritePaths như NormalizeWritePath cho nhiều đường dẫn, giải symlink mỗi thư mục cha một lần
// (mỗi lần giải trên Windows stat từng thành phần đường dẫn).
func NormalizeWritePaths(ps []string) []string {
	dirs := map[string]string{}
	out := make([]string, len(ps))
	for i, p := range ps {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		abs = filepath.Clean(abs)
		d := filepath.Dir(abs)
		dir, ok := dirs[d]
		if !ok {
			dir = pathutil.BestEffort(d)
			dirs[d] = dir
		}
		o := filepath.ToSlash(filepath.Clean(filepath.Join(dir, filepath.Base(abs))))
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			o = strings.ToLower(o)
		}
		out[i] = o
	}
	return out
}
