package iso

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Biến báo "đang trong profile" và tiền tố của bản lưu giá trị thật.
const (
	EnvProfile    = "BINT_ISO_PROFILE" // <tool>/<name>
	EnvHome       = "BINT_ISO_HOME"    // gốc store
	EnvRealHome   = "BINT_ISO_REAL_HOME"
	realPrefix    = "BINT_ISO_REAL_"
	envStateDir   = "BINT_STATE_DIR"
	envConfigHome = "BINT_CONFIG_HOME"
)

// pathVars là các biến đường dẫn mà iso đổi sang giá trị của profile; giá trị thật được ghi vào
// BINT_ISO_REAL_<TÊN> để RealEnv khôi phục (mục 5.2). Biến không đặt ở máy thật thì không có bản REAL
// và RealEnv gỡ nó đi.
var pathVars = []string{
	"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"TMPDIR", "TMP", "TEMP",
}

// shareEnvVars là biến cache của các mục share (khớp Env của danh mục ở share.go; test buộc khớp).
var shareEnvVars = []string{"npm_config_cache", "npm_config_store_dir", "GOMODCACHE", "BUN_INSTALL_CACHE_DIR"}

// ownedVars là mọi biến mà iso.Env có thể đổi/đặt và có bản REAL: RealEnv trả lại giá trị của máy thật.
func ownedVars() []string {
	out := append([]string{}, pathVars...)
	out = append(out, "CODEX_HOME", "CLAUDE_CONFIG_DIR", "DISABLE_UPDATES", "DISABLE_AUTOUPDATER")
	return append(out, shareEnvVars...)
}

func inProfile(e *envList) bool { return e.val(EnvProfile) != "" }

// InProfile cho biết env (dạng KEY=VALUE) thuộc một tiến trình chạy trong profile.
func InProfile(env []string) bool { return inProfile(newEnvList(runtime.GOOS, env)) }

// restoreReal đưa env trong profile về env của máy thật: biến sở hữu lấy lại từ BINT_ISO_REAL_<TÊN>
// (không có bản REAL thì gỡ), gỡ mọi BINT_ISO_REAL_* và BINT_ISO_PROFILE. Env ngoài profile giữ nguyên.
func restoreReal(e *envList) {
	if !inProfile(e) {
		return
	}
	for _, v := range ownedVars() {
		if real := e.val(realPrefix + v); real != "" {
			e.set(v, real)
		} else {
			e.del(v)
		}
	}
	e.delPrefix(realPrefix)
	e.del(EnvProfile)
}

// RealEnv trả env của máy thật: nếu base đang trong profile thì khôi phục HOME, USERPROFILE, APPDATA,
// LOCALAPPDATA, XDG_*, TMPDIR/TMP/TEMP (cùng CODEX_HOME, CLAUDE_CONFIG_DIR, biến cache) về giá trị trước khi
// vào profile; ngoài profile thì giữ nguyên. Biến credential mà Env đã gỡ không khôi phục được (cố ý).
func RealEnv(base []string) []string {
	e := newEnvList(runtime.GOOS, base)
	restoreReal(e)
	return e.list()
}

// RealHome trả thư mục home thật: BINT_ISO_REAL_HOME khi đang trong profile, ngược lại os.UserHomeDir().
func RealHome() (string, error) {
	if !InProfile(os.Environ()) {
		return os.UserHomeDir()
	}
	return realHomeOf(newEnvList(runtime.GOOS, RealEnv(os.Environ())))
}

// realHomeOf lấy home từ env (đã là env thật): USERPROFILE trên Windows, HOME nơi khác.
func realHomeOf(e *envList) (string, error) {
	k := "HOME"
	if e.goos == "windows" {
		k = "USERPROFILE"
	}
	h := e.val(k)
	if h == "" {
		return "", errors.New("không xác định được thư mục home thật (thiếu " + k + " hoặc " + realPrefix + k + ")")
	}
	return h, nil
}

// tempBase là thư mục temp của máy thật theo cách os.TempDir chọn: $TMPDIR hoặc /tmp; Windows: TMP, TEMP,
// USERPROFILE, cuối cùng thư mục Windows. e phải là env thật.
func tempBase(e *envList) string {
	if e.goos == "windows" {
		for _, k := range []string{"TMP", "TEMP", "USERPROFILE"} {
			if v := e.val(k); v != "" {
				return v
			}
		}
		return `C:\Windows`
	}
	if v := e.val("TMPDIR"); v != "" {
		return v
	}
	return "/tmp"
}

// realCodexHomeOf là CODEX_HOME thật (tuyệt đối) hoặc <home thật>/.codex. e phải là env thật.
func realCodexHomeOf(e *envList) (string, error) {
	if d := e.val("CODEX_HOME"); d != "" && filepath.IsAbs(d) {
		return d, nil
	}
	home, err := realHomeOf(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// realClaudeDirOf là CLAUDE_CONFIG_DIR thật (tuyệt đối) hoặc <home thật>/.claude. e phải là env thật.
func realClaudeDirOf(e *envList) (string, error) {
	if d := e.val("CLAUDE_CONFIG_DIR"); d != "" && filepath.IsAbs(d) {
		return d, nil
	}
	home, err := realHomeOf(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func realEnvList(env []string) *envList {
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	return e
}

// RealCodexHome là thư mục cấu hình Codex của máy thật (CODEX_HOME thật hoặc ~/.codex) tính từ env
// (đang trong profile thì khôi phục về env thật trước).
func RealCodexHome(env []string) (string, error) { return realCodexHomeOf(realEnvList(env)) }

// RealClaudeDir là thư mục cấu hình Claude của máy thật (CLAUDE_CONFIG_DIR thật hoặc ~/.claude).
func RealClaudeDir(env []string) (string, error) { return realClaudeDirOf(realEnvList(env)) }

// RealClaudeJSON là .claude.json mức user của Claude thật: $CLAUDE_CONFIG_DIR/.claude.json nếu có biến,
// nếu không <home thật>/.claude.json.
func RealClaudeJSON(env []string) (string, error) {
	e := realEnvList(env)
	if d := e.val("CLAUDE_CONFIG_DIR"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, ".claude.json"), nil
	}
	home, err := realHomeOf(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// RealHomeOf là thư mục home của máy thật tính từ env.
func RealHomeOf(env []string) (string, error) { return realHomeOf(realEnvList(env)) }
