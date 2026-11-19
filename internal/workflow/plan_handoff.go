package workflow

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"bint/internal/flow"
	"bint/internal/task"
)

// Nối tiếp phiên b-req → b-feat (driver process). b-req kết thúc bình thường thì endStep ghi State.LastPlan (handle phiên, fingerprint
// cây, model); b-feat đầu tiên đổi nó thành LastStep lớp plan_handoff rồi đi qua cùng quyết định nối tiếp như mọi bước
// (continuationDecision: cùng harness, model, profile, cây không đổi, không --fresh, phiên còn). Lời nhắn nối tiếp chỉ gồm skill
// b-feat và kế hoạch rút gọn, không chép lại task.yml lẫn khối tri thức mà phiên b-req đã nạp.

// recordLastPlan ghi State.LastPlan khi bước plan kết thúc bình thường và có handle phiên. fp là ảnh chụp cây sau bước chỉ đọc
// (rỗng thì chụp lại); không chụp được thì không nối tiếp.
func (r *Runner) recordLastPlan(ctx context.Context, rs *runState, sr *stepRun, handle, fp string) {
	if handle == "" {
		return
	}
	if fp == "" {
		fp, _ = r.stopFingerprint(ctx)
	}
	lp := &task.StepStop{Step: sr.step, RunID: sr.child, Harness: sr.harness, Profile: sr.profileID(), Model: sr.reqModel, SessionID: handle, Fingerprint: fp,
		Continuable: fp != "", At: r.at(), Continuations: sr.continuations, OriginRunID: sr.originRunID, Usage: sr.usage, Via: viaOf(rs, sr.harness)}
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.LastPlan = lp; return nil }); err == nil {
		rs.t = t
	}
}

// armPlanHandoff: bước implement đầu tiên của tác vụ (driver process) mà b-req vừa kết thúc bình thường để lại State.LastPlan thì
// đặt State.LastStep = plan_handoff (Step là bước implement sắp mở) để armProcessContinuation quyết định nối tiếp. Không đặt khi
// đã có LastStep (b-feat dừng chờ người, vòng sửa...): các lớp đó có đường nối tiếp riêng.
func (r *Runner) armPlanHandoff(ctx context.Context, rs *runState, sr *stepRun) {
	lp := rs.t.State.LastPlan
	if rs.session || sr.kind != flow.KindImplement || !sr.firstFeat || lp == nil || rs.t.State.LastStep != nil {
		return
	}
	ls := *lp
	ls.Step, ls.StopClass, ls.At = sr.step, StopPlanHandoff, r.at()
	ls.Continuable = stopContinuable(StopPlanHandoff, lp.SessionID, lp.Fingerprint)
	r.setLastStep(rs, &ls)
}

var (
	acLineRe  = regexp.MustCompile(`^\s*[-*]\s*\[[ xX]\]\s*AC-\d+`)
	tskLineRe = regexp.MustCompile(`^\s*[-*]\s*\[[ xX]\]\s*TSK-\d+`)
)

// artifactLines trả các dòng của artifact name khớp re (kế hoạch rút gọn: chỉ dòng AC/TSK, không chép thân tài liệu).
func artifactLines(t *task.Task, name string, re *regexp.Regexp) []string {
	b, err := os.ReadFile(t.ArtifactPath(name))
	if err != nil {
		return nil
	}
	var out []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if re.MatchString(l) {
			out = append(out, strings.TrimRight(l, " \t\r"))
		}
	}
	return out
}

// planHandoffPrompt dựng lời nhắn chuyển phiên b-req sang b-feat: skill b-feat (gốc + lớp phủ), thư mục làm việc, đường dẫn bảo vệ,
// kế hoạch rút gọn (dòng AC của requirement.md, dòng TSK của tasks.md, câu hỏi đã trả lời) và đường dẫn tài liệu để tra khi cần.
// Không có task.yml và khối tri thức: phiên đã đọc chúng ở b-req (`bint task show <id>` xem lại phiếu).
func (r *Runner) planHandoffPrompt(rs *runState, sr *stepRun, l *task.StepStop) string {
	t := rs.t
	sd, _ := rs.flow.StepByID(sr.step)
	sk, _ := r.stepSkill(cmp.Or(sd.Skill, sr.step)) // lỗi đã được beginStep chặn trước
	body := strings.ReplaceAll(sk.Body(), "$ARGUMENTS", "tác vụ "+t.State.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "b-req của tác vụ %s đã xong (lượt %s). Giờ bạn làm bước %s (lượt mới %s) NGAY TRONG phiên này: từ đây bạn có quyền ghi và "+
		"làm theo skill b-feat dưới đây, thay cho các quy tắc chỉ đọc của b-req. Phiếu (task.yml) và tri thức dự án bạn đã đọc ở b-req nên không "+
		"chép lại; `bint task show %s` xem lại phiếu khi cần.\n\n", t.State.ID, l.RunID, sr.step, sr.child, t.State.ID)
	b.WriteString(body)
	fmt.Fprintf(&b, "\n\n---\n\n<bint-task id=%q step=%q run=%q>\n", t.State.ID, sr.step, sr.child)
	b.WriteString("Bạn đang chạy dưới `bint run` (Bint Core điều phối). Không gọi `bint task status`, `bint task approve` hay `bint task verify` " +
		"— Bint Core chuyển trạng thái, chạy lệnh kiểm chứng và quyết định hoàn thành. Task ID cũng có trong biến BINT_TASK_ID.\n\n")
	if t.State.Origin != "" {
		b.WriteString(r.worktreeNote(t) + "\n")
	}
	if len(r.Cfg.Limits.ProtectedPaths) > 0 {
		fmt.Fprintf(&b, "Đường dẫn được bảo vệ (cần người duyệt): %s\n\n", strings.Join(r.Cfg.Limits.ProtectedPaths, ", "))
	}
	b.WriteString("Kế hoạch rút gọn (từ tài liệu b-req vừa ghi):\n")
	if ac := artifactLines(t, "requirement", acLineRe); len(ac) > 0 {
		b.WriteString("Tiêu chí chấp nhận (requirement.md):\n" + strings.Join(ac, "\n") + "\n")
	}
	if tsk := artifactLines(t, "tasks", tskLineRe); len(tsk) > 0 {
		b.WriteString("Công việc (tasks.md):\n" + strings.Join(tsk, "\n") + "\n")
	}
	if qs := answeredFor(t); len(qs) > 0 {
		fmt.Fprintf(&b, "Câu hỏi đã trả lời trong requirement.md (nguyên văn; đây là quyết định của người dùng, làm theo):\n%s\n", strings.Join(qs, "\n"))
	}
	b.WriteString(qaBlock(t.State.AnsweredQuestions(sr.step)))
	b.WriteString(goalCheckBlock(t))
	b.WriteString(sliceBlock(t)) // tác vụ chia lát: lượt nối tiếp từ b-req cũng chỉ làm lát hiện tại (slice.go)
	for _, n := range task.ArtifactNames() {
		if p := t.ArtifactPath(n); fileExists(p) {
			fmt.Fprintf(&b, "- %s: %s\n", n, p)
		}
	}
	b.WriteString("Chi tiết thiết kế và rủi ro nằm trong plan.md: đọc khi cần, đừng làm lại phân tích của b-req.\n")
	b.WriteString("</bint-task>\n")
	return b.String()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
