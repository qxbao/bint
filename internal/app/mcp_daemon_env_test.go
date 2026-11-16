package app

import (
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Shim và `bint mcp gateway start` trong profile `bint iso` khởi động daemon bằng iso.RealEnv: HOME/TMPDIR thật, không còn biến
// của profile; ngoài profile thì nil (kế thừa như trước).
func TestDaemonEnvInsideProfileIsRealEnv(t *testing.T) {
	testutil.Isolate(t)
	if env := daemonEnv(); env != nil {
		t.Fatalf("ngoài profile phải là nil, nhận %v", env)
	}
	t.Setenv("HOME", "/store/profiles/codex--work/home")
	t.Setenv("TMPDIR", "/tmp/bint-iso-1000/codex--work")
	t.Setenv("BINT_ISO_PROFILE", "codex/work")
	t.Setenv("BINT_ISO_REAL_HOME", "/home/real")
	t.Setenv("BINT_ISO_REAL_TMPDIR", "/tmp/real")
	t.Setenv("CODEX_HOME", "/store/profiles/codex--work/home/.codex")
	t.Setenv("BINT_ISO_REAL_CODEX_HOME", "/home/real/.codex")
	env := daemonEnv()
	get := func(k string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, k+"="); ok {
				return v
			}
		}
		return ""
	}
	if get("HOME") != "/home/real" || get("TMPDIR") != "/tmp/real" || get("CODEX_HOME") != "/home/real/.codex" {
		t.Fatalf("daemon phải nhận env thật: HOME=%q TMPDIR=%q CODEX_HOME=%q", get("HOME"), get("TMPDIR"), get("CODEX_HOME"))
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "BINT_ISO_PROFILE=") || strings.HasPrefix(kv, "BINT_ISO_REAL_") {
			t.Fatalf("biến profile lọt sang daemon: %s", kv)
		}
	}
	if get("BINT_STATE_DIR") == "" {
		t.Fatal("BINT_STATE_DIR (thật, ghim) phải còn trong env của daemon")
	}
}
