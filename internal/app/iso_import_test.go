//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/harness/claude"
	"bint/internal/iso"
	"bint/internal/testutil"
)

func isoImportStore(t *testing.T) *iso.Store {
	t.Helper()
	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// isoAIStore dựng store ai-isolator giả với một profile; trả gốc store và thư mục profile.
func isoAIStore(t *testing.T, tool, name string, files map[string]string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "profiles", tool+"--"+name)
	files["profile.env"] = "AI_ISOLATOR_TOOL=" + tool + "\nAI_ISOLATOR_PROFILE=" + name + "\n"
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, dir
}

// Nhập Codex: MCP trùng bản thật và hooks.state trỏ vào profile ai-isolator bị bỏ, nên IsoSync ngay sau đó
// không báo xung đột (nhánh "C có, R vắng, C ≠ D") mà dựng lại mục bint với env của máy thật; MCP riêng giữ.
func TestIsoImportCodexDropsDuplicateMCPAndSyncsWithoutConflict(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir()) // ngoài repo: IsoSync không dựng trust/hook dự án và không gọi codex
	realHome := filepath.Join(os.Getenv("HOME"), ".codex")
	writeTOML(t, filepath.Join(realHome, "config.toml"), map[string]any{
		"mcp_servers": map[string]any{
			"bint": map[string]any{"command": "/real/bint", "args": []any{"mcp", "serve"}},
			"web":  map[string]any{"url": "https://example.com/mcp"},
		},
	})
	// Dựng store giả hai bước: hooks.state phải trỏ vào đúng thư mục profile ai-isolator vừa tạo.
	root, dir := isoAIStore(t, "codex", "personal", map[string]string{
		"home/.codex/auth.json": `{"tokens":"x"}`, "home/.codex/config.toml": "",
		"codex-lb.mode": "linked", "codex-lb.url": "http://127.0.0.1:2455",
	})
	oldHooks := filepath.Join(dir, "home", ".codex", "hooks.json")
	cfg := "[mcp_servers.bint]\ncommand = \"/old/bint\"\nargs = [\"mcp\", \"serve\"]\n" +
		"[mcp_servers.web]\nurl = \"https://old.example.com\"\n" +
		"[mcp_servers.mine]\ncommand = \"npx\"\n" +
		"[hooks.state.\"" + oldHooks + ":pre_tool_use:0:0\"]\ntrusted_hash = \"sha256:OLD\"\n"
	if err := os.WriteFile(filepath.Join(dir, "home", ".codex", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := IsoImport(context.Background(), isoImportStore(t), IsoImportOptions{Source: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Status != iso.ImportImported || out[0].Final == nil {
		t.Fatalf("%+v", out)
	}
	fin := out[0].Final
	if fin.SyncErr != nil {
		t.Fatal(fin.SyncErr)
	}
	if c := fin.Sync.Conflicts(); len(c) != 0 {
		t.Fatalf("sync sau nhập không được có xung đột: %+v", c)
	}
	p := out[0].Profile
	got := readTOML(t, filepath.Join(p.Home, ".codex", "config.toml"))
	servers := tomlSub(got, "mcp_servers")
	if cmd, _ := tomlSub(servers, "bint")["command"].(string); cmd != "/real/bint" {
		t.Errorf("mục bint phải theo bản thật sau sync, có %v", servers["bint"])
	}
	env, _ := tomlSub(servers, "bint")["env"].(map[string]any)
	if env["BINT_STATE_DIR"] != os.Getenv("BINT_STATE_DIR") {
		t.Errorf("mục bint phải mang BINT_STATE_DIR thật: %v", servers["bint"])
	}
	if u, _ := tomlSub(servers, "web")["url"].(string); u != "https://example.com/mcp" {
		t.Errorf("mục web phải theo bản thật: %v", servers["web"])
	}
	if tomlSub(servers, "mine") == nil {
		t.Errorf("MCP riêng của profile phải giữ: %v", servers)
	}
	if st := tomlSub(tomlSub(got, "hooks"), "state"); len(st) != 0 {
		t.Errorf("hooks.state trỏ vào profile ai-isolator phải bị bỏ: %v", st)
	}
	// Sau đó --check khớp.
	rep, err := IsoSync(context.Background(), p, IsoSyncOptions{Check: true})
	if err != nil || rep.Drift() {
		t.Errorf("--check sau nhập: %+v %v", rep.Items, err)
	}
	if p.Meta.LB.Mode != iso.LBLinked || p.Meta.LB.URL != "http://127.0.0.1:2455" {
		t.Errorf("LB: %+v", p.Meta.LB)
	}
}

func TestIsoImportClaudeDropsDuplicateMCP(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".claude.json"),
		[]byte(`{"oauthAccount":{"emailAddress":"real@x"},"mcpServers":{"dup":{"command":"real"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root, _ := isoAIStore(t, "claude", "work", map[string]string{
		"home/.claude/.credentials.json": `{}`,
		"home/.claude/.claude.json":      `{"oauthAccount":{"emailAddress":"work@x"},"mcpServers":{"dup":{"command":"old"},"mine":{"command":"y"}}}`,
	})
	out, err := IsoImport(context.Background(), isoImportStore(t), IsoImportOptions{Source: root})
	if err != nil || len(out) != 1 || out[0].Status != iso.ImportImported {
		t.Fatalf("%v %+v", err, out)
	}
	if c := out[0].Final.Sync.Conflicts(); len(c) != 0 {
		t.Fatalf("xung đột: %+v", c)
	}
	b, err := os.ReadFile(filepath.Join(out[0].Profile.Home, ".claude", ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"real"`) || strings.Contains(s, `"old"`) || !strings.Contains(s, `"mine"`) || !strings.Contains(s, "work@x") || strings.Contains(s, "real@x") {
		t.Errorf(".claude.json sau nhập+sync: %s", s)
	}
}

func TestIsoImportDryRunAndAgentGate(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Chdir(t.TempDir())
	root, _ := isoAIStore(t, "codex", "p", map[string]string{"home/.codex/auth.json": "{}"})
	st := isoImportStore(t)
	out, err := IsoImport(context.Background(), st, IsoImportOptions{Source: root, DryRun: true})
	if err != nil || len(out) != 1 || out[0].Status != iso.ImportPlanned || out[0].Final != nil {
		t.Fatalf("%v %+v", err, out)
	}
	if _, err := os.Lstat(st.Dir(iso.Codex, "p")); err == nil {
		t.Error("--dry-run không được tạo profile")
	}
	asAgent(t)
	if _, err := IsoImport(context.Background(), st, IsoImportOptions{Source: root}); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("phiên agent: %v", err)
	}
	if _, err := os.Lstat(st.Dir(iso.Codex, "p")); err == nil {
		t.Error("phiên agent không được tạo profile")
	}
}

// Nhập khi đang đứng trong repo đã trust: trust dự án/hook user Codex và hook bint Claude đã chép từ ai-isolator
// khác bản thật không được làm IsoSync báo xung đột (profile mới chưa có receipt); trust repo khác giữ nguyên.
func TestIsoImportInRepoNoConflict(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repo := testutil.NewRepo(t, map[string]string{"README.md": "x\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	t.Chdir(repo)
	stubCodex(t, hooksListJSON())
	ctx := context.Background()

	// ---- Codex ----
	realHome := filepath.Join(os.Getenv("HOME"), ".codex")
	projHooks := filepath.Join(repo, ".codex", "hooks.json")
	hookKey := projHooks + ":pre_tool_use:0:0"
	writeTOML(t, filepath.Join(realHome, "config.toml"), map[string]any{
		"projects": map[string]any{repo: map[string]any{"trust_level": "trusted"}},
		"hooks":    map[string]any{"state": map[string]any{hookKey: map[string]any{"trusted_hash": "sha256:NEW"}}},
	})
	root, dir := isoAIStore(t, "codex", "personal", map[string]string{"home/.codex/auth.json": `{"tokens":"x"}`, "home/.codex/config.toml": ""})
	cfg := "[projects.\"" + repo + "\"]\ntrust_level = \"untrusted\"\n" +
		"[projects.\"/other/repo\"]\ntrust_level = \"trusted\"\n" +
		"[hooks.state.\"" + hookKey + "\"]\ntrusted_hash = \"sha256:OLD\"\n"
	if err := os.WriteFile(filepath.Join(dir, "home", ".codex", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := IsoImport(ctx, isoImportStore(t), IsoImportOptions{Source: root})
	if err != nil || len(out) != 1 || out[0].Status != iso.ImportImported || out[0].Final == nil {
		t.Fatalf("%v %+v", err, out)
	}
	if out[0].Final.SyncErr != nil {
		t.Fatal(out[0].Final.SyncErr)
	}
	if c := out[0].Final.Sync.Conflicts(); len(c) != 0 {
		t.Fatalf("sync sau nhập trong repo không được có xung đột: %+v", c)
	}
	p := out[0].Profile
	got := readTOML(t, filepath.Join(p.Home, ".codex", "config.toml"))
	projects := tomlSub(got, "projects")
	if tomlSub(projects, repo)["trust_level"] != "trusted" {
		t.Errorf("trust dự án phải theo bản thật: %v", projects)
	}
	if tomlSub(projects, "/other/repo")["trust_level"] != "trusted" {
		t.Errorf("trust repo khác chép từ ai-isolator phải còn: %v", projects)
	}
	if h := tomlSub(tomlSub(tomlSub(got, "hooks"), "state"), hookKey)["trusted_hash"]; h != "sha256:NEW" {
		t.Errorf("hooks.state phải theo bản thật, có %v", h)
	}
	rep, err := IsoSync(ctx, p, IsoSyncOptions{Check: true})
	if err != nil || len(rep.Conflicts()) != 0 || rep.Drift() {
		t.Errorf("--check sau nhập: %+v %v", rep.Items, err)
	}

	// ---- Claude ----
	home := os.Getenv("HOME")
	realSettings := claude.UserSettingsPathIn(home)
	if _, err := claude.InstallHooks(realSettings, "/real/bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), "settings.json")
	if _, err := claude.InstallHooks(old, "/old/bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["hooks"].(map[string]any)["Notification"] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/usr/bin/user-hook"}}}}
	body, _ := json.Marshal(doc)
	croot, _ := isoAIStore(t, "claude", "work", map[string]string{
		"home/.claude/.credentials.json": `{}`, "home/.claude/settings.json": string(body),
	})
	cout, err := IsoImport(ctx, isoImportStore(t), IsoImportOptions{Source: croot})
	if err != nil || len(cout) != 1 || cout[0].Status != iso.ImportImported || cout[0].Final == nil {
		t.Fatalf("%v %+v", err, cout)
	}
	if c := cout[0].Final.Sync.Conflicts(); len(c) != 0 {
		t.Fatalf("sync Claude sau nhập không được có xung đột: %+v", c)
	}
	cp := cout[0].Profile
	profSettings := claude.UserSettingsPathIn(cp.Home)
	cmd, ok, err := claude.InstalledHookCommand(profSettings)
	rc, _, _ := claude.InstalledHookCommand(realSettings)
	if err != nil || !ok || cmd != rc || strings.Contains(cmd, "/old/bint") {
		t.Errorf("hook bint của profile phải theo bản thật: %q (thật %q) %v", cmd, rc, err)
	}
	if b, _ := os.ReadFile(profSettings); !strings.Contains(string(b), "/usr/bin/user-hook") {
		t.Errorf("hook user khác phải còn:\n%s", b)
	}
	if rep, err := IsoSync(ctx, cp, IsoSyncOptions{Check: true}); err != nil || len(rep.Conflicts()) != 0 || rep.Drift() {
		t.Errorf("--check Claude sau nhập: %+v %v", rep.Items, err)
	}
}
