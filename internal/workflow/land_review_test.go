package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/task"
)

// unmergedTask dựng tác vụ worktree đã `land --3way` với app.txt không trộn được (người xoá tệp ở cây gốc).
func unmergedTask(t *testing.T) (*harness, *Runner, string) {
	t.Helper()
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, func(e *Env) error {
		return os.Remove(filepath.Join(h.root, "app.txt"))
	})
	r := h.runner()
	id := h.newDo(r, NewTaskOptions{}).State.ID
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id}); err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("phải blocked: %+v %v", res, err)
	}
	res, err := h.runner().Env.Land(context.Background(), id, LandOptions{ThreeWay: true})
	if err != nil || !slices.Equal(res.Unmerged, []string{"app.txt"}) {
		t.Fatalf("land --3way: %+v %v", res, err)
	}
	return h, r, id
}

// Review .8: cancel không được gỡ worktree còn giữ bản của tác vụ cho tệp không trộn được, trừ khi người --discard.
func TestCancelKeepsWorktreeWithUnmergedFiles(t *testing.T) {
	h, r, id := unmergedTask(t)
	ctx := context.Background()
	wt := r.WorktreePath(id)

	agent := h.runner().Env
	agent.Human = false
	var dr *ErrDiscardRequired
	if _, _, err := agent.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{}); !errors.As(err, &dr) ||
		!slices.Equal(dr.Files, []string{"app.txt"}) {
		t.Fatalf("agent cancel không --discard phải bị từ chối: %v", err)
	}
	if _, _, err := agent.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Discard: true}); !errors.Is(err, ErrAgent) {
		t.Fatalf("agent --discard: %v", err)
	}
	if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{}); !errors.As(err, &dr) {
		t.Fatalf("người cancel không --discard cũng phải bị từ chối: %v", err)
	}
	if _, err := os.Stat(wt); err != nil || h.readFile(wt, "app.txt") != "v2\n" {
		t.Fatalf("worktree phải còn nguyên: %v", err)
	}
	if h.status(id) == task.StatusCancelled {
		t.Fatal("trạng thái không được đổi khi bị từ chối")
	}
	if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Discard: true, Reason: "bỏ"}); err != nil {
		t.Fatalf("người --discard: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree còn sau --discard: %v", err)
	}
}

// Review .11: doctor không gợi ý xoá worktree giữ cho tệp không trộn được khi tác vụ còn chờ người ghép.
func TestOrphanAwaitingMergeNotSuggestedForRemoval(t *testing.T) {
	h, r, id := unmergedTask(t)
	ctx := context.Background()
	orph, err := r.OrphanWorktrees(ctx)
	if err != nil || len(orph) != 1 || !orph[0].Kept || !orph[0].AwaitingMerge {
		t.Fatalf("orphan: %+v %v", orph, err)
	}
	for _, c := range r.CleanupCommands(orph[0]) {
		if strings.Contains(c, "remove") || strings.Contains(c, "branch -D") {
			t.Fatalf("không được gợi ý xoá khi tác vụ còn chờ ghép: %q", c)
		}
	}
	// Người xác nhận xong (done --force) thì worktree mới là mồ côi bình thường.
	if _, _, err := h.runner().SetStatus(ctx, id, task.StatusDone, TransitionOptions{Force: true, Reason: "đã ghép tay"}); err != nil {
		t.Fatal(err)
	}
	orph, _ = r.OrphanWorktrees(ctx)
	if len(orph) != 1 || orph[0].AwaitingMerge {
		t.Fatalf("sau done: %+v", orph)
	}
	if cmds := strings.Join(r.CleanupCommands(orph[0]), "\n"); !strings.Contains(cmds, "worktree remove") {
		t.Fatalf("sau done phải gợi ý dọn: %q", cmds)
	}
}

// Review .10: chỉ dấu xung đột mang nhãn của land mới chặn done; dòng "<<<<<<< " có sẵn trong tệp thì không.
func TestLandConflictsLeftMatchesOwnLabel(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("fixture.txt", "<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> b\n")
	write("left.txt", "a\n<<<<<<< "+mergeLabelOrigin+"\nB\n=======\nC\n>>>>>>> "+mergeLabelWorktree+"\n")
	write("crlf.txt", "a\r\n<<<<<<< "+mergeLabelOrigin+"\r\nB\r\n")
	write("done.txt", "a\nB-gop\n")
	tk := &task.Task{State: task.State{Origin: dir, Landed: &task.Landing{
		Conflicts: []string{"crlf.txt", "done.txt", "fixture.txt", "left.txt"},
	}}}
	if got := landConflictsLeft(tk); !slices.Equal(got, []string{"crlf.txt", "left.txt"}) {
		t.Fatalf("landConflictsLeft = %v", got)
	}
}

// signalWriter báo một lần khi có dòng chứa want được ghi.
type signalWriter struct {
	mu   sync.Mutex
	want string
	once sync.Once
	ch   chan struct{}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.Contains(string(p), w.want) {
		w.once.Do(func() { close(w.ch) })
	}
	return len(p), nil
}

// Review .9: lượt land chờ khoá cây gốc trong lúc lượt khác đã đưa về thì báo "đã đưa về", không áp lần hai và
// không báo xung đột giả.
func TestLandRechecksLandedUnderLock(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	ctx := context.Background()
	id := h.newDo(r, NewTaskOptions{}).State.ID
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	before := h.readFile(h.root, "app.txt")

	held, err := AcquireLock(h.tasks.Dir, h.root, "người khác", LockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	sw := &signalWriter{want: "chờ khoá cây gốc", ch: make(chan struct{})}
	env := h.runner().Env
	env.Out = sw
	done := make(chan error, 1)
	go func() {
		_, err := env.Land(ctx, id, LandOptions{})
		done <- err
	}()
	select {
	case <-sw.ch:
	case <-time.After(30 * time.Second):
		held.Release()
		t.Fatal("Land không chờ khoá cây gốc")
	}
	// Lượt khác đưa về xong trong lúc lượt này chờ.
	if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.State.Landed = &task.Landing{At: time.Now().UTC().Format(time.RFC3339), By: "lượt khác"}
		return nil
	}); err != nil {
		held.Release()
		t.Fatal(err)
	}
	held.Release()
	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Land không trả về sau khi nhả khoá")
	}
	var ce *LandConflictError
	if err == nil || errors.As(err, &ce) || !strings.Contains(err.Error(), "đã đưa về") {
		t.Fatalf("phải báo đã đưa về, không phải xung đột: %v", err)
	}
	if h.readFile(h.root, "app.txt") != before {
		t.Fatal("cây gốc bị áp lần hai")
	}
	if l := loadTask(t, h, id).State.Landed; l == nil || l.By != "lượt khác" {
		t.Fatalf("Landed bị ghi đè: %+v", l)
	}
}
