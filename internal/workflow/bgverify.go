package workflow

import (
	"bytes"
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

// autoCheckQuiet là thời gian cây làm việc phải đứng yên trước khi Bint tự chạy kiểm hẹp; biến để kiểm thử rút ngắn. Bint tự chạy vì không thể trông
// agent gọi `bint task check` (đợt 7: lượt lõi không nhận chỉ dẫn, các lượt sau gọi nhưng sandbox chặn) và vì tiến trình driver nằm ngoài sandbox.
var autoCheckQuiet = 45 * time.Second

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

	// Kiểm hẹp tự động (chỉ goroutine thăm đọc/ghi, trừ checking).
	lastFP      string    // fingerprint cây ở lần thăm trước
	stableSince time.Time // từ lúc nào cây giữ fingerprint lastFP
	checkedFP   string    // fingerprint đã tự kiểm hẹp (hoặc đã có kết quả)
	checking    bool      // đang tự kiểm hẹp (giữ mu)
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
				b.autoCheck(time.Now()) // Bint tự kiểm hẹp khi cây đứng yên, không trông agent gọi `bint task check`
				b.tick("")
			}
		}
	}()
	return b
}

// autoCheck tự chạy kiểm hẹp khi cây làm việc đã đứng yên autoCheckQuiet và chưa có lần kiểm hẹp nào cho đúng cây đó. Một lần một lúc; kết quả vào
// State.Checks như lần agent gọi (agent gọi `bint task check` trên cây đã kiểm thì nhận lại kết quả này ngay). Kiểm hẹp xanh có ảnh chụp cây thì tick
// khởi chạy kiểm nền toàn repo.
func (b *bgVerifier) autoCheck(now time.Time) {
	ctx := b.runCtx
	if ctx.Err() != nil {
		return
	}
	fp := task.WorktreeFingerprint(ctx, b.root)
	if fp == "" {
		return
	}
	if fp != b.lastFP {
		b.lastFP, b.stableSince = fp, now
		return
	}
	if fp == b.checkedFP || now.Sub(b.stableSince) < autoCheckQuiet {
		return
	}
	if t, err := b.r.Tasks.Load(b.id); err == nil {
		if last := lastRanCheck(t); last != nil && last.Worktree == fp {
			b.checkedFP = fp // agent đã kiểm đúng cây này
			return
		}
	}
	b.mu.Lock()
	if b.checking {
		b.mu.Unlock()
		return
	}
	b.checking, b.checkedFP = true, fp
	b.wg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.wg.Done()
		b.r.check(ctx, b.id, checkAutoReason)
		b.mu.Lock()
		b.checking = false
		b.mu.Unlock()
	}()
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
	// Báo đỏ sớm (mục 1.4): gói test đầu tiên đỏ thì ghi ngay một kết quả tạm (Partial) để `bint task check` của agent thấy, không đợi cả lệnh xong
	// (đợt 7: gói đầu FAIL ở phút ~40 mà kết quả chỉ dùng ở phút 103). Lệnh vẫn chạy tới hết và ghi kết quả đầy đủ sau.
	early := &firstFail{on: func() {
		if ctx.Err() != nil {
			return
		}
		pv := task.Verification{At: r.at(), Command: cmd, Confirmed: true, ExitCode: 1, Log: log, Tree: tree, By: "bint", RunID: r.RunID,
			Reason: bgReason + " (tạm: đã có gói đỏ, lệnh còn chạy)", Scope: "full", Partial: true}
		r.Tasks.Update(b.id, r.now(), func(t *task.Task) error {
			t.State.BackgroundVerifications = append(t.State.BackgroundVerifications, pv)
			return nil
		})
	}}
	cr := eval.RunShellWatch(ctx, wt, cmd, time.Duration(r.Cfg.Workflow.VerifyTimeoutMinutes)*verifyTimeoutUnit, log, env, early)
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
	skipped, lastSkip := 0, ""
	for _, list := range [][]task.Verification{t.State.Checks, t.State.BackgroundVerifications} {
		for _, v := range list {
			at, err := time.Parse(time.RFC3339, v.At)
			if err != nil || at.Before(since.Truncate(time.Second)) {
				continue
			}
			if v.Skipped != "" {
				skipped, lastSkip = skipped+1, v.Skipped
				continue
			}
			if first.IsZero() || at.Before(first) {
				first = at
			}
		}
	}
	if first.IsZero() {
		if skipped > 0 {
			// Đợt 7: agent gọi 24 lần nhưng mọi lần bị bỏ qua; ghi chú cũ nói "không chạy" và bị đọc thành "agent bỏ qua chỉ dẫn".
			return fmt.Sprintf("luồng B′: kiểm hẹp được gọi %d lần nhưng lần nào cũng bị bỏ qua (lần cuối: %s) nên chưa có kết quả kiểm chứng sớm", skipped, lastSkip)
		}
		return "luồng B′: không có lần kiểm hẹp nào (agent không gọi, Bint không tự chạy) nên chưa có kết quả kiểm chứng sớm"
	}
	// Mẫu số là ngân sách thật: workflow.budget_minutes khi đặt (đợt 8 ghi "/ 180 phút" theo timeout_minutes dù ngân sách là 170), không thì timeout_minutes.
	mins, key := r.Cfg.Workflow.TimeoutMinutes, "workflow.timeout_minutes"
	if r.Cfg.Workflow.BudgetMinutes > 0 {
		mins, key = r.Cfg.Workflow.BudgetMinutes, "workflow.budget_minutes"
	}
	budget := time.Duration(mins) * time.Minute
	d := first.Sub(since.Truncate(time.Second))
	if budget <= 0 {
		return fmt.Sprintf("luồng B′: kết quả kiểm chứng đầu tiên sau %s", d)
	}
	return fmt.Sprintf("luồng B′: kết quả kiểm chứng đầu tiên ở %.0f %% ngân sách (%s / %d phút, %s)", float64(d)/float64(budget)*100, d, mins, key)
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

// firstFail gọi on đúng một lần khi đầu ra có dòng báo test đỏ của runner (Go: "--- FAIL:" hoặc "FAIL<tab><gói>"). Đọc theo dòng, chịu được dòng bị cắt
// giữa hai lần Write.
type firstFail struct {
	mu   sync.Mutex
	buf  []byte
	done bool
	on   func()
}

func (f *firstFail) Write(p []byte) (int, error) {
	f.mu.Lock()
	if f.done {
		f.mu.Unlock()
		return len(p), nil
	}
	f.buf = append(f.buf, p...)
	hit := false
	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(f.buf[:i]))
		f.buf = f.buf[i+1:]
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL\t") || strings.HasPrefix(line, "FAIL ") {
			hit = true
			break
		}
	}
	if len(f.buf) > 64<<10 {
		f.buf = f.buf[len(f.buf)-4096:]
	}
	if hit {
		f.done, f.buf = true, nil
	}
	f.mu.Unlock()
	if hit {
		f.on()
	}
	return len(p), nil
}
