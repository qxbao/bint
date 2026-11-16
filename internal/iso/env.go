package iso

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"bint/internal/config"
	"bint/internal/state"
)

// stripNames là biến credential/override bị gỡ khỏi env của tiến trình con: danh sách của ai-isolator
// (env -u …) cộng các biến khác cùng loại. Test ghim danh sách này.
var stripNames = []string{
	// ai-isolator
	"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "CODEX_HOME",
	"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
	"AGY_CONFIG_DIR", "AGY_HOME", "AGY_API_KEY", "AGY_AUTH_TOKEN",
	"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS",
	"ANTIGRAVITY_API_KEY", "ANTIGRAVITY_AUTH_TOKEN",
	// thêm ở bint
	"AWS_BEARER_TOKEN_BEDROCK", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CONFIG_DIR", "TMPDIR", "TMP", "TEMP",
	"CODEX_LB_API_KEY", // chỉ chuyển tiếp khi profile linked (hoặc iso.pass_env)
}

// stripPrefixes gỡ cả họ biến.
var stripPrefixes = []string{"XDG_", "GEMINI_", "AGY_", "ANTHROPIC_", realPrefix}

// StripNames và StripPrefixes là bản sao danh sách gỡ mặc định (cho `bint iso status`, tài liệu).
func StripNames() []string    { return append([]string{}, stripNames...) }
func StripPrefixes() []string { return append([]string{}, stripPrefixes...) }

// Env dựng env của tiến trình con chạy trong profile p từ base (dạng KEY=VALUE, thường os.Environ() hoặc
// env sạch của eval). Đây là nơi duy nhất dựng env profile (mục 5.2):
//
//  1. base đang trong một profile (lồng) thì đưa về env thật trước (RealEnv);
//  2. tính giá trị thật: BINT_STATE_DIR, BINT_CONFIG_HOME (tuyệt đối), home/cache/temp thật;
//  3. gỡ biến credential/override (stripNames, stripPrefixes, iso.drop_env) trừ iso.pass_env;
//  4. ghi BINT_ISO_REAL_<VAR> cho mọi biến đường dẫn có giá trị thật;
//  5. đặt HOME/XDG/TMPDIR/CODEX_HOME hoặc CLAUDE_CONFIG_DIR/biến cache/BINT_*.
//
// Lỗi khi không tính được state/config thật (không có home): báo lỗi còn hơn để con rơi về state giả.
func Env(p Profile, base []string) ([]string, error) { return buildEnv(runtime.GOOS, p, base) }

func buildEnv(goos string, p Profile, base []string) ([]string, error) {
	e := newEnvList(goos, base)
	restoreReal(e)

	// (2) giá trị thật, tính trên env thật trước khi sửa gì.
	stateDir, err := state.BaseDirFrom(e.get)
	if err != nil {
		return nil, fmt.Errorf("không ghim được %s: %w", envStateDir, err)
	}
	cfgDir, err := realConfigHome(e)
	if err != nil {
		return nil, err
	}
	realHome, err := realHomeOf(e)
	if err != nil {
		return nil, err
	}
	tmp := p.tempDirIn(tempBase(e))
	realVals := map[string]string{}
	for _, v := range ownedVars() {
		if val := e.val(v); val != "" {
			realVals[v] = val
		}
	}
	shares := p.selected(goos, e)
	lbKey, haveLB := e.get("CODEX_LB_API_KEY")

	// (3) gỡ.
	pass := map[string]bool{}
	for _, v := range p.Settings.PassEnv {
		pass[foldKey(goos, v)] = true
	}
	var del []string
	for _, k := range e.ents {
		if k.dead || pass[foldKey(goos, k.key)] {
			continue
		}
		if stripped(goos, k.key) {
			del = append(del, k.key)
		}
	}
	for _, k := range del {
		e.del(k)
	}
	for _, k := range p.Settings.DropEnv {
		e.del(k)
	}
	// Biến cache của share bị gỡ luôn (cả khi private) rồi mới đặt lại theo share: cache chung của máy
	// không được lọt vào profile private.
	for _, v := range ownedVars() {
		if !pass[foldKey(goos, v)] {
			e.del(v)
		}
	}
	if haveLB && p.Meta.LB.Mode == LBLinked {
		e.set("CODEX_LB_API_KEY", lbKey)
	}

	// (4) ghi bản thật.
	for _, v := range ownedVars() {
		if val, ok := realVals[v]; ok {
			e.set(realPrefix+v, val)
		}
	}

	// (5) đặt.
	home := p.Home
	e.set("HOME", home)
	if goos == "windows" {
		e.set("USERPROFILE", home)
		e.set("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		e.set("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	e.set("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	e.set("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	e.set("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	e.set("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		e.set(k, tmp)
	}
	spec := p.Tool.spec()
	if spec.ConfigEnv != "" {
		e.set(spec.ConfigEnv, filepath.Join(home, spec.ConfigDir))
	}
	if p.Tool == Claude {
		// DISABLE_AUTOUPDATER không chặn `claude update` (S4); DISABLE_UPDATES chặn cả hai đường.
		e.set("DISABLE_UPDATES", "1")
		e.set("DISABLE_AUTOUPDATER", "1")
	}
	for _, s := range shares {
		for _, v := range s.Env {
			e.set(v.Name, v.Value)
		}
	}
	e.set(envStateDir, stateDir)
	e.set(envConfigHome, cfgDir)
	e.set(EnvHome, p.StoreRoot)
	e.set(EnvProfile, p.ID())
	e.set(EnvRealHome, realHome) // Windows không có HOME: lấy USERPROFILE
	return e.list(), nil
}

func stripped(goos, key string) bool {
	k := foldKey(goos, key)
	for _, n := range stripNames {
		if foldKey(goos, n) == k {
			return true
		}
	}
	for _, p := range stripPrefixes {
		if strings.HasPrefix(k, foldKey(goos, p)) {
			return true
		}
	}
	return false
}

// realConfigHome là giá trị BINT_CONFIG_HOME cần ghim: thư mục chứa config.yml toàn cục như
// config.GlobalPath thấy trên env thật (tuyệt đối).
func realConfigHome(e *envList) (string, error) {
	if d := e.val(envConfigHome); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return "", fmt.Errorf("không ghim được %s: %w", envConfigHome, err)
		}
		return abs, nil
	}
	g := config.GlobalPathFrom(e.get)
	if g == "" {
		return "", errors.New("không ghim được " + envConfigHome + ": không xác định được thư mục cấu hình người dùng")
	}
	return filepath.Dir(g), nil
}
