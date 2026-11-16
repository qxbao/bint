//go:build !windows

package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isoSyncCodexReal(t *testing.T, body string) string {
	t.Helper()
	home := filepath.Join(os.Getenv("HOME"), ".codex")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestIsoSyncCodexMCPConflictAndCheck(t *testing.T) {
	e := newIsoEnv(t, nil)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := t.TempDir()
	realCfg := isoSyncCodexReal(t, "model = \"real\"\n\n[mcp_servers.x]\ncommand = \"x-cmd\"\n\n[mcp_servers.bint]\ncommand = \"/opt/bint\"\nargs = [\"mcp\", \"serve\"]\n")
	// Skill thật để liên kết.
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "", "iso", "create", "codex", "--name", "w", "--no-lb")
	if code != 0 || !strings.Contains(out, "codex/w") {
		t.Fatalf("create: %d\n%s", code, out)
	}
	prof := filepath.Join(e.profileDir("codex", "w"), "home", ".codex")
	got, err := os.ReadFile(filepath.Join(prof, "config.toml"))
	if err != nil {
		t.Fatalf("create phải tự sync: %v\n%s", err, out)
	}
	cfg := string(got)
	for _, want := range []string{"x-cmd", "BINT_STATE_DIR", os.Getenv("BINT_STATE_DIR"), "BINT_CONFIG_HOME", os.Getenv("BINT_CONFIG_HOME")} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config.toml của profile thiếu %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "model") {
		t.Errorf("chỉ chép mcp_servers, không chép khoá khác:\n%s", cfg)
	}
	if isl := func() bool {
		fi, err := os.Lstat(filepath.Join(prof, "skills"))
		return err == nil && fi.Mode()&os.ModeSymlink != 0
	}(); !isl {
		t.Error("skills phải là liên kết tới bản thật")
	}
	rc := filepath.Join(e.profileDir("codex", "w"), "sync-receipt.json")
	if fi, err := os.Stat(rc); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("receipt: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(prof, "auth.json")); err == nil {
		t.Fatal("sync không được tạo auth.json")
	}

	// Đã khớp: --check thoát 0.
	if out, code := run(t, dir, "", "iso", "sync", "--check"); code != 0 {
		t.Fatalf("--check khi khớp: %d\n%s", code, out)
	}

	// Sửa tay một MCP ở profile → sync báo xung đột nêu khoá, tệp giữ nguyên từng byte, --check thoát 1.
	hand := strings.Replace(cfg, "x-cmd", "HAND-EDIT", 1)
	cfgPath := filepath.Join(prof, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realCfg, []byte(strings.Replace(readAll(t, realCfg), "x-cmd", "x-cmd2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, dir, "", "iso", "sync", "--profile", "codex/w")
	if code != 0 || !strings.Contains(out, "XUNG ĐỘT") || !strings.Contains(out, "mcp_servers.x") {
		t.Fatalf("sync phải báo xung đột nêu khoá: %d\n%s", code, out)
	}
	if readAll(t, cfgPath) != hand {
		t.Fatalf("tệp profile phải giữ nguyên từng byte:\n%s", readAll(t, cfgPath))
	}
	out, code = run(t, dir, "", "iso", "sync", "--check")
	if code != 1 || !strings.Contains(out, "mcp_servers.x") {
		t.Fatalf("--check khi xung đột: %d\n%s", code, out)
	}
	// Chạy run chỉ cảnh báo, không đổi mã thoát.
	out, code = run(t, dir, "", "iso", "run", "codex", "--name", "w", "--no-lb", "--", "exec")
	if code != 0 || !strings.Contains(out, "lệch bản thật") || !strings.Contains(out, "mcp_servers.x") {
		t.Fatalf("run phải cảnh báo lệch mà không đổi mã thoát: %d\n%s", code, out)
	}
	if readAll(t, cfgPath) != hand {
		t.Fatal("run không được ghi cấu hình")
	}

	// Bỏ sửa tay (trả về đúng bản thật) → khớp, --check thoát 0.
	if err := os.WriteFile(cfgPath, []byte(strings.Replace(hand, "HAND-EDIT", "x-cmd2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "", "iso", "sync", "--check"); code != 0 {
		t.Fatalf("--check sau khi bỏ sửa tay: %d\n%s", code, out)
	}
	// Cờ loại trừ nhau là lỗi dùng.
	if _, code := run(t, dir, "", "iso", "sync", "--all", "--profile", "codex/w"); code != 2 {
		t.Fatalf("--all với --profile: code %d, muốn 2", code)
	}
}

func readAll(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIsoSyncClaudeJSONKeepsAccountKeys(t *testing.T) {
	e := newIsoEnv(t, nil)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := t.TempDir()
	home := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"oauthAccount":{"emailAddress":"real@example.com"},"mcpServers":{"web":{"command":"npx","args":["-y","web"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "", "iso", "create", "claude", "--name", "c"); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	cj := filepath.Join(e.profileDir("claude", "c"), "home", ".claude", ".claude.json")
	got := readAll(t, cj)
	if !strings.Contains(got, `"web"`) || strings.Contains(got, "real@example.com") || strings.Contains(got, "oauthAccount") {
		t.Fatalf("chỉ chép mcpServers, không chép khoá tài khoản:\n%s", got)
	}
}
