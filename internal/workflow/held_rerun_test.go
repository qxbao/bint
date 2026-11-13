package workflow

import (
	"context"
	"strings"
	"testing"

	"bint/internal/task"
)

// heldPlan chạy b-req mà agent sửa app.txt ở lượt đầu (bước bị giữ → blocked); các lượt sau agent không sửa gì.
func heldPlan(t *testing.T) (*harness, *Runner, string) {
	t.Helper()
	h := newHarness(t)
	n := 0
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			if n++; n == 1 {
				h.write("app.txt", "sửa đồng thời\n")
			}
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusBlocked || loadTask(t, h, res.TaskID).State.HeldStep == nil {
		t.Fatalf("lượt đầu phải blocked kèm HeldStep: %+v %v", res, err)
	}
	if !strings.Contains(res.Reason, "bint run resume "+res.TaskID+" --rerun") {
		t.Fatalf("lý do phải nêu lối --rerun: %q", res.Reason)
	}
	return h, r, res.TaskID
}

// .11: resume --rerun bỏ HeldStep (có ghi chú) và chạy lại bước chỉ đọc; không cờ thì chỉ áp cổng (không chạy lại).
func TestResumeRerunReplaysHeldReadOnlyStep(t *testing.T) {
	h, r, id := heldPlan(t)
	ctx := context.Background()
	// Cờ sai chỗ: lỗi cách dùng, phiếu giữ nguyên.
	if _, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: id, Rerun: true}); err == nil || !strings.Contains(err.Error(), "--rerun") {
		t.Fatalf("--rerun không có resume phải lỗi: %v", err)
	}
	if loadTask(t, h, id).State.HeldStep == nil || countCalls(h, StepReq) != 1 {
		t.Fatal("lỗi cách dùng không được đổi phiếu")
	}
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: id, Resume: true, Rerun: true})
	if err != nil || res.Status != task.StatusPlanned || countCalls(h, StepReq) != 2 {
		t.Fatalf("--rerun phải chạy lại b-req: %+v %v calls=%v", res, err, h.calls)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "bỏ kết quả bước giữ "+StepReq) {
		t.Fatalf("thiếu ghi chú: %v", res.Notes)
	}
	if loadTask(t, h, id).State.HeldStep != nil {
		t.Fatal("HeldStep phải được xoá")
	}
	// Không còn bước giữ: --rerun là lỗi cách dùng.
	if _, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: id, Resume: true, Rerun: true}); err == nil || !strings.Contains(err.Error(), "không có bước") {
		t.Fatalf("không có HeldStep: %v", err)
	}
}

// .11: không cờ thì resume vẫn chỉ áp cổng (hành vi cũ, đã có TestResumeAfterHeldPlanDoesNotRerunPlanner).
func TestResumeWithoutRerunKeepsHeldBehaviour(t *testing.T) {
	h, r, id := heldPlan(t)
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepReq, TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusPlanned || countCalls(h, StepReq) != 1 {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
}

// .11 driver session: Next --resume --rerun cũng bỏ HeldStep.
func TestSessionNextRerunDropsHeldStep(t *testing.T) {
	h, r, id := heldPlan(t)
	ctx := context.Background()
	if _, err := r.Next(ctx, NextOptions{TaskRef: id, Rerun: true}); err == nil || !strings.Contains(err.Error(), "--rerun") {
		t.Fatalf("--rerun không có --resume phải lỗi: %v", err)
	}
	// Phiếu lập bằng driver process nhưng Next vẫn mở được bước: chỉ cần HeldStep biến mất và có hành động agent.
	a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true, Rerun: true})
	if err != nil {
		t.Fatal(err)
	}
	if loadTask(t, h, id).State.HeldStep != nil {
		t.Fatalf("HeldStep phải được xoá: %+v", a)
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "bỏ kết quả bước giữ") {
		t.Fatalf("thiếu ghi chú: %+v", a)
	}
}

// .14: người tự chuyển trạng thái (không qua resume) xoá HeldStep, lịch sử có dấu vết; lượt chạy sau không áp cổng cũ.
func TestManualStatusChangeDropsHeldStep(t *testing.T) {
	h, r, id := heldPlan(t)
	ctx := context.Background()
	tk := loadTask(t, h, id)
	to := tk.State.ResumeFrom
	if to == "" {
		to = task.StatusCreated
	}
	got, _, err := r.SetStatus(ctx, id, to, TransitionOptions{Reason: "đã tự sửa"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State.HeldStep != nil {
		t.Fatal("HeldStep phải bị xoá khi người tự chuyển trạng thái")
	}
	if last := got.State.History[len(got.State.History)-1]; !strings.Contains(last.Reason, "bỏ kết quả bước giữ "+StepReq) {
		t.Fatalf("lịch sử: %+v", last)
	}
	// Lượt chạy sau lập kế hoạch lại (chạy bước, không áp kết quả giữ cũ).
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: id})
	if err != nil || res.Status != task.StatusPlanned || countCalls(h, StepReq) != 2 {
		t.Fatalf("phải chạy lại b-req: %+v %v calls=%v", res, err, h.calls)
	}

	// Chuyển của Orchestrator (resume) không xoá giữa chừng: HeldStep còn cho tới khi takeHeld nhận.
	_, r2, id2 := heldPlan(t)
	if _, _, err := r2.SetStatus(ctx, id2, task.StatusCreated, TransitionOptions{Reason: "x", Orchestrator: true}); err != nil {
		t.Fatal(err)
	}
}
