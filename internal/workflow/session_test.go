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

	"bint/internal/config"
	"bint/internal/spool"
	"bint/internal/task"
)

// Test của G3a (docs/isolation-subagent-plan.md mục 6.1–6.3): driver session — `next` / `complete` / `abort`.
// Subagent giả là hàm h.behave chạy giữa Next và Complete, không có BINT_RUN_ID/BINT_TASK_ID như subagent thật.

// subagent chạy h.behave như subagent của Claude Code: ở thư mục của hành động, không biến môi trường lượt/tác vụ.
func (h *harness) subagent(act *SessionAction) {
	h.t.Helper()
	h.mu.Lock()
	h.calls = append(h.calls, act.Step)
	h.prompts[act.Step] = act.Prompt
	h.mu.Unlock()
	env := &Env{Root: act.Worktree, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool, Actor: "agent"}
	if err := h.behave(context.Background(), act.Step, env, act.TaskID); err != nil {
		h.t.Fatalf("subagent %s: %v", act.Step, err)
	}
}

// drive lặp next → subagent → complete tới khi tác vụ dừng; trả các bước agent đã chạy và hành động dừng.
func (h *harness) drive(r *Runner, id string, resume bool) ([]string, *SessionAction) {
	h.t.Helper()
	var steps []string
	for i := 0; i < 20; i++ {
		act, err := r.Next(context.Background(), NextOptions{TaskRef: id, Resume: resume && i == 0})
		if err != nil {
			h.t.Fatal(err)
		}
		if act.Action != ActionAgent {
			return steps, act
		}
		steps = append(steps, act.Step)
		h.subagent(act)
		c, err := r.Complete(context.Background(), CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "agent-" + act.Step})
		if err != nil {
			h.t.Fatal(err)
		}
		if c.Action != "" {
			return steps, c
		}
	}
	h.t.Fatal("vòng session không dừng")
	return nil, nil
}

func (h *harness) newSession(r *Runner, opts NewTaskOptions) *task.Task {
	h.t.Helper()
	opts.Driver = task.DriverSession
	if opts.Request == "" {
		opts.Request = "Thêm tính năng phiên bản v2 cho app.txt"
	}
	if opts.Workflow == "" {
		opts.Workflow = StepDo
	}
	tk, _, err := r.NewTask(context.Background(), opts)
	if err != nil {
		h.t.Fatal(err)
	}
	return tk
}

func (h *harness) leaseFile(id string) string {
	h.t.Helper()
	tk := loadTask(h.t, h, id)
	return leaseFilePath(lockPath(h.tasks.Dir, tk.State.Worktree))
}

// AC-2: đủ chuỗi b-req → b-feat → b-review → done; lượt cha/con và agent_id được ghi.
func TestSessionFullSequenceToDone(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{})
	if c := tk.State.Classification; c.EffectiveDriver() != task.DriverSession || c.DriverReason != task.DriverReasonUser {
		t.Fatalf("driver: %+v", c)
	}
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-req,b-feat,b-review" || last.Action != ActionDone || last.Status != task.StatusDone {
		t.Fatalf("bước %v, dừng: %+v", steps, last)
	}
	if last.Result == nil || last.Result.Step != StepReview || last.Result.Status != "completed" {
		t.Fatalf("kết quả bước cuối: %+v", last.Result)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.PendingStep != nil || got.State.Runner != nil || got.State.SessionRun != "" {
		t.Fatalf("sót trạng thái session: pending=%v runner=%v session=%q", got.State.PendingStep, got.State.Runner, got.State.SessionRun)
	}
	if len(got.State.AgentRuns) != 3 || got.State.AgentRuns[0].AgentID != "agent-b-req" {
		t.Fatalf("AgentRuns: %+v", got.State.AgentRuns)
	}
	if v := got.State.LastVerification(); v == nil || !v.Passed || !v.Confirmed {
		t.Fatalf("kiểm chứng: %+v", v)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hợp đồng thuê còn sót: %v", err)
	}
	// Đề bài cho subagent: không có biến BINT_*, ID tác vụ nằm trong đề bài.
	p := h.prompts[StepFeat]
	if strings.Contains(p, "BINT_TASK_ID.") || !strings.Contains(p, "KHÔNG có biến BINT_TASK_ID") && !strings.Contains(p, "Không có biến BINT_TASK_ID") ||
		!strings.Contains(p, tk.State.ID) {
		t.Fatalf("đề bài subagent:\n%s", p)
	}
	// Lượt: cha (session) + 3 con; agent_id nằm ở workflow.finished của lượt con.
	files, err := spool.Pending(h.spool)
	if err != nil {
		t.Fatal(err)
	}
	var evs []spool.Event
	for _, f := range files {
		es, err := spool.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, es...)
	}
	var started, finished, agents int
	for _, e := range evs {
		switch {
		case e.Type == spool.WorkflowStarted && e.Payload["driver"] == task.DriverSession:
			started++
		case e.Type == spool.WorkflowFinished && e.Payload["agent_id"] != nil:
			agents++
		case e.Type == spool.WorkflowFinished && e.RunID != "" && e.Payload["end_reason"] != nil && strings.HasPrefix(e.Payload["end_reason"].(string), "task "):
			finished++
		}
	}
	if started != 1 || finished != 1 || agents != 3 {
		t.Fatalf("sự kiện: started=%d finished=%d agent_id=%d", started, finished, agents)
	}
}

// b-review sửa tệp ⇒ complete đánh blocked (giữ kết quả bước), như driver process; `next --resume` đi tiếp sau bước đã
// giữ (áp quyết định của vòng review đã ghi), không trả lại action agent cho b-review.
func TestSessionReadonlyReviewModifyingTreeBlocksThenResume(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			h.write("app.txt", "sửa đồng thời\n")
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{})
	id := tk.State.ID
	steps, last := h.drive(r, id, false)
	if steps[len(steps)-1] != StepReview || last.Action != ActionBlocked || last.Result == nil || last.Result.Status != "blocked" ||
		!strings.Contains(last.Result.Error, "bước chỉ đọc") || !strings.Contains(last.Result.Error, "app.txt") ||
		!strings.Contains(last.Result.Error, "bint run next "+id+" --resume") || !strings.Contains(last.Result.Error, "hoàn tác") {
		t.Fatalf("bước %v, dừng: %+v result=%+v", steps, last, last.Result)
	}
	got := loadTask(t, h, id)
	if got.State.Status != task.StatusBlocked || got.State.PendingStep != nil || got.State.HeldStep == nil || got.State.HeldStep.Step != StepReview {
		t.Fatalf("tác vụ: %s pending=%v held=%+v", got.State.Status, got.State.PendingStep, got.State.HeldStep)
	}
	if rev, err := got.LoadReview(); err != nil || rev.Last() == nil {
		t.Fatalf("vòng review phải được giữ: %v", err)
	}

	// Resume: đi tiếp từ sau b-review (áp quyết định), không mở lại b-review.
	reviews := countCalls(h, StepReview)
	if _, _, err := r.Verify(context.Background(), id); err != nil { // cổng done đòi kiểm chứng khớp cây hiện tại
		t.Fatal(err)
	}
	steps2, last2 := h.drive(r, id, true)
	if slices.Contains(steps2, StepReview) || countCalls(h, StepReview) != reviews {
		t.Fatalf("resume không được chạy lại b-review: %v calls=%v", steps2, h.calls)
	}
	if got := loadTask(t, h, id); got.State.Status != task.StatusDone || got.State.HeldStep != nil {
		t.Fatalf("sau resume: %s held=%+v (dừng: %+v)", got.State.Status, got.State.HeldStep, last2)
	}
}

// Sửa đường dẫn bảo vệ ⇒ blocked chờ duyệt như driver process; duyệt rồi next --resume không chạy lại b-feat.
func TestSessionProtectedPathBlocksThenResume(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			h.write("migrations/002.sql", "ALTER TABLE x;\n")
		}
		return base(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	got := loadTask(t, h, tk.State.ID)
	if strings.Join(steps, ",") != "b-feat" || last.Action != ActionBlocked || len(got.State.PendingPaths) != 1 ||
		got.State.PendingPaths[0] != "migrations/002.sql" || !got.State.AwaitingCheck || got.State.PendingStep != nil {
		t.Fatalf("bước %v, dừng: %+v, state: %+v", steps, last, got.State)
	}
	// Chưa xử lý: next (không --resume) vẫn dừng, không mở bước.
	if a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID}); err != nil || a.Action != ActionBlocked {
		t.Fatalf("next khi blocked: %+v %v", a, err)
	}
	// Agent không duyệt được; resume khi còn thay đổi chờ duyệt bị từ chối.
	if a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true}); err != nil || a.Action != ActionBlocked ||
		!strings.Contains(a.Reason, "chờ người duyệt") {
		t.Fatalf("resume chưa duyệt: %+v %v", a, err)
	}
	if _, a, err := r.Approve(tk.State.ID); err != nil || len(a.Paths) != 1 {
		t.Fatalf("duyệt: %+v %v", a, err)
	}
	steps, last = h.drive(r, tk.State.ID, true)
	if strings.Join(steps, ",") != "b-review" || last.Action != ActionDone {
		t.Fatalf("tiếp tục: bước %v, dừng %+v", steps, last)
	}
}

// Vòng review dừng đúng workflow.max_review_rounds.
func TestSessionReviewLoopStopsAtMaxRounds(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`))
			return err
		}
		return base(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat,b-review,b-feat,b-review" || last.Action != ActionBlocked ||
		!strings.Contains(last.Reason, "sau 2 vòng review") {
		t.Fatalf("bước %v, dừng: %+v", steps, last)
	}
	if !strings.Contains(h.prompts[StepFeat], "Phát hiện review vòng 1 cần xử lý") {
		t.Fatalf("b-feat vòng 2 phải nhận phát hiện:\n%s", h.prompts[StepFeat])
	}
}

// Tác vụ worktree: subagent làm việc trong worktree riêng, land ở finalize rồi done.
func TestSessionWorktreeLandsAtFinalize(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	if tk.State.Isolation() != task.IsolationWorktree {
		t.Fatalf("cô lập: %s", tk.State.Isolation())
	}
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat,b-review" || last.Action != ActionDone {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.Landed == nil || len(got.State.Landed.Files) == 0 {
		t.Fatalf("chưa đưa về: %+v", got.State.Landed)
	}
	if h.readFile(h.root, "app.txt") != "v2\n" || h.readFile(h.root, "done.txt") != "ok\n" {
		t.Fatal("cây gốc chưa nhận thay đổi")
	}
	// Đề bài của subagent đòi cd vào worktree (công cụ Agent không nhận cwd).
	if p := h.prompts[StepFeat]; !strings.Contains(p, "cd "+got.State.Origin+"/") && !strings.Contains(p, "`cd ") {
		t.Fatalf("đề bài thiếu cd worktree:\n%s", p)
	}
	for _, d := range h.dirs {
		_ = d
	}
}

// Driver session: tác vụ worktree có setup_command chưa được người xác nhận dừng blocked ở `next` đầu, trước khi
// tạo worktree hay chạy lệnh; sau khi người xác nhận, `next --resume` chạy tới done.
func TestSessionWorktreeSetupNeedsApproval(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	h.cfg.Workflow.Worktree.SetupCommand = "echo s > s.out"
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	steps, last := h.drive(r, id, false)
	if len(steps) != 0 || last.Action != ActionBlocked || !strings.Contains(last.Reason, "bint task approve "+id) {
		t.Fatalf("phải blocked trước mọi bước: %v %+v", steps, last)
	}
	if _, err := os.Stat(r.WorktreePath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree không được tạo: %v", err)
	}
	if out := h.git("branch", "--list", "bint/"+id); strings.TrimSpace(out) != "" {
		t.Fatalf("nhánh không được tạo: %q", out)
	}
	approveSetup(t, r, id)
	steps, last = h.drive(r, id, true)
	if strings.Join(steps, ",") != "b-feat,b-review" || last.Action != ActionDone {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
}

// next gọi hai lần liên tiếp trả cùng bước; complete với run_id sai bị từ chối và không đổi gì.
func TestSessionNextIdempotentAndCompleteRunMismatch(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a1, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a1.Action != ActionAgent || a1.Step != StepFeat || a1.RunID == "" || a1.Subagent != SubagentImplementer ||
		a1.ReadOnly || a1.Worktree != h.root || a1.LeaseUntil == "" || a1.Prompt == "" {
		t.Fatalf("next 1: %+v %v", a1, err)
	}
	before, _ := os.ReadFile(filepath.Join(loadTask(t, h, tk.State.ID).Dir, "state.json"))
	a2, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a2.RunID != a1.RunID || a2.Prompt != a1.Prompt || a2.Step != a1.Step || a2.LeaseUntil != a1.LeaseUntil {
		t.Fatalf("next 2 phải trả đúng bước: %+v %v", a2, err)
	}
	after, _ := os.ReadFile(filepath.Join(loadTask(t, h, tk.State.ID).Dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("next gọi lại không được đổi phiếu")
	}
	for _, run := range []string{"", "01HXXXXXXXXXXXXXXXXXXXXXXX", a1.RunID + "x"} {
		if _, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: run}); !errors.Is(err, ErrRunMismatch) {
			t.Fatalf("run %q: muốn ErrRunMismatch, được %v", run, err)
		}
	}
	after, _ = os.ReadFile(filepath.Join(loadTask(t, h, tk.State.ID).Dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("complete sai run không được đổi phiếu")
	}
	// Tác vụ không có bước dở: cũng từ chối.
	other := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: other.State.ID, RunID: a1.RunID}); !errors.Is(err, ErrRunMismatch) {
		t.Fatalf("tác vụ không có bước dở: %v", err)
	}
}

// abort xoá bước dở và hợp đồng thuê; tiếp tục bằng next --resume chạy lại bước từ đầu.
func TestSessionAbortClearsPendingAndLease(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a1, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a1.Action != ActionAgent {
		t.Fatal(a1, err)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); err != nil {
		t.Fatalf("phải có hợp đồng thuê: %v", err)
	}
	// Subagent đã sửa một tệp trước khi bị bỏ: vẫn thuộc tác vụ.
	h.write("app.txt", "dở dang\n")
	res, err := r.Abort(ctx, tk.State.ID, "orchestrator chết")
	if err != nil {
		t.Fatal(err)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.PendingStep != nil || got.State.Status != task.StatusInterrupted || got.State.SessionRun != "" || got.State.Runner != nil {
		t.Fatalf("sau abort: %+v (%+v)", got.State, res)
	}
	if !slices.Contains(got.State.Touched, "app.txt") {
		t.Fatalf("Touched phải ghi tệp subagent đã sửa: %v", got.State.Touched)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hợp đồng thuê còn sót: %v", err)
	}
	// Khoá đã nhả: tác vụ khác lấy được khoá độc quyền ngay.
	if l, err := AcquireLock(h.tasks.Dir, h.root, tk.State.ID+" b-feat", LockExclusive); err != nil {
		t.Fatalf("khoá phải trống: %v", err)
	} else {
		l.Release()
	}
	// Không --resume: dừng; có --resume: bước mới (lượt khác) cho cùng tác vụ.
	if a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID}); err != nil || a.Action != ActionBlocked {
		t.Fatalf("next sau abort: %+v %v", a, err)
	}
	a2, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID, Resume: true})
	if err != nil || a2.Action != ActionAgent || a2.Step != StepFeat || a2.RunID == a1.RunID {
		t.Fatalf("resume: %+v %v", a2, err)
	}
	// abort không có bước dở: không lỗi.
	if _, err := r.Abort(ctx, tk.State.ID, ""); err != nil {
		t.Fatal(err)
	}
	if res, err := r.Abort(ctx, tk.State.ID, ""); err != nil || !slices.Contains(res.Notes, "tác vụ không có bước dở") {
		t.Fatalf("abort lần hai: %+v %v", res, err)
	}
}

// Lỗi low của review G3a: abort b-feat khi tệp hợp đồng thuê hỏng — AcquireLock bị chặn bởi chính tệp hỏng nên trước đây
// Touched mất. Nay abort gỡ tệp hỏng (đường khôi phục của người), lấy lại khoá và ghi tệp subagent đã sửa.
func TestSessionAbortWithCorruptLeaseFileStillRecordsTouched(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a1, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a1.Action != ActionAgent || a1.Step != StepFeat {
		t.Fatal(a1, err)
	}
	h.write("app.txt", "dở dang\n")
	if err := os.WriteFile(h.leaseFile(tk.State.ID), []byte("{hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := r.Abort(ctx, tk.State.ID, "orchestrator chết")
	if err != nil {
		t.Fatal(err)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.PendingStep != nil || got.State.Status != task.StatusInterrupted {
		t.Fatalf("sau abort: %+v", got.State)
	}
	if !slices.Contains(got.State.Touched, "app.txt") {
		t.Fatalf("Touched phải ghi tệp subagent đã sửa dù tệp hợp đồng hỏng: %v (ghi chú %v)", got.State.Touched, res.Notes)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tệp hợp đồng hỏng phải bị xoá: %v", err)
	}
	// Khoá bận vì chủ KHÁC (không phải tệp hỏng) không được khớp sentinel: abort chỉ ghi chú, không xoá hợp đồng của chủ đó.
	if errors.Is(&lockedError{holders: []string{"x"}}, errLeaseUnreadable) {
		t.Fatal("lockedError không có cause không được khớp errLeaseUnreadable")
	}
}

// Review G3b .4: tác vụ khác trên cùng cây còn bước dở ⇒ abort KHÔNG gỡ tệp hợp đồng hỏng (gỡ là xoá hợp đồng của tác
// vụ kia và quy tệp nó sửa vào Touched của tác vụ này); chỉ ghi chú cho người.
func TestSessionAbortCorruptLeaseKeepsOtherTasksLease(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	ctx := context.Background()
	a := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	b := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	if a1, err := r.Next(ctx, NextOptions{TaskRef: a.State.ID}); err != nil || a1.Action != ActionAgent || a1.Step != StepFeat {
		t.Fatal(a1, err)
	}
	// B có bước dở trên cùng cây (dựng thẳng trong phiếu: hợp đồng của A độc quyền nên next của B sẽ phải chờ).
	if _, err := h.tasks.Update(b.State.ID, time.Now(), func(t *task.Task) error {
		t.State.PendingStep = &task.PendingStep{RunID: "run-b", Step: StepReq, Mode: "shared"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.write("app.txt", "tệp B đang sửa\n")
	if err := os.WriteFile(h.leaseFile(a.State.ID), []byte("{hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := r.Abort(ctx, a.State.ID, "orchestrator chết")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(h.leaseFile(a.State.ID)); err != nil || string(b) != "{hỏng" {
		t.Fatalf("tệp hợp đồng hỏng không được gỡ khi tác vụ khác còn bước dở: %q %v", b, err)
	}
	if got := loadTask(t, h, a.State.ID); slices.Contains(got.State.Touched, "app.txt") {
		t.Fatalf("tệp của tác vụ khác bị ghi vào Touched: %v", got.State.Touched)
	}
	if notes := strings.Join(res.Notes, "\n"); !strings.Contains(notes, b.State.ID) || !strings.Contains(notes, "không gỡ tệp") {
		t.Fatalf("ghi chú phải nêu tác vụ còn bước dở: %v", res.Notes)
	}
}

// Role gán harness khác Claude ⇒ next trả action process kèm lệnh bint run của đúng bước; các bước khác vẫn bằng
// subagent; next đi tiếp sau bước chạy bằng tiến trình, không review lặp.
func TestSessionProcessActionForOtherHarness(t *testing.T) {
	h := newHarness(t)
	h.cfg.Harnesses.Enabled = append(h.cfg.Harnesses.Enabled, "codex")
	h.cfg.Roles.Implementer = "codex"
	h.cfg.Roles.Reviewer = "codex"
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	want := "bint run b-feat --task " + tk.State.ID + " --json"
	if err != nil || a.Action != ActionProcess || a.Command != want || a.Step != StepFeat {
		t.Fatalf("next: %+v %v", a, err)
	}
	if got := loadTask(t, h, tk.State.ID); got.State.PendingStep != nil {
		t.Fatal("action process không được tạo bước dở")
	}
	// Chạy lệnh được trả (driver process, agent giả): tác vụ sang in_review.
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat bằng tiến trình: %+v %v", res, err)
	}
	a, err = r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionProcess || a.Command != "bint run b-review --task "+tk.State.ID+" --json" {
		t.Fatalf("next sau b-feat: %+v %v", a, err)
	}
	res, err = h.runner().Run(ctx, RunOptions{Workflow: StepReview, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-review bằng tiến trình: %+v %v", res, err)
	}
	// Review đã ghi nhưng chưa ai áp quyết định: next áp nó (finalize), không chạy review lần nữa.
	a, err = r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionDone {
		t.Fatalf("next sau review: %+v %v", a, err)
	}
	if n := strings.Count(strings.Join(h.calls, ","), "b-review"); n != 1 {
		t.Fatalf("review lặp: %v", h.calls)
	}
}

// Review chạy riêng bằng tiến trình có phát hiện nghiêm trọng: next áp quyết định (vòng sửa) rồi trả b-feat.
func TestSessionProcessReviewWithBlockingFindingsStartsFixRound(t *testing.T) {
	h := newHarness(t)
	h.cfg.Harnesses.Enabled = append(h.cfg.Harnesses.Enabled, "codex")
	h.cfg.Roles.Reviewer = "codex"
	base := featWrites(h, nil)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`))
			return err
		}
		return base(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat" || last.Action != ActionProcess || last.Step != StepReview {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
	if _, err := h.runner().Run(ctx, RunOptions{Workflow: StepReview, TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent || a.Step != StepFeat || !strings.Contains(a.Prompt, "Phát hiện review vòng 1 cần xử lý") {
		t.Fatalf("vòng sửa: %+v %v", a, err)
	}
}

// Chặn chéo giữa hai driver: bước dở còn hạn ⇒ `bint run` kiểu tiến trình từ chối; hết hạn ⇒ đóng như quá giờ.
func TestSessionPendingStepBlocksProcessRunUntilExpired(t *testing.T) {
	h := newHarness(t)
	advance := fakeLeaseClock(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	_, err = h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID})
	var pe *PendingStepError
	if !errors.As(err, &pe) || pe.RunID != a.RunID || !strings.Contains(err.Error(), "bint run complete") {
		t.Fatalf("muốn PendingStepError, được %v", err)
	}
	advance(time.Duration(h.cfg.Workflow.TimeoutMinutes+1) * time.Minute)
	// Hết hạn: Run đóng bước như quá giờ (tác vụ failed) rồi từ chối vì tác vụ đang dừng.
	_, err = h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID})
	got := loadTask(t, h, tk.State.ID)
	if err == nil || got.State.Status != task.StatusFailed || got.State.PendingStep != nil ||
		!strings.Contains(got.State.Reason, "quá workflow.timeout_minutes") {
		t.Fatalf("sau hết hạn: err=%v status=%s reason=%q pending=%v", err, got.State.Status, got.State.Reason, got.State.PendingStep)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hợp đồng thuê còn sót: %v", err)
	}
	h.behave = featWrites(h, nil)
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Resume: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("tiếp tục bằng driver process: %+v %v", res, err)
	}
}

// next gặp bước dở đã hết hạn: đóng như quá giờ, báo failed.
func TestSessionNextClosesExpiredPendingStep(t *testing.T) {
	h := newHarness(t)
	advance := fakeLeaseClock(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	advance(time.Duration(h.cfg.Workflow.TimeoutMinutes+1) * time.Minute)
	b, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || b.Action != ActionFailed || !strings.Contains(b.Reason, "quá workflow.timeout_minutes") {
		t.Fatalf("next sau hạn: %+v %v", b, err)
	}
	if got := loadTask(t, h, tk.State.ID); got.State.PendingStep != nil {
		t.Fatal("bước dở phải được gỡ")
	}
	// complete muộn với lượt đó bị từ chối (bước đã đóng).
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID}); !errors.Is(err, ErrRunMismatch) {
		t.Fatalf("complete sau khi đóng: %v", err)
	}
}

// Subagent báo thất bại (--failed) ⇒ bước failed, tác vụ failed.
func TestSessionCompleteFailed(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, _ := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	c, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, Failed: "hết lượt"})
	if err != nil || c.Action != ActionFailed || c.Result == nil || c.Result.Status != "failed" || c.Result.Error != "hết lượt" {
		t.Fatalf("%+v %v", c, err)
	}
	if got := loadTask(t, h, tk.State.ID); got.State.PendingStep != nil || got.State.Status != task.StatusFailed {
		t.Fatalf("%+v", got.State)
	}
}

// Huỷ tác vụ có bước dở: gỡ bước dở và hợp đồng thuê.
func TestSessionCancelDropsPendingStep(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	if a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID}); err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	if _, _, err := r.SetStatus(ctx, tk.State.ID, task.StatusCancelled, TransitionOptions{Reason: "bỏ"}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, tk.State.ID); got.State.PendingStep != nil {
		t.Fatal("cancel phải gỡ bước dở")
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hợp đồng thuê còn sót: %v", err)
	}
	if _, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID}); err == nil {
		t.Fatal("next trên tác vụ đã huỷ phải lỗi")
	}
}

func TestChooseDriver(t *testing.T) {
	h := newHarness(t)
	e := h.runner().Env
	cases := []struct {
		flag     string
		flagOnly bool
		cfg      string
		mode     string
		reason   string
	}{
		{"", false, config.DriverProcess, task.DriverProcess, task.DriverReasonDefault},
		{"", false, config.DriverSession, task.DriverSession, task.DriverReasonConfig},
		{"process", false, config.DriverSession, task.DriverProcess, task.DriverReasonUser},
		{"session", false, config.DriverProcess, task.DriverSession, task.DriverReasonUser},
		{"auto", false, config.DriverSession, task.DriverSession, task.DriverReasonConfig},
		// `bint run` tự lập phiếu: chỉ nhận cờ tường minh, không lấy khoá cấu hình.
		{"", true, config.DriverSession, task.DriverProcess, task.DriverReasonDefault},
		{"session", true, config.DriverSession, task.DriverSession, task.DriverReasonUser},
	}
	for _, c := range cases {
		e.Cfg.Workflow.Driver = c.cfg
		got, err := e.chooseDriver(c.flag, c.flagOnly)
		if err != nil || got.Mode != c.mode || got.Reason != c.reason {
			t.Errorf("%+v: %+v %v", c, got, err)
		}
	}
	if _, err := e.chooseDriver("xyz", false); err == nil {
		t.Fatal("driver lạ phải lỗi")
	}
	if _, err := NormalizeDriverFlag("thread"); err == nil {
		t.Fatal("cờ lạ phải lỗi")
	}
}

// `bint run --driver session` lập phiếu rồi dừng; --driver cùng --task là lỗi.
func TestRunDriverSessionCreatesTaskAndStops(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	ctx := context.Background()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt", Driver: task.DriverSession})
	if err != nil || res.Status != task.StatusCreated || res.TaskID == "" || len(h.calls) != 0 ||
		!strings.Contains(strings.Join(res.Notes, " "), "bint run next "+res.TaskID) {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	if got := loadTask(t, h, res.TaskID); got.State.Classification.EffectiveDriver() != task.DriverSession {
		t.Fatalf("driver: %+v", got.State.Classification)
	}
	if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Driver: task.DriverSession}); err == nil ||
		!strings.Contains(err.Error(), "--driver") {
		t.Fatalf("--driver cùng --task: %v", err)
	}
	// workflow.driver: session KHÔNG làm `bint run` tự lập phiếu dừng (bản thân nó là driver process).
	h2 := newHarness(t)
	h2.cfg.Workflow.Driver = config.DriverSession
	res, err = h2.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("bint run với workflow.driver=session: %+v %v", res, err)
	}
}

// fakeLeaseClock đẩy đồng hồ của hợp đồng thuê (leaseNow) về phía trước; khôi phục khi test xong.
func fakeLeaseClock(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	var mu sync.Mutex
	var off time.Duration
	old := leaseNow
	leaseNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return time.Now().Add(off)
	}
	t.Cleanup(func() { leaseNow = old })
	return func(d time.Duration) {
		mu.Lock()
		off += d
		mu.Unlock()
	}
}

// complete thử lại được: tác vụ bị đưa về interrupted giữa chừng (Reconcile sau khi `complete` trước bị giết) mà bước dở còn
// nguyên thì `complete` cùng run_id tự tiếp tục và đóng nốt bước.
func TestSessionCompleteRetriesAfterInterruption(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	h.subagent(a)
	// Mô phỏng Reconcile: tác vụ đang in_progress bị đưa về interrupted, bước dở và hợp đồng thuê còn nguyên.
	if _, err := h.tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
		return t.State.Apply(task.StatusInterrupted, "bint", "tiến trình complete bị giết", task.Now(time.Now()))
	}); err != nil {
		t.Fatal(err)
	}
	c, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID})
	if err != nil || c.Action != "" || c.Result == nil || c.Result.Status != "completed" {
		t.Fatalf("complete lại: %+v %v", c, err)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.Status != task.StatusInReview || got.State.PendingStep != nil {
		t.Fatalf("sau complete lại: %s pending=%v", got.State.Status, got.State.PendingStep)
	}
	if _, err := os.Stat(h.leaseFile(tk.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hợp đồng thuê còn sót: %v", err)
	}
}

// Lỗi low của review G3a: bước còn hạn lúc claimSession kiểm nhưng hết hạn ngay trước pendingAction ⇒ trước đây Next trả
// (nil, nil) và CLI panic. Nay nhận quyền lại một lần, đóng bước như hết hạn và trả hành động dừng.
func TestSessionNextNeverReturnsNilWhenLeaseExpiresMidway(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	var first *SessionAction
	var firstErr error
	var armed bool
	var calls int
	old := leaseNow
	leaseNow = func() time.Time {
		if !armed {
			return time.Now()
		}
		calls++
		if calls == 1 { // lần kiểm trong closure của claimSession: còn hạn
			return time.Now()
		}
		return time.Now().Add(1000 * time.Hour) // pendingAction và lần nhận quyền thứ hai: đã hết hạn
	}
	t.Cleanup(func() { leaseNow = old; nextAfterLoad = nil })
	nextAfterLoad = func() {
		nextAfterLoad = nil
		first, firstErr = h.runner().Next(ctx, NextOptions{TaskRef: id})
		armed = true
	}
	second, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if firstErr != nil || first == nil || first.Action != ActionAgent {
		t.Fatalf("next thứ nhất: %+v %v", first, firstErr)
	}
	if err == nil && second == nil {
		t.Fatal("Next trả (nil, nil)")
	}
	if err != nil {
		t.Fatalf("Next lỗi: %v", err)
	}
	if second.Action == "" {
		t.Fatalf("hành động rỗng: %+v", second)
	}
	if got := loadTask(t, h, id); got.State.PendingStep != nil {
		t.Fatalf("bước hết hạn phải được đóng: %+v", got.State.PendingStep)
	}
}

// Phát hiện .1 (TOCTOU): next B nạp phiếu khi chưa có bước dở; giữa lúc đó next A mở bước và nhả quyền. B nhận quyền xong
// phải thấy bước còn hạn và trả lại ĐÚNG bước đó (cùng run_id), không đóng nó như hết hạn (tác vụ không failed).
func TestSessionNextSeesStepOpenedBetweenLoadAndClaim(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	var first *SessionAction
	var firstErr error
	nextAfterLoad = func() {
		nextAfterLoad = nil
		first, firstErr = h.runner().Next(ctx, NextOptions{TaskRef: id})
	}
	t.Cleanup(func() { nextAfterLoad = nil })
	second, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if firstErr != nil || first == nil || first.Action != ActionAgent {
		t.Fatalf("next thứ nhất: %+v %v", first, firstErr)
	}
	if err != nil || second.Action != ActionAgent || second.RunID != first.RunID || second.Step != first.Step || second.Prompt != first.Prompt {
		t.Fatalf("next thứ hai phải trả lại đúng bước của next thứ nhất: %+v %v", second, err)
	}
	got := loadTask(t, h, id)
	if got.State.Status == task.StatusFailed || got.State.PendingStep == nil || got.State.PendingStep.RunID != first.RunID {
		t.Fatalf("bước bị đóng nhầm: status=%s reason=%q pending=%+v", got.State.Status, got.State.Reason, got.State.PendingStep)
	}
	if _, err := os.Stat(h.leaseFile(id)); err != nil {
		t.Fatalf("hợp đồng thuê phải còn: %v", err)
	}
	if got.State.Runner != nil {
		t.Fatalf("next thứ hai phải nhả quyền điều phối: %+v", got.State.Runner)
	}
	// next thứ hai không được ghi đè lượt cha của next thứ nhất.
	if got.State.SessionRun == "" || got.State.PendingStep.ParentRunID != got.State.SessionRun {
		t.Fatalf("SessionRun %q lệch lượt cha của bước %q", got.State.SessionRun, got.State.PendingStep.ParentRunID)
	}
	// Bước vẫn đóng được bằng complete như thường.
	h.behave = featWrites(h, nil)
	h.subagent(first)
	if c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: first.RunID}); err != nil || c.Result == nil || c.Result.Status != "completed" {
		t.Fatalf("complete: %+v %v", c, err)
	}
}

// Phát hiện .1: `bint run` kiểu tiến trình nạp phiếu khi chưa có bước dở, `next` mở bước giữa chừng ⇒ Run trả
// PendingStepError, không chạy chồng lên bước subagent và không đóng nó.
func TestRunProcessSeesStepOpenedBetweenLoadAndClaim(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	var first *SessionAction
	var firstErr error
	runAfterLoad = func() {
		runAfterLoad = nil
		first, firstErr = h.runner().Next(ctx, NextOptions{TaskRef: id})
	}
	t.Cleanup(func() { runAfterLoad = nil })
	_, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id})
	if firstErr != nil || first == nil || first.Action != ActionAgent {
		t.Fatalf("next: %+v %v", first, firstErr)
	}
	var pe *PendingStepError
	if !errors.As(err, &pe) || pe.RunID != first.RunID {
		t.Fatalf("muốn PendingStepError lượt %s, được %v", first.RunID, err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("agent kiểu tiến trình không được chạy: %v", h.calls)
	}
	got := loadTask(t, h, id)
	if got.State.Status == task.StatusFailed || got.State.PendingStep == nil || got.State.PendingStep.RunID != first.RunID || got.State.Runner != nil {
		t.Fatalf("trạng thái: %s pending=%+v runner=%+v", got.State.Status, got.State.PendingStep, got.State.Runner)
	}
}

// Phát hiện .3: tác vụ blocked/interrupted mà bước dở còn hạn (complete bị ngắt) ⇒ next KHÔNG trả action agent (sẽ chạy
// lại subagent) mà chỉ lệnh complete/abort; kể cả với --resume. complete --run vẫn đóng được bước.
func TestSessionNextOnPausedTaskWithLivePendingStepDoesNotReturnAgent(t *testing.T) {
	for _, status := range []string{task.StatusInterrupted, task.StatusBlocked} {
		t.Run(status, func(t *testing.T) {
			h := newHarness(t)
			h.behave = featWrites(h, nil)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
			id := tk.State.ID
			ctx := context.Background()
			a, err := r.Next(ctx, NextOptions{TaskRef: id})
			if err != nil || a.Action != ActionAgent {
				t.Fatal(a, err)
			}
			h.subagent(a)
			if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
				return t.State.Apply(status, "bint", "complete bị ngắt", task.Now(time.Now()))
			}); err != nil {
				t.Fatal(err)
			}
			for _, resume := range []bool{false, true} {
				n, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: resume})
				if err != nil || n.Action != ActionBlocked || n.Prompt != "" || n.Subagent != "" {
					t.Fatalf("resume=%v: không được trả action agent: %+v %v", resume, n, err)
				}
				if !strings.Contains(n.Reason, "bint run complete "+id+" --run "+a.RunID) || !strings.Contains(n.Command, a.RunID) {
					t.Fatalf("resume=%v: phải chỉ lệnh complete: %+v", resume, n)
				}
			}
			got := loadTask(t, h, id)
			if got.State.PendingStep == nil || got.State.PendingStep.RunID != a.RunID || got.State.Status != status {
				t.Fatalf("next không được đổi gì: %s pending=%+v", got.State.Status, got.State.PendingStep)
			}
			c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID})
			if err != nil || c.Action != "" || c.Result == nil || c.Result.Status != "completed" {
				t.Fatalf("complete: %+v %v", c, err)
			}
		})
	}
}

// Phát hiện .4: subagent b-feat sửa tệp ngoài scope.allow ⇒ hành vi như driver process: mặc định chỉ ghi chú và đi
// tiếp; autonomy ask_always thì blocked chờ duyệt (PendingPaths), bước dở đã đóng.
func TestSessionFeatOutsideScopeAllow(t *testing.T) {
	outside := func(h *harness) func(context.Context, string, *Env, string) error {
		return featWrites(h, func(e *Env) error { return writeAt(e.Root, "other.txt", "ngoài phạm vi\n") })
	}
	t.Run("mặc định chỉ ghi chú", func(t *testing.T) {
		h := newHarness(t)
		h.behave = outside(h)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Plan: ptr(false), Edit: TicketEdit{Allow: []string{"app.txt", "done.txt"}}})
		ctx := context.Background()
		a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
		if err != nil || a.Action != ActionAgent || a.Step != StepFeat {
			t.Fatal(a, err)
		}
		h.subagent(a)
		c, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID})
		if err != nil || c.Action != "" || c.Result == nil || c.Result.Status != "completed" {
			t.Fatalf("complete: %+v %v", c, err)
		}
		if !slices.ContainsFunc(c.Notes, func(n string) bool { return strings.Contains(n, "sửa ngoài scope.allow: other.txt") }) {
			t.Fatalf("thiếu ghi chú ngoài scope.allow: %v", c.Notes)
		}
		got := loadTask(t, h, tk.State.ID)
		if got.State.Status != task.StatusInReview || len(got.State.PendingPaths) != 0 || got.State.PendingStep != nil {
			t.Fatalf("trạng thái: %s paths=%v pending=%v", got.State.Status, got.State.PendingPaths, got.State.PendingStep)
		}
	})
	t.Run("ask_always chờ duyệt", func(t *testing.T) {
		h := newHarness(t)
		h.behave = outside(h)
		r := h.runner()
		// Cho phép b-feat chạy; câu hỏi về tệp ngoài phạm vi thì không ai trả lời (như phiên không có người).
		r.Confirm = func(q Question) bool { return q.Key == QConfirmFeat }
		ask := task.AutonomyAskAlways
		tk := h.newSession(r, NewTaskOptions{Plan: ptr(false), Edit: TicketEdit{Allow: []string{"app.txt", "done.txt"}, Autonomy: &ask}})
		steps, last := h.drive(r, tk.State.ID, false)
		got := loadTask(t, h, tk.State.ID)
		if strings.Join(steps, ",") != "b-feat" || last.Action != ActionBlocked || !slices.Equal(got.State.PendingPaths, []string{"other.txt"}) ||
			got.State.PendingStep != nil || got.State.Runner != nil {
			t.Fatalf("bước %v, dừng %+v, paths=%v pending=%v", steps, last, got.State.PendingPaths, got.State.PendingStep)
		}
		// Chưa duyệt thì resume bị từ chối như protected path.
		if a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true}); err != nil || a.Action != ActionBlocked ||
			!strings.Contains(a.Reason, "chờ người duyệt") {
			t.Fatalf("resume chưa duyệt: %+v %v", a, err)
		}
	})
}
