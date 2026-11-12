package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"bint/internal/eval"
	adapters "bint/internal/harness"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/ulid"
)

// Driver session (docs/isolation-subagent-plan.md mục 6): phiên Claude Code đang mở (orchestrator) chạy từng bước
// bằng subagent, còn Bint giữ máy trạng thái và mọi cổng kiểm tra. `next` và `complete` là hai nửa của runStep
// (beginStep / endStep) cắt quanh lời gọi agent; nửa giữa nằm ở task.PendingStep giữa hai lệnh, vì tiến trình
// `bint run next` đã thoát và flock không còn được giữ (xem hợp đồng thuê ở lease.go).
//
//	bint run next <id> --json      → {action: agent|process|done|blocked|failed, …}
//	  Agent(subagent_type, model, prompt)        ← orchestrator chạy subagent
//	bint run complete <id> --run <run_id> [--agent-id <agentId>] [--failed "<lý do>"] --json
//	bint run abort <id>            ← phiên orchestrator chết giữa bước: gỡ bước dở và hợp đồng thuê
//
// Driver session luôn điều phối theo workflow b-do (Classify → b-req → b-feat → b-review → Finalize).

// Giá trị của SessionAction.Action.
const (
	ActionAgent   = "agent"   // chạy subagent với prompt này rồi `bint run complete`
	ActionProcess = "process" // role gán harness khác Claude: chạy lệnh Command (bint run <bước> --task), rồi `bint run next`
	ActionDone    = "done"
	ActionBlocked = "blocked" // chờ người (xem Reason, Questions); người xử lý xong thì `bint run next --resume`
	ActionFailed  = "failed"
)

// Tên subagent do Bint cài theo role (tệp định nghĩa do `bint init` sinh, phiếu G3b).
const (
	SubagentPlanner     = adapters.SubagentPlanner
	SubagentImplementer = adapters.SubagentImplementer
	SubagentReviewer    = adapters.SubagentReviewer
)

// SubagentFor trả tên subagent của bước ("" nếu bước không chạy bằng subagent).
func SubagentFor(step string) string {
	switch adapters.StepRole(step) {
	case "planner":
		return SubagentPlanner
	case "implementer":
		return SubagentImplementer
	case "reviewer":
		return SubagentReviewer
	}
	return ""
}

// ErrRunMismatch: `complete --run` không khớp bước đang dở của tác vụ (hoặc tác vụ không có bước dở).
var ErrRunMismatch = errors.New("lượt chạy không khớp bước đang dở của tác vụ")

// PendingStepError: tác vụ có bước đang chạy bằng subagent còn hạn nên `bint run` kiểu tiến trình không chạy được.
type PendingStepError struct {
	TaskID, Step, RunID, Until string
}

func (e *PendingStepError) Error() string {
	return fmt.Sprintf("tác vụ %s có bước %s đang chạy bằng subagent (lượt %s, hết hạn %s): `bint run complete %s --run %s` khi subagent xong, "+
		"hoặc `bint run abort %s` để bỏ bước đó", e.TaskID, e.Step, e.RunID, e.Until, e.TaskID, e.RunID, e.TaskID)
}

// SessionAction là kết quả của `bint run next|complete|abort` (JSON snake_case, mọi trường tuỳ chọn trừ task_id).
type SessionAction struct {
	// Action: agent | process | done | blocked | failed. Rỗng ở `complete` khi tác vụ còn tiếp tục (gọi `next`).
	Action string `json:"action,omitempty"`
	TaskID string `json:"task_id"`
	Status string `json:"task_status,omitempty"`
	// Các trường của action "agent".
	Step       string `json:"step,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	Subagent   string `json:"subagent,omitempty"`
	Model      string `json:"model,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	ReadOnly   bool   `json:"readonly,omitempty"`
	MaxTurns   *int   `json:"max_turns,omitempty"`
	LeaseUntil string `json:"lease_until,omitempty"`
	// Command của action "process" (hoặc lệnh kế tiếp ở `complete` khi tác vụ còn tiếp tục).
	Command   string   `json:"command,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Questions []string `json:"questions,omitempty"`
	Notes     []string `json:"notes,omitempty"`
	// PlanningIssues: lỗi cấu trúc artifact b-req (hợp đồng v1) khiến tác vụ dừng.
	PlanningIssues []task.ArtifactIssue `json:"planning_issues,omitempty"`
	// Result: kết quả bước vừa đóng (chỉ ở `complete`).
	Result *StepResult `json:"result,omitempty"`
}

// NextOptions điều khiển Runner.Next.
type NextOptions struct {
	TaskRef string
	// Resume: tiếp tục tác vụ blocked/interrupted/failed (người đã xử lý nguyên nhân).
	Resume bool
	// NoWait: khoá worktree đang bị giữ thì dừng ngay (blocked) thay vì chờ tới workflow.timeout_minutes.
	NoWait bool
}

// CompleteOptions điều khiển Runner.Complete.
type CompleteOptions struct {
	TaskRef string
	// RunID phải khớp lượt của bước đang dở (SessionAction.RunID của `next`).
	RunID string
	// AgentID là agentId của subagent (kết quả công cụ Agent): lưu vào lượt chạy (ghép sự kiện spool theo agent_id) và
	// dùng tìm transcript để tính chi phí (session_cost.go).
	AgentID string
	// Failed khác rỗng: subagent báo thất bại (lý do); bước bị đánh failed.
	Failed string
	NoWait bool
}

// DriverChoice là driver được chọn lúc lập phiếu.
type DriverChoice struct {
	Mode   string // task.DriverProcess | task.DriverSession
	Reason string // task.DriverReason*
}

// NormalizeDriverFlag chuẩn hoá giá trị cờ --driver: ""/auto → ""; process|session giữ nguyên.
func NormalizeDriverFlag(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return "", nil
	case task.DriverProcess:
		return task.DriverProcess, nil
	case task.DriverSession:
		return task.DriverSession, nil
	}
	return "", fmt.Errorf("driver %q không hợp lệ (process | session)", s)
}

// chooseDriver chọn driver cho phiếu mới: cờ tường minh > workflow.driver > process. flagOnly bỏ qua cấu hình.
func (e *Env) chooseDriver(flag string, flagOnly bool) (DriverChoice, error) {
	flag, err := NormalizeDriverFlag(flag)
	if err != nil {
		return DriverChoice{}, err
	}
	switch {
	case flag != "":
		return DriverChoice{Mode: flag, Reason: task.DriverReasonUser}, nil
	case !flagOnly && e.Cfg.Workflow.Driver == "session":
		return DriverChoice{Mode: task.DriverSession, Reason: task.DriverReasonConfig}, nil
	}
	return DriverChoice{Mode: task.DriverProcess, Reason: task.DriverReasonDefault}, nil
}

// sessionEnv đổi Env của Runner sang bản sao cho một lệnh (như Run) và trả hàm khôi phục.
func (r *Runner) sessionEnv() (restore func()) {
	orig := r.Env
	cp := *orig
	r.Env = &cp
	r.Source = "bint-run"
	return func() { r.Env = orig }
}

// sessionState dựng runState cho một lệnh session. Driver session chỉ điều phối b-do. Ngân sách của b-do và bộ
// đếm lượt sống trong một tiến trình nên mỗi lệnh bắt đầu từ 0 (chưa trừ phần đã dùng ở bước trước).
func (r *Runner) sessionState(t *task.Task, runID string, resume, noWait bool) *runState {
	rs := &runState{opts: RunOptions{Workflow: StepDo, TaskRef: t.State.ID, Resume: resume, NoWait: noWait},
		t: t, runID: runID, res: &Result{TaskID: t.State.ID, RunID: runID, Steps: []StepResult{}}, ran: map[string]int{},
		limit: r.Cfg.Limits.ForWorkflow(StepDo), session: true}
	return rs
}

// SessionStep dựng hành động agent từ bước đang dở (gọi lại an toàn: trả đúng prompt đã dựng).
func sessionAgentAction(t *task.Task, ps *task.PendingStep) *SessionAction {
	a := &SessionAction{Action: ActionAgent, TaskID: t.State.ID, Status: t.State.Status, Step: ps.Step, RunID: ps.RunID,
		Subagent: SubagentFor(ps.Step), Model: ps.Model, Prompt: ps.Prompt, Worktree: t.State.Worktree,
		ReadOnly: readonlySteps[ps.Step], MaxTurns: ps.MaxTurns, LeaseUntil: ps.LeaseUntil}
	return a
}

// stopAction dựng hành động dừng (done | blocked | failed) từ trạng thái tác vụ hiện tại.
func (r *Runner) stopAction(rs *runState) *SessionAction {
	st := rs.t.State
	a := &SessionAction{TaskID: st.ID, Status: st.Status, Reason: st.Reason, Questions: rs.res.Questions,
		Notes: rs.res.Notes, PlanningIssues: rs.res.PlanningIssues}
	switch st.Status {
	case task.StatusDone:
		a.Action = ActionDone
	case task.StatusFailed:
		a.Action = ActionFailed
	default:
		a.Action = ActionBlocked
		if a.Reason == "" {
			a.Reason = rs.res.Reason
		}
	}
	return a
}

// claimSession nhận quyền điều phối tác vụ cho một lệnh session và, nếu open, mở lượt cha mới khi cần. Lượt cha
// (State.SessionRun) mở ở `next` đầu tiên và đóng khi tác vụ dừng; tác vụ đang dừng (paused) thì lượt cũ đã đóng nên
// `next` mở lượt mới. `complete`/`abort` (open=false) dùng lại lượt đang có. Trả lỗi (BusyError) nếu tiến trình điều
// phối khác đang sống.
func (r *Runner) claimSession(rs *runState, open bool) error {
	id := rs.t.State.ID
	var run, parent string
	var spend *task.SessionSpend
	opened := false
	nt, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		// Mọi quyết định dựa trên bản phiếu nạp dưới khoá, không dựa trên bản nạp trước đó: giữa hai lần nạp một `next`
		// khác có thể đã mở bước, ghi PendingStep và nhả quyền (TOCTOU). Bước dở còn hạn thì không nhận quyền để đóng nó.
		if open && t.State.PendingActive(leaseNow()) {
			return errPendingActive
		}
		run, opened = t.State.SessionRun, false
		switch {
		case open && (run == "" || task.Paused(t.State.Status)):
			run, opened = ulid.New(), true
		case run == "":
			if ps := t.State.PendingStep; ps != nil && ps.ParentRunID != "" {
				run = ps.ParentRunID
			} else {
				run = ulid.New()
			}
		}
		parent = t.State.LastRun()
		spend = nil
		if sp := t.State.SessionSpend; sp != nil && sp.RunID == run {
			c := *sp
			spend = &c
		}
		if err := t.ClaimRunner(run, r.now()); err != nil {
			return err
		}
		if opened || t.State.SessionRun == "" {
			t.State.SessionRun = run
			t.State.LinkRun(run)
		}
		return nil
	})
	if err != nil {
		if nt != nil {
			rs.t = nt // bản mới nhất, để người gọi xử lý errPendingActive theo đúng trạng thái hiện tại
		}
		return err
	}
	rs.t, rs.runID, rs.res.RunID = nt, run, run
	if spend != nil { // ngân sách b-do đã dùng ở các lệnh trước của cùng lượt cha
		rs.spent, rs.turns, rs.costMissing, rs.costKnown = spend.Cost, spend.Turns, spend.CostUnknown, spend.Cost > 0
	}
	r.RunID = run
	if opened {
		r.emitRun(run, spool.WorkflowStarted, map[string]any{"workflow": StepDo, "parent_run_id": parent, "driver": task.DriverSession,
			"bint_version": r.Version, "task_id": nt.State.ID})
	}
	return nil
}

// errPendingActive: claimSession(open) thấy bước dở còn hạn (do một `next` khác vừa mở) nên không nhận quyền.
var errPendingActive = errors.New("tác vụ có bước dở còn hạn")

// pendingAction là hành động cho tác vụ có bước dở còn hạn (nil nếu không có). Tác vụ đang chạy: trả lại đúng bước
// (gọi lại an toàn). Tác vụ đang dừng (blocked/interrupted vì `complete` bị ngắt hoặc khoá bị chặn): bước và hợp đồng
// còn nguyên, subagent đã xong việc — KHÔNG trả action agent (orchestrator sẽ chạy lại subagent) mà chỉ lệnh đóng bước.
func pendingAction(t *task.Task) *SessionAction {
	ps := t.State.PendingStep
	if ps == nil || !t.State.PendingActive(leaseNow()) {
		return nil
	}
	if !task.Paused(t.State.Status) {
		return sessionAgentAction(t, ps)
	}
	id := t.State.ID
	return &SessionAction{Action: ActionBlocked, TaskID: id, Status: t.State.Status, Step: ps.Step, RunID: ps.RunID,
		LeaseUntil: ps.LeaseUntil, Command: "bint run complete " + id + " --run " + ps.RunID + " --json",
		Reason: fmt.Sprintf("tác vụ %s đang %s (%s) nhưng bước %s (lượt %s) vẫn còn hạn tới %s: KHÔNG chạy lại subagent; "+
			"gọi `bint run complete %s --run %s` để đóng bước, hoặc `bint run abort %s` để bỏ nó",
			id, t.State.Status, t.State.Reason, ps.Step, ps.RunID, ps.LeaseUntil, id, ps.RunID, id)}
}

// releaseSession nhả quyền điều phối sau một lệnh mà tác vụ còn tiếp tục (không đóng lượt cha).
func (r *Runner) releaseSession(rs *runState) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		if t.State.Runner != nil && t.State.Runner.RunID == rs.runID {
			t.State.Runner = nil
		}
		return nil
	}); err == nil {
		rs.t = t
	}
}

// endSession đóng lượt cha khi tác vụ dừng (done/blocked/failed/interrupted): nhả quyền, ghi workflow.finished và
// xoá State.SessionRun. Trả hành động dừng.
func (r *Runner) endSession(rs *runState) *SessionAction {
	r.finish(rs)
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		if t.State.SessionRun == rs.runID {
			t.State.SessionRun = ""
		}
		return nil
	}); err == nil {
		rs.t = t
	}
	return r.stopAction(rs)
}

// pendingReview trả vòng review mới nhất khi nó chưa được áp quyết định (review chạy riêng bằng tiến trình).
func pendingReview(t *task.Task) *task.ReviewRound {
	if t.State.Status != task.StatusInReview || t.State.AwaitingLand || t.State.Landed != nil {
		return nil
	}
	rev, err := t.LoadReview()
	if err != nil {
		return nil
	}
	if last := rev.Last(); last != nil && last.Round > t.State.ReviewDecided {
		return last
	}
	return nil
}

// nextAfterLoad, runAfterLoad: chỉ để kiểm thử — chạy giữa lần nạp phiếu và lần nhận quyền điều phối (khe TOCTOU).
var nextAfterLoad, runAfterLoad func()

// Next trả hành động kế tiếp của tác vụ (driver session). Bước agent: mở bước (chuyển trạng thái, giới hạn, khoá,
// ảnh chụp, prompt), ghi State.PendingStep và hợp đồng thuê rồi trả flock — gọi lại khi bước còn hạn trả đúng
// bước đó, không mở bước mới. Bước gán harness khác Claude trả `process`. Phần việc không cần agent (kiểm phạm vi và
// kiểm chứng sau b-feat, quyết định sau review chạy riêng, finalize) chạy ngay trong lệnh này.
func (r *Runner) Next(ctx context.Context, o NextOptions) (*SessionAction, error) {
	defer r.sessionEnv()()
	t, err := r.Tasks.Load(o.TaskRef)
	if err != nil {
		return nil, err
	}
	t = r.Reconcile(t)
	id := t.State.ID
	switch t.State.Status {
	case task.StatusDone:
		return &SessionAction{Action: ActionDone, TaskID: id, Status: t.State.Status, Reason: t.State.Reason}, nil
	case task.StatusCancelled:
		return nil, fmt.Errorf("tác vụ %s đã %s", id, t.State.Status)
	}
	// Bước còn hạn: trả lại đúng nó (gọi lại an toàn); tác vụ đang dừng thì chỉ lệnh đóng bước.
	if a := pendingAction(t); a != nil {
		return a, nil
	}
	if nextAfterLoad != nil {
		nextAfterLoad()
	}
	if task.Paused(t.State.Status) {
		rs := r.sessionState(t, "", false, o.NoWait)
		rs.res.Reason = t.State.Reason
		switch {
		case !o.Resume:
			a := r.stopAction(rs)
			if t.State.PendingStep == nil {
				a.Notes = append(a.Notes, "tiếp tục sau khi xử lý nguyên nhân: bint run next "+id+" --resume")
			}
			return a, nil
		case len(t.State.PendingPaths) > 0:
			a := r.stopAction(rs)
			a.Action = ActionBlocked
			a.Reason = fmt.Sprintf("tác vụ %s có thay đổi chờ người duyệt: %s (bint task approve %s)", id, strings.Join(t.State.PendingPaths, ", "), id)
			return a, nil
		}
	}
	rs := r.sessionState(t, "", o.Resume, o.NoWait)
	if err := r.claimSession(rs, true); err != nil {
		if !errors.Is(err, errPendingActive) {
			return nil, err
		}
		// Một `next` khác vừa mở bước giữa lúc nạp và nhận quyền: trả lại đúng bước đó, không đóng nó như hết hạn.
		if a := pendingAction(rs.t); a != nil {
			return a, nil
		}
		// Bước còn hạn lúc claimSession kiểm nhưng hết hạn ngay trước pendingAction: không có gì để trả lại, nên nhận quyền
		// lại một lần — lần này bước đã hết hạn và đi tiếp nhánh "bước dở đã hết hạn" bên dưới. Không bao giờ trả (nil, nil).
		if err := r.claimSession(rs, true); err != nil {
			if errors.Is(err, errPendingActive) {
				return nil, fmt.Errorf("tác vụ %s: bước dở đổi trạng thái liên tục khi nhận quyền điều phối, thử lại `bint run next %s`: %w", id, id, err)
			}
			return nil, err
		}
	}
	stopped := false
	defer func() {
		if !stopped {
			r.releaseSession(rs)
		}
	}()
	stop := func() (*SessionAction, error) {
		stopped = true
		return r.endSession(rs), nil
	}

	if task.Paused(rs.t.State.Status) { // chỉ tới đây khi o.Resume
		to := rs.t.State.ResumeFrom
		if to == "" {
			to = task.StatusCreated
		}
		if to == task.StatusInReview {
			// Người đã xem phát hiện review lúc tác vụ dừng; resume không áp lại quyết định cũ (sẽ dừng lại y hệt).
			if rev, err := rs.t.LoadReview(); err == nil {
				if last := rev.Last(); last != nil {
					if t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
						t.State.ReviewDecided = max(t.State.ReviewDecided, last.Round)
						return nil
					}); err == nil {
						rs.t = t
					}
				}
			}
		}
		if err := r.transition(rs, to, "tiếp tục (bint run next --resume)"); err != nil {
			return stop()
		}
	}
	// Bước dở đã hết hạn (claimSession đã loại bước còn hạn dưới khoá phiếu): đóng như bước quá giờ (vẫn kiểm chỉ đọc để báo vi phạm).
	if ps := rs.t.State.PendingStep; ps != nil {
		r.closePending(ctx, rs, ps, closeArgs{reason: "hết hạn thuê khoá, không có lệnh complete", timedOut: true})
		if t, err := r.Tasks.Load(id); err == nil {
			rs.t = t
		}
		return stop()
	}
	taskRoot, rootErr := r.RootFor(rs.t)
	if rootErr == nil && taskRoot != r.Root {
		r.Env.Root = taskRoot
	}
	if rootErr != nil {
		r.fail(rs, "", "thư mục làm việc của tác vụ không còn: "+rootErr.Error())
		return stop()
	}
	if !r.ensureWorktree(ctx, rs) {
		return stop()
	}
	if a := r.Assess(rs.t); len(a.Questions) > 0 && strings.TrimSpace(rs.t.Ticket.Verify.Command) == "" {
		if !r.askVerify(rs, a) {
			return stop()
		}
	}

	for guard := 0; guard < 4+3*(r.Cfg.Workflow.MaxReviewRounds+1); guard++ {
		if ctx.Err() != nil {
			r.interrupt(rs, "người dùng huỷ")
			return stop()
		}
		if last := pendingReview(rs.t); last != nil {
			// Review chạy riêng bằng tiến trình (role reviewer gán harness khác) đã ghi kết quả nhưng chưa ai áp quyết định.
			if !r.reviewDecision(ctx, rs, last) {
				return stop()
			}
			continue
		}
		step := r.nextStep(rs)
		if step == "" {
			return stop()
		}
		if step == stepCheck {
			// b-feat đã xong (kể cả chạy riêng bằng tiến trình); chỉ còn kiểm phạm vi và kiểm chứng.
			if !r.afterStep(ctx, rs, StepFeat) {
				return stop()
			}
			continue
		}
		h, _, err := adapters.ForStep(r.Cfg, adapters.Selection{}, step)
		if err != nil {
			r.fail(rs, step, err.Error())
			return stop()
		}
		if h != "claude" {
			// Role gán harness khác Claude: không có subagent cho nó, bước chạy kiểu tiến trình.
			a := &SessionAction{Action: ActionProcess, TaskID: id, Status: rs.t.State.Status, Step: step, Worktree: rs.t.State.Worktree,
				Command: processCommand(step, id), Notes: []string{fmt.Sprintf("role %s gán harness %s: chạy lệnh này rồi `bint run next %s --json`",
					adapters.StepRole(step), h, id)}}
			return a, nil
		}
		sr, ok := r.beginStep(ctx, rs, step, true)
		if !ok {
			return stop()
		}
		ps := r.pendingFrom(rs, sr)
		if err := r.grantPending(rs, sr, ps); err != nil {
			r.fail(rs, step, "ghi bước dở và hợp đồng thuê khoá: "+err.Error())
			return stop()
		}
		sr.release() // trả flock: từ đây hợp đồng thuê giữ chỗ
		return sessionAgentAction(rs.t, ps), nil
	}
	r.fail(rs, "", "vòng điều phối không dừng (bất thường)")
	return stop()
}

func processCommand(step, id string) string { return "bint run " + step + " --task " + id + " --json" }

// pendingFrom dựng PendingStep từ bước vừa mở.
func (r *Runner) pendingFrom(rs *runState, sr *stepRun) *task.PendingStep {
	now := leaseNow()
	until := now.Add(time.Duration(r.Cfg.Workflow.TimeoutMinutes) * lockWaitUnit)
	return &task.PendingStep{RunID: sr.child, ParentRunID: rs.runID, Step: sr.step, Mode: leaseModeName(sr.mode),
		Snapshot: sr.snapBefore, FeatBefore: sr.featBefore, FirstFeat: sr.firstFeat, Harness: sr.harness, Model: sr.reqModel,
		Subagent: SubagentFor(sr.step), Prompt: sr.prompt, MaxTurns: sr.maxTurns,
		StartedAt: sr.started.UTC().Format(time.RFC3339Nano), LeaseUntil: until.UTC().Format(time.RFC3339Nano)}
}

// grantPending ghi hợp đồng thuê (đang giữ flock của bước) rồi bước dở vào phiếu. Lỗi thì gỡ cả hai và nhả khoá.
func (r *Runner) grantPending(rs *runState, sr *stepRun, ps *task.PendingStep) error {
	id := rs.t.State.ID
	err := grantLease(r.Tasks.Dir, r.Root, leaseEntry{Task: id, Step: ps.Step, RunID: ps.RunID, Mode: ps.Mode,
		At: ps.StartedAt, Until: ps.LeaseUntil})
	if err == nil {
		var nt *task.Task
		nt, err = r.Tasks.Update(id, r.now(), func(t *task.Task) error { t.State.PendingStep = ps; return nil })
		if err == nil {
			rs.t = nt
			return nil
		}
		releaseLease(r.Tasks.Dir, r.Root, id, ps.RunID, false)
	}
	sr.release()
	return err
}

// closeArgs là thông tin đóng một bước dở.
type closeArgs struct {
	failed  string // lý do subagent báo thất bại
	agentID string
	// timedOut: bước quá hạn thuê. reason ghi vào kết quả khi timedOut.
	timedOut bool
	reason   string
}

// closePending là nửa sau của bước dở: lấy lại khoá đúng chế độ của bước, kiểm chỉ đọc, ghi kết quả (endStep), rồi gỡ
// bước dở và hợp đồng thuê (trong lúc còn giữ khoá). Dùng chung cho Complete, Next và Run khi bước hết hạn. Trả
// true nếu bước hoàn thành (afterStep còn việc); false thì trạng thái tác vụ đã đặt.
func (r *Runner) closePending(ctx context.Context, rs *runState, ps *task.PendingStep, a closeArgs) bool {
	mode := LockExclusive
	if ps.Mode == leaseModeName(LockShared) {
		mode = LockShared
	}
	lock, ok := r.lockStep(ctx, rs, ps.Step, mode)
	if !ok {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, ps.StartedAt)
	if err != nil {
		started = leaseNow()
	}
	id := rs.t.State.ID
	sr := &stepRun{step: ps.Step, child: ps.RunID, stepID: "st_" + ps.RunID, readOnly: readonlySteps[ps.Step], mode: mode,
		snapBefore: ps.Snapshot, featBefore: ps.FeatBefore, firstFeat: ps.FirstFeat, started: started, maxTurns: ps.MaxTurns,
		reqModel: ps.Model, harness: ps.Harness, stepLog: rs.t.LogPath(ps.Step + "-" + ps.RunID + ".log"), lock: lock,
		agentID: a.agentID, timedOut: a.timedOut}
	root := r.Root
	sr.onEnd = func() {
		r.Tasks.Update(id, r.now(), func(t *task.Task) error {
			if t.State.PendingStep != nil && t.State.PendingStep.RunID == ps.RunID {
				t.State.PendingStep = nil
			}
			return nil
		})
		releaseLease(r.Tasks.Dir, root, id, ps.RunID, false)
	}
	res := eval.AgentResult{Duration: max(leaseNow().Sub(started), 0), TimedOut: a.timedOut, Model: ps.Model}
	switch {
	case a.failed != "":
		res.ExitCode, res.Error = 1, a.failed
	case a.timedOut && a.reason != "":
		res.Error = a.reason
	}
	r.fillSubagentUsage(rs, &res, ps.Step, a.agentID)
	ok = r.endStep(ctx, rs, sr, res)
	r.saveSessionSpend(rs)
	return ok
}

// Complete đóng bước đang dở: subagent đã xong (hoặc báo thất bại). Làm phần sau của bước như `bint run` (kiểm chỉ
// đọc, phạm vi, kiểm chứng, tài liệu, quyết định sau review). Không mở bước mới: tác vụ còn tiếp tục thì Action
// rỗng, gọi `bint run next`. run_id sai (hoặc không có bước dở) ⇒ ErrRunMismatch, không đổi gì.
func (r *Runner) Complete(ctx context.Context, o CompleteOptions) (*SessionAction, error) {
	defer r.sessionEnv()()
	t, err := r.Tasks.Load(o.TaskRef)
	if err != nil {
		return nil, err
	}
	t = r.Reconcile(t)
	id := t.State.ID
	ps := t.State.PendingStep
	if ps == nil || o.RunID == "" || ps.RunID != o.RunID {
		have := "tác vụ không có bước dở"
		if ps != nil {
			have = "bước dở là lượt " + ps.RunID
		}
		return nil, fmt.Errorf("%w: --run %q (%s)", ErrRunMismatch, o.RunID, have)
	}
	root, rerr := r.RootFor(t)
	if rerr != nil {
		return nil, fmt.Errorf("thư mục làm việc của tác vụ không còn: %w", rerr)
	}
	r.Env.Root = root
	rs := r.sessionState(t, "", false, o.NoWait)
	if err := r.claimSession(rs, false); err != nil {
		return nil, err
	}
	stopped := false
	defer func() {
		if !stopped {
			r.releaseSession(rs)
		}
	}()
	stop := func() (*SessionAction, error) {
		stopped = true
		return r.endSession(rs), nil
	}
	if task.Paused(rs.t.State.Status) {
		// Lệnh complete trước đó bị ngắt giữa chừng (hoặc khoá bị chặn): bước vẫn dở nên tiếp tục đóng nó.
		to := rs.t.State.ResumeFrom
		if to == "" {
			to = task.StatusInProgress
		}
		if err := r.transition(rs, to, "tiếp tục (bint run complete)"); err != nil {
			return stop()
		}
	}
	if o.AgentID != "" {
		if nt, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
			for _, ar := range t.State.AgentRuns {
				if ar.AgentID == o.AgentID && ar.RunID == ps.RunID {
					return nil
				}
			}
			t.State.AgentRuns = append(t.State.AgentRuns, task.AgentRun{AgentID: o.AgentID, RunID: ps.RunID})
			return nil
		}); err == nil {
			rs.t = nt
		}
	}
	expired := !rs.t.State.PendingActive(leaseNow())
	ok := r.closePending(ctx, rs, ps, closeArgs{failed: strings.TrimSpace(o.Failed), agentID: o.AgentID, timedOut: expired})
	if t, err := r.Tasks.Load(id); err == nil {
		rs.t = t
	}
	if ok {
		ok = r.afterStep(ctx, rs, ps.Step)
	}
	var result *StepResult
	if n := len(rs.res.Steps); n > 0 {
		result = &rs.res.Steps[n-1]
	}
	if !ok {
		a, _ := stop()
		a.Result = result
		return a, nil
	}
	a := &SessionAction{TaskID: id, Status: rs.t.State.Status, Notes: rs.res.Notes, Result: result,
		Command: "bint run next " + id + " --json"}
	a.Notes = append(a.Notes, "gọi `bint run next "+id+" --json` để lấy bước tiếp theo")
	if o.AgentID == "" {
		a.Notes = append(a.Notes, "thiếu --agent-id: sự kiện của subagent không gắn được vào lượt chạy này")
	}
	return a, nil
}

// Abort bỏ bước đang dở (phiên orchestrator chết hoặc người muốn dừng): xoá State.PendingStep và hợp đồng thuê,
// đóng lượt con của bước là interrupted, đưa tác vụ về interrupted và đóng lượt cha. Tiếp tục bằng
// `bint run next <id> --resume` (bước dở chạy lại từ đầu).
func (r *Runner) Abort(ctx context.Context, taskRef, reason string) (*SessionAction, error) {
	defer r.sessionEnv()()
	t, err := r.Tasks.Load(taskRef)
	if err != nil {
		return nil, err
	}
	t = r.Reconcile(t)
	id := t.State.ID
	if task.Terminal(t.State.Status) {
		return nil, fmt.Errorf("tác vụ %s đã %s", id, t.State.Status)
	}
	if root, rerr := r.RootFor(t); rerr == nil {
		r.Env.Root = root
	} else if t.State.Worktree != "" {
		r.Env.Root = t.State.Worktree // chỉ để tìm tệp khoá/hợp đồng của worktree đã mất
	}
	rs := r.sessionState(t, "", false, true)
	if err := r.claimSession(rs, false); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "người dùng chạy bint run abort"
	}
	notes := r.discardPending(ctx, rs, reason)
	if task.Active(rs.t.State.Status) {
		r.interrupt(rs, "bint run abort: "+reason)
	}
	rs.res.Notes = append(rs.res.Notes, notes...)
	a := r.endSession(rs)
	if len(notes) == 0 && t.State.PendingStep == nil {
		a.Notes = append(a.Notes, "tác vụ không có bước dở")
	}
	a.Action = "" // abort không đưa ra hành động tiếp theo
	return a, nil
}

// otherPendingOnRoot liệt kê tác vụ khác chưa kết thúc trên cùng cây làm việc còn bước dở của driver session (tức có thể
// đang giữ hợp đồng thuê trong tệp khoá của cây này).
func (e *Env) otherPendingOnRoot(id, root string) []string {
	all, _ := e.Tasks.List()
	var out []string
	for _, o := range all {
		if o.State.ID == id || task.Terminal(o.State.Status) || o.State.PendingStep == nil || o.State.Worktree == "" ||
			!samePath(o.State.Worktree, root) {
			continue
		}
		out = append(out, o.State.ID)
	}
	return out
}

// discardPending gỡ bước đang dở của tác vụ (nếu có): ghi nhận tệp b-feat đã đổi, đóng lượt con, xoá PendingStep và hợp
// đồng thuê. Trả ghi chú. Dùng khi abort và khi huỷ tác vụ.
func (r *Runner) discardPending(ctx context.Context, rs *runState, reason string) []string {
	id := rs.t.State.ID
	ps := rs.t.State.PendingStep
	var notes []string
	if ps != nil {
		if ps.Step == StepFeat && ps.FeatBefore != "" {
			// Tệp subagent đã sửa trước khi bị bỏ vẫn thuộc tác vụ: ghi vào Touched (dưới khoá độc quyền nếu lấy được).
			l, err := AcquireLock(r.Tasks.Dir, r.Root, id+" "+ps.Step, LockExclusive)
			if err != nil && errors.Is(err, errLeaseUnreadable) {
				// Tệp hợp đồng thuê hỏng chặn khoá, còn abort chính là đường người chủ động khôi phục: gỡ tệp hỏng (releaseLease
				// theo tác vụ xoá nó) rồi lấy khoá lại để ghi Touched. Khoá bận vì chủ khác thì không vào nhánh này.
				// Gỡ tệp hỏng xoá hợp đồng của MỌI tác vụ trên cùng gốc: chỉ làm khi không tác vụ nào khác ở đó còn bước dở
				// (driver session), nếu không tác vụ kia mất hợp đồng và tệp nó sửa bị ghi nhầm vào Touched của tác vụ này.
				if others := r.otherPendingOnRoot(id, r.Root); len(others) > 0 {
					notes = append(notes, "tệp hợp đồng thuê khoá hỏng nhưng tác vụ khác trên cùng cây còn bước dở ("+
						strings.Join(others, ", ")+"): không gỡ tệp; ghi nhận tệp b-feat đã sửa bị bỏ qua. Người xử lý: kiểm tệp "+
						leaseFilePath(lockPath(r.Tasks.Dir, r.Root))+" rồi abort các tác vụ đó hoặc xoá tệp")
				} else if rerr := releaseLease(r.Tasks.Dir, r.Root, id, "", true); rerr == nil {
					notes = append(notes, "đã xoá tệp hợp đồng thuê khoá hỏng để ghi nhận tệp b-feat đã sửa")
					l, err = AcquireLock(r.Tasks.Dir, r.Root, id+" "+ps.Step, LockExclusive)
				}
			}
			if err == nil {
				r.endTouched(ctx, rs, ps.FeatBefore)
				l.Release()
			} else {
				notes = append(notes, "không ghi nhận được tệp b-feat đã sửa (khoá worktree đang bị giữ): "+err.Error())
			}
		}
		r.emitRun(ps.RunID, spool.WorkflowFinished, map[string]any{"status": "interrupted", "end_reason": "bint run abort: " + reason, "task_id": id})
		parent := ps.ParentRunID
		if parent == "" {
			parent = rs.runID
		}
		r.emitRun(parent, spool.StepFinished, map[string]any{"step_id": "st_" + ps.RunID, "name": ps.Step, "status": "interrupted"})
		r.RecordError(id, ps.Step, "bước "+ps.Step+" bị bỏ (bint run abort): "+reason)
		if t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error { t.State.PendingStep = nil; return nil }); err == nil {
			rs.t = t
		}
	}
	// Hợp đồng của tác vụ bị xoá kể cả khi phiếu không còn bước dở (trạng thái hỏng): không để khoá kẹt tới hết hạn.
	if err := releaseLease(r.Tasks.Dir, r.Root, id, "", len(r.otherPendingOnRoot(id, r.Root)) == 0); err != nil {
		notes = append(notes, "không xoá được hợp đồng thuê khoá: "+err.Error())
	}
	return notes
}

// discardPendingForCancel gỡ bước dở của tác vụ sắp bị huỷ (Env, không cần Runner): xoá PendingStep và hợp đồng thuê
// để việc dọn worktree không bị chính hợp đồng của tác vụ chặn.
func (e *Env) discardPendingForCancel(t *task.Task) {
	root := t.State.Worktree
	if root == "" {
		root = e.Root
	}
	if ps := t.State.PendingStep; ps != nil {
		e.emitRun(ps.RunID, spool.WorkflowFinished, map[string]any{"status": "interrupted", "end_reason": "tác vụ bị huỷ", "task_id": t.State.ID})
		e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error { t.State.PendingStep = nil; return nil })
	}
	releaseLease(e.Tasks.Dir, root, t.State.ID, "", len(e.otherPendingOnRoot(t.State.ID, root)) == 0)
}

// emitRun ghi sự kiện của một lượt chạy do Bint cấp (Runner có bản gắn nhật ký lỗi ghi).
func (e *Env) emitRun(runID, typ string, payload map[string]any) error {
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "ev_wf_" + ulid.New(), Type: typ, Source: "bint-run",
		Timestamp: e.now().UTC(), RunID: runID, Worktree: e.Root, Payload: payload}
	_, err := spool.Write(e.Spool, ev)
	return err
}
