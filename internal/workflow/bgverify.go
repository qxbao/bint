package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/gitrepo"
	"bint/internal/spool"
	"bint/internal/task"
)

// Kiểm chứng toàn repo chạy nền (luồng B′, giai đoạn 3). Trong lúc b-feat chạy, mỗi kiểm hẹp xanh mới (State.Checks) khởi chạy verify.command
// đầy đủ trên ảnh chụp cây của lần kiểm đó, trong worktree tạm; tối đa một lần chạy một lúc (kiểm hẹp xanh mới hơn khi đang chạy thì chờ,
// gộp về cây mới nhất). Sau b-feat, kết quả xanh trên đúng cây cuối thay lần kiểm chứng tuần tự (reuseBackground).

// bgPollInterval là chu kỳ thăm State.Checks; biến để kiểm thử rút ngắn.
var bgPollInterval = 5 * time.Second

// bgReason là Verification.Reason của lần kiểm nền.
const bgReason = "kiểm nền"

type bgVerifier struct {
	r    *Runner
	id   string
	root string

	pollCancel context.CancelFunc
	pollDone   chan struct{}
	runCtx     context.Context
	runCancel  context.CancelFunc

	mu      sync.Mutex
	tried   map[string]bool // cây đã (đang) kiểm nền
	running string          // cây đang chạy ("" = rảnh)
	wg      sync.WaitGroup
}

// startBackground bật kiểm nền cho bước implement của tác vụ B′; trả nil khi không áp dụng (classic, lệnh chưa xác nhận, thiếu lệnh).
func (r *Runner) startBackground(ctx context.Context, rs *runState) *bgVerifier {
	t := rs.t
	if !task.IsBPrime(t) || strings.TrimSpace(t.Ticket.Verify.Command) == "" || !r.VerifyConfirmed(t) && r.Cfg.Security.RequireApprovalForCommands {
		return nil
	}
	root, err := r.RootFor(t)
	if err != nil {
		return nil
	}
	b := &bgVerifier{r: r, id: t.State.ID, root: root, tried: map[string]bool{}, pollDone: make(chan struct{})}
	b.runCtx, b.runCancel = context.WithCancel(ctx)
	var pollCtx context.Context
	pollCtx, b.pollCancel = context.WithCancel(b.runCtx)
	go func() {
		defer close(b.pollDone)
		tk := time.NewTicker(bgPollInterval)
		defer tk.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-tk.C:
				b.tick("")
			}
		}
	}()
	return b
}

// tick khởi chạy kiểm nền cho kiểm hẹp xanh mới nhất chưa kiểm nền, nếu đang rảnh. only != "" thì chỉ khi cây đó là only.
func (b *bgVerifier) tick(only string) {
	t, err := b.r.Tasks.Load(b.id)
	if err != nil {
		return
	}
	tree := ""
	for i := len(t.State.Checks) - 1; i >= 0; i-- {
		if c := t.State.Checks[i]; c.Passed && c.Tree != "" {
			tree = c.Tree
			break
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if tree == "" || b.running != "" || b.tried[tree] || only != "" && tree != only || b.runCtx.Err() != nil {
		return
	}
	b.tried[tree], b.running = true, tree
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.run(t, tree)
		b.mu.Lock()
		b.running = ""
		b.mu.Unlock()
	}()
}

// run chạy verify.command trên cây tree trong worktree tạm và ghi kết quả.
func (b *bgVerifier) run(t *task.Task, tree string) {
	r, ctx := b.r, b.runCtx
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	commit, err := commitTree(ctx, b.root, tree, "bint kiểm nền")
	if err != nil {
		return
	}
	base := verifyTempBase(r.Tasks.Dir)
	if os.MkdirAll(base, 0o700) != nil {
		return
	}
	wt, err := os.MkdirTemp(base, "bg")
	if err != nil {
		return
	}
	if _, err := gitrepo.Run(ctx, b.root, "worktree", "add", "-q", "--detach", wt, commit); err != nil {
		removeAllForce(wt)
		return
	}
	defer func() {
		gitrepo.Run(context.Background(), b.root, "worktree", "remove", "--force", wt)
		removeAllForce(wt)
	}()
	log := t.LogPath(fmt.Sprintf("bg-%d.log", len(t.State.BackgroundVerifications)+1))
	env := eval.VerifyEnvFrom(verifyEnviron())
	if tmp, cleanup := r.verifyTempDir(); tmp != "" {
		defer cleanup()
		env = append(env, verifyTempEnv(tmp)...)
	}
	cr := eval.RunShell(ctx, wt, cmd, time.Duration(r.Cfg.Workflow.VerifyTimeoutMinutes)*verifyTimeoutUnit, log, env)
	if ctx.Err() != nil {
		return // bị huỷ (cây đã đổi hoặc người huỷ): không ghi kết quả dở
	}
	v := task.Verification{At: r.at(), Command: cmd, Confirmed: true, ExitCode: cr.ExitCode, Passed: cr.OK(), TimedOut: cr.TimedOut, Log: log,
		Tree: tree, By: "bint", RunID: r.RunID, Reason: bgReason, Scope: "full"}
	r.Tasks.Update(b.id, r.now(), func(t *task.Task) error {
		t.State.BackgroundVerifications = append(t.State.BackgroundVerifications, v)
		return nil
	})
}

// finish dừng thăm và chốt theo cây cuối của b-feat: lần chạy trên đúng cây cuối thì chờ xong, khác cây thì huỷ.
func (b *bgVerifier) finish(ctx context.Context, finalTree string) {
	b.pollCancel()
	<-b.pollDone
	b.tick(finalTree) // kiểm hẹp xanh cuối vừa ghi mà chưa tới chu kỳ thăm
	b.mu.Lock()
	if b.running != finalTree {
		b.runCancel()
	}
	b.mu.Unlock()
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		b.runCancel()
		<-done
	}
	b.runCancel()
}

// reuseBackground: có lần kiểm nền xanh trên đúng cây hiện tại thì ghi nó làm lần kiểm chứng đầy đủ (không chạy lại tuần tự).
func (r *Runner) reuseBackground(ctx context.Context, rs *runState) bool {
	t := rs.t
	if !task.IsBPrime(t) || len(t.State.BackgroundVerifications) == 0 {
		return false
	}
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err != nil || snap == "" {
		return false
	}
	var bg *task.Verification
	for i := len(t.State.BackgroundVerifications) - 1; i >= 0; i-- {
		if c := t.State.BackgroundVerifications[i]; c.Passed && c.Tree == snap && c.Command == strings.TrimSpace(t.Ticket.Verify.Command) {
			bg = &c
			break
		}
	}
	if bg == nil {
		return false
	}
	v := *bg
	v.Worktree, v.Reason, v.At = task.WorktreeFingerprint(ctx, r.Root), "kiểm chứng đầy đủ đã chạy nền trên đúng cây cuối ("+bg.At+")", r.at()
	nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
		t.State.Verifications = append(t.State.Verifications, v)
		return nil
	})
	if err != nil {
		return false
	}
	rs.t = nt
	r.pinGreen(ctx, r.Root, nt.State.ID, v)
	r.emit(nt, spool.TaskVerified, map[string]any{"command": v.Command, "passed": true, "confirmed": true, "exit_code": 0, "scope": "full", "reason": "background"})
	r.logf("  kiểm chứng: %s → qua (đã chạy nền trên đúng cây cuối)", v.Command)
	return true
}

// firstResultNote ghi chú thời điểm kết quả kiểm chứng đầu tiên của bước b-feat theo phần trăm ngân sách (workflow.timeout_minutes).
func (r *Runner) firstResultNote(t *task.Task, since time.Time) string {
	first := time.Time{}
	for _, list := range [][]task.Verification{t.State.Checks, t.State.BackgroundVerifications} {
		for _, v := range list {
			if at, err := time.Parse(time.RFC3339, v.At); err == nil && !at.Before(since.Truncate(time.Second)) && (first.IsZero() || at.Before(first)) {
				first = at
			}
		}
	}
	if first.IsZero() {
		return "luồng B′: b-feat không chạy kiểm hẹp theo mốc nên chưa có kết quả kiểm chứng sớm"
	}
	budget := time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute
	d := first.Sub(since.Truncate(time.Second))
	if budget <= 0 {
		return fmt.Sprintf("luồng B′: kết quả kiểm chứng đầu tiên sau %s", d)
	}
	return fmt.Sprintf("luồng B′: kết quả kiểm chứng đầu tiên ở %.0f %% ngân sách b-feat (%s / %d phút)", float64(d)/float64(budget)*100, d, r.Cfg.Workflow.TimeoutMinutes)
}

// endBackground chạy sau agent của bước implement: chốt kiểm nền theo cây cuối và ghi chú thời điểm kết quả sớm đầu tiên (chỉ luồng B′).
func (r *Runner) endBackground(ctx context.Context, rs *runState, sr *stepRun, bg *bgVerifier, since time.Time) {
	if sr.kind != flow.KindImplement || !task.IsBPrime(rs.t) {
		return
	}
	if bg != nil {
		final, _ := WorktreeSnapshot(ctx, bg.root)
		bg.finish(ctx, final)
	}
	if nt, err := r.Tasks.Load(rs.t.State.ID); err == nil {
		rs.t = nt
	}
	note := r.firstResultNote(rs.t, since)
	rs.res.Notes = append(rs.res.Notes, note)
	r.logf("  %s", note)
}
