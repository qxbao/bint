package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"bint/internal/detect"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/spool"
	"bint/internal/task"
)

// Hậu kiểm cô lập worktree (NT-ISO-LEAK, docs/workflows.md). Hook PreToolUse là lớp chặn duy nhất của subagent
// bint-implementer và fail-open (Claude Code cho lệnh đi qua khi hook không chạy hoặc không trả quyết định). Ở `complete`,
// Bint quét lại transcript của subagent (cùng tệp đã đọc để tính chi phí) bằng đúng quy tắc của hook
// (hookcore.WorktreeViolation): lời gọi vi phạm đã THỰC SỰ chạy thì bước không được tính là hoàn thành.

const (
	maxIsolationItems  = 10 // số vi phạm nêu trong lý do blocked
	maxIsolationEvents = 20 // số sự kiện guardrail.triggered ghi cho một bước
	isoNotePrefix      = "không hậu kiểm được cô lập"
)

// isoViolation là một lời gọi đã chạy ngoài worktree.
type isoViolation struct {
	id, tool, what, subject string // subject: đường dẫn hoặc lệnh (đã che)
	kind                    hookcore.ToolKind
}

func (v isoViolation) String() string {
	return fmt.Sprintf("%s (%s): %s", v.id, v.tool, v.subject)
}

// ranForReal: lời gọi đã thực sự chạy, không bị hook chặn. Thiếu tool_result (transcript cắt, subagent bị dừng giữa chừng) thì
// không biết nên không tính. Edit-type: mọi kết quả lỗi đều là chưa ghi được. Bash: lỗi do hook chặn là chưa chạy; lệnh chạy
// rồi thoát khác 0 cũng là is_error nhưng ĐÃ chạy.
func ranForReal(c claude.ToolCall, kind hookcore.ToolKind) bool {
	if !c.HasResult {
		return false
	}
	if kind == hookcore.ToolEdit {
		return !c.IsError && !hookcore.IsHookDenial(c.Result)
	}
	return !(c.IsError && hookcore.IsHookDenial(c.Result))
}

// auditIsolation quét transcript của subagent agentID của bước ps. Chỉ áp cho bint-implementer của tác vụ worktree
// (State.Origin != ""); tác vụ shared và subagent khác bỏ qua, không ghi chú. Thiếu dữ liệu thì chỉ ghi chú (giống chi phí).
// Có vi phạm: ghi guardrail.triggered (missed_by_hook) và trả lý do blocked; không đụng tới tệp nào ở cây gốc.
func (r *Runner) auditIsolation(rs *runState, ps *task.PendingStep, agentID string) string {
	if ps.Subagent != SubagentImplementer || rs.t.State.Origin == "" {
		return ""
	}
	note := func(why string) string {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: %s — %s", ps.Step, isoNotePrefix, why))
		return ""
	}
	if agentID == "" {
		return note("thiếu --agent-id")
	}
	transcript, why := r.subagentTranscript(agentID)
	if transcript == "" {
		return note(why)
	}
	calls, err := claude.SubagentToolCalls(transcript)
	if err != nil && len(calls) == 0 {
		return note("transcript " + transcript + ": " + err.Error())
	}
	if err != nil {
		note(err.Error() + "; chỉ kiểm phần đã đọc được")
	} else if len(calls) == 0 {
		note("transcript " + transcript + " không có lời gọi công cụ nào nhận ra được (bố cục đổi?)")
	}
	wt, aerr := filepath.Abs(r.Root)
	if aerr != nil {
		wt = r.Root
	}
	var bad []isoViolation
	for _, c := range calls {
		info := claude.Classify(c.Name, c.Input)
		if info.Kind != hookcore.ToolEdit && info.Kind != hookcore.ToolShell {
			continue
		}
		if !ranForReal(c, info.Kind) {
			continue
		}
		what, viol := hookcore.WorktreeViolation(wt, c.Cwd, info)
		if !viol {
			continue
		}
		v := isoViolation{id: c.ID, tool: c.Name, what: what, kind: info.Kind}
		if info.Kind == hookcore.ToolShell {
			v.subject = detect.Redact(info.Command, 200)
		} else {
			v.subject = detect.Redact(strings.Join(info.Paths, ", "), 300)
		}
		bad = append(bad, v)
	}
	if len(bad) == 0 {
		return ""
	}
	r.emitIsolationLeaks(rs, ps, agentID, wt, bad)
	return isolationReason(rs.t.State.Origin, ps.Step, agentID, bad)
}

func isolationReason(origin, step, agentID string, bad []isoViolation) string {
	var items []string
	for i, v := range bad {
		if i == maxIsolationItems {
			items = append(items, fmt.Sprintf("+%d nữa", len(bad)-i))
			break
		}
		items = append(items, v.String())
	}
	return fmt.Sprintf("bước %s: subagent %s (agent %s) đã chạy %d lời gọi ngoài worktree của tác vụ mà hook không chặn: %s. "+
		"Hãy kiểm tra cây gốc %s: các tệp/thay đổi đó KHÔNG thuộc tác vụ này và Bint không tự xoá hay khôi phục. "+
		"Xem xong thì tiếp tục bằng `bint run resume`.", step, SubagentImplementer, agentID, len(bad), strings.Join(items, "; "), origin)
}

// emitIsolationLeaks ghi mỗi vi phạm một sự kiện guardrail.triggered (guardrail bint-worktree, outcome missed_by_hook, on
// post_step). Đây là phát hiện của workflow, không phải quyết định của hook: source là của Env, blocked=false (lời gọi đã chạy).
// event_id ổn định theo (lượt con, tool_use_id) nên chạy lại `complete` không ghi trùng.
func (r *Runner) emitIsolationLeaks(rs *runState, ps *task.PendingStep, agentID, wt string, bad []isoViolation) {
	src := r.Source
	if src == "" {
		src = "bint-run"
	}
	for i, v := range bad {
		if i >= maxIsolationEvents {
			break
		}
		payload := map[string]any{"guardrail": hookcore.GuardrailWorktree, "outcome": hookcore.OutcomeMissedByHook, "on": hookcore.OnPostStep,
			"blocked": false, "agent_type": SubagentImplementer, "agent_id": agentID, "tool": v.tool,
			"tool_use_id": v.id, "task_id": rs.t.State.ID, "run_id": ps.RunID, "worktree": wt, "what": v.what}
		if v.kind == hookcore.ToolShell {
			payload["command"] = v.subject
		} else {
			payload["paths"] = []string{v.subject}
		}
		ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: hookcore.EventID("bint-run", ps.RunID, v.id, "guardrail", hookcore.OutcomeMissedByHook),
			Type: spool.GuardrailTriggered, Source: src, Timestamp: r.now().UTC(), RunID: ps.RunID, Worktree: r.Root, Payload: payload}
		if _, err := spool.Write(r.Spool, ev); err != nil {
			r.logf("! ghi sự kiện: %v", err)
		}
	}
}
