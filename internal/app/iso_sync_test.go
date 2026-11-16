//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"bint/internal/config"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/iso"
	"bint/internal/testutil"
)

func isoSyncProfile(t *testing.T, tool iso.Tool) iso.Profile {
	t.Helper()
	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(tool, "w")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeTOML(t *testing.T, path string, tree map[string]any) {
	t.Helper()
	b, err := toml.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	m, err := readTOMLMap(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// stubCodex đặt một `codex` giả đứng đầu PATH: trả hooks/list theo tệp fixture và ghi lại CODEX_HOME + cwd nó nhận.
func stubCodex(t *testing.T, fixture string) (seenHome, seenCwd string) {
	t.Helper()
	bin := t.TempDir()
	fx := filepath.Join(bin, "response.json")
	if err := os.WriteFile(fx, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	seenHome, seenCwd = filepath.Join(bin, "seen-home"), filepath.Join(bin, "seen-cwd")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$CODEX_HOME\" > %q\npwd > %q\nwhile IFS= read -r line; do\ncase \"$line\" in\n*'\"method\":\"initialize\"'*) printf '%%s\\n' '{\"id\":1,\"result\":{}}';;\n*'\"method\":\"hooks/list\"'*) cat %q;;\nesac\ndone\n", seenHome, seenCwd, fx)
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return seenHome, seenCwd
}

func hooksListJSON(hooks ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"id": 2, "result": map[string]any{"data": []any{map[string]any{"cwd": "/x", "hooks": hooks, "errors": []any{}}}}})
	return string(b)
}

func hookRow(key, event, source, hash, status string) map[string]any {
	return map[string]any{"key": key, "eventName": event, "sourcePath": source, "command": `"/real/bint" hook codex`,
		"enabled": true, "currentHash": hash, "trustStatus": status}
}

func TestIsoSyncCodexTrustAndUserHooks(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repo := testutil.NewRepo(t, map[string]string{"README.md": "x\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	realHome := filepath.Join(os.Getenv("HOME"), ".codex")
	realHooks := codex.UserHooksPathIn(realHome)
	if _, err := codex.InstallHooks(realHooks, "/real/bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	projHooks := filepath.Join(repo, ".codex", "hooks.json")
	writeTOML(t, filepath.Join(realHome, "config.toml"), map[string]any{
		"model": "real",
		"mcp_servers": map[string]any{
			"bint": map[string]any{"command": "/real/bint", "args": []any{"mcp", "serve"}},
			"web":  map[string]any{"url": "https://example.com/mcp"},
		},
		"projects": map[string]any{
			repo:     map[string]any{"trust_level": "trusted"},
			"/other": map[string]any{"trust_level": "trusted"},
		},
		"hooks": map[string]any{"state": map[string]any{
			projHooks + ":pre_tool_use:0:0":     map[string]any{"trusted_hash": "sha256:PROJ"},
			"/other/.codex/hooks.json:stop:0:0": map[string]any{"trusted_hash": "sha256:OTHER"},
			// hook user ở bản thật: PreToolUse trust đúng định nghĩa, PostToolUse trust định nghĩa cũ.
			realHooks + ":pre_tool_use:0:0":  map[string]any{"trusted_hash": "sha256:USER-PRE"},
			realHooks + ":post_tool_use:0:0": map[string]any{"trusted_hash": "sha256:USER-POST-OLD"},
		}},
	})

	p := isoSyncProfile(t, iso.Codex)
	profHooks := codex.UserHooksPathIn(filepath.Join(p.Home, ".codex"))
	seenHome, seenCwd := stubCodex(t, hooksListJSON(
		hookRow(projHooks+":pre_tool_use:0:0", "preToolUse", projHooks, "sha256:PROJ", "trusted"),
		hookRow(profHooks+":pre_tool_use:0:0", "preToolUse", profHooks, "sha256:USER-PRE", "untrusted"),
		hookRow(profHooks+":post_tool_use:0:0", "postToolUse", profHooks, "sha256:USER-POST-NEW", "untrusted"),
	))

	rep, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: repo})
	if err != nil {
		t.Fatal(err)
	}
	cfg := readTOML(t, filepath.Join(p.Home, ".codex", "config.toml"))
	if cfg["model"] != nil {
		t.Errorf("không chép khoá ngoài mcp_servers/projects/hooks.state: %v", cfg)
	}
	servers := tomlSub(cfg, "mcp_servers")
	bintEnv, _ := tomlSub(servers, "bint")["env"].(map[string]any)
	if bintEnv["BINT_STATE_DIR"] != os.Getenv("BINT_STATE_DIR") || bintEnv["BINT_CONFIG_HOME"] != os.Getenv("BINT_CONFIG_HOME") {
		t.Errorf("mục bint phải mang env tuyệt đối của máy thật: %v", servers)
	}
	if tomlSub(servers, "web")["url"] == nil || tomlSub(servers, "web")["env"] != nil {
		t.Errorf("MCP URL giữ nguyên: %v", servers)
	}
	projects := tomlSub(cfg, "projects")
	if tomlSub(projects, repo)["trust_level"] != "trusted" || projects["/other"] != nil {
		t.Errorf("chỉ chép trust của repo hiện tại: %v", projects)
	}
	state := tomlSub(tomlSub(cfg, "hooks"), "state")
	if tomlSub(state, projHooks+":pre_tool_use:0:0")["trusted_hash"] != "sha256:PROJ" {
		t.Errorf("hook dự án phải được chép trusted_hash: %v", state)
	}
	if state["/other/.codex/hooks.json:stop:0:0"] != nil {
		t.Errorf("không chép trust hook của dự án khác: %v", state)
	}
	if tomlSub(state, profHooks+":pre_tool_use:0:0")["trusted_hash"] != "sha256:USER-PRE" {
		t.Errorf("hook user khớp currentHash phải được chép sang key của profile: %v", state)
	}
	if state[profHooks+":post_tool_use:0:0"] != nil {
		t.Errorf("hook user không khớp currentHash không được chép: %v", state)
	}
	// hooks.json của profile: cài bằng InstallHooks với lệnh giống bản thật.
	pc, ok, err := codex.InstalledHookCommand(profHooks)
	rc, _, _ := codex.InstalledHookCommand(realHooks)
	if err != nil || !ok || pc != rc {
		t.Errorf("lệnh hook trong profile %q, bản thật %q (%v)", pc, rc, err)
	}
	// hooks/list chạy với env của profile, cwd là gốc repo.
	if got, _ := os.ReadFile(seenHome); string(got) != filepath.Join(p.Home, ".codex") {
		t.Errorf("hooks/list phải chạy với CODEX_HOME của profile: %q", got)
	}
	if got, _ := os.ReadFile(seenCwd); !strings.HasPrefix(strings.TrimSpace(string(got)), repo) {
		t.Errorf("cwd = %q", got)
	}
	notes := strings.Join(rep.Notes, "\n")
	for _, want := range []string{"hook dự án của bint trong profile: 1/1 trusted", "hook user postToolUse của bint chưa trust"} {
		if !strings.Contains(notes, want) {
			t.Errorf("thiếu ghi chú %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "preToolUse của bint chưa trust") {
		t.Errorf("hook user đã chép trust không được báo thiếu:\n%s", notes)
	}

	// Lần sau: khớp hoàn toàn (kể cả --check, không chạy Codex).
	rep, err = IsoSync(context.Background(), p, IsoSyncOptions{Dir: repo, Check: true})
	if err != nil || rep.Drift() {
		t.Fatalf("--check sau sync: %+v %v", rep.Items, err)
	}
	// Bản thật gỡ hook user: sync gỡ khỏi profile (chưa sửa tay).
	if _, err := codex.UninstallHooks(realHooks, t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: repo, Static: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := codex.InstalledHookCommand(profHooks); ok {
		t.Error("bản thật đã gỡ hook thì profile cũng gỡ")
	}
}

func TestIsoSyncCodexOutsideRepoSkipsTrustKeepsMCP(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("CODEX_HOME", "")
	realHome := filepath.Join(os.Getenv("HOME"), ".codex")
	writeTOML(t, filepath.Join(realHome, "config.toml"), map[string]any{
		"mcp_servers": map[string]any{"x": map[string]any{"command": "x"}},
		"projects":    map[string]any{"/some/repo": map[string]any{"trust_level": "trusted"}},
	})
	p := isoSyncProfile(t, iso.Codex)
	rep, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := readTOML(t, filepath.Join(p.Home, ".codex", "config.toml"))
	if tomlSub(cfg, "mcp_servers")["x"] == nil || cfg["projects"] != nil {
		t.Fatalf("ngoài repo: chép MCP, bỏ trust: %v", cfg)
	}
	if !strings.Contains(strings.Join(rep.Notes, "\n"), "bỏ qua trust dự án") {
		t.Errorf("phải báo một dòng: %v", rep.Notes)
	}
}

// Profile là toàn máy: sync ở repo B không được gỡ trust của repo A (review 01M7H8YGSZWSEDBV4A2WNYKQAW.1), nhưng
// khi bản thật bỏ trust của A thì sync (ở bất kỳ repo nào) vẫn gỡ.
func TestIsoSyncTrustSurvivesSwitchingRepos(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	repoA, repoB := testutil.NewRepo(t, map[string]string{"a": "x\n"}), testutil.NewRepo(t, map[string]string{"b": "x\n"})
	repoA, _ = filepath.EvalSymlinks(repoA)
	repoB, _ = filepath.EvalSymlinks(repoB)
	realHome := filepath.Join(os.Getenv("HOME"), ".codex")
	hooksA, hooksB := filepath.Join(repoA, ".codex", "hooks.json")+":pre_tool_use:0:0", filepath.Join(repoB, ".codex", "hooks.json")+":pre_tool_use:0:0"
	realCfg := filepath.Join(realHome, "config.toml")
	write := func(withA bool) {
		projects := map[string]any{repoB: map[string]any{"trust_level": "trusted"}}
		state := map[string]any{hooksB: map[string]any{"trusted_hash": "sha256:B"}}
		if withA {
			projects[repoA] = map[string]any{"trust_level": "trusted"}
			state[hooksA] = map[string]any{"trusted_hash": "sha256:A"}
		}
		writeTOML(t, realCfg, map[string]any{"projects": projects, "hooks": map[string]any{"state": state}})
	}
	write(true)
	p := isoSyncProfile(t, iso.Codex)
	profCfg := filepath.Join(p.Home, ".codex", "config.toml")
	sync := func(dir string, check bool) iso.SyncReport {
		rep, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: dir, Static: true, Check: check})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	sync(repoA, false)
	if _, err := os.Stat(profCfg); err != nil {
		t.Fatal(err)
	}
	if c := readTOML(t, profCfg); tomlSub(tomlSub(c, "projects"), repoA) == nil || tomlSub(tomlSub(tomlSub(c, "hooks"), "state"), hooksA) == nil {
		t.Fatalf("sync ở A phải chép trust của A: %v", c)
	}
	rep := sync(repoB, true)
	for _, it := range rep.Items {
		if it.Kind == iso.KindTrust && it.Action == iso.ActRemove {
			t.Errorf("--check ở B không được báo gỡ trust của A: %+v", it)
		}
	}
	sync(repoB, false)
	c := readTOML(t, profCfg)
	if tomlSub(tomlSub(c, "projects"), repoA) == nil || tomlSub(tomlSub(tomlSub(c, "hooks"), "state"), hooksA) == nil {
		t.Fatalf("sync ở B đã gỡ trust của A: %v", c)
	}
	if tomlSub(tomlSub(c, "projects"), repoB) == nil || tomlSub(tomlSub(tomlSub(c, "hooks"), "state"), hooksB) == nil {
		t.Fatalf("sync ở B phải chép trust của B: %v", c)
	}
	// Bản thật bỏ trust của A: sync ở B gỡ nó khỏi profile.
	write(false)
	sync(repoB, false)
	c = readTOML(t, profCfg)
	if tomlSub(tomlSub(c, "projects"), repoA) != nil || tomlSub(tomlSub(tomlSub(c, "hooks"), "state"), hooksA) != nil {
		t.Fatalf("bản thật đã bỏ trust của A thì profile phải gỡ: %v", c)
	}
	if tomlSub(tomlSub(c, "projects"), repoB) == nil {
		t.Fatalf("trust của B phải còn: %v", c)
	}
}

func TestIsoSyncClaudeMCPHookAndNoAccountKeys(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := os.Getenv("HOME")
	secret := `{"oauthAccount":{"emailAddress":"real@example.com"},"userID":"U-SECRET","mcpServers":{"web":{"command":"npx","args":["web"]}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".credentials.json"} {
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".claude", f), []byte(`{"token":"TOK-SECRET"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	realSettings := claude.UserSettingsPathIn(home)
	if _, err := claude.InstallHooks(realSettings, "/real/bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	p := isoSyncProfile(t, iso.Claude)
	// Profile có tài khoản riêng: phải còn nguyên.
	own := filepath.Join(p.Home, ".claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(own), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(own, []byte(`{"oauthAccount":{"emailAddress":"profile@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(own)
	if !strings.Contains(string(got), "profile@example.com") || !strings.Contains(string(got), `"web"`) || strings.Contains(string(got), "real@example.com") {
		t.Fatalf(".claude.json của profile:\n%s", got)
	}
	pc, ok, _ := claude.InstalledHookCommand(claude.UserSettingsPathIn(p.Home))
	rc, _, _ := claude.InstalledHookCommand(realSettings)
	if !ok || pc != rc {
		t.Fatalf("hook user trong profile %q, bản thật %q", pc, rc)
	}
	// Không có chữ bí mật nào của bản thật trong profile; không có tệp đăng nhập.
	_ = filepath.WalkDir(p.Dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "U-SECRET") || strings.Contains(string(b), "TOK-SECRET") || strings.Contains(string(b), "real@example.com") {
				t.Errorf("%s chứa dữ liệu đăng nhập của bản thật", path)
			}
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(p.Home, ".claude", ".credentials.json")); err == nil {
		t.Error("sync không được tạo .credentials.json")
	}
}

// Live: hook dự án của bint chạy trong profile Codex mới không cần trust tay (tiêu chí "xong" của GĐ4). Dùng Codex thật
// (initialize + hooks/list, không gọi mô hình). Chạy bằng BINT_CODEX_CONFIG_LIVE=1.
func TestIsoSyncCodexProjectHooksTrustedLive(t *testing.T) {
	if os.Getenv("BINT_CODEX_CONFIG_LIVE") != "1" {
		t.Skip("đặt BINT_CODEX_CONFIG_LIVE=1 để kiểm hooks/list của Codex thật")
	}
	origPath := os.Getenv("PATH") // Isolate đặt codex giả đứng đầu PATH: cần codex thật
	testutil.Isolate(t)
	t.Setenv("PATH", origPath)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	realHome := t.TempDir()
	t.Setenv("CODEX_HOME", realHome)
	repo := testutil.NewRepo(t, map[string]string{"README.md": "x\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	projHooks := filepath.Join(repo, ".codex", "hooks.json")
	if _, err := codex.InstallHooks(projHooks, "fixture-bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	realUserHooks := codex.UserHooksPathIn(realHome)
	if _, err := codex.InstallHooks(realUserHooks, "fixture-bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	realCfg := filepath.Join(realHome, "config.toml")
	writeTOML(t, realCfg, map[string]any{"projects": map[string]any{repo: map[string]any{"trust_level": "trusted"}}})
	// Người dùng trust hook ở máy thật: trusted_hash = currentHash do chính Codex báo.
	list, err := codex.ListHooks(context.Background(), "codex", repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	for _, h := range list.Hooks {
		if h.SourcePath == projHooks || h.SourcePath == realUserHooks {
			state[h.Key] = map[string]any{"trusted_hash": h.CurrentHash}
		}
	}
	if len(state) != 8 {
		t.Fatalf("Codex phải báo 4 hook dự án + 4 hook user: %+v", list)
	}
	writeTOML(t, realCfg, map[string]any{
		"projects": map[string]any{repo: map[string]any{"trust_level": "trusted"}},
		"hooks":    map[string]any{"state": state},
	})

	p := isoSyncProfile(t, iso.Codex)
	rep, err := IsoSync(context.Background(), p, IsoSyncOptions{Dir: repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("báo cáo: %+v", rep)
	penv, cleanup, err := iso.ProbeEnv(p, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	plist, err := codex.ListHooks(context.Background(), "codex", repo, penv)
	if err != nil {
		t.Fatal(err)
	}
	profUserHooks := codex.UserHooksPathIn(filepath.Join(p.Home, ".codex"))
	n, nu := 0, 0
	for _, h := range plist.Hooks {
		if h.SourcePath == profUserHooks {
			nu++
			if h.TrustStatus != "trusted" {
				t.Errorf("hook user %s: %s, muốn trusted (trusted_hash chép vì currentHash khớp)", h.Key, h.TrustStatus)
			}
		}
		if h.SourcePath == projHooks {
			n++
			if h.TrustStatus != "trusted" {
				t.Errorf("hook %s: %s, muốn trusted mà không trust tay trong profile", h.Key, h.TrustStatus)
			}
		}
	}
	if n != 4 || nu != 4 {
		t.Fatalf("profile thấy %d hook dự án và %d hook user, muốn 4 và 4: %+v", n, nu, plist)
	}
	// Đối chứng: profile không sync thì hook chưa trusted.
	bare := isoSyncProfile2(t, iso.Codex, "bare")
	benv, bclean, err := iso.ProbeEnv(bare, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	defer bclean()
	blist, err := codex.ListHooks(context.Background(), "codex", repo, benv)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range blist.Hooks {
		if h.SourcePath == projHooks && h.TrustStatus == "trusted" {
			t.Errorf("profile chưa sync mà hook %s đã trusted", h.Key)
		}
	}
}

func isoSyncProfile2(t *testing.T, tool iso.Tool, name string) iso.Profile {
	t.Helper()
	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(tool, name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
