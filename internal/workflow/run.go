package workflow

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"bint/internal/affected"
	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/gitrepo"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/harness/hookcore"
	"bint/internal/iso"
	"bint/internal/skills"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/ulid"
	"gopkg.in/yaml.v3"
)

// stepCheck: bước giả — kiểm tra phạm vi và kiểm chứng sau b-feat (không gọi agent).
const stepCheck = "post-feat"

// stepGoalCheck: bước giả — b-goal đã chạy nhưng kiểm phạm vi dừng chờ người (State.GoalAwaitingCheck); resume chỉ áp phần sau kiểm phạm vi
// (đóng băng, cổng mốc, goalfreeze.go), không chạy lại agent b-goal.
const stepGoalCheck = "post-goal"

// stepHeld: bước giả — bước chỉ đọc đã kết thúc nhưng bị giữ vì cây đổi trong lúc chạy (State.HeldStep); resume chỉ áp
// cổng sau bước của nó (lint + planned cho plan, quyết định cho review), không chạy lại agent.
const stepHeld = "held-step"

// Bước của workflow.
const (
	StepReq    = "b-req"
	StepFeat   = "b-feat"
	StepReview = "b-review"
	StepDo     = "b-do"
)

// AgentFactory tạo agent cho một bước (spec dựng từ flow đang chạy) với giới hạn chi phí/lượt (nil = không giới hạn).
type AgentFactory func(spec StepSpec, maxCostUSD *float64, maxTurns *int) (eval.Agent, error)

// Khoá của các câu hỏi giữa chừng (Question.Key). TUI dịch theo khoá; CLI in Text.
const (
	QAskVerifyCommand = "workflow.ask.verify_command" // Params: question
	QConfirmFeat      = "workflow.confirm.feat"       // Params: task, root
	QConfirmScope     = "workflow.confirm.scope"      // Paths: tệp cần duyệt
)

// Question là một câu hỏi giữa chừng của Runner. Text là câu hỏi tiếng Việt đầy đủ (CLI in nguyên văn);
// Key và Params để giao diện dịch theo ngôn ngữ; Paths là danh sách tệp đi kèm (nếu có).
type Question struct {
	Key    string
	Text   string
	Params map[string]any
	Paths  []string
}

// Runner điều phối workflow theo chương trình (adapter cấp 3, mục 7).
type Runner struct {
	*Env
	Agent AgentFactory
	// Confirm hỏi người dùng có/không (nil = không tương tác: những gì cần người
	// duyệt sẽ làm tác vụ chuyển blocked).
	Confirm func(Question) bool
	// Ask hỏi người dùng một câu trả lời tự do (nil = không tương tác).
	Ask func(Question) string
	// OnStep báo bước agent bắt đầu/kết thúc (nil = bỏ qua); chạy trong goroutine của Runner.
	OnStep func(StepEvent)
	// Context dựng gói ngữ cảnh cho mục tiêu (nil = không chèn).
	Context func(ctx context.Context, goal string, paths []string) (ctxengine.Pack, error)
	// Logf in tiến trình cho người dùng.
	Logf func(format string, a ...any)
	// SubagentUsage trả token của subagent agentID (driver session) cùng lý do khi không đọc được; nil = đọc theo
	// hook (runtime/subagents/<agent_id>.json, dự phòng tệp spool chờ) rồi transcript. Seam cho kiểm thử.
	SubagentUsage func(agentID string) (*claude.Usage, string)
	// BintExe là đường dẫn binary Bint đặt vào cấu hình MCP của bước (rỗng = os.Executable()).
	BintExe string
}

// StepEvent báo một bước agent của lượt điều phối. Log là đường dẫn log của bước (ghi dần khi agent chạy).
type StepEvent struct {
	TaskID string
	Step   string
	RunID  string // lượt con của bước
	Log    string
	Done   bool   // false: vừa bắt đầu; true: vừa kết thúc
	Status string // chỉ khi Done: completed | failed | interrupted
}

// RunOptions điều khiển một lượt điều phối.
type RunOptions struct {
	Workflow string // b-do | b-req | b-feat | b-review, hoặc tên flow tuỳ biến trong .bint/flows
	Request  string
	TaskRef  string
	// Step: chỉ với TaskRef và flow tuỳ biến: chạy đúng một bước (phải là bước kế tiếp của tác vụ) cùng cổng sau bước rồi
	// dừng. Driver session dùng cho bước gán harness khác Claude (`bint run <flow> --task <id> --step <bước>`).
	Step string
	Plan *bool
	// Fast: chọn đường nhanh của b-do (--fast): lúc tự lập phiếu, hoặc với TaskRef khi phiếu còn created. Chỉ b-do gốc; không đi cùng
	// Plan/Depth. Phạm vi chạm đường dẫn bảo vệ thì giữ b-req.
	Fast bool
	// Depth: ""/auto = tự chọn (hoặc giữ độ sâu đã lưu); standard|deep = người dùng chỉ định.
	Depth    string
	NoReview bool
	Edit     TicketEdit
	// Resume: tiếp tục tác vụ đang blocked/interrupted/failed.
	Resume bool
	// NoWait: khoá worktree đang bị giữ thì dừng ngay (blocked) thay vì chờ tới workflow.timeout_minutes.
	NoWait bool
	// Rerun (cùng Resume): bỏ kết quả bước chỉ đọc đang giữ (HeldStep) và chạy lại bước đó thay vì chỉ áp cổng sau bước
	// của nó (`bint run resume --rerun`). Lỗi nếu phiếu không có bước giữ.
	Rerun bool
	// Fresh: bỏ nối tiếp phiên cũ của bước dừng gần nhất (driver process: `--fresh` của resume và run <workflow> --task): bước chạy
	// mới với đề bài đầy đủ dù nối tiếp được.
	Fresh bool
	// Answer (cùng Resume): câu trả lời của người cho b-feat đang chờ (câu hỏi `bint task error --question` hoặc b-feat không đổi
	// tệp nào): ghi vào phiếu rồi chạy lại b-feat trên chính tác vụ, đề bài mang nguyên văn câu hỏi và câu trả lời.
	Answer string
	// Accept (cùng Resume): tác vụ đang chờ sau khi hết vòng sửa — chấp nhận kết quả như hiện tại, vào thẳng finalize.
	// FixRound: cho thêm đúng một vòng sửa (b-feat rồi b-review). Chỉ người dùng được (app kiểm), loại trừ nhau.
	Accept, FixRound bool
	// Isolation: chế độ cô lập khi tự lập phiếu (""/auto | shared | worktree); không dùng cùng TaskRef.
	Isolation string
	// Driver: process|session khi tự lập phiếu (--driver); không dùng cùng TaskRef. session ⇒ lập phiếu rồi dừng
	// (Result.Status = created): các bước do phiên điều phối chạy bằng `bint run next`.
	Driver string
	// AgentChoices là lựa chọn hiệu lực được chụp khi tạo lượt đầu; resume tái sử dụng.
	AgentChoices map[string]task.AgentChoice
	// ResolveAgentChoices runs after Classify and planning overrides, before
	// claiming the task. It lets callers select only roles this run may use; the flow is the one being run (zero
	// Def when a custom flow cannot be loaded: the run then stops as blocked before any step).
	ResolveAgentChoices func(*task.Task, flow.Def) (map[string]task.AgentChoice, error)
}

// StepResult là kết quả một bước agent.
type StepResult struct {
	Step     string   `json:"step"`
	RunID    string   `json:"run_id"`
	Harness  string   `json:"harness,omitempty"`
	Model    string   `json:"model,omitempty"`
	Status   string   `json:"status"` // completed | failed | interrupted
	CostUSD  *float64 `json:"cost_usd"`
	Duration string   `json:"duration"`
	Error    string   `json:"error,omitempty"`
	// HumanWrites: tệp bị đổi trong bước chỉ đọc nhưng được miễn vì người ghi qua Bint (nhật ký ghi).
	HumanWrites []string `json:"human_writes,omitempty"`
	// StopClass là lớp lý do dừng (ClassifyStop; rỗng = hoàn thành bình thường), Continuable: có thể nối tiếp phiên
	// (lớp cho phép, có handle và fingerprint), SessionID: handle phiên harness của bước.
	StopClass   string `json:"stop_class,omitempty"`
	Continuable bool   `json:"continuable,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	// Profile là profile `bint iso` của bước (<tool>/<tên>); rỗng = không dùng profile.
	Profile string `json:"profile,omitempty"`
}

// Result là kết quả một lượt điều phối.
type Result struct {
	TaskID string       `json:"task_id"`
	RunID  string       `json:"run_id"`
	Status string       `json:"task_status"`
	Reason string       `json:"reason,omitempty"`
	Steps  []StepResult `json:"steps"`
	// CostUSD là tổng chi phí của lượt điều phối, chỉ khi MỌI bước đều báo chi phí; nếu có
	// bước không báo (vd. Codex) thì null, phần đã biết nằm ở CostPartialUSD.
	CostUSD        *float64 `json:"cost_usd"`
	CostPartialUSD *float64 `json:"cost_usd_partial,omitempty"`
	// Questions: câu hỏi chờ người trả lời (tác vụ blocked).
	Questions []string `json:"questions,omitempty"`
	Notes     []string `json:"notes,omitempty"`
	// Choices: các lựa chọn bằng lệnh cho người khi tác vụ blocked chờ quyết định (trả lời b-feat; chấp nhận / sửa thêm / huỷ).
	Choices []Choice `json:"choices,omitempty"`
	// PlanningIssues: lỗi cấu trúc artifact b-req (hợp đồng v1) khiến tác vụ dừng.
	PlanningIssues []task.ArtifactIssue `json:"planning_issues,omitempty"`
	// Isolation là chế độ cô lập của tác vụ; Worktree/Branch/Origin chỉ có khi tác vụ chạy trong worktree riêng
	// (Origin: cây gốc nhận thay đổi); Landed: đã đưa về cây gốc.
	Isolation   string   `json:"isolation,omitempty"`
	Worktree    string   `json:"worktree,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	Landed      bool     `json:"landed,omitempty"`
	LandedFiles []string `json:"landed_files,omitempty"`
	// LastGreen và OpenFindings chỉ có khi tác vụ dừng (blocked/interrupted/failed): điểm verify xanh cuối kèm tệp đã đổi từ đó,
	// và phát hiện còn mở của vòng review mới nhất, để người quyết định giao hay quay về (bint task restore-green).
	LastGreen    *GreenInfo    `json:"last_green,omitempty"`
	OpenFindings []OpenFinding `json:"open_findings,omitempty"`
}

type runState struct {
	opts RunOptions
	// flow là định nghĩa flow của workflow đang chạy (nhúng hoặc tuỳ biến): nguồn của kind bước, thứ tự bước, chế độ chỉ
	// đọc, role và cổng sau bước.
	flow flow.Def
	// flowBlock khác rỗng: flow của tác vụ đã đổi/không còn dùng được, Run chặn tác vụ (blocked) trước khi chạy bước nào.
	flowBlock string
	t         *task.Task
	runID     string
	res       *Result
	spent     float64
	costKnown bool
	// costMissing: có bước chạy xong mà harness không báo chi phí.
	costMissing bool
	// turns: tổng số lượt agent đã dùng trong lượt điều phối (chỉ tính bước báo num_turns).
	turns        int64
	limit        config.PerRunLimits
	ran          map[string]int
	warnedNoCost bool
	// warnedNoTurns: đã ghi chú harness không áp được max_turns.
	warnedNoTurns bool
	// featScope*: tập tệp phạm vi tính ngay sau b-feat khi còn giữ khoá độc quyền (phát hiện .8, mục 12.5);
	// checkScope dùng rồi xoá cờ. featScopeSet=false ⇒ checkScope tự tính dưới khoá chia sẻ.
	featScope    []string
	featScopeErr error
	featScopeSet bool
	// session: lệnh của driver session (bint run next/complete); subagent không có BINT_RUN_ID.
	session bool
	// heldRunID: lượt con của bước đang giữ vừa được nhận để áp cổng sau bước (takeHeld); rs.res.Steps rỗng khi resume.
	heldRunID string
	// finalizeResume: resume sau khi finalize bị chặn vì thiếu bằng chứng và worktree không đổi kể từ lúc chặn: vào thẳng
	// finalize (không mở b-review mới) và chạy lại lệnh kiểm chứng đúng một lần.
	finalizeResume bool
	// verifiedResume: resume vòng sửa vì kiểm chứng đầy đủ ở finalize hỏng khi lần kiểm chứng mới nhất đã qua đầy đủ trên
	// đúng cây hiện tại (markVerifiedFixResume): vào thẳng finalize, không kiểm chứng lại.
	verifiedResume bool
	// last là bước vừa kết thúc trong lượt này (endStep ghi): cổng chờ người sau bước (markAwaitingHuman) dùng lại handle.
	last *endedStep
	// planCont: tác vụ planned đang chờ câu trả lời của b-req và planner cũ nối tiếp được (planContinuation): pickStep trả bước
	// plan thay vì bước implement; xoá khi bước plan mở.
	planCont bool
	// scopeFiles: tập tệp của tác vụ do checkScope vừa tính (rỗng = tác vụ chưa đổi tệp nào): cổng implementGate dùng.
	scopeFiles []string
	// acceptReview: resume --accept: vào thẳng finalize, không chạy bước nào.
	acceptReview bool
}

// endedStep là handle phiên của bước vừa kết thúc, giữ để cổng sau bước ghi awaiting_human vào State.LastStep.
type endedStep struct {
	step, runID, harness, handle string
	// profile: profile `bint iso` của bước (<tool>/<tên>), rỗng nếu không dùng.
	profile string
	// Nối tiếp phiên (driver session): số lần nối tiếp, lượt đầu của agent, usage cộng dồn lúc đóng.
	continuations int
	originRunID   string
	usage         *task.UsageMark
	// noSession khác rỗng: tệp phiên của harness không có trên đĩa lúc bước dừng ⇒ không nối tiếp được (lý do).
	noSession string
}

// afterStepUnlock là seam kiểm thử (nil ở sản phẩm): gọi ngay sau khi runStep nhả khoá worktree của bước,
// tức thời điểm một b-feat của tác vụ khác có thể giành khoá.
var afterStepUnlock func(step string)

func (r *Runner) logf(format string, a ...any) {
	if r.Logf != nil {
		r.Logf(format, a...)
	}
}

// Run điều phối một workflow (mục 6.4). b-do: Classify → b-req (nếu cần) → b-feat →
// b-review (tối đa workflow.max_review_rounds vòng sửa) → Finalize. Các workflow
// đơn (b-req, b-feat, b-review) chạy đúng một bước trên tác vụ.
func (r *Runner) Run(ctx context.Context, o RunOptions) (*Result, error) {
	if o.Rerun && !o.Resume {
		return nil, errors.New("--rerun chỉ dùng khi tiếp tục tác vụ (bint run resume <id> --rerun)")
	}
	if o.Step != "" && (o.TaskRef == "" || !boundFlow(o.Workflow)) {
		return nil, errors.New("--step chỉ dùng với --task và flow tuỳ biến hoặc b-debug (workflow gốc khác chạy theo tên: bint run <workflow> --task <id>)")
	}
	// Mọi thay đổi Env của lượt này (Source, RunID, Root khi vào worktree) nằm trên bản sao, trả lại Env gốc khi xong
	// để Runner/TaskWS dùng lại được cho lượt sau (nếu không RunID còn sót lại làm lượt sau bị coi là chạy lồng).
	origEnv := r.Env
	cp := *origEnv
	r.Env = &cp
	defer func() { r.Env = origEnv }()
	r.Source = "bint-run"
	// Flow của workflow: nhúng, hoặc tuỳ biến từ .bint/flows (đã qua flow.Validate). Tác vụ có sẵn của flow tuỳ biến mà flow
	// không còn dùng được thì bị chặn (blocked) thay vì lỗi, để người khôi phục flow rồi resume.
	fd, ferr := r.resolveFlow(o.Workflow)
	if ferr != nil && (o.TaskRef == "" || !isCustomFlow(o.Workflow)) {
		return nil, ferr
	}
	rs := &runState{opts: o, flow: fd, runID: ulid.New(), res: &Result{Steps: []StepResult{}}, ran: map[string]int{},
		limit: r.Cfg.Limits.ForWorkflow(o.Workflow)}
	if ferr != nil {
		rs.flow = flow.Def{Name: o.Workflow}
	}
	rs.res.RunID = rs.runID

	// Classify: lập phiếu hoặc nạp tác vụ có sẵn.
	var assess task.Assessment
	if o.TaskRef == "" {
		if strings.TrimSpace(o.Request) == "" {
			return nil, errors.New("thiếu mô tả yêu cầu (hoặc --task <id>)")
		}
		saved := r.RunID
		r.RunID = ""
		t, a, err := r.NewTask(ctx, NewTaskOptions{Request: o.Request, Workflow: o.Workflow, Edit: o.Edit, Plan: o.Plan, Depth: o.Depth, Fast: o.Fast,
			Isolation: o.Isolation, Driver: o.Driver, DriverFlagOnly: true})
		r.RunID = saved
		if err != nil {
			return nil, err
		}
		rs.t, assess = t, a
		r.logf("Tác vụ %s: %s", t.State.ID, t.Ticket.Goal)
		if c := t.State.Classification; c != nil && fd.PlanOptional() {
			r.logf("Classify: %s", c.Reason)
			if c.Plan && c.Depth != "" {
				r.logf("Phân tích b-req: %s (%s)", c.Depth, c.DepthReason)
			}
		}
		if c := t.State.Classification; c != nil {
			r.logf("Cô lập: %s (%s)", c.EffectiveIsolation(), c.IsolationReason)
			if c.EffectiveDriver() == task.DriverSession {
				r.logf("Driver: session (%s)", c.DriverReason)
			}
		}
	} else {
		if strings.TrimSpace(o.Isolation) != "" {
			if iso, err := NormalizeIsolationFlag(o.Isolation); err != nil || iso != "" {
				return nil, errors.New("--worktree/--shared chỉ chọn lúc lập phiếu: không dùng cùng --task (chế độ đã ghi trong phiếu)")
			}
		}
		if drv, err := NormalizeDriverFlag(o.Driver); err != nil || drv != "" {
			return nil, errors.New("--driver chỉ chọn lúc lập phiếu: không dùng cùng --task (driver đã ghi trong phiếu)")
		}
		t, err := r.Tasks.Load(o.TaskRef)
		if err != nil {
			return nil, err
		}
		t = r.Reconcile(t)
		if task.Terminal(t.State.Status) {
			return nil, fmt.Errorf("tác vụ %s đã %s", t.State.ID, t.State.Status)
		}
		if err := checkFlowSwitch(t, o.Workflow); err != nil {
			return nil, err
		}
		if ferr != nil {
			rs.flowBlock = flowBlockReason(t, o.Workflow, fmt.Sprintf("flow %s không còn dùng được: %v", o.Workflow, ferr))
		} else if isCustomFlow(o.Workflow) && t.State.Workflow == o.Workflow {
			_, rs.flowBlock = r.flowGate(t)
		}
		if ps := t.State.PendingStep; ps != nil {
			// Bước đang chạy bằng subagent (driver session): không chạy chồng lên nó. Hết hạn thì đóng như bước quá giờ
			// (tác vụ failed), rồi đi tiếp theo trạng thái mới.
			if t.State.PendingActive(leaseNow()) {
				return nil, &PendingStepError{TaskID: t.State.ID, Step: ps.Step, RunID: ps.RunID, Until: ps.LeaseUntil}
			}
			if _, err := r.Complete(ctx, CompleteOptions{TaskRef: t.State.ID, RunID: ps.RunID, NoWait: o.NoWait}); err != nil {
				return nil, err
			}
			if t, err = r.Tasks.Load(o.TaskRef); err != nil {
				return nil, err
			}
			t = r.Reconcile(t)
		}
		planningState := t.State.Status
		if o.Resume && task.Paused(planningState) {
			planningState = t.State.ResumeFrom
			if planningState == "" {
				planningState = task.StatusCreated
			}
		}
		if fd.OnlyKind(flow.KindPlan) && !slices.Contains([]string{task.StatusCreated, task.StatusPlanned}, planningState) {
			return nil, fmt.Errorf("b-req chỉ chạy trên tác vụ created/planned (đang %s)", t.State.Status)
		}
		if !o.Resume && task.Paused(t.State.Status) {
			return nil, fmt.Errorf("tác vụ %s đang %s (%s): dùng `%s`", t.State.ID, t.State.Status, t.State.Reason, t.State.ResumeCommand())
		}
		if !o.Resume && (o.Answer != "" || o.Accept || o.FixRound) {
			return nil, errors.New("--answer/--accept/--fix-round chỉ dùng khi tiếp tục tác vụ (bint run resume <id>)")
		}
		if err := checkResumeFlags(t, o.Answer, o.Accept, o.FixRound); err != nil {
			return nil, err
		}
		if rs.flowBlock == "" {
			if t, err = r.applyOverrides(t, o, fd); err != nil {
				return nil, err
			}
		}
		rs.t, assess = t, r.Assess(t)
		if o.Step != "" && rs.flowBlock == "" {
			if err := r.checkStepRequest(rs, o); err != nil {
				return nil, err
			}
		}
	}
	rs.res.TaskID = rs.t.State.ID
	rs.res.Notes = assess.Notes
	if o.ResolveAgentChoices != nil {
		choices, err := o.ResolveAgentChoices(rs.t, rs.flow)
		if err != nil {
			return nil, err
		}
		o.AgentChoices = choices
	}
	if o.TaskRef == "" && rs.t.State.Classification != nil && rs.t.State.Classification.EffectiveDriver() == task.DriverSession {
		if len(o.AgentChoices) > 0 {
			t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
				t.State.AgentChoices = cloneAgentChoices(o.AgentChoices)
				return nil
			})
			if err != nil {
				return nil, err
			}
			rs.t = t
		}
		rs.res.Status, rs.res.Isolation = rs.t.State.Status, rs.t.State.Isolation()
		rs.res.Notes = append(assess.Notes, "driver session: tiếp bằng `bint run next "+rs.t.State.ID+" --json`")
		return rs.res, nil
	}

	// Root của tác vụ (State.Worktree): chạy ở đó dù đang đứng ở thư mục khác. Dùng bản sao Env để
	// không đổi Root của Env dùng chung (TaskWS); trả lại khi xong để Runner dùng lại được.
	taskRoot, rootErr := r.RootFor(rs.t)
	if rootErr == nil && taskRoot != r.Root {
		r.Env.Root = taskRoot // bản sao Env của lượt này (xem đầu Run)
		r.logf("chạy trong %s", taskRoot)
	}

	// Nhận quyền điều phối tác vụ và mở lượt chạy.
	if runAfterLoad != nil {
		runAfterLoad()
	}
	parent := rs.t.State.LastRun()
	var rerunDropped *task.HeldStep
	t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		// Kiểm lại dưới khoá phiếu: một `bint run next` có thể đã mở bước subagent sau lần nạp ở trên (TOCTOU), và hợp
		// đồng của chính tác vụ không chặn AcquireLock ⇒ không được chạy chồng lên bước đó.
		if ps := t.State.PendingStep; ps != nil && t.State.PendingActive(leaseNow()) {
			return &PendingStepError{TaskID: t.State.ID, Step: ps.Step, RunID: ps.RunID, Until: ps.LeaseUntil}
		}
		if o.Rerun {
			if h, err := dropHeldForRerun(t, o.Resume); err != nil {
				return err
			} else {
				rerunDropped = h
			}
		}
		if err := t.ClaimRunner(rs.runID, r.now()); err != nil {
			return err
		}
		if len(o.AgentChoices) > 0 {
			t.State.AgentChoices = cloneAgentChoices(o.AgentChoices)
		}
		// Tác vụ lập bởi workflow b-req nhưng nay do b-do/b-feat/b-review (hoặc một flow tuỳ biến) điều phối: `resume`
		// phải đi theo workflow này, nếu không sẽ báo "b-req chỉ chạy trên created/planned" và không có đường tiếp
		// tục. Đích là flow tuỳ biến thì ghi băm của nó từ lúc này (cùng ý nghĩa với băm lúc lập phiếu).
		if builtinFlow(t.State.Workflow).OnlyKind(flow.KindPlan) && o.Workflow != t.State.Workflow {
			t.State.Workflow = o.Workflow
			t.State.FlowSHA = ""
		}
		// Phiếu của flow tuỳ biến chưa có băm (vừa chuyển từ b-req, hoặc lần chuyển trước gặp flow hỏng nên chưa thể ghi):
		// ghi băm ở lần đầu flow nạp được, để cổng "flow đổi giữa chừng" có hiệu lực từ đó (không mất vĩnh viễn).
		if isCustomFlow(t.State.Workflow) && t.State.Workflow == o.Workflow && t.State.FlowSHA == "" && rs.flowBlock == "" {
			t.State.FlowSHA = flow.Hash(fd)
		}
		t.State.LinkRun(rs.runID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	rs.t = t
	if rerunDropped != nil {
		rs.res.Notes = append(rs.res.Notes, rerunNote(rerunDropped))
	}
	r.RunID = rs.runID
	r.emitRun(rs.runID, spool.WorkflowStarted, map[string]any{"workflow": o.Workflow, "parent_run_id": parent,
		"bint_version": r.Version, "git_head": gitrepo.HeadCommit(ctx, r.Root), "git_branch": gitrepo.Branch(ctx, r.Root),
		"task_id": rs.t.State.ID})
	defer r.finish(rs)

	// Paused → tiếp tục từ trạng thái trước đó.
	if task.Paused(rs.t.State.Status) {
		to, why := r.resumeTarget(rs, o.Accept, o.FixRound)
		if err := r.transition(rs, to, why); err != nil {
			return rs.res, err
		}
		r.applyResume(rs, o.Answer, o.FixRound, rs.reviewFixStep(rs.currentReview()))
		if o.Accept {
			rs.acceptReview = true
		} else {
			r.markFinalizeResume(ctx, rs, to, o.Rerun)
			r.markVerifiedFixResume(ctx, rs, to, o.Rerun)
		}
	}
	if rs.flowBlock != "" {
		// Flow đã đổi giữa chừng (hoặc không còn dùng được): không chạy bước nào, người khôi phục flow rồi resume.
		r.block(rs, rs.flowBlock)
		return rs.res, nil
	}
	if rootErr != nil {
		// Bảng chuyển không cho blocked/interrupted sang failed: đã về trạng thái hoạt động ở trên.
		r.fail(rs, "", "thư mục làm việc của tác vụ không còn: "+rootErr.Error())
		return rs.res, nil
	}
	// Tác vụ ở chế độ worktree: tạo worktree ở lượt đầu (mọi workflow), từ đây mọi bước chạy trong đó.
	if !r.ensureWorktree(ctx, rs) {
		return rs.res, nil
	}
	r.armPlanContinuation(ctx, rs)

	// Chỉ hỏi lại khi thiếu trường quan trọng cho tác vụ rủi ro cao (mục 6.7).
	if len(assess.Questions) > 0 && strings.TrimSpace(rs.t.Ticket.Verify.Command) == "" {
		if !r.askVerify(rs, assess) {
			return rs.res, nil
		}
	}

	if rs.acceptReview {
		// --accept: người chấp nhận kết quả sau khi hết vòng sửa: vào thẳng finalize (cổng done, kiểm chứng lại khi cây đổi và land
		// giữ nguyên), không chạy b-feat hay b-review nào. finalize trả true khi kiểm chứng đầy đủ hỏng và còn vòng sửa: đi tiếp.
		rs.acceptReview = false
		if !r.finalize(ctx, rs) {
			return rs.res, nil
		}
	}
	for guard := 0; guard < rs.loopGuard(r.fixCeiling(rs)); guard++ {
		if ctx.Err() != nil {
			r.interrupt(rs, "người dùng huỷ")
			return rs.res, nil
		}
		step := r.nextStep(rs)
		if step == "" {
			break
		}
		if step == stepHeld {
			// Bước chỉ đọc đã xong ở lượt trước (cây đổi trong lúc chạy, người đã xem rồi resume): chỉ áp cổng sau bước.
			if !r.afterHeld(ctx, rs, r.takeHeld(rs).Step) {
				break
			}
			if o.Step != "" {
				break
			}
			continue
		}
		if step == stepGoalCheck {
			// b-goal đã chạy ở lượt trước (người vừa duyệt phạm vi); chỉ còn đóng băng và cổng mốc.
			if !r.afterGoal(ctx, rs) {
				break
			}
			if o.Step != "" {
				break
			}
			continue
		}
		if step == stepCheck {
			// Bước implement đã chạy ở lượt trước; chỉ còn kiểm tra phạm vi và kiểm chứng.
			if !r.afterStep(ctx, rs, rs.currentImplement().ID) {
				break
			}
			if o.Step != "" {
				break
			}
			continue
		}
		if !r.runStep(ctx, rs, step) {
			break
		}
		if !r.afterStep(ctx, rs, step) {
			break
		}
		if o.Step != "" {
			break // --step: đúng một bước và cổng sau bước của nó
		}
	}
	return rs.res, nil
}

// loopGuard là trần số vòng của vòng điều phối (chống vòng lặp vô hạn do lỗi): mỗi vòng sửa sau review có thể chạy lại
// mọi bước implement và review.
func (rs *runState) loopGuard(maxReviewRounds int) int {
	return 4 + max(3, 2*rs.countKind(flow.KindImplement)+rs.countKind(flow.KindReview))*(maxReviewRounds+1)
}

// checkFlowSwitch kiểm luật đổi workflow khi chạy tác vụ có sẵn: tác vụ lập cho flow gắn phiếu (flow tuỳ biến, b-debug;
// xem boundFlow) chỉ chạy bằng đúng flow đó (bint run <flow> --task, resume); flow gắn phiếu chỉ nhận tác vụ của chính nó hoặc tác vụ mới chỉ lập kế hoạch (b-req).
// Workflow gốc với nhau thì giữ nguyên hành vi cũ (ví dụ b-feat --task trên tác vụ b-do).
func checkFlowSwitch(t *task.Task, workflow string) error {
	tw := t.State.Workflow
	switch {
	case tw == "" || tw == workflow:
		return nil
	case builtinFlow(tw).OnlyKind(flow.KindPlan):
		return nil
	case boundFlow(tw):
		return fmt.Errorf("tác vụ %s thuộc flow %s, không chạy được bằng %s giữa chừng: `bint run %s --task %s` hoặc `bint run resume %s`",
			t.State.ID, tw, workflow, tw, t.State.ID, t.State.ID)
	case boundFlow(workflow):
		return fmt.Errorf("tác vụ %s thuộc workflow %s, không chạy được bằng flow %s (flow tuỳ biến và b-debug chỉ nhận tác vụ lập cho chính nó, "+
			"hoặc tác vụ mới chỉ lập kế hoạch bằng b-req)", t.State.ID, tw, workflow)
	}
	return nil
}

// checkStepRequest kiểm --step: phải đúng bước kế tiếp của tác vụ theo trạng thái sẽ có sau khi tiếp tục (tác vụ đang
// dừng thì tính từ ResumeFrom). Không đổi gì trên tác vụ.
func (r *Runner) checkStepRequest(rs *runState, o RunOptions) error {
	cp := *rs.t
	if task.Paused(cp.State.Status) {
		cp.State.Status = cp.State.ResumeFrom
		if cp.State.Status == "" {
			cp.State.Status = task.StatusCreated
		}
	}
	probe := *rs
	probe.t = &cp
	if want, _ := probe.pickStep(); want != o.Step {
		if want == "" || want == stepCheck || want == stepGoalCheck {
			want = "(chưa có bước agent nào: " + rs.t.State.Status + ")"
		}
		return fmt.Errorf("--step %s: bước kế tiếp của tác vụ %s là %s", o.Step, rs.t.State.ID, want)
	}
	return nil
}

func cloneAgentChoices(in map[string]task.AgentChoice) map[string]task.AgentChoice {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]task.AgentChoice, len(in))
	for role, choice := range in {
		out[role] = choice
	}
	return out
}

// applyOverrides áp --plan/--no-plan/--depth lên tác vụ có sẵn và nâng/hạ hợp đồng artifact.
//   - --depth tường minh chỉ nhận khi tác vụ `created`, hoặc khi `b-req --task` chạy lại trên `planned`;
//     từ in_progress trở đi trả lỗi thay vì lặng lẽ đổi độ sâu giữa lượt. Hàm ý Plan=true.
//   - Độ sâu đã lưu được giữ nguyên; chỉ tính lại khi người dùng đổi Plan lúc tác vụ còn `created`.
//   - Phiên bản hợp đồng chỉ hạ về 0 khi tác vụ còn `created` (chưa có artifact để mất).
func (r *Runner) applyOverrides(t *task.Task, o RunOptions, fd flow.Def) (*task.Task, error) {
	depth, err := task.NormalizeDepth(o.Depth)
	if err != nil {
		return t, err
	}
	explicit := depth != ""
	planning, planOnly := fd.HasKind(flow.KindPlan), fd.OnlyKind(flow.KindPlan)
	if err := checkFastFlag(o.Fast, o.Workflow, fd.IsBuiltin(), o.Plan, depth); err != nil {
		return t, err
	}
	if o.Fast && t.State.Status != task.StatusCreated {
		return t, fmt.Errorf("--fast chỉ nhận khi tác vụ còn created (đang %s): đường nhanh được chọn một lần lúc lập phiếu", t.State.Status)
	}
	if explicit {
		if !planning {
			return t, fmt.Errorf("--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)", o.Workflow)
		}
		if o.Plan != nil && !*o.Plan {
			return t, errors.New("--depth standard|deep hàm ý có b-req: không đi cùng --no-plan")
		}
		if st := t.State.Status; st != task.StatusCreated && !(planOnly && st == task.StatusPlanned) {
			return t, fmt.Errorf("không đổi được độ sâu phân tích khi tác vụ đang %s: --depth chỉ nhận khi tác vụ còn created, "+
				"hoặc để lập kế hoạch lại bằng `bint run b-req --task %s --depth %s` trên tác vụ planned", st, t.State.ID, depth)
		}
	}
	if !explicit && o.Plan == nil && !o.Fast && !(planOnly && t.State.Status == task.StatusCreated) && !hasEdit(o.Edit) {
		return t, nil
	}
	var sync criteriaSync
	nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
		if hasEdit(o.Edit) {
			if task.Terminal(t.State.Status) {
				return fmt.Errorf("tác vụ đã %s", t.State.Status)
			}
			if !r.Human && o.Edit.Autonomy != nil && *o.Edit.Autonomy == task.AutonomyAuto {
				return fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", ErrAgent)
			}
			before := slices.Clone(t.Ticket.Verify.Criteria)
			if err := o.Edit.apply(&t.Ticket); err != nil {
				return err
			}
			if err := r.syncCriteria(t, before, &sync); err != nil {
				return err
			}
		}
		if o.Fast {
			r.setFastFlag(t)
		}
		if hasEdit(o.Edit) && o.Plan == nil && !explicit {
			r.dropFastIfProtected(t)
		}
		if !explicit && o.Plan == nil && !(planOnly && t.State.Status == task.StatusCreated) {
			return nil
		}
		created := t.State.Status == task.StatusCreated
		cls := task.Classification{Plan: true, Reason: "mặc định"}
		if t.State.Classification != nil {
			cls = *t.State.Classification
		}
		if o.Plan != nil || explicit || (planOnly && created) {
			cls.Fast = false // cờ chỉ định (--plan/--no-plan/--depth) hoặc b-req chạy trên phiếu created: đường đầy đủ (không phải đường nhanh)
		}
		if o.Plan != nil {
			cls.Plan, cls.Reason = *o.Plan, r.chooser()+" chỉ định"
			if a := r.Assess(t); !cls.Plan && !r.Human && created && a.Risk == "high" && planning {
				protected := protectedPlan(a.Protected, true)
				cls.Plan, cls.Reason = protected.Plan, protected.Reason
			}
		}
		switch {
		case explicit:
			cls.Plan = true
			if o.Plan == nil {
				cls.Reason = r.chooser() + " chỉ định độ sâu " + depth
			}
			cls.Depth, cls.DepthReason = depth, task.DepthReasonUser
		case o.Plan != nil && created && !cls.Plan:
			cls.Depth, cls.DepthReason = "", ""
		case o.Plan != nil && created && cls.Depth == "":
			req := strings.TrimSpace(t.Ticket.Request)
			if req == "" {
				req = t.Ticket.Goal
			}
			cls.Depth, cls.DepthReason = task.ChoosePlanningDepth(req, r.Assess(t))
		}
		t.State.Classification = &cls
		if created && planning && (o.Plan != nil || explicit) {
			if cls.Plan {
				ensurePlanningContract(t)
			} else {
				t.State.PlanningContractVersion = 0
			}
		}
		if planOnly && created {
			ensurePlanningContract(t)
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	r.emitSynced(nt, sync)
	for _, n := range sync.notes {
		r.logf("đồng bộ tiêu chí: %s", n)
	}
	return nt, nil
}

func hasEdit(e TicketEdit) bool {
	return e.Goal != nil || e.VerifyCommand != nil || len(e.Criteria) > 0 || e.ClearCriteria || len(e.Allow) > 0 ||
		len(e.Deny) > 0 || e.ClearScope || len(e.Constraints) > 0 || e.Autonomy != nil
}

// armPlanContinuation: tác vụ planned đang chờ câu trả lời của b-req (LastStep awaiting_human của bước plan, requirement.md đã có
// câu trả lời, hết câu hỏi mở) chạy bằng flow nhiều bước (b-do): planner cũ được nối tiếp trước khi sang bước implement
// (rs.planCont ⇒ pickStep trả bước plan) thay vì vào thẳng b-feat. Không nối tiếp được thì giữ hành vi cũ và ghi lý do. Workflow chỉ
// có bước plan (b-req --task) không cần cờ: runStep tự quyết định nối tiếp.
func (r *Runner) armPlanContinuation(ctx context.Context, rs *runState) {
	plan, ok := rs.flow.FirstOfKind(flow.KindPlan)
	if !ok || !rs.chain() || (rs.opts.Step != "" && rs.opts.Step != plan.ID) {
		return
	}
	cd, ok := r.planContinuation(ctx, rs, rs.opts.Fresh, false, rs.t.State.Status)
	switch {
	case !ok:
	case cd.Continue:
		rs.planCont = true
	case cd.Why != "":
		rs.res.Notes = append(rs.res.Notes, "không nối tiếp planner cũ: "+cd.Why)
	}
}

// nextStep chọn bước tiếp theo theo trạng thái tác vụ và cấu trúc của flow đang chạy (kind của bước, bước plan có thể
// bỏ qua, flow một hay nhiều bước, bước implement/review đang chạy theo State.FlowStep). Trả "" khi không còn bước agent
// nào (kể cả sau khi finalize), stepCheck khi bước implement vừa chạy chỉ còn cổng sau bước.
func (r *Runner) nextStep(rs *runState) string {
	if rs.t.State.HeldStep != nil && rs.heldStepOf() == nil {
		r.takeHeld(rs) // bước giữ của flow khác: bỏ kèm ghi chú
	}
	step, fin := rs.pickStep()
	if fin {
		if r.finalize(context.Background(), rs) {
			return r.nextStep(rs) // kiểm chứng đầy đủ ở finalize hỏng: tác vụ đã về bước sửa
		}
		return ""
	}
	if step == stepCheck && !rs.chain() {
		rs.ran[rs.flow.Steps[0].ID]++
	}
	return step
}

// pickStep là phần không có tác dụng phụ của nextStep: bước kế tiếp, hoặc finalize=true khi flow đã xong mọi bước agent
// (tác vụ đã qua review, bỏ review, hoặc chỉ còn việc đưa về cây gốc).
func (rs *runState) pickStep() (step string, finalize bool) {
	st := rs.t.State.Status
	fd := rs.flow
	if rs.heldStepOf() != nil {
		return stepHeld, false // bước chỉ đọc đã xong, chỉ còn cổng sau bước của nó
	}
	if !rs.chain() {
		// Flow một bước: chạy đúng bước đó một lần. Bước implement vừa chạy xong ở lượt trước (AwaitingCheck) chỉ còn
		// cổng sau bước.
		if len(fd.Steps) == 0 {
			return "", false
		}
		only := fd.Steps[0]
		if only.Kind == flow.KindImplement && rs.ran[only.ID] == 0 && rs.t.State.AwaitingCheck && st == task.StatusInProgress {
			return stepCheck, false
		}
		if rs.ran[only.ID] > 0 {
			return "", false
		}
		return only.ID, false
	}
	plan, hasPlan := fd.FirstOfKind(flow.KindPlan)
	impl, hasImpl := fd.FirstOfKind(flow.KindImplement)
	_, hasReview := fd.FirstOfKind(flow.KindReview)
	if !hasImpl {
		return "", false
	}
	switch st {
	case task.StatusCreated:
		if hasPlan {
			if c := rs.t.State.Classification; c == nil || c.Plan || !plan.Optional {
				return plan.ID, false
			}
		}
		return impl.ID, false
	case task.StatusPlanned, task.StatusInProgress:
		if rs.planCont && st == task.StatusPlanned && hasPlan {
			return plan.ID, false // planner cũ nối tiếp được: nhận câu trả lời trước khi sang bước implement
		}
		if rs.t.State.AwaitingCheck && st == task.StatusInProgress {
			return stepCheck, false
		}
		if st == task.StatusInProgress {
			return rs.currentImplement().ID, false // bước implement đang dở hoặc kế tiếp (nhiều bước implement)
		}
		if rs.t.State.GoalAwaitingCheck && rs.t.State.GoalTest == nil && st == task.StatusPlanned {
			return stepGoalCheck, false // b-goal đã chạy, chỉ còn kiểm phạm vi + đóng băng (goalfreeze.go)
		}
		if rs.goalPending() { // b-do: kiểm tra mục tiêu độc lập viết trước b-feat (goalfreeze.go)
			g, _ := fd.FirstOfKind(flow.KindGoal)
			return g.ID, false
		}
		return impl.ID, false
	case task.StatusInReview:
		if rs.t.State.AwaitingLand || rs.t.State.Landed != nil || rs.finalizeResume || rs.verifiedResume {
			// Đã qua review và cổng done ở worktree, chỉ còn đưa về (xung đột/lỗi lần trước): không review lại.
			return "", true
		}
		if rs.opts.NoReview || !hasReview || rs.t.State.FastFixed {
			return "", true
		}
		return rs.currentReview().ID, false
	}
	return "", false
}

func (r *Runner) transition(rs *runState, to, reason string) error {
	t, _, err := r.SetStatus(context.Background(), rs.t.State.ID, to, TransitionOptions{Reason: reason, Orchestrator: true})
	if t != nil {
		rs.t = t
	}
	return err
}

// block chặn tác vụ chờ người vì lý do bất kỳ; xoá dấu vân tay finalize (FinalizeFP) để lần resume sau không bỏ review
// nhầm. Riêng finalize thiếu bằng chứng đi qua blockFinalize (đặt lại dấu vân tay rồi blockKeep).
func (r *Runner) block(rs *runState, reason string) {
	r.setFinalizeFP(rs, "")
	r.blockKeep(rs, reason)
}

// setFinalizeFP ghi FinalizeFP của phiếu (rỗng = xoá); không làm gì nếu không đổi.
func (r *Runner) setFinalizeFP(rs *runState, fp string) {
	if rs.t.State.FinalizeFP == fp {
		return
	}
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.FinalizeFP = fp; return nil }); err == nil {
		rs.t = t
	}
}

func (r *Runner) blockKeep(rs *runState, reason string) {
	if err := r.transition(rs, task.StatusBlocked, reason); err != nil {
		r.logf("! %v", err)
	}
	rs.res.Reason = reason
	r.logf("⏸ Tác vụ chờ người: %s", reason)
}

func (r *Runner) fail(rs *runState, step, reason string) {
	r.RecordError(rs.t.State.ID, step, reason)
	if err := r.transition(rs, task.StatusFailed, reason); err != nil {
		r.logf("! %v", err)
	}
	rs.res.Reason = reason
}

func (r *Runner) interrupt(rs *runState, reason string) {
	r.RecordError(rs.t.State.ID, "", reason)
	if task.Active(rs.t.State.Status) {
		r.transition(rs, task.StatusInterrupted, reason)
	}
	rs.res.Reason = reason
}

// askVerify hỏi lệnh kiểm chứng cho tác vụ rủi ro cao; không tương tác thì chặn.
func (r *Runner) askVerify(rs *runState, a task.Assessment) bool {
	if r.Ask == nil {
		rs.res.Questions = a.Questions
		r.block(rs, strings.Join(a.Questions, " ")+" — trả lời: bint task set "+rs.t.State.ID+" --verify-command '…' rồi "+r.resumeCmd(rs))
		return false
	}
	ans := strings.TrimSpace(r.Ask(Question{Key: QAskVerifyCommand, Params: map[string]any{"question": a.Questions[0]},
		Text: a.Questions[0] + " (bỏ trống để tiếp tục không có lệnh kiểm chứng)"}))
	if ans == "" {
		rs.res.Notes = append(rs.res.Notes, "người dùng chọn tiếp tục không có lệnh kiểm chứng")
		return true
	}
	t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.Ticket.Verify.Command = ans
		// Người tự gõ lệnh: coi là đã xác nhận.
		t.State.VerifyApproval = &task.Approval{Command: ans, By: r.actor(), At: r.at()}
		return nil
	})
	if err != nil {
		r.fail(rs, "", err.Error())
		return false
	}
	rs.t = t
	return true
}

// budget trả về giới hạn chi phí còn lại của lượt điều phối (nil = không giới hạn).
func (rs *runState) remaining() *float64 {
	if rs.limit.MaxCostUSD == nil {
		return nil
	}
	v := *rs.limit.MaxCostUSD - rs.spent
	return &v
}

func minPtr(a, b *float64) *float64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case *a < *b:
		return a
	}
	return b
}

// stepRun gom trạng thái của một bước agent giữa beginStep và endStep. Driver process giữ nó trong runStep; driver
// session lưu phần cần thiết vào task.PendingStep giữa `bint run next` và `bint run complete` rồi dựng lại.
type stepRun struct {
	step, child, stepID string
	// kind là kind của bước theo flow lúc mở (ghi vào PendingStep.Kind ở driver session); endStep rẽ nhánh theo nó.
	kind flow.Kind
	// role, base: role của bước và role gốc theo kind (chọn quyền MCP của bước driver process); rỗng ở bước dựng lại từ
	// PendingStep (driver session không cấp MCP theo role cho subagent).
	role, base string
	readOnly   bool
	mode       LockMode
	snapBefore string
	featBefore string
	// tasksBefore: tasks.md trước bước implement khi tác vụ v1 đang đạt hợp đồng (guardTasks phục hồi nếu bước làm hỏng).
	tasksBefore       string
	firstFeat         bool
	started           time.Time
	maxCost           *float64
	maxTurns          *int
	reqModel, harness string
	// agent chỉ có ở driver process; driver session không gọi harness từ tiến trình Bint.
	agent eval.Agent
	// prof: profile `bint iso` hiệu lực của agent (driver process; nil = không dùng). Ghi vào handle phiên và quyết định thư mục
	// chứa tệp phiên (prof.ConfigDir()).
	prof    *iso.Profile
	stepLog string
	prompt  string
	lock    *Lock
	// agentID là agent_id của subagent (driver session, từ `complete --agent-id`); ghi vào workflow.finished.
	agentID string
	// onEnd chạy trong endStep khi vẫn giữ khoá, ngay trước khi nhả (driver session: xoá bước dở và hợp đồng thuê).
	onEnd func()
	// timedOut: driver session, hợp đồng thuê đã hết hạn lúc đóng bước.
	timedOut bool
	// sessionID là UUID phiên Bint cấp cho harness (Claude --session-id); rỗng với harness khác.
	sessionID string
	// agentFailed: subagent báo thất bại qua `bint run complete --failed`.
	agentFailed bool
	// Nối tiếp phiên (driver session, dựng lại từ PendingStep): số lần nối tiếp tính cả lần này, lượt đầu của agent, mốc usage
	// trước đó (chi phí chỉ tính phần vượt mốc) và usage cộng dồn đọc được lúc đóng (mốc cho lần nối tiếp sau).
	continuations int
	originRunID   string
	usageBase     *task.UsageMark
	usage         *task.UsageMark
	// fullPrompt: đề bài đầy đủ của bước nối tiếp phiên (driver process; prompt là lời nhắn nối tiếp). Dùng khi harness báo
	// không còn phiên để nối tiếp.
	fullPrompt string
	// noSession: xem endedStep.noSession (driver process, harness claude/codex).
	noSession string
	// isoLeak: lý do hậu kiểm cô lập thấy subagent đã chạy lời gọi ngoài worktree (auditIsolation); khác rỗng ⇒ bước blocked.
	isoLeak string
}

// profileID là <tool>/<tên> của profile `bint iso` của bước ("" nếu không dùng profile).
func (sr *stepRun) profileID() string {
	if sr.prof == nil {
		return ""
	}
	return sr.prof.ID()
}

// release nhả khoá worktree của bước (và seam kiểm thử): gọi đúng một lần khi bước kết thúc hoặc không bắt đầu được.
func (sr *stepRun) release() {
	if sr.lock == nil {
		return
	}
	if sr.onEnd != nil {
		sr.onEnd()
		sr.onEnd = nil
	}
	sr.lock.Release()
	sr.lock = nil
	if afterStepUnlock != nil {
		afterStepUnlock(sr.step)
	}
}

// runStep chạy một bước agent (driver process): beginStep → quyết định nối tiếp phiên → agent.Run → endStep. Trả false nếu
// workflow phải dừng.
//
// Nối tiếp phiên (docs/step-continuation-plan.md giai đoạn 3): bước dừng gần nhất cùng bước, cùng harness (Via = tên harness),
// lớp dừng cho phép, cây không đổi, chưa quá giới hạn và người không yêu cầu --fresh thì gọi harness ở chế độ resume với lời nhắn
// nối tiếp (BINT_RUN_ID vẫn là lượt con mới, bộ đếm và lượt gốc kế thừa); thiếu một điều kiện, hoặc phiên không còn trên đĩa, thì
// chạy mới với đề bài đầy đủ và ghi lý do vào notes. Harness báo không tìm thấy phiên thì chạy lại một lần với đề bài đầy đủ.
func (r *Runner) runStep(ctx context.Context, rs *runState, step string) bool {
	sr, ok := r.beginStep(ctx, rs, step, false)
	if !ok {
		return false
	}
	rs.planCont = false
	env := []string{EnvRunID + "=" + sr.child, EnvTaskID + "=" + rs.t.State.ID, "BINT_AGENT=1",
		task.EnvRunnerPID + "=" + strconv.Itoa(os.Getpid()),
		EnvApprovedPaths + "=" + strings.Join(ApprovedPaths(rs.t), "\n")}
	if sr.kind == flow.KindGoal {
		// Rào thực thi của b-goal (goalfreeze.go): hook không cho đọc plan.md/tasks.md; chỉ bước này có biến.
		env = append(env, hookcore.EnvDenyRead+"="+strings.Join([]string{rs.t.ArtifactPath("plan"), rs.t.ArtifactPath("tasks")}, "\n"))
	}
	// Mọi bước driver process có đúng một MCP server là bint (M0 của docs/mcp-gateway-plan.md).
	mcpPath, mcpTools, cleanMCP := r.stepMCP(sr, rs.t.State.ID)
	defer cleanMCP()
	denySettings, cleanDeny := r.stepDenyReadSettings(sr) // b-goal (Claude): hook cho Grep/Glob để rào BINT_DENY_READ chặn được (mcp.go)
	defer cleanDeny()
	r.armPlanHandoff(ctx, rs, sr)
	last := rs.t.State.LastStep // chụp trước khi xoá (bước thật sự bắt đầu ngay dưới)
	resumeID := r.armProcessContinuation(ctx, rs, sr, last)
	if resumeID == "" && sr.harness == "claude" {
		sr.sessionID = eval.NewSessionUUID() // handle bền kể cả khi tiến trình chết trước khi in JSON
	}
	// Bước thật sự bắt đầu: bước dừng cũ không còn là "gần nhất" (driver session xoá cùng lần ghi PendingStep, grantPending).
	// Không có LastStep thì khỏi ghi (tránh một lần Update vô ích mỗi bước); lỗi ghi vào Notes thay vì nuốt im lặng.
	if rs.t.State.LastStep != nil || (sr.kind == flow.KindImplement && rs.t.State.LastPlan != nil) {
		if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.LastStep = nil
			if sr.kind == flow.KindImplement {
				t.State.LastPlan = nil // b-feat thật sự bắt đầu: kế hoạch đã được giao (hoặc không nối tiếp được, đã ghi lý do)
			}
			return nil
		}); err != nil {
			rs.res.Notes = append(rs.res.Notes, "không xoá được LastStep của phiếu: "+err.Error())
		} else {
			rs.t = nt
		}
	}
	runAgent := func(prompt, sessionID, resume string) eval.AgentResult {
		return sr.agent.Run(ctx, eval.AgentRequest{Dir: r.Root, Prompt: prompt, SessionID: sessionID, ResumeSessionID: resume, Profile: sr.prof,
			Timeout: time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute,
			LogPath: sr.stepLog, Env: env, ReadOnly: sr.readOnly, WritableDirs: r.stateDirs(),
			MCPConfig: mcpPath, ExclusiveMCP: mcpPath != "", MCPTools: mcpTools, Settings: denySettings})
	}
	var res eval.AgentResult
	if resumeID != "" {
		res = runAgent(sr.prompt, "", resumeID)
		if res.ResumeMissing && ctx.Err() == nil {
			// Phiên biến mất giữa lúc kiểm và lúc gọi (hoặc harness tìm theo cách khác): chạy lại một lần với đề bài đầy đủ.
			msg := fmt.Sprintf("harness không tìm thấy phiên %s để nối tiếp bước %s: chạy mới với đề bài đầy đủ", shortID(resumeID), step)
			r.logf("! %s", msg)
			rs.res.Notes = append(rs.res.Notes, msg)
			first := res
			r.disarmProcessContinuation(sr)
			resumeID = ""
			res = runAgent(sr.prompt, sr.sessionID, "")
			res.Duration += first.Duration
			if first.CostUSD != nil && res.CostUSD != nil {
				sum := *first.CostUSD + *res.CostUSD
				res.CostUSD = &sum
			}
		}
	} else {
		res = runAgent(sr.prompt, sr.sessionID, "")
	}
	r.settleProcessUsage(rs, sr, &res, cmp.Or(res.SessionID, sr.sessionID), resumeID != "")
	return r.endStep(ctx, rs, sr, res)
}

// armProcessContinuation quyết định nối tiếp cho bước driver process vừa mở. Nối tiếp được thì đặt các trường nối tiếp của sr
// (bộ đếm, lượt gốc, mốc usage, handle phiên), đổi sr.prompt thành lời nhắn nối tiếp (đề bài đầy đủ giữ ở sr.fullPrompt) và trả
// phiên cần resume; không thì ghi lý do vào notes và trả "".
func (r *Runner) armProcessContinuation(ctx context.Context, rs *runState, sr *stepRun, last *task.StepStop) string {
	dec := r.decideContinuation(ctx, rs, sr.step, rs.opts.Fresh, sr.harness, sr.profileID(), sr.reqModel)
	if dec.Continue {
		if why := r.processSessionMissing(sr.harness, last.SessionID, sr.prof); why != "" {
			dec = ContinueDecision{Why: why}
		}
	}
	if !dec.Continue {
		if dec.Why != "" {
			rs.res.Notes = append(rs.res.Notes, "không nối tiếp phiên cũ: "+dec.Why)
		}
		return ""
	}
	var answered []string
	if last.StopClass == StopAwaitingHuman && rs.kindOf(last.Step) == flow.KindPlan {
		answered = answeredFor(rs.t)
	}
	sr.fullPrompt = sr.prompt
	sr.usageBase = last.Usage
	sr.sessionID = last.SessionID
	msg := ""
	if last.StopClass == StopPlanHandoff {
		// b-feat đầu tiên nối tiếp phiên b-req: lời nhắn gồm skill b-feat và kế hoạch rút gọn; bộ đếm/lượt gốc không kế thừa.
		sr.prompt = r.planHandoffPrompt(rs, sr, last)
		sr.continuations, sr.originRunID = 0, ""
		msg = fmt.Sprintf("b-feat nối tiếp phiên %s của b-req (kế hoạch rút gọn, không nạp lại ngữ cảnh từ đầu)", shortID(last.SessionID))
		if last.Model != sr.reqModel {
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("b-feat đổi model khi nối tiếp phiên b-req: %s → %s; cache prompt sẽ được dựng lại ở lượt đầu, ngữ cảnh phiên được giữ",
				modelDesc(last.Model), modelDesc(sr.reqModel)))
		}
	} else {
		sr.prompt = continuationPrompt(rs.t.State.ID, sr.step, sr.child, last, answered) + r.continuationExtra(rs, last)
		sr.continuations = continuationCount(last)
		sr.originRunID = cmp.Or(last.OriginRunID, last.RunID)
		msg = fmt.Sprintf("nối tiếp phiên %s của bước %s (lớp dừng %s, lần %d)", shortID(last.SessionID), sr.step, last.StopClass, sr.continuations)
	}
	r.logf("↻ %s", msg)
	rs.res.Notes = append(rs.res.Notes, msg)
	return last.SessionID
}

// disarmProcessContinuation đưa bước nối tiếp về bước chạy mới (đề bài đầy đủ, UUID phiên mới, không kế thừa bộ đếm/mốc usage).
func (r *Runner) disarmProcessContinuation(sr *stepRun) {
	sr.prompt, sr.fullPrompt = sr.fullPrompt, ""
	sr.continuations, sr.originRunID, sr.usageBase = 0, "", nil
	sr.sessionID = ""
	if sr.harness == "claude" {
		sr.sessionID = eval.NewSessionUUID()
	}
}

// processSessionMissing kiểm tệp phiên còn trên đĩa ngay trước khi nối tiếp (Claude: transcript ở thư mục dự án của cwd của bước;
// Codex: rollout của thread). Trả lý do khi mất ("" nếu còn hoặc harness không có tệp phiên để kiểm).
func (r *Runner) processSessionMissing(harness, id string, prof *iso.Profile) string {
	switch harness {
	case "claude":
		if claude.SessionTranscriptPath(stepConfigDir(harness, prof), r.Root, id) == "" {
			return fmt.Sprintf("transcript của phiên %s không còn ở thư mục dự án của %s", shortID(id), r.Root)
		}
	case "codex":
		if codex.RolloutPath(stepConfigDir(harness, prof), id) == "" {
			return fmt.Sprintf("rollout của thread %s không còn", shortID(id))
		}
	}
	return ""
}

// beginStep là nửa trước của một bước: chuyển trạng thái, giới hạn, khoá, ảnh chụp trước, sự kiện bắt đầu, đề bài.
// session=true (driver session) không dựng agent của tiến trình: harness/model lấy từ cấu hình role. Trả ok=false
// nếu workflow phải dừng (trạng thái tác vụ đã đặt); khi ok, khoá của bước vẫn được giữ trong sr.lock cho tới endStep
// (hoặc sr.release).
func (r *Runner) beginStep(ctx context.Context, rs *runState, step string, session bool) (sr *stepRun, ok bool) {
	// Chuyển trạng thái trước bước.
	firstFeat := false
	kind := rs.kindOf(step)
	switch kind {
	case flow.KindImplement, flow.KindGoal:
		if sd, _ := rs.flow.StepByID(step); sd.ExpectsFail() && !r.preflightRepro(rs, step) {
			return nil, false
		}
		if !r.preflightFeat(rs) {
			return nil, false
		}
		firstFeat = !rs.t.State.EverInProgress() // phải xét trước khi chuyển in_progress ở dưới
		if others := r.sharedWriteConflicts(rs.t); len(others) > 0 {
			id := rs.t.State.ID
			r.block(rs, fmt.Sprintf("cây chung %s còn tác vụ khác chưa kết thúc (%s): hai tác vụ không được cùng ghi một cây. "+
				"Chờ tác vụ đó xong (bint task show %s) rồi %s; hoặc huỷ phiếu này (bint task cancel %s) "+
				"và lập lại bằng --worktree", rs.t.State.Worktree, strings.Join(others, ", "), others[0], r.resumeCmd(rs), id))
			return nil, false
		}
		if rs.t.Ticket.Autonomy == task.AutonomyAskAlways {
			q := Question{Key: QConfirmFeat, Params: map[string]any{"task": rs.t.State.ID, "root": r.Root},
				Text: fmt.Sprintf("Tác vụ %s (autonomy: ask_always): cho phép %s sửa mã trong %s?", rs.t.State.ID, step, r.Root)}
			if r.Confirm == nil || !r.Confirm(q) {
				r.block(rs, "autonomy ask_always: chờ người cho phép bước "+step+" ("+r.resumeCmd(rs)+")")
				return nil, false
			}
		}
		if kind == flow.KindImplement && rs.t.State.Status != task.StatusInProgress { // b-goal giữ trạng thái planned
			if err := r.transition(rs, task.StatusInProgress, step+" bắt đầu"); err != nil {
				r.fail(rs, step, err.Error())
				return nil, false
			}
		}
	case flow.KindReview:
		if rs.t.State.Status != task.StatusInReview {
			if err := r.transition(rs, task.StatusInReview, step+" bắt đầu"); err != nil {
				r.fail(rs, step, err.Error())
				return nil, false
			}
		}
	}

	// Giới hạn vận hành (mục 5): giới hạn của bước, không vượt phần còn lại của lượt.
	stepLim := r.Cfg.Limits.ForWorkflow(step)
	maxCost := stepLim.MaxCostUSD
	if rs.chain() { // ngân sách chung của cả chuỗi bước (limits.workflows.<flow>)
		rem := rs.remaining()
		if rem != nil && *rem <= 0 {
			r.block(rs, fmt.Sprintf("hết ngân sách limits.workflows.%s.max_cost_usd (đã tiêu %.2f USD)", rs.flow.Name, rs.spent))
			return nil, false
		}
		maxCost = minPtr(maxCost, rem)
	}
	maxTurns := stepLim.MaxTurns
	if rs.chain() && rs.limit.MaxTurns != nil {
		rem := int64(*rs.limit.MaxTurns) - rs.turns
		if rem <= 0 {
			r.block(rs, fmt.Sprintf("hết ngân sách limits.workflows.%s.max_turns (đã dùng %d lượt)", rs.flow.Name, rs.turns))
			return nil, false
		}
		if maxTurns == nil || int64(*maxTurns) > rem {
			v := int(rem)
			maxTurns = &v
		}
	}
	spec := rs.spec(step)
	if _, err := r.stepSkill(cmp.Or(spec.Skill, step)); err != nil {
		r.fail(rs, step, err.Error())
		return nil, false
	}
	sr = &stepRun{step: step, kind: kind, role: spec.Role, base: spec.Base, firstFeat: firstFeat, maxCost: maxCost, maxTurns: maxTurns}
	if kind == flow.KindImplement {
		sr.tasksBefore = tasksSnapshot(rs.t)
	}
	var who string
	if session {
		h, model, err := r.sessionChoice(rs.t, spec)
		if err != nil {
			r.fail(rs, step, err.Error())
			return nil, false
		}
		sr.harness = h
		sr.reqModel = model
		who = h + " (subagent)"
	} else {
		agent, err := r.Agent(spec, maxCost, maxTurns)
		if err != nil {
			r.fail(rs, step, err.Error())
			return nil, false
		}
		if maxCost != nil && !agent.ReportsCost() && !rs.warnedNoCost {
			rs.warnedNoCost = true
			rs.res.Notes = append(rs.res.Notes, agent.Name()+" không báo chi phí: giới hạn max_cost_usd chỉ là chỉ dẫn")
		}
		if tl, ok := agent.(eval.TurnLimiter); ok && maxTurns != nil && !tl.EnforcesTurns() && !rs.warnedNoTurns {
			rs.warnedNoTurns = true
			rs.res.Notes = append(rs.res.Notes, agent.Name()+" không có giới hạn số lượt: max_turns chỉ là chỉ dẫn")
		}
		if mn, ok := agent.(eval.ModelNamer); ok {
			sr.reqModel = mn.RequestedModel()
		}
		sr.agent, sr.harness = agent, agent.Name()
		who = agent.Name()
		if p := eval.EffectiveProfile(agent, eval.AgentRequest{}); p != nil {
			sr.prof = p
			who += "@" + p.ID()
		}
	}
	if sr.reqModel != "" {
		who += " · " + sr.reqModel
	}

	// Cô lập (P5): bước chỉ đọc giữ khoá chia sẻ, bước có quyền ghi giữ khoá độc quyền của worktree
	// (chờ tới khi lấy được). Khoá lấy TRƯỚC ảnh chụp trước và giữ tới sau ảnh chụp sau, nên b-feat của
	// tác vụ khác không chen vào giữa hai ảnh chụp của bước chỉ đọc.
	sr.readOnly = spec.ReadOnly
	sr.mode = LockExclusive
	if sr.readOnly {
		sr.mode = LockShared
	}
	lock, lok := r.lockStep(ctx, rs, step, sr.mode)
	if !lok {
		return nil, false
	}
	sr.lock = lock
	defer func() {
		if !ok { // không bắt đầu được: trả khoá như khi bước kết thúc
			sr.release()
		}
	}()
	sr.started = time.Now() // đồng hồ thật: cùng đồng hồ với dòng nhật ký ghi (không dùng r.now() có thể bị giả)
	if sr.readOnly {
		snap, err := WorktreeSnapshot(ctx, r.Root)
		if err != nil {
			r.fail(rs, step, "chụp cây làm việc trước bước chỉ đọc: "+err.Error())
			return nil, false
		}
		sr.snapBefore = snap
	} else if kind == flow.KindImplement || kind == flow.KindGoal {
		// b-goal ghi tệp trước b-feat: đặt BaseTree ở đây (firstFeat) để tệp test vào Touched; b-feat sau đó giữ BaseTree đã có.
		sr.featBefore = r.beginTouched(ctx, rs, firstFeat || kind == flow.KindGoal)
	}

	sr.child = ulid.New()
	sr.stepID = "st_" + sr.child
	rs.ran[step]++
	r.emitRun(rs.runID, spool.StepStarted, map[string]any{"step_id": sr.stepID, "name": step, "status": "running"})
	r.emitRun(sr.child, spool.WorkflowStarted, map[string]any{"workflow": step, "parent_run_id": rs.runID, "harness": sr.harness, "model": sr.reqModel,
		"bint_version": r.Version, "git_head": gitrepo.HeadCommit(ctx, r.Root), "git_branch": gitrepo.Branch(ctx, r.Root),
		"task_id": rs.t.State.ID})
	if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.LinkRun(sr.child)
		if kind == flow.KindImplement {
			// Bước implement mới: câu hỏi mở của lượt trước mà người không trả lời bị bỏ (không chặn lượt này); handle của b-feat
			// trước hết hiệu lực; không còn chờ quyết định sau hết vòng sửa.
			for i := range t.State.Questions {
				if q := t.State.Questions[i]; q.Open() && (q.Step == step || q.Step == "") {
					t.State.Questions[i].Closed = true
				}
			}
			t.State.LastImplement, t.State.ReviewExhausted, t.State.AwaitingAnswer = nil, false, false
		}
		// LastStep chưa xoá ở đây: bước chỉ "thật sự bắt đầu" khi PendingStep được ghi (grantPending, driver session) hoặc
		// ngay trước agent.Run (runStep, driver process); lỗi giữa chừng không được làm mất handle nối tiếp.
		if rs.chain() {
			t.State.FlowStep = step // bước đang chạy: nguồn của nextStep khi flow có nhiều bước implement/review
		}
		return nil
	}); err == nil {
		rs.t = nt
	}

	sr.prompt = r.prompt(ctx, rs, step, sr.child, session)
	sr.stepLog = rs.t.LogPath(step + "-" + sr.child + ".log")
	r.logf("▶ %s [%s] (lượt %s)…", step, who, sr.child)
	if r.OnStep != nil {
		r.OnStep(StepEvent{TaskID: rs.t.State.ID, Step: step, RunID: sr.child, Log: sr.stepLog})
	}
	return sr, true
}

// endStep là nửa sau của một bước: ghi tập tệp đã đổi (b-feat), tính phạm vi, kiểm ảnh chụp sau của bước chỉ đọc,
// ghi kết quả/sự kiện, rồi xử lý bước lỗi/huỷ/chạm trần. Nhả khoá của bước (sr.lock) khi trả về. Trả false nếu
// workflow phải dừng.
func (r *Runner) endStep(ctx context.Context, rs *runState, sr *stepRun, res eval.AgentResult) bool {
	defer sr.release()
	step, child := sr.step, sr.child
	r.endTouched(ctx, rs, sr.featBefore)
	if (sr.kind == flow.KindImplement || sr.kind == flow.KindGoal) && ctx.Err() == nil {
		if sr.kind == flow.KindImplement {
			r.guardTasks(rs, step, sr.tasksBefore) // tasks.md nằm ở thư mục trạng thái, không vào phạm vi tệp
		}
		// Tính phạm vi khi còn giữ khoá độc quyền: sau khi nhả, b-feat của tác vụ khác có thể ghi tệp dở
		// dang vào cây và bị quy nhầm cho tác vụ này. Chỉ tính ở đây; hỏi người (checkScope) làm sau khi
		// nhả khoá để không chặn tác vụ khác trong lúc chờ.
		rs.featScope, rs.featScopeErr = ScopeFiles(ctx, r.Root, r.Tasks, rs.t, r.Cfg.Limits.ProtectedPaths)
		rs.featScopeSet = true
	}
	var humanWrites, violations []string
	afterSnap := ""
	if sr.readOnly && ctx.Err() == nil {
		after, err := WorktreeSnapshot(ctx, r.Root)
		if err == nil {
			afterSnap = after
		}
		switch {
		case err != nil:
			res.Error = "chụp cây làm việc sau bước chỉ đọc: " + err.Error()
		case after != sr.snapBefore:
			chk := ReadonlyViolations(ctx, r.Root, sr.snapBefore, after, sr.started, WritesLogPath(filepath.Dir(r.Tasks.Dir)))
			if len(chk.HumanWrites) > 0 {
				humanWrites = chk.HumanWrites
				list := strings.Join(humanWrites, ", ")
				r.logf("bỏ qua thay đổi do người ghi qua Bint: %s", list)
				rs.res.Notes = append(rs.res.Notes, step+": bỏ qua thay đổi do người ghi qua Bint: "+list)
			}
			// Không còn đặt res.Error: agent kết thúc bình thường mà cây đổi thì giữ kết quả bước và chờ người (blocked,
			// HeldStep) thay vì failed bỏ kết quả — thay đổi có thể do người sửa đồng thời trên cây chung.
			violations = chk.Violations
		}
	}

	model := res.Model
	if model == "" {
		model = sr.reqModel
	}
	srr := StepResult{Step: step, RunID: child, Harness: sr.harness, Model: model, CostUSD: res.CostUSD, Duration: res.Duration.Round(time.Second).String(), Error: res.Error, HumanWrites: humanWrites,
		Profile: sr.profileID()}
	if res.CostUSD != nil {
		rs.spent += *res.CostUSD
		rs.costKnown = true
	} else {
		rs.costMissing = true
	}
	if res.NumTurns != nil {
		rs.turns += *res.NumTurns
	}
	status, endReason := "completed", ""
	switch {
	case ctx.Err() != nil:
		status, endReason = "interrupted", "người dùng huỷ"
	case res.TimedOut:
		status, endReason = "failed", fmt.Sprintf("quá workflow.timeout_minutes (%d phút)", r.Cfg.Workflow.TimeoutMinutes)
	case res.Error != "" || res.ExitCode != 0:
		status, endReason = "failed", res.Error
		if endReason == "" {
			endReason = fmt.Sprintf("agent thoát với mã %d", res.ExitCode)
		}
	}
	var deniedPaths []string
	// Luôn đọc (và dọn) nhật ký deny của child run: bước bị ngắt thì bỏ qua kết quả, nhưng không để thư
	// mục spool/denials/<run> nằm lại vì resume chạy bằng child run mới.
	reason, paths, denied, guardDenials := hookDenial(r.Spool, child)
	for _, g := range guardDenials {
		r.logf("  guardrail đã từ chối một thao tác của agent (không chặn tác vụ): %s", g)
	}
	if denied && status != "interrupted" {
		status, endReason, deniedPaths = "blocked", reason, paths
	}
	held := false
	if len(violations) > 0 {
		if status == "completed" {
			status, held = "blocked", true
			endReason = r.heldReason(rs, sr, violations)
		} else {
			// Agent lỗi/quá giờ/bị chặn kèm vi phạm: giữ trạng thái đó, nối tệp đổi vào lý do (không giữ kết quả bước).
			endReason += fmt.Sprintf("; bước chỉ đọc %s còn sửa cây làm việc: %s (Bint không hoàn tác, hãy xem lại)", step, strings.Join(violations, ", "))
		}
		srr.Error = endReason
	}
	if sr.isoLeak != "" && status != "interrupted" {
		// Ghi đè cả failed/timeout: người phải xem cây gốc trước khi tác vụ đi tiếp. Rò cô lập thắng việc giữ kết quả bước
		// chỉ đọc (held): resume phải chạy lại bước, không coi kết quả của lượt đã chạy ngoài worktree là dùng được.
		status, endReason, held = "blocked", sr.isoLeak, false
		r.RecordError(rs.t.State.ID, step, sr.isoLeak)
	}
	srr.Status = status
	costCap := status == "failed" && sr.maxCost != nil && res.CostUSD != nil && *res.CostUSD >= *sr.maxCost*0.98
	class := ClassifyStop(StopFacts{
		Failed: res.Error != "" || res.ExitCode != 0, AgentFailed: sr.agentFailed, CostCap: costCap,
		Interrupted: ctx.Err() != nil, LeaseExpired: sr.timedOut, TimedOut: res.TimedOut && !sr.timedOut,
		Infra: res.Infra != "", QuotaHit: res.QuotaHit, NoResult: res.NoResult,
		ReadonlyViolation: len(violations) > 0, IsolationLeak: sr.isoLeak != "" && status != "interrupted",
		ProtectedDenied: len(deniedPaths) > 0,
	})
	handle := cmp.Or(sr.agentID, res.SessionID, sr.sessionID)
	rs.last = &endedStep{step: step, runID: child, harness: sr.harness, profile: sr.profileID(), handle: handle, continuations: sr.continuations,
		originRunID: sr.originRunID, usage: sr.usage, noSession: sr.noSession}
	var stop *task.StepStop
	if class != "" {
		fp := afterSnap
		if fp == "" {
			var err error
			if fp, err = r.stopFingerprint(ctx); err != nil {
				rs.res.Notes = append(rs.res.Notes, step+": không chụp được fingerprint cây lúc dừng (không nối tiếp được): "+err.Error())
			}
		}
		stop = &task.StepStop{Step: step, RunID: child, Harness: sr.harness, Profile: sr.profileID(), StopClass: class, SessionID: handle, Fingerprint: fp,
			Continuable: stopContinuable(class, handleIf(sr.noSession, handle), fp), At: r.at(), Continuations: sr.continuations,
			OriginRunID: sr.originRunID, Usage: sr.usage, Via: viaOf(rs, sr.harness)}
		if sr.noSession != "" && stopContinuable(class, handle, fp) {
			rs.res.Notes = append(rs.res.Notes, step+": không nối tiếp được ở lần sau — "+sr.noSession)
		}
		srr.StopClass, srr.Continuable, srr.SessionID = class, stop.Continuable, handle
	} else {
		srr.SessionID = handle
	}
	if rs.session && res.CostUSD != nil {
		// Subagent không dừng được giữa chừng, nên vượt max_cost_usd của bước chỉ được ghi lỗi (bước không failed); ngân sách
		// b-do vẫn chặn bước sau qua beginStep.
		if lim := r.Cfg.Limits.ForWorkflow(step).MaxCostUSD; lim != nil && *res.CostUSD > *lim {
			msg := fmt.Sprintf("bước %s tốn %.4f USD, vượt limits.workflows.%s.max_cost_usd = %.4f", step, *res.CostUSD, step, *lim)
			r.RecordError(rs.t.State.ID, step, msg)
			rs.res.Notes = append(rs.res.Notes, msg)
		}
	}
	if r.OnStep != nil {
		r.OnStep(StepEvent{TaskID: rs.t.State.ID, Step: step, RunID: child, Log: sr.stepLog, Done: true, Status: status})
	}
	payload := map[string]any{"status": status, "end_reason": endReason, "model": res.Model,
		"result_summary": truncate(strings.TrimSpace(res.Text), 500), "task_id": rs.t.State.ID}
	if len(humanWrites) > 0 {
		payload["human_writes"] = humanWrites
	}
	if sr.agentID != "" {
		payload["agent_id"] = sr.agentID
	}
	if class != "" {
		payload["stop_class"] = class
		payload["continuable"] = stop.Continuable
	}
	if handle != "" {
		payload["session_id"] = handle
	}
	if res.CostUSD != nil {
		payload["cost_usd"] = *res.CostUSD
	}
	if res.InputTokens != nil {
		payload["input_tokens"] = *res.InputTokens
	}
	if res.OutputTokens != nil {
		payload["output_tokens"] = *res.OutputTokens
	}
	r.emitRun(child, spool.WorkflowFinished, payload)
	r.emitRun(rs.runID, spool.StepFinished, map[string]any{"step_id": sr.stepID, "name": step, "status": status})
	rs.res.Steps = append(rs.res.Steps, srr)
	if t, err := r.Tasks.Load(rs.t.State.ID); err == nil {
		rs.t = t
	}
	if stop != nil {
		r.setLastStep(rs, stop)
	}

	if held {
		r.RecordError(rs.t.State.ID, step, endReason)
		if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.HeldStep = &task.HeldStep{Step: step, RunID: child, Kind: string(sr.kind), Files: violations, At: r.at()}
			return nil
		}); err == nil {
			rs.t = t
		}
	}
	if status == "blocked" {
		if len(deniedPaths) > 0 {
			if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
				t.State.PendingPaths = appendUnique(t.State.PendingPaths, deniedPaths...)
				t.State.AwaitingCheck = false
				return nil
			}); err == nil {
				rs.t = t
			}
		}
		r.block(rs, endReason)
		return false
	}
	if status == "completed" && sr.kind == flow.KindImplement {
		if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.AwaitingCheck = true; return nil }); err == nil {
			rs.t = t
		}
		if class == "" {
			r.recordLastImplement(ctx, rs, sr, handle) // vòng sửa sau review nối tiếp phiên của b-feat này (armReviewFix)
		}
	}
	if status == "completed" && sr.kind == flow.KindPlan && class == "" && !rs.session {
		r.recordLastPlan(ctx, rs, sr, handleIf(sr.noSession, handle), afterSnap) // b-feat đầu tiên nối tiếp phiên này (plan_handoff)
		if sr.noSession != "" {
			rs.res.Notes = append(rs.res.Notes, step+": b-feat không nối tiếp được phiên b-req — "+sr.noSession)
		}
	}
	switch status {
	case "interrupted":
		r.interrupt(rs, "người dùng huỷ trong bước "+step)
		return false
	case "failed":
		if sr.maxCost != nil && res.CostUSD != nil && *res.CostUSD >= *sr.maxCost*0.98 {
			r.RecordError(rs.t.State.ID, step, endReason)
			r.block(rs, fmt.Sprintf("bước %s chạm giới hạn chi phí %.2f USD", step, *sr.maxCost))
			return false
		}
		r.fail(rs, step, step+": "+endReason)
		return false
	}
	r.logf("✓ %s xong (%s)", step, srr.Duration)
	return true
}

// stopFingerprint chụp cây làm việc lúc bước dừng. ctx có thể đã huỷ (Ctrl-C) nên tách khỏi huỷ, kèm hạn giờ ngắn.
func (r *Runner) stopFingerprint(ctx context.Context) (string, error) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return WorktreeSnapshot(c, r.Root)
}

// setLastStep ghi State.LastStep của phiếu (lỗi ghi chỉ làm mất thông tin chẩn đoán, không làm hỏng bước).
func (r *Runner) setLastStep(rs *runState, ls *task.StepStop) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.LastStep = ls; return nil }); err == nil {
		rs.t = t
	}
}

// markAwaitingHuman ghi bước vừa kết thúc (rs.last) là dừng chờ người: câu hỏi mở sau b-req, đường dẫn bảo vệ cần duyệt
// sau b-feat. Không có bước nào vừa kết thúc trong lượt này (resume) thì giữ nguyên LastStep đã ghi từ lượt trước.
func (r *Runner) markAwaitingHuman(rs *runState) {
	l := rs.last
	if l == nil {
		return
	}
	fp, _ := r.stopFingerprint(context.Background())
	r.setLastStep(rs, &task.StepStop{Step: l.step, RunID: l.runID, Harness: l.harness, Profile: l.profile, StopClass: StopAwaitingHuman, SessionID: l.handle,
		Fingerprint: fp, Continuable: stopContinuable(StopAwaitingHuman, handleIf(l.noSession, l.handle), fp), At: r.at(), Via: viaOf(rs, l.harness),
		Continuations: l.continuations, OriginRunID: l.originRunID, Usage: l.usage})
}

// viaOf là State.LastStep.Via của bước: ViaSubagent ở driver session, tên harness ở driver process.
func viaOf(rs *runState, harness string) string {
	if rs.session {
		return ViaSubagent
	}
	return harness
}

// handleIf trả "" (không có handle để nối tiếp) khi phiên không có trên đĩa (noSession khác rỗng), ngược lại handle.
func handleIf(noSession, handle string) string {
	if noSession != "" {
		return ""
	}
	return handle
}

// heldReason là lý do chuyển blocked khi bước chỉ đọc kết thúc bình thường nhưng cây đổi trong lúc chạy: nêu tệp đổi,
// nói rõ kết quả bước được giữ và hai lối ra (người sửa đồng thời → resume; agent sửa → xem lại/hoàn tác rồi resume).
func (r *Runner) heldReason(rs *runState, sr *stepRun, files []string) string {
	id := rs.t.State.ID
	resume := r.resumeCmd(rs)
	rerun := resume + " --rerun"
	return fmt.Sprintf("bước chỉ đọc %s (%s) đã kết thúc nhưng cây làm việc đổi trong lúc chạy: %s — kết quả của bước đã được giữ. "+
		"Nếu chính bạn sửa đồng thời: `%s` để đi tiếp từ sau bước %s (không chạy lại). "+
		"Nếu do agent sửa: xem `git diff`, hoàn tác (Bint không hoàn tác) rồi `%s` để bỏ kết quả giữ và chạy lại bước, hoặc `bint task cancel %s`",
		sr.step, sr.harness, strings.Join(files, ", "), resume, sr.step, rerun, id)
}

// dropHeldForRerun xoá HeldStep của phiếu t cho `resume --rerun` (bước đó sẽ chạy lại, kết quả giữ bị bỏ). Lỗi cách
// dùng nếu không phải resume hoặc phiếu không có bước giữ.
func dropHeldForRerun(t *task.Task, resume bool) (*task.HeldStep, error) {
	if !resume {
		return nil, errors.New("--rerun chỉ dùng khi tiếp tục tác vụ (resume)")
	}
	h := t.State.HeldStep
	if h == nil && t.State.FinalizeFP != "" {
		// Finalize bị chặn thiếu bằng chứng: --rerun bỏ lối tắt "không review lại" và mở lượt b-review mới như cũ.
		t.State.FinalizeFP = ""
		return nil, nil
	}
	if h == nil {
		return nil, fmt.Errorf("--rerun: tác vụ %s không có bước chỉ đọc nào đang được giữ để chạy lại", t.State.ID)
	}
	t.State.HeldStep = nil
	return h, nil
}

func rerunNote(h *task.HeldStep) string {
	if h == nil {
		return "bỏ lối tắt sau finalize bị chặn theo yêu cầu chạy lại, review lại như thường"
	}
	return fmt.Sprintf("bỏ kết quả bước giữ %s (lượt %s), chạy lại bước theo yêu cầu của người dùng", h.Step, h.RunID)
}

// heldStepOf trả bước đang giữ nếu nó thuộc flow đang chạy (nil nếu không có hoặc thuộc flow khác).
func (rs *runState) heldStepOf() *task.HeldStep {
	h := rs.t.State.HeldStep
	if h == nil {
		return nil
	}
	if _, ok := rs.flow.StepByID(h.Step); !ok {
		return nil
	}
	return h
}

// takeHeld nhận bước đang giữ để áp cổng sau bước của nó: xoá HeldStep khỏi phiếu và nhớ lượt của bước cho
// reviewIsFresh (rs.res.Steps rỗng khi resume). Bước giữ thuộc flow khác (đổi workflow giữa chừng) bị bỏ kèm ghi chú.
func (r *Runner) takeHeld(rs *runState) *task.HeldStep {
	h := rs.t.State.HeldStep
	if h == nil {
		return nil
	}
	foreign := rs.heldStepOf() == nil
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.HeldStep = nil; return nil }); err == nil {
		rs.t = t
	}
	if foreign {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("bỏ bước giữ %s: không thuộc flow %s đang chạy", h.Step, rs.flow.Name))
		return nil
	}
	rs.heldRunID = h.RunID
	return h
}

// afterHeld áp cổng sau bước cho bước giữ vừa nhận (takeHeld) rồi quên lượt của nó: heldRunID chỉ đúng cho đúng
// cổng này; để lại thì reviewIsFresh của b-review chạy sau đó (cùng lượt Run) so nhầm với lượt của bước giữ.
func (r *Runner) afterHeld(ctx context.Context, rs *runState, step string) bool {
	defer func() { rs.heldRunID = "" }()
	return r.afterStep(ctx, rs, step)
}

// lockWaitUnit là đơn vị của workflow.timeout_minutes khi chờ khoá; biến để kiểm thử rút ngắn.
var lockWaitUnit = time.Minute

// lockStep lấy khoá worktree cho một bước. Mặc định chờ (tối đa workflow.timeout_minutes) và in
// "chờ khoá do <chủ>"; NoWait thì lấy một lần, bận thì tác vụ chuyển blocked. Hết hạn chờ → blocked;
// ctx huỷ → interrupted. Trả false nếu workflow phải dừng.
func (r *Runner) lockStep(ctx context.Context, rs *runState, step string, mode LockMode) (*Lock, bool) {
	owner := rs.t.State.ID + " " + step
	if rs.opts.NoWait {
		l, err := AcquireLock(r.Tasks.Dir, r.Root, owner, mode)
		if err != nil {
			r.block(rs, err.Error())
			return nil, false
		}
		return l, true
	}
	mins := r.Cfg.Workflow.TimeoutMinutes
	l, err := WaitLock(ctx, r.Tasks.Dir, r.Root, owner, mode, WaitOptions{
		Timeout: time.Duration(mins) * lockWaitUnit,
		OnWait: func(holders string) {
			if holders == "" {
				holders = "chủ khoá chưa rõ"
			}
			r.logf("⏳ chờ khoá do %s (bước %s cần khoá %s)", holders, step, mode)
		},
	})
	switch {
	case err == nil:
		return l, true
	case ctx.Err() != nil:
		r.interrupt(rs, "người dùng huỷ khi chờ khoá của bước "+step)
	case errors.Is(err, ErrLocked):
		r.block(rs, fmt.Sprintf("chờ khoá quá %d phút: %v", mins, err))
	default:
		r.fail(rs, step, "lấy khoá worktree: "+err.Error())
	}
	return nil, false
}

// hookDenial đọc rồi dọn nhật ký deny của child run (spool/denials, hook ghi). Chỉ deny đường dẫn bảo vệ chặn tác
// vụ: harness không hỏi được người dùng (Codex) nên hook đổi "cần duyệt" thành deny, tệp chưa được sửa và bước
// không được coi là xong. Deny của guardrail là phản hồi cho agent (docs/harness-components.md: Bint không nhốt
// agent), agent có thể đổi cách làm; nó chỉ được trả về trong guardrail để ghi log, không đổi trạng thái tác vụ.
func hookDenial(dir, runID string) (reason string, paths []string, blocked bool, guardrail []string) {
	events, err := spool.Denials(dir, runID)
	_ = spool.ClearDenials(dir, runID)
	if err != nil {
		return "", nil, false, nil
	}
	var reasons []string
	for _, ev := range events {
		var hit []string
		switch raw := ev.Payload["protected_paths"].(type) {
		case []any:
			for _, item := range raw {
				if p, ok := item.(string); ok && p != "" {
					hit = append(hit, p)
				}
			}
		case []string:
			for _, p := range raw {
				if p != "" {
					hit = append(hit, p)
				}
			}
		}
		why, _ := ev.Payload["reason"].(string)
		if len(hit) == 0 {
			if why == "" {
				why = "hook Bint đã từ chối công cụ"
			}
			guardrail = appendUnique(guardrail, why)
			continue
		}
		paths = appendUnique(paths, hit...)
		if why == "" {
			why = "đường dẫn bảo vệ cần người duyệt: " + strings.Join(hit, ", ")
		}
		reasons = appendUnique(reasons, why)
	}
	if len(paths) == 0 {
		return "", nil, false, guardrail
	}
	return strings.Join(reasons, "; "), paths, true, guardrail
}

// afterStep xử lý kết quả bước; trả false nếu workflow dừng.
func (r *Runner) afterStep(ctx context.Context, rs *runState, step string) bool {
	switch rs.kindOf(step) {
	case flow.KindPlan:
		missing := 0
		for _, n := range task.ArtifactNames() {
			if _, err := os.Stat(rs.t.ArtifactPath(n)); err != nil {
				missing++
			}
		}
		if missing == len(task.ArtifactNames()) {
			r.fail(rs, step, step+" không ghi tài liệu nào (requirement/plan/tasks)")
			return false
		}
		// Agent đã kết thúc: từ đây tác vụ theo hợp đồng v1 (task cũ chạy lại b-req được nâng ở đây,
		// không nâng sớm để agent lỗi giữa chừng không biến task cũ thành v1).
		if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			ensurePlanningContract(t)
			// Kế hoạch mới: b-do gốc phải áp lại cổng cỡ kế hoạch; b-req chạy riêng (người xem kế hoạch rồi mới chạy tiếp) và flow tuỳ biến giữ hành vi cũ.
			t.State.PlanSize = ""
			if !rs.planSizeGated() {
				t.State.PlanSize = planSizePassed
			}
			return nil
		}); err == nil {
			rs.t = t
		}
		rep := task.LintPlanningTask(rs.t, true)
		if err := r.transition(rs, task.StatusPlanned, step+" xong"); err != nil {
			r.fail(rs, step, err.Error())
			return false
		}
		if rep.Blocking() {
			return r.stopOnPlanning(rs, rep)
		}
		for _, w := range rep.Warnings {
			if w.Code == "goal_check_missing" {
				rs.res.Notes = append(rs.res.Notes, "requirement.md thiếu mục `## Kiểm tra mục tiêu`: b-feat và b-review không có đặc tả kiểm tra theo mục tiêu người dùng "+
					"(bổ sung bằng `bint task write "+rs.t.State.ID+" requirement -` rồi `bint task lint "+rs.t.State.ID+"`)")
			}
		}
		if n := phaseNote(rs.t, false); n != "" {
			rs.res.Notes = append(rs.res.Notes, n)
		}
		if a := r.Assess(rs.t); len(a.Questions) > 0 && strings.TrimSpace(rs.t.Ticket.Verify.Command) == "" {
			if !r.askVerify(rs, a) {
				return false
			}
		}
		if len(rep.OpenQuestions) > 0 {
			return r.stopOnQuestions(rs, rep)
		}
		if !r.planSizeGate(rs) { // b-do gốc: kế hoạch lớn dừng chờ người, nhỏ chạy thẳng b-feat (plansize.go)
			return false
		}
		return rs.chain() // flow nhiều bước đi tiếp sang bước sau plan; plan chạy riêng thì dừng
	case flow.KindGoal:
		return r.afterGoal(ctx, rs)
	case flow.KindImplement:
		if !r.freezeGate(rs) {
			return false // tệp kiểm tra mục tiêu đóng băng bị sửa: chặn trước kiểm phạm vi, kiểm chứng và review (goalfreeze.go)
		}
		if !r.checkScope(ctx, rs) {
			return false // kiểm phạm vi sau MỖI bước implement, kể cả khi sau nó còn bước implement khác
		}
		// Bước implement kế tiếp (flow nhiều bước implement): giữ in_progress và đi tiếp, không qua review.
		next, hasNext := rs.stepAfter(step)
		nextImpl := hasNext && next.Kind == flow.KindImplement
		sd, _ := rs.flow.StepByID(step)
		// Cổng sau bước implement: câu hỏi của b-feat, hoặc bước implement cuối không đổi tệp nào → chờ người, không kiểm chứng/review/done.
		if !r.implementGate(rs, step, !nextImpl && !sd.ExpectsFail()) {
			return false
		}
		if sd.ExpectsFail() {
			return r.redGate(ctx, rs, sd, next)
		}
		// Kiểm chứng chạy khi bước khai báo verify (mặc định có), và LUÔN chạy trước bước review đầu tiên và trước khi
		// done (bước implement cuối): luật này nằm ở Runner chứ không chỉ ở lint, nên flow không nới được cổng.
		// Dấu vân tay cây ngay trước kiểm chứng: vòng sửa chỉ nối tiếp phiên b-feat khi cây lúc này còn khớp lúc b-feat kết thúc.
		preVerify := r.preVerifyFingerprint(ctx, rs)
		if strings.TrimSpace(rs.t.Ticket.Verify.Command) != "" && (sd.VerifyOn() || !nextImpl) {
			// Kiểm chứng chạy dưới khoá độc quyền: lệnh kiểm chứng hay ghi tệp (coverage, snapshot, build) nên
			// không được chạy song song với bước chỉ đọc của tác vụ khác (sẽ bị quy là vi phạm chỉ đọc).
			lock, ok := r.lockStep(ctx, rs, "verify", LockExclusive)
			if !ok {
				return false
			}
			t, vr, err := r.VerifyScoped(ctx, rs.t.State.ID, "")
			lock.Release()
			switch {
			case err != nil && ctx.Err() != nil:
				r.interrupt(rs, "người dùng huỷ khi kiểm chứng")
				return false
			case err != nil:
				r.fail(rs, "verify", err.Error())
				return false
			case vr.Skipped != "":
				rs.res.Notes = append(rs.res.Notes, "kiểm chứng: "+vr.Skipped)
			default:
				rs.t = t
				v := vr.Verification
				switch {
				case v.Scope == string(affected.TierNone):
					r.logf("  kiểm chứng (scope %s): không chạy lệnh — không test nào bị ảnh hưởng bởi thay đổi (lệnh của phiếu: %s)", v.Scope, v.Command)
				case v.Scope != "":
					r.logf("  kiểm chứng (scope %s): %s → %s", v.Scope, cmpStr(v.Ran, v.Command), passWord(v.Passed))
				default:
					r.logf("  kiểm chứng: %s → %s", v.Command, passWord(v.Passed))
				}
			}
		}
		if !r.goalGate(ctx, rs, "") { // cổng mục tiêu riêng (log goal-N.log); đỏ vào vòng sửa qua redVerification
			return false
		}
		r.refreshLastImplement(ctx, rs, preVerify)
		if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.AwaitingCheck = false
			if nextImpl {
				t.State.FlowStep = next.ID
			}
			return nil
		}); err == nil {
			rs.t = t
		}
		if nextImpl {
			return true
		}
		if err := r.transition(rs, task.StatusInReview, step+" xong"); err != nil {
			r.fail(rs, step, err.Error())
			return false
		}
		if !rs.chain() {
			return false
		}
		if rs.opts.NoReview {
			return r.finalize(ctx, rs)
		}
		if rs.t.State.FastFixed {
			// Đường nhanh: vòng sửa duy nhất đã chạy xong; kiểm chứng ở trên là cổng. Finalize không review lại.
			if n := fastFixNote(rs.t); n != "" {
				rs.res.Notes = append(rs.res.Notes, n)
			}
			if v := redVerification(rs.t); v != nil {
				// Kiểm chứng sau vòng sửa duy nhất không qua: không có vòng nào nữa, nên dừng với ba lựa chọn thay vì để finalize chặn không lối ra.
				r.blockExhausted(rs, fmt.Sprintf("đường nhanh: kiểm chứng sau vòng sửa duy nhất không qua (`%s`, mã thoát %d, log %s).",
					v.Command, v.ExitCode, cmpStr(v.Log, "(không có)")))
				return false
			}
			return r.finalize(ctx, rs)
		}
		return true
	case flow.KindReview:
		rev, err := rs.t.LoadReview()
		if err != nil {
			r.fail(rs, step, err.Error())
			return false
		}
		last := rev.Last()
		if !r.reviewIsFresh(rs, last) {
			r.fail(rs, step, step+" không ghi kết quả ở lượt này (bint task review)")
			return false
		}
		return r.reviewDecision(ctx, rs, last)
	}
	return false
}

// preflightRepro chặn trước bước implement có expect: fail khi cổng đỏ không thể chạy: phiếu thiếu verify.command, hoặc
// lệnh chưa được người xác nhận (Bint không chạy lệnh do agent đề xuất). Chặn trước khi gọi agent để không tốn lượt chạy
// rồi mới biết không kiểm được. Trả true nếu chạy tiếp được.
func (r *Runner) preflightRepro(rs *runState, step string) bool {
	id := rs.t.State.ID
	cmd := strings.TrimSpace(rs.t.Ticket.Verify.Command)
	switch {
	case cmd == "":
		r.block(rs, fmt.Sprintf("bước %s cần lệnh kiểm chứng để biết test đã tái hiện lỗi: đặt bằng `bint task set %s --verify-command '…'`, "+
			"người xác nhận bằng `bint task approve %s`, rồi %s", step, id, id, r.resumeCmd(rs)))
		return false
	case !r.VerifyConfirmed(rs.t) && r.Cfg.Security.RequireApprovalForCommands:
		r.block(rs, fmt.Sprintf("bước %s cần lệnh kiểm chứng đã được người xác nhận (Bint không chạy lệnh do agent đề xuất): "+
			"`bint task approve %s`, rồi %s", step, id, r.resumeCmd(rs)))
		return false
	}
	return true
}

// redGate là cổng sau bước implement có expect: fail (bước viết test tái hiện lỗi): kiểm chứng phải KHÔNG qua. Hỏng thì
// sang bước kế (bản sửa). Qua nghĩa là test chưa tái hiện lỗi: chặn chờ người, FlowStep giữ ở bước này nên resume chạy
// lại nó. Không chạy được kiểm chứng (bỏ qua, quá giờ) cũng chặn nhưng giữ AwaitingCheck: resume chỉ chạy lại cổng, không
// gọi lại agent. Trả true nếu workflow đi tiếp.
func (r *Runner) redGate(ctx context.Context, rs *runState, sd flow.Step, next flow.Step) bool {
	lock, ok := r.lockStep(ctx, rs, "verify", LockExclusive)
	if !ok {
		return false
	}
	t, vr, err := r.Verify(ctx, rs.t.State.ID)
	lock.Release()
	switch {
	case err != nil && ctx.Err() != nil:
		r.interrupt(rs, "người dùng huỷ khi kiểm chứng")
		return false
	case err != nil:
		r.fail(rs, "verify", err.Error())
		return false
	case vr.Skipped != "":
		r.block(rs, fmt.Sprintf("không chạy được cổng đỏ của bước %s: %s; xử lý rồi %s (chỉ chạy lại kiểm chứng, không gọi lại agent)",
			sd.ID, vr.Skipped, r.resumeCmd(rs)))
		return false
	}
	rs.t = t
	v := vr.Verification
	r.logf("  kiểm chứng (cổng đỏ): %s → %s", v.Command, passWord(v.Passed))
	if v.TimedOut {
		r.block(rs, fmt.Sprintf("cổng đỏ của bước %s: lệnh kiểm chứng quá giờ nên chưa biết test có tái hiện lỗi không; "+
			"xử lý rồi %s (chỉ chạy lại kiểm chứng, không gọi lại agent)", sd.ID, r.resumeCmd(rs)))
		return false
	}
	if v.Passed {
		if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.AwaitingCheck = false
			t.State.FlowStep = sd.ID
			return nil
		}); err == nil {
			rs.t = t
		}
		r.block(rs, fmt.Sprintf("bước %s xong nhưng lệnh kiểm chứng vẫn qua: test chưa tái hiện lỗi (cần một test hỏng trước khi sửa). "+
			"Xem lại test hoặc lệnh kiểm chứng (bint task set %s --verify-command), rồi %s để chạy lại %s",
			sd.ID, rs.t.State.ID, r.resumeCmd(rs), sd.ID))
		return false
	}
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.AwaitingCheck = false
		t.State.FlowStep = next.ID
		return nil
	}); err == nil {
		rs.t = t
	}
	return true
}

// reviewIsFresh: vòng review last do chính bước b-review vừa chạy ghi. Driver process: lượt ghi khớp lượt của bước
// (BINT_RUN_ID). Driver session: subagent không có biến môi trường nên vòng ghi không mang lượt; nhận vòng mới hơn
// vòng đã áp quyết định gần nhất.
func (r *Runner) reviewIsFresh(rs *runState, last *task.ReviewRound) bool {
	if last == nil {
		return false
	}
	// Lượt của bước vừa chạy; khi resume áp bước đang giữ thì rs.res.Steps rỗng và lượt lấy từ HeldStep.
	runID := rs.heldRunID
	if n := len(rs.res.Steps); runID == "" && n > 0 {
		runID = rs.res.Steps[n-1].RunID
	}
	if runID == "" {
		return false
	}
	if last.RunID == runID {
		return true
	}
	return rs.session && last.RunID == "" && last.Round > rs.t.State.ReviewDecided
}

// reviewDecision áp quyết định sau một vòng review (flow nhiều bước): chuyển sang bước review kế tiếp hoặc finalize khi
// không còn phát hiện nghiêm trọng, quay về bước sửa (Step.Fix của bước review đang chạy; rỗng = bước implement cuối)
// khi còn vòng sửa, ngược lại chờ người. Trả true nếu workflow tiếp tục. Flow một bước (`bint run b-review --task`)
// không áp quyết định: vòng này vẫn chờ; driver session áp ở `next` khi review chạy riêng bằng tiến trình.
func (r *Runner) reviewDecision(ctx context.Context, rs *runState, last *task.ReviewRound) bool {
	blocking := last.Blocking()
	r.logf("  review vòng %d: %d phát hiện, %d nghiêm trọng", last.Round, len(last.Findings), len(blocking))
	if !rs.chain() {
		return false
	}
	rv := rs.currentReview()
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.ReviewDecided = max(t.State.ReviewDecided, last.Round)
		return nil
	}); err == nil {
		rs.t = t
	}
	red := redVerification(rs.t)
	if len(blocking) == 0 {
		if next, ok := rs.nextReview(rv.ID); ok { // flow có nhiều bước review: sạch thì sang bước review kế tiếp
			r.setFlowStep(rs, next.ID)
			return true
		}
		if red == nil {
			return r.finalize(ctx, rs)
		}
		// Review không còn phát hiện nghiêm trọng nhưng lần kiểm chứng cuối không qua (thường là test kiểm tra mục tiêu còn đỏ): không finalize
		// (cổng done sẽ chặn không lối ra) mà mở vòng sửa trong ngân sách; hết ngân sách thì dừng với ba lựa chọn.
		return r.reviewRedVerify(ctx, rs, rv, last, red)
	}
	used := r.reviewRoundsUsed(rs, last)
	limit := r.fixLimit(rs)
	fix := rs.reviewFixStep(rv)
	prev, stalled := r.reviewStalled(rs, last)
	converging := prev != nil && !stalled // có vòng trước để so và số phát hiện nghiêm trọng giảm hẳn
	stalled = stalled && used <= limit    // hết ngân sách vòng sửa thì báo như hết vòng sửa (thông điệp cũ), không phải không hội tụ
	ceiling := r.fixCeiling(rs)
	// Đường nhanh: đúng một vòng sửa sau review đầu tiên, rồi finalize (không review lại); ngân sách/hội tụ không áp.
	// Chỉ ở review vòng 1: vòng review sau --fix-round (người cho thêm vòng sửa) đi luật thường (ngân sách, hội tụ), không mở thêm vòng nhanh.
	if rs.fastPath() && last.Round == 1 && limit > 0 {
		return r.fastFix(ctx, rs, fix, last, blocking)
	}
	// Trong ngân sách: sửa nếu không đứng yên. Ngoài ngân sách (used > limit): chỉ sửa tiếp khi review đang hội tụ, tới trần
	// 2 × max_review_rounds + số vòng người cho thêm.
	extended := used > limit && converging && used <= ceiling
	canFix := (used <= limit && !stalled) || extended
	// Hết vòng vẫn đặt LastStep để --fix-round sau này nối tiếp được, nhưng không ghi note "vòng sửa chạy…" khi không có vòng sửa nào mở.
	r.armReviewFix(ctx, rs, fix, canFix)
	if canFix {
		r.setFlowStep(rs, fix)
		reason := fmt.Sprintf("sửa theo review vòng %d (%d phát hiện nghiêm trọng; vòng sửa %d/%d)", last.Round, len(blocking), used, limit)
		if extended {
			reason = fmt.Sprintf("sửa theo review vòng %d (%d phát hiện nghiêm trọng; vòng sửa %d, ngoài ngân sách %d vì review đang hội tụ %d→%d, trần %d)",
				last.Round, len(blocking), used, limit, len(prev.Blocking()), len(blocking), ceiling)
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("vòng sửa %d chạy ngoài workflow.max_review_rounds vì review đang hội tụ (%d→%d phát hiện nghiêm trọng)",
				used, len(prev.Blocking()), len(blocking)))
		}
		if err := r.transition(rs, task.StatusInProgress, reason); err != nil {
			r.fail(rs, rv.ID, err.Error())
			return false
		}
		return true
	}
	// Hết vòng sửa hoặc review không hội tụ: người chọn (chấp nhận / sửa thêm một vòng / huỷ). used vòng review nghĩa là đã sửa used-1 vòng.
	extra := ""
	if n := rs.t.State.ExtraFixRounds; n > 0 {
		extra = fmt.Sprintf(" + %d vòng người cho thêm", n)
	}
	head := fmt.Sprintf("còn %d phát hiện nghiêm trọng sau %d vòng review và %d vòng sửa (tối đa %d vòng sửa: workflow.max_review_rounds = %d%s).",
		len(blocking), used, used-1, limit, r.Cfg.Workflow.MaxReviewRounds, extra)
	if converging && used > ceiling {
		head += fmt.Sprintf(" Review vẫn đang giảm (%d→%d) nhưng đã chạm trần %d vòng sửa (2 × workflow.max_review_rounds%s).",
			len(prev.Blocking()), len(blocking), ceiling, extra)
	}
	if stalled {
		head = fmt.Sprintf("review không hội tụ: vòng review %d còn %d phát hiện nghiêm trọng, không giảm so với vòng review %d (%d) sau %d vòng sửa nên dừng thay vì sửa tiếp (còn %d vòng sửa trong workflow.max_review_rounds = %d).",
			last.Round, len(blocking), prev.Round, len(prev.Blocking()), used-1, max(limit-(used-1), 0), r.Cfg.Workflow.MaxReviewRounds)
	}
	if red != nil { // vừa còn phát hiện nghiêm trọng vừa kiểm chứng đỏ: nêu cả kiểm chứng đỏ để người biết test nào đỏ
		how := fmt.Sprintf("mã thoát %d", red.ExitCode)
		if red.TimedOut {
			how = "quá giờ"
		}
		head += fmt.Sprintf(" Kiểm chứng cuối cũng không qua (`%s`, %s, log %s).", red.Command, how, cmpStr(red.Log, "(không có)"))
	}
	r.blockExhausted(rs, head)
	return false
}

// blockExhausted dừng tác vụ chờ người với ba lựa chọn (chấp nhận / sửa thêm một vòng / huỷ): đặt ReviewExhausted (nguồn của
// choicesFor và điều kiện của --accept/--fix-round) rồi chặn với lý do head.
func (r *Runner) blockExhausted(rs *runState, head string) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.ReviewExhausted = true; return nil }); err == nil {
		rs.t = t
	}
	id := rs.t.State.ID
	r.block(rs, fmt.Sprintf("%s Xem %s rồi chọn một: chấp nhận kết quả như hiện tại `%s`; cho sửa thêm một vòng `%s`; huỷ `bint task cancel %s`",
		head, filepath.Join(filepath.Dir(rs.t.ArtifactPath("plan")), task.ReviewMD),
		r.resumeWith(rs, "--accept"), r.resumeWith(rs, "--fix-round"), id))
}

// reviewRedVerify áp quyết định khi review không còn phát hiện nghiêm trọng nhưng lần kiểm chứng cuối red không qua. Trong ngân sách vòng sửa
// (workflow.max_review_rounds + vòng người cho thêm): mở vòng sửa (đường nhanh vòng 1 dùng đúng một vòng sửa như fastFix). Ngoài ngân sách:
// dừng với ba lựa chọn, câu đầu nêu kiểm chứng đỏ (không dùng thông điệp "không hội tụ": 0 phát hiện không phải review đứng yên).
func (r *Runner) reviewRedVerify(ctx context.Context, rs *runState, rv flow.Step, last *task.ReviewRound, red *task.Verification) bool {
	used := r.redFixRounds(rs, last)
	limit := r.fixLimit(rs)
	fix := rs.reviewFixStep(rv)
	how := fmt.Sprintf("mã thoát %d", red.ExitCode)
	if red.TimedOut {
		how = "quá giờ"
	}
	what := fmt.Sprintf("review vòng %d không còn phát hiện nghiêm trọng nhưng kiểm chứng cuối không qua (`%s`, %s, log %s)",
		last.Round, red.Command, how, cmpStr(red.Log, "(không có)"))
	if rs.fastPath() && last.Round == 1 && limit > 0 {
		return r.fastFix(ctx, rs, fix, last, nil)
	}
	if used > limit {
		r.blockExhausted(rs, fmt.Sprintf("%s; đã dùng hết vòng sửa (%d vòng sửa trong workflow.max_review_rounds = %d%s).", what, used-1, r.Cfg.Workflow.MaxReviewRounds,
			extraRoundsNote(rs)))
		return false
	}
	r.armReviewFix(ctx, rs, fix, true)
	r.setFlowStep(rs, fix)
	if err := r.transition(rs, task.StatusInProgress, fmt.Sprintf("sửa vì %s (vòng sửa %d/%d)", what, used, limit)); err != nil {
		r.fail(rs, rv.ID, err.Error())
		return false
	}
	return true
}

// extraRoundsNote nêu số vòng người cho thêm (--fix-round) trong lý do hết vòng sửa; rỗng khi không có.
func extraRoundsNote(rs *runState) string {
	if n := rs.t.State.ExtraFixRounds; n > 0 {
		return fmt.Sprintf(" + %d vòng người cho thêm", n)
	}
	return ""
}

// redFixReasonPrefix mở đầu lý do chuyển in_progress của vòng sửa mở vì kiểm chứng đỏ (reviewRedVerify): dùng để đếm các vòng đó.
const redFixReasonPrefix = "sửa vì review vòng"

// redFixRounds là số thứ tự vòng sửa sắp mở khi review sạch mà kiểm chứng đỏ (để so với ngân sách): flow một bước review là last.Round.
// Flow nhiều bước review: mỗi bước thêm một vòng vào review.json dù sạch nên last.Round không đếm được vòng sửa; đếm các vòng review trước
// có phát hiện nghiêm trọng (mỗi vòng là một lần quay về sửa) cộng các vòng sửa đã mở vì kiểm chứng đỏ (các vòng review của chúng sạch nên
// không có trong số trước) cộng vòng này.
func (r *Runner) redFixRounds(rs *runState, last *task.ReviewRound) int {
	if rs.countKind(flow.KindReview) <= 1 {
		return last.Round
	}
	n := 1
	if rev, err := rs.t.LoadReview(); err == nil {
		for _, rd := range rev.Rounds {
			if rd.Round < last.Round && len(rd.Blocking()) > 0 {
				n++
			}
		}
	}
	for _, tr := range rs.t.State.History {
		if tr.To == task.StatusInProgress && strings.HasPrefix(tr.Reason, redFixReasonPrefix) {
			n++
		}
	}
	return n
}

// reviewStalled cho biết review không hội tụ: từ vòng 2, số phát hiện nghiêm trọng của vòng last không giảm so với vòng review
// liền trước (prev, vòng đó cũng phải có phát hiện nghiêm trọng). Chỉ áp cho flow có một bước review (flow nhiều bước review xen
// kẽ các reviewer nên số hai vòng liền kề không so sánh được). Sau --fix-round vẫn so như thường: người cho thêm đúng một vòng sửa,
// vòng đó có giảm thì chạy tiếp, không giảm thì dừng lại hỏi người.
func (r *Runner) reviewStalled(rs *runState, last *task.ReviewRound) (prev *task.ReviewRound, stalled bool) {
	if last.Round < 2 || rs.countKind(flow.KindReview) > 1 {
		return nil, false
	}
	rev, err := rs.t.LoadReview()
	if err != nil {
		return nil, false
	}
	for i := range rev.Rounds {
		if rd := &rev.Rounds[i]; rd.Round < last.Round && (prev == nil || rd.Round > prev.Round) {
			prev = rd
		}
	}
	if prev == nil || len(prev.Blocking()) == 0 {
		return nil, false
	}
	return prev, len(last.Blocking()) >= len(prev.Blocking())
}

// reviewRoundsUsed là số vòng review đã dùng để so với workflow.max_review_rounds. Flow có một bước review (b-do, flow
// tuỳ biến thông thường): số vòng của review.json (last.Round). Flow có nhiều bước review: mỗi bước thêm một vòng vào
// review.json dù sạch, nên last.Round đếm cả lượt review không đòi sửa và ngân sách bị chia theo số bước review; chỉ
// đếm các vòng có phát hiện nghiêm trọng (mỗi vòng như vậy là một lần quay về sửa, kể cả vòng hiện tại).
func (r *Runner) reviewRoundsUsed(rs *runState, last *task.ReviewRound) int {
	if rs.countKind(flow.KindReview) <= 1 {
		return last.Round
	}
	rev, err := rs.t.LoadReview()
	if err != nil {
		return last.Round
	}
	n := 0
	for _, rd := range rev.Rounds {
		if rd.Round <= last.Round && len(rd.Blocking()) > 0 {
			n++
		}
	}
	for _, tr := range rs.t.State.History { // vòng sửa đã mở vì kiểm chứng đỏ dùng chung ngân sách
		if tr.To == task.StatusInProgress && strings.HasPrefix(tr.Reason, redFixReasonPrefix) {
			n++
		}
	}
	return max(n, 1) // vòng hiện tại có phát hiện nghiêm trọng (người gọi đã kiểm)
}

// setFlowStep ghi bước kế tiếp của flow vào phiếu (lỗi ghi không chặn: bước kế tiếp suy lại từ trạng thái).
func (r *Runner) setFlowStep(rs *runState, id string) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.FlowStep = id
		return nil
	}); err == nil {
		rs.t = t
	}
}

// nextReview trả bước review đứng sau bước id trong flow.
func (rs *runState) nextReview(id string) (flow.Step, bool) {
	seen := false
	for _, s := range rs.flow.Steps {
		switch {
		case s.ID == id:
			seen = true
		case seen && s.Kind == flow.KindReview:
			return s, true
		}
	}
	return flow.Step{}, false
}

// stopOnPlanning dừng workflow vì artifact b-req lỗi cấu trúc. Tác vụ đã ở `planned`: b-do chuyển
// `blocked` (resume về planned, không chạy lại b-req); workflow b-req chạy riêng giữ `planned` vì
// resume của nó sẽ gọi lại agent b-req và ghi đè bản người vừa sửa. Luôn trả false.
func (r *Runner) stopOnPlanning(rs *runState, rep task.ArtifactReport) bool {
	rs.res.PlanningIssues = rep.Errors
	reason := "artifact b-req chưa đạt hợp đồng v1: " + strings.Join(issueLines(rep.Errors), "; ") +
		" — sửa bằng `bint task write " + rs.t.State.ID + " <requirement|plan|tasks> -`, kiểm bằng `bint task lint " +
		rs.t.State.ID + "`, rồi " + r.continueHint(rs)
	if rs.flow.HasKind(flow.KindImplement) {
		r.block(rs, reason)
	} else {
		rs.res.Reason = reason
	}
	return false
}

// continueHint là lệnh tiếp tục sau khi người sửa artifact/trả lời câu hỏi. b-do/b-feat chuyển tác vụ
// sang blocked nên `bint run resume` vào thẳng bước kế tiếp (không chạy lại b-req); riêng câu hỏi đã trả lời của b-req thì
// resume (và b-do --task) nối tiếp phiên planner cũ khi nối tiếp được (docs/step-continuation-plan.md giai đoạn 3). Workflow
// b-req chạy riêng giữ tác vụ ở `planned` và KHÔNG được dùng resume — khi chưa nối tiếp được, resume của b-req gọi lại agent b-req
// và ghi đè bản người vừa sửa; tiếp tục bằng b-do (nối tiếp planner rồi sang b-feat) hoặc b-feat. (Quyết định theo workflow,
// không theo trạng thái hiện tại, vì lúc dựng lý do tác vụ chưa được chuyển sang blocked.)
func (r *Runner) continueHint(rs *runState) string {
	id := rs.t.State.ID
	if rs.flow.HasKind(flow.KindImplement) {
		return "`" + r.resumeCmd(rs) + "`"
	}
	return "`bint run b-do --task " + id + "` (nối tiếp planner cũ nếu được rồi sang b-feat; hoặc `bint run b-feat --task " + id +
		"` vào thẳng b-feat; không dùng `resume` vì khi chưa nối tiếp được sẽ chạy lại b-req)"
}

// preflightFeat lint lại artifact trước mỗi lần gọi agent b-feat trên tác vụ v1 đã qua b-req
// (planned/in_progress, gồm vòng sửa sau review): bắt artifact bị sửa/xóa sau lúc lập kế hoạch.
// Tác vụ v1 còn `created` gọi thẳng b-feat bỏ qua b-req: không có artifact để gate nên hạ về v0 — nếu
// giữ v1 thì lần b-feat thứ hai (resume, vòng sửa sau review) bị chặn oan vì thiếu artifact và không có
// đường ra. Tác vụ cũ (v0) không bị chặn.
func (r *Runner) preflightFeat(rs *runState) bool {
	t := rs.t
	if t.State.PlanningContractVersion < task.PlanningContractV1 {
		return true
	}
	switch t.State.Status {
	case task.StatusCreated:
		if nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
			t.State.PlanningContractVersion = 0
			return nil
		}); err == nil {
			rs.t = nt
		}
		return true
	case task.StatusPlanned, task.StatusInProgress, task.StatusInReview:
	default:
		return true
	}
	rep := task.LintPlanningTask(t, true)
	if rep.Blocking() {
		return r.stopOnPlanning(rs, rep)
	}
	if len(rep.OpenQuestions) > 0 {
		return r.stopOnQuestions(rs, rep)
	}
	return r.preflightPlanSize(rs)
}

// stopOnQuestions dừng vì requirement.md còn câu hỏi cản trở triển khai. Tác vụ đã ở `planned`;
// b-do/b-feat chuyển `blocked` (resume về planned rồi vào thẳng b-feat sau khi có câu trả lời, không
// chạy lại b-req); workflow b-req chạy riêng giữ `planned` vì resume của nó gọi lại agent b-req.
// Core chỉ kiểm hình thức có câu trả lời, không xác thực nội dung. Luôn trả false.
func (r *Runner) stopOnQuestions(rs *runState, rep task.ArtifactReport) bool {
	qs := questionLines(rep.OpenQuestions)
	r.markAwaitingHuman(rs)
	rs.res.Questions = qs
	id := rs.t.State.ID
	reason := fmt.Sprintf("còn %d câu hỏi mở trong requirement.md: %s — ghi câu trả lời (`- [x] Q-N: … — Trả lời: …`) bằng "+
		"`bint task write %s requirement -`, kiểm bằng `bint task lint %s`, rồi %s",
		len(qs), strings.Join(qs, "; "), id, id, r.continueHint(rs))
	if rs.flow.HasKind(flow.KindImplement) {
		r.block(rs, reason)
	} else {
		rs.res.Reason = reason
	}
	return false
}

func passWord(ok bool) string {
	if ok {
		return "qua"
	}
	return "không qua"
}

// checkScope đối chiếu tệp đã sửa với phạm vi phiếu và đường dẫn bảo vệ (mục 5, 6.7).
func (r *Runner) checkScope(ctx context.Context, rs *runState) bool {
	var files []string
	var err error
	if rs.featScopeSet {
		files, err = rs.featScope, rs.featScopeErr
		rs.featScope, rs.featScopeErr, rs.featScopeSet = nil, nil, false
	} else {
		// Resume (b-feat chạy ở lượt trước): khoá chia sẻ đợi b-feat đang chạy của tác vụ khác xong nên tệp
		// dở dang của nó không bị tính; nhả ngay, trước khi hỏi người.
		lock, ok := r.lockStep(ctx, rs, "scope", LockShared)
		if !ok {
			return false
		}
		files, err = ScopeFiles(ctx, r.Root, r.Tasks, rs.t, r.Cfg.Limits.ProtectedPaths)
		lock.Release()
	}
	if err != nil {
		r.fail(rs, "scope", "không xác định được tệp đã sửa: "+err.Error())
		return false
	}
	files = r.withoutIntactGoal(r.Root, rs.t, files) // tệp kiểm tra mục tiêu đóng băng còn nguyên: không phải thay đổi cần duyệt/đếm
	rs.scopeFiles = files
	sc := task.CheckScope(files, rs.t.Ticket, r.Cfg.Limits.ProtectedPaths)
	approved := ApprovedPaths(rs.t)
	var need []string
	for _, f := range sc.NeedsApproval() {
		if !slices.Contains(approved, f) {
			need = append(need, f)
		}
	}
	// Ngoài scope.allow (không bảo vệ, không deny): chỉ hỏi khi autonomy ask_always.
	if len(sc.OutsideAllow) > 0 {
		if rs.t.Ticket.Autonomy != task.AutonomyAskAlways {
			rs.res.Notes = append(rs.res.Notes, "sửa ngoài scope.allow: "+strings.Join(sc.OutsideAllow, ", "))
		} else {
			for _, f := range sc.OutsideAllow {
				if !slices.Contains(approved, f) {
					need = append(need, f)
				}
			}
		}
	}
	// Chồng lấn: tệp mà tác vụ chưa kết thúc khác trên cùng worktree cũng sửa. Không tự xử lý: đi qua
	// luồng duyệt (hỏi / PendingPaths / bint task approve) để người quyết định chấp nhận chồng lấn.
	var overlapNotes []string
	ov := r.overlaps(rs.t)
	ids := make([]string, 0, len(ov))
	for id := range ov {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var fresh []string
		for _, f := range ov[id] {
			if !slices.Contains(approved, f) {
				fresh = append(fresh, f)
			}
		}
		if len(fresh) > 0 {
			overlapNotes = append(overlapNotes, fmt.Sprintf("tác vụ %s cũng sửa: %s", id, strings.Join(fresh, ", ")))
			need = appendUnique(need, fresh...)
		}
	}
	if len(need) == 0 {
		return true
	}
	// Tệp cần duyệt thuộc Touched của tác vụ KHÁC (không phải do t sửa): nêu tác vụ sở hữu và trạng thái của nó.
	own := r.owners(rs.t, need)
	for _, f := range need {
		for i, o := range own[f] {
			if i == maxOwnersPerFile {
				overlapNotes = append(overlapNotes, fmt.Sprintf("%s: và %d tác vụ khác", f, len(own[f])-i))
				break
			}
			state := "(đang chạy)"
			if o.Done {
				state = "(đã kết thúc)"
			}
			overlapNotes = append(overlapNotes, fmt.Sprintf("%s thuộc tác vụ %s %s", f, o.ID, state))
		}
	}
	why := ""
	if len(overlapNotes) > 0 {
		why = "\n" + strings.Join(overlapNotes, "\n")
	}
	q := Question{Key: QConfirmScope, Paths: slices.Clone(need),
		Text: fmt.Sprintf("Có thay đổi cần duyệt (đường dẫn bảo vệ / deny / ngoài phạm vi / tác vụ khác cũng sửa):\n  %s%s\nDuyệt các thay đổi này?", strings.Join(need, "\n  "), why)}
	if r.Confirm != nil && r.Confirm(q) {
		t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			prev := ApprovedPaths(t)
			t.State.ProtectedApproval = &task.Approval{Paths: appendUnique(append([]string{}, prev...), need...), By: r.actor(), At: r.at()}
			return nil
		})
		if err == nil {
			rs.t = t
			return true
		}
	}
	t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.PendingPaths = need
		return nil
	})
	if err == nil {
		rs.t = t
	}
	r.markAwaitingHuman(rs)
	r.block(rs, "thay đổi cần người duyệt: "+strings.Join(need, ", ")+strings.Join(append([]string{""}, overlapNotes...), "; ")+" (xem diff, rồi bint task approve "+rs.t.State.ID+" && "+r.resumeCmd(rs)+")")
	return false
}

// finalize: chuyển done nếu đủ bằng chứng, nếu không thì chờ người với lý do cụ thể. Tác vụ worktree: kiểm
// điều kiện done (ở worktree) → đưa về cây gốc (Land) → done. Xung đột ⇒ blocked "xung đột khi đưa về: …".
// Trả true khi tác vụ quay về bước sửa (kiểm chứng đầy đủ hỏng sau lần kiểm chứng affected, còn vòng): workflow đi tiếp.
func (r *Runner) finalize(ctx context.Context, rs *runState) bool {
	switch r.reverifyIfStale(ctx, rs) {
	case reverifyStop:
		return false
	case reverifyFix:
		return true
	}
	if !r.goalFinal(ctx, rs) { // cổng mục tiêu: lần chạy cuối phải qua trên đúng cây cuối
		return false
	}
	if rs.t.State.NeedsLand() {
		if !r.landStep(ctx, rs) {
			return false
		}
	}
	t, blockers, err := r.SetStatus(ctx, rs.t.State.ID, task.StatusDone, TransitionOptions{Reason: rs.flow.Name + " finalize", Orchestrator: true})
	if t != nil {
		rs.t = t
	}
	if err == nil {
		r.logf("✓ Hoàn thành")
		if n := phaseNote(rs.t, true); n != "" {
			rs.res.Notes = append(rs.res.Notes, n)
		}
		return false
	}
	var nd *NotDoneError
	if errors.As(err, &nd) {
		r.blockFinalize(ctx, rs, "chưa đủ bằng chứng hoàn thành: "+strings.Join(blockers, "; ")+"; xử lý xong thì `"+r.resumeCmd(rs)+"`")
		return false
	}
	r.fail(rs, "finalize", err.Error())
	return false
}

// reverifyResult là kết quả reverifyIfStale.
type reverifyResult int

const (
	reverifyOK   reverifyResult = iota // đi tiếp tới land/done
	reverifyStop                       // workflow dừng (đã đặt trạng thái)
	reverifyFix                        // kiểm chứng đầy đủ hỏng: tác vụ về bước sửa, workflow đi tiếp
)

// fullVerifyFailRounds là số lần kiểm chứng đầy đủ ở finalize (sau lần kiểm chứng affected) đã hỏng, kể cả lần cuối.
func fullVerifyFailRounds(t *task.Task) int {
	n := 0
	for _, v := range t.State.Verifications {
		if !v.Passed && v.Full() && strings.HasPrefix(v.Reason, partialReasonPrefix) {
			n++
		}
	}
	return n
}

// fixAfterFullVerify đưa tác vụ về bước sửa khi kiểm chứng đầy đủ ở finalize hỏng và còn vòng trong
// workflow.max_review_rounds (cùng ngân sách với vòng sửa sau review). Trả false khi hết vòng hoặc chuyển trạng thái lỗi.
func (r *Runner) fixAfterFullVerify(rs *runState, how string) bool {
	used := fullVerifyFailRounds(rs.t)
	if used > r.fixLimit(rs) {
		return false
	}
	r.setFinalizeFP(rs, "")
	r.setFlowStep(rs, rs.lastImplement().ID)
	if err := r.transition(rs, task.StatusInProgress, fmt.Sprintf(fixVerifyReasonPrefix+" (%s) sau lần kiểm chứng chỉ test bị ảnh hưởng (vòng %d/%d)",
		how, used, r.fixLimit(rs))); err != nil {
		r.fail(rs, "finalize", err.Error())
		return false
	}
	return true
}

// reverifyIfStale: khi cổng done chỉ bị chặn vì tệp NGOÀI tác vụ đổi sau lần kiểm chứng cuối (staleVerifyOnly) thì tự
// chạy lại lệnh kiểm chứng đã được xác nhận, đúng một lần, dưới khoá độc quyền (như kiểm chứng sau bước implement), ghi
// một Verification mới có lý do. Dùng chung cho driver process và session (cả hai vào finalize). Trả false nếu workflow
// phải dừng (đã đặt trạng thái): lỗi, huỷ, hoặc kiểm chứng lại không qua. Trường hợp khác trả true và để cổng done
// quyết định như cũ (kể cả khi lệnh bị bỏ qua).
func (r *Runner) reverifyIfStale(ctx context.Context, rs *runState) reverifyResult {
	forced := rs.finalizeResume
	rs.finalizeResume, rs.verifiedResume = false, false
	files, ok := r.staleVerifyOnly(ctx, rs.t)
	partial := false
	if !ok {
		partial = r.partialVerifyOnly(ctx, rs.t)
	}
	if !ok && !partial && !forced {
		return reverifyOK
	}
	if forced && !ok && !partial && r.freshFullPass(ctx, rs.t) {
		// Người đã chạy lại kiểm chứng (bint task verify) sau lúc chặn và lần đó qua đầy đủ trên đúng cây hiện tại.
		r.setFinalizeFP(rs, "")
		rs.res.Notes = append(rs.res.Notes, "lần kiểm chứng mới nhất đã qua đầy đủ trên đúng cây hiện tại: không chạy lại")
		return reverifyOK
	}
	id := rs.t.State.ID
	reason := "tiếp tục sau khi finalize bị chặn (worktree không đổi, không review lại)"
	if ok {
		reason = reverifyReason(files)
	} else if partial {
		reason = partialReason(rs.t.State.LastVerification().Scope)
	}
	r.logf("  %s: chạy lại kiểm chứng", reason)
	lock, locked := r.lockStep(ctx, rs, "verify", LockExclusive)
	if !locked {
		return reverifyStop
	}
	t, vr, err := r.VerifyWith(ctx, id, reason)
	lock.Release()
	switch {
	case err != nil && ctx.Err() != nil:
		r.interrupt(rs, "người dùng huỷ khi kiểm chứng")
		return reverifyStop
	case err != nil:
		r.fail(rs, "verify", err.Error())
		return reverifyStop
	case vr.Skipped != "":
		rs.res.Notes = append(rs.res.Notes, "kiểm chứng: "+vr.Skipped)
		return reverifyOK
	}
	rs.t = t
	r.setFinalizeFP(rs, "") // đã kiểm chứng lại xong: dấu vân tay cũ hết giá trị (hỏng lại thì blockFinalize đặt mới)
	v := vr.Verification
	r.logf("  kiểm chứng: %s → %s", v.Command, passWord(v.Passed))
	rs.res.Notes = append(rs.res.Notes, "đã tự kiểm chứng lại một lần ("+reason+") → "+passWord(v.Passed))
	if v.Passed {
		return reverifyOK
	}
	how := fmt.Sprintf("mã thoát %d", v.ExitCode)
	if v.TimedOut {
		how = "quá giờ"
	}
	if partial {
		if r.fixAfterFullVerify(rs, how) {
			return reverifyFix
		}
		if rs.t.State.Status != task.StatusInReview {
			return reverifyStop // chuyển trạng thái lỗi (đã r.fail)
		}
	}
	r.blockFinalize(ctx, rs, fmt.Sprintf("chưa đủ bằng chứng hoàn thành: tự kiểm chứng lại sau khi %s không qua (%s, log %s); "+
		"xử lý xong thì `%s`", reason, how, v.Log, r.resumeCmd(rs)))
	return reverifyStop
}

// blockFinalize chặn tác vụ ở finalize vì thiếu bằng chứng, nhớ dấu vân tay worktree để resume không đổi thì không mở
// lượt b-review mới (chỉ chạy lại kiểm chứng một lần rồi áp cổng done).
func (r *Runner) blockFinalize(ctx context.Context, rs *runState, reason string) {
	// Chỉ khi lần kiểm chứng cuối không qua (kiểm chứng hỏng, có thể do lý do ngoài mã); chặn vì lý do khác (lệnh chưa
	// xác nhận, tiêu chí chưa đạt...) thì xoá dấu vân tay: resume vẫn review lại như cũ.
	fp := ""
	if v := rs.t.State.LastVerification(); v != nil && !v.Passed {
		if root, err := r.RootFor(rs.t); err == nil {
			fp = task.WorktreeFingerprint(ctx, root)
		}
	}
	r.setFinalizeFP(rs, fp)
	r.blockKeep(rs, reason)
}

// markFinalizeResume đặt rs.finalizeResume khi resume từ blocked ở finalize (to = in_review) mà worktree không đổi kể từ
// lúc chặn, không --rerun và không có bước giữ. Gọi sau khi đã chuyển trạng thái.
func (r *Runner) markFinalizeResume(ctx context.Context, rs *runState, to string, rerun bool) {
	st := &rs.t.State
	if rerun || to != task.StatusInReview || st.FinalizeFP == "" || st.HeldStep != nil || st.Landed != nil {
		return
	}
	root, err := r.RootFor(rs.t)
	if err != nil {
		return
	}
	if task.WorktreeFingerprint(ctx, root) == st.FinalizeFP {
		rs.finalizeResume = true
		rs.res.Notes = append(rs.res.Notes, "worktree không đổi kể từ lúc finalize bị chặn: không review lại, chạy lại kiểm chứng một lần")
	}
}

// resumeCmd là lệnh tiếp tục tác vụ theo driver của lượt này (session: bint run next <id> --resume).
func (r *Runner) resumeCmd(rs *runState) string {
	if rs.session {
		return "bint run next " + rs.t.State.ID + " --resume"
	}
	return rs.t.State.ResumeCommand()
}

// landStep đưa thay đổi của tác vụ worktree về cây gốc ở finalize. Trả false nếu workflow phải dừng (đã đặt
// trạng thái). Điều kiện done được kiểm bên trong Land, ở worktree.
func (r *Runner) landStep(ctx context.Context, rs *runState) bool {
	id := rs.t.State.ID
	res, err := r.Land(ctx, id, LandOptions{Orchestrator: true})
	var (
		nd *NotDoneError
		ce *LandConflictError
	)
	switch {
	case err == nil:
		rs.t = res.Task
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("đã đưa %d tệp về cây gốc %s; kiểm chứng đã chạy trên cây cô lập (worktree), "+
			"cây gốc có thể còn thay đổi khác chưa được kiểm cùng", len(res.Files), rs.t.State.Origin))
		rs.res.Notes = append(rs.res.Notes, res.Notes...)
		r.logf("✓ Đã đưa %d tệp về cây gốc %s", len(res.Files), rs.t.State.Origin)
		return true
	case errors.As(err, &nd):
		r.blockFinalize(ctx, rs, "chưa đủ bằng chứng hoàn thành: "+strings.Join(nd.Blockers, "; ")+"; xử lý xong thì `"+r.resumeCmd(rs)+"`")
	case errors.As(err, &ce):
		r.awaitLand(rs)
		r.block(rs, err.Error()+"; xong thì `"+r.resumeCmd(rs)+"` (không chạy lại review)")
	default:
		r.awaitLand(rs)
		r.fail(rs, "land", "đưa về cây gốc: "+err.Error())
	}
	return false
}

// awaitLand đánh dấu tác vụ chỉ còn việc đưa về: resume vào thẳng finalize.
func (r *Runner) awaitLand(rs *runState) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.AwaitingLand = true; return nil }); err == nil {
		rs.t = t
	}
}

// finish đóng lượt điều phối: giải phóng tác vụ, ghi workflow.finished.
func (r *Runner) finish(rs *runState) {
	t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		if t.State.Runner != nil && t.State.Runner.RunID == rs.runID {
			t.State.Runner = nil
		}
		return nil
	})
	if err == nil {
		rs.t = t
	}
	st := rs.t.State
	status := "completed"
	switch st.Status {
	case task.StatusFailed:
		status = "failed"
	case task.StatusInterrupted:
		status = "interrupted"
	case task.StatusCancelled:
		status = "cancelled"
	}
	payload := map[string]any{"status": status, "end_reason": "task " + st.Status + reasonSuffix(st.Reason),
		"result_summary": fmt.Sprintf("task %s: %s", st.ID, st.Status), "task_id": st.ID}
	if v := st.LastVerification(); v != nil {
		if v.Passed {
			payload["verification_status"] = "passed"
		} else {
			payload["verification_status"] = "failed"
		}
	}
	if rs.costKnown {
		c := rs.spent
		if rs.costMissing {
			// Tổng một phần không được trình bày như tổng thật (thiếu = unknown, không phải 0).
			payload["cost_usd_partial"] = c
			rs.res.CostPartialUSD = &c
		} else {
			payload["cost_usd"] = c
			rs.res.CostUSD = &c
		}
	}
	r.emitRun(rs.runID, spool.WorkflowFinished, payload)
	rs.res.Isolation = st.Isolation()
	if st.Origin != "" {
		rs.res.Worktree, rs.res.Branch, rs.res.Origin = st.Worktree, st.Branch, st.Origin
	}
	if st.Landed != nil {
		rs.res.Landed, rs.res.LandedFiles = true, st.Landed.Files
	}
	rs.res.Status = st.Status
	if rs.res.Reason == "" {
		rs.res.Reason = st.Reason
	}
	if len(rs.res.Choices) == 0 {
		rs.res.Choices = choicesFor(rs.t)
	}
	r.attachGreen(rs)
}

func reasonSuffix(s string) string {
	if s == "" {
		return ""
	}
	return ": " + truncate(s, 300)
}

// emitRun ghi sự kiện của một lượt chạy do Bint cấp.
func (r *Runner) emitRun(runID, typ string, payload map[string]any) {
	if err := r.Env.emitRun(runID, typ, payload); err != nil {
		r.logf("! ghi sự kiện: %v", err)
	}
}

// stepSkill nạp skill của bước: skill gốc ghép lớp phủ của repository, hoặc skill tuỳ biến (.bint/skills/<tên>) qua kiểm
// hình thức. Skill không có hoặc lỗi lint là lỗi: bước không chạy với đề bài rỗng.
func (r *Runner) stepSkill(name string) (skills.Skill, error) {
	croot := r.configRoot()
	if sk, ok := skills.Get(name); ok {
		if c, _, err := skills.Composed(croot, sk); err == nil {
			sk = c
		}
		return sk, nil
	}
	cs, err := skills.LoadCustomOne(croot, name)
	if err != nil {
		return skills.Skill{}, fmt.Errorf("skill %q của bước không dùng được: %w", name, err)
	}
	if errs, _ := skills.CheckCustom(croot, cs); len(errs) > 0 {
		return skills.Skill{}, fmt.Errorf("skill tuỳ biến %q có lỗi (xem `bint skill lint %s`): %s", name, name, strings.Join(errs, "; "))
	}
	return cs.AsSkill(), nil
}

// prompt dựng đề bài cho một bước: nội dung skill (gốc + lớp phủ) và khối tác vụ.
func (r *Runner) prompt(ctx context.Context, rs *runState, step, child string, session bool) string {
	t := rs.t
	stepDef, _ := rs.flow.StepByID(step)
	skill := stepDef.Skill
	if skill == "" {
		skill = step
	}
	sk, _ := r.stepSkill(skill) // lỗi đã được beginStep chặn trước khi dựng đề bài
	body := strings.ReplaceAll(sk.Body(), "$ARGUMENTS", "tác vụ "+t.State.ID)
	var b strings.Builder
	b.WriteString(body)
	fmt.Fprintf(&b, "\n\n---\n\n<bint-task id=%q step=%q run=%q>\n", t.State.ID, step, child)
	if session {
		// Subagent không có biến môi trường riêng: ID tác vụ chỉ nằm trong khối này, lệnh `bint task …` phải truyền nó.
		fmt.Fprintf(&b, "Bạn đang chạy như subagent của một phiên điều phối (Bint Core điều phối, `bint run next/complete`). Không gọi "+
			"`bint task status`, `bint task approve` hay `bint task verify` — Bint Core chuyển trạng thái, chạy lệnh kiểm chứng và quyết định "+
			"hoàn thành. Không có biến BINT_TASK_ID/BINT_RUN_ID: mọi lệnh `bint task …` phải truyền ID tác vụ %s (ví dụ "+
			"`bint task write %s <requirement|plan|tasks> -`).\n\n", t.State.ID, t.State.ID)
	} else {
		b.WriteString("Bạn đang chạy dưới `bint run` (Bint Core điều phối). Không gọi `bint task status`, `bint task approve` hay `bint task verify` " +
			"— Bint Core chuyển trạng thái, chạy lệnh kiểm chứng và quyết định hoàn thành. Task ID cũng có trong biến BINT_TASK_ID.\n\n")
	}
	if t.State.Origin != "" {
		b.WriteString(r.worktreeNote(t))
		if session {
			// Công cụ Agent không nhận thư mục làm việc: subagent bắt đầu ở thư mục của phiên cha.
			// Tiền tố cd trích dẫn sẵn (hookcore.CdPrefix): đường dẫn có dấu cách (macOS) hoặc `\` (Windows) vẫn đúng cú pháp và
			// khớp guardrail (review G4 .1).
			cd := hookcore.CdPrefix(r.Root)
			fmt.Fprintf(&b, "Bạn đang đứng ở thư mục của phiên cha, KHÔNG phải worktree: mọi lệnh Bash phải bắt đầu bằng `%s…` "+
				"(giữ nguyên dấu nháy), và Read/Edit/Write phải dùng đường dẫn tuyệt đối dưới %s. Hook của Bint chặn (deny) "+
				"Edit/Write/NotebookEdit có đường dẫn ngoài thư mục đó và lệnh Bash không bắt đầu bằng `%s…`; bị chặn thì sửa lại "+
				"đường dẫn hoặc lệnh, đừng thử vòng qua.\n", cd, r.Root, cd)
		}
		b.WriteString("\n")
	}
	if stepDef.Kind == flow.KindPlan {
		depth, reason := task.DepthStandard, "mặc định (tác vụ chưa ghi độ sâu)"
		if c := t.State.Classification; c != nil {
			depth = c.EffectiveDepth()
			if c.DepthReason != "" {
				reason = c.DepthReason
			}
		}
		fmt.Fprintf(&b, "Phân tích: %s\nLý do: %s\n(Độ sâu do Bint Core chọn một lần lúc tạo tác vụ; làm theo mục \"Độ sâu phân tích\" của skill.)\n\n", depth, reason)
	}
	ticket, _ := yaml.Marshal(t.Ticket)
	fmt.Fprintf(&b, "Phiếu tác vụ (task.yml):\n```yaml\n%s```\n", ticket)
	for _, n := range task.ArtifactNames() {
		if stepDef.Kind == flow.KindGoal && n != "requirement" {
			continue // b-goal chỉ thấy requirement.md: không nêu đường dẫn plan.md/tasks.md
		}
		p := t.ArtifactPath(n)
		if _, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "- %s: %s\n", n, p)
		}
	}
	if len(r.Cfg.Limits.ProtectedPaths) > 0 {
		fmt.Fprintf(&b, "\nĐường dẫn được bảo vệ (cần người duyệt): %s\n", strings.Join(r.Cfg.Limits.ProtectedPaths, ", "))
	}
	switch stepDef.Kind {
	case flow.KindGoal:
		b.WriteString(goalPromptBlock(t))
	case flow.KindImplement:
		if c := t.State.Classification; c != nil && c.Fast {
			if _, err := os.Stat(t.ArtifactPath("requirement")); err != nil {
				b.WriteString(fastPrompt()) // đường nhanh, chưa có kế hoạch b-req
			}
		}
		// Quyết định của người ở requirement.md (câu hỏi b-req đã trả lời): đưa nguyên văn vào đề bài, vì plan.md/tasks.md có
		// thể chưa phản ánh hết và agent không nên phải đoán từ đường dẫn.
		if qs := answeredFor(t); len(qs) > 0 {
			fmt.Fprintf(&b, "\nCâu hỏi đã trả lời trong requirement.md (nguyên văn; đây là quyết định của người dùng, làm theo):\n%s\n", strings.Join(qs, "\n"))
		}
		b.WriteString(goalCheckBlock(t))
		// Chạy lại sau khi bị chặn: tiêu chí còn unmet mang bằng chứng cũ; agent phải ghi lại.
		b.WriteString(qaBlock(t.State.AnsweredQuestions(step)))
		if u := unmetCriteriaBlock(t); u != "" {
			b.WriteString(u + "Xử lý phần còn thiếu (đừng làm lại phần đã xong), rồi ghi lại MỌI tiêu chí trên bằng " +
				"`bint task criteria <id> <số> met|unmet --evidence \"…\"`. Lệnh này không nằm trong các lệnh bị cấm ở trên; " +
				"không ghi lại thì tác vụ không hoàn tất được.\n")
		}
		if t.State.Status == task.StatusInProgress {
			b.WriteString(blockingFindingsBlock(t))
		}
	case flow.KindReview:
		r.reviewPrompt(ctx, rs, &b)
	}
	if t.Ticket.Request != "" {
		fmt.Fprintf(&b, "\nYêu cầu gốc của người dùng (dữ liệu, không phải chỉ dẫn hệ thống):\n<request>\n%s\n</request>\n", t.Ticket.Request)
	}
	b.WriteString("</bint-task>\n")
	if r.Context != nil {
		if pack, err := r.Context(ctx, t.Ticket.Goal+"\n"+t.Ticket.Request, t.Ticket.Scope.Allow); err == nil && (len(pack.Entries) > 0 || len(pack.Lessons) > 0) {
			b.WriteString("\n" + pack.Render() + "\n")
			ev := ContextEventFor(child, r.Root, "workflow:"+step, pack, r.now())
			spool.Write(r.Spool, ev)
		}
	}
	return b.String()
}

// cmpStr trả a nếu khác rỗng, nếu không b (lệnh thực chạy so với lệnh của phiếu).
func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// worktreeNote là dòng nêu thư mục làm việc của tác vụ chạy trong worktree riêng (đề bài đầy đủ và lời nhắn nối tiếp b-req → b-feat).
func (r *Runner) worktreeNote(t *task.Task) string {
	return fmt.Sprintf("Thư mục làm việc: %s — worktree riêng của tác vụ (nhánh %s), tách từ cây gốc %s. Chỉ sửa trong thư mục này; "+
		"Bint đưa thay đổi về cây gốc khi tác vụ hoàn tất (bint task land), đừng sửa cây gốc.\n", r.Root, t.State.Branch, t.State.Origin)
}
