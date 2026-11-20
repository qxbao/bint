package workflow

import (
	"context"
	"testing"

	"bint/internal/config"
)

// workflow.verify.done = graph (mục 1.7): lần kiểm chứng chọn theo đồ thị phụ thuộc đã xanh trên đúng cây được nhận làm bằng chứng done; mặc định
// (full) thì không; cây đổi sau đó thì không.
func TestGraphDoneAccepted(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n"); h.write("done.txt", "ok\n") })
	id := h.runFeat("Đổi A thành 2", "").TaskID
	tk := h.load(id)
	v := tk.State.LastVerification()
	if v == nil || !v.Passed || v.Full() || v.Selection == nil || v.Selection.Source == "" {
		t.Fatalf("cần lần kiểm chứng affected xanh có nguồn đồ thị: %+v", v)
	}
	r := h.runner()
	ctx := context.Background()
	if r.graphDoneAccepted(ctx, tk) {
		t.Error("mặc định (full) mà vẫn nhận lần kiểm chứng theo đồ thị")
	}
	r.Cfg.Workflow.Verify.Done = config.VerifyDoneGraph
	if !r.graphDoneAccepted(ctx, tk) {
		t.Fatal("done = graph mà không nhận lần kiểm chứng theo đồ thị xanh trên đúng cây")
	}
	nt, err := r.adoptGraphAsFull(tk)
	if err != nil {
		t.Fatal(err)
	}
	if last := nt.State.LastVerification(); !last.Full() || !last.Passed || last.Worktree != v.Worktree {
		t.Errorf("bản dẫn xuất phải đầy đủ, xanh, cùng cây: %+v", last)
	}
	h.write("a/a.go", "package a\n\nfunc A() int { return 3 }\n")
	if r.graphDoneAccepted(ctx, tk) {
		t.Error("cây đã đổi sau lần kiểm chứng mà vẫn nhận")
	}
}
