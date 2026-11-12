package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"bint/internal/eval"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/spool"
	"bint/internal/task"
)

// Chi phí của bước chạy bằng subagent (docs/isolation-subagent-plan.md mục 6.6): `complete --agent-id X` đọc transcript
// của subagent (đường dẫn do hook ghi từ SubagentStop), cộng message.usage khử trùng lặp theo message.id, quy USD theo bảng
// giá dựng sẵn (ghi đè bằng harnesses.claude.pricing). Không đọc được hoặc model không có giá ⇒ chi phí unknown (nil, không
// phải 0) kèm ghi chú: max_cost_usd khi đó chỉ là chỉ dẫn. Không tìm thấy thì không đoán.

// subagentUsage tìm token của subagent: seam kiểm thử, rồi tệp hook ghi (runtime/subagents/<agent_id>.json), rồi sự kiện
// subagent.stopped còn chờ trong spool (chưa bị nạp vào SQLite). Trả lý do khi không đọc được.
func (r *Runner) subagentUsage(agentID string) (*claude.Usage, string) {
	if r.SubagentUsage != nil {
		return r.SubagentUsage(agentID)
	}
	transcript := ""
	runtimeDir := filepath.Dir(r.Tasks.Dir)
	if rec, ok := hookcore.ReadSubagentRecord(runtimeDir, agentID); ok {
		transcript = rec.TranscriptPath
	}
	if transcript == "" && r.Spool != "" {
		transcript = transcriptFromSpool(r.Spool, agentID)
	}
	if transcript == "" {
		return nil, "chưa có SubagentStop của " + agentID + " (hook chưa cài hoặc chưa tới)"
	}
	if _, err := os.Stat(transcript); err != nil {
		return nil, "không mở được transcript " + transcript + ": " + err.Error()
	}
	u := claude.SubagentUsage(transcript)
	if u == nil {
		return nil, "transcript " + transcript + " không có dòng usage"
	}
	return u, ""
}

// transcriptFromSpool tìm agent_transcript_path của subagent.stopped trong các tệp spool còn chờ.
func transcriptFromSpool(spoolDir, agentID string) string {
	files, err := spool.Pending(spoolDir)
	if err != nil {
		return ""
	}
	for _, f := range files {
		evs, err := spool.ReadFile(f)
		if err != nil {
			continue
		}
		for _, e := range evs {
			if e.Type != spool.SubagentStopped {
				continue
			}
			if id, _ := e.Payload["agent_id"].(string); id != agentID {
				continue
			}
			if p, _ := e.Payload["agent_transcript_path"].(string); p != "" {
				return p
			}
		}
	}
	return ""
}

// fillSubagentUsage điền chi phí/token/lượt/model của subagent vào kết quả bước. Thiếu --agent-id hoặc không đọc được ⇒
// để nil (unknown) và ghi chú.
func (r *Runner) fillSubagentUsage(rs *runState, res *eval.AgentResult, step, agentID string) {
	if agentID == "" {
		rs.res.Notes = append(rs.res.Notes, step+": thiếu --agent-id nên chi phí của subagent unknown (max_cost_usd chỉ là chỉ dẫn)")
		return
	}
	u, why := r.subagentUsage(agentID)
	if u == nil {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: chi phí của subagent %s unknown — %s (max_cost_usd chỉ là chỉ dẫn)", step, agentID, why))
		return
	}
	in, out := u.Totals()
	turns := int64(u.Turns)
	res.InputTokens, res.OutputTokens, res.NumTurns = &in, &out, &turns
	if m := u.Model(); m != "" {
		res.Model = m
	}
	res.CostUSD = u.Cost(r.Cfg.Harnesses.Claude.Pricing)
	if res.CostUSD == nil {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: model %q của subagent %s không có giá nên chi phí unknown — đặt harnesses.claude.pricing để tính",
			step, u.Model(), agentID))
	}
}

// saveSessionSpend ghi phần ngân sách b-do đã dùng của lượt cha vào phiếu, để `next` ở tiến trình khác thấy.
func (r *Runner) saveSessionSpend(rs *runState) {
	if !rs.session || rs.runID == "" {
		return
	}
	sp := &task.SessionSpend{RunID: rs.runID, Cost: rs.spent, CostUnknown: rs.costMissing, Turns: rs.turns}
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.SessionSpend = sp; return nil }); err == nil {
		rs.t = t
	}
}
