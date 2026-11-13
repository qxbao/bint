package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/hookcore"
	"bint/internal/spool"
	"bint/internal/task"
)

// NT-ISO-LEAK: hậu kiểm cô lập worktree từ transcript subagent ở `complete`.

type isoCall struct {
	id, tool string
	input    map[string]any
	cwd      string
	result   string // "" = chưa có tool_result
	isErr    bool
}

func (h *harness) isoTranscript(calls []isoCall) string {
	h.t.Helper()
	var lines []string
	for _, c := range calls {
		in, _ := json.Marshal(c.input)
		lines = append(lines, fmt.Sprintf(`{"type":"assistant","cwd":%q,"message":{"content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}}`, c.cwd, c.id, c.tool, in))
		if c.result != "" {
			lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"is_error":%v,"content":%q}]}}`, c.id, c.isErr, c.result))
		}
	}
	p := filepath.Join(h.t.TempDir(), "agent-A1.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// isoRun mở bước b-feat của một tác vụ (worktree hoặc shared), cho subagent giả ghi transcript do build dựng (nhận worktree
// và cây gốc), rồi chạy complete. record=false: không ghi tệp SubagentStop của hook.
func isoRun(t *testing.T, shared bool, agentID string, record bool, build func(h *harness, wt string) []isoCall) (*harness, *task.Task, *SessionAction, string) {
	t.Helper()
	h := newHarness(t)
	if !shared {
		h = h.wtHarness()
	}
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	h.subagent(a)
	wt, _ := filepath.Abs(a.Worktree)
	if record && build != nil {
		tr := h.isoTranscript(build(h, wt))
		if err := hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A1", AgentType: SubagentImplementer, TranscriptPath: tr}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := h.runner().Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: agentID})
	if err != nil || c.Result == nil {
		t.Fatalf("complete: %+v %v", c, err)
	}
	return h, tk, c, a.RunID
}

func isoLeakEvents(t *testing.T, h *harness) []spool.Event {
	t.Helper()
	files, _ := spool.Pending(h.spool)
	var out []spool.Event
	for _, f := range files {
		evs, _ := spool.ReadFile(f)
		for _, e := range evs {
			if e.Type == spool.GuardrailTriggered && e.Payload["outcome"] == hookcore.OutcomeMissedByHook {
				out = append(out, e)
			}
		}
	}
	return out
}

func hasIsoNote(c *SessionAction) bool {
	for _, n := range c.Notes {
		if strings.Contains(n, isoNotePrefix) {
			return true
		}
	}
	return false
}

// Write ra cây gốc đã thành công, hook không chặn ⇒ complete: bước blocked, tác vụ blocked, lý do nêu tệp + tool_use_id +
// lời nhắc kiểm tra cây gốc, spool có đúng một missed_by_hook; tệp ở cây gốc còn nguyên.
func TestIsolationAuditWriteToRootBlocks(t *testing.T) {
	var leak string
	h, tk, c, run := isoRun(t, false, "A1", true, func(h *harness, wt string) []isoCall {
		leak = filepath.Join(h.root, "LEAK.txt")
		os.WriteFile(leak, []byte("probe"), 0o600)
		return []isoCall{
			{id: "toolu_ok", tool: "Edit", input: map[string]any{"file_path": filepath.Join(wt, "app.txt")}, cwd: wt, result: "updated"},
			{id: "toolu_leak", tool: "Write", input: map[string]any{"file_path": leak, "content": "probe"}, cwd: h.root, result: "File created successfully"},
		}
	})
	if c.Result.Status != "blocked" {
		t.Fatalf("status bước %q, muốn blocked", c.Result.Status)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.Status != task.StatusBlocked {
		t.Fatalf("tác vụ %q, muốn blocked", got.State.Status)
	}
	for _, want := range []string{"toolu_leak", "LEAK.txt", "kiểm tra cây gốc", "KHÔNG thuộc tác vụ", got.State.Origin} {
		if !strings.Contains(c.Reason, want) {
			t.Fatalf("lý do thiếu %q: %s", want, c.Reason)
		}
	}
	if strings.Contains(c.Reason, "toolu_ok") {
		t.Fatalf("lời gọi trong worktree không được nêu: %s", c.Reason)
	}
	evs := isoLeakEvents(t, h)
	if len(evs) != 1 {
		t.Fatalf("muốn đúng một guardrail.triggered missed_by_hook, có %d", len(evs))
	}
	p := evs[0].Payload
	if p["guardrail"] != hookcore.GuardrailWorktree || p["on"] != hookcore.OnPostStep || p["agent_id"] != "A1" || p["tool_use_id"] != "toolu_leak" ||
		p["blocked"] != false || evs[0].RunID != run {
		t.Fatalf("payload: %+v (run %q)", p, evs[0].RunID)
	}
	if b, err := os.ReadFile(leak); err != nil || string(b) != "probe" {
		t.Fatalf("Bint không được xoá/sửa tệp ở cây gốc: %q %v", b, err)
	}
	hist, _ := json.Marshal(got.State)
	if !strings.Contains(string(hist), "toolu_leak") {
		t.Fatalf("lịch sử lỗi của phiếu thiếu lý do: %s", hist)
	}
}

// Các ca không phải vi phạm (hoặc chưa chắc đã chạy) ⇒ bước completed, không sự kiện.
func TestIsolationAuditNotViolations(t *testing.T) {
	deny := "PreToolUse:Write hook error: Bint: Write bị chặn — đường dẫn nằm ngoài worktree"
	cases := map[string]func(h *harness, wt string) []isoCall{
		"Write ra cây gốc bị hook chặn": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": filepath.Join(h.root, "x.txt")}, cwd: h.root, result: deny, isErr: true}}
		},
		"Write ra cây gốc is_error khác": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": filepath.Join(h.root, "x.txt")}, cwd: h.root, result: "EACCES", isErr: true}}
		},
		"Edit trong worktree": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Edit", input: map[string]any{"file_path": filepath.Join(wt, "app.txt")}, cwd: h.root, result: "ok"}}
		},
		"Bash cd worktree": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Bash", input: map[string]any{"command": hookcore.CdPrefix(wt) + "go test ./..."}, cwd: h.root, result: "ok"}}
		},
		"Bash cd worktree rồi thoát khác 0": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Bash", input: map[string]any{"command": hookcore.CdPrefix(wt) + "false"}, cwd: h.root, result: "Exit code 1", isErr: true}}
		},
		"Bash cwd trong worktree": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Bash", input: map[string]any{"command": "ls"}, cwd: wt, result: "ok"}}
		},
		"Bash ngoài worktree bị hook chặn": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Bash", input: map[string]any{"command": "ls"}, cwd: h.root, result: "PreToolUse:Bash hook error: Bint: Bash bị chặn", isErr: true}}
		},
		"lời gọi chưa có kết quả": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": filepath.Join(h.root, "x.txt")}, cwd: h.root}}
		},
		"công cụ chỉ đọc ở cây gốc": func(h *harness, wt string) []isoCall {
			return []isoCall{{id: "t1", tool: "Read", input: map[string]any{"file_path": filepath.Join(h.root, "app.txt")}, cwd: h.root, result: "ok"}}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			h, tk, c, _ := isoRun(t, false, "A1", true, build)
			if c.Result.Status != "completed" || loadTask(t, h, tk.State.ID).State.Status == task.StatusBlocked {
				t.Fatalf("status %q, lý do %q", c.Result.Status, c.Reason)
			}
			if n := len(isoLeakEvents(t, h)); n != 0 {
				t.Fatalf("không được ghi sự kiện, có %d", n)
			}
			if hasIsoNote(c) {
				t.Fatalf("không được ghi chú thiếu dữ liệu: %v", c.Notes)
			}
		})
	}
}

// Bash không cd mà cwd ngoài worktree, đã chạy ⇒ tính; Bash lỗi không phải do hook cũng tính (lệnh đã chạy); cd ra ngoài tính.
func TestIsolationAuditShellViolations(t *testing.T) {
	cases := map[string]func(h *harness, wt string) isoCall{
		"không cd, cwd cây gốc": func(h *harness, wt string) isoCall {
			return isoCall{id: "tb", tool: "Bash", input: map[string]any{"command": "touch LEAK"}, cwd: h.root, result: "ok"}
		},
		"lỗi không phải hook": func(h *harness, wt string) isoCall {
			return isoCall{id: "tb", tool: "Bash", input: map[string]any{"command": "touch LEAK && false"}, cwd: h.root, result: "Exit code 1", isErr: true}
		},
		"cd ra ngoài": func(h *harness, wt string) isoCall {
			return isoCall{id: "tb", tool: "Bash", input: map[string]any{"command": "cd '" + h.root + "' && touch LEAK"}, cwd: wt, result: "ok"}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			h, _, c, _ := isoRun(t, false, "A1", true, func(h *harness, wt string) []isoCall { return []isoCall{mk(h, wt)} })
			if c.Result.Status != "blocked" || !strings.Contains(c.Reason, "tb (Bash)") || !strings.Contains(c.Reason, "LEAK") {
				t.Fatalf("status %q, lý do %q", c.Result.Status, c.Reason)
			}
			evs := isoLeakEvents(t, h)
			if len(evs) != 1 || !strings.Contains(fmt.Sprint(evs[0].Payload["command"]), "LEAK") {
				t.Fatalf("sự kiện: %+v", evs)
			}
		})
	}
}

// Tác vụ shared (cùng transcript vi phạm) ⇒ bỏ qua hoàn toàn, không ghi chú.
func TestIsolationAuditSkipsSharedTask(t *testing.T) {
	h, tk, c, _ := isoRun(t, true, "A1", true, func(h *harness, wt string) []isoCall {
		return []isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": "/ngoai/x.txt"}, cwd: "/ngoai", result: "ok"}}
	})
	if c.Result.Status != "completed" || loadTask(t, h, tk.State.ID).State.Status == task.StatusBlocked || hasIsoNote(c) || len(isoLeakEvents(t, h)) != 0 {
		t.Fatalf("shared phải bỏ qua: status %q notes %v", c.Result.Status, c.Notes)
	}
}

// Thiếu transcript / thiếu SubagentStop / thiếu --agent-id ⇒ chỉ ghi chú, bước vẫn completed.
func TestIsolationAuditMissingDataOnlyNotes(t *testing.T) {
	bad := func(h *harness, wt string) []isoCall {
		return []isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": filepath.Join(h.root, "x")}, cwd: h.root, result: "ok"}}
	}
	for name, c := range map[string]struct {
		agentID string
		record  bool
	}{
		"không có tệp hook":   {"A1", false},
		"thiếu --agent-id":    {"", true},
		"agent_id không khớp": {"A9", true},
	} {
		t.Run(name, func(t *testing.T) {
			h, _, act, _ := isoRun(t, false, c.agentID, c.record, bad)
			if act.Result.Status != "completed" || !hasIsoNote(act) || len(isoLeakEvents(t, h)) != 0 {
				t.Fatalf("status %q notes %v", act.Result.Status, act.Notes)
			}
		})
	}
	t.Run("transcript mất", func(t *testing.T) {
		h := newHarness(t).wtHarness()
		h.behave = featWrites(h, nil)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
		a, _ := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
		h.subagent(a)
		hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A1", AgentType: SubagentImplementer, TranscriptPath: filepath.Join(t.TempDir(), "mat.jsonl")})
		act, err := h.runner().Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "A1"})
		if err != nil || act.Result.Status != "completed" || !hasIsoNote(act) {
			t.Fatalf("%+v %v", act, err)
		}
	})
}

// Subagent khác bint-implementer: bỏ qua (kiểm thẳng auditIsolation với bước dở của loại khác).
func TestIsolationAuditSkipsOtherSubagent(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, _ := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	h.subagent(a)
	wt, _ := filepath.Abs(a.Worktree)
	tr := h.isoTranscript([]isoCall{{id: "t1", tool: "Write", input: map[string]any{"file_path": filepath.Join(h.root, "x")}, cwd: wt, result: "ok"}})
	hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A1", TranscriptPath: tr})
	rn := h.runner()
	rn.Env.Root = a.Worktree // Complete đặt Root theo worktree của tác vụ
	rs := rn.sessionState(loadTask(t, h, tk.State.ID), "", false, false)
	ps := *loadTask(t, h, tk.State.ID).State.PendingStep
	if got := rn.auditIsolation(rs, &ps, "A1"); got == "" {
		t.Fatal("bint-implementer phải bị bắt (đối chứng)")
	}
	ps.Subagent = "bint-reviewer"
	if got := rn.auditIsolation(rs, &ps, "A1"); got != "" {
		t.Fatalf("subagent khác phải bỏ qua: %q", got)
	}
}
