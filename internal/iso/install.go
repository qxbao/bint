package iso

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Bản cài harness (docs/iso-plan.md mục 5.4). Claude: bint không bao giờ chạy `claude update`, và Env đặt
// DISABLE_UPDATES nên không có gì để chia sẻ. Codex: daemon app-server tải ~430 MB vào <CODEX_HOME>/packages
// khi `codex app-server daemon start` thấy thư mục trống (S2), nên packages của profile là liên kết tới bản
// thật khi bản thật có sẵn.

// ShareCodexPackages là tên "mục chia sẻ" của packages Codex trong ShareResult (không nằm trong iso.share:
// luôn bật, vì tải lại bản cài vào mọi profile là lỗi chứ không phải lựa chọn).
const ShareCodexPackages = "codex-packages"

// realCodexPackages trả thư mục packages của Codex thật (CODEX_HOME thật, mặc định <home thật>/.codex) và
// cho biết bản cài thật đã có (app-server-daemon/current tồn tại).
func realCodexPackages(e *envList) (dir string, ok bool, err error) {
	codexHome, err := realCodexHomeOf(e)
	if err != nil {
		return "", false, err
	}
	dir = filepath.Join(codexHome, "packages")
	if _, err := os.Stat(filepath.Join(dir, "app-server-daemon", "current")); err != nil {
		return dir, false, nil
	}
	return dir, true, nil
}

// codexPackagesPath là đường dẫn packages trong HOME giả.
func (p Profile) codexPackagesPath() string {
	return filepath.Join(p.Home, p.Tool.spec().ConfigDir, "packages")
}

// EnsureInstall liên kết home/.codex/packages tới packages của Codex thật khi trong đó có
// app-server-daemon/current (create và run gọi). Không có bản thật thì không tạo gì; thư mục packages có dữ
// liệu hoặc liên kết khác thì giữ nguyên và báo qua State.Conflict(). Tool khác Codex: không làm gì.
func EnsureInstall(p Profile, env []string) ([]ShareResult, error) {
	return ensureInstall(runtime.GOOS, p, env)
}

func ensureInstall(goos string, p Profile, env []string) ([]ShareResult, error) {
	if p.Tool != Codex {
		return nil, nil
	}
	e := newEnvList(goos, env)
	restoreReal(e)
	target, ok, err := realCodexPackages(e)
	if err != nil {
		return nil, err
	}
	if !ok || within(target, p.StoreRoot) {
		return nil, nil
	}
	link := p.codexPackagesPath()
	st, err := EnsureLink(link, target)
	if err != nil {
		return nil, fmt.Errorf("share %s: %w", ShareCodexPackages, err)
	}
	return []ShareResult{{Share: ShareCodexPackages, Link: link, Target: target, State: st}}, nil
}

// InstallIssues trả cảnh báo về bản cài harness của profile, chỉ đọc (cho status/du).
func InstallIssues(p Profile, env []string) []string {
	return installIssues(runtime.GOOS, p, env)
}

func installIssues(goos string, p Profile, env []string) []string {
	if p.Tool != Codex {
		return nil
	}
	e := newEnvList(goos, env)
	restoreReal(e)
	target, ok, err := realCodexPackages(e)
	if err != nil {
		return []string{fmt.Sprintf("không xác định được bản cài Codex thật: %v", err)}
	}
	link := p.codexPackagesPath()
	if !ok {
		isl, _ := linkOrMissing(link)
		if isl {
			return nil
		}
		if fi, err := os.Lstat(link); err == nil && fi.IsDir() {
			if ents, _ := os.ReadDir(link); len(ents) > 0 {
				return []string{fmt.Sprintf("%s là bản cài riêng của profile (không có bản Codex thật ở %s để liên kết); bint iso gc --deep dọn bản thừa", link, target)}
			}
		}
		return []string{fmt.Sprintf("không có bản cài Codex thật ở %s/app-server-daemon/current: `codex app-server daemon start` trong profile sẽ tải lại ~430 MB vào %s", target, link)}
	}
	st, err := InspectLink(link, target)
	if err != nil {
		return []string{err.Error()}
	}
	switch st {
	case LinkOK:
		return nil
	case LinkCreated, LinkReplacedEmpty:
		return []string{fmt.Sprintf("%s chưa liên kết tới %s (bint iso run sẽ tạo)", link, target)}
	default:
		return []string{fmt.Sprintf("%s giữ nguyên (%s): không liên kết tới %s; daemon trong profile giữ bản cài riêng", link, st, target)}
	}
}

// linkOrMissing: path là liên kết (isLink) — lỗi/không có thì false.
func linkOrMissing(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		return false, err
	}
	return isLink(path)
}
