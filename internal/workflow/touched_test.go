package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/task"
	"bint/internal/testutil"
)

func (h *harness) git(args ...string) string { return testutil.Git(h.t, h.root, args...) }

// runFeat chạy một bước b-feat trên tác vụ mới (request) hoặc có sẵn (ref).
func (h *harness) runFeat(request, ref string) *Result {
	h.t.Helper()
	o := RunOptions{Workflow: StepFeat, Request: request}
	if ref != "" {
		o = RunOptions{Workflow: StepFeat, TaskRef: ref, Resume: true}
	}
	res, err := h.runner().Run(context.Background(), o)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func (h *harness) load(id string) *task.Task {
	h.t.Helper()
	tk, err := h.tasks.Load(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return tk
}

// behaveFeat thay hành vi b-feat bằng f, các bước khác như `happy`.
func (h *harness) behaveFeat(f func()) {
	happy := h.behave
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			f()
			return nil
		}
		return happy(ctx, step, e, id)
	}
}

// AC-3: tệp chưa commit có sẵn trước khi lập phiếu (kể cả đường dẫn bảo vệ) không vào phạm vi của tác vụ.
func TestTouchedExcludesPreexistingDirtyFiles(t *testing.T) {
	h := newHarness(t)
	h.write("migrations/001.sql", "-- đang sửa dở từ trước\n")
	h.write("scratch.txt", "nháp\n")

	res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if res.Status != task.StatusInReview {
		t.Fatalf("tệp bẩn có sẵn không được chặn tác vụ: %+v", res)
	}
	tk := h.load(res.TaskID)
	if !tk.State.TracksTouched() || !slices.Equal(tk.State.Touched, []string{"app.txt", "done.txt"}) {
		t.Fatalf("Touched = %v, BaseTree = %q", tk.State.Touched, tk.State.BaseTree)
	}
	if len(tk.State.PendingPaths) != 0 {
		t.Fatalf("PendingPaths = %v", tk.State.PendingPaths)
	}
	// Cây gốc được giữ sống bằng ref, đúng mã cây đã ghi.
	if got := h.git("rev-parse", "refs/bint/tasks/"+res.TaskID+"/base^{tree}"); got != tk.State.BaseTree {
		t.Fatalf("ref cây gốc = %s, BaseTree = %s", got, tk.State.BaseTree)
	}
	// UnapprovedChanges cũng dùng Touched.
	r := h.runner()
	if need, err := r.UnapprovedChanges(context.Background(), tk); err != nil || len(need) != 0 {
		t.Fatalf("UnapprovedChanges = %v %v", need, err)
	}
}

// Phiếu cũ (không BaseTree) giữ hành vi cũ: toàn bộ thay đổi so với base bị tính, kể cả tệp bẩn có sẵn.
func TestLegacyTaskKeepsChangedFilesBehaviour(t *testing.T) {
	h := newHarness(t)
	h.write("migrations/001.sql", "-- đang sửa dở từ trước\n")
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// Tác vụ đã có b-feat từ trước khi Bint theo dõi tệp riêng: đã in_progress, không có BaseTree.
	if _, err := h.tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
		return t.State.Apply(task.StatusInProgress, "người", "làm tay", "2026-01-01T00:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}
	res := h.runFeat("", tk.State.ID)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "migrations/001.sql") {
		t.Fatalf("phiếu cũ phải tính cả migrations/001.sql: %+v", res)
	}
	got := h.load(tk.State.ID)
	if got.State.TracksTouched() || got.State.Touched != nil {
		t.Fatalf("phiếu cũ không được tự bật theo dõi giữa chừng: %+v", got.State)
	}
	need, err := h.runner().UnapprovedChanges(context.Background(), got)
	if err != nil || !slices.Contains(need, "migrations/001.sql") {
		t.Fatalf("UnapprovedChanges (phiếu cũ) = %v %v", need, err)
	}
}

// Tệp của tác vụ khác không vào phạm vi / UnapprovedChanges của tác vụ này; thông báo done liệt kê tệp ngoài tác vụ.
func TestOtherTaskFilesNotCountedAndDoneMessage(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if a.Status != task.StatusInReview {
		t.Fatalf("A: %+v", a)
	}
	// B sửa một tệp được bảo vệ (migrations/): chặn B, không liên quan tới A.
	h.behaveFeat(func() { h.write("migrations/002.sql", "-- của B\n") })
	b := h.runFeat("Việc khác trên migrations", "")
	if b.Status != task.StatusBlocked || !strings.Contains(b.Reason, "migrations/002.sql") {
		t.Fatalf("B phải bị chặn vì đường dẫn bảo vệ của chính nó: %+v", b)
	}
	tkA := h.load(a.TaskID)
	r := h.runner()
	if need, err := r.UnapprovedChanges(ctx, tkA); err != nil || len(need) != 0 {
		t.Fatalf("tệp của B bị tính cho A: %v %v", need, err)
	}
	// A: mã đã đổi từ lần verify cuối nhưng chỉ do B → thông báo nêu tệp ngoài tác vụ.
	_, blockers, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "thử"})
	if err == nil {
		t.Fatal("done phải bị chặn vì mã đã đổi sau lần kiểm chứng")
	}
	msg := strings.Join(blockers, "\n")
	if !strings.Contains(msg, "thay đổi không thuộc tác vụ") || !strings.Contains(msg, "migrations/002.sql") ||
		!strings.Contains(msg, "bint task verify "+a.TaskID) || strings.Contains(msg, "app.txt") {
		t.Fatalf("thông báo done: %q", msg)
	}
	if strings.Contains(msg, "đường dẫn bảo vệ") {
		t.Fatalf("done của A không được bị chặn vì tệp bảo vệ của B: %q", msg)
	}
	// Chạy lại kiểm chứng rồi done thì qua.
	if _, vr, err := r.Verify(ctx, a.TaskID); err != nil || !vr.Verification.Passed || vr.Verification.Tree == "" {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	if _, _, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "xong"}); err != nil {
		t.Fatalf("done sau khi kiểm chứng lại: %v", err)
	}
}

// Chồng lấn: hai tác vụ chưa kết thúc cùng sửa một tệp → blocked + PendingPaths nêu tác vụ kia;
// người approve rồi resume thì đi tiếp tới in_review.
func TestOverlapBlocksThenApproveResumes(t *testing.T) {
	h := newHarness(t)
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if a.Status != task.StatusInReview {
		t.Fatalf("A: %+v", a)
	}
	h.behaveFeat(func() { h.write("app.txt", "v3 của B\n") })
	b := h.runFeat("Đổi app.txt sang v3", "")
	if b.Status != task.StatusBlocked || !strings.Contains(b.Reason, "tác vụ "+a.TaskID+" cũng sửa: app.txt") {
		t.Fatalf("B phải bị chặn vì chồng lấn: %+v", b)
	}
	tkB := h.load(b.TaskID)
	if !slices.Equal(tkB.State.PendingPaths, []string{"app.txt"}) {
		t.Fatalf("PendingPaths = %v", tkB.State.PendingPaths)
	}
	if _, _, err := h.runner().Approve(b.TaskID); err != nil {
		t.Fatal(err)
	}
	res := h.runFeat("", b.TaskID)
	if res.Status != task.StatusInReview {
		t.Fatalf("sau approve + resume: %+v", res)
	}
	// Tác vụ A vẫn không bị tính tệp nào của B ngoài phần chung đã có.
	if got := h.load(a.TaskID).State.Touched; !slices.Equal(got, []string{"app.txt", "done.txt"}) {
		t.Fatalf("Touched của A = %v", got)
	}
}

// Tác vụ kia đã kết thúc (done/cancelled) thì không còn tính chồng lấn.
func TestOverlapIgnoresFinishedTasks(t *testing.T) {
	h := newHarness(t)
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if _, err := h.tasks.Update(a.TaskID, time.Now(), func(t *task.Task) error {
		return t.State.Apply(task.StatusCancelled, "người", "huỷ", "2026-01-01T00:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}
	h.behaveFeat(func() { h.write("app.txt", "v3 của B\n") })
	if b := h.runFeat("Đổi app.txt sang v3", ""); b.Status != task.StatusInReview {
		t.Fatalf("B: %+v", b)
	}
}

// Xoá hoặc đổi tên tệp được bảo vệ vẫn bị bắt (Touched dùng --no-renames).
func TestProtectedDeleteAndRenameStillCaught(t *testing.T) {
	cases := map[string]func(h *harness){
		"xoá": func(h *harness) {
			if err := os.Remove(filepath.Join(h.root, "migrations", "001.sql")); err != nil {
				h.t.Fatal(err)
			}
		},
		"đổi tên": func(h *harness) {
			if err := os.Rename(filepath.Join(h.root, "migrations", "001.sql"), filepath.Join(h.root, "docs-001.sql")); err != nil {
				h.t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.behaveFeat(func() { mutate(h) })
			res := h.runFeat("Dọn dẹp tệp SQL cũ", "")
			if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "migrations/001.sql") {
				t.Fatalf("%s tệp bảo vệ phải bị chặn: %+v", name, res)
			}
		})
	}
}

// b-feat lỗi hoặc bị huỷ vẫn ghi lại tệp đã đổi (chụp sau không bị huỷ theo).
func TestTouchedRecordedWhenAgentFails(t *testing.T) {
	h := newHarness(t)
	happy := h.behave
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			h.write("migrations/003.sql", "-- viết dở rồi lỗi\n")
			return os.ErrInvalid
		}
		return happy(ctx, step, e, id)
	}
	res := h.runFeat("Thêm bảng mới trong migrations", "")
	if res.Status != task.StatusFailed {
		t.Fatalf("%+v", res)
	}
	if got := h.load(res.TaskID).State.Touched; !slices.Equal(got, []string{"migrations/003.sql"}) {
		t.Fatalf("Touched sau b-feat lỗi = %v", got)
	}
}

// Nhiều lượt b-feat: BaseTree giữ nguyên, Touched là hợp (sắp xếp, bỏ trùng).
func TestTouchedAccumulatesAcrossFeatRuns(t *testing.T) {
	h := newHarness(t)
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	base := h.load(a.TaskID).State.BaseTree
	h.behaveFeat(func() { h.write("c.txt", "thêm\n"); h.write("app.txt", "v4\n") })
	if res := h.runFeat("", a.TaskID); res.Status != task.StatusInReview {
		t.Fatalf("%+v", res)
	}
	st := h.load(a.TaskID).State
	if st.BaseTree != base || !slices.Equal(st.Touched, []string{"app.txt", "c.txt", "done.txt"}) {
		t.Fatalf("BaseTree=%q (cũ %q) Touched=%v", st.BaseTree, base, st.Touched)
	}
}

// Sửa tay ngoài b-feat của Runner (phát hiện review .1/.5): không bỏ theo dõi, nhưng tệp bảo vệ sửa ngoài b-feat
// vẫn bị bắt ở UnapprovedChanges, cổng done và bint task diff — ở mọi trạng thái tác vụ.
func TestManualEditOutsideFeatIsCaught(t *testing.T) {
	for _, tc := range []struct {
		name  string
		moves []string // trạng thái chuyển tay trước khi sửa ("" = giữ in_review)
	}{
		{"in_review", nil}, // .5: không đổi trạng thái, sửa bằng phiên agent khác
		{"in_progress", []string{task.StatusInProgress}}, // .1: b-feat làm tay (bint task status in_progress)
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
			if a.Status != task.StatusInReview || !h.load(a.TaskID).State.TracksTouched() {
				t.Fatalf("%+v", a)
			}
			r := h.runner()
			for _, to := range tc.moves {
				if _, _, err := r.SetStatus(ctx, a.TaskID, to, TransitionOptions{Reason: "sửa tay"}); err != nil {
					t.Fatal(err)
				}
			}
			h.write("migrations/001.sql", "-- sửa tay ngoài Runner\n")
			tk := h.load(a.TaskID)
			if !tk.State.TracksTouched() {
				t.Fatal("không được bỏ theo dõi: mất BaseTree làm mất lọc tệp của tác vụ khác")
			}
			if need, err := r.UnapprovedChanges(ctx, tk); err != nil || !slices.Contains(need, "migrations/001.sql") {
				t.Fatalf("UnapprovedChanges = %v %v", need, err)
			}
			files, err := ScopeFiles(ctx, h.root, h.tasks, tk)
			if err != nil || !slices.Contains(files, "migrations/001.sql") || !slices.Contains(files, "app.txt") {
				t.Fatalf("ScopeFiles = %v %v", files, err)
			}
			// Kiểm chứng lại (fingerprint mới) rồi done vẫn phải bị chặn vì tệp bảo vệ chưa duyệt.
			if _, vr, err := r.Verify(ctx, a.TaskID); err != nil || !vr.Verification.Passed {
				t.Fatalf("verify: %+v %v", vr, err)
			}
			_, blockers, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "thử"})
			if err == nil || !strings.Contains(strings.Join(blockers, "\n"), "migrations/001.sql") {
				t.Fatalf("done phải bị chặn vì migrations/001.sql: %v %v", blockers, err)
			}
			// Người duyệt xong thì qua.
			if _, _, err := r.Approve(a.TaskID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "xong"}); err != nil {
				t.Fatalf("done sau duyệt: %v", err)
			}
			// Kết thúc: ref cây gốc bị xoá (.4).
			if out, err := gitrepoRun(h, "rev-parse", "--verify", "-q", "refs/bint/tasks/"+a.TaskID+"/base"); err == nil {
				t.Fatalf("ref cây gốc phải bị xoá khi done, còn %q", out)
			}
		})
	}
}

// Tệp sửa ngoài b-feat của tác vụ A không bị quy cho tác vụ B nếu đã thuộc Touched của B (và ngược lại tệp tay vẫn bị quy cho cả hai).
func TestOutsideFeatExcludesOtherTaskTouched(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	h.behaveFeat(func() { h.write("docs/b.md", "của B\n") })
	b := h.runFeat("Việc khác trên docs", "")
	if b.Status != task.StatusInReview {
		t.Fatalf("%+v", b)
	}
	files, err := ScopeFiles(ctx, h.root, h.tasks, h.load(a.TaskID))
	if err != nil || slices.Contains(files, "docs/b.md") {
		t.Fatalf("tệp của B bị tính cho A: %v %v", files, err)
	}
}

// Không giữ được cây gốc (ref không tạo được) thì quay về chế độ cũ kèm ghi chú, không làm hỏng tác vụ.
func TestTouchedFallsBackWhenBaseNotPinned(t *testing.T) {
	h := newHarness(t)
	// refs/bint là tệp thường nên không tạo được refs/bint/tasks/<id>/base.
	if err := os.WriteFile(filepath.Join(h.root, ".git", "refs", "bint"), []byte("chắn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if res.Status != task.StatusInReview {
		t.Fatalf("%+v", res)
	}
	if st := h.load(res.TaskID).State; st.TracksTouched() || st.Touched != nil {
		t.Fatalf("phải quay về chế độ cũ: %+v", st)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "không theo dõi riêng tệp của tác vụ") {
		t.Fatalf("thiếu ghi chú: %v", res.Notes)
	}
}

// AC-5: đề bài b-review dùng `bint task diff` (tác vụ có theo dõi tệp riêng), phiếu cũ cũng trỏ về lệnh này kèm ghi chú.
func TestReviewPromptUsesTaskDiff(t *testing.T) {
	h := newHarness(t)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	p := h.prompts[StepReview]
	if !strings.Contains(p, "bint task diff "+res.TaskID) || !strings.Contains(p, "--stat") || strings.Contains(p, "`git diff "+h.load(res.TaskID).State.Base+"`") {
		t.Fatalf("đề bài b-review:\n%s", p)
	}
	// Phiếu cũ (không BaseTree): cũng trỏ về lệnh này, kèm ghi chú so trên toàn cây.
	h2 := newHarness(t)
	r := h2.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Workflow: StepReview, Request: "Review thay đổi cũ"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepReview, TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	if p := h2.prompts[StepReview]; !strings.Contains(p, "bint task diff "+tk.State.ID) || !strings.Contains(p, "toàn cây") {
		t.Fatalf("đề bài b-review (phiếu cũ):\n%s", p)
	}
}

func gitrepoRun(h *harness, args ...string) (string, error) {
	return gitrepo.Run(context.Background(), h.root, args...)
}
