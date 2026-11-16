package workflow

import (
	"cmp"
	"context"
	"time"

	"bint/internal/flow"
	"fmt"
	"os"
	"strings"

	"bint/internal/task"
)

// Nối tiếp phiên của bước ở driver session (docs/step-continuation-plan.md giai đoạn 2). Quyết định là hàm thuần; việc mở
// bước nối tiếp vẫn đi qua beginStep/pendingFrom/grantPending nên mọi cổng (khoá, lease, ngân sách) giữ nguyên.

// ViaSubagent là State.LastStep.Via của bước chạy bằng subagent (nối tiếp bằng SendMessage tới agent cũ).
const ViaSubagent = "subagent"

// Giới hạn số lần nối tiếp liên tiếp của một bước theo lớp dừng. awaiting_human không tính vào giới hạn (mỗi vòng cần người).
const (
	maxContinueExternal = 3
	maxContinueTimeout  = 1
)

// ContinueFacts là dữ kiện của quyết định nối tiếp.
type ContinueFacts struct {
	// Last là bước dừng gần nhất (State.LastStep); Step là bước sắp mở.
	Last *task.StepStop
	Step string
	// LastIsPlan: Last.Step là bước plan của flow (chỉ awaiting_human của bước plan được nối tiếp: sau duyệt đường dẫn bảo
	// vệ, resume không cần agent).
	LastIsPlan bool
	// Fingerprint là fingerprint cây hiện tại ("" = không chụp được); Fresh: người yêu cầu bỏ nối tiếp (--fresh).
	Fingerprint string
	Fresh       bool
}

// ContinueDecision là kết quả: Continue=true thì mở lại Step dạng nối tiếp; Why giải thích khi không nối tiếp (vào notes).
type ContinueDecision struct {
	Continue bool
	Step     string
	Why      string
}

// continuationDecision quyết định có nối tiếp phiên của bước dừng gần nhất không. Thứ tự: không có LastStep hoặc bước sắp mở khác
// bước dừng (không có gì để nối tiếp: Why rỗng, kể cả khi --fresh, để notes không nhiễu) → fresh → không phải subagent → không
// continuable → awaiting_human không phải plan → fingerprint khác → quá giới hạn. Thiếu một điều kiện thì chạy đề bài đầy đủ.
func continuationDecision(f ContinueFacts) ContinueDecision {
	no := func(why string) ContinueDecision { return ContinueDecision{Why: why} }
	l := f.Last
	switch {
	case l == nil || l.Step != f.Step:
		return ContinueDecision{}
	case f.Fresh:
		return no("--fresh: không nối tiếp phiên cũ, dùng đề bài đầy đủ")
	case l.Via != ViaSubagent:
		return no("bước dừng trước không chạy bằng subagent của driver session (không có agent để nối tiếp)")
	case !l.Continuable:
		return no(fmt.Sprintf("bước %s dừng với lớp %s, không nối tiếp được (thiếu agent_id/fingerprint hoặc lỗi nội tại)", l.Step, l.StopClass))
	case l.StopClass == StopAwaitingHuman && !f.LastIsPlan:
		return no("chỉ bước plan chờ người mới nối tiếp phiên; bước " + l.Step + " chạy mới")
	}
	want := l.Step
	switch {
	case f.Fingerprint == "" || l.Fingerprint == "":
		return no("không so được fingerprint cây làm việc")
	case f.Fingerprint != l.Fingerprint:
		return no("cây làm việc đã đổi kể từ lúc bước " + l.Step + " dừng")
	}
	switch l.StopClass {
	case StopExternal:
		if l.Continuations >= maxContinueExternal {
			return no(fmt.Sprintf("đã nối tiếp %d lần liên tiếp (tối đa %d)", l.Continuations, maxContinueExternal))
		}
	case StopTimeout:
		if l.Continuations >= maxContinueTimeout {
			return no("bước quá giờ đã được nối tiếp một lần")
		}
	}
	return ContinueDecision{Continue: true, Step: want}
}

// continuationCount là số lần nối tiếp của bước mới mở: +1 khi nối tiếp sau lớp external/timeout, giữ nguyên sau awaiting_human.
func continuationCount(l *task.StepStop) int {
	if l.StopClass == StopExternal || l.StopClass == StopTimeout {
		return l.Continuations + 1
	}
	return l.Continuations
}

// continuationPrompt dựng lời nhắn nối tiếp gửi cho agent cũ (SendMessage). Không lặp lại đề bài đầy đủ: agent còn ngữ cảnh.
func continuationPrompt(id, step, run string, l *task.StepStop, answered []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tiếp tục bước %s của tác vụ %s (lượt mới %s, nối tiếp lượt %s).\n", step, id, run, l.RunID)
	switch l.StopClass {
	case StopExternal:
		b.WriteString("Lý do lượt trước dừng: nguyên nhân ngoại cảnh (hạn mức, lỗi/quá tải API, mất kết nối hoặc hết hạn thuê), không phải lỗi của bạn.\n")
	case StopTimeout:
		b.WriteString("Lý do lượt trước dừng: quá thời gian cho phép của bước.\n")
	case StopAwaitingHuman:
		b.WriteString("Lý do lượt trước dừng: chờ người trả lời.\n")
	}
	fmt.Fprintf(&b, "Tiếp tục từ chỗ dở, đừng làm lại phần đã xong; kiểm lại trạng thái cây làm việc trước khi sửa. Mọi quy tắc của đề bài "+
		"ban đầu vẫn áp dụng; mọi lệnh `bint task …` truyền ID tác vụ %s.\n", id)
	if len(answered) > 0 {
		b.WriteString("\nNgười dùng đã trả lời các câu hỏi mở trong requirement.md (nguyên văn):\n")
		for _, q := range answered {
			b.WriteString(q + "\n")
		}
		fmt.Fprintf(&b, "\nCập nhật requirement.md, plan.md và tasks.md theo các câu trả lời (`bint task write %s <requirement|plan|tasks> -`). "+
			"Nếu câu trả lời làm nảy sinh câu hỏi mới, ghi chúng vào requirement.md dạng `- [ ] Q-N: <câu hỏi>` (đánh số tiếp, giữ nguyên "+
			"các dòng đã trả lời); hết câu hỏi thì không thêm. Xong thì dừng.\n", id)
	}
	return b.String()
}

// answeredFor đọc requirement.md của tác vụ và trả các dòng câu hỏi đã trả lời (nil nếu không có tệp).
func answeredFor(t *task.Task) []string {
	b, err := os.ReadFile(t.ArtifactPath("requirement"))
	if err != nil {
		return nil
	}
	return task.AnsweredQuestions(string(b))
}

// decideContinuation dựng dữ kiện (kể cả chụp fingerprint cây, chỉ khi cần) rồi quyết định nối tiếp cho bước sắp mở.
func (r *Runner) decideContinuation(ctx context.Context, rs *runState, step string, fresh bool) ContinueDecision {
	last := rs.t.State.LastStep
	f := ContinueFacts{Last: last, Step: step, Fresh: fresh}
	if last != nil {
		f.LastIsPlan = rs.kindOf(last.Step) == flow.KindPlan
		if !fresh && last.Via == ViaSubagent && last.Continuable {
			f.Fingerprint, _ = r.stopFingerprint(ctx)
		}
	}
	return continuationDecision(f)
}

// planContinuation: resume tác vụ đang chờ câu trả lời của b-req. ok=false khi không thuộc ca này (không có LastStep
// awaiting_human của bước plan, hoặc requirement.md chưa có câu trả lời / còn câu hỏi mở / artifact lỗi cấu trúc: hành vi cũ
// tự chặn lại). ok=true thì quyết định nối tiếp planner cũ ở thư mục của tác vụ (Root chưa đổi ở thời điểm này).
func (r *Runner) planContinuation(ctx context.Context, rs *runState, fresh bool) (ContinueDecision, bool) {
	t := rs.t
	last := t.State.LastStep
	if last == nil || last.StopClass != StopAwaitingHuman || rs.kindOf(last.Step) != flow.KindPlan ||
		t.State.ResumeFrom != task.StatusPlanned || len(answeredFor(t)) == 0 {
		return ContinueDecision{}, false
	}
	if rep := task.LintPlanningTask(t, true); rep.Blocking() || len(rep.OpenQuestions) > 0 {
		return ContinueDecision{}, false
	}
	f := ContinueFacts{Last: last, Step: last.Step, LastIsPlan: true, Fresh: fresh}
	if !fresh && last.Via == ViaSubagent && last.Continuable {
		if root, err := r.RootFor(t); err == nil {
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			f.Fingerprint, _ = WorktreeSnapshot(c, root)
			cancel()
		}
	}
	return continuationDecision(f), true
}

// armContinuation biến bước vừa mở (ps, đề bài đầy đủ trong ps.Prompt) thành bước nối tiếp phiên của last: lời nhắn nối tiếp
// thay Prompt, đề bài đầy đủ giữ ở FullPrompt (--fresh đổi lại), bộ đếm/lượt gốc/mốc usage kế thừa từ bước dừng.
func (r *Runner) armContinuation(rs *runState, ps *task.PendingStep, last *task.StepStop) {
	var answered []string
	if last.StopClass == StopAwaitingHuman && rs.kindOf(last.Step) == flow.KindPlan {
		answered = answeredFor(rs.t)
	}
	ps.FullPrompt = ps.Prompt
	ps.ContinueAgentID = last.SessionID
	ps.Continuations = continuationCount(last)
	ps.OriginRunID = cmp.Or(last.OriginRunID, last.RunID)
	ps.UsageBase = last.Usage
	ps.Prompt = continuationPrompt(rs.t.State.ID, ps.Step, ps.RunID, last, answered)
}

// freshenPending đổi bước dở còn hạn đang là bước nối tiếp sang đề bài đầy đủ (giữ run_id, hợp đồng thuê, khoá). Trả phiếu mới,
// hoặc nil nếu không có gì để đổi (không có bước nối tiếp, hết hạn, hoặc tác vụ đang dừng: chỉ còn lệnh đóng bước).
func (r *Runner) freshenPending(t *task.Task) *task.Task {
	ps := t.State.PendingStep
	if ps == nil || ps.ContinueAgentID == "" || !t.State.PendingActive(leaseNow()) || task.Paused(t.State.Status) {
		return nil
	}
	origin := ps.OriginRunID
	nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
		p := t.State.PendingStep
		if p == nil || p.RunID != ps.RunID || p.ContinueAgentID == "" {
			return nil
		}
		p.Prompt, p.FullPrompt = p.FullPrompt, ""
		p.ContinueAgentID, p.OriginRunID, p.Continuations, p.UsageBase = "", "", 0, nil
		return nil
	})
	if err != nil || nt.State.PendingStep == nil || nt.State.PendingStep.ContinueAgentID != "" {
		return nil
	}
	removeSessionStep(r.Tasks.Dir, origin)
	return nt
}
