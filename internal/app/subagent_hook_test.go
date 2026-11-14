package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/spool"
	"bint/internal/testutil"
)

func subagentRepo(t *testing.T) (root string, ws *Workspace) {
	t.Helper()
	testutil.Isolate(t)
	root = testutil.NewRepo(t, map[string]string{"src/a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no()})
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	return root, ws
}

func spoolEventsOf(t *testing.T, ws *Workspace, typ string) []spool.Event {
	t.Helper()
	ents, _ := os.ReadDir(ws.Paths.Spool)
	var out []spool.Event
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		evs, err := spool.ReadFile(filepath.Join(ws.Paths.Spool, e.Name()))
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.Type == typ {
				out = append(out, ev)
			}
		}
	}
	return out
}

func preTool(root, agentType, agentID, tool string, in map[string]any, id string) string {
	extra := map[string]any{"tool_name": tool, "tool_input": in, "tool_use_id": id}
	if agentType != "" {
		extra["agent_type"] = agentType
	}
	if agentID != "" {
		extra["agent_id"] = agentID
	}
	return hookInput("sess-sub", "PreToolUse", root, extra)
}

// Guardrail chỉ đọc 6.4a: reviewer + touch x ⇒ deny kèm lý do và sự kiện guardrail.triggered; lệnh khớp readonly_tools hoặc
// agent_type khác/không có ⇒ không can thiệp; Edit/Write của planner ⇒ deny; không mở SQLite, không trả lỗi.
func TestReadonlyGuardrailForSubagents(t *testing.T) {
	root, ws := subagentRepo(t)
	dbBefore, dbErr := os.Stat(ws.Paths.DB)
	denyReason := func(out string) string {
		h := decision(t, out)
		if h == nil || h["permissionDecision"] != "deny" {
			return ""
		}
		r, _ := h["permissionDecisionReason"].(string)
		return r
	}

	res := runHook(t, preTool(root, "bint-reviewer", "agent-r1", "Bash", map[string]any{"command": "touch x"}, "tu1"))
	if r := denyReason(res.Output); !strings.Contains(r, "bint-reviewer") || !strings.Contains(r, "touch x") {
		t.Fatalf("touch x của reviewer phải bị deny kèm lý do: %q", res.Output)
	}
	evs := spoolEventsOf(t, ws, spool.GuardrailTriggered)
	if len(evs) != 1 {
		t.Fatalf("muốn đúng một guardrail.triggered, được %d", len(evs))
	}
	p := evs[0].Payload
	if p["guardrail"] != "bint-readonly" || p["outcome"] != "fired" || p["on"] != "pre_bash" || p["action"] != "deny" ||
		p["blocked"] != true || p["agent_type"] != "bint-reviewer" || p["agent_id"] != "agent-r1" || p["command"] != "touch x" {
		t.Fatalf("payload guardrail: %+v", p)
	}
	if evs[0].Harness != "claude" || evs[0].SessionID != "sess-sub" {
		t.Fatalf("sự kiện: %+v", evs[0])
	}

	// Lệnh khớp readonly_tools: không can thiệp (không deny, không sự kiện mới).
	for _, cmd := range []string{"git status", hookcore.CdPrefix(root) + "git diff", "bint task show X"} {
		out := runHook(t, preTool(root, "bint-reviewer", "agent-r1", "Bash", map[string]any{"command": cmd}, "tu-ok")).Output
		if denyReason(out) != "" {
			t.Fatalf("%q không được bị deny: %s", cmd, out)
		}
	}
	// agent_type khác hoặc không có: không can thiệp.
	for _, at := range []string{"", "general-purpose", "bint-implementer"} {
		out := runHook(t, preTool(root, at, "", "Bash", map[string]any{"command": "touch x"}, "tu-other")).Output
		if denyReason(out) != "" {
			t.Fatalf("agent_type %q không được bị deny: %s", at, out)
		}
		out = runHook(t, preTool(root, at, "", "Write", map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "tu-w")).Output
		if denyReason(out) != "" {
			t.Fatalf("agent_type %q: Write không được bị deny: %s", at, out)
		}
	}
	// Edit/Write/NotebookEdit của planner (kể cả đường dẫn ngoài repository) ⇒ deny.
	for _, c := range []struct {
		tool string
		in   map[string]any
	}{
		{"Edit", map[string]any{"file_path": filepath.Join(root, "src", "a.go")}},
		{"Write", map[string]any{"file_path": "/tmp/ngoai-repo.txt"}},
		{"NotebookEdit", map[string]any{"notebook_path": filepath.Join(root, "n.ipynb")}},
	} {
		out := runHook(t, preTool(root, "bint-planner", "agent-p1", c.tool, c.in, "tu-"+c.tool)).Output
		if denyReason(out) == "" {
			t.Fatalf("%s của planner phải bị deny: %q", c.tool, out)
		}
	}
	if n := len(spoolEventsOf(t, ws, spool.GuardrailTriggered)); n != 4 {
		t.Fatalf("muốn 4 sự kiện guardrail (1 Bash + 3 sửa tệp), được %d", n)
	}
	// Không mở SQLite: tệp CSDL không xuất hiện/đổi.
	dbAfter, dbErr2 := os.Stat(ws.Paths.DB)
	if (dbErr == nil) != (dbErr2 == nil) || dbErr == nil && (dbBefore.ModTime() != dbAfter.ModTime() || dbBefore.Size() != dbAfter.Size()) {
		t.Fatalf("hook không được mở/ghi SQLite: trước %v %v, sau %v %v", dbBefore, dbErr, dbAfter, dbErr2)
	}
}

// Dialect không khai SubagentReadonly (Codex, Antigravity, harness giả) thì guardrail không áp dù agent_type trùng tên.
func TestReadonlyGuardrailNeedsDialectCapability(t *testing.T) {
	root, _ := subagentRepo(t)
	d := claude.Dialect
	d.SubagentReadonly = false
	raw := []byte(preTool(root, "bint-reviewer", "a", "Bash", map[string]any{"command": "touch x"}, "tu1"))
	hi, err := hookcore.NormalizeClaudeStyle(raw)
	if err != nil {
		t.Fatal(err)
	}
	res, err := hookcore.Handle(context.Background(), hi, d, hookcore.Env{})
	if err != nil || strings.Contains(res.Output, "deny") {
		t.Fatalf("Dialect không hỗ trợ ⇒ không can thiệp: %+v %v", res, err)
	}
}

// Sự kiện công cụ của subagent mang agent_id và agent_type trong payload; SubagentStop ghi subagent.stopped kèm đường dẫn
// transcript và tệp runtime/subagents/<agent_id>.json.
func TestSubagentStopEventAndRuntimeRecord(t *testing.T) {
	root, ws := subagentRepo(t)
	runHook(t, hookInput("sess-sub", "PostToolUse", root, map[string]any{"tool_name": "Edit", "tool_use_id": "tu9",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "agent_id": "agent-i1", "agent_type": "bint-implementer"}))
	mod := spoolEventsOf(t, ws, spool.FileModified)
	if len(mod) != 1 || mod[0].Payload["agent_id"] != "agent-i1" || mod[0].Payload["agent_type"] != "bint-implementer" {
		t.Fatalf("file.modified của subagent: %+v", mod)
	}
	tr := filepath.Join(root, "transcripts", "agent-agent-i1.jsonl")
	runHook(t, hookInput("sess-sub", "SubagentStop", root, map[string]any{"agent_id": "agent-i1", "agent_type": "bint-implementer",
		"agent_transcript_path": tr}))
	stops := spoolEventsOf(t, ws, spool.SubagentStopped)
	if len(stops) != 1 {
		t.Fatalf("muốn một subagent.stopped, được %d", len(stops))
	}
	p := stops[0].Payload
	if p["agent_id"] != "agent-i1" || p["agent_type"] != "bint-implementer" || p["agent_transcript_path"] != tr {
		t.Fatalf("payload subagent.stopped: %+v", p)
	}
	rec, ok := hookcore.ReadSubagentRecord(ws.Paths.Runtime, "agent-i1")
	if !ok || rec.TranscriptPath != tr || rec.AgentType != "bint-implementer" {
		t.Fatalf("tệp runtime: %+v %v", rec, ok)
	}
	// agent_id lạ (đường dẫn thoát thư mục) không tạo tệp ngoài runtime/subagents.
	runHook(t, hookInput("sess-sub", "SubagentStop", root, map[string]any{"agent_id": "../../evil", "agent_transcript_path": tr}))
	if _, err := os.Stat(filepath.Join(ws.Paths.Runtime, "..", "evil.json")); err == nil {
		t.Fatal("agent_id không hợp lệ không được ghi tệp")
	}
	// SubagentStop không agent_id: bỏ qua, không lỗi.
	res := runHook(t, hookInput("sess-sub", "SubagentStop", root, nil))
	if res.Written {
		t.Fatalf("thiếu agent_id không được ghi: %+v", res)
	}
}
