package workflow

import (
	"context"
	"testing"

	"bint/internal/task"
)

// Cổng hiệu năng chỉ chạy lệnh workflow.perf.command đã được người xác nhận (băm lệnh); lệnh đổi sau đó thì mất xác nhận.
func TestPerfCommandNeedsApproval(t *testing.T) {
	h := newHarness(t)
	if err := bgSetYAML(h, "workflow:\n  perf:\n    command: sh perf.sh\n"); err != nil {
		t.Fatal(err)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	tk := loadTask(t, h, res.TaskID)
	if r.PerfConfirmed(tk) || r.perfEnabled(tk) {
		t.Fatal("lệnh perf chưa được người duyệt thì không được chạy")
	}
	if tk.State.PerfBaseline != nil {
		t.Fatalf("không được đo mốc khi chưa duyệt: %v", *tk.State.PerfBaseline)
	}
	_, a, err := r.Approve(res.TaskID)
	if err != nil || a.PerfCommand != "sh perf.sh" {
		t.Fatalf("approve phải xác nhận perf.command: %+v %v", a, err)
	}
	tk = loadTask(t, h, res.TaskID)
	if !r.PerfConfirmed(tk) || !r.perfEnabled(tk) || tk.State.PerfApproval == nil || tk.State.PerfApproval.SHA256 != task.CommandSHA256("sh perf.sh") {
		t.Fatalf("sau approve phải xác nhận: %+v", tk.State.PerfApproval)
	}
	r.Cfg.Workflow.Perf.Command = "sh evil.sh"
	if r.PerfConfirmed(tk) || r.perfEnabled(tk) {
		t.Fatal("lệnh đổi sau khi duyệt thì không được chạy")
	}
}
