package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/state"
	"bint/internal/task"
)

// addWorktree tạo worktree Git thứ hai của h.root.
func (h *harness) addWorktree() string {
	h.t.Helper()
	wt := filepath.Join(h.t.TempDir(), "wt")
	h.git("worktree", "add", "-q", "-b", "wt-branch", wt)
	return wt
}

// taskIn lập phiếu b-feat với Worktree là wt (như khi gõ `bint task new` đứng ở wt).
func (h *harness) taskIn(wt string) string {
	h.t.Helper()
	r := h.runner()
	r.Env.Root = wt
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		h.t.Fatal(err)
	}
	if !samePath(tk.State.Worktree, wt) {
		h.t.Fatalf("State.Worktree = %q, mong %q", tk.State.Worktree, wt)
	}
	return tk.State.ID
}

// AC-4: resume gõ ở thư mục khác chạy đúng State.Worktree: agent nhận Dir của worktree tác vụ, kiểm chứng và
// phạm vi tính ở đó, cây của thư mục đang đứng không bị chạm.
func TestRootForResumeRunsInTaskWorktree(t *testing.T) {
	h := newHarness(t)
	wt := h.addWorktree()
	id := h.taskIn(wt)

	h.behave = func(ctx context.Context, step string, e *Env, tid string) error {
		if step == StepFeat {
			for name, body := range map[string]string{"app.txt": "v2\n", "done.txt": "ok\n"} {
				if err := os.WriteFile(filepath.Join(wt, name), []byte(body), 0o644); err != nil {
					return err
				}
			}
		}
		return nil
	}
	sink := newLogSink()
	r := h.runner() // Root = thư mục chính, khác State.Worktree
	r.Logf = sink.logf
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusInReview {
		t.Fatalf("%+v", res)
	}
	if len(h.dirs) != 1 || !samePath(h.dirs[0], wt) {
		t.Fatalf("agent nhận Dir = %v, mong %s", h.dirs, wt)
	}
	if !strings.Contains(sink.all(), "chạy trong "+wt) {
		t.Fatalf("thiếu dòng 'chạy trong':\n%s", sink.all())
	}
	if _, err := os.Stat(filepath.Join(h.root, "done.txt")); err == nil {
		t.Fatal("thư mục chính bị chạm")
	}
	tk := h.load(id)
	if v := tk.State.LastVerification(); v == nil || !v.Passed {
		t.Fatalf("kiểm chứng phải chạy ở worktree của tác vụ: %+v", tk.State.Verifications)
	}
	if !slices.Equal(tk.State.Touched, []string{"app.txt", "done.txt"}) {
		t.Fatalf("Touched = %v", tk.State.Touched)
	}
	// Runner dùng lại được: Env gốc không bị đổi Root.
	if !samePath(r.Root, h.root) {
		t.Fatalf("Runner.Root bị đổi: %s", r.Root)
	}
}

// Thư mục làm việc của tác vụ không còn: failed với thông báo rõ (kể cả khi tác vụ đang blocked); không lùi về cwd.
func TestRootForWorktreeGoneFails(t *testing.T) {
	for _, paused := range []bool{false, true} {
		h := newHarness(t)
		wt := h.addWorktree()
		id := h.taskIn(wt)
		if paused {
			if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
				return t.State.Apply(task.StatusBlocked, "người", "chờ", "2026-01-01T00:00:00Z")
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id, Resume: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != task.StatusFailed || !strings.Contains(res.Reason, "thư mục làm việc của tác vụ không còn") ||
			!strings.Contains(res.Reason, wt) || len(h.calls) != 0 {
			t.Fatalf("paused=%v: %+v calls=%v", paused, res, h.calls)
		}
		if _, err := os.Stat(filepath.Join(h.root, "done.txt")); err == nil {
			t.Fatal("không được chạy lùi về thư mục chính")
		}
		// Các thao tác khác trên tác vụ báo lỗi rõ thay vì chạy nhầm cây.
		r := h.runner()
		tk := h.load(id)
		if _, err := r.RootFor(tk); !errors.Is(err, ErrWorktreeGone) || !strings.Contains(err.Error(), wt) {
			t.Fatalf("RootFor: %v", err)
		}
		if _, _, err := r.Verify(context.Background(), id); !errors.Is(err, ErrWorktreeGone) {
			t.Fatalf("Verify: %v", err)
		}
		if _, err := r.UnapprovedChanges(context.Background(), tk); !errors.Is(err, ErrWorktreeGone) {
			t.Fatalf("UnapprovedChanges: %v", err)
		}
		if _, err := r.DoneInputFor(context.Background(), tk); !errors.Is(err, ErrWorktreeGone) {
			t.Fatalf("DoneInputFor: %v", err)
		}
	}
}

// RootFor: phiếu cũ (không Worktree) và Worktree trùng Root dùng Root; thư mục tồn tại nhưng không còn là gốc
// worktree Git thì cũng là "không còn".
func TestRootForCases(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := &task.Task{}
	if got, err := r.RootFor(tk); err != nil || got != h.root {
		t.Fatalf("phiếu cũ: %q %v", got, err)
	}
	tk.State.Worktree = h.root + string(os.PathSeparator)
	if got, err := r.RootFor(tk); err != nil || got != h.root {
		t.Fatalf("trùng Root: %q %v", got, err)
	}
	plain := t.TempDir() // tồn tại nhưng không phải worktree Git
	tk.State.Worktree = plain
	if _, err := r.RootFor(tk); !errors.Is(err, ErrWorktreeGone) {
		t.Fatalf("thư mục không phải worktree: %v", err)
	}
	// Thư mục con của worktree không phải gốc.
	sub := filepath.Join(h.root, "migrations")
	tk.State.Worktree = sub
	if _, err := r.RootFor(tk); !errors.Is(err, ErrWorktreeGone) {
		t.Fatalf("thư mục con: %v", err)
	}
	wt := h.addWorktree()
	tk.State.Worktree = wt
	if got, err := r.RootFor(tk); err != nil || state.NormalizePath(got) != state.NormalizePath(wt) {
		t.Fatalf("worktree thứ hai: %q %v", got, err)
	}
}
