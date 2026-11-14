package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/spool"
)

// Guardrail bint-worktree (docs/isolation-subagent-plan.md mục 12.14), qua hook thật: ánh xạ agent_id ⇒ worktree bằng dòng
// đầu transcript của subagent + tệp runtime/session-steps, không cần `attach`.
type wtFixture struct {
	root, wt, tdir string
	ws             *Workspace
}

func newWtFixture(t *testing.T) *wtFixture {
	t.Helper()
	root, ws := subagentRepo(t)
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	return &wtFixture{root: root, wt: wt, tdir: filepath.Join(base, "projects", "p"), ws: ws}
}

const wtSession = "sess-sub"

// transcript ghi dòng đầu của subagent agentID theo bố cục thật; prompt rỗng ⇒ không tạo tệp. Trả transcript_path của phiên cha.
func (f *wtFixture) transcript(t *testing.T, agentID, prompt string) string {
	t.Helper()
	parent := filepath.Join(f.tdir, wtSession+".jsonl")
	if prompt == "" {
		return parent
	}
	sub := filepath.Join(f.tdir, wtSession, "subagents", "agent-"+agentID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(map[string]any{"type": "user", "agentId": agentID, "message": map[string]any{"role": "user", "content": prompt}})
	if err := os.WriteFile(sub, append(line, '\n', '{', '}', '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return parent
}

func (f *wtFixture) step(t *testing.T, run, task string, isolated bool, until time.Time) {
	t.Helper()
	wt := f.wt
	if !isolated {
		wt = f.root
	}
	if err := hookcore.WriteSessionStep(f.ws.Paths.Runtime, hookcore.SessionStepRecord{RunID: run, TaskID: task, Step: "b-feat",
		Subagent: "bint-implementer", Worktree: wt, Isolated: isolated, Until: until.UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
}

func wtPrompt(task, run string) string {
	return "thân skill\n\n---\n\n<bint-task id=\"" + task + "\" step=\"b-feat\" run=\"" + run + "\">\nphiếu…\n"
}

// call chạy PreToolUse của subagent loại agentType; trả lý do deny ("" nếu không deny).
func (f *wtFixture) call(t *testing.T, agentType, tool string, in map[string]any, parent string) (reason string) {
	t.Helper()
	extra := map[string]any{"tool_name": tool, "tool_input": in, "tool_use_id": "tu-" + tool, "agent_id": "a1", "transcript_path": parent}
	if agentType != "" {
		extra["agent_type"] = agentType
	}
	res := runHook(t, hookInput(wtSession, "PreToolUse", f.root, extra))
	h := decision(t, res.Output)
	if h == nil || h["permissionDecision"] != "deny" {
		return ""
	}
	r, _ := h["permissionDecisionReason"].(string)
	return r
}

func TestWorktreeGuardrailMapped(t *testing.T) {
	f := newWtFixture(t)
	dbBefore, dbErr := os.Stat(f.ws.Paths.DB)
	now := time.Now()
	f.step(t, "RUNISO", "TISO", true, now.Add(time.Hour))
	f.step(t, "RUNSH", "TSH", false, now.Add(time.Hour))
	iso := f.transcript(t, "a1", wtPrompt("TISO", "RUNISO"))
	outside := filepath.Join(f.root, "src", "a.go")
	inside := filepath.Join(f.wt, "src", "a.go")

	for _, tool := range []struct{ name, key string }{{"Edit", "file_path"}, {"Write", "file_path"}, {"NotebookEdit", "notebook_path"}} {
		r := f.call(t, "bint-implementer", tool.name, map[string]any{tool.key: outside}, iso)
		if !strings.Contains(r, f.wt) || !strings.Contains(r, "đường dẫn tuyệt đối") {
			t.Fatalf("%s ra cây gốc phải bị deny kèm worktree: %q", tool.name, r)
		}
		if r := f.call(t, "bint-implementer", tool.name, map[string]any{tool.key: inside}, iso); r != "" {
			t.Fatalf("%s trong worktree không được can thiệp: %q", tool.name, r)
		}
	}
	evs := spoolEventsOf(t, f.ws, spool.GuardrailTriggered)
	if len(evs) != 3 {
		t.Fatalf("muốn 3 guardrail.triggered, được %d", len(evs))
	}
	p := evs[0].Payload
	if p["guardrail"] != "bint-worktree" || p["outcome"] != "fired" || p["on"] != "pre_edit" || p["action"] != "deny" || p["blocked"] != true ||
		p["agent_id"] != "a1" || p["agent_type"] != "bint-implementer" || p["task_id"] != "TISO" || p["run_id"] != "RUNISO" || p["worktree"] != f.wt {
		t.Fatalf("payload: %+v", p)
	}

	// Bash: cd vào worktree thì qua; không cd (cwd cây gốc) hoặc cd ra cây gốc thì deny kèm lệnh đã che trong sự kiện.
	if r := f.call(t, "bint-implementer", "Bash", map[string]any{"command": hookcore.CdPrefix(f.wt) + "go test ./..."}, iso); r != "" {
		t.Fatalf("cd worktree: %q", r)
	}
	if r := f.call(t, "bint-implementer", "Bash", map[string]any{"command": "go test ./..."}, iso); !strings.Contains(r, "cd <worktree>") {
		t.Fatalf("bash không cd phải bị deny: %q", r)
	}
	if r := f.call(t, "bint-implementer", "Bash", map[string]any{"command": hookcore.CdPrefix(f.root) + "touch x"}, iso); r == "" {
		t.Fatal("cd ra cây gốc phải bị deny")
	}
	bash := 0
	for _, e := range spoolEventsOf(t, f.ws, spool.GuardrailTriggered) {
		if e.Payload["on"] == "pre_bash" {
			bash++
			if e.Payload["command"] == nil || e.Payload["worktree"] != f.wt {
				t.Fatalf("sự kiện bash: %+v", e.Payload)
			}
		}
	}
	if bash != 2 {
		t.Fatalf("muốn 2 sự kiện pre_bash, được %d", bash)
	}

	// Tác vụ shared: không can thiệp. agent_type khác/rỗng: không can thiệp.
	shared := f.transcript(t, "a1", wtPrompt("TSH", "RUNSH"))
	if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": outside}, shared); r != "" {
		t.Fatalf("tác vụ shared không bị chặn: %q", r)
	}
	for _, at := range []string{"", "general-purpose"} {
		if r := f.call(t, at, "Write", map[string]any{"file_path": outside}, iso); r != "" {
			t.Fatalf("agent_type %q không bị chặn: %q", at, r)
		}
	}
	// Mã tác vụ trong prompt lệch tệp ⇒ coi như không ánh xạ được (rơi vào fail-closed vì có bước isolated còn hạn).
	mismatch := f.transcript(t, "a1", wtPrompt("TKHAC", "RUNISO"))
	if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": outside}, mismatch); !strings.Contains(r, "không xác định được") {
		t.Fatalf("task_id lệch ⇒ unmapped: %q", r)
	}

	// Không mở SQLite.
	dbAfter, dbErr2 := os.Stat(f.ws.Paths.DB)
	if (dbErr == nil) != (dbErr2 == nil) || dbErr == nil && (dbBefore.ModTime() != dbAfter.ModTime() || dbBefore.Size() != dbAfter.Size()) {
		t.Fatalf("hook không được mở/ghi SQLite: %v %v / %v %v", dbBefore, dbErr, dbAfter, dbErr2)
	}
}

// Không ánh xạ được (không có transcript, hoặc transcript không có khối bint-task): fail-closed có giới hạn — chỉ áp khi có
// bước isolated còn hạn.
func TestWorktreeGuardrailUnmappedFailClosedBounded(t *testing.T) {
	f := newWtFixture(t)
	outside := filepath.Join(f.root, "src", "a.go")
	inside := filepath.Join(f.wt, "a.go")
	noTranscript := f.transcript(t, "a1", "")

	// Không có bước nào: không can thiệp.
	if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": outside}, noTranscript); r != "" {
		t.Fatalf("không có bước worktree ⇒ không can thiệp: %q", r)
	}
	// Có bước isolated nhưng đã hết hạn: vẫn không can thiệp.
	f.step(t, "RUNOLD", "TOLD", true, time.Now().Add(-time.Minute))
	if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": outside}, noTranscript); r != "" {
		t.Fatalf("bước hết hạn ⇒ không can thiệp: %q", r)
	}
	// Có bước isolated còn hạn: ngoài worktree bị chặn (unmapped), trong worktree thì qua.
	f.step(t, "RUNLIVE", "TLIVE", true, time.Now().Add(time.Hour))
	for name, parent := range map[string]string{"không có transcript": noTranscript, "không có khối bint-task": f.transcript(t, "a1", "prompt không có khối")} {
		if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": outside}, parent); !strings.Contains(r, "không xác định được") {
			t.Fatalf("%s: phải deny unmapped: %q", name, r)
		}
		if r := f.call(t, "bint-implementer", "Write", map[string]any{"file_path": inside}, parent); r != "" {
			t.Fatalf("%s: sửa trong worktree đang chạy không bị chặn: %q", name, r)
		}
		if r := f.call(t, "bint-implementer", "Bash", map[string]any{"command": hookcore.CdPrefix(f.wt) + "ls"}, parent); r != "" {
			t.Fatalf("%s: cd worktree đang chạy không bị chặn: %q", name, r)
		}
		if r := f.call(t, "bint-implementer", "Bash", map[string]any{"command": "ls"}, parent); r == "" {
			t.Fatalf("%s: bash không cd phải bị chặn", name)
		}
	}
	evs := spoolEventsOf(t, f.ws, spool.GuardrailTriggered)
	if len(evs) == 0 {
		t.Fatal("phải có guardrail.triggered")
	}
	for _, e := range evs {
		if e.Payload["outcome"] != "unmapped" || e.Payload["guardrail"] != "bint-worktree" {
			t.Fatalf("sự kiện: %+v", e.Payload)
		}
		if _, has := e.Payload["task_id"]; has {
			t.Fatalf("unmapped không có task_id: %+v", e.Payload)
		}
	}
}

// Dialect không khai SubagentPrompt (Codex, Antigravity…) thì guardrail không áp, kể cả fail-closed.
func TestWorktreeGuardrailNeedsDialectCapability(t *testing.T) {
	f := newWtFixture(t)
	f.step(t, "RUNLIVE", "TLIVE", true, time.Now().Add(time.Hour))
	d := claudeDialectWithoutSubagentPrompt()
	raw := hookInput(wtSession, "PreToolUse", f.root, map[string]any{"tool_name": "Write", "agent_type": "bint-implementer", "agent_id": "a1",
		"tool_input": map[string]any{"file_path": filepath.Join(f.root, "src", "a.go")}, "tool_use_id": "t"})
	res := handleWith(t, d, raw)
	if strings.Contains(res.Output, "deny") {
		t.Fatalf("Dialect không hỗ trợ ⇒ không can thiệp: %q", res.Output)
	}
}

func claudeDialectWithoutSubagentPrompt() hookcore.Dialect {
	d := claude.Dialect
	d.SubagentPrompt = nil
	return d
}

func handleWith(t *testing.T, d hookcore.Dialect, raw string) hookcore.Result {
	t.Helper()
	hi, err := hookcore.NormalizeClaudeStyle([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	res, err := hookcore.Handle(context.Background(), hi, d, hookcore.Env{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}
