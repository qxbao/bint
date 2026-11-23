package workflow

import (
	"context"
	"testing"
	"time"

	"bint/internal/task"
)

// A1: blockFinalize chỉ nhớ dấu vân tay (v == nil) khi lý do chặn duy nhất là lệnh kiểm chứng chưa xác nhận.
func TestBlockFinalizeFPOnlyWhenUnconfirmedVerifyIsTheOnlyReason(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.finishFeat()
	h.cfg.Eval.TestCommand = nil // lệnh chưa được xác nhận
	r := h.runner()
	if _, err := r.Tasks.Update(id, time.Now(), func(tk *task.Task) error { tk.State.Verifications = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	block := func() string {
		rs := &runState{t: h.load(id), res: &Result{Steps: []StepResult{}}, ran: map[string]int{}}
		r.blockFinalize(ctx, rs, "thử")
		return h.load(id).State.FinalizeFP
	}
	if fp := block(); fp == "" {
		t.Fatal("chỉ thiếu xác nhận lệnh: phải nhớ dấu vân tay")
	}
	setCriteriaRaw(t, r, id, "app.txt ghi v2")
	if _, err := r.CheckCriterion(id, 1, false, "chưa xong"); err != nil {
		t.Fatal(err)
	}
	if fp := block(); fp != "" {
		t.Fatalf("còn tiêu chí chưa đạt: phải xoá dấu vân tay, có %q", fp)
	}
}

// A4: số đo đỏ của lát bị bỏ không vào vòng sửa.
func TestDiscardVoidsRedChecks(t *testing.T) {
	tk := &task.Task{}
	tk.State.Verifications = []task.Verification{{Passed: false, RunID: "r1"}}
	tk.State.GoalChecks = []task.Verification{{Passed: false, RunID: "r1"}}
	if redVerification(tk) == nil {
		t.Fatal("chưa bỏ lát: phải thấy đỏ")
	}
	voidRedChecks(&tk.State)
	if v := redVerification(tk); v != nil {
		t.Fatalf("sau discard phải bỏ qua: %+v", v)
	}
	tk.State.Verifications = append(tk.State.Verifications, task.Verification{Passed: false, RunID: "r2"})
	if redVerification(tk) == nil {
		t.Fatal("lần đỏ mới sau discard phải tính")
	}
}
