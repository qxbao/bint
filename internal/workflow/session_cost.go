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

// subagentTranscript tìm đường dẫn transcript của subagent: tệp hook ghi (runtime/subagents/<agent_id>.json), rồi sự kiện
// subagent.stopped còn chờ trong spool (chưa bị nạp vào SQLite). Trả lý do khi không có hoặc không mở được. Dùng chung cho
// chi phí và hậu kiểm cô lập.
func (r *Runner) subagentTranscript(agentID string) (string, string) {
	transcript := ""
	runtimeDir := filepath.Dir(r.Tasks.Dir)
	if rec, ok := hookcore.ReadSubagentRecord(runtimeDir, agentID); ok {
		transcript = rec.TranscriptPath
	}
	if transcript == "" && r.Spool != "" {
		transcript = transcriptFromSpool(r.Spool, agentID)
	}
	if transcript == "" {
		return "", "chưa có SubagentStop của " + agentID + " (hook chưa cài hoặc chưa tới)"
	}
	if _, err := os.Stat(transcript); err != nil {
		return "", "không mở được transcript " + transcript + ": " + err.Error()
	}
	return transcript, ""
}

// subagentUsage tìm token của subagent: seam kiểm thử, rồi transcript (subagentTranscript). Trả lý do khi không đọc được.
func (r *Runner) subagentUsage(agentID string) (*claude.Usage, string, string) {
	if r.SubagentUsage != nil {
		u, why := r.SubagentUsage(agentID)
		return u, "seam:" + agentID, why // seam không có đường dẫn: coi như một transcript cố định theo agent
	}
	transcript, why := r.subagentTranscript(agentID)
	if transcript == "" {
		return nil, "", why
	}
	u := claude.SubagentUsage(transcript)
	if u == nil {
		return nil, transcript, "transcript " + transcript + " không có dòng usage"
	}
	return u, transcript, ""
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
// để nil (unknown) và ghi chú. Bước nối tiếp (sr.usageBase cùng transcript) chỉ tính phần phát sinh sau lần đóng trước của
// cùng agent: usage trong transcript là cộng dồn cả đời agent. Mốc cộng dồn mới được lưu vào sr.usage cho lần nối tiếp sau.
func (r *Runner) fillSubagentUsage(rs *runState, res *eval.AgentResult, sr *stepRun, agentID string) {
	step := sr.step
	if agentID == "" {
		rs.res.Notes = append(rs.res.Notes, step+": thiếu --agent-id nên chi phí của subagent unknown (max_cost_usd chỉ là chỉ dẫn)")
		return
	}
	u, transcript, why := r.subagentUsage(agentID)
	if u == nil {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: chi phí của subagent %s unknown — %s (max_cost_usd chỉ là chỉ dẫn)", step, agentID, why))
		return
	}
	sr.usage = usageMark(u, transcript)
	delta, continued := u, false
	if b := sr.usageBase; b != nil && b.Transcript == transcript {
		delta, continued = usageSince(u, b), true
	}
	in, out := delta.Totals()
	turns := int64(delta.Turns)
	res.InputTokens, res.OutputTokens, res.NumTurns = &in, &out, &turns
	if m := delta.Model(); m != "" {
		res.Model = m
	} else if m := u.Model(); m != "" {
		res.Model = m
	}
	if continued && len(delta.Models) == 0 {
		zero := 0.0 // bước nối tiếp không phát sinh gì sau mốc trước; bước thường usage rỗng giữ unknown (nil) như trước GĐ2
		res.CostUSD = &zero
		return
	}
	res.CostUSD = delta.Cost(r.Cfg.Harnesses.Claude.Pricing)
	if res.CostUSD == nil {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: model %q của subagent %s không có giá nên chi phí unknown — đặt harnesses.claude.pricing để tính",
			step, u.Model(), agentID))
	}
}

// usageMark chụp usage cộng dồn của agent thành mốc lưu vào phiếu.
func usageMark(u *claude.Usage, transcript string) *task.UsageMark {
	m := &task.UsageMark{Transcript: transcript, Turns: u.Turns}
	for _, mu := range u.Models {
		m.Models = append(m.Models, task.UsageModel{Model: mu.Model, Long: mu.Long, Input: mu.Input, Output: mu.Output,
			CacheWrite: mu.CacheWrite, CacheRead: mu.CacheRead})
	}
	return m
}

// usageSince trả phần usage của u vượt mốc base (theo từng model/bậc giá, sàn 0: không bao giờ ra giá trị âm dù transcript
// bị cắt ngắn), bỏ các dòng không còn gì.
func usageSince(u *claude.Usage, base *task.UsageMark) *claude.Usage {
	type key struct {
		model string
		long  bool
	}
	old := map[key]task.UsageModel{}
	for _, m := range base.Models {
		old[key{m.Model, m.Long}] = m
	}
	d := &claude.Usage{Turns: max(u.Turns-base.Turns, 0)}
	for _, m := range u.Models {
		b := old[key{m.Model, m.Long}]
		n := claude.ModelUsage{Model: m.Model, Long: m.Long, Input: max(m.Input-b.Input, 0), Output: max(m.Output-b.Output, 0),
			CacheWrite: max(m.CacheWrite-b.CacheWrite, 0), CacheRead: max(m.CacheRead-b.CacheRead, 0)}
		if n.Input+n.Output+n.CacheWrite+n.CacheRead > 0 {
			d.Models = append(d.Models, n)
		}
	}
	return d
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
