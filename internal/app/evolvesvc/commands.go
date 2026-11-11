package evolvesvc

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

// ---- scan ----

// ScanOptions: `bint evolve scan`.
type ScanOptions struct {
	Days  int  `flag:"days"`
	NoGit bool `flag:"no-git-scan"`
}

func ScanDefaults(config.Config) ScanOptions { return ScanOptions{} }

// Validate: chưa có ràng buộc (days <= 0 nghĩa là cửa sổ mặc định).
func (o ScanOptions) Validate() error { return nil }

// Scan phát hiện cơ hội tiến hoá và kiểm tra điều kiện tiên quyết về dữ liệu (không gọi mô hình).
func Scan(ctx context.Context, e *Workspace, opts ScanOptions) (evolve.ScanResult, error) {
	if err := opts.Validate(); err != nil {
		return evolve.ScanResult{}, err
	}
	res, _, err := e.scanData(ctx, opts.Days, !opts.NoGit)
	return res, err
}

// ---- propose ----

// ProposeOptions: `bint evolve propose [op-id|khoá...]`.
type ProposeOptions struct {
	Refs           []string `arg:"refs" form:"suggest=opportunities"`
	BudgetUSD      float64  `flag:"budget"`
	Max            int      `flag:"max" form:"advanced"`
	Days           int      `flag:"days" form:"advanced"`
	AgentCommand   string   `flag:"agent-command" form:"advanced"`
	Model          string   `flag:"model" form:"advanced"`
	ReflectorModel string   `flag:"reflector-model" form:"advanced"`
	NoAgent        bool     `flag:"no-agent"`
	// Warn nhận cảnh báo khi một agent phụ không dùng được (bỏ qua cơ hội tương ứng); nil = bỏ.
	Warn io.Writer
}

func ProposeDefaults(config.Config) ProposeOptions { return ProposeOptions{} }

// Validate: chưa có ràng buộc mức giá trị (cần ngân sách do evolve.Propose kiểm theo cơ hội).
func (o ProposeOptions) Validate() error { return nil }

// ProposeResult: báo cáo đề xuất kèm kết quả quét dùng để tạo.
type ProposeResult struct {
	evolve.ProposeReport
	Scan evolve.ScanResult
}

// Propose tạo đề xuất cho cơ hội sẵn sàng. Thao tác chỉ dành cho người (trong phiên agent trả
// lỗi bọc ErrAgentForbidden). Mọi đường gọi evolve.Propose phải truyền Cases và RepoDir (nếu
// thiếu, kiểm rò rỉ của bài học báo "không tìm thấy tác vụ" sau khi reflector đã chạy: tốn tiền
// rồi bỏ hết).
func Propose(ctx context.Context, e *Workspace, opts ProposeOptions) (*ProposeResult, error) {
	if err := app.RequireHuman("bint evolve propose"); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	scan, h, err := e.scanData(ctx, opts.Days, true)
	if err != nil {
		return nil, err
	}
	warnf := func(f string, a ...any) {
		if opts.Warn != nil {
			fmt.Fprintf(opts.Warn, f, a...)
		}
	}
	var agent, reflector, hproposer eval.Agent
	if !opts.NoAgent {
		if agent, err = EvolverAgent(e.K, opts.Model, opts.AgentCommand); err != nil {
			return nil, err
		}
		// Như reflector: không dùng được (roles.evolver khác claude) chỉ làm mất cơ hội quy trình.
		if hproposer, err = HarnessProposerAgent(e.K, opts.Model, opts.AgentCommand); err != nil {
			warnf("! không dùng được agent đề xuất quy trình (bỏ qua cơ hội quy trình): %v\n", err)
			hproposer = nil
		}
		// Reflector không dùng được (vd roles.reflector khác claude) chỉ làm mất cơ hội
		// failure_lesson; đề xuất xác định và của evolver vẫn chạy (như đường deferred).
		if reflector, err = ReflectorAgent(e.K, opts.ReflectorModel, opts.AgentCommand); err != nil {
			warnf("! không dùng được agent reflector (bỏ qua cơ hội bài học từ thất bại): %v\n", err)
			reflector = nil
		}
	}
	cases, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	rep, err := evolve.Propose(ctx, evolve.ProposeInput{K: e.K, DB: e.Store.DB, Scan: scan, History: h, Refs: opts.Refs,
		Max: opts.Max, Agent: agent, Reflector: reflector, HarnessProposer: hproposer, Cases: cases, RepoDir: e.WS.Repo.Toplevel,
		BudgetUSD: opts.BudgetUSD, Timeout: time.Duration(e.K.Config.Eval.TimeoutMinutes) * time.Minute})
	if err != nil {
		return nil, err
	}
	return &ProposeResult{ProposeReport: rep, Scan: scan}, nil
}

// ---- list ----

// ListOptions: `bint evolve list`.
type ListOptions struct {
	All bool `flag:"all"`
}

func ListDefaults(config.Config) ListOptions { return ListOptions{} }
func (o ListOptions) Validate() error        { return nil }

// ListRow là một đề xuất kèm trạng thái hiển thị và các lần đối chiếu.
type ListRow struct {
	Proposal    *evolve.Proposal
	State       string
	Evaluations []evolve.Evaluation
}

// List liệt kê đề xuất (mặc định: đang chờ).
func List(ctx context.Context, e *Workspace, opts ListOptions) ([]ListRow, error) {
	var statuses []string
	if !opts.All {
		statuses = []string{evolve.StatusProposed}
	}
	ps, err := evolve.ListProposals(ctx, e.Store.DB, statuses...)
	if err != nil {
		return nil, err
	}
	rows := make([]ListRow, 0, len(ps))
	for _, p := range ps {
		st, _ := e.ProposalState(p)
		evs, err := evolve.Evaluations(ctx, e.Store.DB, p.ID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, ListRow{Proposal: p, State: st, Evaluations: evs})
	}
	return rows, nil
}

// ---- show ----

// ShowOptions: `bint evolve show <id>`.
type ShowOptions struct {
	ID string `arg:"id" form:"required,suggest=proposals"`
}

func ShowDefaults(config.Config) ShowOptions { return ShowOptions{} }

func (o ShowOptions) Validate() error {
	if o.ID == "" {
		return app.Usagef("cần ID đề xuất")
	}
	return nil
}

// ShowResult là chi tiết một đề xuất.
type ShowResult struct {
	Proposal     *evolve.Proposal
	State        string
	BaseProblems []evolve.BaseProblem
	Evaluations  []evolve.Evaluation
	Adoptions    []evolve.Adoption
	LabelDrift   []string
	// Items, Evolved, Cases: ngữ cảnh để so sánh nội dung đề xuất với hiện trạng (lỗi đọc bị bỏ qua).
	Items   []*knowledge.Item
	Evolved []*skills.Evolved
	Cases   map[string]*eval.Case
	// Harness: thành phần quy trình hiện có (.bint/harness) để so với đề xuất loại harness (lỗi đọc bị bỏ qua).
	Harness []hcomp.Component
}

// Show đọc chi tiết đề xuất: thay đổi, phiên bản gốc, căn cứ, kế hoạch và kết quả đối chiếu.
func Show(ctx context.Context, e *Workspace, opts ShowOptions) (*ShowResult, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	p, err := evolve.GetProposal(ctx, e.Store.DB, opts.ID)
	if err != nil {
		return nil, err
	}
	st, probs := e.ProposalState(p)
	evs, err := evolve.Evaluations(ctx, e.Store.DB, p.ID)
	if err != nil {
		return nil, err
	}
	ads, err := evolve.Adoptions(ctx, e.Store.DB, p.ID)
	if err != nil {
		return nil, err
	}
	h, err := evolve.LoadHistory(ctx, e.Store.DB, time.Now(), 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	res := &ShowResult{Proposal: p, State: st, BaseProblems: probs, Evaluations: evs, Adoptions: ads,
		LabelDrift: evolve.LabelDrift(p, h), Cases: e.CaseMap()}
	res.Items, _, _ = e.K.Items()
	res.Evolved, _, _ = skills.LoadEvolved(e.K.Root)
	res.Harness = hcomp.Load(e.K.Root).Components
	return res, nil
}

// ---- apply / reject / rollback ----

// ApplyOptions: `bint evolve apply <id>`.
type ApplyOptions struct {
	Ref         string `arg:"id" form:"required,suggest=proposals"`
	Unverified  bool   `flag:"unverified" form:"hidden"`
	ConfirmRisk bool   `flag:"confirm-risk" form:"hidden"`
	Reason      string `flag:"reason"`
}

func ApplyDefaults(config.Config) ApplyOptions { return ApplyOptions{} }

func (o ApplyOptions) Validate() error {
	if o.Ref == "" {
		return app.Usagef("cần ID đề xuất")
	}
	return nil
}

// Apply áp dụng đề xuất vào .bint/knowledge hoặc .bint/skills/evolved và ghi bản kiểm toán để quay
// lui. Thao tác chỉ dành cho người (kiểm tra nằm trong evolve.Apply).
func Apply(ctx context.Context, e *Workspace, opts ApplyOptions) (evolve.ApplyResult, error) {
	if err := opts.Validate(); err != nil {
		return evolve.ApplyResult{}, err
	}
	return evolve.Apply(ctx, evolve.ApplyOptions{K: e.K, DB: e.Store.DB, Ref: opts.Ref,
		Unverified: opts.Unverified, ConfirmRisk: opts.ConfirmRisk, Reason: opts.Reason, Cases: e.CaseMap()})
}

// RejectOptions: `bint evolve reject <id>`.
type RejectOptions struct {
	Ref    string `arg:"id" form:"required,suggest=proposals"`
	Reason string `flag:"reason"`
}

func RejectDefaults(config.Config) RejectOptions { return RejectOptions{} }

func (o RejectOptions) Validate() error {
	if o.Ref == "" {
		return app.Usagef("cần ID đề xuất")
	}
	return nil
}

// Reject bác bỏ đề xuất đang chờ. Thao tác chỉ dành cho người (kiểm tra nằm trong evolve.Reject).
func Reject(ctx context.Context, e *Workspace, opts RejectOptions) (*evolve.Proposal, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return evolve.Reject(ctx, e.Store.DB, opts.Ref, opts.Reason, time.Now())
}

// RollbackOptions: `bint evolve rollback <id>`.
type RollbackOptions struct {
	Ref    string `arg:"id" form:"required,suggest=proposals"`
	Force  bool   `flag:"force" form:"hidden"`
	Reason string `flag:"reason"`
	// ExpectConflicts (không phải cờ): tệp xung đột người đã xem khi bật Force (TUI), kèm dấu vân tay nội dung lúc
	// xem (RollbackPlan.SeenConflicts, ContentHash). Khác nil: nếu tập xung đột hoặc nội dung một tệp xung đột đã đổi
	// thì từ chối và không ghi gì (người chỉ đồng ý ghi đè đúng nội dung đã xem). CLI không đặt.
	ExpectConflicts map[string]string
}

func RollbackDefaults(config.Config) RollbackOptions { return RollbackOptions{} }

func (o RollbackOptions) Validate() error {
	if o.Ref == "" {
		return app.Usagef("cần ID đề xuất")
	}
	return nil
}

// Rollback quay lui đề xuất đã áp dụng (khôi phục tệp từ bản kiểm toán). Chỉ dành cho người.
func Rollback(ctx context.Context, e *Workspace, opts RollbackOptions) (evolve.RollbackResult, error) {
	if err := opts.Validate(); err != nil {
		return evolve.RollbackResult{}, err
	}
	if opts.Force && opts.ExpectConflicts != nil {
		// Người đã xem một tập tệp xung đột và đồng ý ghi đè đúng tập đó: nếu tệp đã đổi giữa lúc xem và lúc xác
		// nhận thì từ chối, không ghi gì (người phải xem lại).
		if err := app.RequireHuman("bint evolve rollback"); err != nil {
			return evolve.RollbackResult{}, err
		}
		plan, err := PlanRollback(ctx, e, opts)
		if err != nil {
			return evolve.RollbackResult{}, err
		}
		if !sameConflicts(plan, opts.ExpectConflicts) {
			seen := slices.Sorted(maps.Keys(opts.ExpectConflicts))
			return evolve.RollbackResult{}, fmt.Errorf("tệp xung đột đã thay đổi so với bản đã xem (đã xem: %s; hiện tại: %s): xem lại rồi quay lui lần nữa",
				listOrNone(seen), listOrNone(plan.Conflicts))
		}
	}
	return evolve.Rollback(ctx, evolve.RollbackOptions{K: e.K, DB: e.Store.DB, Ref: opts.Ref, Force: opts.Force, Reason: opts.Reason})
}

// ---- history ----

// HistoryOptions: `bint evolve history`.
type HistoryOptions struct{}

func HistoryDefaults(config.Config) HistoryOptions { return HistoryOptions{} }
func (o HistoryOptions) Validate() error           { return nil }

// HistoryResult: lịch sử áp dụng/quay lui và tổng các vòng tự động đã lưu.
type HistoryResult struct {
	Entries []evolve.HistoryEntry
	// Cycles: số vòng đã lưu; CycleTotals: tổng chi phí và kết quả của chúng (0 vòng: rỗng).
	Cycles      int
	CycleTotals CycleReport
}

// History trả lịch sử áp dụng/quay lui: kiểm chứng, mô hình, theo dõi sau áp dụng, chi phí tiến hoá.
func History(ctx context.Context, e *Workspace, _ HistoryOptions) (*HistoryResult, error) {
	cur := evolve.CurrentModel(ctx, e.Store.DB, e.K.Config.Eval.Agent.Model)
	hs, err := evolve.HistoryOf(ctx, e.Store.DB, cur, e.K.Config.Evolution.Adoption.BindToModelVersion, e.CaseMap())
	if err != nil {
		return nil, err
	}
	n, sum := CycleTotals(e.K.Paths.Runtime)
	return &HistoryResult{Entries: hs, Cycles: n, CycleTotals: sum}, nil
}
