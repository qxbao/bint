package workflow

import (
	"context"
	"fmt"
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

// Tệp KHÔNG bảo vệ của tác vụ chưa kết thúc khác không vào phạm vi của tác vụ này; tệp bảo vệ của nó thì có
// (phát hiện .7: không miễn đường dẫn bảo vệ/deny theo tác vụ khác). Thông báo done liệt kê tệp ngoài tác vụ.
func TestOtherTaskFilesNotCountedAndDoneMessage(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if a.Status != task.StatusInReview {
		t.Fatalf("A: %+v", a)
	}
	// B (chưa kết thúc) sửa một tệp bảo vệ và một tệp thường; chặn B vì đường dẫn bảo vệ của chính nó.
	h.behaveFeat(func() { h.write("migrations/002.sql", "-- của B\n"); h.write("docs/b.md", "của B\n") })
	b := h.runFeat("Việc khác trên migrations", "")
	if b.Status != task.StatusBlocked || !strings.Contains(b.Reason, "migrations/002.sql") {
		t.Fatalf("B phải bị chặn vì đường dẫn bảo vệ của chính nó: %+v", b)
	}
	tkA := h.load(a.TaskID)
	r := h.runner()
	need, err := r.UnapprovedChanges(ctx, tkA)
	if err != nil || !slices.Equal(need, []string{"migrations/002.sql"}) {
		t.Fatalf("UnapprovedChanges(A) = %v %v: tệp bảo vệ của B phải bị tính, docs/b.md thì không", need, err)
	}
	_, blockers, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "thử"})
	if err == nil {
		t.Fatal("done phải bị chặn")
	}
	msg := strings.Join(blockers, "\n")
	if !strings.Contains(msg, "thay đổi không thuộc tác vụ") || !strings.Contains(msg, "migrations/002.sql") ||
		!strings.Contains(msg, "docs/b.md") || !strings.Contains(msg, "bint task verify "+a.TaskID) || strings.Contains(msg, "app.txt") {
		t.Fatalf("thông báo done: %q", msg)
	}
	// Người duyệt đường dẫn bảo vệ rồi chạy lại kiểm chứng thì done qua.
	if _, _, err := r.Approve(a.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, vr, err := r.Verify(ctx, a.TaskID); err != nil || !vr.Verification.Passed || vr.Verification.Tree == "" {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	if _, _, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "xong"}); err != nil {
		t.Fatalf("done sau khi duyệt và kiểm chứng lại: %v", err)
	}
}

// Phát hiện .7 (+ .3 của tác vụ 01M774RD…): tác vụ B ĐÃ kết thúc (cancelled/done) không còn che tệp của nó: sửa tay
// tệp mà B từng sửa vẫn bị quy cho A (UnapprovedChanges, cổng done). B chạy TRƯỚC A nên BaseTree(A) đã chứa thay đổi
// của B: trước bước sửa tay hai tệp của B không khác BaseTree(A) nên không nằm trong phạm vi của A; chỉ bước sửa tay
// mới đưa chúng vào. Nhờ vậy test đỏ khi bỏ bước sửa tay (không có gì để quy) hoặc khi bỏ lọc task.Terminal ở
// outsideFeat (docs/b.md, không bảo vệ, được Touched của B che dù B đã kết thúc).
func TestOutsideFeatTerminalTaskDoesNotShield(t *testing.T) {
	for _, final := range []string{task.StatusCancelled, task.StatusDone} {
		t.Run(final, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			// B chạy trước: sửa migrations/001.sql (bảo vệ) và docs/b.md; người duyệt để B qua checkScope.
			happy := h.behave
			h.behaveFeat(func() {
				h.write("migrations/001.sql", "-- của B\n")
				h.write("docs/b.md", "của B\n")
				h.write("done.txt", "ok\n") // để verify.command ("test -f done.txt") của B qua
			})
			rb := h.runner()
			rb.Confirm = func(Question) bool { return true }
			res, err := rb.Run(ctx, RunOptions{Workflow: StepFeat, Request: "Sửa migrations"})
			if err != nil || res.Status != task.StatusInReview {
				t.Fatalf("B: %+v %v", res, err)
			}
			h.behave = happy
			// A chạy sau: BaseTree(A) đã chứa thay đổi của B.
			a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
			if a.Status != task.StatusInReview {
				t.Fatalf("A: %+v", a)
			}
			if final == task.StatusDone {
				if _, vr, err := rb.Verify(ctx, res.TaskID); err != nil || !vr.Verification.Passed {
					t.Fatalf("verify B: %+v %v", vr, err)
				}
			}
			if _, _, err := rb.SetStatus(ctx, res.TaskID, final, TransitionOptions{Reason: "kết thúc B"}); err != nil {
				t.Fatalf("B → %s: %v", final, err)
			}
			r := h.runner()
			tkA := h.load(a.TaskID)
			// TRƯỚC sửa tay: hai tệp của B không có trong phạm vi của A.
			files, err := ScopeFiles(ctx, h.root, h.tasks, tkA, h.cfg.Limits.ProtectedPaths)
			if err != nil || slices.Contains(files, "migrations/001.sql") || slices.Contains(files, "docs/b.md") {
				t.Fatalf("trước sửa tay ScopeFiles(A) = %v %v: không được có tệp của B", files, err)
			}
			if need, err := r.UnapprovedChanges(ctx, tkA); err != nil || slices.Contains(need, "migrations/001.sql") {
				t.Fatalf("trước sửa tay UnapprovedChanges(A) = %v %v", need, err)
			}
			// Sửa tay cả hai tệp: chúng thuộc Touched của B nhưng B đã kết thúc nên không che.
			h.write("migrations/001.sql", "-- sửa tay\n")
			h.write("docs/b.md", "sửa tay\n")
			files, err = ScopeFiles(ctx, h.root, h.tasks, tkA, h.cfg.Limits.ProtectedPaths)
			if err != nil || !slices.Contains(files, "migrations/001.sql") || !slices.Contains(files, "docs/b.md") {
				t.Fatalf("sau sửa tay ScopeFiles(A) = %v %v: tệp của tác vụ đã kết thúc không được che", files, err)
			}
			if need, err := r.UnapprovedChanges(ctx, tkA); err != nil || !slices.Contains(need, "migrations/001.sql") {
				t.Fatalf("sau sửa tay UnapprovedChanges(A) = %v %v", need, err)
			}
			if _, vr, err := r.Verify(ctx, a.TaskID); err != nil || !vr.Verification.Passed {
				t.Fatalf("verify A: %+v %v", vr, err)
			}
			_, blockers, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "thử"})
			if err == nil || !strings.Contains(strings.Join(blockers, "\n"), "migrations/001.sql") {
				t.Fatalf("done của A phải bị chặn vì migrations/001.sql: %v %v", blockers, err)
			}
		})
	}
}

// Giới hạn đã biết (mục 12.5): A chạy TRƯỚC B nên BaseTree(A) không chứa thay đổi của B; khi B kết thúc thì tệp của B
// còn khác BaseTree(A) và bị quy cho A dù không ai sửa tay thêm. Test này giữ hành vi đó (bảo thủ: chỉ hỏi duyệt).
func TestOutsideFeatTerminalTaskOlderBaseTreeKnownLimit(t *testing.T) {
	for _, final := range []string{task.StatusCancelled, task.StatusDone} {
		t.Run(final, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
			if a.Status != task.StatusInReview {
				t.Fatalf("A: %+v", a)
			}
			// B sửa migrations/001.sql, người duyệt để B qua checkScope.
			h.behaveFeat(func() { h.write("migrations/001.sql", "-- của B\n"); h.write("docs/b.md", "của B\n") })
			rb := h.runner()
			rb.Confirm = func(Question) bool { return true }
			res, err := rb.Run(ctx, RunOptions{Workflow: StepFeat, Request: "Sửa migrations"})
			if err != nil || res.Status != task.StatusInReview {
				t.Fatalf("B: %+v %v", res, err)
			}
			if final == task.StatusDone {
				if _, vr, err := rb.Verify(ctx, res.TaskID); err != nil || !vr.Verification.Passed {
					t.Fatalf("verify B: %+v %v", vr, err)
				}
			}
			if _, _, err := rb.SetStatus(ctx, res.TaskID, final, TransitionOptions{Reason: "kết thúc B"}); err != nil {
				t.Fatalf("B → %s: %v", final, err)
			}
			// Sửa tay thêm migrations/001.sql: tệp thuộc Touched của B nhưng B đã kết thúc.
			h.write("migrations/001.sql", "-- sửa tay\n")
			r := h.runner()
			tkA := h.load(a.TaskID)
			if need, err := r.UnapprovedChanges(ctx, tkA); err != nil || !slices.Contains(need, "migrations/001.sql") {
				t.Fatalf("UnapprovedChanges(A) = %v %v", need, err)
			}
			// Tệp không bảo vệ của B (đã kết thúc) cũng không được miễn: nó còn khác BaseTree của A (giới hạn đã biết).
			files, err := ScopeFiles(ctx, h.root, h.tasks, tkA, h.cfg.Limits.ProtectedPaths)
			if err != nil || !slices.Contains(files, "docs/b.md") {
				t.Fatalf("ScopeFiles(A) = %v %v: tệp của tác vụ đã kết thúc không được che", files, err)
			}
			if _, vr, err := r.Verify(ctx, a.TaskID); err != nil || !vr.Verification.Passed {
				t.Fatalf("verify A: %+v %v", vr, err)
			}
			_, blockers, err := r.SetStatus(ctx, a.TaskID, task.StatusDone, TransitionOptions{Reason: "thử"})
			if err == nil || !strings.Contains(strings.Join(blockers, "\n"), "migrations/001.sql") {
				t.Fatalf("done của A phải bị chặn vì migrations/001.sql: %v %v", blockers, err)
			}
		})
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
			files, err := ScopeFiles(ctx, h.root, h.tasks, tk, h.cfg.Limits.ProtectedPaths)
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
	files, err := ScopeFiles(ctx, h.root, h.tasks, h.load(a.TaskID), h.cfg.Limits.ProtectedPaths)
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

// Phát hiện .8: ScopeFiles được tính khi Runner còn giữ khoá độc quyền của b-feat. Giả lập b-feat của tác vụ
// khác giành khoá ngay khi A nhả (afterStepUnlock ghi một tệp bảo vệ chưa thuộc Touched của ai): tệp đó không
// được quy cho A, nên không có câu hỏi duyệt, không có PendingPaths/ProtectedApproval.
func TestScopeComputedUnderFeatLock(t *testing.T) {
	h := newHarness(t)
	old := afterStepUnlock
	t.Cleanup(func() { afterStepUnlock = old })
	afterStepUnlock = func(step string) {
		if step == StepFeat {
			h.write("migrations/003.sql", "-- b-feat của tác vụ khác vừa giành khoá\n")
		}
	}
	var asked []Question
	r := h.runner()
	r.Confirm = func(q Question) bool { asked = append(asked, q); return true }
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	tk := h.load(res.TaskID)
	if res.Status != task.StatusInReview || len(asked) != 0 || tk.State.ProtectedApproval != nil || len(tk.State.PendingPaths) != 0 {
		t.Fatalf("tệp của tác vụ khác bị quy cho A: %+v, hỏi %+v, trạng thái %+v", res, asked, tk.State)
	}
	if _, err := os.Stat(filepath.Join(h.root, "migrations", "003.sql")); err != nil {
		t.Fatalf("seam không chạy: %v", err)
	}
}

// Nhánh resume (stepCheck: b-feat đã chạy ở lượt trước): checkScope lấy khoá CHIA SẺ nên đợi b-feat đang chạy
// của tác vụ khác xong, rồi nhả trước khi hỏi người.
func TestScopeResumeTakesSharedLock(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.behaveFeat(func() {
		h.write("app.txt", "v2\n")
		h.write("migrations/002.sql", "-- cần duyệt\n")
		h.write("done.txt", "ok\n")
	})
	res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	tk := h.load(res.TaskID)
	if res.Status != task.StatusBlocked || !tk.State.AwaitingCheck {
		t.Fatalf("lượt đầu phải chờ duyệt: %+v %+v", res, tk.State)
	}
	if _, _, err := h.runner().Approve(res.TaskID); err != nil {
		t.Fatal(err)
	}
	// Một b-feat khác đang giữ khoá độc quyền.
	other, err := AcquireLock(h.tasks.Dir, h.root, "tác vụ khác b-feat", LockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	r := h.runner()
	r.Logf = func(format string, a ...any) {
		if strings.Contains(fmt.Sprintf(format, a...), "bước scope cần khoá chỉ đọc") {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	}
	done := make(chan *Result, 1)
	go func() {
		out, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: res.TaskID, Resume: true})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		other.Release()
		t.Fatal("checkScope của lượt resume không chờ khoá")
	}
	select {
	case <-done:
		other.Release()
		t.Fatal("lượt resume xong khi b-feat khác còn giữ khoá độc quyền")
	case <-time.After(200 * time.Millisecond):
	}
	other.Release()
	select {
	case out := <-done:
		if out == nil || out.Status != task.StatusInReview {
			t.Fatalf("sau khi nhả khoá: %+v", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lượt resume không tiếp tục sau khi khoá được nhả")
	}
}
