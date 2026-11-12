package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/harness/hookcore"
	"bint/internal/task"
)

// Test của G4 (docs/isolation-subagent-plan.md mục 7, 12.14): tệp runtime/session-steps cho guardrail bint-worktree của hook,
// câu trong đề bài, và hai tác vụ worktree chạy xen kẽ bằng subagent giả.

func TestSubagentNamesMatchHookcore(t *testing.T) {
	if hookcore.SubagentImplementer != SubagentImplementer {
		t.Fatalf("hookcore lệch workflow: %q / %q", hookcore.SubagentImplementer, SubagentImplementer)
	}
}

func (h *harness) sessionStep(runID string) (hookcore.SessionStepRecord, bool) {
	h.t.Helper()
	return hookcore.ReadSessionStep(filepath.Dir(h.tasks.Dir), runID)
}

func (h *harness) sessionStepFiles() []string {
	ents, _ := os.ReadDir(filepath.Join(filepath.Dir(h.tasks.Dir), "session-steps"))
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// Tác vụ worktree: `next` ghi tệp ánh xạ đúng worktree và isolated; đề bài đòi cd + đường dẫn tuyệt đối và báo hook chặn;
// `complete` gỡ tệp.
func TestSessionStepRecordWorktreeAndPrompt(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	act, err := h.runner().Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || act.Action != ActionAgent {
		t.Fatal(act, err)
	}
	rec, ok := h.sessionStep(act.RunID)
	if !ok {
		t.Fatalf("thiếu tệp session-steps/%s.json (có %v)", act.RunID, h.sessionStepFiles())
	}
	wt, _ := filepath.Abs(act.Worktree)
	if rec.TaskID != tk.State.ID || rec.Step != StepFeat || rec.Subagent != SubagentImplementer || !rec.Isolated || rec.Worktree != wt ||
		rec.Worktree == h.root || rec.Until != act.LeaseUntil {
		t.Fatalf("bản ghi: %+v (hành động %+v)", rec, act)
	}
	for _, want := range []string{hookcore.CdPrefix(wt) + "…", "giữ nguyên dấu nháy", "đường dẫn tuyệt đối dưới " + wt, "Hook của Bint chặn", "KHÔNG phải worktree"} {
		if !strings.Contains(act.Prompt, want) {
			t.Fatalf("đề bài thiếu %q:\n%s", want, act.Prompt)
		}
	}
	h.subagent(act)
	if _, err := h.runner().Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: act.RunID, AgentID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.sessionStep(act.RunID); ok {
		t.Fatal("complete phải gỡ tệp session-steps")
	}
}

// Tác vụ shared: vẫn có tệp (isolated=false, worktree = cây gốc) để hook phân biệt với "không ánh xạ được"; đề bài không có
// câu về hook chặn đường dẫn.
func TestSessionStepRecordSharedTask(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || act.Action != ActionAgent {
		t.Fatal(act, err)
	}
	rec, ok := h.sessionStep(act.RunID)
	if !ok || rec.Isolated || rec.TaskID != tk.State.ID {
		t.Fatalf("bản ghi shared: %+v %v", rec, ok)
	}
	if strings.Contains(act.Prompt, "Hook của Bint chặn") || strings.Contains(act.Prompt, "KHÔNG phải worktree") {
		t.Fatalf("đề bài shared không được có câu về worktree:\n%s", act.Prompt)
	}
}

// Mọi nơi bước dở bị gỡ đều gỡ tệp: abort, huỷ tác vụ, đóng bước hết hạn bởi `next`.
func TestSessionStepRecordRemovedWhenPendingDropped(t *testing.T) {
	ctx := context.Background()
	open := func(t *testing.T) (*harness, *Runner, *task.Task, *SessionAction) {
		h := newHarness(t).wtHarness()
		h.behave = featWrites(h, nil)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
		act, err := h.runner().Next(ctx, NextOptions{TaskRef: tk.State.ID})
		if err != nil || act.Action != ActionAgent {
			t.Fatal(act, err)
		}
		if _, ok := h.sessionStep(act.RunID); !ok {
			t.Fatal("thiếu tệp sau next")
		}
		return h, r, tk, act
	}
	t.Run("abort", func(t *testing.T) {
		h, _, tk, act := open(t)
		if _, err := h.runner().Abort(ctx, tk.State.ID, "thử"); err != nil {
			t.Fatal(err)
		}
		if _, ok := h.sessionStep(act.RunID); ok {
			t.Fatalf("abort phải gỡ tệp: %v", h.sessionStepFiles())
		}
	})
	t.Run("huỷ tác vụ", func(t *testing.T) {
		h, _, tk, act := open(t)
		if _, _, err := h.runner().SetStatus(ctx, tk.State.ID, task.StatusCancelled, TransitionOptions{Reason: "bỏ"}); err != nil {
			t.Fatal(err)
		}
		if _, ok := h.sessionStep(act.RunID); ok {
			t.Fatalf("huỷ phải gỡ tệp: %v", h.sessionStepFiles())
		}
	})
	t.Run("hết hạn", func(t *testing.T) {
		advance := fakeLeaseClock(t)
		h, _, tk, act := open(t)
		advance(48 * time.Hour)
		if _, err := h.runner().Next(ctx, NextOptions{TaskRef: tk.State.ID}); err != nil {
			t.Fatal(err)
		}
		if _, ok := h.sessionStep(act.RunID); ok {
			t.Fatalf("đóng bước hết hạn phải gỡ tệp: %v", h.sessionStepFiles())
		}
	})
}

// Hai tác vụ worktree chạy xen kẽ bằng subagent giả; mỗi tác vụ tự land ở finalize.
type twoTasks struct {
	h      *harness
	a, b   *task.Task
	files  map[string][2]string // task id → {tệp, nội dung}
	opened map[string]*SessionAction
}

func newTwoTasks(t *testing.T, fileA, fileB string) *twoTasks {
	h := newHarness(t).wtHarness()
	x := &twoTasks{h: h, opened: map[string]*SessionAction{}}
	// Mỗi tác vụ chỉ ghi tệp của riêng nó (không có done.txt chung, nếu không hai tác vụ cùng thêm một tệp sẽ xung đột).
	cmd := "test -f a.txt || test -f b.txt || test -f app.txt"
	h.cfg.Eval.TestCommand = &cmd
	r := h.runner()
	x.a = h.newSession(r, NewTaskOptions{Plan: ptr(false), Request: "Việc A: " + fileA})
	x.b = h.newSession(r, NewTaskOptions{Plan: ptr(false), Request: "Việc B: " + fileB})
	x.files = map[string][2]string{x.a.State.ID: {fileA, "từ A\n"}, x.b.State.ID: {fileB, "từ B\n"}}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepFeat:
			f := x.files[id]
			return writeAt(e.Root, f[0], f[1])
		case StepReview:
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		}
		return nil
	}
	return x
}

func (x *twoTasks) next(t *testing.T, id string) *SessionAction {
	t.Helper()
	act, err := x.h.runner().Next(context.Background(), NextOptions{TaskRef: id})
	if err != nil || act.Action != ActionAgent {
		t.Fatalf("next %s: %+v %v", id, act, err)
	}
	x.opened[id] = act
	return act
}

func (x *twoTasks) complete(t *testing.T, id string) *SessionAction {
	t.Helper()
	act := x.opened[id]
	x.h.subagent(act)
	c, err := x.h.runner().Complete(context.Background(), CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "ag-" + act.RunID})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Hai bước b-feat mở cùng lúc (hai tệp ánh xạ), hoàn thành đảo thứ tự, rồi hai bước b-review cũng chồng lên nhau.
func (x *twoTasks) run(t *testing.T) (resA, resB *SessionAction) {
	t.Helper()
	a, b := x.a.State.ID, x.b.State.ID
	fa, fb := x.next(t, a), x.next(t, b)
	if fa.Worktree == fb.Worktree || fa.RunID == fb.RunID {
		t.Fatalf("hai tác vụ phải có worktree/lượt riêng: %q %q", fa.Worktree, fb.Worktree)
	}
	for _, act := range []*SessionAction{fa, fb} {
		if rec, ok := x.h.sessionStep(act.RunID); !ok || !rec.Isolated || rec.TaskID != act.TaskID {
			t.Fatalf("tệp ánh xạ của %s: %+v %v", act.TaskID, rec, ok)
		}
	}
	if len(x.h.sessionStepFiles()) != 2 {
		t.Fatalf("muốn 2 tệp ánh xạ cùng lúc, có %v", x.h.sessionStepFiles())
	}
	if c := x.complete(t, b); c.Action != "" {
		t.Fatalf("complete B: %+v", c)
	}
	if c := x.complete(t, a); c.Action != "" {
		t.Fatalf("complete A: %+v", c)
	}
	ra, rb := x.next(t, a), x.next(t, b)
	if ra.Step != StepReview || rb.Step != StepReview {
		t.Fatalf("bước kế: %s %s", ra.Step, rb.Step)
	}
	resA = x.complete(t, a) // A land trước
	resB = x.complete(t, b)
	if n := len(x.h.sessionStepFiles()); n != 0 {
		t.Fatalf("còn %d tệp ánh xạ sau khi cả hai xong: %v", n, x.h.sessionStepFiles())
	}
	return resA, resB
}

func TestSessionTwoWorktreeTasksInterleaveToDone(t *testing.T) {
	x := newTwoTasks(t, "a.txt", "b.txt")
	resA, resB := x.run(t)
	if resA.Action != ActionDone || resB.Action != ActionDone {
		t.Fatalf("A: %+v\nB: %+v", resA, resB)
	}
	for _, tk := range []*task.Task{x.a, x.b} {
		got := loadTask(t, x.h, tk.State.ID)
		if got.State.Status != task.StatusDone || got.State.Landed == nil {
			t.Fatalf("%s: %s landed=%v", tk.State.ID, got.State.Status, got.State.Landed)
		}
	}
	if x.h.readFile(x.h.root, "a.txt") != "từ A\n" || x.h.readFile(x.h.root, "b.txt") != "từ B\n" {
		t.Fatal("cây gốc phải có đủ thay đổi của cả hai tác vụ")
	}
}

// Cùng tệp, nội dung khác: tác vụ land sau bị blocked riêng (xung đột khi đưa về); tác vụ trước vẫn done và cây gốc chỉ có
// thay đổi của nó.
func TestSessionTwoWorktreeTasksConflictBlocksOnlyLater(t *testing.T) {
	x := newTwoTasks(t, "app.txt", "app.txt")
	resA, resB := x.run(t)
	if resA.Action != ActionDone {
		t.Fatalf("A (land trước) phải done: %+v", resA)
	}
	if resB.Action != ActionBlocked || !strings.HasPrefix(resB.Reason, "xung đột khi đưa về: app.txt") {
		t.Fatalf("B (land sau) phải blocked vì xung đột: %+v", resB)
	}
	if got := loadTask(t, x.h, x.a.State.ID); got.State.Status != task.StatusDone {
		t.Fatalf("A: %s", got.State.Status)
	}
	if got := loadTask(t, x.h, x.b.State.ID); got.State.Status != task.StatusBlocked || got.State.Landed != nil {
		t.Fatalf("B: %s landed=%v", got.State.Status, got.State.Landed)
	}
	if x.h.readFile(x.h.root, "app.txt") != "từ A\n" {
		t.Fatalf("cây gốc chỉ có thay đổi của A, được %q", x.h.readFile(x.h.root, "app.txt"))
	}
}
