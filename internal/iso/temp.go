package iso

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// tempDirIn là thư mục TMPDIR của profile: <temp thật>/bint-iso-<uid>/<tool>--<name> (Windows: bint-iso,
// vì %TEMP% đã riêng theo người dùng). base là temp của máy thật (tempBase).
func (p Profile) tempDirIn(base string) string {
	return filepath.Join(base, tempParentName(), dirName(p.Tool, p.Name))
}

// TempDir trả thư mục temp của profile theo temp thật của máy (đang trong profile thì dùng bản REAL của
// TMPDIR, không phải temp của chính profile). Không tạo thư mục.
func (p Profile) TempDir() string { return p.tempDirFrom(os.Environ()) }

// tempDirFrom như TempDir nhưng theo env cho trước (đã hoặc chưa ở trong profile).
func (p Profile) tempDirFrom(env []string) string {
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	return p.tempDirIn(tempBase(e))
}

// EnsureTemp tạo thư mục temp của profile (0700) và trả đường dẫn. Thư mục cha bint-iso-<uid> chung của mọi
// profile nằm trong /tmp có thể bị người khác tạo sẵn, nên trên Unix chỉ nhận khi nó là thư mục thật (không
// phải symlink), thuộc người dùng hiện tại và không cho nhóm/người khác quyền nào; ngược lại báo lỗi, không
// chmod hay xoá. Phải gọi mỗi lần `run`: Codex không tự tạo lại TMPDIR (S3, docs/iso-plan.md mục 11.3).
func (p Profile) EnsureTemp() (string, error) {
	dir := p.TempDir()
	if err := ensureTempDir(dir); err != nil {
		return "", fmt.Errorf("thư mục temp của profile %s: %w", p.ID(), err)
	}
	return dir, nil
}
