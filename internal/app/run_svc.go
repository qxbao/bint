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
	Harness       string   `flag:"harness" form:"choices=claude|codex|antigravity"`
	Roles         []string `flag:"role"`
	Model         string   `flag:"model"`
	AgentCommand  string   `flag:"agent-command" form:"advanced"`

	GoalSet, VerifyCommandSet, AutonomySet bool
	RunCallbacks
}

// RunWorkflowDefaults: độ sâu tự chọn.
func RunWorkflowDefaults(config.Config) RunWorkflowOptions {
	return RunWorkflowOptions{Depth: task.DepthAuto}
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
		NoReview: o.NoReview, Edit: o.fields().edit()}
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
	wo := workflow.RunOptions{Workflow: wf, TaskRef: t.State.ID, Resume: true, NoReview: o.NoReview}
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
