package workflow

import (
	"cmp"
	"context"
	"time"

	"bint/internal/eval"
	"bint/internal/flow"
	"fmt"
	"os"
	"strings"

	"bint/internal/task"
)

// Nối tiếp phiên của bước (docs/step-continuation-plan.md giai đoạn 2 và 3). Quyết định là hàm thuần dùng chung cho driver
// session (Via = ViaSubagent, nối tiếp bằng SendMessage; việc mở bước đi qua beginStep/pendingFrom/grantPending nên mọi cổng
// khoá, lease, ngân sách giữ nguyên) và driver process (Via = tên harness, runStep gọi lại harness ở chế độ resume).

// ViaSubagent là State.LastStep.Via của bước chạy bằng subagent (nối tiếp bằng SendMessage tới agent cũ). Bước driver process
// ghi Via = tên harness ("claude", "codex").
const ViaSubagent = "subagent"

// resumableVia: cách chạy có đường nối tiếp phiên (subagent: SendMessage; claude: `--resume`; codex: `exec resume`).
func resumableVia(via string) bool {
	switch via {
	case ViaSubagent, "claude", "codex":
		return true
	}
	return false
}

// modelSwitchVia: cách chạy đã kiểm thật việc nối tiếp phiên sang model khác (docs/acceptance.md NT-CONT-CLAUDE-MODEL,
// NT-CONT-CODEX-MODEL). Cách chạy khác (subagent...) giữ chặn khác-model cho tới khi được kiểm.
func modelSwitchVia(via string) bool {
	return via == "claude" || via == "codex"
}

// viaDesc mô tả cách chạy trong lý do không nối tiếp.
func viaDesc(via string) string {
	if via == ViaSubagent {
		return "subagent của driver session"
	}
	return "harness " + via
}

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
	// Via là cách chạy của bước sắp mở: ViaSubagent (driver session, mặc định khi để trống) hoặc tên harness của driver process.
	// Phải khớp Last.Via (không nối tiếp chéo subagent/harness hay claude/codex) và hỗ trợ nối tiếp (resumableVia).
	Via string
	// Profile là profile `bint iso` (<tool>/<tên>, "" = không dùng) của bước sắp mở; phải bằng Last.Profile: phiên nằm trong thư
	// mục cấu hình của profile (tài khoản khác không thấy phiên đó), nên đổi profile giữa hai lượt thì chạy mới.
	Profile string
	// Fingerprint là fingerprint cây hiện tại ("" = không chụp được); Fresh: người yêu cầu bỏ nối tiếp (--fresh).
	Fingerprint string
	Fresh       bool
	// PlanNotReady khác rỗng: Last là awaiting_human của bước plan nhưng planner chưa nên được nối tiếp (chưa có câu trả lời,
	// còn câu hỏi mở hoặc artifact lỗi cấu trúc); giá trị là lý do (planResumeWhy).
	PlanNotReady string
	// Model là model yêu cầu của bước sắp mở; lớp plan_handoff nối tiếp được khi khác Last.Model chỉ với cách chạy đã kiểm (modelSwitchVia: claude, codex).
	Model string
	// ImplAnswered: Last là awaiting_human của bước implement (câu hỏi của b-feat hoặc b-feat không đổi tệp) và người đã trả lời
	// sau lúc dừng (`--answer`): b-feat được nối tiếp với câu trả lời. Không có câu trả lời thì bước implement chờ người vẫn chạy mới.
	ImplAnswered bool
}

// ContinueDecision là kết quả: Continue=true thì mở lại Step dạng nối tiếp; Why giải thích khi không nối tiếp (vào notes).
type ContinueDecision struct {
	Continue bool
	Step     string
	Why      string
}

// continuationDecision quyết định có nối tiếp phiên của bước dừng gần nhất không. Thứ tự: không có LastStep hoặc bước sắp mở khác
// bước dừng (không có gì để nối tiếp: Why rỗng, kể cả khi --fresh, để notes không nhiễu) → fresh → harness không hỗ trợ → khác
// cách chạy (Via) → không continuable → awaiting_human không phải plan / plan chưa sẵn sàng → fingerprint khác → quá giới hạn.
// Thiếu một điều kiện thì chạy đề bài đầy đủ.
func continuationDecision(f ContinueFacts) ContinueDecision {
	no := func(why string) ContinueDecision { return ContinueDecision{Why: why} }
	l := f.Last
	via := cmp.Or(f.Via, ViaSubagent)
	switch {
	case l == nil || l.Step != f.Step:
		return ContinueDecision{}
	case f.Fresh:
		return no("--fresh: không nối tiếp phiên cũ, dùng đề bài đầy đủ")
	case !resumableVia(via):
		return no(viaDesc(via) + " không hỗ trợ nối tiếp phiên")
	case l.Via != via:
		if l.Via == "" {
			return no("bước dừng trước không chạy bằng " + viaDesc(via) + " (không ghi cách chạy: phiếu cũ hoặc bước chưa có phiên để nối tiếp)")
		}
		return no(fmt.Sprintf("bước dừng trước chạy bằng %s, bước sắp mở chạy bằng %s: không nối tiếp chéo", viaDesc(l.Via), viaDesc(via)))
	case l.Profile != f.Profile:
		return no(fmt.Sprintf("bước dừng trước chạy %s, bước sắp mở chạy %s: phiên cũ nằm ở tài khoản khác, chạy mới với đề bài đầy đủ",
			profileDesc(l.Profile), profileDesc(f.Profile)))
	case l.StopClass == StopPlanHandoff && l.Model != f.Model && !modelSwitchVia(via):
		return no(fmt.Sprintf("b-req chạy model %s, bước %s chạy model %s: %s chưa được kiểm việc nối tiếp phiên sang model khác, dùng đề bài đầy đủ",
			modelDesc(l.Model), l.Step, modelDesc(f.Model), viaDesc(via)))
	case !l.Continuable:
		return no(fmt.Sprintf("bước %s dừng với lớp %s, không nối tiếp được (thiếu phiên/fingerprint hoặc lỗi nội tại)", l.Step, l.StopClass))
	case l.StopClass == StopAwaitingHuman && !f.LastIsPlan && !f.ImplAnswered:
		return no("chỉ bước plan chờ người (hoặc bước implement đã có câu trả lời của người) mới nối tiếp phiên; bước " + l.Step + " chạy mới")
	case l.StopClass == StopAwaitingHuman && f.PlanNotReady != "":
		return no(f.PlanNotReady)
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

// modelDesc mô tả model trong lý do không nối tiếp.
func modelDesc(m string) string {
	if m == "" {
		return "mặc định của harness"
	}
	return m
}

// profileDesc mô tả profile trong lý do không nối tiếp.
func profileDesc(id string) string {
	if id == "" {
		return "không có profile (tài khoản thật)"
	}
	return "ở profile " + id
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
	case StopReviewFix:
		b.WriteString("Lý do nối tiếp: lượt trước đã xong, nhưng cần sửa tiếp: b-review tìm thấy phát hiện nghiêm trọng và/hoặc lần kiểm chứng cuối không qua (xem bên dưới).\n")
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

// planResumeWhy trả lý do planner chưa nên được nối tiếp ("" = sẵn sàng): requirement.md phải có câu hỏi đã trả lời, không còn
// câu hỏi mở và artifact không lỗi cấu trúc (không thì hành vi cũ tự chặn lại). Dùng chung cho driver session và driver process.
func planResumeWhy(t *task.Task) string {
	if len(answeredFor(t)) == 0 {
		return "requirement.md chưa có câu hỏi nào được trả lời"
	}
	if rep := task.LintPlanningTask(t, true); rep.Blocking() {
		return "artifact b-req còn lỗi cấu trúc, chưa nối tiếp planner"
	} else if len(rep.OpenQuestions) > 0 {
		return "requirement.md còn câu hỏi mở chưa trả lời, chưa nối tiếp planner"
	}
	return ""
}

// decideContinuation dựng dữ kiện (kể cả chụp fingerprint cây, chỉ khi cần) rồi quyết định nối tiếp cho bước sắp mở chạy bằng
// via (ViaSubagent hoặc tên harness của driver process).
func (r *Runner) decideContinuation(ctx context.Context, rs *runState, step string, fresh bool, via, profile, model string) ContinueDecision {
	last := rs.t.State.LastStep
	f := ContinueFacts{Last: last, Step: step, Fresh: fresh, Via: via, Profile: profile, Model: model}
	if last != nil {
		f.LastIsPlan = rs.kindOf(last.Step) == flow.KindPlan
		if f.LastIsPlan && last.StopClass == StopAwaitingHuman {
			f.PlanNotReady = planResumeWhy(rs.t)
		}
		if last.StopClass == StopAwaitingHuman && rs.kindOf(last.Step) == flow.KindImplement {
			for _, q := range rs.t.State.AnsweredQuestions(last.Step) {
				if q.AnsweredAt >= last.At {
					f.ImplAnswered = true
				}
			}
		}
		if !fresh && last.Via == via && last.Profile == profile && last.Continuable && resumableVia(via) {
			f.Fingerprint, _ = r.stopFingerprint(ctx)
		}
	}
	return continuationDecision(f)
}

// planContinuation: resume/chạy tiếp tác vụ đang chờ câu trả lời của b-req. ok=false khi không thuộc ca này (tác vụ không ở
// trạng thái planned (status), không có LastStep awaiting_human của bước plan, hoặc requirement.md chưa có câu trả lời / còn câu
// hỏi mở / artifact lỗi cấu trúc: hành vi cũ tự chặn lại). ok=true thì quyết định nối tiếp planner cũ ở thư mục của tác vụ (Root
// chưa đổi ở thời điểm này). session=false (driver process): via là harness mà role của bước plan sẽ chạy.
func (r *Runner) planContinuation(ctx context.Context, rs *runState, fresh, session bool, status string) (ContinueDecision, bool) {
	t := rs.t
	last := t.State.LastStep
	if last == nil || last.StopClass != StopAwaitingHuman || rs.kindOf(last.Step) != flow.KindPlan || status != task.StatusPlanned ||
		planResumeWhy(t) != "" {
		return ContinueDecision{}, false
	}
	via, profile := ViaSubagent, ""
	if !session {
		if via, profile = r.processHarness(rs, last.Step); via == "" {
			return ContinueDecision{}, false
		}
	}
	f := ContinueFacts{Last: last, Step: last.Step, LastIsPlan: true, Fresh: fresh, Via: via, Profile: profile}
	if !fresh && last.Via == via && last.Profile == profile && last.Continuable && resumableVia(via) {
		if root, err := r.RootFor(t); err == nil {
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			f.Fingerprint, _ = WorktreeSnapshot(c, root)
			cancel()
		}
	}
	return continuationDecision(f), true
}

// processHarness là harness mà bước step sẽ chạy ở driver process (tên agent của factory; "" nếu không dựng được) cùng profile
// `bint iso` của agent đó (<tool>/<tên>, "" nếu không dùng). Chỉ để quyết định nối tiếp trước khi mở bước: agent dựng ở đây bị
// bỏ, không chạy.
func (r *Runner) processHarness(rs *runState, step string) (harness, profile string) {
	if r.Agent == nil {
		return "", ""
	}
	a, err := r.Agent(rs.spec(step), nil, nil)
	if err != nil || a == nil {
		return "", ""
	}
	if p := eval.EffectiveProfile(a, eval.AgentRequest{}); p != nil {
		profile = p.ID()
	}
	return a.Name(), profile
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
	ps.Prompt = continuationPrompt(rs.t.State.ID, ps.Step, ps.RunID, last, answered) + r.continuationExtra(rs, last)
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
