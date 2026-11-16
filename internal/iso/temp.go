package iso

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"bint/internal/state"
)

// StoreKey là khoá ngắn (10 hex) của gốc store, đứng giữa tên thư mục cha và thư mục profile để hai store
// (BINT_ISO_HOME / iso.home khác nhau) có cùng codex/default không dùng chung TMPDIR: khoá phiên session.lock
// nằm trong từng store, nên TMPDIR dùng chung sẽ bị phiên cuối của store này xoá dưới chân phiên của store
// kia. Chuẩn hoá đường dẫn trước khi băm để alias symlink hay khác hoa/thường (macOS/Windows) ra cùng khoá.
func StoreKey(root string) string {
	sum := sha256.Sum256([]byte(state.NormalizePath(root)))
	return hex.EncodeToString(sum[:])[:10]
}

// tempDirIn là thư mục TMPDIR của profile: <temp thật>/bint-iso-<uid>/<StoreKey>/<tool>--<name> (Windows:
// bint-iso, vì %TEMP% đã riêng theo người dùng). base là temp của máy thật (tempBase).
func (p Profile) tempDirIn(base string) string {
	return filepath.Join(p.storeTempDirIn(base), dirName(p.Tool, p.Name))
}

// storeTempDirIn là thư mục chứa temp của mọi profile thuộc cùng store với p.
func (p Profile) storeTempDirIn(base string) string {
	return filepath.Join(base, tempParentName(), StoreKey(p.StoreRoot))
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
// store nằm trong /tmp có thể bị người khác tạo sẵn, nên trên Unix chỉ nhận khi nó là thư mục thật (không
// phải symlink), thuộc người dùng hiện tại và không cho nhóm/người khác quyền nào; ngược lại báo lỗi, không
// chmod hay xoá. Phải gọi mỗi lần `run`: Codex không tự tạo lại TMPDIR (S3, docs/iso-plan.md mục 11.3).
func (p Profile) EnsureTemp() (string, error) {
	dir := p.TempDir()
	if err := ensureTempDir(dir); err != nil {
		return "", fmt.Errorf("thư mục temp của profile %s: %w", p.ID(), err)
	}
	return dir, nil
}
