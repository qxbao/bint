package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/task"
	"bint/internal/workflow"
)

// RunCallbacks là các hàm gọi ngược của một lượt điều phối; nil = bỏ qua. Confirm/Ask chỉ được cài khi thao
// tác là của người (ngoài phiên agent): trong phiên agent Runner không tương tác nên những gì cần người duyệt
// làm tác vụ chuyển blocked. Các hàm chạy trong goroutine của Runner và có thể chặn (TUI chờ hộp thoại).
type RunCallbacks struct {
	Confirm func(workflow.Question) bool
	Ask     func(workflow.Question) string
	// Log nhận từng dòng tiến trình của orchestrator (đã định dạng, không có xuống dòng cuối).
	Log func(string)
	// OnStep báo bước agent bắt đầu/kết thúc kèm đường dẫn log của bước.
	OnStep func(workflow.StepEvent)
	// Agent thay cách dựng agent của từng bước (nil = harness theo cấu hình và cờ --harness/--role/--agent-command);
	// dùng trong kiểm thử để tiêm agent giả.
	Agent workflow.AgentFactory
}

// RunWorkflowOptions: `bint run <workflow> [mô tả]`.
type RunWorkflowOptions struct {
	Workflow      string   `arg:"workflow" form:"required,choices=b-do|b-req|b-feat|b-review"`
	Request       string   `arg:"description" form:"multiline"`
	Task          string   `flag:"task" form:"suggest=tasks"`
	Goal          string   `flag:"goal" form:"advanced"`
	VerifyCommand string   `flag:"verify-command" form:"advanced"`
	Criteria      []string `flag:"criteria" form:"advanced"`
	Allow         []string `flag:"allow" form:"advanced,suggest=files"`
	Deny          []string `flag:"deny" form:"advanced,suggest=files"`
	Constraints   []string `flag:"constraint" form:"advanced"`
	Autonomy      string   `flag:"autonomy" form:"advanced,choices=ask_always|ask_on_protected|auto"`
	ClearScope    bool     `flag:"clear-scope" form:"advanced"`
	Plan          bool     `flag:"plan"`
	NoPlan        bool     `flag:"no-plan"`
	Depth         string   `flag:"depth" form:"choices=auto|standard|deep"`
	NoReview      bool     `flag:"no-review"`
	NoWait        bool     `flag:"no-wait" form:"advanced"`
	Worktree      bool     `flag:"worktree" form:"advanced"`
	Shared        bool     `flag:"shared" form:"advanced"`
	Driver        string   `flag:"driver" form:"advanced,choices=auto|process|session"`
	Harness       string   `flag:"harness" form:"choices=claude|codex|antigravity"`
	Roles         []string `flag:"role"`
	Model         string   `flag:"model"`
	AgentCommand  string   `flag:"agent-command" form:"advanced"`

	GoalSet, VerifyCommandSet, AutonomySet bool
	RunCallbacks
}

// RunWorkflowDefaults: độ sâu và driver tự chọn.
func RunWorkflowDefaults(config.Config) RunWorkflowOptions {
	return RunWorkflowOptions{Depth: task.DepthAuto, Driver: "auto"}
}

func (o RunWorkflowOptions) fields() ticketFields {
	return ticketFields{o.Goal, o.VerifyCommand, o.Autonomy, o.GoalSet, o.VerifyCommandSet, o.AutonomySet,
		o.Criteria, o.Allow, o.Deny, o.Constraints, false, o.ClearScope}
}

// Validate: --plan/--no-plan loại trừ nhau, --depth hợp lệ, harness/role hợp lệ và không đi cùng --agent-command.
// Workflow không có trong danh sách do Runner báo (lỗi thường, không phải lỗi dùng sai) như CLI cũ.
func (o RunWorkflowOptions) Validate() error {
	if strings.TrimSpace(o.Workflow) == "" {
		return Usagef("thiếu workflow (b-do | b-req | b-feat | b-review)")
	}
	if o.Plan && o.NoPlan {
		return Usagef("--plan và --no-plan loại trừ nhau")
	}
	if o.Worktree && o.Shared {
		return Usagef("--worktree và --shared loại trừ nhau")
	}
	if (o.Worktree || o.Shared) && strings.TrimSpace(o.Task) != "" {
		return Usagef("--worktree/--shared chỉ chọn lúc lập phiếu: không dùng cùng --task (chế độ đã ghi trong phiếu)")
	}
	if drv, err := workflow.NormalizeDriverFlag(o.Driver); err != nil {
		return UsageError{err}
	} else if drv != "" && strings.TrimSpace(o.Task) != "" {
		return Usagef("--driver chỉ chọn lúc lập phiếu: không dùng cùng --task (driver đã ghi trong phiếu)")
	}
	if _, err := checkDepth(o.Depth, o.NoPlan, o.Workflow); err != nil {
		return err
	}
	_, err := harnessSelection(o.Harness, o.Roles, o.AgentCommand)
	return err
}

// RunResumeOptions: `bint run resume <task-id>`.
type RunResumeOptions struct {
	Ref          string   `arg:"task-id" form:"required,suggest=tasks"`
	NoReview     bool     `flag:"no-review"`
	NoWait       bool     `flag:"no-wait" form:"advanced"`
	Harness      string   `flag:"harness" form:"choices=claude|codex|antigravity"`
	Roles        []string `flag:"role"`
	Model        string   `flag:"model"`
	AgentCommand string   `flag:"agent-command" form:"advanced"`
	RunCallbacks
}

func RunResumeDefaults(config.Config) RunResumeOptions { return RunResumeOptions{} }

func (o RunResumeOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	_, err := harnessSelection(o.Harness, o.Roles, o.AgentCommand)
	return err
}

// RunWorkflow điều phối một workflow (b-do, b-req, b-feat, b-review). Kết quả blocked/failed/interrupted là
// kết quả hợp lệ (Result.Status), không phải lỗi; huỷ ctx thì tác vụ chuyển interrupted.
func RunWorkflow(ctx context.Context, ws *TaskWS, o RunWorkflowOptions) (*workflow.Result, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	wo := workflow.RunOptions{Workflow: o.Workflow, Request: strings.TrimSpace(o.Request), TaskRef: o.Task,
		NoReview: o.NoReview, NoWait: o.NoWait, Edit: o.fields().edit(), Isolation: isolationFlag(o.Worktree, o.Shared)}
	wo.Driver, _ = workflow.NormalizeDriverFlag(o.Driver) // đã kiểm ở Validate
	if o.Plan || o.NoPlan {
		p := o.Plan
		wo.Plan = &p
	}
	wo.Depth, _ = checkDepth(o.Depth, o.NoPlan, o.Workflow) // đã kiểm ở Validate
	if err := requireHumanForAutonomy(wo.Edit, "bint run"); err != nil {
		return nil, err
	}
	return runWith(ctx, ws, wo, o.Harness, o.Roles, o.AgentCommand, o.Model, o.RunCallbacks)
}

// ResumeWorkflow tiếp tục tác vụ blocked/interrupted/failed từ bước đang dở.
func ResumeWorkflow(ctx context.Context, ws *TaskWS, o RunResumeOptions) (*workflow.Result, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	t, err := ws.Tasks.Load(o.Ref)
	if err != nil {
		return nil, err
	}
	t = ws.Reconcile(t)
	if len(t.State.PendingPaths) > 0 {
		return nil, fmt.Errorf("tác vụ %s có thay đổi chờ người duyệt: %s (bint task approve %s)", t.State.ID,
			strings.Join(t.State.PendingPaths, ", "), t.State.ID)
	}
	wf := t.State.Workflow
	if wf == "" {
		wf = workflow.StepDo
	}
	wo := workflow.RunOptions{Workflow: wf, TaskRef: t.State.ID, Resume: true, NoReview: o.NoReview, NoWait: o.NoWait}
	return runWith(ctx, ws, wo, o.Harness, o.Roles, o.AgentCommand, o.Model, o.RunCallbacks)
}

func runWith(ctx context.Context, ws *TaskWS, wo workflow.RunOptions, harnessName string, roles []string, agentCommand, model string,
	cb RunCallbacks) (*workflow.Result, error) {
	if ws.RunID != "" {
		return nil, fmt.Errorf("không chạy `bint run` lồng trong một bước workflow khác (BINT_RUN_ID=%s)", ws.RunID)
	}
	sel, err := harnessSelection(harnessName, roles, agentCommand)
	if err != nil {
		return nil, err
	}
	k := ws.K
	r := &workflow.Runner{Env: ws.Env, Agent: workflow.HarnessAgents(k.Config, agentCommand, model, sel), OnStep: cb.OnStep}
	if cb.Agent != nil {
		r.Agent = cb.Agent
	}
	if cb.Log != nil {
		r.Logf = func(format string, a ...any) { cb.Log(fmt.Sprintf(format, a...)) }
	}
	if ws.Human { // trong phiên agent không tương tác: cần người duyệt thì tác vụ chuyển blocked
		r.Confirm, r.Ask = cb.Confirm, cb.Ask
	}
	if k.Config.Context.Interfaces.CLI {
		r.Context = func(ctx context.Context, goal string, paths []string) (ctxengine.Pack, error) {
			c, err := OpenContext(ctx, k.Root)
			if err != nil {
				return ctxengine.Pack{}, err
			}
			defer c.Close()
			return c.Engine.ForTask(ctx, goal, paths)
		}
	}
	res, err := r.Run(ctx, wo)
	if err != nil {
		return nil, err
	}
	// Nạp sự kiện ngay để `bint runs` thấy lượt vừa chạy.
	if s, err := k.OpenStore(context.Background()); err == nil {
		k.Ingest(context.Background(), s, time.Now())
		s.Close()
	}
	return res, nil
}

// ActiveStepsWatch là thứ cần để đọc lại các bước đang chạy mà không phải mở lại workspace (đọc và phân tích
// .bint/config.yml, mở repo): thư mục tác vụ và gốc worktree, dựng một lần bằng ResolveActiveSteps.
type ActiveStepsWatch struct {
	TasksDir, Root string
}

// ResolveActiveSteps dựng ActiveStepsWatch cho repository chứa dir. Chưa init hoặc không mở được thì ok=false.
func ResolveActiveSteps(ctx context.Context, dir string) (ActiveStepsWatch, bool) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return ActiveStepsWatch{}, false
	}
	return ActiveStepsWatch{TasksDir: task.NewStore(ws.Paths.Runtime).Dir, Root: ws.Repo.Toplevel}, true
}

// Steps liệt kê các bước đang giữ khoá (chỉ đọc tệp chủ khoá; không đụng cấu hình nên vẫn chạy khi
// .bint/config.yml hỏng sau lần dựng).
func (w ActiveStepsWatch) Steps() []workflow.ActiveStep {
	return workflow.ActiveSteps(w.TasksDir, w.Root)
}

// ActiveSteps liệt kê các bước `bint run` đang chạy trên cây làm việc của repository chứa dir (đọc tệp chủ khoá
// của worktree, chỉ đọc). TUI dùng để báo "<bước> của <tác vụ> đang chạy trên cây này". Chưa init hoặc không
// đọc được thì trả rỗng: đây chỉ là thông tin, không phải điều kiện cho thao tác nào. Gọi lặp thì dùng
// ResolveActiveSteps một lần rồi Steps() để khỏi mở workspace mỗi lượt.
func ActiveSteps(ctx context.Context, dir string) []workflow.ActiveStep {
	w, ok := ResolveActiveSteps(ctx, dir)
	if !ok {
		return nil
	}
	return w.Steps()
}

// ---- driver session: bint run next | complete | abort (docs/isolation-subagent-plan.md mục 6) ----

// RunNextOptions: `bint run next <task-id>` — hành động kế tiếp của tác vụ (phiên điều phối chạy bước bằng subagent).
type RunNextOptions struct {
	Ref    string `arg:"task-id" form:"required,suggest=tasks"`
	Resume bool   `flag:"resume"`
	NoWait bool   `flag:"no-wait" form:"advanced"`
}

func RunNextDefaults(config.Config) RunNextOptions { return RunNextOptions{} }

func (o RunNextOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// RunCompleteOptions: `bint run complete <task-id> --run <run_id>` — đóng bước subagent vừa chạy.
type RunCompleteOptions struct {
	Ref     string `arg:"task-id" form:"required,suggest=tasks"`
	Run     string `flag:"run" form:"required"`
	AgentID string `flag:"agent-id" form:"advanced"`
	Failed  string `flag:"failed" form:"advanced"`
	NoWait  bool   `flag:"no-wait" form:"advanced"`
}

func RunCompleteDefaults(config.Config) RunCompleteOptions { return RunCompleteOptions{} }

func (o RunCompleteOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	if strings.TrimSpace(o.Run) == "" {
		return Usagef("thiếu --run <run_id> (lượt của bước do `bint run next` trả về)")
	}
	return nil
}

// RunAbortOptions: `bint run abort <task-id>` — bỏ bước subagent đang dở (gỡ hợp đồng thuê khoá).
type RunAbortOptions struct {
	Ref    string `arg:"task-id" form:"required,suggest=tasks"`
	Reason string `flag:"reason"`
}

func RunAbortDefaults(config.Config) RunAbortOptions { return RunAbortOptions{} }

func (o RunAbortOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// sessionRunner dựng Runner cho lệnh session. Driver session không gọi harness từ tiến trình Bint (Agent nil) và
// không tương tác: những gì cần người duyệt làm tác vụ chuyển blocked. Cấm chạy lồng trong một bước workflow khác.
func sessionRunner(ws *TaskWS) (*workflow.Runner, error) {
	if ws.RunID != "" {
		return nil, fmt.Errorf("không chạy `bint run next/complete/abort` lồng trong một bước workflow khác (BINT_RUN_ID=%s)", ws.RunID)
	}
	k := ws.K
	r := &workflow.Runner{Env: ws.Env}
	if k.Config.Context.Interfaces.CLI {
		r.Context = func(ctx context.Context, goal string, paths []string) (ctxengine.Pack, error) {
			c, err := OpenContext(ctx, k.Root)
			if err != nil {
				return ctxengine.Pack{}, err
			}
			defer c.Close()
			return c.Engine.ForTask(ctx, goal, paths)
		}
	}
	return r, nil
}

// ingestAfterSession nạp sự kiện ngay để `bint runs` thấy lượt vừa chạy.
func ingestAfterSession(ws *TaskWS) {
	if s, err := ws.K.OpenStore(context.Background()); err == nil {
		ws.K.Ingest(context.Background(), s, time.Now())
		s.Close()
	}
}

// RunNext trả hành động kế tiếp của tác vụ: agent (chạy subagent với prompt rồi RunComplete), process (chạy lệnh
// bint run <bước> --task rồi RunNext), done, blocked hoặc failed. Gọi lại khi bước còn hạn trả đúng bước đó.
func RunNext(ctx context.Context, ws *TaskWS, o RunNextOptions) (*workflow.SessionAction, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	r, err := sessionRunner(ws)
	if err != nil {
		return nil, err
	}
	a, err := r.Next(ctx, workflow.NextOptions{TaskRef: o.Ref, Resume: o.Resume, NoWait: o.NoWait})
	if err != nil {
		return nil, err
	}
	ingestAfterSession(ws)
	return a, nil
}

// RunComplete đóng bước đang dở của tác vụ (subagent đã xong hoặc báo thất bại): kiểm chỉ đọc, phạm vi, kiểm chứng,
// tài liệu, quyết định sau review. run_id sai ⇒ lỗi (workflow.ErrRunMismatch).
func RunComplete(ctx context.Context, ws *TaskWS, o RunCompleteOptions) (*workflow.SessionAction, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	r, err := sessionRunner(ws)
	if err != nil {
		return nil, err
	}
	a, err := r.Complete(ctx, workflow.CompleteOptions{TaskRef: o.Ref, RunID: o.Run, AgentID: o.AgentID, Failed: o.Failed, NoWait: o.NoWait})
	if err != nil {
		return nil, err
	}
	ingestAfterSession(ws)
	return a, nil
}

// RunAbort bỏ bước đang dở: gỡ bước, hợp đồng thuê khoá, đưa tác vụ về interrupted.
func RunAbort(ctx context.Context, ws *TaskWS, o RunAbortOptions) (*workflow.SessionAction, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	r, err := sessionRunner(ws)
	if err != nil {
		return nil, err
	}
	a, err := r.Abort(ctx, o.Ref, o.Reason)
	if err != nil {
		return nil, err
	}
	ingestAfterSession(ws)
	return a, nil
}
