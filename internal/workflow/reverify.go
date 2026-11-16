package workflow

import (
	"context"
	"slices"
	"strings"

	"bint/internal/task"
)

// staleVerifyOnly cho biết cổng done của t chỉ bị chặn vì lần kiểm chứng cuối đã cũ do tệp NGOÀI tác vụ đổi sau đó
// (người hay tác vụ khác commit/land vào cùng cây), và trả về các tệp đó. Khi đúng, Finalize tự chạy lại lệnh kiểm
// chứng đã được người xác nhận (kiểm chứng phải chạy trên mã hiện tại, không bỏ qua cổng) thay vì dừng chờ người.
//
// Điều kiện: phiếu có mã (không phải chỉ lập kế hoạch); lệnh kiểm chứng đã xác nhận, lần chạy cuối qua, đúng lệnh
// hiện tại và có ảnh chụp cây; vân tay mã khác lần đó; mọi tệp đổi kể từ đó đều ngoài Touched (có ít nhất một);
// và nếu coi lần kiểm chứng là còn mới thì mọi điều chặn khác của cổng done (đường dẫn bảo vệ chưa duyệt, tiêu chí
// chưa đạt, đưa về xung đột...) đều rỗng: lý do chặn khác nào cũng loại việc chạy lại, không phải liệt kê tay.
func (e *Env) staleVerifyOnly(ctx context.Context, t *task.Task) ([]string, bool) {
	if e.planOnlyTask(t) || t.State.Landed != nil {
		return nil, false
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" || !e.VerifyConfirmed(t) {
		return nil, false
	}
	v := t.State.LastVerification()
	if v == nil || v.Command != cmd || v.Tree == "" || v.Worktree == "" {
		return nil, false
	}
	if !v.Passed {
		return e.failedVerifyOutdated(ctx, t, v)
	}
	pg := e.planGateFor(ctx, t)
	in, err := e.DoneInputDetailed(ctx, t)
	if err != nil || in.Worktree == "" || in.Worktree == v.Worktree ||
		len(in.OutsideSinceVerify) == 0 || len(in.TouchedSinceVerify) > 0 {
		return nil, false
	}
	fresh := in
	fresh.Worktree = v.Worktree // coi như lần kiểm chứng còn mới: chỉ xét các lý do chặn còn lại
	fresh.OutsideSinceVerify = nil
	fresh.IgnoreLanding = t.State.NeedsLand()
	if len(e.doneBlockersFor(ctx, t, fresh, nil, pg)) > 0 {
		return nil, false
	}
	return in.OutsideSinceVerify, true
}

// failedVerifyOutdated: lần kiểm chứng cuối KHÔNG qua nhưng nội dung mã đã đổi kể từ đó (người sửa nguyên nhân bằng
// commit vào nhánh tác vụ, cherry-pick...; kể cả tệp Touched): kết quả cũ không còn nói gì về mã hiện tại nên cũng
// phải chạy lại. Chỉ khi, coi lần kiểm chứng là qua và còn mới, cổng done không còn điều chặn nào khác. Commit chỉ đổi
// HEAD mà cây giữ nguyên thì không phải thay đổi (kết quả cũ còn đúng). Trả các tệp đã đổi.
func (e *Env) failedVerifyOutdated(ctx context.Context, t *task.Task, v *task.Verification) ([]string, bool) {
	root, err := e.RootFor(t)
	if err != nil {
		return nil, false
	}
	cur, err := WorktreeSnapshot(ctx, root)
	if err != nil || cur == v.Tree {
		return nil, false
	}
	files, err := DiffPaths(ctx, root, v.Tree, cur)
	if err != nil || len(files) == 0 {
		return nil, false
	}
	in, err := e.DoneInputDetailed(ctx, t)
	if err != nil || in.Worktree == "" {
		return nil, false
	}
	// Bản sao phiếu có lần kiểm chứng cuối "qua" và ứng với mã hiện tại: chỉ còn lại các lý do chặn khác.
	tc := *t
	tc.State.Verifications = slices.Clone(t.State.Verifications)
	tc.State.Verifications[len(tc.State.Verifications)-1].Passed = true
	tc.State.Verifications[len(tc.State.Verifications)-1].Scope = ""
	in.Worktree = v.Worktree
	in.OutsideSinceVerify = nil
	in.IgnoreLanding = t.State.NeedsLand()
	if len(e.doneBlockersFor(ctx, &tc, in, nil, e.planGateFor(ctx, t))) > 0 {
		return nil, false
	}
	return files, true
}

// reverifyReason là lý do ghi vào Verification.Reason khi Finalize tự kiểm chứng lại.
func reverifyReason(files []string) string {
	return "mã khác đổi sau lần kiểm chứng: " + strings.Join(files, ", ")
}

// partialVerifyOnly: lần kiểm chứng cuối đã qua nhưng chỉ chạy test bị ảnh hưởng (workflow.verify.scope: affected) và,
// nếu coi nó là đầy đủ và còn mới, cổng done không còn điều chặn nào khác. Khi đó Finalize chạy verify.command đầy đủ
// một lần trước done/land (cổng done không nhận lần kiểm chứng một phần).
func (e *Env) partialVerifyOnly(ctx context.Context, t *task.Task) bool {
	if e.planOnlyTask(t) || t.State.Landed != nil {
		return false
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" || !e.VerifyConfirmed(t) {
		return false
	}
	v := t.State.LastVerification()
	if v == nil || v.Command != cmd || !v.Passed || v.Full() {
		return false
	}
	in, err := e.DoneInputDetailed(ctx, t)
	if err != nil {
		return false
	}
	tc := *t
	tc.State.Verifications = slices.Clone(t.State.Verifications)
	tc.State.Verifications[len(tc.State.Verifications)-1].Scope = ""
	in.Worktree = v.Worktree
	in.OutsideSinceVerify = nil
	in.IgnoreLanding = t.State.NeedsLand()
	return len(e.doneBlockersFor(ctx, &tc, in, nil, e.planGateFor(ctx, t))) == 0
}

const partialReasonPrefix = "lần kiểm chứng trước chỉ chạy test bị ảnh hưởng (scope "

// partialReason là lý do ghi vào Verification.Reason khi Finalize chạy đầy đủ sau lần kiểm chứng một phần.
func partialReason(scope string) string {
	return partialReasonPrefix + scope + "): chạy đầy đủ trước khi done"
}

// fixVerifyReasonPrefix mở đầu lý do chuyển in_review → in_progress của fixAfterFullVerify (vòng sửa vì kiểm chứng đầy
// đủ ở finalize hỏng); markVerifiedFixResume nhận ra vòng sửa đó qua lịch sử trạng thái.
const fixVerifyReasonPrefix = "sửa: kiểm chứng đầy đủ ở finalize hỏng"

// freshFullPass: lần kiểm chứng mới nhất của t (của Runner hay do người chạy `bint task verify`) đã được xác nhận, qua,
// chạy đầy đủ, đúng lệnh hiện tại và trên đúng cây hiện tại (vân tay worktree trùng). Khi đó kiểm chứng lại chỉ lặp kết
// quả đã có.
func (e *Env) freshFullPass(ctx context.Context, t *task.Task) bool {
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" || !e.VerifyConfirmed(t) {
		return false
	}
	v := t.State.LastVerification()
	if v == nil || !v.Passed || !v.Confirmed || !v.Full() || v.Command != cmd || v.Worktree == "" {
		return false
	}
	root, err := e.RootFor(t)
	if err != nil {
		return false
	}
	return task.WorktreeFingerprint(ctx, root) == v.Worktree
}

// inVerifyFix: tác vụ đang ở vòng sửa do fixAfterFullVerify mở, tức lần rời in_review gần nhất là chuyển sang in_progress
// với lý do fixVerifyReasonPrefix (có thể bị chặn/ngắt sau đó rồi resume về in_progress).
func inVerifyFix(t *task.Task) bool {
	h := t.State.History
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].From == task.StatusInReview {
			return h[i].To == task.StatusInProgress && strings.HasPrefix(h[i].Reason, fixVerifyReasonPrefix)
		}
	}
	return false
}

// markVerifiedFixResume: resume (về in_progress) một tác vụ đang ở vòng sửa vì kiểm chứng đầy đủ ở finalize hỏng, trong khi
// người đã chạy lại kiểm chứng và lần mới nhất qua đầy đủ trên đúng cây hiện tại, và cây vẫn y như lúc lần đầy đủ hỏng
// (chưa có sửa nào; review trước finalize vẫn đúng cây này). Vòng sửa không còn lý do: đưa tác vụ về in_review và vào thẳng
// finalize (không chạy lại b-feat/b-review, không kiểm chứng lại) như khi kiểm chứng ở finalize đã xanh. Gọi sau khi đã
// chuyển trạng thái. Không áp với --rerun.
func (r *Runner) markVerifiedFixResume(ctx context.Context, rs *runState, to string, rerun bool) {
	st := &rs.t.State
	if rerun || to != task.StatusInProgress || !rs.chain() || st.HeldStep != nil || st.Landed != nil || st.AwaitingCheck ||
		!inVerifyFix(rs.t) {
		return
	}
	vs := st.Verifications
	if len(vs) < 2 {
		return
	}
	last := vs[len(vs)-1]
	var failed *task.Verification
	for i := len(vs) - 2; i >= 0; i-- {
		if !vs[i].Passed {
			failed = &vs[i]
			break
		}
	}
	if failed == nil || !failed.Full() || !strings.HasPrefix(failed.Reason, partialReasonPrefix) || failed.Worktree != last.Worktree {
		return
	}
	if !r.freshFullPass(ctx, rs.t) {
		return
	}
	if err := r.transition(rs, task.StatusInReview, "bỏ vòng sửa: lần kiểm chứng đầy đủ mới nhất đã qua trên đúng cây hiện tại"); err != nil {
		r.logf("! %v", err)
		return
	}
	rs.verifiedResume = true
	note := "lần kiểm chứng đầy đủ mới nhất (" + last.At + ") đã qua trên đúng cây hiện tại: bỏ vòng sửa, vào thẳng finalize"
	r.logf("  %s", note)
	rs.res.Notes = append(rs.res.Notes, note)
}
