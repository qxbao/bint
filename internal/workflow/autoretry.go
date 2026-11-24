package workflow

import (
	"context"
	"fmt"
	"time"

	"bint/internal/task"
)

// Tự phục hồi khi bước dừng vì lỗi ngoại cảnh (lớp StopExternal: 5xx/529 quá tải, hạn mức, mất kết nối, harness chết): thay vì
// chuyển tác vụ sang failed, chờ theo lịch workflow.external_retry rồi nối tiếp phiên cũ. Lần thử thứ N dùng chung bộ đếm nối tiếp
// (StepStop.Continuations) nên tính vào maxContinueExternal; mỗi lần nối tiếp đi qua beginStep nên ngân sách limits vẫn được kiểm.

// AutoRetryFacts là dữ kiện của quyết định tự thử lại.
type AutoRetryFacts struct {
	// Class và Continuable là của bước dừng; Interrupted: người huỷ; Fresh: người yêu cầu bỏ nối tiếp.
	Class                         string
	Continuable, Interrupted, Fresh bool
	// Continuations là số lần nối tiếp external/timeout liên tiếp đã dùng cho bước (đã gồm các lần tự thử trước).
	Continuations int
	// Schedule là lịch chờ (workflow.external_retry); rỗng = tắt.
	Schedule []time.Duration
}

// AutoRetry là quyết định tự thử: Retry=true thì chờ Wait rồi nối tiếp (lần thứ Attempt/Max); Why giải thích khi không thử.
type AutoRetry struct {
	Retry   bool
	Wait    time.Duration
	Attempt int
	Max     int
	Why     string
}

// autoRetryDecision là hàm thuần quyết định có tự thử lại bước vừa dừng không. Chỉ lớp external, nối tiếp được, không do người huỷ,
// không --fresh, còn lượt trong lịch và dưới maxContinueExternal.
func autoRetryDecision(f AutoRetryFacts) AutoRetry {
	max := min(len(f.Schedule), maxContinueExternal)
	no := func(why string) AutoRetry { return AutoRetry{Max: max, Why: why} }
	switch {
	case len(f.Schedule) == 0:
		return no("workflow.external_retry rỗng (tắt tự thử lại)")
	case f.Class != StopExternal:
		return no("lớp dừng " + f.Class + " không tự thử lại")
	case f.Interrupted:
		return no("người huỷ")
	case f.Fresh:
		return no("--fresh")
	case !f.Continuable:
		return no("bước không nối tiếp được (thiếu phiên hoặc fingerprint)")
	case f.Continuations >= max:
		return no(fmt.Sprintf("đã dùng hết %d lượt tự thử", max))
	}
	return AutoRetry{Retry: true, Wait: f.Schedule[f.Continuations], Attempt: f.Continuations + 1, Max: max}
}

// retryTail là đuôi lý do failed nêu số lần đã tự thử ("" nếu chưa tự thử lần nào trong lượt này).
func retryTail(tries int, class string) string {
	if tries == 0 || class != StopExternal {
		return ""
	}
	return fmt.Sprintf(" (đã tự thử lại %d lần, vẫn lỗi ngoại cảnh)", tries)
}

// pendingRetry là lần tự thử đã quyết định, chờ runStep thực hiện sau khi endStep nhả khoá của bước.
type pendingRetry struct {
	dec    AutoRetry
	step   string
	reason string
	// retryAt (RFC3339): driver session; thời điểm sớm nhất thử lại.
	retryAt string
}

// sleepFor chờ d hoặc tới khi ctx bị huỷ; false nếu bị huỷ.
func (r *Runner) sleepFor(ctx context.Context, d time.Duration) bool {
	if r.RetrySleep != nil {
		return r.RetrySleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// waitAutoRetry chờ theo lịch rồi kiểm lại điều kiện nối tiếp (fingerprint cây không đổi...). Trả true để runStep chạy lại bước ở
// chế độ nối tiếp; false thì tác vụ đã được đặt interrupted hoặc failed.
func (r *Runner) waitAutoRetry(ctx context.Context, rs *runState, sr *stepRun, pr *pendingRetry) bool {
	if !r.sleepFor(ctx, pr.dec.Wait) {
		r.interrupt(rs, "người dùng huỷ trong lúc chờ tự thử lại bước "+sr.step)
		return false
	}
	d := r.decideContinuation(ctx, rs, sr.step, rs.opts.Fresh, sr.harness, sr.profileID(), sr.reqModel)
	if !d.Continue {
		why := cmp2(d.Why, "không nối tiếp được")
		r.fail(rs, sr.step, fmt.Sprintf("%s: %s; không tự thử lại được lần %d/%d: %s%s", sr.step, pr.reason, pr.dec.Attempt, pr.dec.Max, why,
			retryTail(rs.autoTries, StopExternal)))
		return false
	}
	return true
}

func cmp2(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// planAutoRetry gọi từ nhánh failed của endStep: quyết định và (nếu thử) ghi notes + sự kiện. Trả nil khi không thử.
func (r *Runner) planAutoRetry(rs *runState, sr *stepRun, stop *task.StepStop, ctxErr error, reason string) *pendingRetry {
	if stop == nil {
		return nil
	}
	dec := autoRetryDecision(AutoRetryFacts{Class: stop.StopClass, Continuable: stop.Continuable, Interrupted: ctxErr != nil, Fresh: rs.opts.Fresh,
		Continuations: stop.Continuations, Schedule: r.Cfg.Workflow.ExternalRetrySchedule()})
	if !dec.Retry {
		return nil
	}
	rs.autoTries++
	pr := &pendingRetry{dec: dec, step: sr.step, reason: truncate(reason, 200)}
	msg := fmt.Sprintf("bước %s dừng vì lỗi ngoại cảnh (%s): tự thử lại lần %d/%d sau %s, nối tiếp phiên cũ", sr.step, pr.reason, dec.Attempt, dec.Max, dec.Wait)
	if rs.session {
		// Driver session không chờ trong tiến trình: ghi thời điểm thử lại vào bước dừng; `bint run next` trả wait tới lúc đó.
		pr.retryAt = task.Now(r.now().Add(dec.Wait))
		stop.RetryAt = pr.retryAt
		r.setLastStep(rs, stop)
		msg += " (lúc " + pr.retryAt + ")"
	}
	r.logf("⏳ %s", msg)
	rs.res.Notes = append(rs.res.Notes, msg)
	r.RecordError(rs.t.State.ID, sr.step, "tự thử lại: "+msg)
	return pr
}

// retryWaitAction trả action wait khi tác vụ đang chờ lần tự thử lại (LastStep.RetryAt chưa tới); nil nếu không phải ca đó.
func (r *Runner) retryWaitAction(t *task.Task) *SessionAction {
	ls := t.State.LastStep
	if ls == nil || ls.RetryAt == "" || task.Paused(t.State.Status) {
		return nil
	}
	at, err := time.Parse(time.RFC3339, ls.RetryAt)
	if err != nil || !r.now().Before(at) {
		return nil
	}
	max := min(len(r.Cfg.Workflow.ExternalRetrySchedule()), maxContinueExternal)
	return &SessionAction{Action: ActionWait, TaskID: t.State.ID, Status: t.State.Status, RetryAt: ls.RetryAt, RetryAttempt: ls.Continuations + 1, RetryMax: max,
		Reason: fmt.Sprintf("bước %s dừng vì lỗi ngoại cảnh: tự thử lại lần %d/%d lúc %s; chờ tới lúc đó rồi gọi lại `bint run next %s --json`",
			ls.Step, ls.Continuations+1, max, ls.RetryAt, t.State.ID)}
}
