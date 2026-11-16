//go:build !windows

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAIStore dựng store ai-isolator giả và đặt AI_ISOLATOR_HOME vào nó.
func fakeAIStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AI_ISOLATOR_HOME", root)
	files := map[string]string{
		"profiles/codex--personal/profile.env":                    "AI_ISOLATOR_TOOL=codex\nAI_ISOLATOR_PROFILE=personal\n",
		"profiles/codex--personal/codex-lb.mode":                  "linked\n",
		"profiles/codex--personal/codex-lb.url":                   "http://127.0.0.1:2455\n",
		"profiles/codex--personal/home/.codex/auth.json":          `{"tokens":"x"}`,
		"profiles/codex--personal/home/.codex/config.toml":        "model = \"gpt\"\n",
		"profiles/codex--personal/home/.codex/sessions/a/r.jsonl": "s",
		"profiles/codex--personal/home/.codex/packages/p/bin":     "bin",
		"profiles/codex--personal/home/Projects/x.txt":            "user",
		"profiles/codex--personal/cache/c":                        "cache",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIsoImport(t *testing.T) {
	e := newIsoEnv(t, nil)
	fakeAIStore(t)
	dir := t.TempDir() // ngoài repo: sync không dựng trust/hook dự án

	if out, code := run(t, dir, "", "iso", "import"); code != 2 || !strings.Contains(out, "--from-ai-isolator") {
		t.Fatalf("thiếu --from-ai-isolator: code %d\n%s", code, out)
	}

	out, code := run(t, dir, "", "iso", "import", "--from-ai-isolator", "--dry-run", "--with-sessions")
	if code != 0 || !strings.Contains(out, "sẽ nhập") || !strings.Contains(out, "chưa ghi gì") {
		t.Fatalf("dry-run: code %d\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("codex", "personal")); err == nil {
		t.Fatal("--dry-run không được tạo profile")
	}

	out, code = run(t, dir, "", "iso", "import", "--from-ai-isolator", "--json")
	if code != 0 {
		t.Fatalf("import: code %d\n%s", code, out)
	}
	var res []struct {
		ID       string                     `json:"id"`
		Status   string                     `json:"status"`
		HasCreds bool                       `json:"has_credentials"`
		LB       struct{ Mode, URL string } `json:"lb"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "["):]), &res); err != nil || len(res) != 1 {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if res[0].ID != "codex/personal" || res[0].Status != "imported" || !res[0].HasCreds {
		t.Errorf("%+v", res[0])
	}
	home := filepath.Join(e.profileDir("codex", "personal"), "home")
	if b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); err != nil || string(b) != `{"tokens":"x"}` {
		t.Errorf("auth.json: %q %v", b, err)
	}
	for _, rel := range []string{".codex/sessions", ".codex/packages", "Projects"} {
		if _, err := os.Lstat(filepath.Join(home, rel)); err == nil {
			t.Errorf("%s không được chép khi không có --with-sessions", rel)
		}
	}
	if mode, url := lbOf(e.meta(t, "codex", "personal")); mode != "linked" || url != "http://127.0.0.1:2455" {
		t.Errorf("lb = %q %q", mode, url)
	}

	// Lần hai: profile đã có thì bỏ qua và báo, không ghi đè.
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, dir, "", "iso", "import", "--from-ai-isolator", "--with-sessions")
	if code != 0 || !strings.Contains(out, "bỏ qua") || !strings.Contains(out, "đã có") {
		t.Fatalf("nhập lại: code %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); string(b) != "mine" {
		t.Error("profile đã có bị ghi đè")
	}

	// Tên không có trong nguồn: lỗi, thoát 1.
	if out, code := run(t, dir, "", "iso", "import", "--from-ai-isolator", "codex--none"); code != 1 || !strings.Contains(out, "không có trong store") {
		t.Fatalf("tên vắng: code %d\n%s", code, out)
	}
	// --source đè AI_ISOLATOR_HOME; store không tồn tại là lỗi.
	if out, code := run(t, dir, "", "iso", "import", "--from-ai-isolator", "--source", filepath.Join(t.TempDir(), "none")); code != 1 || !strings.Contains(out, "không thấy store") {
		t.Fatalf("--source vắng: code %d\n%s", code, out)
	}
}

func TestIsoImportWithSessions(t *testing.T) {
	e := newIsoEnv(t, nil)
	fakeAIStore(t)
	out, code := run(t, t.TempDir(), "", "iso", "import", "--from-ai-isolator", "--with-sessions", "codex--personal")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.profileDir("codex", "personal"), "home", ".codex", "sessions", "a", "r.jsonl")); err != nil {
		t.Errorf("--with-sessions phải chép sessions/: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(e.profileDir("codex", "personal"), "home", ".codex", "packages")); err == nil {
		t.Error("bản cài không được chép")
	}
}

// Trong phiên agent import bị chặn (thao tác của người) và không tạo gì.
func TestIsoImportBlockedInAgentSession(t *testing.T) {
	e := newIsoEnv(t, nil)
	fakeAIStore(t)
	asAgent(t)
	out, code := run(t, t.TempDir(), "", "iso", "import", "--from-ai-isolator")
	if code == 0 || !strings.Contains(out, "agent") {
		t.Fatalf("phải bị chặn: code %d\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("codex", "personal")); err == nil {
		t.Error("phiên agent không được tạo profile")
	}
}
