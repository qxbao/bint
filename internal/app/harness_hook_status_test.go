package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bint/internal/harness"
	"bint/internal/harness/codex"
	"bint/internal/testutil"
)

func TestCodexHookStatusCurrentAndTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture app-server dùng POSIX shell")
	}
	testutil.Isolate(t)
	root := t.TempDir()
	path := filepath.Join(root, ".codex", "hooks.json")
	ad := harness.Codex{}
	status := func(binary string) HookStatus { return hookStatus(context.Background(), ad, ad, root, binary) }
	if got := status("unused"); got.Level != LevelWarn || !strings.Contains(got.Detail, "chưa cài") {
		t.Fatalf("missing: %+v", got)
	}
	if _, err := codex.InstallHooks(path, "bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	fresh, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.ReplaceAll(string(fresh), "|local_shell", "")
	if stale == string(fresh) {
		t.Fatal("fixture thiếu local_shell")
	}
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := status("unused"); got.Level != LevelWarn || !strings.Contains(got.Detail, "matcher đã cũ") {
		t.Fatalf("stale: %+v", got)
	}
	if err := os.WriteFile(path, fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state  string
		wantOK bool
	}{{"trusted", true}, {"modified", false}, {"partial", false}, {"disabled", false}, {"initial", false}, {"error", false}} {
		t.Run(tc.state, func(t *testing.T) {
			var raw []byte
			if tc.state == "error" {
				raw = []byte(`{"id":2,"error":{"message":"metadata unavailable"}}`)
			} else {
				var err error
				raw, err = os.ReadFile(filepath.Join("..", "harness", "codex", "testdata", "codex-0.167", "trust-"+tc.state+".json"))
				if err != nil {
					t.Fatal(err)
				}
				raw = []byte(strings.ReplaceAll(string(raw), "/repo", filepath.ToSlash(root)))
			}
			dir := t.TempDir()
			fixture := filepath.Join(dir, "response.json")
			if err := os.WriteFile(fixture, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(dir, "codex")
			script := fmt.Sprintf("#!/bin/sh\nwhile IFS= read -r line; do\ncase \"$line\" in\n*'\"method\":\"initialize\"'*) printf '%%s\\n' '{\"id\":1,\"result\":{}}';;\n*'\"method\":\"hooks/list\"'*) cat %q;;\nesac\ndone\n", fixture)
			if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			got := status(binary)
			if (got.Level == LevelOK) != tc.wantOK {
				t.Fatalf("status: %+v", got)
			}
		})
	}
}
