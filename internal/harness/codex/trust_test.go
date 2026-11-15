package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHooksTrustedUsesCurrentCodexDefinition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub giao thức POSIX, không chạy Codex model")
	}
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "hooks.json")
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.MkdirAll(filepath.Dir(path), 0o755)
	for _, tc := range []struct {
		state string
		want  bool
	}{{"initial", false}, {"partial", false}, {"modified", false}, {"disabled", false}, {"trusted", true}} {
		t.Run(tc.state, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/codex-0.167/trust-" + tc.state + ".json")
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(bin, "response.json")
			os.WriteFile(fixture, []byte(strings.ReplaceAll(string(raw), "/repo", filepath.ToSlash(root))), 0o600)
			// Dấu vết cũ có mặt cả ở partial/modified: đây là lỗi đã tái hiện thật.
			config := ""
			if tc.state != "initial" {
				config = "[hooks.state.\"" + filepath.ToSlash(path) + ":pre_tool_use:0:0\"]\ntrusted_hash = 'old-hash'\n"
			}
			os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600)
			stub := fmt.Sprintf("#!/bin/sh\nwhile IFS= read -r line; do\ncase \"$line\" in\n*'\"method\":\"initialize\"'*) printf '%%s\\n' '{\"id\":1,\"result\":{}}';;\n*'\"method\":\"hooks/list\"'*) cat %q;;\nesac\ndone\n", fixture)
			os.WriteFile(filepath.Join(bin, "codex"), []byte(stub), 0o755)
			if got := HooksTrusted(path); got != tc.want {
				t.Fatalf("%s: trusted=%v, want=%v (không được tin chỉ vì có một state entry cũ)", tc.state, got, tc.want)
			}
		})
	}
}

func TestCodexHookMetadataLive(t *testing.T) {
	if os.Getenv("BINT_CODEX_CONFIG_LIVE") != "1" {
		t.Skip("set BINT_CODEX_CONFIG_LIVE=1 to check installed Codex hooks/list")
	}
	root, home := t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	t.Setenv("CODEX_HOME", home)
	quoted, _ := json.Marshal(root)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[projects."+string(quoted)+"]\ntrust_level='trusted'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".codex", "hooks.json")
	if _, err := InstallHooks(path, "fixture-bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	report, err := InspectHookTrust(context.Background(), "codex", root, path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 4 || report.Untrusted != 4 || report.AllTrusted() {
		t.Fatalf("new fixture hooks must remain untrusted: %+v", report)
	}
}
