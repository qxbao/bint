package iso

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
)

// profileAt dựng Profile trong bộ nhớ (không chạm đĩa) để test bảng env.
func profileAt(root string, tool Tool, name string, set Settings, lb LB) Profile {
	dir := filepath.Join(root, "profiles", dirName(tool, name))
	return Profile{Tool: tool, Name: name, StoreRoot: root, Dir: dir, Home: filepath.Join(dir, "home"),
		Meta: Meta{Version: MetaVersion, Tool: tool, Name: name, LB: lb}, Settings: set}
}

func defaultSettings(t *testing.T, mut func(*config.Iso)) Settings {
	t.Helper()
	c := config.Default().Iso
	if mut != nil {
		mut(&c)
	}
	s, err := SettingsFrom(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func credentialEnv() []string {
	return []string{
		"OPENAI_API_KEY=k1", "CODEX_API_KEY=k2", "CODEX_ACCESS_TOKEN=k3", "ANTHROPIC_API_KEY=k4",
		"CLAUDE_CODE_OAUTH_TOKEN=k5", "ANTHROPIC_AUTH_TOKEN=k6", "ANTHROPIC_BASE_URL=http://x", "ANTHROPIC_MODEL=m",
		"AGY_CONFIG_DIR=/a", "AGY_HOME=/a", "AGY_API_KEY=k7", "AGY_AUTH_TOKEN=k8", "AGY_ANYTHING=1",
		"GEMINI_API_KEY=k9", "GEMINI_ANYTHING=1", "GOOGLE_API_KEY=k10", "GOOGLE_APPLICATION_CREDENTIALS=/g",
		"ANTIGRAVITY_API_KEY=k11", "ANTIGRAVITY_AUTH_TOKEN=k12",
		"AWS_BEARER_TOKEN_BEDROCK=k13", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1",
		"CODEX_HOME=/real/codex", "CLAUDE_CONFIG_DIR=/real/claude", "CODEX_LB_API_KEY=lbk",
		"XDG_CONFIG_HOME=/real/xc", "XDG_DATA_HOME=/real/xd", "XDG_STATE_HOME=/real/xs",
		"TMP=/real/tmp", "TEMP=/real/tmp",
	}
}

var credentialKeys = func() []string {
	var ks []string
	for _, s := range credentialEnv() {
		k, _, _ := cutEnv(s)
		ks = append(ks, k)
	}
	return ks
}()

func TestEnvLinuxTable(t *testing.T) {
	r := newFakeReal(t)
	root := filepath.Join(r.Dir, "isoroot")
	base := append(slices.Clone(r.Env), credentialEnv()...)
	base = append(base, "EDITOR=vim", "GOMODCACHE=/real/gomod", "npm_config_cache=/real/npm")
	cases := []struct {
		name    string
		tool    Tool
		set     Settings
		lb      LB
		want    map[string]string
		absent  []string
		present []string // phải còn từ base
	}{
		{
			name: "claude shared", tool: Claude, set: defaultSettings(t, nil),
			want: map[string]string{
				"CLAUDE_CONFIG_DIR": "HOME/.claude", "DISABLE_UPDATES": "1", "DISABLE_AUTOUPDATER": "1",
				"XDG_CACHE_HOME": r.Dir + "/xcache", "GOMODCACHE": "/real/gomod", "npm_config_cache": r.Home + "/.npm",
			},
			absent:  []string{"CODEX_LB_API_KEY"},
			present: []string{"EDITOR", "PATH", "LANG"},
		},
		{
			name: "claude private", tool: Claude, set: defaultSettings(t, func(c *config.Iso) { c.Cache = "private" }),
			want:   map[string]string{"XDG_CACHE_HOME": "HOME/.cache", "DISABLE_UPDATES": "1"},
			absent: []string{"GOMODCACHE", "npm_config_cache", "npm_config_store_dir", "BUN_INSTALL_CACHE_DIR"},
		},
		{
			name: "codex shared", tool: Codex, set: defaultSettings(t, nil),
			want:   map[string]string{"CODEX_HOME": "HOME/.codex", "XDG_CACHE_HOME": r.Dir + "/xcache"},
			absent: []string{"CLAUDE_CONFIG_DIR", "DISABLE_UPDATES", "DISABLE_AUTOUPDATER", "CODEX_LB_API_KEY"},
		},
		{
			name: "share chỉ npm", tool: Codex, set: defaultSettings(t, func(c *config.Iso) { c.Share = []string{"npm"} }),
			want:   map[string]string{"XDG_CACHE_HOME": "HOME/.cache", "npm_config_cache": r.Home + "/.npm"},
			absent: []string{"GOMODCACHE"},
		},
		{
			name: "pass_env và drop_env", tool: Codex,
			set: defaultSettings(t, func(c *config.Iso) {
				c.PassEnv = []string{"OPENAI_API_KEY", "GEMINI_API_KEY"}
				c.DropEnv = []string{"EDITOR"}
			}),
			want:    map[string]string{"OPENAI_API_KEY": "k1", "GEMINI_API_KEY": "k9"},
			absent:  []string{"EDITOR", "ANTHROPIC_API_KEY", "GEMINI_ANYTHING"},
			present: []string{"PATH"},
		},
		{
			name: "LB linked chuyển tiếp key", tool: Codex, set: defaultSettings(t, nil), lb: LB{Mode: LBLinked, URL: "http://127.0.0.1:2455"},
			want: map[string]string{"CODEX_LB_API_KEY": "lbk"},
		},
		{
			name: "LB direct không chuyển key", tool: Codex, set: defaultSettings(t, nil), lb: LB{Mode: LBDirect},
			absent: []string{"CODEX_LB_API_KEY"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := profileAt(root, c.tool, "w", c.set, c.lb)
			env, err := buildEnv("linux", p, base)
			if err != nil {
				t.Fatal(err)
			}
			m := envMap(env)
			// bất biến chung
			fixed := map[string]string{
				"HOME": p.Home, "XDG_CONFIG_HOME": p.Home + "/.config", "XDG_DATA_HOME": p.Home + "/.local/share",
				"XDG_STATE_HOME": p.Home + "/.local/state",
				"BINT_STATE_DIR": r.State, "BINT_CONFIG_HOME": r.Cfg,
				"BINT_ISO_REAL_HOME": r.Home, "BINT_ISO_HOME": root, "BINT_ISO_PROFILE": string(c.tool) + "/w",
				"BINT_ISO_REAL_TMPDIR": r.Tmp, "BINT_ISO_REAL_XDG_CONFIG_HOME": "/real/xc",
			}
			tmp := filepath.Join(r.Tmp, tempParentName(), StoreKey(root), string(c.tool)+"--w")
			for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
				fixed[k] = tmp
			}
			for k, v := range c.want {
				fixed[k] = strings.ReplaceAll(v, "HOME", p.Home)
			}
			for k, v := range fixed {
				if m[k] != v {
					t.Errorf("%s = %q, muốn %q", k, m[k], v)
				}
			}
			for _, k := range append(slices.Clone(credentialKeys), c.absent...) {
				if v, ok := m[k]; ok {
					if _, isWant := fixed[k]; isWant || strings.HasPrefix(v, p.Home) {
						continue // iso tự đặt lại (CODEX_HOME, CLAUDE_CONFIG_DIR...) hoặc được yêu cầu giữ
					}
					t.Errorf("%s phải bị gỡ, còn %q", k, v)
				}
			}
			for _, k := range c.present {
				if _, ok := m[k]; !ok {
					t.Errorf("%s phải còn", k)
				}
			}
		})
	}
}

func TestEnvStripListPinned(t *testing.T) {
	want := []string{
		"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "CODEX_HOME",
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"AGY_CONFIG_DIR", "AGY_HOME", "AGY_API_KEY", "AGY_AUTH_TOKEN",
		"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS",
		"ANTIGRAVITY_API_KEY", "ANTIGRAVITY_AUTH_TOKEN",
		"AWS_BEARER_TOKEN_BEDROCK", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CONFIG_DIR", "TMPDIR", "TMP", "TEMP", "CODEX_LB_API_KEY",
	}
	if !slices.Equal(StripNames(), want) {
		t.Errorf("stripNames đổi:\n got %v\nwant %v", StripNames(), want)
	}
	if !slices.Equal(StripPrefixes(), []string{"GEMINI_", "AGY_", "ANTHROPIC_", "BINT_ISO_REAL_"}) {
		t.Errorf("stripPrefixes đổi: %v", StripPrefixes())
	}
	// Mọi biến trong yêu cầu (phiếu GĐ1) phải bị gỡ.
	r := newFakeReal(t)
	p := profileAt(filepath.Join(r.Dir, "s"), Claude, "w", defaultSettings(t, nil), LB{})
	var base []string
	for _, k := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "AWS_BEARER_TOKEN_BEDROCK", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
		"TMPDIR", "TMP", "TEMP", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "GEMINI_X", "AGY_X", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		base = append(base, k+"=secret")
	}
	env, err := buildEnv("linux", p, append(base, r.Env...))
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	for _, s := range base {
		k, _, _ := cutEnv(s)
		switch k {
		case "TMPDIR", "TMP", "TEMP", "CLAUDE_CONFIG_DIR":
			if m[k] == "secret" {
				t.Errorf("%s còn giá trị cũ", k)
			}
		default:
			if _, ok := m[k]; ok {
				t.Errorf("%s phải bị gỡ", k)
			}
		}
	}
}

func TestEnvNestedProfile(t *testing.T) {
	r := newFakeReal(t)
	root := filepath.Join(r.Dir, "s")
	set := defaultSettings(t, nil)
	a := profileAt(root, Codex, "outer", set, LB{})
	b := profileAt(root, Claude, "inner", set, LB{})
	base := append(slices.Clone(r.Env), "XDG_DATA_HOME=/real/xd", "GOMODCACHE=/real/gomod")
	envA, err := buildEnv("linux", a, base)
	if err != nil {
		t.Fatal(err)
	}
	envB, err := buildEnv("linux", b, envA)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(envB)
	if m["HOME"] != b.Home || m["BINT_ISO_PROFILE"] != "claude/inner" {
		t.Errorf("profile trong phải thắng: HOME=%q PROFILE=%q", m["HOME"], m["BINT_ISO_PROFILE"])
	}
	// Giá trị thật là của máy, không phải của profile ngoài.
	for k, v := range map[string]string{
		"BINT_ISO_REAL_HOME": r.Home, "BINT_ISO_REAL_TMPDIR": r.Tmp, "BINT_ISO_REAL_XDG_DATA_HOME": "/real/xd",
		"BINT_ISO_REAL_XDG_CACHE_HOME": r.Dir + "/xcache", "BINT_STATE_DIR": r.State, "BINT_CONFIG_HOME": r.Cfg,
		"GOMODCACHE": "/real/gomod", "BINT_ISO_REAL_GOMODCACHE": "/real/gomod",
	} {
		if m[k] != v {
			t.Errorf("%s = %q, muốn %q", k, m[k], v)
		}
	}
	if strings.Contains(m["BINT_ISO_REAL_HOME"], "profiles") {
		t.Error("REAL_HOME lấy từ profile ngoài")
	}
	// Temp lồng nằm dưới temp thật, không dưới temp của profile ngoài.
	if !strings.HasPrefix(m["TMPDIR"], r.Tmp) {
		t.Errorf("TMPDIR lồng %q không dưới temp thật %q", m["TMPDIR"], r.Tmp)
	}
	for k := range m {
		if strings.HasPrefix(k, "BINT_ISO_REAL_") && strings.Contains(m[k], "profiles") {
			t.Errorf("%s=%q trỏ vào profile", k, m[k])
		}
	}
	// Biến thật của profile ngoài không rò: CODEX_HOME của ngoài bị gỡ, claude đặt CLAUDE_CONFIG_DIR.
	if _, ok := m["CODEX_HOME"]; ok {
		t.Error("CODEX_HOME của profile ngoài còn lại")
	}
}

func TestEnvPinsRelativeStateAndConfig(t *testing.T) {
	r := newFakeReal(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	p := profileAt(filepath.Join(r.Dir, "s"), Codex, "w", defaultSettings(t, nil), LB{})
	env, err := buildEnv("linux", p, []string{"HOME=" + r.Home, "BINT_STATE_DIR=rel/state", "BINT_CONFIG_HOME=rel/cfg"})
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	for k, rel := range map[string]string{"BINT_STATE_DIR": "rel/state", "BINT_CONFIG_HOME": "rel/cfg"} {
		if !filepath.IsAbs(m[k]) || filepath.Base(filepath.Dir(m[k])) != "rel" || !strings.HasSuffix(filepath.ToSlash(m[k]), rel) {
			t.Errorf("%s = %q phải tuyệt đối và kết thúc %q", k, m[k], rel)
		}
	}
}

func TestEnvPinsDefaultsWhenUnset(t *testing.T) {
	r := newFakeReal(t)
	p := profileAt(filepath.Join(r.Dir, "s"), Codex, "w", defaultSettings(t, nil), LB{})
	env, err := buildEnv("linux", p, []string{"HOME=" + r.Home, "XDG_CONFIG_HOME=" + filepath.Join(r.Dir, "xc")})
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	if want := filepath.Join(r.Home, ".local", "state", "bint"); m["BINT_STATE_DIR"] != want {
		t.Errorf("BINT_STATE_DIR = %q, muốn %q", m["BINT_STATE_DIR"], want)
	}
	if want := filepath.Join(r.Dir, "xc", "bint"); m["BINT_CONFIG_HOME"] != want {
		t.Errorf("BINT_CONFIG_HOME = %q, muốn %q", m["BINT_CONFIG_HOME"], want)
	}
	if _, ok := m["XDG_CACHE_HOME"]; !ok {
		t.Error("thiếu XDG_CACHE_HOME")
	}
	// Không có XDG_CACHE_HOME thật: cache thật là ~/.cache.
	if m["XDG_CACHE_HOME"] != filepath.Join(r.Home, ".cache") {
		t.Errorf("XDG_CACHE_HOME = %q", m["XDG_CACHE_HOME"])
	}
	if _, ok := m["BINT_ISO_REAL_XDG_CACHE_HOME"]; ok {
		t.Error("biến thật không đặt thì không có bản REAL")
	}
}

func TestEnvErrorsWithoutHome(t *testing.T) {
	p := profileAt(t.TempDir(), Codex, "w", defaultSettings(t, nil), LB{})
	if _, err := buildEnv("linux", p, []string{"PATH=/bin"}); err == nil {
		t.Error("không có home phải lỗi")
	}
	if _, err := buildEnv("linux", p, []string{"PATH=/bin", "BINT_STATE_DIR=/s", "BINT_CONFIG_HOME=/c"}); err == nil {
		t.Error("không có home thật phải lỗi dù đã ghim state/config")
	}
}

// Nhánh Windows: khoá không phân biệt hoa thường; USERPROFILE/APPDATA/LOCALAPPDATA trỏ vào HOME giả.
func TestEnvWindowsTable(t *testing.T) {
	r := newFakeReal(t)
	root := filepath.Join(r.Dir, "s")
	p := profileAt(root, Claude, "w", defaultSettings(t, nil), LB{})
	base := []string{
		"Path=C:\\bin", "UserProfile=" + r.Home, "AppData=" + r.Home + "\\Roaming", "LocalAppData=" + r.Home + "\\Local",
		"Temp=" + r.Tmp, "Tmp=" + r.Tmp, "Anthropic_Api_Key=secret", "openai_api_key=secret2", "Gemini_Key=s", "Claude_Config_Dir=C:\\x",
		"xdg_cache_home=" + r.Dir + "\\cache", "Disable_Updates=0", "=C:=C:\\work",
		"BINT_STATE_DIR=" + r.State, "Bint_Config_Home=" + r.Cfg,
	}
	env, err := buildEnv("windows", p, base)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	for _, k := range []string{"Anthropic_Api_Key", "openai_api_key", "Gemini_Key", "Claude_Config_Dir", "Disable_Updates", "xdg_cache_home", "Temp", "Tmp", "UserProfile", "AppData", "LocalAppData", "Bint_Config_Home"} {
		if _, ok := m[k]; ok {
			t.Errorf("khoá %q (chữ hoa/thường khác) phải được gỡ và đặt lại dưới tên chuẩn hoặc bỏ", k)
		}
	}
	want := map[string]string{
		"HOME": p.Home, "USERPROFILE": p.Home,
		"APPDATA":           filepath.Join(p.Home, "AppData", "Roaming"),
		"LOCALAPPDATA":      filepath.Join(p.Home, "AppData", "Local"),
		"CLAUDE_CONFIG_DIR": filepath.Join(p.Home, ".claude"), "DISABLE_UPDATES": "1", "DISABLE_AUTOUPDATER": "1",
		"BINT_STATE_DIR": r.State, "BINT_CONFIG_HOME": r.Cfg, "BINT_ISO_REAL_HOME": r.Home,
		"BINT_ISO_REAL_USERPROFILE": r.Home, "BINT_ISO_REAL_TEMP": r.Tmp, "BINT_ISO_REAL_TMP": r.Tmp,
		"BINT_ISO_REAL_APPDATA": r.Home + "\\Roaming",
		"=C:":                   "C:\\work", // biến '=C:' của Windows đi qua nguyên vẹn
		"Path":                  "C:\\bin",
	}
	for k, v := range want {
		if k == "=C:" {
			continue // cutEnv của envMap tách khác; kiểm bên dưới
		}
		if m[k] != v {
			t.Errorf("%s = %q, muốn %q", k, m[k], v)
		}
	}
	if !slices.Contains(env, "=C:=C:\\work") {
		t.Error("biến =C: phải giữ nguyên")
	}
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got := m[k]; !strings.HasPrefix(got, r.Tmp) || !strings.HasSuffix(got, "claude--w") {
			t.Errorf("%s = %q", k, got)
		}
	}
	if hasKeyFold(m, "anthropic_api_key") || hasKeyFold(m, "openai_api_key") {
		t.Error("credential còn lại")
	}
}

func TestWindowsRealEnvFoldsKeys(t *testing.T) {
	e := newEnvList("windows", []string{"Bint_Iso_Profile=codex/x", "Bint_Iso_Real_Temp=C:\\t", "TEMP=C:\\fake", "Home=C:\\fake"})
	restoreReal(e)
	if e.val("temp") != "C:\\t" {
		t.Errorf("TEMP = %q", e.val("temp"))
	}
	if _, ok := e.get("HOME"); ok {
		t.Error("HOME không có bản REAL phải bị gỡ")
	}
	if inProfile(e) || len(e.delPrefixList(realPrefix)) != 0 {
		t.Error("dấu hiệu profile còn sót")
	}
}

// Mọi biến mà Env đặt mới (không có trong base) phải nằm trong config.IsoManagedEnv hoặc họ BINT_ISO_REAL_;
// biến cache của danh mục share cũng phải do iso quản lý. Config không import iso nên test này giữ hai nơi khớp.
func TestEnvSetVarsAreManaged(t *testing.T) {
	r := newFakeReal(t)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, tool := range Tools() {
			p := profileAt(filepath.Join(r.Dir, "s"), tool, "w", defaultSettings(t, nil), LB{Mode: LBLinked})
			base := append(slices.Clone(r.Env), "UserProfile="+r.Home, "CODEX_LB_API_KEY=k")
			env, err := buildEnv(goos, p, base)
			if err != nil {
				t.Fatal(err)
			}
			for k := range envMap(env) {
				if strings.HasPrefix(k, realPrefix) {
					continue
				}
				if !slices.ContainsFunc(config.IsoManagedEnv, func(m string) bool { return strings.EqualFold(m, k) }) &&
					!slices.ContainsFunc(base, func(s string) bool { return strings.HasPrefix(strings.ToUpper(s), strings.ToUpper(k)+"=") }) {
					t.Errorf("%s/%s: Env đặt %s nhưng config.IsoManagedEnv không có", goos, tool, k)
				}
			}
		}
	}
	for _, v := range ownedVars() {
		if !slices.Contains(config.IsoManagedEnv, v) {
			t.Errorf("biến sở hữu %s thiếu trong config.IsoManagedEnv", v)
		}
	}
	for _, m := range config.IsoManagedEnv {
		if !slices.Contains(ownedVars(), m) && !strings.HasPrefix(m, "BINT_") {
			t.Errorf("config.IsoManagedEnv có %s mà iso không quản lý", m)
		}
	}
}

// Họ XDG_ không bị gỡ nguyên khối: biến hệ thống/phiên (XDG_RUNTIME_DIR, XDG_DATA_DIRS, XDG_CONFIG_DIRS,
// XDG_SESSION_*) phải tới được tiến trình con, còn bốn XDG_*_HOME trỏ vào profile.
func TestEnvKeepsSystemXDG(t *testing.T) {
	r := newFakeReal(t)
	p := profileAt(filepath.Join(r.Dir, "s"), Claude, "w", defaultSettings(t, nil), LB{})
	sys := map[string]string{
		"XDG_RUNTIME_DIR": "/run/user/1000", "XDG_DATA_DIRS": "/usr/share:/usr/local/share",
		"XDG_CONFIG_DIRS": "/etc/xdg", "XDG_SESSION_TYPE": "wayland", "XDG_SESSION_ID": "3",
	}
	base := slices.Clone(r.Env)
	for k, v := range sys {
		base = append(base, k+"="+v)
	}
	base = append(base, "XDG_CONFIG_HOME=/real/xc", "XDG_DATA_HOME=/real/xd", "XDG_STATE_HOME=/real/xs")
	env, err := buildEnv("linux", p, base)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	for k, v := range sys {
		if m[k] != v {
			t.Errorf("%s = %q, muốn giữ %q", k, m[k], v)
		}
	}
	for k, v := range map[string]string{
		"XDG_CONFIG_HOME": p.Home + "/.config", "XDG_DATA_HOME": p.Home + "/.local/share",
		"XDG_STATE_HOME": p.Home + "/.local/state",
	} {
		if m[k] != v {
			t.Errorf("%s = %q, muốn %q", k, m[k], v)
		}
	}
	// lồng profile: RealEnv không làm mất biến hệ thống.
	back := envMap(RealEnv(env))
	for k, v := range sys {
		if back[k] != v {
			t.Errorf("RealEnv: %s = %q, muốn %q", k, back[k], v)
		}
	}
	if back["XDG_CONFIG_HOME"] != "/real/xc" {
		t.Errorf("RealEnv: XDG_CONFIG_HOME = %q", back["XDG_CONFIG_HOME"])
	}
	// iso.drop_env vẫn gỡ được.
	p2 := profileAt(filepath.Join(r.Dir, "s"), Claude, "w", defaultSettings(t, func(c *config.Iso) { c.DropEnv = []string{"XDG_RUNTIME_DIR"} }), LB{})
	env2, err := buildEnv("linux", p2, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := envMap(env2)["XDG_RUNTIME_DIR"]; ok {
		t.Error("drop_env phải gỡ XDG_RUNTIME_DIR")
	}
}
