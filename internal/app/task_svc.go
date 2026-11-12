package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/runlog"
	"bint/internal/task"
	"bint/internal/workflow"
)

// Dịch vụ tác vụ dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.3): mỗi lệnh `task *` có kiểu Options
// (thẻ `flag`/`arg`/`form`), Validate() và Defaults(cfg). CLI chỉ còn phân tích cờ và in văn bản.

// TaskWS là môi trường thao tác tác vụ của một repository (cần `bint init` trên máy này).
type TaskWS struct {
	*workflow.Env
	K *KnowledgeWS
}

// OpenTasks mở môi trường tác vụ của repository chứa dir. version là phiên bản Bint ghi vào sự kiện.
// Ngoài phiên agent thì thao tác là của người (Env.Human) và Actor là danh tính Git.
func OpenTasks(ctx context.Context, dir, version string) (*TaskWS, error) {
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !k.Paths.Initialized() {
		return nil, ErrNotInitialized
	}
	inAgent, _ := InAgentSession()
	env := &workflow.Env{
		Root: k.Repo.Toplevel, ConfigRoot: k.Root, Cfg: k.Config, Tasks: task.NewStore(k.Paths.Runtime),
		Spool: k.Paths.Spool, Version: version, Human: !inAgent, Actor: "agent",
		RunID: strings.TrimSpace(os.Getenv(workflow.EnvRunID)), Now: time.Now,
	}
	if env.Human {
		env.Actor = GitIdentity(ctx, k.Repo.Toplevel)
	}
	return &TaskWS{Env: env, K: k}, nil
}

// ---- phần sửa phiếu dùng chung cho task new / task set / run ----

// ticketFields gom các cờ sửa phiếu; *Set = người đặt tường minh (kể cả giá trị rỗng, vd. xoá lệnh kiểm chứng).
type ticketFields struct {
	goal, verify, autonomy             string
	goalSet, verifySet, autonomySet    bool
	criteria, allow, deny, constraints []string
	clearCriteria, clearScope          bool
}

func (f ticketFields) edit() workflow.TicketEdit {
	ed := workflow.TicketEdit{Criteria: f.criteria, Allow: f.allow, Deny: f.deny, Constraints: f.constraints,
		ClearCriteria: f.clearCriteria, ClearScope: f.clearScope}
	if f.goalSet || f.goal != "" {
		g := f.goal
		ed.Goal = &g
	}
	if f.verifySet || f.verify != "" {
		v := f.verify
		ed.VerifyCommand = &v
	}
	if f.autonomySet || f.autonomy != "" {
		a := f.autonomy
		ed.Autonomy = &a
	}
	return ed
}

// requireHumanForAutonomy: đặt autonomy: auto chỉ dành cho người (kiểm ở dịch vụ, không chỉ ở CLI).
func requireHumanForAutonomy(ed workflow.TicketEdit, op string) error {
	if ed.Autonomy != nil && *ed.Autonomy == task.AutonomyAuto {
		if err := RequireHuman(op + " --autonomy auto"); err != nil {
			return fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", err)
		}
	}
	return nil
}

// DepthFlagHelp là mô tả cờ --depth dùng chung.
const DepthFlagHelp = "độ sâu phân tích của b-req: auto | standard | deep (chỉ b-do/b-req; standard|deep hàm ý có b-req)"

func wfOrDefault(wf string) string {
	if wf == "" {
		return workflow.StepDo
	}
	return wf
}

// checkDepth kiểm cờ --depth: `auto` hoặc rỗng không đổi nghĩa --plan/--no-plan. Trả "" khi tự chọn.
func checkDepth(value string, noPlan bool, wf string) (string, error) {
	d, err := task.NormalizeDepth(value)
	if err != nil {
		return "", UsageError{err}
	}
	if d == "" {
		return "", nil
	}
	if noPlan {
		return "", Usagef("--depth standard|deep hàm ý có b-req: không đi cùng --no-plan")
	}
	if wf != workflow.StepDo && wf != workflow.StepReq {
		return "", Usagef("--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)", wf)
	}
	return d, nil
}

// isolationFlag đổi cặp cờ --worktree/--shared thành giá trị của workflow.NewTaskOptions.Isolation ("" = theo cấu hình).
func isolationFlag(worktree, shared bool) string {
	switch {
	case worktree:
		return task.IsolationWorktree
	case shared:
		return task.IsolationShared
	}
	return ""
}

// ---- task new ----

// TaskNewOptions: `bint task new [mô tả | -]`. Request là mô tả (CLI điền từ đối số, stdin hoặc --file).
type TaskNewOptions struct {
	Request          string   `arg:"description" form:"required,multiline"`
	Goal             string   `flag:"goal" form:"advanced"`
	VerifyCommand    string   `flag:"verify-command"`
	Criteria         []string `flag:"criteria"`
	Allow            []string `flag:"allow" form:"suggest=files"`
	Deny             []string `flag:"deny" form:"advanced,suggest=files"`
	Constraints      []string `flag:"constraint" form:"advanced"`
	Autonomy         string   `flag:"autonomy" form:"advanced,choices=ask_always|ask_on_protected|auto"`
	ClearScope       bool     `flag:"clear-scope" form:"advanced"`
	Workflow         string   `flag:"workflow" form:"choices=b-do|b-req|b-feat|b-review"`
	Plan             bool     `flag:"plan"`
	NoPlan           bool     `flag:"no-plan"`
	Depth            string   `flag:"depth" form:"advanced,choices=auto|standard|deep"`
	Worktree         bool     `flag:"worktree" form:"advanced"`
	Shared           bool     `flag:"shared" form:"advanced"`
	Driver           string   `flag:"driver" form:"advanced,choices=auto|process|session"`
	GoalSet          bool     // đặt tường minh dù rỗng (CLI: cờ có mặt)
	VerifyCommandSet bool
	AutonomySet      bool
}

// TaskNewDefaults: workflow b-do, độ sâu và driver tự chọn (như mặc định của cờ).
func TaskNewDefaults(config.Config) TaskNewOptions {
	return TaskNewOptions{Workflow: workflow.StepDo, Depth: task.DepthAuto, Driver: "auto"}
}

func (o TaskNewOptions) fields() ticketFields {
	return ticketFields{o.Goal, o.VerifyCommand, o.Autonomy, o.GoalSet, o.VerifyCommandSet, o.AutonomySet,
		o.Criteria, o.Allow, o.Deny, o.Constraints, false, o.ClearScope}
}

// Validate: --plan/--no-plan loại trừ nhau, cần mô tả, --depth hợp lệ với workflow.
func (o TaskNewOptions) Validate() error {
	if o.Plan && o.NoPlan {
		return Usagef("--plan và --no-plan loại trừ nhau")
	}
	if o.Worktree && o.Shared {
		return Usagef("--worktree và --shared loại trừ nhau")
	}
	if strings.TrimSpace(o.Request) == "" {
		return Usagef("thiếu mô tả tác vụ")
	}
	if _, err := workflow.NormalizeDriverFlag(o.Driver); err != nil {
		return UsageError{err}
	}
	_, err := checkDepth(o.Depth, o.NoPlan, wfOrDefault(o.Workflow))
	return err
}

// TaskCreated là kết quả lập phiếu.
type TaskCreated struct {
	Task            *task.Task
	Assessment      task.Assessment
	VerifyConfirmed bool
}

// NewTask lập phiếu tác vụ từ mô tả (bước Classify). Đặt autonomy: auto chỉ dành cho người.
func NewTask(ctx context.Context, ws *TaskWS, o TaskNewOptions) (TaskCreated, error) {
	var res TaskCreated
	if err := o.Validate(); err != nil {
		return res, err
	}
	wo := workflow.NewTaskOptions{Request: o.Request, Workflow: o.Workflow, Edit: o.fields().edit(), Isolation: isolationFlag(o.Worktree, o.Shared)}
	wo.Driver, _ = workflow.NormalizeDriverFlag(o.Driver) // đã kiểm ở Validate
	if o.Plan || o.NoPlan {
		p := o.Plan
		wo.Plan = &p
	}
	var err error
	if wo.Depth, err = checkDepth(o.Depth, o.NoPlan, wfOrDefault(o.Workflow)); err != nil {
		return res, err
	}
	if err := requireHumanForAutonomy(wo.Edit, "bint task new"); err != nil {
		return res, err
	}
	t, a, err := ws.NewTask(ctx, wo)
	if err != nil {
		return res, err
	}
	return TaskCreated{Task: t, Assessment: a, VerifyConfirmed: ws.VerifyConfirmed(t)}, nil
}

// ---- task list ----

// TaskListOptions: `bint task list`. NoReconcile không phải cờ: chế độ chỉ đọc của TUI không ghi lại
// trạng thái của tiến trình điều phối đã chết.
type TaskListOptions struct {
	All         bool `flag:"all"`
	NoReconcile bool
}

func TaskListDefaults(config.Config) TaskListOptions { return TaskListOptions{} }
func (o TaskListOptions) Validate() error            { return nil }

// TaskList là danh sách tác vụ (đã đối chiếu tiến trình điều phối) kèm lỗi đọc từng tệp.
type TaskList struct {
	Tasks []*task.Task
	Bad   []error
}

// ListTasks liệt kê tác vụ; không All thì bỏ done/cancelled.
func ListTasks(_ context.Context, ws *TaskWS, o TaskListOptions) (TaskList, error) {
	ts, bad := ws.Tasks.List()
	out := TaskList{Bad: bad}
	for _, t := range ts {
		if !o.NoReconcile {
			t = ws.Reconcile(t)
		}
		if !o.All && task.Terminal(t.State.Status) {
			continue
		}
		out.Tasks = append(out.Tasks, t)
	}
	return out, nil
}

// ---- task show ----

// TaskShowOptions: `bint task show <task-id>`. NoReconcile như TaskListOptions.
type TaskShowOptions struct {
	Ref         string `arg:"task-id" form:"required,suggest=tasks"`
	NoReconcile bool
}

func TaskShowDefaults(config.Config) TaskShowOptions { return TaskShowOptions{} }

func (o TaskShowOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskArtifact là một tài liệu của tác vụ (requirement/plan/tasks...).
type TaskArtifact struct {
	Name   string
	Path   string
	Exists bool
}

// TaskDetail là chi tiết một tác vụ.
type TaskDetail struct {
	Task            *task.Task
	Assessment      task.Assessment
	Review          *task.Review
	Blockers        []string // điều còn thiếu để chuyển done
	VerifyConfirmed bool
	// Setup là trạng thái xác nhận workflow.worktree.setup_command của tác vụ (Command rỗng = không có lệnh setup).
	Setup     workflow.SetupStatus
	Artifacts []TaskArtifact
	// Lint là kết quả kiểm tài liệu (nil nếu chưa có tài liệu nào).
	Lint *task.ArtifactReport
}

// ShowTask dựng chi tiết tác vụ: phiếu, đánh giá, review, điều chặn done, tài liệu và kết quả lint.
func ShowTask(ctx context.Context, ws *TaskWS, o TaskShowOptions) (TaskDetail, error) {
	var d TaskDetail
	t, err := ws.Tasks.Load(o.Ref)
	if err != nil {
		return d, err
	}
	if !o.NoReconcile {
		t = ws.Reconcile(t)
	}
	rev, err := t.LoadReview()
	if err != nil {
		return d, err
	}
	d = TaskDetail{Task: t, Assessment: ws.Assess(t), Review: rev, VerifyConfirmed: ws.VerifyConfirmed(t), Setup: ws.SetupStatus(t)}
	in, derr := ws.DoneInputFor(ctx, t)
	d.Blockers = task.DoneBlockers(t, in)
	if derr != nil {
		d.Blockers = append(d.Blockers, derr.Error())
	}
	any := false
	for _, n := range task.ArtifactNames() {
		a := TaskArtifact{Name: n, Path: t.ArtifactPath(n)}
		if _, err := os.Stat(a.Path); err == nil {
			a.Exists, any = true, true
		}
		d.Artifacts = append(d.Artifacts, a)
	}
	if any {
		rep := task.LintPlanningTask(t, t.State.PlanningContractVersion >= task.PlanningContractV1)
		d.Lint = &rep
	}
	return d, nil
}

// ReadTaskArtifact đọc nội dung một tài liệu của tác vụ ("" nếu chưa có).
func ReadTaskArtifact(a TaskArtifact) (string, error) {
	if !a.Exists {
		return "", nil
	}
	b, err := os.ReadFile(a.Path)
	return string(b), err
}

// ---- task set ----

// TaskSetOptions: `bint task set <task-id>`. Danh sách (criteria/allow/deny/constraint) thêm vào danh sách hiện có.
type TaskSetOptions struct {
	Ref              string   `arg:"task-id" form:"required,suggest=tasks"`
	Goal             string   `flag:"goal"`
	VerifyCommand    string   `flag:"verify-command"`
	Criteria         []string `flag:"criteria"`
	Allow            []string `flag:"allow" form:"suggest=files"`
	Deny             []string `flag:"deny" form:"advanced,suggest=files"`
	Constraints      []string `flag:"constraint" form:"advanced"`
	Autonomy         string   `flag:"autonomy" form:"advanced,choices=ask_always|ask_on_protected|auto"`
	ClearCriteria    bool     `flag:"clear-criteria" form:"advanced"`
	ClearScope       bool     `flag:"clear-scope" form:"advanced"`
	GoalSet          bool
	VerifyCommandSet bool
	AutonomySet      bool
}

func TaskSetDefaults(config.Config) TaskSetOptions { return TaskSetOptions{} }

// TaskSetFrom dựng giá trị khởi đầu của form sửa phiếu từ phiếu hiện có (mục tiêu, lệnh kiểm chứng, autonomy
// đã điền sẵn và coi là đặt tường minh nên xoá trắng lệnh kiểm chứng được).
func TaskSetFrom(t *task.Task) TaskSetOptions {
	return TaskSetOptions{Ref: t.State.ID, Goal: t.Ticket.Goal, VerifyCommand: t.Ticket.Verify.Command, Autonomy: t.Ticket.Autonomy,
		GoalSet: true, VerifyCommandSet: true, AutonomySet: true}
}

func (o TaskSetOptions) fields() ticketFields {
	return ticketFields{o.Goal, o.VerifyCommand, o.Autonomy, o.GoalSet, o.VerifyCommandSet, o.AutonomySet,
		o.Criteria, o.Allow, o.Deny, o.Constraints, o.ClearCriteria, o.ClearScope}
}

func (o TaskSetOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskUpdated là kết quả sửa phiếu.
type TaskUpdated struct {
	Task       *task.Task
	Assessment task.Assessment
}

// SetTask sửa phiếu. Đặt autonomy: auto chỉ dành cho người.
func SetTask(_ context.Context, ws *TaskWS, o TaskSetOptions) (TaskUpdated, error) {
	if err := o.Validate(); err != nil {
		return TaskUpdated{}, err
	}
	ed := o.fields().edit()
	if err := requireHumanForAutonomy(ed, "bint task set"); err != nil {
		return TaskUpdated{}, err
	}
	t, a, err := ws.SetTicket(o.Ref, ed)
	return TaskUpdated{Task: t, Assessment: a}, err
}

// ---- task write ----

// TaskWriteOptions: `bint task write <task-id> <requirement|plan|tasks> [tệp | -]`. Content là nội dung
// (CLI điền từ tệp hoặc stdin).
type TaskWriteOptions struct {
	Ref     string `arg:"task-id" form:"required,suggest=tasks"`
	Name    string `arg:"document" form:"required,choices=requirement|plan|tasks"`
	Content string `arg:"content" form:"multiline"`
}

func TaskWriteDefaults(config.Config) TaskWriteOptions { return TaskWriteOptions{Name: "requirement"} }
func (o TaskWriteOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// WriteTaskArtifact ghi tài liệu của tác vụ; trả đường dẫn đã ghi.
func WriteTaskArtifact(_ context.Context, ws *TaskWS, o TaskWriteOptions) (string, error) {
	_, p, err := ws.WriteArtifact(o.Ref, o.Name, o.Content)
	return p, err
}

// ---- task status ----

// TaskStatusOptions: `bint task status <task-id> <trạng thái>`.
type TaskStatusOptions struct {
	Ref    string `arg:"task-id" form:"required,suggest=tasks"`
	To     string `arg:"status" form:"required,choices=created|planned|in_progress|in_review|done|blocked|interrupted|failed|cancelled"`
	Reason string `flag:"reason"`
	Force  bool   `flag:"force" form:"advanced"`
}

func TaskStatusDefaults(config.Config) TaskStatusOptions { return TaskStatusOptions{} }
func (o TaskStatusOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskStatusResult là kết quả đổi trạng thái.
type TaskStatusResult struct {
	Task     *task.Task
	Blockers []string // điều thiếu khi người --force xác nhận done
}

// SetTaskStatus chuyển trạng thái theo bảng chuyển. --force chỉ dành cho người (kiểm ở dịch vụ).
func SetTaskStatus(ctx context.Context, ws *TaskWS, o TaskStatusOptions) (TaskStatusResult, error) {
	if o.Force {
		if err := RequireHuman("bint task status --force"); err != nil {
			return TaskStatusResult{}, err
		}
	}
	t, blockers, err := ws.SetStatus(ctx, o.Ref, o.To, workflow.TransitionOptions{Reason: o.Reason, Force: o.Force})
	return TaskStatusResult{Task: t, Blockers: blockers}, err
}

// TaskTransition là một dòng của bảng chuyển trạng thái.
type TaskTransition = task.TransitionRow

// TaskTransitions trả bảng chuyển trạng thái tác vụ.
func TaskTransitions() []TaskTransition { return task.TransitionTable() }

// ---- task verify ----

// TaskVerifyOptions: `bint task verify <task-id>`.
type TaskVerifyOptions struct {
	Ref string `arg:"task-id" form:"required,suggest=tasks"`
}

func TaskVerifyDefaults(config.Config) TaskVerifyOptions { return TaskVerifyOptions{} }
func (o TaskVerifyOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskVerified là kết quả chạy kiểm chứng.
type TaskVerified struct {
	Task   *task.Task
	Result workflow.VerifyResult
}

// VerifyTask chạy lệnh kiểm chứng của phiếu (chỉ khi người đã xác nhận); ctx huỷ thì dừng lệnh.
func VerifyTask(ctx context.Context, ws *TaskWS, o TaskVerifyOptions) (TaskVerified, error) {
	t, res, err := ws.Verify(ctx, o.Ref)
	return TaskVerified{Task: t, Result: res}, err
}

// ---- task criteria ----

// TaskCriteriaOptions: `bint task criteria <task-id> <số> met|unmet`.
type TaskCriteriaOptions struct {
	Ref      string `arg:"task-id" form:"required,suggest=tasks"`
	Index    int    `arg:"number" form:"required"`
	Result   string `arg:"result" form:"required,choices=met|unmet"`
	Evidence string `flag:"evidence" form:"required,multiline"`
}

func TaskCriteriaDefaults(config.Config) TaskCriteriaOptions {
	return TaskCriteriaOptions{Result: "met"}
}

func (o TaskCriteriaOptions) Validate() error {
	if o.Index < 1 {
		return Usagef("số tiêu chí không hợp lệ %d (từ 1)", o.Index)
	}
	switch strings.ToLower(o.Result) {
	case "met", "unmet":
		return nil
	}
	return Usagef("kết quả phải là met hoặc unmet")
}

// CheckTaskCriterion ghi kết quả đối chiếu một tiêu chí.
func CheckTaskCriterion(_ context.Context, ws *TaskWS, o TaskCriteriaOptions) (*task.Task, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return ws.CheckCriterion(o.Ref, o.Index, strings.ToLower(o.Result) == "met", o.Evidence)
}

// ---- task error ----

// TaskErrorOptions: `bint task error <task-id> <nội dung>`.
type TaskErrorOptions struct {
	Ref     string `arg:"task-id" form:"required,suggest=tasks"`
	Message string `arg:"message" form:"required,multiline"`
	Step    string `flag:"step" form:"choices=b-req|b-feat|b-review"`
}

func TaskErrorDefaults(config.Config) TaskErrorOptions { return TaskErrorOptions{} }
func (o TaskErrorOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// RecordTaskError ghi một lỗi hoặc lần thử sửa thất bại.
func RecordTaskError(_ context.Context, ws *TaskWS, o TaskErrorOptions) (*task.Task, error) {
	return ws.RecordError(o.Ref, o.Step, o.Message)
}

// ---- task review ----

// TaskReviewOptions: `bint task review <task-id> [tệp | -]`. JSON là nội dung vòng review (CLI điền từ tệp/stdin).
type TaskReviewOptions struct {
	Ref  string `arg:"task-id" form:"required,suggest=tasks"`
	JSON string `arg:"json" form:"required,multiline"`
}

func TaskReviewDefaults(config.Config) TaskReviewOptions { return TaskReviewOptions{} }
func (o TaskReviewOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskReviewTemplate là mẫu JSON của một vòng review (TUI mở trong $EDITOR).
const TaskReviewTemplate = `{
  "summary": "",
  "findings": [
    {"kind": "bug", "severity": "medium", "title": "", "path": "", "lines": "", "evidence": "", "body": "", "criterion": 0}
  ]
}
`

// TaskReviewed là kết quả ghi vòng review.
type TaskReviewed struct {
	Task  *task.Task
	Round task.ReviewRound
}

// SubmitTaskReview ghi một vòng review.
func SubmitTaskReview(_ context.Context, ws *TaskWS, o TaskReviewOptions) (TaskReviewed, error) {
	t, r, err := ws.SubmitReview(o.Ref, []byte(o.JSON))
	return TaskReviewed{Task: t, Round: r}, err
}

// ---- task lint ----

// TaskLintOptions: `bint task lint <task-id>`.
type TaskLintOptions struct {
	Ref    string `arg:"task-id" form:"required,suggest=tasks"`
	Strict bool   `flag:"strict"`
}

func TaskLintDefaults(config.Config) TaskLintOptions { return TaskLintOptions{} }
func (o TaskLintOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// TaskLinted là kết quả kiểm tài liệu.
type TaskLinted struct {
	Task            *task.Task
	ContractVersion int
	Report          task.ArtifactReport
}

// LintTask kiểm ba tài liệu của b-req (không ghi gì). Task theo hợp đồng v1 luôn kiểm nghiêm ngặt.
func LintTask(_ context.Context, ws *TaskWS, o TaskLintOptions) (TaskLinted, error) {
	t, err := ws.Tasks.Load(o.Ref)
	if err != nil {
		return TaskLinted{}, err
	}
	v1 := t.State.PlanningContractVersion >= task.PlanningContractV1
	return TaskLinted{Task: t, ContractVersion: t.State.PlanningContractVersion, Report: task.LintPlanningTask(t, o.Strict || v1)}, nil
}

// ---- task approve ----

// TaskApproveOptions: `bint task approve <task-id>`.
type TaskApproveOptions struct {
	Ref   string   `arg:"task-id" form:"required,suggest=tasks"`
	Paths []string `flag:"path" form:"advanced,suggest=files"`
	// SetupSHA: băm (tiền tố ≥ 12 ký tự hex) của lệnh setup worktree người đã xem; thiếu thì lệnh setup không được xác nhận.
	SetupSHA string `flag:"setup-sha" form:"advanced"`
}

func TaskApproveDefaults(config.Config) TaskApproveOptions { return TaskApproveOptions{} }
func (o TaskApproveOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// ApprovePlan là những gì `task approve` SẼ xác nhận/duyệt, tính trước khi ghi (chỉ đọc).
type ApprovePlan struct {
	Task *task.Task
	// VerifyCommand là lệnh kiểm chứng sẽ được xác nhận ("" nếu không có lệnh hoặc đã xác nhận).
	VerifyCommand string
	// SetupCommand là lệnh workflow.worktree.setup_command sẽ được xác nhận ("" nếu không cần hoặc đã xác nhận);
	// SetupSHA là băm của đúng lệnh đó, gửi kèm lúc ghi để Approve từ chối nếu cấu hình đổi sau khi người xem.
	SetupCommand string
	SetupSHA     string
	// Paths là các đường dẫn sẽ được duyệt: đang chờ (PendingPaths), thay đổi chưa duyệt tính từ diff hiện tại,
	// và đường dẫn --path.
	Paths []string
	// Resumable: tác vụ đang tạm dừng nên sau khi duyệt có thể `run resume`.
	Resumable bool
}

// Empty: không có gì để duyệt.
func (p ApprovePlan) Empty() bool {
	return p.VerifyCommand == "" && p.SetupCommand == "" && len(p.Paths) == 0
}

// PlanTaskApprove tính điều sẽ được duyệt mà không ghi gì (không đổi state.json). Cùng quy tắc với Approve.
func PlanTaskApprove(ctx context.Context, ws *TaskWS, o TaskApproveOptions) (ApprovePlan, error) {
	var p ApprovePlan
	t, err := ws.Tasks.Load(o.Ref)
	if err != nil {
		return p, err
	}
	p.Task = t
	if cmd := strings.TrimSpace(t.Ticket.Verify.Command); cmd != "" && !ws.VerifyConfirmed(t) {
		p.VerifyCommand = cmd
	}
	if ss := ws.SetupStatus(t); ss.Pending() {
		p.SetupCommand, p.SetupSHA = ss.Command, ss.SHA256
	}
	add := func(xs ...string) {
		for _, x := range xs {
			if x = strings.TrimSpace(x); x != "" && !slices.Contains(p.Paths, x) {
				p.Paths = append(p.Paths, x)
			}
		}
	}
	add(t.State.PendingPaths...)
	add(o.Paths...)
	if cur, err := ws.UnapprovedChanges(ctx, t); err == nil {
		add(cur...)
	}
	p.Resumable = task.Paused(t.State.Status)
	return p, nil
}

// ApproveTask: người xác nhận lệnh kiểm chứng và duyệt thay đổi đang chờ. Chỉ dành cho người (RequireHuman).
func ApproveTask(_ context.Context, ws *TaskWS, o TaskApproveOptions) (*task.Task, workflow.Approval, error) {
	if err := RequireHuman("bint task approve"); err != nil {
		return nil, workflow.Approval{}, err
	}
	return ws.ApproveWith(o.Ref, workflow.ApproveInput{Paths: o.Paths, SetupSHA: o.SetupSHA})
}

// ---- task land ----

// TaskLandOptions: `bint task land <task-id> [--3way] [--keep]`.
type TaskLandOptions struct {
	Ref      string `arg:"task-id" form:"required,suggest=tasks"`
	ThreeWay bool   `flag:"3way"`
	Keep     bool   `flag:"keep"`
}

func TaskLandDefaults(config.Config) TaskLandOptions { return TaskLandOptions{} }
func (o TaskLandOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// LandTask đưa thay đổi của tác vụ chạy trong worktree riêng về cây gốc (xem workflow.Env.Land). --3way chỉ
// dành cho người (RequireHuman): nó ghi dấu xung đột vào tệp ở cây gốc.
func LandTask(ctx context.Context, ws *TaskWS, o TaskLandOptions) (*workflow.LandResult, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.ThreeWay {
		if err := RequireHuman("bint task land --3way"); err != nil {
			return nil, err
		}
	}
	return ws.Land(ctx, o.Ref, workflow.LandOptions{ThreeWay: o.ThreeWay, Keep: o.Keep})
}

// ---- task cancel ----

// TaskCancelOptions: `bint task cancel <task-id> [--reason …] [--discard]`.
type TaskCancelOptions struct {
	Ref     string `arg:"task-id" form:"required,suggest=tasks"`
	Reason  string `flag:"reason"`
	Discard bool   `flag:"discard"`
}

func TaskCancelDefaults(config.Config) TaskCancelOptions { return TaskCancelOptions{} }
func (o TaskCancelOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// CancelTask huỷ tác vụ (cùng đường với `task status <id> cancelled`). Tác vụ worktree: gỡ worktree, nhánh, ref
// base, tệp khoá; còn thay đổi chưa đưa về thì cần --discard (chỉ người, RequireHuman).
func CancelTask(ctx context.Context, ws *TaskWS, o TaskCancelOptions) (*task.Task, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.Discard {
		if err := RequireHuman("bint task cancel --discard"); err != nil {
			return nil, err
		}
	}
	t, _, err := ws.SetStatus(ctx, o.Ref, task.StatusCancelled, workflow.TransitionOptions{Reason: o.Reason, Discard: o.Discard})
	return t, err
}

// ---- run status ----

// TaskRunsOptions: `bint run status <task-id | run-id>`. NoIngest (không phải cờ): chế độ chỉ đọc của TUI không
// nạp spool vào cơ sở dữ liệu.
type TaskRunsOptions struct {
	Ref      string `arg:"id" form:"required,suggest=tasks"`
	NoIngest bool
}

func TaskRunsDefaults(config.Config) TaskRunsOptions { return TaskRunsOptions{} }
func (o TaskRunsOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ hoặc lượt chạy")
	}
	return nil
}

// TaskRunRow là một lượt chạy gắn với tác vụ.
type TaskRunRow struct {
	RunID    string   `json:"run_id"`
	Workflow string   `json:"workflow"`
	Parent   string   `json:"parent_run_id,omitempty"`
	Status   string   `json:"status"`
	Started  string   `json:"started_at"`
	Duration string   `json:"duration"`
	CostUSD  *float64 `json:"cost_usd"`
	Verify   string   `json:"verification_status,omitempty"`
	Reason   string   `json:"end_reason,omitempty"`
}

// TaskRunsResult là tác vụ và các lượt chạy gắn với nó.
type TaskRunsResult struct {
	Task *task.Task
	Runs []TaskRunRow
}

// TaskRuns liệt kê lượt chạy của tác vụ (hoặc của tác vụ chứa lượt run-id).
func TaskRuns(ctx context.Context, ws *TaskWS, o TaskRunsOptions) (TaskRunsResult, error) {
	var res TaskRunsResult
	s, err := ws.K.OpenStore(ctx)
	if err != nil {
		return res, err
	}
	defer s.Close()
	if !o.NoIngest {
		if _, err := ws.K.Ingest(ctx, s, time.Now()); err != nil {
			return res, err
		}
	}
	t, terr := ws.Tasks.Load(o.Ref)
	if terr != nil {
		r, err := runlog.GetRun(ctx, s.DB, o.Ref)
		if err != nil {
			return res, fmt.Errorf("không tìm thấy tác vụ hay lượt chạy %s", o.Ref)
		}
		m, err := runlog.GetRunMeta(ctx, s.DB, r.RunID)
		if err != nil || !m.TaskID.Valid {
			return res, fmt.Errorf("lượt %s không gắn với tác vụ nào (bint runs show %s)", r.RunID, r.RunID)
		}
		if t, err = ws.Tasks.Load(m.TaskID.String); err != nil {
			return res, err
		}
	}
	if !o.NoIngest {
		t = ws.Reconcile(t)
	}
	res.Task = t
	runs, err := runlog.RunsForTask(ctx, s.DB, t.State.ID)
	if err != nil {
		return res, err
	}
	res.Runs = []TaskRunRow{}
	for _, r := range runs {
		m, _ := runlog.GetRunMeta(ctx, s.DB, r.RunID)
		dur := "-"
		if r.DurationMS.Valid {
			dur = (time.Duration(r.DurationMS.Int64) * time.Millisecond).Round(time.Second).String()
		}
		row := TaskRunRow{RunID: r.RunID, Workflow: r.Workflow.String, Parent: m.ParentRunID.String, Status: r.Status,
			Started: r.StartedAt.String, Duration: dur, Verify: m.VerificationStatus.String, Reason: r.EndReason.String}
		if m.CostUSD.Valid {
			c := m.CostUSD.Float64
			row.CostUSD = &c
		}
		res.Runs = append(res.Runs, row)
	}
	return res, nil
}

// harnessSelection dựng lựa chọn harness từ cờ (dùng chung run và run resume); lỗi là UsageError.
func harnessSelection(harnessName string, roles []string, agentCommand string) (harness.Selection, error) {
	sel := harness.Selection{All: harnessName}
	if harnessName != "" {
		if _, ok := harness.Get(harnessName); !ok {
			return sel, Usagef("--harness: harness không hỗ trợ %q (%s)", harnessName, strings.Join(harness.Names(), ", "))
		}
	}
	var err error
	if sel.Roles, err = harness.ParseRoleOverrides(roles); err != nil {
		return sel, UsageError{err}
	}
	if agentCommand != "" && (harnessName != "" || len(roles) > 0) {
		return sel, Usagef("--agent-command không đi cùng --harness/--role")
	}
	return sel, nil
}
