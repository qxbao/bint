package workflow

import (
	"cmp"
	"fmt"

	"bint/internal/eval"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/task"
)

// Chi phí và dấu vết phiên của bước driver process chạy bằng Claude/Codex (docs/step-continuation-plan.md giai đoạn 3).
//
// Ngữ nghĩa chi phí sau `claude -p --resume` / `codex exec resume` KHÔNG được dựa vào số harness báo trong kết quả lần chạy
// đó: `total_cost_usd` của Claude có thể là chi phí cả phiên hoặc chỉ của tiến trình tuỳ phiên bản, và `turn.completed.usage` của
// Codex lấy từ bộ đếm thread nên có thể cộng dồn sau resume. Bint đọc tệp phiên do chính harness ghi (transcript JSONL của Claude,
// rollout của Codex) — usage trong đó cộng dồn theo cả đời phiên — rồi trừ mốc chụp lúc bước dừng (task.StepStop.Usage):
//
//   - Claude: usage cộng từ message.usage của transcript (claude.SubagentUsage, khử trùng lặp theo message.id), quy USD theo bảng giá
//     như chi phí subagent; phần của lần nối tiếp = usageSince(mốc), chỉ khi cùng transcript (khác tệp thì tính cả tệp mới: không
//     trừ nhầm, có thể thiếu nhưng không trùng).
//   - Codex: bộ đếm total_token_usage của thread (codex.RolloutUsage) trừ mốc; Codex không báo USD nên CostUSD giữ nil.
//
// Bước thường (không nối tiếp) giữ nguyên số harness báo. Không đọc được tệp phiên ⇒ unknown (nil) kèm ghi chú, không đoán.

// settleProcessUsage chụp mốc usage cộng dồn của phiên (sr.usage, dùng cho lần nối tiếp sau), ghi nhận phiên có tồn tại trên đĩa
// (sr.noSession khác rỗng ⇒ bước không nối tiếp được) và, nếu bước này là bước nối tiếp (resumed), thay chi phí/token/lượt của
// kết quả bằng phần vượt mốc trước. Chỉ áp cho harness claude/codex; handle là phiên của bước (kết quả harness hoặc UUID Bint cấp).
func (r *Runner) settleProcessUsage(rs *runState, sr *stepRun, res *eval.AgentResult, handle string, resumed bool) {
	switch sr.harness {
	case "claude":
		r.settleClaudeUsage(rs, sr, res, handle, resumed)
	case "codex":
		r.settleCodexUsage(rs, sr, res, handle, resumed)
	}
}

// unknownUsage đặt chi phí/token/lượt của bước nối tiếp thành unknown (không đo được phần mới) kèm ghi chú.
func unknownUsage(rs *runState, res *eval.AgentResult, step, why string) {
	res.CostUSD, res.InputTokens, res.OutputTokens, res.NumTurns = nil, nil, nil, nil
	rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: chi phí của lần nối tiếp unknown — %s (max_cost_usd chỉ là chỉ dẫn)", step, why))
}

func (r *Runner) settleClaudeUsage(rs *runState, sr *stepRun, res *eval.AgentResult, handle string, resumed bool) {
	path := claude.SessionTranscriptPath("", r.Root, handle)
	if path == "" {
		sr.noSession = fmt.Sprintf("phiên %s chưa được Claude ghi vào thư mục dự án của %s (tiến trình dừng trước khi ghi phiên)", shortID(handle), r.Root)
		if resumed {
			unknownUsage(rs, res, sr.step, "không có transcript của phiên")
		}
		return
	}
	u := claude.SubagentUsage(path)
	if u == nil {
		// Tệp có nhưng chưa có dòng usage: phiên nối tiếp được, mốc rỗng (mọi thứ sau đó đều là phần mới).
		sr.usage = &task.UsageMark{Transcript: path}
		if resumed {
			unknownUsage(rs, res, sr.step, "transcript "+path+" không có dòng usage")
		}
		return
	}
	sr.usage = usageMark(u, path)
	if !resumed {
		return
	}
	base := sr.usageBase
	if base == nil {
		unknownUsage(rs, res, sr.step, "không có mốc usage lúc bước trước dừng")
		return
	}
	delta := u
	if base.Transcript == path {
		delta = usageSince(u, base)
	}
	in, out := delta.Totals()
	turns := int64(delta.Turns)
	res.InputTokens, res.OutputTokens, res.NumTurns = &in, &out, &turns
	if m := delta.Model(); m != "" {
		res.Model = m
	}
	if len(delta.Models) == 0 {
		zero := 0.0 // lần nối tiếp không phát sinh lượt gọi API nào sau mốc
		res.CostUSD = &zero
		return
	}
	res.CostUSD = delta.Cost(r.Cfg.Harnesses.Claude.Pricing)
	if res.CostUSD == nil {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("%s: model %q của lần nối tiếp không có giá nên chi phí unknown — đặt harnesses.claude.pricing để tính", sr.step, delta.Model()))
	}
}

func (r *Runner) settleCodexUsage(rs *runState, sr *stepRun, res *eval.AgentResult, handle string, resumed bool) {
	path := codex.RolloutPath("", handle)
	if path == "" {
		sr.noSession = fmt.Sprintf("thread %s chưa có rollout của Codex (tiến trình dừng trước khi ghi phiên)", shortID(handle))
		if resumed {
			unknownUsage(rs, res, sr.step, "không có rollout của thread")
		}
		return
	}
	// Codex không báo chi phí USD nên CostUSD luôn nil; usage JSONL của lần nối tiếp có thể cộng dồn theo thread nên khi nối tiếp
	// được thay bằng bộ đếm rollout trừ mốc.
	tu := codex.RolloutUsage(path)
	if tu == nil {
		sr.usage = &task.UsageMark{Transcript: path}
		if resumed {
			unknownUsage(rs, res, sr.step, "rollout "+path+" chưa có bộ đếm total_token_usage")
		}
		return
	}
	sr.usage = &task.UsageMark{Transcript: path, Models: []task.UsageModel{{Model: cmp.Or(res.Model, sr.reqModel), Input: tu.Input, Output: tu.Output,
		CacheRead: tu.CachedInput}}}
	if !resumed {
		return
	}
	base := sr.usageBase
	if base == nil || len(base.Models) != 1 {
		unknownUsage(rs, res, sr.step, "không có mốc token lúc bước trước dừng")
		return
	}
	// Bộ đếm là của cả thread (kể cả khi Codex ghi sang rollout mới): trừ mốc bất kể tệp, sàn 0.
	in, out := max(tu.Input-base.Models[0].Input, 0), max(tu.Output-base.Models[0].Output, 0)
	res.InputTokens, res.OutputTokens, res.NumTurns, res.CostUSD = &in, &out, nil, nil
}

// shortID rút gọn handle phiên khi đưa vào thông báo.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:8] + "…"
	}
	return id
}
