package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

func (h *harness) read(name string) string {
	b, err := os.ReadFile(filepath.Join(h.root, filepath.FromSlash(name)))
	if err != nil {
		return "<không có>"
	}
	return string(b)
}

// stalledRun chạy b-do tới khi review không hội tụ (blocked) và trả kết quả.
func (h *harness) stalledRun(t *testing.T) (*Runner, *Result) {
	t.Helper()
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(3, 3)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("%+v %v", res, err)
	}
	return r, res
}

// D1: tác vụ dừng thì kết quả có điểm verify xanh cuối (đã ghim bằng ref) và các phát hiện còn mở.
func TestGreenReportedOnStop(t *testing.T) {
	h := newHarness(t)
	_, res := h.stalledRun(t)
	g := res.LastGreen
	if g == nil || !g.Pinned || g.Command != "test -f done.txt" || len(g.ChangedSince) != 0 || g.Note != "" {
		t.Fatalf("last_green: %+v", g)
	}
	if got := strings.TrimSpace(h.git("rev-parse", greenRef(res.TaskID)+"^{tree}")); got != g.Tree {
		t.Fatalf("ref green trỏ cây %s, muốn %s", got, g.Tree)
	}
	if len(res.OpenFindings) != 3 || res.OpenFindings[0].Round != 2 || res.OpenFindings[0].Severity != "high" {
		t.Fatalf("phát hiện còn mở của vòng review mới nhất: %+v", res.OpenFindings)
	}
	// Phát hiện đã đặt kết cục fixed thì không còn mở.
	if _, err := h.runner().Env.SetFindingOutcome(context.Background(), []string{res.OpenFindings[0].ID}, "rejected", "", "không phải lỗi"); err != nil {
		t.Fatal(err)
	}
	if _, open := h.runner().Env.GreenReport(context.Background(), loadTask(t, h, res.TaskID)); len(open) != 2 {
		t.Fatalf("còn %d phát hiện mở, muốn 2", len(open))
	}
}

// Tác vụ không dừng (done) thì không gắn điểm xanh vào kết quả.
func TestGreenNotAttachedWhenDone(t *testing.T) {
	h := newHarness(t)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone || res.LastGreen != nil || res.OpenFindings != nil {
		t.Fatalf("%+v %v", res, err)
	}
}

// RestoreGreen: xem trước không ghi; --apply chỉ đụng tệp của tác vụ, giữ tệp lạ, ghim đường lui; kết thúc tác vụ thì xoá ref.
func TestRestoreGreenPreviewApplyAndCleanup(t *testing.T) {
	h := newHarness(t)
	r, res := h.stalledRun(t)
	id := res.TaskID
	ctx := context.Background()
	// Mô phỏng cây dở: b-feat sửa dở app.txt, tạo tệp phụ; người dùng có tệp lạ không thuộc tác vụ.
	h.write("app.txt", "dở dang\n")
	h.write("done.txt", "ok\n") // không đổi so với điểm xanh: không được liệt kê
	h.write("user-notes.txt", "của người dùng\n")
	g, _ := r.Env.GreenReport(ctx, loadTask(t, h, id))
	if g == nil || !slices.Equal(g.ChangedSince, []string{"app.txt", "user-notes.txt"}) {
		t.Fatalf("đã đổi kể từ điểm xanh: %+v", g)
	}
	pre, err := r.Env.RestoreGreen(ctx, id, false)
	if err != nil || pre.Applied || !slices.Equal(pre.Restore, []string{"app.txt"}) || !slices.Equal(pre.Skipped, []string{"user-notes.txt"}) {
		t.Fatalf("xem trước: %+v %v", pre, err)
	}
	if h.read("app.txt") != "dở dang\n" {
		t.Fatal("xem trước không được ghi gì")
	}
	stagedBefore := h.git("diff", "--cached", "--name-only")
	done, err := r.Env.RestoreGreen(ctx, id, true)
	if err != nil || !done.Applied || done.Backup == "" {
		t.Fatalf("apply: %+v %v", done, err)
	}
	if h.read("app.txt") != "v2\n" || h.read("user-notes.txt") != "của người dùng\n" {
		t.Fatalf("app.txt=%q user-notes=%q", h.read("app.txt"), h.read("user-notes.txt"))
	}
	if got := h.git("show", done.Backup+":app.txt"); got != "dở dang" { // testutil.Git cắt khoảng trắng cuối
		t.Fatalf("đường lui phải giữ bản dở dang: %q", got)
	}
	if h.git("diff", "--cached", "--name-only") != stagedBefore {
		t.Fatal("không được đụng chỉ mục")
	}
	// Tệp thuộc tác vụ (Touched) mà tạo sau điểm xanh thì bị xoá; tệp lạ không thuộc tác vụ thì giữ.
	if _, err := h.tasks.Update(id, r.now(), func(tk *task.Task) error {
		tk.State.Touched = append(tk.State.Touched, "extra.txt")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.write("extra.txt", "x\n")
	again, err := r.Env.RestoreGreen(ctx, id, true)
	if err != nil || !slices.Equal(again.Restore, []string{"extra.txt"}) || !slices.Contains(again.Skipped, "user-notes.txt") ||
		h.read("extra.txt") != "<không có>" || h.read("user-notes.txt") != "của người dùng\n" {
		t.Fatalf("tệp tạo sau điểm xanh phải bị xoá, tệp lạ giữ: %+v %v", again, err)
	}
	// Kết thúc tác vụ: ref green và before-restore bị xoá cùng base.
	if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{greenRef(id), beforeRestoreRef(id)} {
		if _, err := gitrepoRun(h, "rev-parse", "--verify", "-q", ref); err == nil {
			t.Errorf("ref %s còn sau khi tác vụ kết thúc", ref)
		}
	}
}

func TestRestoreGreenRefusals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Tác vụ đã done: chỉ tác vụ dừng mới quay về được.
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := h.runner().Env.RestoreGreen(ctx, res.TaskID, true); err == nil || !strings.Contains(err.Error(), "chỉ quay về điểm xanh khi tác vụ dừng") {
		t.Fatalf("tác vụ done phải bị từ chối: %v", err)
	}
	// Tác vụ dừng mà chưa từng có kiểm chứng xanh: ErrNoGreen.
	h2 := newHarness(t)
	h2.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			h2.write("app.txt", "v2\n") // không tạo done.txt: kiểm chứng đỏ
			return nil
		}
		return h2.happy(ctx, step, e, id)
	}
	r2 := h2.runner()
	res2, err := r2.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || !task.Paused(res2.Status) {
		t.Fatalf("%+v %v", res2, err)
	}
	if res2.LastGreen != nil {
		t.Fatalf("chưa xanh lần nào thì không có last_green: %+v", res2.LastGreen)
	}
	if _, err := r2.Env.RestoreGreen(ctx, res2.TaskID, false); err != ErrNoGreen {
		t.Fatalf("muốn ErrNoGreen: %v", err)
	}
}

// Driver session: hành động blocked cũng mang điểm xanh.
func TestSessionStopCarriesGreen(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(2, 2)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	_, last := h.drive(r, tk.State.ID, false)
	if last.Action != ActionBlocked || last.LastGreen == nil || !last.LastGreen.Pinned || len(last.OpenFindings) != 2 {
		t.Fatalf("%+v", last)
	}
}
