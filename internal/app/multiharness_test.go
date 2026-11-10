package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/harness/antigravity"
	"bint/internal/harness/codex"
	"bint/internal/harness/hookcore"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/testutil"
)

func TestRealCodexTestOutcomeIsNotAssumedFromPostToolUse(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"calc.go": "package probe2\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"codex"}, Hooks: no()})
	for _, tc := range []struct {
		name        string
		known, want bool
	}{{"failed", true, false}, {"passed", true, true}, {"missing", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.name
			if name == "missing" {
				name = "failed"
			}
			b, err := os.ReadFile("../harness/codex/testdata/codex-0.167/test-" + name + "-hook.json")
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err = json.Unmarshal(b, &m); err != nil {
				t.Fatal(err)
			}
			m["cwd"] = root
			trace := filepath.Join(t.TempDir(), "transcript.jsonl")
			m["transcript_path"] = trace
			if tc.known {
				b, err = os.ReadFile("../harness/codex/testdata/codex-0.167/test-" + name + "-transcript.jsonl")
				if err != nil {
					t.Fatal(err)
				}
				os.WriteFile(trace, b, 0o600)
			}
			r := codexHook(t, m)
			if !r.Written {
				t.Fatalf("không ghi test command: %+v", r)
			}
			events, err := spool.ReadFile(r.SpoolFile)
			if err != nil || len(events) != 1 {
				t.Fatalf("events: %v %v", events, err)
			}
			got, known := events[0].Payload["ok"].(bool)
			if known != tc.known || tc.known && got != tc.want {
				t.Fatalf("%s: ok=%v known=%v; muốn ok=%v known=%v", tc.name, got, known, tc.want, tc.known)
			}
		})
	}
}

func TestCodexWritingTestInstructionsIsNotATestRun(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"calc.go": "package p\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"codex"}, Hooks: no()})
	b, err := os.ReadFile("../harness/codex/testdata/codex-0.167/planning-command.txt")
	if err != nil {
		t.Fatal(err)
	}
	r := codexHook(t, map[string]any{"session_id": "planning", "cwd": root, "hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_use_id": "planning-docs", "tool_input": map[string]any{"command": string(b)}})
	if r.Written {
		t.Fatalf("đã ghi test.completed dù shell chỉ ghi tài liệu: %+v", r)
	}
}

func TestInitBootstrapHintMatchesDefaultHarness(t *testing.T) {
	testutil.Isolate(t)
	for _, name := range []string{"codex", "claude"} {
		root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
		r := mustInit(t, InitOptions{Dir: root, Harnesses: []string{name}, Hooks: no()})
		steps := strings.Join(r.NextSteps, "\n")
		if name == "codex" && !strings.Contains(steps, "$b-bootstrap trong Codex") {
			t.Fatalf("Codex nhận hướng dẫn sai harness: %s", steps)
		}
		if name == "claude" && !strings.Contains(steps, "/b-bootstrap trong Claude Code") {
			t.Fatal("đổi hướng dẫn Claude mặc định")
		}
	}
}

func hashTree(t *testing.T, root string, rels ...string) string {
	t.Helper()
	h := sha256.New()
	sort.Strings(rels)
	for _, r := range rels {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(r)))
		h.Write([]byte(r))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// P5: cùng một nguồn skill và tri thức được xuất bản cho ba harness; hook của từng
// harness được cài mà không chạm cấu hình khác; chạy lại không đổi gì.
func TestMultiHarnessInit(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	// Hook sẵn có của người dùng trong .agents/hooks.json phải được giữ.
	os.MkdirAll(filepath.Join(root, ".agents"), 0o755)
	os.WriteFile(filepath.Join(root, ".agents", "hooks.json"), []byte(`{"lint":{"PostToolUse":[{"matcher":"run_command","hooks":[{"command":"./lint.sh"}]}]}}`), 0o644)
	os.MkdirAll(filepath.Join(root, ".codex"), 0o755)
	os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte("model = \"x\"\n"), 0o644)

	rep := mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex", "antigravity"}, Hooks: yes(), MCP: yes()})
	if strings.Join(rep.Harnesses, ",") != "claude,codex,antigravity" {
		t.Fatalf("harnesses = %v", rep.Harnesses)
	}
	files := []string{
		".claude/skills/b-req/SKILL.md", ".agents/skills/b-req/SKILL.md", ".agents/skills/b-do/SKILL.md",
		".claude/settings.local.json", ".codex/hooks.json", ".agents/hooks.json", ".codex/config.toml", ".mcp.json", ".gitignore",
		".bint/skills/core.lock",
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("thiếu %s", f)
		}
	}
	// Định dạng .agents: frontmatter chỉ name + description, thân giữ nguyên.
	sk := read(t, filepath.Join(root, ".agents/skills/b-req/SKILL.md"))
	parts := strings.SplitN(sk, "---\n", 3)
	var fm map[string]any
	if len(parts) != 3 || yaml.Unmarshal([]byte(parts[1]), &fm) != nil || len(fm) != 2 || fm["name"] != "b-req" || fm["description"] == "" {
		t.Fatalf("frontmatter .agents sai: %q", parts[1])
	}
	if !strings.Contains(read(t, filepath.Join(root, ".claude/skills/b-req/SKILL.md")), "disable-model-invocation: true") {
		t.Fatal("skill Claude mất disable-model-invocation")
	}
	if !strings.HasSuffix(sk, strings.SplitN(read(t, filepath.Join(root, ".claude/skills/b-req/SKILL.md")), "---\n", 3)[2]) {
		t.Fatal("thân skill .agents khác bản Claude")
	}
	if !strings.Contains(read(t, filepath.Join(root, ".bint/skills/core.lock")), "agents:") {
		t.Fatal("core.lock thiếu băm định dạng agents")
	}
	ag := read(t, filepath.Join(root, ".agents/hooks.json"))
	if !strings.Contains(ag, "./lint.sh") || !strings.Contains(ag, "bint hook antigravity PreToolUse") {
		t.Fatalf(".agents/hooks.json: %s", ag)
	}
	cx := read(t, filepath.Join(root, ".codex/hooks.json"))
	if !strings.Contains(cx, "bint hook codex") || !strings.Contains(cx, `"apply_patch|Bash|shell|exec_command"`) {
		t.Fatalf(".codex/hooks.json: %s", cx)
	}
	toml := read(t, filepath.Join(root, ".codex/config.toml"))
	if !strings.HasPrefix(toml, "model = \"x\"\n") || !strings.Contains(toml, "[mcp_servers.bint]") || !codex.MCPInstalled(root) {
		t.Fatalf(".codex/config.toml: %s", toml)
	}
	gi := read(t, filepath.Join(root, ".gitignore"))
	for _, want := range []string{".claude/settings.local.json", ".codex/hooks.json", ".agents/hooks.json"} {
		if !strings.Contains(gi, want) {
			t.Fatalf(".gitignore thiếu %s:\n%s", want, gi)
		}
	}

	// Chạy lại: không đổi tệp nào.
	before := hashTree(t, root, files...)
	mustInit(t, InitOptions{Dir: root, Hooks: yes(), MCP: yes()})
	if hashTree(t, root, files...) != before {
		t.Fatal("init lần hai thay đổi tệp")
	}

	// Gỡ hook và MCP: tệp của người dùng còn nguyên, không còn dấu vết Bint.
	mustInit(t, InitOptions{Dir: root, Hooks: no(), MCP: no()})
	ag = read(t, filepath.Join(root, ".agents/hooks.json"))
	if strings.Contains(ag, "bint") || !strings.Contains(ag, "./lint.sh") {
		t.Fatalf("gỡ hook antigravity: %s", ag)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex/hooks.json")); err == nil && strings.Contains(read(t, filepath.Join(root, ".codex/hooks.json")), "bint") {
		t.Fatal("hook codex còn sót")
	}
	if toml := read(t, filepath.Join(root, ".codex/config.toml")); toml != "model = \"x\"\n" {
		t.Fatalf("gỡ MCP codex: %q", toml)
	}

	// doctor báo từng harness.
	d := Doctor(context.Background(), root)
	names := map[string]bool{}
	for _, c := range d.Checks {
		names[c.Name] = true
	}
	for _, n := range []string{"hook codex", "hook antigravity", "hook claude"} {
		if !names[n] {
			t.Errorf("doctor thiếu %q", n)
		}
	}
}

func codexHook(t *testing.T, m map[string]any) hookcore.Result {
	t.Helper()
	b, _ := json.Marshal(m)
	res, err := codex.HandleHook(context.Background(), strings.NewReader(string(b)), hookcore.Env{BintVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRealCodexDenialExplainsApproval(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"codex"}, Hooks: no()})
	t.Setenv("BINT_TASK_ID", "01M72Y6VA6NQ42T45QCVDXBF50")
	b, err := os.ReadFile("../harness/codex/testdata/codex-0.167/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Stdin map[string]any `json:"stdin"`
	}
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		m := record.Stdin
		input, _ := m["tool_input"].(map[string]any)
		patch, _ := input["command"].(string)
		if m["hook_event_name"] != "PreToolUse" || !strings.Contains(patch, "migrations/001.sql") {
			continue
		}
		m["cwd"] = root
		input["command"] = strings.ReplaceAll(patch, "/repo", root)
		r := codexHook(t, m)
		if !strings.Contains(r.Output, "deny") || !strings.Contains(r.Output, "--path 'migrations/001.sql'") || !strings.Contains(r.Output, "bint run resume") {
			t.Fatalf("deny không có cách tiếp tục: %s", r.Output)
		}
		return
	}
	t.Fatal("thiếu fixture deny thật")
}

func agyHook(t *testing.T, event string, m map[string]any) hookcore.Result {
	t.Helper()
	b, _ := json.Marshal(m)
	res, err := antigravity.HandleHook(context.Background(), event, strings.NewReader(string(b)), hookcore.Env{BintVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// P5: sự kiện của Codex và Antigravity được chuẩn hoá về cùng mô hình lượt chạy;
// sự kiện harness không có (kết thúc phiên Antigravity) không bị tổng hợp giả.
func TestCodexAndAntigravityHooks(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex", "antigravity"}, Hooks: no()})

	// Payload đúng khuôn đã bắt được từ codex-cli 0.167.
	base := map[string]any{"session_id": "cx-1", "cwd": root, "model": "gpt-x", "permission_mode": "bypassPermissions"}
	with := func(kv map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range kv {
			out[k] = v
		}
		return out
	}
	if r := codexHook(t, with(map[string]any{"hook_event_name": "SessionStart", "source": "startup"})); !r.Written {
		t.Fatalf("codex start: %+v", r)
	}
	patch := "*** Begin Patch\n*** Add File: " + filepath.Join(root, "b.go") + "\n+package a\n*** Update File: a.go\n@@\n-package a\n+package a // x\n*** End Patch"
	if r := codexHook(t, with(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "apply_patch", "tool_use_id": "e1",
		"tool_input": map[string]any{"command": patch}, "tool_response": "Success"})); !r.Written {
		t.Fatalf("codex apply_patch: %+v", r)
	}
	// Giữ kỳ vọng test qua, bổ sung đúng bằng chứng exit_code mà payload Codex thiếu.
	trace, err := os.ReadFile("../harness/codex/testdata/codex-0.167/test-passed-transcript.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(t.TempDir(), "codex.jsonl")
	os.WriteFile(tracePath, trace, 0o600)
	if r := codexHook(t, with(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_use_id": "exec-5ec2f312-e1d8-473b-8579-579b517a59a0", "transcript_path": tracePath,
		"tool_input": map[string]any{"command": "go test ./..."}})); !r.Written {
		t.Fatalf("codex test: %+v", r)
	}
	if r := codexHook(t, with(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_use_id": "e3",
		"tool_input": map[string]any{"command": "ls"}})); r.Written {
		t.Fatal("codex ls được ghi")
	}
	// Đường dẫn được bảo vệ: Codex nhận deny kèm lý do (không hỏi được trong codex exec).
	r := codexHook(t, with(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "apply_patch", "tool_use_id": "e4",
		"tool_input": map[string]any{"command": "*** Begin Patch\n*** Add File: migrations/001.sql\n+x\n*** End Patch"}}))
	if !strings.Contains(r.Output, `"permissionDecision":"deny"`) || !strings.Contains(r.Output, "migrations/001.sql") {
		t.Fatalf("codex protected: %+v", r)
	}
	codexHook(t, with(map[string]any{"hook_event_name": "SessionEnd", "reason": "other"}))

	// Antigravity: payload camelCase, tên sự kiện qua đối số.
	common := map[string]any{"conversationId": "ag-1", "workspacePaths": []string{root}, "modelName": "gemini-x"}
	agw := func(kv map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range common {
			out[k] = v
		}
		for k, v := range kv {
			out[k] = v
		}
		return out
	}
	if r := agyHook(t, "SessionStart", agw(nil)); !r.Written || r.Output != "{}" {
		t.Fatalf("agy start: %+v", r)
	}
	if r := agyHook(t, "PostToolUse", agw(map[string]any{"stepIdx": 3, "toolCall": map[string]any{"name": "write_to_file",
		"args": map[string]any{"TargetFile": filepath.Join(root, "c.go"), "CodeContent": "package a"}}})); !r.Written {
		t.Fatalf("agy write: %+v", r)
	}
	if r := agyHook(t, "PostToolUse", agw(map[string]any{"stepIdx": 4, "error": "exit status 1", "toolCall": map[string]any{
		"name": "run_command", "args": map[string]any{"CommandLine": "go test ./..."}}})); !r.Written {
		t.Fatalf("agy test fail: %+v", r)
	}
	if r := agyHook(t, "PreToolUse", agw(map[string]any{"stepIdx": 5, "toolCall": map[string]any{"name": "replace_file_content",
		"args": map[string]any{"TargetFile": "migrations/002.sql"}}})); !strings.Contains(r.Output, `"decision":"ask"`) {
		t.Fatalf("agy protected: %+v", r)
	}
	if r := agyHook(t, "PreToolUse", agw(map[string]any{"stepIdx": 6, "toolCall": map[string]any{"name": "view_file",
		"args": map[string]any{"AbsolutePath": filepath.Join(root, "a.go")}}})); r.Written || r.Output != `{"decision":""}` {
		t.Fatalf("agy view_file phải trung tính: %+v", r)
	}
	if r := agyHook(t, "Stop", agw(map[string]any{"terminationReason": "NO_TOOL_CALL"})); r.Written || r.Output != "{}" {
		t.Fatalf("agy Stop không được ghi thành kết thúc phiên: %+v", r)
	}

	ws := mustWS(t, root)
	s, err := ws.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := ws.Ingest(context.Background(), s, time.Now()); err != nil {
		t.Fatal(err)
	}
	runs, err := runlog.ListRuns(context.Background(), s.DB, 10)
	if err != nil {
		t.Fatal(err)
	}
	byH := map[string]runlog.Run{}
	for _, r := range runs {
		byH[r.Harness.String] = r
	}
	cr, ar := byH["codex"], byH["antigravity"]
	if cr.Status != "completed" || cr.FilesModified != 2 || cr.TestsOK != 1 || cr.Model.String != "gpt-x" {
		t.Errorf("codex run = %+v", cr)
	}
	if ar.Status != "running" || ar.FilesModified != 1 || ar.TestsFailed != 1 || ar.Model.String != "gemini-x" {
		t.Errorf("antigravity run = %+v", ar)
	}
}
