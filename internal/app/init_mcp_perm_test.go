package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/claude"
	"bint/internal/testutil"
)

func readJSONFile(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m
}

func countRule(m map[string]any) int {
	perms, _ := m["permissions"].(map[string]any)
	allow, _ := perms["allow"].([]any)
	n := 0
	for _, a := range allow {
		if a == claude.PermissionRule {
			n++
		}
	}
	return n
}

// bint init --mcp và --update thêm mcp__bint__* vào permissions.allow của settings Claude theo scope hook; chạy lại không thêm
// trùng; khoá và quy tắc khác giữ nguyên.
func TestInitAddsMCPPermissionRule(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	settings := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"model":"opus","permissions":{"allow":["Bash(go test:*)"],"deny":["Read(.env)"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := InitOptions{Dir: root, Hooks: yes(), HooksScope: claude.ScopeProject, MCP: yes()}
	mustInit(t, opts)
	m := readJSONFile(t, settings)
	if countRule(m) != 1 {
		t.Fatalf("muốn đúng một quy tắc %s: %v", claude.PermissionRule, m["permissions"])
	}
	perms := m["permissions"].(map[string]any)
	if allow := perms["allow"].([]any); allow[0] != "Bash(go test:*)" || len(allow) != 2 {
		t.Errorf("quy tắc sẵn có phải giữ nguyên và đứng trước: %v", allow)
	}
	if perms["deny"] == nil || m["model"] != "opus" || m["hooks"] == nil {
		t.Errorf("khoá khác phải giữ nguyên: %v", m)
	}
	before, _ := os.ReadFile(settings)
	mustInit(t, opts)
	mustInit(t, InitOptions{Dir: root, HooksScope: claude.ScopeProject, Update: true})
	after, _ := os.ReadFile(settings)
	if string(before) != string(after) {
		t.Errorf("chạy lại không được đổi tệp:\n%s\n---\n%s", before, after)
	}
	if countRule(readJSONFile(t, settings)) != 1 {
		t.Error("chạy lại thêm trùng quy tắc")
	}
}

// --update một mình cũng thêm quy tắc (scope mặc định local → settings.local.json).
func TestInitUpdateAddsMCPPermissionRule(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: no()})
	local := filepath.Join(root, ".claude", "settings.local.json")
	if _, err := os.Stat(local); err == nil {
		t.Fatal("chưa có quy tắc nào trước --update")
	}
	mustInit(t, InitOptions{Dir: root, Update: true})
	if countRule(readJSONFile(t, local)) != 1 {
		t.Fatalf("--update phải thêm quy tắc: %s", func() string { b, _ := os.ReadFile(local); return string(b) }())
	}
}

// bint init --mcp-user ghi mục bint vào ~/.gemini/config/mcp_config.json của Antigravity (giữ server khác, có sao lưu), chạy lại
// không đổi; không có cờ thì không ghi.
func TestInitMCPUserAntigravity(t *testing.T) {
	testutil.Isolate(t) // HOME tạm
	home, _ := os.UserHomeDir()
	cfg := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "antigravity"}, Hooks: no()})
	if _, err := os.Stat(cfg); err == nil {
		t.Fatal("không có --mcp-user thì không được ghi cấu hình người dùng")
	}
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{"jira-run":{"command":"uvx","args":["x"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := mustInit(t, InitOptions{Dir: root, MCPUser: true, Hooks: no()})
	m := readJSONFile(t, cfg)
	servers := m["mcpServers"].(map[string]any)
	bint, ok := servers["bint"].(map[string]any)
	if !ok || servers["jira-run"] == nil {
		t.Fatalf("mcpServers: %v", servers)
	}
	args, _ := bint["args"].([]any)
	if bint["command"] != "bint" || len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Errorf("mục bint: %v", bint)
	}
	// Sao lưu bản cũ trong thư mục trạng thái.
	found := false
	_ = filepath.WalkDir(os.Getenv("BINT_STATE_DIR"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(p, "mcp_config.json") {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("thiếu bản sao lưu mcp_config.json")
	}
	seen := false
	for _, st := range rep.Steps {
		if st.Name == "MCP cấp người dùng antigravity" && st.Status == StepUpdated {
			seen = true
		}
	}
	if !seen {
		t.Errorf("báo cáo thiếu bước MCP cấp người dùng: %+v", rep.Steps)
	}
	before, _ := os.ReadFile(cfg)
	mustInit(t, InitOptions{Dir: root, MCPUser: true, Hooks: no()})
	after, _ := os.ReadFile(cfg)
	if string(before) != string(after) {
		t.Errorf("chạy lại phải không đổi:\n%s\n---\n%s", before, after)
	}
}

// Phát hiện M3 .9: --update không chọn --hooks-scope thì quy tắc vào phạm vi mà hook đang được cài (project), không vào
// settings.local.json; --no-mcp gỡ quy tắc ở các phạm vi trong repository, giữ quy tắc khác.
func TestInitPermissionRuleFollowsInstalledHookScopeAndNoMCPRemoves(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes(), HooksScope: claude.ScopeProject})
	project := filepath.Join(root, ".claude", "settings.json")
	local := filepath.Join(root, ".claude", "settings.local.json")
	opts, err := (InitFlagOptions{Update: true}).ToInit(root, "bint", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !opts.HooksScopeDefault {
		t.Fatal("không truyền --hooks-scope thì HooksScopeDefault phải bật")
	}
	mustInit(t, opts)
	if countRule(readJSONFile(t, project)) != 1 {
		t.Fatalf("quy tắc phải vào settings.json (hook ở project): %v", readJSONFile(t, project)["permissions"])
	}
	if _, err := os.Stat(local); err == nil && countRule(readJSONFile(t, local)) != 0 {
		t.Fatal("không được ghi quy tắc vào settings.local.json")
	}
	// Thêm một quy tắc khác ở local rồi --no-mcp: quy tắc của Bint bị gỡ ở cả hai tệp, quy tắc khác giữ.
	if err := os.WriteFile(local, []byte(`{"permissions":{"allow":["Bash(ls:*)","`+claude.PermissionRule+`"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mustInit(t, InitOptions{Dir: root, MCP: no()})
	if countRule(readJSONFile(t, project)) != 0 || countRule(readJSONFile(t, local)) != 0 {
		t.Fatal("--no-mcp phải gỡ quy tắc ở mọi phạm vi trong repository")
	}
	if allow := readJSONFile(t, local)["permissions"].(map[string]any)["allow"].([]any); len(allow) != 1 || allow[0] != "Bash(ls:*)" {
		t.Errorf("quy tắc khác phải giữ: %v", allow)
	}
	if readJSONFile(t, project)["hooks"] == nil {
		t.Error("--no-mcp không được đụng hook")
	}
}

// Phát hiện M3 .10: dự án khai service có nhóm ghi mà không khai grants.interactive thì init cảnh báo (cùng mcp__bint__* tool ghi
// chạy không hỏi); khai interactive thì không cảnh báo.
func TestInitWarnsImplicitInteractiveWriteGrants(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: no()})
	cfgPath := filepath.Join(root, ".bint", "config.yml")
	setMCP := func(services, grants string) {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, l := range strings.Split(string(b), "\n") {
			switch {
			case strings.HasPrefix(l, "  services: "):
				l = "  services: " + services
			case strings.HasPrefix(l, "  grants: "):
				l = "  grants: " + grants
			}
			out = append(out, l)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.Join(out, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warned := func(rep *InitReport) string {
		for _, st := range rep.Steps {
			if st.Status == StepWarn && strings.Contains(st.Detail, "interactive") {
				return st.Detail
			}
		}
		return ""
	}
	setMCP("{jira: {}, codegraph: {}}", "{}")
	w := warned(mustInit(t, InitOptions{Dir: root, MCP: yes(), Hooks: no()}))
	if !strings.Contains(w, "jira.write") || strings.Contains(w, "codegraph") {
		t.Fatalf("muốn cảnh báo nêu jira.write (không nêu codegraph chỉ đọc): %q", w)
	}
	setMCP("{jira: {}, codegraph: {}}", "{interactive: [jira.read]}")
	if w := warned(mustInit(t, InitOptions{Dir: root, MCP: yes(), Hooks: no()})); w != "" {
		t.Fatalf("đã khai grants.interactive thì không cảnh báo: %q", w)
	}
}
