package iso

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRealEnvOutsideProfileUnchanged(t *testing.T) {
	r := newFakeReal(t)
	env := append(slices.Clone(r.Env), "BINT_ISO_REAL_HOME=/stray", "OPENAI_API_KEY=x")
	// Không có BINT_ISO_PROFILE thì không động tới gì, kể cả biến REAL lạc.
	if got := RealEnv(env); !slices.Equal(got, env) {
		t.Errorf("RealEnv ngoài profile phải giữ nguyên:\n got %v\nwant %v", got, env)
	}
	if InProfile(env) {
		t.Error("InProfile sai")
	}
}

func TestRealEnvInsideProfile(t *testing.T) {
	unixLayoutOnly(t)
	r := newFakeReal(t)
	set := defaultSettings(t, nil)
	p := profileAt(filepath.Join(r.Dir, "s"), Claude, "w", set, LB{})
	base := append(slices.Clone(r.Env), "XDG_DATA_HOME=/real/xd", "OPENAI_API_KEY=secret", "EDITOR=vim")
	env, err := buildEnv("linux", p, base)
	if err != nil {
		t.Fatal(err)
	}
	if !InProfile(env) {
		t.Fatal("env của profile phải InProfile")
	}
	real := envMap(RealEnv(env))
	for k, v := range map[string]string{
		"HOME": r.Home, "TMPDIR": r.Tmp, "XDG_DATA_HOME": "/real/xd", "XDG_CACHE_HOME": r.Dir + "/xcache",
		"EDITOR": "vim", "BINT_STATE_DIR": r.State, "BINT_CONFIG_HOME": r.Cfg,
	} {
		if real[k] != v {
			t.Errorf("RealEnv %s = %q, muốn %q", k, real[k], v)
		}
	}
	// Biến thật không đặt thì bị gỡ (không để giá trị giả).
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "TMP", "TEMP", "USERPROFILE", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "DISABLE_UPDATES", "BINT_ISO_PROFILE", "BINT_ISO_REAL_HOME", "GOMODCACHE"} {
		if v, ok := real[k]; ok {
			t.Errorf("RealEnv còn %s=%q", k, v)
		}
	}
	// Credential đã bị gỡ ở Env không được tự hiện lại.
	if _, ok := real["OPENAI_API_KEY"]; ok {
		t.Error("credential hiện lại")
	}
}

func TestRealHome(t *testing.T) {
	r := newFakeReal(t)
	t.Setenv("HOME", r.Home)
	t.Setenv("USERPROFILE", r.Home)
	t.Setenv(EnvProfile, "")
	t.Setenv(EnvRealHome, "")
	want, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := RealHome(); err != nil || got != want {
		t.Fatalf("ngoài profile: %q %v, muốn %q", got, err, want)
	}
	fake := filepath.Join(t.TempDir(), "fake-home")
	t.Setenv("HOME", fake)
	t.Setenv("USERPROFILE", fake)
	t.Setenv(EnvProfile, "codex/x")
	t.Setenv(EnvRealHome, r.Home)
	t.Setenv("BINT_ISO_REAL_USERPROFILE", r.Home)
	if got, err := RealHome(); err != nil || got != r.Home {
		t.Fatalf("trong profile: %q %v, muốn %q", got, err, r.Home)
	}
	t.Setenv(EnvRealHome, "")
	t.Setenv("BINT_ISO_REAL_USERPROFILE", "")
	if _, err := RealHome(); err == nil {
		t.Error("trong profile mà thiếu bản thật phải lỗi, không được trả HOME giả")
	}
}
