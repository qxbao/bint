package workflow

import (
	"context"
	"testing"
)

// --slice discard vẫn được áp khi chia lát đã tắt giữa chừng: cây về điểm xanh rồi mới thôi chia lát.
func TestSliceDiscardAppliedWhenSlicingTurnedOff(t *testing.T) {
	f := slBox(t, slOpts{timeoutMin: 45, l1NoProgress: true, hold: true})
	f.need(t)
	if tk := loadTask(t, f.h, f.id); tk.State.HeldSlice() == nil {
		t.Fatalf("lát 1 phải đang giữ chờ người")
	}
	if got := f.h.read("c1.txt"); got != "sai\n" {
		t.Fatalf("tiền điều kiện: c1.txt = %q", got)
	}
	f.h.cfg.Workflow.SliceTimeoutMinutes = 0 // tắt chia lát
	if _, err := f.h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: f.id, Resume: true, Slice: SliceDiscard}); err != nil {
		t.Fatal(err)
	}
	if got := f.h.read("c1.txt"); got == "sai\n" {
		t.Errorf("--slice discard bị bỏ qua khi chia lát tắt: c1.txt vẫn %q", got)
	}
	items, _ := stateSlices(loadTask(t, f.h, f.id))
	if len(items) == 0 || slStatus(items[0]) != "discarded" {
		t.Errorf("L1 phải discarded: %s", slDump(items))
	}
}

// keep một lát chưa đo (quá trần) giữ số đo của điểm xanh trước, không ghi Measured=false.
func TestSliceKeepUnmeasuredKeepsPreviousGreenMeasure(t *testing.T) {
	d := slBox(t, slOpts{timeoutMin: 15, l2TimedOut: true, hold: true})
	d.need(t)
	before := loadTask(t, d.h, d.id)
	if before.State.HeldSlice() == nil || before.State.SliceGreen == nil || !before.State.SliceGreen.Measured {
		t.Fatalf("tiền điều kiện: lát 2 giữ chờ người, điểm xanh đã đo: %+v", before.State.SliceGreen)
	}
	prev := *before.State.SliceGreen
	if _, err := d.h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: d.id, Resume: true, Slice: SliceKeep}); err != nil {
		t.Fatal(err)
	}
	g := loadTask(t, d.h, d.id).State.SliceGreen
	if g == nil || !g.Measured || len(g.Red) != len(prev.Red) {
		t.Errorf("keep lát chưa đo phải giữ số đo trước (Measured=%v Red=%v), được %+v", prev.Measured, prev.Red, g)
	}
}
