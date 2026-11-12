package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/harness/hookcore"
	"bint/internal/hcomp"
	"bint/internal/spool"
	"bint/internal/testutil"
)

// guardRepo dựng repository đã init với các thành phần quy trình cho trước (tệp trong .bint/harness).
func guardRepo(t *testing.T, harnesses []string, files map[string]string) string {
	t.Helper()
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"src/a.go": "package a\n", "docs/x.md": "x\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: harnesses, Hooks: no()})
	for rel, body := range files {
		p := filepath.Join(root, ".bint", "harness", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func gmeta(id, status string) string {
	return "id: " + id + "\nscope: global\nstatus: " + status + "\norigin: human\nversion: 1\n"
}

func guardEvents(t *testing.T, root string) []spool.Event {
	t.Helper()
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(ws.Paths.Spool)
	var out []spool.Event
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		evs, err := spool.ReadFile(filepath.Join(ws.Paths.Spool, e.Name()))
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.Type == spool.GuardrailTriggered {
				out = append(out, ev)
			}
		}
	}
	return out
}

func decision(t *testing.T, out string) (hso map[string]any) {
	t.Helper()
	if out == "" {
		return nil
	}
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("đầu ra không phải JSON: %q: %v", out, err)
	}
	return m["hookSpecificOutput"]
}

const stopGuard = "on: stop\nwhen: { edited: \"src/**\" }\nunless: { ran: test }\naction: remind\nmessage: Chưa chạy test.\n"

func TestGuardrailStopRemindsOnceAndRecordsFollowed(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/test-before-stop.yml": gmeta("test-before-stop", "verified") + stopGuard,
	})
	post := func(tool string, in map[string]any, id string) {
		runHook(t, hookInput("s1", "PostToolUse", root, map[string]any{"tool_name": tool, "tool_input": in, "tool_use_id": id}))
	}
	stop := func(active bool) string {
		return runHook(t, hookInput("s1", "Stop", root, map[string]any{"stop_hook_active": active})).Output
	}
	// Chưa sửa gì: không chặn.
	if out := stop(false); out != "" {
		t.Fatalf("chưa sửa thì không chặn: %s", out)
	}
	post("Edit", map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "t1")
	out := stop(false)
	var m map[string]any
	if json.Unmarshal([]byte(out), &m) != nil || m["decision"] != "block" || !strings.Contains(m["reason"].(string), "Chưa chạy test") {
		t.Fatalf("stop phải chặn kèm lý do: %s", out)
	}
	// Chặn một lần: lần stop kế (stop_hook_active) và kể cả không active đều cho qua.
	if out := stop(true); out != "" {
		t.Fatalf("remind chỉ một lần: %s", out)
	}
	if out := stop(false); out != "" {
		t.Fatalf("remind chỉ một lần mỗi phiên: %s", out)
	}
	// Agent chạy test sau lời nhắc → ghi followed.
	post("Bash", map[string]any{"command": "go test ./..."}, "t2")
	var outcomes []string
	for _, ev := range guardEvents(t, root) {
		outcomes = append(outcomes, ev.Payload["outcome"].(string))
		if ev.Payload["guardrail"] != "test-before-stop" || ev.Harness != "claude" {
			t.Fatalf("sự kiện: %+v", ev)
		}
	}
	if strings.Join(sortedStrings(outcomes), ",") != "fired,followed" {
		t.Fatalf("spool guardrail.triggered = %v", outcomes)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestGuardrailStopSatisfiedByTestAfterEdit(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/g.yml": gmeta("g", "verified") + stopGuard,
	})
	runHook(t, hookInput("s", "PostToolUse", root, map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": "src/b.go"}, "tool_use_id": "1"}))
	runHook(t, hookInput("s", "PostToolUse", root, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "go test ./src/..."}, "tool_use_id": "2"}))
	if out := runHook(t, hookInput("s", "Stop", root, nil)).Output; out != "" {
		t.Fatalf("đã chạy test sau khi sửa thì không chặn: %s", out)
	}
	if len(guardEvents(t, root)) != 0 {
		t.Fatal("không kích hoạt thì không có sự kiện guardrail")
	}
}

func TestGuardrailStopDenyCapsAtThree(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/g.yml": gmeta("g", "verified") + strings.Replace(stopGuard, "action: remind", "action: deny", 1),
	})
	runHook(t, hookInput("s", "PostToolUse", root, map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"file_path": "src/a.go"}, "tool_use_id": "1"}))
	blocks := 0
	for i := 0; i < 6; i++ {
		if out := runHook(t, hookInput("s", "Stop", root, map[string]any{"stop_hook_active": i > 0})).Output; out != "" {
			blocks++
		}
	}
	if blocks != hcomp.MaxStopDenies {
		t.Fatalf("deny ở stop chặn %d lần, muốn tối đa %d", blocks, hcomp.MaxStopDenies)
	}
	var outcomes []string
	for _, ev := range guardEvents(t, root) {
		outcomes = append(outcomes, ev.Payload["outcome"].(string))
	}
	if !strings.Contains(strings.Join(outcomes, ","), "bypass") {
		t.Fatalf("vượt trần phải ghi bypass: %v", outcomes)
	}
}

func TestGuardrailPreBashDenyAskRemind(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/no-push.yml":   gmeta("no-push", "verified") + "on: pre_bash\nwhen: { command: \"git push\" }\naction: deny\nmessage: Không push.\n",
		"guardrails/ask-rm.yml":    gmeta("ask-rm", "verified") + "on: pre_bash\nwhen: { command: \"rm -rf\" }\naction: ask\nmessage: Xoá đệ quy cần hỏi.\n",
		"guardrails/note-make.yml": gmeta("note-make", "verified") + "on: pre_bash\nwhen: { command: \"^make \" }\naction: remind\nmessage: Dùng make check.\n",
		"guardrails/cand.yml":      gmeta("cand", "candidate") + "on: pre_bash\nwhen: { command: \"ls\" }\naction: deny\nmessage: không có hiệu lực.\n",
	})
	pre := func(cmd string) map[string]any {
		return decision(t, runHook(t, hookInput("s", "PreToolUse", root, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": cmd}, "tool_use_id": cmd})).Output)
	}
	if o := pre("git push origin main"); o["permissionDecision"] != "deny" || !strings.Contains(o["permissionDecisionReason"].(string), "no-push") {
		t.Fatalf("deny: %v", o)
	}
	if o := pre("rm -rf build"); o["permissionDecision"] != "ask" {
		t.Fatalf("ask: %v", o)
	}
	o := pre("make build")
	if o["permissionDecision"] != nil || !strings.Contains(o["additionalContext"].(string), "make check") {
		t.Fatalf("remind chỉ chèn ngữ cảnh, không chặn: %v", o)
	}
	if o := pre("make build"); o != nil {
		t.Fatalf("remind chỉ một lần mỗi phiên: %v", o)
	}
	if o := pre("ls -la"); o != nil {
		t.Fatalf("candidate không có hiệu lực: %v", o)
	}
}

func TestGuardrailPreEditMergesWithProtectedPath(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/mig.yml": gmeta("mig", "verified") + "on: pre_edit\nwhen: { paths: \"migrations/**\" }\naction: remind\nmessage: Thêm migration mới, đừng sửa cũ.\n",
	})
	out := runHook(t, hookInput("s", "PreToolUse", root, map[string]any{"tool_name": "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "migrations", "001.sql")}, "tool_use_id": "e1"})).Output
	o := decision(t, out)
	// migrations/** nằm trong limits.protected_paths mặc định: vừa hỏi người, vừa nhắc.
	if o["permissionDecision"] != "ask" || !strings.Contains(o["additionalContext"].(string), "Thêm migration mới") {
		t.Fatalf("gộp guardrail với đường dẫn bảo vệ: %s", out)
	}
	// Tệp ngoài vùng: im lặng.
	if out := runHook(t, hookInput("s", "PreToolUse", root, map[string]any{"tool_name": "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "tool_use_id": "e2"})).Output; out != "" {
		t.Fatalf("không khớp: %s", out)
	}
}

func TestCodexProtectedPathDenyWritesChildRunEvent(t *testing.T) {
	root := guardRepo(t, []string{"codex"}, nil)
	t.Setenv(hookcore.EnvRunID, "child-denied")
	in := hookInput("session", "PreToolUse", root, map[string]any{"tool_name": "apply_patch",
		"tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: migrations/001.sql\n*** End Patch"}, "tool_use_id": "tool-1"})
	res, err := codex.HandleHook(context.Background(), strings.NewReader(in), hookcore.Env{BintVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := decision(t, res.Output)["permissionDecision"]; got != "deny" {
		t.Fatalf("Codex must deny protected path: %s", res.Output)
	}
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	events, err := spool.Denials(ws.Paths.Spool, "child-denied")
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == spool.ToolDenied {
			paths, _ := ev.Payload["protected_paths"].([]any)
			if len(paths) == 1 && paths[0] == "migrations/001.sql" {
				return
			}
		}
	}
	t.Fatalf("missing child-scoped denial event: %+v", events)
}

func TestCodexGuardrailDeniesWriteDurableWorkflowEvents(t *testing.T) {
	root := guardRepo(t, []string{"codex"}, map[string]string{
		"guardrails/no-edit.yml": gmeta("no-edit", "verified") + "on: pre_edit\nwhen: { paths: \"src/**\" }\naction: deny\nmessage: Không sửa mã.\n",
		"guardrails/no-push.yml": gmeta("no-push", "verified") + "on: pre_bash\nwhen: { command: \"git push\" }\naction: deny\nmessage: Không push.\n",
	})
	t.Setenv(hookcore.EnvRunID, "child-guardrail")
	for _, in := range []string{
		hookInput("session", "PreToolUse", root, map[string]any{"tool_name": "apply_patch",
			"tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: src/a.go\n@@\n-package a\n+package b\n*** End Patch"}, "tool_use_id": "edit-1"}),
		hookInput("session", "PreToolUse", root, map[string]any{"tool_name": "Bash",
			"tool_input": map[string]any{"command": "git push origin main"}, "tool_use_id": "bash-1"}),
	} {
		res, err := codex.HandleHook(context.Background(), strings.NewReader(in), hookcore.Env{BintVersion: "test"})
		if err != nil || decision(t, res.Output)["permissionDecision"] != "deny" {
			t.Fatalf("expected Codex deny, got %q: %v", res.Output, err)
		}
	}
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	events, err := spool.Denials(ws.Paths.Spool, "child-guardrail")
	if err != nil || len(events) != 2 {
		t.Fatalf("expected durable pre_edit and pre_bash denial events, got %d: %v", len(events), err)
	}
	foundEditPath := false
	for _, ev := range events {
		if got, _ := ev.Payload["paths"].([]any); len(got) == 1 && got[0] == "src/a.go" {
			foundEditPath = true
		}
	}
	if !foundEditPath {
		t.Fatalf("pre_edit affected path missing: %+v", events)
	}
}

func TestGuardrailBrokenYAMLNeverBlocks(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/broken.yml": "id: broken\n  : : [\n",
		"guardrails/badre.yml":  gmeta("badre", "verified") + "on: pre_bash\nwhen: { command: \"(\" }\naction: deny\nmessage: m\n",
	})
	for _, in := range []string{
		hookInput("s", "PreToolUse", root, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git push"}, "tool_use_id": "1"}),
		hookInput("s", "PostToolUse", root, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "go test"}, "tool_use_id": "2"}),
		hookInput("s", "Stop", root, nil),
	} {
		res, err := claudeHandle(in)
		if err != nil || res.Output != "" {
			t.Fatalf("guardrail hỏng không được chặn hay trả lỗi: %q %v", res.Output, err)
		}
	}
}

func claudeHandle(in string) (hookcore.Result, error) {
	return claude.HandleHook(context.Background(), strings.NewReader(in), claude.HookEnv{BintVersion: "test"})
}

func TestGuardrailDegradesOnCodexAndAntigravity(t *testing.T) {
	files := map[string]string{
		"guardrails/ask-rm.yml":    gmeta("ask-rm", "verified") + "on: pre_bash\nwhen: { command: \"rm -rf\" }\naction: ask\nmessage: Hỏi trước.\n",
		"guardrails/note-make.yml": gmeta("note-make", "verified") + "on: pre_bash\nwhen: { command: \"^make \" }\naction: remind\nmessage: Dùng make check.\n",
		"guardrails/no-push.yml":   gmeta("no-push", "verified") + "on: pre_bash\nwhen: { command: \"git push\" }\naction: deny\nmessage: Không push.\n",
		"guardrails/stop.yml":      gmeta("stop", "verified") + stopGuard,
	}
	t.Run("codex", func(t *testing.T) {
		root := guardRepo(t, []string{"codex"}, files)
		pre := func(cmd string) map[string]any {
			return decision(t, codexHook(t, map[string]any{"session_id": "c1", "hook_event_name": "PreToolUse", "cwd": root,
				"tool_name": "Bash", "tool_input": map[string]any{"command": cmd}, "tool_use_id": "c-" + cmd}).Output)
		}
		// ask hạ xuống remind (Codex không hỏi được): chỉ chèn ngữ cảnh.
		if o := pre("rm -rf x"); o["permissionDecision"] != nil || !strings.Contains(o["additionalContext"].(string), "Hỏi trước") {
			t.Fatalf("ask → remind: %v", o)
		}
		if o := pre("git push"); o["permissionDecision"] != "deny" {
			t.Fatalf("deny giữ nguyên: %v", o)
		}
		// Không có Stop: không giả lập.
		r := codexHook(t, map[string]any{"session_id": "c1", "hook_event_name": "Stop", "cwd": root})
		if r.Output != "" {
			t.Fatalf("Codex không có Stop: %q", r.Output)
		}
	})
	t.Run("antigravity", func(t *testing.T) {
		root := guardRepo(t, []string{"antigravity"}, files)
		pre := func(cmd string) map[string]any {
			res := agyHook(t, "PreToolUse", map[string]any{"conversationId": "a1", "workspacePaths": []string{root},
				"toolCall": map[string]any{"name": "run_command", "args": map[string]any{"CommandLine": cmd}}, "stepIdx": 1})
			var m map[string]any
			if err := json.Unmarshal([]byte(res.Output), &m); err != nil {
				t.Fatal(err)
			}
			return m
		}
		if o := pre("rm -rf x"); o["decision"] != "ask" {
			t.Fatalf("ask: %v", o)
		}
		if o := pre("git push"); o["decision"] != "deny" {
			t.Fatalf("deny: %v", o)
		}
		// remind bị bỏ: PreToolUse không chèn được ngữ cảnh.
		if o := pre("make all"); o["decision"] != "" {
			t.Fatalf("remind bị bỏ: %v", o)
		}
	})
}

func TestGuardrailDomainGuidanceInjectedOncePerSession(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guidance/db-tips.md": "---\nid: db-tips\nscope: \"domain:src/**\"\nstatus: verified\norigin: human\nversion: 1\n---\nVùng src: luôn chạy gofmt.\n",
		"guidance/cand.md":    "---\nid: cand\nscope: \"domain:src/**\"\nstatus: candidate\norigin: agent\nversion: 1\n---\nChưa duyệt.\n",
	})
	pre := func(id string) string {
		return runHook(t, hookInput("s", "PreToolUse", root, map[string]any{"tool_name": "Edit",
			"tool_input": map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "tool_use_id": id})).Output
	}
	out := pre("1")
	if !strings.Contains(out, "luôn chạy gofmt") || strings.Contains(out, "Chưa duyệt") {
		t.Fatalf("guidance riêng verified phải được chèn, candidate thì không: %s", out)
	}
	if out := pre("2"); strings.Contains(out, "gofmt") {
		t.Fatalf("chèn một lần mỗi phiên: %s", out)
	}
}

func TestAgentsBlockAppendsVerifiedGlobalGuidance(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, nil)
	ws, err := OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := AgentsBlock(root, ws.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(base, "Công cụ tri thức") {
		t.Fatalf("guidance dựng sẵn của eval không vào AGENTS.md thật:\n%s", base)
	}
	write := func(rel, body string) {
		p := filepath.Join(root, ".bint", "harness", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("guidance/run-tests.md", "---\n"+gmeta("run-tests", "verified")+"title: Chạy test\n---\nChạy `go test` của gói đã sửa.\n")
	write("guidance/draft.md", "---\n"+gmeta("draft", "candidate")+"---\nNháp chưa duyệt.\n")
	write("guidance/dom.md", "---\nid: dom\nscope: \"domain:src/**\"\nstatus: verified\norigin: human\nversion: 1\n---\nRiêng vùng src.\n")
	got, _, err := AgentsBlock(root, ws.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, strings.TrimRight(base, "\n")) || !strings.Contains(got, "## Chạy test\n\nChạy `go test` của gói đã sửa.") {
		t.Fatalf("thiếu guidance chung đã duyệt:\n%s", got)
	}
	if strings.Contains(got, "Nháp chưa duyệt") || strings.Contains(got, "Riêng vùng src") {
		t.Fatalf("candidate và guidance riêng không vào khối chung:\n%s", got)
	}
}
