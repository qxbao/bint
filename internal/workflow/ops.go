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
	"path"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/gitrepo"
	"bint/internal/knowledge"
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
	// Depth: ""/auto = tự chọn; standard|deep = người dùng chỉ định (hàm ý có b-req; chỉ b-do/b-req).
	Depth string
	// Isolation: ""/auto = theo workflow.isolation; shared|worktree = người dùng chỉ định (--shared/--worktree).
	Isolation string
	// Driver: ""/auto = theo workflow.driver; process|session = người dùng chỉ định (--driver).
	Driver string
	// DriverFlagOnly: bỏ qua workflow.driver, chỉ nhận cờ tường minh (`bint run` tự lập phiếu: bản thân nó là driver
	// process nên không lấy khoá cấu hình của driver session).
	DriverFlagOnly bool
	// FromFindings: ID phát hiện review mà phiếu được lập để sửa (`task new --from-findings`); phạm vi suy từ
	// đường dẫn của chúng và Request có thể rỗng.
	FromFindings []string
	// Parent: phiếu cha (kế hoạch) để kế thừa phạm vi/bối cảnh; DependsOn: phiếu phải done trước (ID hoặc tiền tố).
	Parent    string
	DependsOn []string
}

// NewTask lập phiếu tác vụ từ mô tả tự do (bước Classify).
func (e *Env) NewTask(ctx context.Context, o NewTaskOptions) (*task.Task, task.Assessment, error) {
	now := e.now()
	confirmed := ""
	if tc := e.Cfg.Eval.TestCommand; tc != nil {
		confirmed = *tc
	}
	var parentTask *task.Task
	var depIDs []string
	if o.Parent != "" {
		ids, err := e.resolveTaskRefs("--parent", []string{o.Parent})
		if err != nil {
			return nil, task.Assessment{}, err
		}
		if parentTask, err = e.Tasks.Load(ids[0]); err != nil {
			return nil, task.Assessment{}, dependencyInputf("--parent %q: %v", o.Parent, err)
		}
	}
	if len(o.DependsOn) > 0 {
		var err error
		if depIDs, err = e.resolveTaskRefs("--depends-on", o.DependsOn); err != nil {
			return nil, task.Assessment{}, err
		}
	}
	var findViews []FindingView
	if len(o.FromFindings) > 0 {
		var paths []string
		var err error
		if findViews, paths, err = e.fromFindings(o.FromFindings); err != nil {
			return nil, task.Assessment{}, err
		}
		o.Request = findingsRequest(o.Request, findViews)
		o.Edit.ClearScope = true
		o.Edit.Allow = appendUnique(append([]string(nil), paths...), o.Edit.Allow...)
	}
	t := task.FromRequest(e.Root, o.Request, confirmed)
	if err := o.Edit.apply(&t); err != nil {
		return nil, task.Assessment{}, err
	}
	var inherited []string
	if parentTask != nil {
		// Trước Classify/Assess: references và đường dẫn bảo vệ kế thừa phải có hiệu lực ở cả hai.
		inherited = task.Inherit(&t, parentTask.Ticket, o.Edit.ClearScope)
	}
	if o.Workflow == "" {
		o.Workflow = "b-do" // workflow mặc định khi không chỉ định (lập phiếu cho flow nhúng b-do)
	}
	depth, err := task.NormalizeDepth(o.Depth)
	if err != nil {
		return nil, task.Assessment{}, err
	}
	fd, err := e.resolveFlow(o.Workflow)
	if err != nil {
		return nil, task.Assessment{}, err
	}
	planning := fd.HasKind(flow.KindPlan)
	if depth != "" {
		if !planning {
			return nil, task.Assessment{}, fmt.Errorf("--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)", o.Workflow)
		}
		if o.Plan != nil && !*o.Plan {
			return nil, task.Assessment{}, errors.New("--depth standard|deep hàm ý có b-req: không đi cùng --no-plan")
		}
	}
	cls := task.Classify(o.Request, t.References...)
	if o.Plan != nil {
		cls = task.Classification{Plan: *o.Plan, Reason: e.chooser() + " chỉ định"}
	}
	if !fd.IsBuiltin() && planning && !fd.PlanOptional() {
		// Bước plan của flow tuỳ biến không khai báo optional: luôn chạy, phân loại không bỏ được nó.
		if o.Plan != nil && !*o.Plan {
			return nil, task.Assessment{}, fmt.Errorf("flow %s có bước plan bắt buộc: không đi cùng --no-plan", fd.Name)
		}
		cls = task.Classification{Plan: true, Reason: "flow " + fd.Name + " có bước plan bắt buộc"}
	}
	// Độ sâu là quyết định riêng với rủi ro quyền ghi: chỉ dùng Assess để biết có chạm đường dẫn bảo vệ.
	// Chưa có Task nên gọi thẳng task.Assess trên phiếu.
	assess := task.Assess(t, e.Cfg.Limits.ProtectedPaths, e.commandConfirmed(t.Verify.Command))
	switch {
	case depth != "":
		cls.Plan = true
		if o.Plan == nil {
			cls.Reason = e.chooser() + " chỉ định độ sâu " + depth
		}
	case planning && !cls.Plan && assess.Risk == "high" && (o.Plan == nil || !e.Human):
		cls = protectedPlan(assess.Protected, o.Plan != nil)
	}
	if planning && cls.Plan {
		if depth != "" {
			cls.Depth, cls.DepthReason = depth, task.DepthReasonUser
		} else {
			cls.Depth, cls.DepthReason = task.ChoosePlanningDepth(o.Request, assess)
		}
	}
	iso, err := e.chooseIsolation(o.Isolation)
	if err != nil {
		return nil, task.Assessment{}, err
	}
	cls.Isolation, cls.IsolationReason, cls.IsolationTasks = iso.Mode, iso.Reason, iso.Tasks
	drv, err := e.chooseDriver(o.Driver, o.DriverFlagOnly)
	if err != nil {
		return nil, task.Assessment{}, err
	}
	cls.Driver, cls.DriverReason = drv.Mode, drv.Reason
	st := task.State{Workflow: o.Workflow, Base: gitrepo.HeadCommit(ctx, e.Root), Branch: gitrepo.Branch(ctx, e.Root),
		Worktree: e.Root, Classification: &cls}
	if planning && (!fd.PlanOptional() || cls.Plan) {
		st.PlanningContractVersion = task.PlanningContractV1
	}
	if !fd.IsBuiltin() {
		st.FlowSHA = flow.Hash(fd) // flow đổi giữa chừng thì tác vụ bị chặn (flowGate)
	}
	for _, v := range findViews {
		st.FromFindings = append(st.FromFindings, v.Finding.ID)
	}
	if parentTask != nil {
		st.Parent, st.Inherited = parentTask.State.ID, inherited
	}
	st.DependsOn = depIDs
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

// chooser là bên ghi đè Classify: agent (orchestrator) được nâng/hạ b-req theo phán đoán của
// mình, trừ việc bỏ b-req trên tác vụ chạm đường dẫn bảo vệ (protectedPlan).
func (e *Env) chooser() string {
	if e.Human {
		return "người dùng"
	}
	return "orchestrator"
}

// protectedPlan giữ b-req cho tác vụ chạm đường dẫn bảo vệ. refused: agent đã xin --no-plan;
// chỉ người bỏ được b-req trong trường hợp này.
func protectedPlan(protected []string, refused bool) task.Classification {
	reason := "chạm đường dẫn được bảo vệ (" + strings.Join(protected, ", ") + "): cần b-req"
	if refused {
		reason += "; agent không bỏ được b-req ở đây, người dùng tự chạy với --no-plan nếu muốn"
	}
	return task.Classification{Plan: true, Reason: reason}
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

// ensurePlanningContract nâng task lên hợp đồng artifact v1 (không bao giờ hạ). Dùng chung
// cho mọi chỗ Plan chuyển false→true hoặc b-req được chọn chạy.
func ensurePlanningContract(t *task.Task) {
	if t.State.PlanningContractVersion < task.PlanningContractV1 {
		t.State.PlanningContractVersion = task.PlanningContractV1
	}
}

// PlanningError: artifact b-req chưa đạt hợp đồng v1.
type PlanningError struct {
	TaskID string
	Report task.ArtifactReport
}

func (e *PlanningError) Error() string {
	var parts []string
	if len(e.Report.Errors) > 0 {
		parts = append(parts, "artifact b-req chưa đạt hợp đồng v1:\n  - "+strings.Join(issueLines(e.Report.Errors), "\n  - "))
	}
	if len(e.Report.OpenQuestions) > 0 {
		parts = append(parts, "còn câu hỏi mở trong requirement.md (ghi `— Trả lời:` rồi tiếp tục):\n  - "+
			strings.Join(questionLines(e.Report.OpenQuestions), "\n  - "))
	}
	return strings.Join(parts, "\n") + "\nKiểm lại bằng `bint task lint " + e.TaskID + "`"
}

func questionLines(is []task.ArtifactIssue) []string {
	out := make([]string, 0, len(is))
	for _, i := range is {
		out = append(out, i.Detail)
	}
	return out
}

func issueLines(is []task.ArtifactIssue) []string {
	out := make([]string, 0, len(is))
	for _, i := range is {
		out = append(out, i.String())
	}
	return out
}

// ErrAgent: thao tác chỉ dành cho người.
var ErrAgent = errors.New("thao tác này chỉ dành cho người dùng, không chạy được từ trong phiên agent")

// SetTicket sửa phiếu. Agent không được nâng autonomy lên auto (mức tự chủ do người quyết).
func (e *Env) SetTicket(ref string, ed TicketEdit) (*task.Task, task.Assessment, error) {
	t, a, _, err := e.SetTicketSynced(ref, ed)
	return t, a, err
}

// SetTicketSynced như SetTicket và trả thêm các ghi chú đồng bộ requirement.md/tasks.md khi danh sách tiêu chí đổi.
func (e *Env) SetTicketSynced(ref string, ed TicketEdit) (*task.Task, task.Assessment, []string, error) {
	if !e.Human && ed.Autonomy != nil && *ed.Autonomy == task.AutonomyAuto {
		return nil, task.Assessment{}, nil, fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", ErrAgent)
	}
	var sync criteriaSync
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if task.Terminal(t.State.Status) {
			return fmt.Errorf("tác vụ đã %s", t.State.Status)
		}
		before := slices.Clone(t.Ticket.Verify.Criteria)
		if err := ed.apply(&t.Ticket); err != nil {
			return err
		}
		return e.syncCriteria(t, before, &sync)
	})
	if err != nil {
		return nil, task.Assessment{}, nil, err
	}
	e.emitSynced(t, sync)
	return t, e.Assess(t), sync.notes, nil
}

// criteriaSync gom kết quả đồng bộ tiêu chí trong một lần Update để phát sự kiện sau khi phiếu đã được lưu.
type criteriaSync struct {
	written []string
	notes   []string
}

// syncCriteria (gọi trong callback Update đang giữ khoá phiếu): nếu danh sách tiêu chí đổi so với before thì
// viết lại các dòng AC trong requirement.md và bổ sung TSK trong tasks.md (task.SyncCriteriaArtifacts).
func (e *Env) syncCriteria(t *task.Task, before []string, out *criteriaSync) error {
	if slices.Equal(before, t.Ticket.Verify.Criteria) {
		return nil
	}
	written, notes, err := task.SyncCriteriaArtifacts(t)
	if err != nil {
		return fmt.Errorf("đồng bộ tiêu chí sang requirement.md/tasks.md: %w", err)
	}
	if t.State.Artifacts == nil && len(written) > 0 {
		t.State.Artifacts = map[string]string{}
	}
	for _, n := range written {
		t.State.Artifacts[n] = e.at()
	}
	out.written, out.notes = written, notes
	return nil
}

func (e *Env) emitSynced(t *task.Task, s criteriaSync) {
	for _, n := range s.written {
		e.emit(t, spool.TaskArtifact, map[string]any{"name": n, "path": t.ArtifactPath(n)})
	}
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
	// Discard: huỷ tác vụ worktree dù còn thay đổi chưa đưa về (chỉ người).
	Discard bool
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
	if o.Discard && !e.Human {
		return nil, nil, fmt.Errorf("%w: --discard", ErrAgent)
	}
	var blockers []string
	var doneIn task.DoneInput
	var doneErr error
	var pg planGate
	if to == task.StatusCancelled {
		// Tác vụ worktree còn thay đổi chưa đưa về: huỷ sẽ mất chúng, cần --discard (chỉ người).
		if t0, err := e.Tasks.Load(ref); err == nil && t0.State.Origin != "" && !task.Terminal(t0.State.Status) {
			if err := e.checkCancelWorktree(ctx, t0, o.Discard); err != nil {
				return nil, nil, err
			}
		}
	}
	if to == task.StatusDone {
		// Fingerprint tính ở đúng Root của tác vụ (State.Worktree), nên nạp tác vụ trước khi vào khoá.
		t0, err := e.Tasks.Load(ref)
		if err != nil {
			return nil, nil, err
		}
		// Phiếu chỉ lập kế hoạch (ở worktree: diff chỉ gồm mục tri thức candidate) không có mã để chụp/so.
		if pg = e.planGateFor(ctx, t0); !pg.PlanOnly {
			doneIn, doneErr = e.DoneInputDetailed(ctx, t0)
		}
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if !o.Orchestrator && t.OwnedByOther() {
			return fmt.Errorf("tác vụ đang được `bint run` điều phối (lượt %s): trạng thái do Bint Core chuyển", t.State.Runner.RunID)
		}
		// Task v1: chuyển tay sang planned phải qua lint; planned → in_progress còn bị chặn khi có câu hỏi
		// mở (created → planned thì không: planned là nơi ghi câu trả lời rồi resume). Runner tự lint ở
		// afterStep/preflight.
		if !o.Orchestrator && t.State.PlanningContractVersion >= task.PlanningContractV1 {
			toPlanned := to == task.StatusPlanned && t.State.Status != task.StatusPlanned
			toImpl := to == task.StatusInProgress && (t.State.Status == task.StatusPlanned ||
				(task.Paused(t.State.Status) && (t.State.ResumeFrom == task.StatusPlanned || t.State.ResumeFrom == task.StatusInProgress)))
			if toPlanned || toImpl {
				rep := task.LintPlanningTask(t, true)
				if rep.Blocking() || (toImpl && len(rep.OpenQuestions) > 0) {
					if !toImpl {
						rep.OpenQuestions = nil
					}
					return &PlanningError{TaskID: t.State.ID, Report: rep}
				}
			}
		}
		reason := o.Reason
		if h := t.State.HeldStep; h != nil && !o.Orchestrator {
			// Người (hoặc agent) tự chuyển trạng thái thay cho `bint run resume`: kết quả bước chỉ đọc đang giữ có thể đã
			// cũ so với cây họ vừa sửa; lượt chạy sau không được áp lại cổng của bước đó. Có dấu vết trong lịch sử.
			t.State.HeldStep = nil
			reason = strings.TrimSpace(reason + " [bỏ kết quả bước giữ " + h.Step + "]")
		}
		if to == task.StatusDone {
			blockers = e.doneBlockersFor(ctx, t, doneIn, doneErr, pg)
			if len(blockers) > 0 && !o.Force {
				return &NotDoneError{Blockers: blockers}
			}
			// --force không bỏ qua "chưa đưa về": done rồi thì Land từ chối và thay đổi kẹt trong worktree
			// (doctor lại gợi ý xoá nó). Muốn bỏ thay đổi thì `cancel --discard`.
			if t.State.NeedsLand() {
				return &NotDoneError{Blockers: append(blockers, "cờ ép không bỏ qua việc đưa về cây gốc: `bint task land "+
					t.State.ID+"`, hoặc người huỷ bằng `bint task cancel "+t.State.ID+"` để bỏ thay đổi")}
			}
			if len(blockers) > 0 { // chỉ tới đây khi --force: ghi vào lịch sử người đã ép qua điều chặn nào (kể cả ClosePlan)
				reason = strings.TrimSpace(reason + " [người xác nhận dù thiếu bằng chứng: " + strings.Join(blockers, "; ") + "]")
			}
			if e.planOnlyTask(t) {
				if !pg.PlanOnly {
					// Có thay đổi ngoài mục tri thức candidate: ClosePlan không áp dụng, bảng chuyển không có planned → done.
					// doneBlockersFor đã thêm điều chặn nêu lối ra; --force không mở được lối này.
					return &NotDoneError{Blockers: blockers}
				}
				// Bảng chuyển giữ nguyên (planned → done không có): chỉ phiếu chỉ lập kế hoạch mới đóng được bằng ClosePlan.
				return t.State.ClosePlan(e.actor(), reason, e.at())
			}
		}
		return t.State.Apply(to, e.actor(), reason, e.at())
	})
	if err != nil {
		return t, blockers, err
	}
	if task.Terminal(to) {
		// Tác vụ kết thúc: ref giữ cây gốc (base, base-tree) không còn cần (nó giữ cả tệp chưa theo dõi, không bị gc dọn).
		if root, rerr := e.RootFor(t); rerr == nil {
			deleteTaskRefs(ctx, root, t.State.ID)
		}
	}
	if to == task.StatusCancelled {
		// Bước đang chạy bằng subagent: gỡ bước dở và hợp đồng thuê trước, nếu không hợp đồng của chính tác vụ chặn việc dọn.
		e.discardPendingForCancel(t)
		e.removeWorktreeAfterCancel(ctx, t)
	}
	e.emit(t, spool.TaskStatus, map[string]any{"status": to, "reason": o.Reason, "forced": o.Force && len(blockers) > 0})
	return t, blockers, nil
}

// UnapprovedChanges trả về tệp đã sửa của tác vụ cần người duyệt nhưng chưa nằm trong
// ProtectedApproval. Tệp của tác vụ là Touched nếu có theo dõi, ngược lại toàn bộ thay đổi so với
// base (phiếu cũ); tính ở Root của tác vụ.
func (e *Env) UnapprovedChanges(ctx context.Context, t *task.Task) ([]string, error) {
	var files []string
	if t.State.Landed != nil {
		// Đã đưa về (worktree có thể đã gỡ): tập tệp là bản lưu lúc đưa về; cổng done đã kiểm chúng ở worktree.
		files = t.State.Landed.Files
	} else {
		root, err := e.RootFor(t)
		if err != nil {
			return nil, err
		}
		if files, err = ScopeFiles(ctx, root, e.Tasks, t, e.Cfg.Limits.ProtectedPaths); err != nil {
			return nil, err
		}
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

// DoneInputFor dựng DoneInput của tác vụ t ở Root của nó, rẻ (chỉ fingerprint): dùng khi chỉ hiển thị điều
// chặn (app.ShowTask, TUI nạp chi tiết mỗi lần chọn). Lỗi (thư mục làm việc không còn) được trả kèm
// DoneInput còn dùng được.
func (e *Env) DoneInputFor(ctx context.Context, t *task.Task) (task.DoneInput, error) {
	in := task.DoneInput{VerifyConfirmed: e.VerifyConfirmed(t)}
	if l := t.State.Landed; l != nil {
		in.Worktree = l.Fingerprint // worktree có thể đã gỡ: dùng bản lưu lúc đưa về
		in.LandConflicts = landConflictsLeft(t)
		return in, nil
	}
	root, err := e.RootFor(t)
	if err != nil {
		return in, err
	}
	in.Worktree = task.WorktreeFingerprint(ctx, root)
	return in, nil
}

// DoneInputDetailed như DoneInputFor, thêm: khi mã đã đổi kể từ lần kiểm chứng cuối, có ảnh chụp lúc kiểm
// chứng và tác vụ theo dõi tập tệp riêng thì liệt kê các tệp đổi mà không thuộc Touched (việc của tác vụ
// khác) để thông báo chặn done chỉ rõ. Đắt hơn (chụp cây + diff) nên chỉ dùng ở SetStatus(done).
func (e *Env) DoneInputDetailed(ctx context.Context, t *task.Task) (task.DoneInput, error) {
	in, err := e.DoneInputFor(ctx, t)
	if err != nil || t.State.Landed != nil {
		return in, err
	}
	root, _ := e.RootFor(t)
	v := t.State.LastVerification()
	if v == nil || in.Worktree == "" || v.Worktree == in.Worktree || v.Tree == "" || !t.State.TracksTouched() {
		return in, nil
	}
	cur, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		return in, nil
	}
	changed, err := DiffPaths(ctx, root, v.Tree, cur)
	if err != nil {
		return in, nil
	}
	for _, f := range changed {
		if slices.Contains(t.State.Touched, f) {
			in.TouchedSinceVerify = append(in.TouchedSinceVerify, f)
		} else {
			in.OutsideSinceVerify = append(in.OutsideSinceVerify, f)
		}
	}
	return in, nil
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
	if e.commandConfirmed(cmd) {
		return true
	}
	a := t.State.VerifyApproval
	return a != nil && a.Command == cmd
}

// SetupStatus là trạng thái xác nhận workflow.worktree.setup_command của một tác vụ.
type SetupStatus struct {
	Command string // lệnh setup đang cấu hình (đã trim)
	SHA256  string // băm của Command
	// Required: tác vụ còn cần tạo worktree bằng lệnh này (lệnh khác rỗng, chế độ worktree, chưa tạo xong, chưa kết thúc).
	Required bool
	// Confirmed: người đã xác nhận đúng lệnh này (băm trùng SetupApproval).
	Confirmed bool
}

// Pending: lệnh setup cần người xác nhận mà chưa có.
func (s SetupStatus) Pending() bool { return s.Required && !s.Confirmed }

// SetupStatus tính trạng thái xác nhận lệnh setup của tác vụ t theo cấu hình hiện tại.
func (e *Env) SetupStatus(t *task.Task) SetupStatus {
	cmd := strings.TrimSpace(e.Cfg.Workflow.Worktree.SetupCommand)
	st := SetupStatus{Command: cmd}
	if cmd == "" {
		return st
	}
	st.SHA256 = task.CommandSHA256(cmd)
	s := &t.State
	st.Required = s.Isolation() == task.IsolationWorktree && s.Origin == "" && s.Landed == nil && !task.Terminal(s.Status)
	st.Confirmed = s.SetupApproval != nil && s.SetupApproval.SHA256 == st.SHA256
	return st
}

// commandConfirmed: lệnh trùng eval.test_command đã xác nhận trong cấu hình.
func (e *Env) commandConfirmed(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	tc := e.Cfg.Eval.TestCommand
	return cmd != "" && tc != nil && strings.TrimSpace(*tc) == cmd
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
	return e.VerifyWith(ctx, ref, "")
}

// VerifyWith như Verify, ghi thêm lý do chạy (Verification.Reason, payload task.verified) khi Bint tự kiểm chứng lại.
func (e *Env) VerifyWith(ctx context.Context, ref, reason string) (*task.Task, VerifyResult, error) {
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
	root, err := e.RootFor(t)
	if err != nil {
		return t, res, err
	}
	confirmed := e.VerifyConfirmed(t)
	if !confirmed && e.Cfg.Security.RequireApprovalForCommands {
		res.Skipped = "lệnh kiểm chứng chưa được người xác nhận; Bint không chạy lệnh do agent đề xuất (bint task approve " + t.State.ID + ")"
		return t, res, nil
	}
	n := len(t.State.Verifications) + 1
	log := t.LogPath(fmt.Sprintf("verify-%d.log", n))
	timeout := time.Duration(e.Cfg.Workflow.VerifyTimeoutMinutes) * time.Minute
	r := eval.RunShell(ctx, root, cmd, timeout, log, nil)
	if r.Err != nil && ctx.Err() != nil {
		return t, res, ctx.Err()
	}
	v := task.Verification{At: e.at(), Command: cmd, Confirmed: confirmed, ExitCode: r.ExitCode, Passed: r.OK(),
		TimedOut: r.TimedOut, Log: log, Worktree: task.WorktreeFingerprint(ctx, root), By: e.actor(), RunID: e.RunID, Reason: reason}
	// Ảnh chụp sau lệnh kiểm chứng: để thông báo done liệt kê tệp đổi ngoài tác vụ. Lỗi thì để rỗng.
	if t.State.TracksTouched() {
		v.Tree, _ = WorktreeSnapshot(ctx, root)
	}
	t, err = e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
		t.State.Verifications = append(t.State.Verifications, v)
		return nil
	})
	if err != nil {
		return t, res, err
	}
	res.Verification = v
	payload := map[string]any{"command": cmd, "passed": v.Passed, "confirmed": confirmed,
		"exit_code": v.ExitCode, "timed_out": v.TimedOut}
	if reason != "" {
		payload["reason"] = reason
	}
	e.emit(t, spool.TaskVerified, payload)
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
	SetupCommand  string
	Paths         []string
	// SetupWaiting: lệnh setup còn chờ xác nhận vì người chưa đưa băm của lệnh đã xem (ApproveInput.SetupSHA rỗng);
	// SetupWaitingSHA là băm đầy đủ của lệnh đó để in lệnh xác nhận.
	SetupWaiting    string
	SetupWaitingSHA string
}

// ApproveInput là điều người duyệt. SetupSHA là băm sha256 (hoặc tiền tố từ MinSetupSHAPrefix ký tự hex) của lệnh setup
// mà người đã XEM; Approve chỉ ghi SetupApproval khi băm của lệnh trong cấu hình lúc ghi trùng nó, để agent đổi
// .bint/config.yml giữa lúc người xem và lúc ghi không làm lệnh mới được duyệt (review .1 của tác vụ 01M785XQ…).
type ApproveInput struct {
	Paths    []string
	SetupSHA string
}

// MinSetupSHAPrefix là độ dài tối thiểu của tiền tố băm lệnh setup khi xác nhận.
const MinSetupSHAPrefix = 12

// ErrSetupChanged: băm người đưa không khớp lệnh setup hiện tại trong cấu hình.
var ErrSetupChanged = errors.New("lệnh setup worktree đã đổi so với lệnh bạn đã xem")

// setupSHAMatches: seen là tiền tố (≥ MinSetupSHAPrefix ký tự, không phân biệt hoa thường) của băm cur.
func setupSHAMatches(seen, cur string) bool {
	seen = strings.ToLower(strings.TrimSpace(seen))
	return len(seen) >= MinSetupSHAPrefix && cur != "" && strings.HasPrefix(cur, seen)
}

// Approve duyệt mà không kèm băm lệnh setup đã xem: lệnh setup đang chờ KHÔNG được xác nhận (báo ở Approval.SetupWaiting).
func (e *Env) Approve(ref string, requestedPaths ...string) (*task.Task, Approval, error) {
	return e.ApproveWith(ref, ApproveInput{Paths: requestedPaths})
}

// ApproveWith: người xác nhận lệnh kiểm chứng hiện tại, lệnh setup worktree (workflow.worktree.setup_command, chỉ khi
// in.SetupSHA khớp lệnh hiện tại) và duyệt các thay đổi đang chờ (đường dẫn bảo vệ/deny). Không đổi trạng thái;
// `bint run resume` tiếp tục.
func (e *Env) ApproveWith(ref string, in ApproveInput) (*task.Task, Approval, error) {
	var a Approval
	requestedPaths := in.Paths
	if !e.Human {
		return nil, a, fmt.Errorf("%w: bint task approve", ErrAgent)
	}
	var explicit []string
	for _, p := range requestedPaths {
		p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
		if err := knowledge.ValidRelPath(p); err != nil {
			return nil, a, err
		}
		if p == "." || strings.ContainsAny(p, "*?[]\x00\r\n") {
			return nil, a, fmt.Errorf("--path cần tên tệp cụ thể trong repository, không phải glob: %q", p)
		}
		explicit = appendUnique(explicit, p)
	}
	t, err := e.Tasks.Update(ref, e.now(), func(t *task.Task) error {
		if len(explicit) > 0 && task.Terminal(t.State.Status) {
			return fmt.Errorf("không duyệt trước đường dẫn cho tác vụ đã %s", t.State.Status)
		}
		if cmd := strings.TrimSpace(t.Ticket.Verify.Command); cmd != "" && !e.VerifyConfirmed(t) {
			t.State.VerifyApproval = &task.Approval{Command: cmd, By: e.actor(), At: e.at()}
			a.VerifyCommand = cmd
		}
		if ss := e.SetupStatus(t); ss.Pending() {
			switch {
			case strings.TrimSpace(in.SetupSHA) == "":
				a.SetupWaiting, a.SetupWaitingSHA = ss.Command, ss.SHA256
			case !setupSHAMatches(in.SetupSHA, ss.SHA256):
				return fmt.Errorf("%w: lệnh hiện tại là %q (sha256 %s…); xem lại rồi xác nhận: bint task approve %s --setup-sha %s",
					ErrSetupChanged, ss.Command, ss.SHA256[:MinSetupSHAPrefix], t.State.ID, ss.SHA256[:MinSetupSHAPrefix])
			default:
				t.State.SetupApproval = &task.Approval{Command: ss.Command, SHA256: ss.SHA256, By: e.actor(), At: e.at()}
				a.SetupCommand = ss.Command
			}
		}
		// Tính lại từ diff hiện tại (task chưa qua Runner/checkScope thì PendingPaths rỗng).
		pending := append([]string{}, t.State.PendingPaths...)
		pending = appendUnique(pending, explicit...)
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
		if len(explicit) > 0 && task.Paused(t.State.Status) {
			// Hook deny trước khi sửa nên không có diff và b-feat chưa thực hiện
			// phần việc này. Chỉ lựa chọn --path mới yêu cầu chạy lại b-feat.
			t.State.ResumeFrom = task.StatusInProgress
			t.State.AwaitingCheck = false
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
