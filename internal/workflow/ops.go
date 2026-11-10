// Package workflow là Workflow Orchestrator tối thiểu (mục 2.4, 6): thao tác trên
// tác vụ (dùng chung cho `bint task` do người/agent gọi và cho `bint run`), kiểm
// chứng, đối chiếu tiêu chí, review, duyệt; và điều phối chuỗi b-req → b-feat →
// b-review theo chương trình với giới hạn vận hành (mục 5).
package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/eval"
	"bint/internal/gitrepo"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/ulid"
)

// Biến môi trường do `bint run` đặt cho tiến trình agent.
const (
	EnvRunID         = "BINT_RUN_ID"
	EnvTaskID        = "BINT_TASK_ID"
	EnvApprovedPaths = "BINT_APPROVED_PATHS"
)

// Env là môi trường thao tác tác vụ của một repository.
type Env struct {
	// Root là thư mục gốc repository (nơi chạy lệnh kiểm chứng và agent).
	Root string
	// ConfigRoot là thư mục chứa .bint/ (lớp phủ skill); rỗng = Root.
	ConfigRoot string
	Cfg        config.Config
	Tasks      task.Store
	Spool      string
	Version    string
	// Actor là người/agent thực hiện thao tác ("agent", hoặc danh tính Git của người).
	Actor string
	// Human: thao tác do người dùng (ngoài phiên agent).
	Human bool
	// RunID là lượt chạy hiện tại (BINT_RUN_ID hoặc lượt do orchestrator cấp).
	RunID string
	// Source của sự kiện spool ("cli" hoặc "bint-run").
	Source string
	Now    func() time.Time
	Out    io.Writer
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// emit ghi một sự kiện tác vụ vào spool, gắn vào lượt chạy hiện tại: BINT_RUN_ID,
// phiên Claude Code đang gọi lệnh, hoặc lượt gần nhất của tác vụ. Không xác định
// được lượt nào thì bỏ qua (trạng thái tác vụ vẫn nằm trong tệp).
func (e *Env) emit(t *task.Task, typ string, payload map[string]any) {
	if e.Spool == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["task_id"] = t.State.ID
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "ev_task_" + ulid.New(), Type: typ,
		Source: e.source(), Timestamp: e.now().UTC(), Worktree: e.Root, Payload: payload}
	switch {
	case e.RunID != "":
		ev.RunID = e.RunID
	case os.Getenv("CLAUDE_CODE_SESSION_ID") != "":
		ev.Harness, ev.SessionID = "claude", os.Getenv("CLAUDE_CODE_SESSION_ID")
	case t.State.LastRun() != "":
		ev.RunID = t.State.LastRun()
	default:
		return
	}
	spool.Write(e.Spool, ev)
}

func (e *Env) source() string {
	if e.Source != "" {
		return e.Source
	}
	return "cli"
}

func (e *Env) at() string { return task.Now(e.now()) }

// NewTaskOptions điều khiển việc lập phiếu.
type NewTaskOptions struct {
	Request  string
	Workflow string
	Edit     TicketEdit
	// Plan ghi đè kết quả Classify (nil = theo heuristic).
	Plan *bool
}

// NewTask lập phiếu tác vụ từ mô tả tự do (bước Classify).
func (e *Env) NewTask(ctx context.Context, o NewTaskOptions) (*task.Task, task.Assessment, error) {
	now := e.now()
	confirmed := ""
	if tc := e.Cfg.Eval.TestCommand; tc != nil {
		confirmed = *tc
	}
	t := task.FromRequest(e.Root, o.Request, confirmed)
	if err := o.Edit.apply(&t); err != nil {
		return nil, task.Assessment{}, err
	}
	cls := task.Classify(o.Request)
	if o.Plan != nil {
		cls = task.Classification{Plan: *o.Plan, Reason: "người dùng chỉ định"}
	}
	if o.Workflow == "" {
		o.Workflow = "b-do"
	}
	st := task.State{Workflow: o.Workflow, Base: gitrepo.HeadCommit(ctx, e.Root), Branch: gitrepo.Branch(ctx, e.Root),
		Worktree: e.Root, Classification: &cls}
	if e.RunID != "" {
		st.LinkRun(e.RunID)
	}
	tk, err := e.Tasks.Create(t, st, now)
	if err != nil {
		return nil, task.Assessment{}, err
	}
	e.emit(tk, spool.TaskLinked, map[string]any{"workflow": o.Workflow})
	return tk, e.Assess(tk), nil
}

// Assess đánh giá rủi ro của tác vụ.
func (e *Env) Assess(t *task.Task) task.Assessment {
	return task.Assess(t.Ticket, e.Cfg.Limits.ProtectedPaths, e.VerifyConfirmed(t))
}

// TicketEdit là thay đổi phiếu (`bint task new/set`). Con trỏ nil = giữ nguyên.
type TicketEdit struct {
	Goal          *string
	VerifyCommand *string
	Criteria      []string
	ClearCriteria bool
	Allow, Deny   []string
	ClearScope    bool
	Constraints   []string
	Autonomy      *string
}

func (ed TicketEdit) apply(t *task.Ticket) error {
	if ed.Goal != nil {
		t.Goal = strings.TrimSpace(*ed.Goal)
	}
	if ed.VerifyCommand != nil {
		t.Verify.Command = strings.TrimSpace(*ed.VerifyCommand)
	}
	if ed.ClearCriteria {
		t.Verify.Criteria = nil
	}
	t.Verify.Criteria = appendUnique(t.Verify.Criteria, ed.Criteria...)
	if ed.ClearScope {
		t.Scope = task.Scope{}
	}
	t.Scope.Allow = appendUnique(t.Scope.Allow, ed.Allow...)
	t.Scope.Deny = appendUnique(t.Scope.Deny, ed.Deny...)
	t.Constraints = appendUnique(t.Constraints, ed.Constraints...)
	if ed.Autonomy != nil {
		t.Autonomy = *ed.Autonomy
	}
	return t.Validate()
}

func appendUnique(xs []string, add ...string) []string {
	for _, a := range add {
		a = strings.TrimSpace(a)
		if a != "" && !slices.Contains(xs, a) {
			xs = append(xs, a)
		}
	}
	return xs
}

// ErrAgent: thao tác chỉ dành cho người.
var ErrAgent = errors.New("thao tác này chỉ dành cho người dùng, không chạy được từ trong phiên agent")

// SetTicket sửa phiếu. Agent không được nâng autonomy lên auto (mức tự chủ do người quyết).
func (e *Env) SetTicket(ref string, ed TicketEdit) (*task.Task, task.Assessment, error) {
	if !e.Human && ed.Autonomy != nil && *ed.Autonomy == task.AutonomyAuto {
		return nil, task.Assessment{}, fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", ErrAgent)
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if task.Terminal(t.State.Status) {
			return fmt.Errorf("tác vụ đã %s", t.State.Status)
		}
		return ed.apply(&t.Ticket)
	})
	if err != nil {
		return nil, task.Assessment{}, err
	}
	return t, e.Assess(t), nil
}

// WriteArtifact ghi tài liệu của tác vụ.
func (e *Env) WriteArtifact(ref, name, content string) (*task.Task, string, error) {
	var path string
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		p, err := t.WriteArtifact(name, content)
		if err != nil {
			return err
		}
		path = p
		if t.State.Artifacts == nil {
			t.State.Artifacts = map[string]string{}
		}
		t.State.Artifacts[name] = e.at()
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	e.emit(t, spool.TaskArtifact, map[string]any{"name": name, "path": path})
	return t, path, nil
}

// TransitionOptions điều khiển SetStatus.
type TransitionOptions struct {
	Reason string
	// Force: người xác nhận hoàn thành dù thiếu bằng chứng.
	Force bool
	// Orchestrator: lệnh do chính tiến trình điều phối gọi.
	Orchestrator bool
}

// SetStatus chuyển trạng thái tác vụ theo bảng chuyển. Chuyển sang done cần bằng
// chứng (task.DoneBlockers); chỉ người mới --force được.
func (e *Env) SetStatus(ctx context.Context, ref, to string, o TransitionOptions) (*task.Task, []string, error) {
	if o.Force && !e.Human {
		return nil, nil, fmt.Errorf("%w: --force", ErrAgent)
	}
	if !slices.Contains(task.Statuses, to) {
		return nil, nil, fmt.Errorf("trạng thái %q không hợp lệ (%s)", to, strings.Join(task.Statuses, ", "))
	}
	var blockers []string
	fp := ""
	if to == task.StatusDone {
		fp = task.WorktreeFingerprint(ctx, e.Root)
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if !o.Orchestrator && t.OwnedByOther() {
			return fmt.Errorf("tác vụ đang được `bint run` điều phối (lượt %s): trạng thái do Bint Core chuyển", t.State.Runner.RunID)
		}
		if to == task.StatusDone {
			blockers = task.DoneBlockers(t, task.DoneInput{Worktree: fp, VerifyConfirmed: e.VerifyConfirmed(t)})
			// Tính lại từ diff hiện tại, không tin PendingPaths (chỉ Runner điền).
			pend, err := e.UnapprovedChanges(ctx, t)
			if err != nil {
				blockers = append(blockers, "không xác định được tệp đã sửa: "+err.Error())
			} else if len(pend) > 0 {
				blockers = append(blockers, "thay đổi chạm đường dẫn bảo vệ/deny/ngoài phạm vi chưa được duyệt: "+strings.Join(pend, ", ")+" (bint task approve)")
			}
			if len(blockers) > 0 && !o.Force {
				return &NotDoneError{Blockers: blockers}
			}
		}
		reason := o.Reason
		if o.Force && len(blockers) > 0 {
			reason = strings.TrimSpace(reason + " [người xác nhận dù thiếu bằng chứng: " + strings.Join(blockers, "; ") + "]")
		}
		return t.State.Apply(to, e.actor(), reason, e.at())
	})
	if err != nil {
		return t, blockers, err
	}
	e.emit(t, spool.TaskStatus, map[string]any{"status": to, "reason": o.Reason, "forced": o.Force && len(blockers) > 0})
	return t, blockers, nil
}

// UnapprovedChanges trả về tệp đã sửa (so với base của tác vụ) cần người duyệt
// nhưng chưa nằm trong ProtectedApproval.
func (e *Env) UnapprovedChanges(ctx context.Context, t *task.Task) ([]string, error) {
	files, err := task.ChangedFiles(ctx, e.Root, t.State.Base)
	if err != nil {
		return nil, err
	}
	sc := task.CheckScope(files, t.Ticket, e.Cfg.Limits.ProtectedPaths)
	approved := ApprovedPaths(t)
	need := sc.NeedsApproval()
	if t.Ticket.Autonomy == task.AutonomyAskAlways {
		need = append(append([]string{}, need...), sc.OutsideAllow...)
	}
	var out []string
	for _, f := range need {
		if !slices.Contains(approved, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// NotDoneError: chưa đủ bằng chứng để coi là hoàn thành.
type NotDoneError struct{ Blockers []string }

func (e *NotDoneError) Error() string {
	return "chưa đủ bằng chứng để chuyển done:\n  - " + strings.Join(e.Blockers, "\n  - ")
}

func (e *Env) actor() string {
	if e.Actor != "" {
		return e.Actor
	}
	if e.Human {
		return "human"
	}
	return "agent"
}

// VerifyConfirmed: lệnh kiểm chứng hiện tại của phiếu đã được người xác nhận
// (bint task approve), hoặc trùng eval.test_command đã xác nhận trong cấu hình.
func (e *Env) VerifyConfirmed(t *task.Task) bool {
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" {
		return false
	}
	if tc := e.Cfg.Eval.TestCommand; tc != nil && strings.TrimSpace(*tc) == cmd {
		return true
	}
	a := t.State.VerifyApproval
	return a != nil && a.Command == cmd
}

// VerifyResult là kết quả kiểm chứng.
type VerifyResult struct {
	Verification task.Verification
	Skipped      string // lý do không chạy
}

// Verify chạy lệnh kiểm chứng của phiếu (mục 6.7) trong thư mục gốc repository và
// ghi kết quả. Lệnh chưa được người xác nhận không được chạy khi
// security.require_approval_for_commands (mặc định); nếu tắt, kết quả chỉ là tín
// hiệu trung bình.
func (e *Env) Verify(ctx context.Context, ref string) (*task.Task, VerifyResult, error) {
	var res VerifyResult
	t, err := e.Tasks.Load(ref)
	if err != nil {
		return nil, res, err
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" {
		res.Skipped = "phiếu chưa có verify.command (bint task set --verify-command)"
		return t, res, nil
	}
	confirmed := e.VerifyConfirmed(t)
	if !confirmed && e.Cfg.Security.RequireApprovalForCommands {
		res.Skipped = "lệnh kiểm chứng chưa được người xác nhận; Bint không chạy lệnh do agent đề xuất (bint task approve " + t.State.ID + ")"
		return t, res, nil
	}
	n := len(t.State.Verifications) + 1
	log := t.LogPath(fmt.Sprintf("verify-%d.log", n))
	timeout := time.Duration(e.Cfg.Workflow.VerifyTimeoutMinutes) * time.Minute
	r := eval.RunShell(ctx, e.Root, cmd, timeout, log, nil)
	v := task.Verification{At: e.at(), Command: cmd, Confirmed: confirmed, ExitCode: r.ExitCode, Passed: r.OK(),
		TimedOut: r.TimedOut, Log: log, Worktree: task.WorktreeFingerprint(ctx, e.Root), By: e.actor(), RunID: e.RunID}
	if r.Err != nil && ctx.Err() != nil {
		return t, res, ctx.Err()
	}
	t, err = e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
		t.State.Verifications = append(t.State.Verifications, v)
		return nil
	})
	if err != nil {
		return t, res, err
	}
	res.Verification = v
	e.emit(t, spool.TaskVerified, map[string]any{"command": cmd, "passed": v.Passed, "confirmed": confirmed,
		"exit_code": v.ExitCode, "timed_out": v.TimedOut})
	return t, res, nil
}

// CheckCriterion ghi kết quả đối chiếu một tiêu chí (tự đánh giá; tín hiệu trung bình).
func (e *Env) CheckCriterion(ref string, index int, met bool, evidence string) (*task.Task, error) {
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return nil, errors.New("cần --evidence: bằng chứng cụ thể (lệnh + kết quả, tệp:dòng)")
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if index < 1 || index > len(t.Ticket.Verify.Criteria) {
			return fmt.Errorf("tiêu chí %d không tồn tại (phiếu có %d tiêu chí)", index, len(t.Ticket.Verify.Criteria))
		}
		t.State.Criteria = append(t.State.Criteria, task.CriterionCheck{Index: index, Met: met, Evidence: evidence, By: e.actor(), At: e.at()})
		return nil
	})
	if err != nil {
		return t, err
	}
	e.emit(t, spool.TaskCriteria, map[string]any{"index": index, "met": met, "evidence": truncate(evidence, 300)})
	return t, nil
}

// RecordError ghi một lỗi hoặc lần thử thất bại.
func (e *Env) RecordError(ref, step, msg string) (*task.Task, error) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil, errors.New("thiếu nội dung lỗi")
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		t.State.Errors = append(t.State.Errors, task.ErrorRecord{Step: step, Message: truncate(msg, 2000), At: e.at(), By: e.actor(), RunID: e.RunID})
		return nil
	})
	if err != nil {
		return t, err
	}
	e.emit(t, spool.TaskError, map[string]any{"step": step, "message": truncate(msg, 300)})
	return t, nil
}

// SubmitReview ghi một vòng review.
func (e *Env) SubmitReview(ref string, data []byte) (*task.Task, task.ReviewRound, error) {
	sub, errs := task.ParseSubmission(data, e.Root)
	if len(errs) > 0 {
		return nil, task.ReviewRound{}, errors.New("dữ liệu review không hợp lệ:\n  - " + strings.Join(errs, "\n  - "))
	}
	var round task.ReviewRound
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		r, err := t.AddRound(sub, e.actor(), e.RunID, e.at())
		round = r
		return err
	})
	if err != nil {
		return t, round, err
	}
	e.emit(t, spool.ReviewCompleted, map[string]any{"round": round.Round, "findings": len(round.Findings),
		"blocking": len(round.Blocking()), "path": t.Dir + string(os.PathSeparator) + task.ReviewFile})
	return t, round, nil
}

// Approval là những gì người vừa duyệt.
type Approval struct {
	VerifyCommand string
	Paths         []string
}

// Approve: người xác nhận lệnh kiểm chứng hiện tại và duyệt các thay đổi đang chờ
// (đường dẫn bảo vệ/deny). Không đổi trạng thái; `bint run resume` tiếp tục.
func (e *Env) Approve(ref string) (*task.Task, Approval, error) {
	var a Approval
	if !e.Human {
		return nil, a, fmt.Errorf("%w: bint task approve", ErrAgent)
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if cmd := strings.TrimSpace(t.Ticket.Verify.Command); cmd != "" && !e.VerifyConfirmed(t) {
			t.State.VerifyApproval = &task.Approval{Command: cmd, By: e.actor(), At: e.at()}
			a.VerifyCommand = cmd
		}
		// Tính lại từ diff hiện tại (task chưa qua Runner/checkScope thì PendingPaths rỗng).
		pending := append([]string{}, t.State.PendingPaths...)
		if cur, derr := e.UnapprovedChanges(context.Background(), t); derr == nil {
			pending = appendUnique(pending, cur...)
		}
		if len(pending) > 0 {
			prev := []string{}
			if t.State.ProtectedApproval != nil {
				prev = t.State.ProtectedApproval.Paths
			}
			t.State.ProtectedApproval = &task.Approval{Paths: appendUnique(prev, pending...), By: e.actor(), At: e.at()}
			a.Paths = pending
			t.State.PendingPaths = nil
		}
		return nil
	})
	return t, a, err
}

// ApprovedPaths trả về các tệp được bảo vệ mà người đã duyệt cho tác vụ.
func ApprovedPaths(t *task.Task) []string {
	if t.State.ProtectedApproval == nil {
		return nil
	}
	return t.State.ProtectedApproval.Paths
}

// Reconcile phát hiện tiến trình điều phối đã chết (mục 9.4): tác vụ → interrupted
// và lượt chạy của tiến trình đó được đóng ở trạng thái interrupted.
func (e *Env) Reconcile(t *task.Task) *task.Task {
	if t.State.Runner == nil || task.RunnerAlive(t.State.Runner) {
		return t
	}
	var runID string
	nt, err := e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
		id, changed := t.Reconcile(e.now())
		if !changed {
			return errNoChange
		}
		runID = id
		return nil
	})
	if err != nil {
		return t
	}
	if runID != "" && e.Spool != "" {
		spool.Write(e.Spool, spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "ev_wf_reconcile_" + runID,
			Type: spool.WorkflowFinished, Source: "bint-run", Timestamp: e.now().UTC(), RunID: runID, Worktree: e.Root,
			Payload: map[string]any{"status": "interrupted", "end_reason": "tiến trình điều phối kết thúc bất thường", "task_id": nt.State.ID}})
	}
	return nt
}

var errNoChange = errors.New("không đổi")

// ContextEventFor dựng sự kiện truy xuất ngữ cảnh gắn với lượt chạy.
func ContextEventFor(runID, worktree, channel string, p ctxengine.Pack, now time.Time) spool.Event {
	ev := ctxengine.ContextEvent("", runID, worktree, channel, runID, p, now)
	ev.SessionID = ""
	ev.RunID = runID
	return ev
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
