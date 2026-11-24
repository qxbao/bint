package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/task"
)

func TestAutoRetryDecision(t *testing.T) {
	sched := []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute}
	ok := AutoRetryFacts{Class: StopExternal, Continuable: true, Schedule: sched}
	with := func(mod func(*AutoRetryFacts)) AutoRetryFacts { f := ok; mod(&f); return f }
	cases := []struct {
		name  string
		f     AutoRetryFacts
		retry bool
		wait  time.Duration
		n     int
	}{
		{"lần đầu", ok, true, 30 * time.Second, 1},
		{"lần hai", with(func(f *AutoRetryFacts) { f.Continuations = 1 }), true, 2 * time.Minute, 2},
		{"lần ba", with(func(f *AutoRetryFacts) { f.Continuations = 2 }), true, 5 * time.Minute, 3},
		{"hết lịch / chạm maxContinueExternal", with(func(f *AutoRetryFacts) { f.Continuations = 3 }), false, 0, 0},
		{"lịch ngắn hơn bộ đếm", with(func(f *AutoRetryFacts) { f.Schedule = sched[:1]; f.Continuations = 1 }), false, 0, 0},
		{"lịch rỗng = tắt", with(func(f *AutoRetryFacts) { f.Schedule = nil }), false, 0, 0},
		{"intrinsic", with(func(f *AutoRetryFacts) { f.Class = StopIntrinsic }), false, 0, 0},
		{"timeout", with(func(f *AutoRetryFacts) { f.Class = StopTimeout }), false, 0, 0},
		{"awaiting_human", with(func(f *AutoRetryFacts) { f.Class = StopAwaitingHuman }), false, 0, 0},
		{"người huỷ", with(func(f *AutoRetryFacts) { f.Interrupted = true }), false, 0, 0},
		{"--fresh", with(func(f *AutoRetryFacts) { f.Fresh = true }), false, 0, 0},
		{"không nối tiếp được", with(func(f *AutoRetryFacts) { f.Continuable = false }), false, 0, 0},
	}
	for _, tc := range cases {
		got := autoRetryDecision(tc.f)
		if got.Retry != tc.retry || got.Wait != tc.wait || got.Attempt != tc.n || (!tc.retry && got.Why == "") {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

func TestAutoRetryProcessRecoversInOneRun(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.cfg.Workflow.ExternalRetry = []string{"30s", "2m"}
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n == 1 {
			externalFail(res)
		}
	})
	res, id := startDo(t, h)
	if res.Status != task.StatusDone {
		t.Fatalf("status = %s (%s), muốn done sau một lần tự thử", res.Status, res.Reason)
	}
	if len(h.waits) != 1 || h.waits[0] != 30*time.Second {
		t.Fatalf("lần chờ = %v, muốn [30s]", h.waits)
	}
	var first, second eval.AgentRequest
	n := 0
	for i, c := range h.calls {
		if c == StepFeat {
			if n == 0 {
				first = h.reqs[i]
			} else {
				second = h.reqs[i]
			}
			n++
		}
	}
	if n != 2 || second.ResumeSessionID == "" || second.ResumeSessionID != first.SessionID {
		t.Fatalf("b-feat phải chạy 2 lần, lần hai nối tiếp phiên %q: %d lần, %+v", first.SessionID, n, second)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "tự thử lại lần 1/2 sau 30s") {
		t.Fatalf("notes thiếu dòng tự thử lại: %v", res.Notes)
	}
	if loadTask(t, h, id).State.Status != task.StatusDone {
		t.Fatal("phiếu chưa done")
	}
}

func TestAutoRetryProcessExhaustedFailsWithCount(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.cfg.Workflow.ExternalRetry = []string{"1s", "2s"}
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat {
			externalFail(res)
		}
	})
	res, id := startDo(t, h)
	if res.Status != task.StatusFailed || !strings.Contains(res.Reason, "đã tự thử lại 2 lần") {
		t.Fatalf("status = %s, reason = %q", res.Status, res.Reason)
	}
	if len(h.waits) != 2 || h.waits[0] != time.Second || h.waits[1] != 2*time.Second {
		t.Fatalf("lần chờ = %v", h.waits)
	}
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.StopClass != StopExternal || ls.Continuations != 2 {
		t.Fatalf("LastStep = %+v", ls)
	}
}

func TestAutoRetryProcessSkipsIntrinsicAndDisabled(t *testing.T) {
	procSetup(t)
	// Lỗi nội tại (không phải hạ tầng): không tự thử.
	h := newHarness(t)
	h.cfg.Workflow.ExternalRetry = []string{"1s"}
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat {
			res.ExitCode, res.Error = 1, "agent tự báo lỗi"
		}
	})
	res, _ := startDo(t, h)
	if res.Status != task.StatusFailed || len(h.waits) != 0 {
		t.Fatalf("intrinsic: status = %s, waits = %v", res.Status, h.waits)
	}
	// Lịch rỗng = tắt: lỗi ngoại cảnh vẫn failed như cũ.
	h2 := newHarness(t)
	h2.cfg.Workflow.ExternalRetry = []string{}
	h2.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat {
			externalFail(res)
		}
	})
	res2, _ := startDo(t, h2)
	if res2.Status != task.StatusFailed || len(h2.waits) != 0 || strings.Contains(res2.Reason, "tự thử") {
		t.Fatalf("tắt: status = %s, waits = %v, reason = %q", res2.Status, h2.waits, res2.Reason)
	}
}

func TestAutoRetryCancelledWhileWaitingIsInterrupted(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.cfg.Workflow.ExternalRetry = []string{"30s"}
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat {
			externalFail(res)
		}
	})
	r := h.runner()
	r.RetrySleep = func(context.Context, time.Duration) bool { return false } // người huỷ giữa lúc chờ
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusInterrupted || countCalls(h, StepFeat) != 1 {
		t.Fatalf("status = %s, số lần b-feat = %d", res.Status, countCalls(h, StepFeat))
	}
}

func TestAutoRetrySessionWaitsThenContinues(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.ExternalRetry = []string{"30s", "2m"}
	r := h.runner()
	clock := time.Now()
	r.Env.Now = func() time.Time { return clock }
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || a.Action != ActionAgent {
		t.Fatalf("next: %+v %v", a, err)
	}
	// Lỗi ngoại cảnh: complete không trả failed mà trả wait kèm retry_at; tác vụ không failed.
	c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "A1", Failed: "API Error: 529 overloaded"})
	if err != nil || c.Action != ActionWait || c.RetryAt == "" || c.RetryAttempt != 1 || c.RetryMax != 2 {
		t.Fatalf("complete: %+v %v", c, err)
	}
	if st := loadTask(t, h, id).State; st.Status == task.StatusFailed || st.LastStep == nil || st.LastStep.RetryAt != c.RetryAt {
		t.Fatalf("trạng thái: %s %+v", st.Status, st.LastStep)
	}
	at, err := time.Parse(time.RFC3339, c.RetryAt)
	if err != nil || at.Sub(clock) < 29*time.Second || at.Sub(clock) > 31*time.Second {
		t.Fatalf("retry_at = %s (%v), muốn ~30s sau %s", c.RetryAt, err, clock)
	}
	// Gọi lại trước retry_at: vẫn wait, không mở bước.
	clock = clock.Add(10 * time.Second)
	w, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || w.Action != ActionWait || w.RetryAt != c.RetryAt {
		t.Fatalf("next sớm: %+v %v", w, err)
	}
	// Sau retry_at: bước nối tiếp phiên cũ.
	clock = clock.Add(time.Minute)
	n, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || n.Action != ActionAgent || n.Step != StepFeat || n.ContinueAgentID != "A1" {
		t.Fatalf("next sau retry_at: %+v %v", n, err)
	}
	// Lần hai cũng lỗi: wait lần 2/2; lần ba hết lịch thì failed, reason nêu số lần.
	c2, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: n.RunID, AgentID: "A1", Failed: "API Error: 529 overloaded"})
	if err != nil || c2.Action != ActionWait || c2.RetryAttempt != 2 {
		t.Fatalf("complete 2: %+v %v", c2, err)
	}
	clock = clock.Add(5 * time.Minute)
	n2, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || n2.Action != ActionAgent || n2.ContinueAgentID != "A1" {
		t.Fatalf("next 2: %+v %v", n2, err)
	}
	c3, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: n2.RunID, AgentID: "A1", Failed: "API Error: 529 overloaded"})
	if err != nil || c3.Action != ActionFailed || !strings.Contains(c3.Reason, "đã tự thử lại 2 lần") {
		t.Fatalf("complete 3: %+v %v", c3, err)
	}
}

func TestAutoRetrySessionDisabledStaysFailed(t *testing.T) {
	h := newHarness(t) // ExternalRetry = nil
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	failFeatSession(t, h, r, tk.State.ID, "A1") // chờ ActionFailed
}
