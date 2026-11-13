package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/task"
)

const sharedReq = "Thêm tính năng phiên bản v2 cho app.txt"

func implementCalls(h *harness) int {
	n := 0
	for _, c := range h.calls {
		if c == StepFeat {
			n++
		}
	}
	return n
}

// Phát hiện .8: phiếu b-req planned (shared, bị bỏ khỏi chooseIsolation) được chạy tiếp bằng b-do trong lúc cây chung
// có phiếu khác chưa kết thúc: dừng blocked, lý do nêu tác vụ đó và lối ra, không bước ghi nào chạy.
func TestPlanOnlyContinuedToWriteStepBlocksOnSharedTask(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.Isolation = config.IsolationAuto
	r := h.runner()
	ctx := context.Background()
	a := planOnlyPlanned(t, r)
	callsAfterPlan := len(h.calls)
	b, _, err := r.NewTask(ctx, NewTaskOptions{Request: sharedReq, Workflow: StepFeat})
	if err != nil || b.State.Isolation() != task.IsolationShared {
		t.Fatalf("B phải shared (A chỉ lập kế hoạch): %v %v", b, err)
	}
	bid := b.State.ID

	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: a})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("A phải blocked: %+v %v", res, err)
	}
	if !strings.Contains(res.Reason, bid) || !strings.Contains(res.Reason, "--worktree") || !strings.Contains(res.Reason, "bint run resume "+a) {
		t.Fatalf("lý do thiếu ID tác vụ kia hoặc lối ra: %q", res.Reason)
	}
	if len(h.calls) != callsAfterPlan {
		t.Fatalf("không bước nào được chạy: %v", h.calls[callsAfterPlan:])
	}
	if got := loadTask(t, h, a); got.State.Status != task.StatusBlocked || got.State.EverInProgress() {
		t.Fatalf("A: %s", got.State.Status)
	}

	// B không bị A (blocked, chưa từng ghi) chặn: chạy được. Còn B chưa kết thúc thì A resume vẫn bị chặn.
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: bid}); err != nil || res.Status == task.StatusBlocked || implementCalls(h) == 0 {
		t.Fatalf("B phải chạy được: %+v %v", *res, err)
	}
	n := implementCalls(h)
	if res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: a, Resume: true}); err != nil || res.Status != task.StatusBlocked || implementCalls(h) != n {
		t.Fatalf("B chưa xong: A resume phải blocked tiếp: %+v %v", *res, err)
	}
	if _, _, err := r.SetStatus(ctx, bid, task.StatusCancelled, TransitionOptions{Reason: "xong", Discard: true}); err != nil {
		t.Fatal(err)
	}
	res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: a, Resume: true})
	if err != nil || res.Status == task.StatusBlocked || implementCalls(h) == 0 {
		t.Fatalf("A resume sau khi B xong phải chạy tiếp: %+v %v calls=%v", res, err, h.calls)
	}
}

// --shared do người chọn (hoặc cấu hình) không bị chặn; phiếu b-req planned không bị phiếu chỉ lập kế hoạch khác chặn.
func TestSharedWriteConflictSkipsExplicitChoice(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.Isolation = config.IsolationAuto
	r := h.runner()
	ctx := context.Background()
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, Request: sharedReq, Isolation: task.IsolationShared})
	if err != nil || res.Status != task.StatusPlanned {
		t.Fatalf("b-req: %+v %v", res, err)
	}
	a := res.TaskID
	if _, _, err := r.NewTask(ctx, NewTaskOptions{Request: sharedReq, Workflow: StepFeat}); err != nil {
		t.Fatal(err)
	}
	res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: a})
	if err != nil || res.Status == task.StatusBlocked || implementCalls(h) == 0 {
		t.Fatalf("--shared của người không bị chặn: %+v %v", res, err)
	}

	// Chỉ có một phiếu b-req planned khác (chỉ lập kế hoạch): không xung đột.
	h2 := newHarness(t)
	h2.cfg.Workflow.Isolation = config.IsolationAuto
	r2 := h2.runner()
	a1 := planOnlyPlanned(t, r2)
	planOnlyPlanned(t, r2)
	if res, err := r2.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: a1}); err != nil || res.Status == task.StatusBlocked {
		t.Fatalf("phiếu chỉ lập kế hoạch khác không chặn: %+v %v", res, err)
	}
}

// Driver session: Next trước khi mở bước implement đầu tiên cũng dừng blocked.
func TestSessionNextBlocksOnSharedWriteConflict(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.Isolation = config.IsolationAuto
	r := h.runner()
	ctx := context.Background()
	a := planOnlyPlanned(t, r)
	b, _, err := r.NewTask(ctx, NewTaskOptions{Request: sharedReq, Workflow: StepFeat})
	if err != nil {
		t.Fatal(err)
	}
	// Chuyển tiếp phiếu A sang driver session với workflow có bước ghi (như Run làm khi chạy tiếp b-req).
	if _, err := h.tasks.Update(a, time.Now(), func(t *task.Task) error {
		t.State.Workflow = StepDo
		t.State.Classification.Driver = task.DriverSession
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	act, err := r.Next(ctx, NextOptions{TaskRef: a})
	if err != nil || act.Action != ActionBlocked || !strings.Contains(act.Reason, b.State.ID) || !strings.Contains(act.Reason, "--worktree") {
		t.Fatalf("Next phải blocked nêu %s: %+v %v", b.State.ID, act, err)
	}
}
