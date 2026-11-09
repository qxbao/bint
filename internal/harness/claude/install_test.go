package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallPreservesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude", "settings.local.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	orig := `{
  "zeta": 1,
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [
      {"type": "command", "command": "old/bint hook claude"},
      {"type": "command", "command": "audit.sh"}
    ]}]
  },
  "alpha": true
}`
	os.WriteFile(p, []byte(orig), 0o644)
	backups := filepath.Join(dir, "backups")

	res, err := InstallHooks(p, `C:\Program Files\bint\bint.exe`, backups, time.Now())
	if err != nil || !res.Changed || res.BackupPath == "" {
		t.Fatalf("install: %+v %v", res, err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	// Thứ tự khoá cấp cao nhất được giữ.
	if !(strings.Index(s, `"zeta"`) < strings.Index(s, `"permissions"`) && strings.Index(s, `"hooks"`) < strings.Index(s, `"alpha"`)) {
		t.Fatalf("key order changed:\n%s", s)
	}
	for _, keep := range []string{"echo hi", "audit.sh", "Bash(ls:*)"} {
		if !strings.Contains(s, keep) {
			t.Fatalf("lost %q:\n%s", keep, s)
		}
	}
	if strings.Contains(s, "old/bint") {
		t.Fatal("stale bint hook not replaced")
	}
	if !strings.Contains(s, `\"C:/Program Files/bint/bint.exe\" hook claude`) {
		t.Fatalf("command not quoted:\n%s", s)
	}
	if ok, _ := HooksInstalled(p); !ok {
		t.Fatal("not detected as installed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}

	res, err = InstallHooks(p, `C:\Program Files\bint\bint.exe`, backups, time.Now())
	if err != nil || res.Changed {
		t.Fatalf("second install should be no-op: %+v %v", res, err)
	}

	res, err = UninstallHooks(p, backups, time.Now())
	if err != nil || !res.Changed {
		t.Fatalf("uninstall: %+v %v", res, err)
	}
	got, _ = os.ReadFile(p)
	if strings.Contains(string(got), "hook claude") || !strings.Contains(string(got), "audit.sh") || !strings.Contains(string(got), "echo hi") {
		t.Fatalf("after uninstall:\n%s", got)
	}
	if ok, _ := HooksInstalled(p); ok {
		t.Fatal("still installed")
	}
}

func TestInstallCreatesFileAndRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if _, err := InstallHooks(p, "bint", dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if ok, _ := HooksInstalled(p); !ok {
		t.Fatal("not installed")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("[1,2]"), 0o644)
	if _, err := InstallHooks(bad, "bint", dir, time.Now()); err == nil {
		t.Fatal("non-object settings must be rejected")
	}
	if b, _ := os.ReadFile(bad); string(b) != "[1,2]" {
		t.Fatal("bad file modified")
	}
}

func TestModelNameAndRelPath(t *testing.T) {
	if modelName(json.RawMessage(`"claude-x"`)) != "claude-x" ||
		modelName(json.RawMessage(`{"id":"m-id","display_name":"M"}`)) != "m-id" ||
		modelName(nil) != "" {
		t.Fatal("modelName")
	}
	root := t.TempDir()
	if r, ok := relPath(root, root, filepath.Join(root, "a", "b.go")); !ok || r != "a/b.go" {
		t.Fatalf("abs: %q %v", r, ok)
	}
	if r, ok := relPath(root, filepath.Join(root, "a"), "b.go"); !ok || r != "a/b.go" {
		t.Fatalf("rel: %q %v", r, ok)
	}
	if _, ok := relPath(root, root, filepath.Join(filepath.Dir(root), "x.go")); ok {
		t.Fatal("outside accepted")
	}
	if _, ok := relPath(root, root, "../x.go"); ok {
		t.Fatal("dotdot accepted")
	}
}
