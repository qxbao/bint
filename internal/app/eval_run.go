package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/harness"
)

// EvalSelection là bộ lọc tác vụ dùng chung cho estimate, run và bootstrap.
type EvalSelection struct {
	Split   string   `flag:"split" form:"choices=build|validation|test"`
	CaseIDs []string `flag:"case" form:"suggest=eval_ids"`
	Limit   int      `flag:"limit" form:"advanced"`
}

// Validate: tên tập hợp lệ.
func (s EvalSelection) Validate() error { return checkSplit("--split", s.Split) }

// Pick chọn các tác vụ đã duyệt theo bộ lọc, sắp theo ID.
func (s EvalSelection) Pick(e *EvalWorkspace) ([]*eval.Case, error) {
	all, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	var out []*eval.Case
	if len(s.CaseIDs) > 0 {
		for _, id := range s.CaseIDs {
			c, approved, err := e.Cases.Get(id)
			if err != nil {
				return nil, err
			}
			if !approved {
				return nil, fmt.Errorf("%s chưa được duyệt (`bint eval approve %s`)", c.ID, c.ID)
			}
			out = append(out, c)
		}
	} else {
		out = all
	}
	if s.Split != "" {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		out = FilterSplit(out, s.Split)
	}
	slices.SortFunc(out, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
	if s.Limit > 0 && len(out) > s.Limit {
		out = out[:s.Limit]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("không có tác vụ đã duyệt nào phù hợp (`bint eval mine`, rồi `bint eval approve`)")
	}
	return out, nil
}

// ---- eval estimate ----

// EvalEstimateOptions: `bint eval estimate`.
type EvalEstimateOptions struct {
	EvalSelection
	// Arms rỗng = lấy eval.arms; ArmsExplicit = người dùng chỉ định (nhánh không dùng được là lỗi).
	Arms         []string `flag:"arms" form:"suggest=arms"`
	ArmsExplicit bool
	Repeats      int      `flag:"repeats"`
	Effect       float64  `flag:"effect" form:"advanced"`
	CostPerRun   *float64 `flag:"cost-per-run" form:"advanced"`
	Model        string   `flag:"model" form:"advanced"`
	Harness      string   `flag:"harness" form:"advanced,suggest=harnesses"`
	// Log nhận các dòng thông báo (nhánh bị bỏ); nil = bỏ.
	Log io.Writer
}

// EvalEstimateDefaults: số lần lặp và nhánh theo cấu hình.
func EvalEstimateDefaults(cfg config.Config) EvalEstimateOptions {
	return EvalEstimateOptions{Arms: cfg.Eval.Arms, Repeats: cfg.Eval.Repeats, Effect: 0.10, Harness: "claude"}
}

func (o EvalEstimateOptions) Validate() error { return o.EvalSelection.Validate() }

// EvalEstimateResult: ước tính kèm các tham số đã dùng (để định dạng).
type EvalEstimateResult struct {
	Estimate  eval.Estimate
	Arms      []string
	Repeats   int
	BudgetUSD *float64
}

func logf(w io.Writer) func(string, ...any) {
	return func(f string, a ...any) {
		if w != nil {
			fmt.Fprintf(w, f, a...)
		}
	}
}

// EvalEstimate ước tính số lượt, chi phí và cỡ mẫu trước khi chạy. Cần mở với withDB.
func EvalEstimate(ctx context.Context, e *EvalWorkspace, opts EvalEstimateOptions) (*EvalEstimateResult, error) {
	cases, err := opts.EvalSelection.Pick(e)
	if err != nil {
		return nil, err
	}
	arms, err := e.resolveArms(opts.Arms, opts.ArmsExplicit, logf(opts.Log))
	if err != nil {
		return nil, err
	}
	in := eval.EstimateInput{Cases: len(cases), Arms: arms, Repeats: opts.Repeats, Harness: opts.Harness, Model: opts.Model,
		Effect: opts.Effect, CostPerRun: opts.CostPerRun}
	est, err := eval.EstimateRun(ctx, e.Store.DB, in)
	if err != nil {
		return nil, err
	}
	return &EvalEstimateResult{Estimate: est, Arms: arms, Repeats: opts.Repeats, BudgetUSD: e.WS.Config.Eval.BudgetUSD}, nil
}

// ---- eval run ----

// EvalRunOptions: `bint eval run`.
type EvalRunOptions struct {
	EvalSelection
	Arms         []string `flag:"arms" form:"suggest=arms"`
	ArmsExplicit bool
	Repeats      int      `flag:"repeats"`
	BudgetUSD    *float64 `flag:"budget"` // mặc định eval.budget_usd (xem EvalRunDefaults)
	// BudgetExplicit: người dùng chỉ định ngân sách (khi tiếp tục đợt cũ thì ghi đè ngân sách của đợt).
	BudgetExplicit bool
	CostPerRun     *float64 `flag:"cost-per-run" form:"advanced"`
	AgentCommand   string   `flag:"agent-command" form:"advanced"`
	Harness        string   `flag:"harness" form:"suggest=harnesses"`
	Model          string   `flag:"model" form:"advanced"`
	Bare           bool     `flag:"bare" form:"advanced"`
	Parallel       int      `flag:"parallel" form:"advanced"`
	Seed           uint64   `flag:"seed" form:"advanced"`
	KeepWorkdirs   bool     `flag:"keep-workdirs" form:"advanced"`
	Resume         string   `flag:"resume" form:"hidden"` // đặt bởi hành động "Chạy tiếp" của màn Đánh giá
	LeakageMode    string   `flag:"leakage-mode" form:"choices=strict|economy"`
	Yes            bool     `flag:"yes" form:"hidden"`
	BintVersion    string

	// Confirm hỏi "Bắt đầu chạy?" (nil và không Yes: lỗi dùng sai "cần --yes khi chạy không tương tác").
	Confirm ConfirmFunc
	// Log nhận thông báo nhánh bị bỏ.
	Log io.Writer
	// OnResume được gọi khi tiếp tục một đợt cũ; OnEstimate ngay sau khi ước tính (trước các
	// kiểm tra ngân sách); OnNotes sau các kiểm tra ngân sách (reportsCost = harness báo chi phí).
	OnResume func(b eval.Batch)
	// OnPlan báo kế hoạch của đợt (tác vụ × nhánh × lần lặp) ngay trước lời hỏi xác nhận, cả đợt mới (sau Pick và
	// resolveArms) lẫn đợt tiếp tục (Done: các lượt đã có kết quả trong cơ sở dữ liệu); TUI dựng lưới ô từ đây.
	OnPlan     func(EvalRunPlan)
	OnEstimate func(est eval.Estimate, arms []string, repeats int, budget *float64)
	OnNotes    func(notes []string, reportsCost bool)
	Progress   func(r eval.AttemptRow, n, total int)
}

// EvalRunPlan là kế hoạch của một đợt chạy: tập ô là tích CaseIDs × Arms × [1..Repeats].
type EvalRunPlan struct {
	BatchID string // chỉ có khi tiếp tục một đợt cũ
	CaseIDs []string
	Arms    []string
	Repeats int
	// Done: các lượt đã có kết quả của đợt (chỉ khi tiếp tục; không gồm lượt dở hoặc lỗi hạ tầng vì chúng chạy lại).
	Done []eval.AttemptRow
}

// EvalRunDefaults: số lần lặp, chế độ chống rò rỉ và harness theo cấu hình.
func EvalRunDefaults(cfg config.Config) EvalRunOptions {
	return EvalRunOptions{Arms: cfg.Eval.Arms, Repeats: cfg.Eval.Repeats, Parallel: 1, Harness: cfg.Eval.Harness,
		LeakageMode: cfg.Eval.LeakageMode, BudgetUSD: cfg.Eval.BudgetUSD}
}

// Validate: ràng buộc mức giá trị. Khi tiếp tục một đợt (Resume) các tham số chọn tác vụ không
// được dùng lại nên không kiểm tra.
func (o EvalRunOptions) Validate() error {
	if o.Resume != "" {
		return nil
	}
	if err := o.EvalSelection.Validate(); err != nil {
		return err
	}
	if o.Repeats < 1 {
		return Usagef("--repeats phải >= 1")
	}
	if o.LeakageMode != "" && o.LeakageMode != eval.LeakStrict && o.LeakageMode != eval.LeakEconomy {
		return Usagef("--leakage-mode phải là strict | economy")
	}
	return nil
}

// EvalRunResult là kết quả một đợt.
type EvalRunResult struct {
	eval.RunResult
	ReportsCost bool
}

// runNotes là các lưu ý hiện trước khi chạy.
func runNotes(cases []*eval.Case, mode string, agent eval.Agent, bare bool) []string {
	var notes []string
	if mode == eval.LeakEconomy {
		notes = append(notes, "chế độ economy: kết quả chỉ dùng để sàng lọc, không dùng cho cổng quyết định")
	}
	unv := 0
	for _, c := range cases {
		if !c.Verified() {
			unv++
		}
	}
	if unv > 0 {
		notes = append(notes, fmt.Sprintf("%d tác vụ chưa kiểm chứng fail→pass", unv))
	}
	if agent.Name() == "claude" && !bare {
		notes = append(notes, "CLAUDE.md và bộ nhớ cấp người dùng (~/.claude) vẫn có thể được nạp ở mọi nhánh; dùng --bare để cô lập hoàn toàn")
	}
	notes = append(notes, "agent chạy với quyền của người dùng, mạng không bị chặn ở tầng hệ điều hành (security.allow_network_in_evals chỉ là chỉ dẫn ở P1)")
	return notes
}

// EvalRunNotes xuất khẩu các lưu ý trước khi chạy cho nhóm tiến hoá (đối chiếu dùng chung khung chạy).
func EvalRunNotes(cases []*eval.Case, mode string, agent eval.Agent, bare bool) []string {
	return runNotes(cases, mode, agent, bare)
}

// NewEvalAgent dựng agent chạy tác vụ: lệnh tuỳ chỉnh, hoặc harness (mặc định eval.harness).
func NewEvalAgent(cfg config.Config, command, harnessName, model string, bare bool) (eval.Agent, error) {
	if command != "" {
		return &eval.CommandAgent{Command: command}, nil
	}
	h := harnessName
	if h == "" {
		h = cfg.Eval.Harness
	}
	if bare && h != "claude" {
		return nil, Usagef("--bare chỉ dùng với harness claude")
	}
	return harness.NewAgent(h, harness.AgentOptions{Config: cfg, Eval: true, Model: model,
		MaxCost: cfg.Limits.PerRun.MaxCostUSD, Bare: bare})
}

// EvalRun chạy các nhánh so sánh × nhiều lần lặp trên tác vụ đã duyệt. Lỗi hệ thống và huỷ giữa
// chừng trả kèm kết quả đợt (nếu đã có), để người gọi vẫn in được kết quả một phần.
func EvalRun(ctx context.Context, e *EvalWorkspace, opts EvalRunOptions) (*EvalRunResult, error) {
	cfg := e.WS.Config
	agent, err := NewEvalAgent(cfg, opts.AgentCommand, opts.Harness, opts.Model, opts.Bare)
	if err != nil {
		return nil, err
	}
	ro := eval.RunOptions{
		Agent: agent, RequestedModel: opts.Model, Parallel: opts.Parallel, Seed: opts.Seed, KeepWorkdirs: opts.KeepWorkdirs,
		BintVersion: opts.BintVersion, ResumeBatch: opts.Resume, PriorRunUSD: cfg.Eval.PriorRunUSD, BudgetUSD: opts.BudgetUSD,
	}
	allCases, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	if opts.Resume != "" {
		ro.Cases = allCases
		b, err := eval.GetBatch(ctx, e.Store.DB, opts.Resume)
		if err != nil {
			return nil, err
		}
		if !opts.BudgetExplicit {
			ro.BudgetUSD = b.BudgetUSD
		}
		if opts.OnResume != nil {
			opts.OnResume(b)
		}
		if opts.OnPlan != nil {
			done, err := eval.Attempts(ctx, e.Store.DB, "batch_id = ?", b.BatchID)
			if err != nil {
				return nil, err
			}
			// Cùng tiêu chí với eval.Run: lượt đang chạy dở và lượt hỏng vì hạ tầng sẽ được chạy lại.
			done = slices.DeleteFunc(done, func(r eval.AttemptRow) bool {
				return r.Status == "running" || (r.Status == "error" && strings.HasPrefix(r.Error, eval.InfraErrorPrefix))
			})
			opts.OnPlan(EvalRunPlan{BatchID: b.BatchID, CaseIDs: b.CaseIDs, Arms: b.Arms, Repeats: b.Repeats, Done: done})
		}
	} else {
		if err := opts.Validate(); err != nil {
			return nil, err
		}
		cases, err := opts.EvalSelection.Pick(e)
		if err != nil {
			return nil, err
		}
		arms, err := e.resolveArms(opts.Arms, opts.ArmsExplicit, logf(opts.Log))
		if err != nil {
			return nil, err
		}
		mode := cfg.Eval.LeakageMode
		if opts.LeakageMode != "" {
			mode = opts.LeakageMode
		}
		seed := opts.Seed
		if seed == 0 {
			seed = uint64(time.Now().UnixNano())
		}
		ro.Cases, ro.Arms, ro.Repeats, ro.LeakageMode, ro.Seed = cases, arms, opts.Repeats, mode, seed
		ro.Notes = runNotes(cases, mode, agent, opts.Bare)

		in := eval.EstimateInput{Cases: len(cases), Arms: arms, Repeats: opts.Repeats, Harness: agent.Name(), Model: opts.Model,
			Effect: 0.10, CostPerRun: opts.CostPerRun}
		est, err := eval.EstimateRun(ctx, e.Store.DB, in)
		if err != nil {
			return nil, err
		}
		if opts.OnEstimate != nil {
			opts.OnEstimate(est, arms, opts.Repeats, ro.BudgetUSD)
		}
		ro.EstimatedUSD = est.TotalCost
		if agent.ReportsCost() {
			if ro.BudgetUSD == nil {
				return nil, Usagef("cần ngân sách: --budget <USD> hoặc eval.budget_usd (mục 12.5: không chạy khi chưa có ngân sách được duyệt)")
			}
			if est.TotalCost != nil && *est.TotalCost > *ro.BudgetUSD {
				return nil, fmt.Errorf("%w: ước tính $%.2f > ngân sách $%.2f; giảm --limit/--repeats/--arms hoặc tăng --budget",
					eval.ErrBudget, *est.TotalCost, *ro.BudgetUSD)
			}
		}
		if opts.OnNotes != nil {
			opts.OnNotes(ro.Notes, agent.ReportsCost())
		}
		if opts.OnPlan != nil {
			ids := make([]string, len(cases))
			for i, c := range cases {
				ids[i] = c.ID
			}
			opts.OnPlan(EvalRunPlan{CaseIDs: ids, Arms: arms, Repeats: opts.Repeats})
		}
	}
	if err := ConfirmOrFail(opts.Yes, opts.Confirm, ConfirmRequest{Key: ConfirmEvalRunKey, Text: "Bắt đầu chạy?"}); err != nil {
		return nil, err
	}
	ro.Progress = opts.Progress
	res, err := eval.Run(ctx, e.Env, e.Store.DB, ro)
	return &EvalRunResult{RunResult: res, ReportsCost: agent.ReportsCost()}, err
}

// ---- eval report ----

// EvalReportOptions: `bint eval report [batch-id]`.
type EvalReportOptions struct {
	BatchID  string `arg:"batch-id" form:"suggest=batches"`
	Baseline string `flag:"baseline" form:"suggest=arms"`
	List     bool   `flag:"list" form:"hidden"` // TUI luôn có danh sách đợt ở thẻ Đợt
	// (--json, --cases chỉ định dạng đầu ra: không thuộc dịch vụ)
}

func EvalReportDefaults(config.Config) EvalReportOptions { return EvalReportOptions{} }
func (o EvalReportOptions) Validate() error              { return nil }

// EvalReportResult: danh sách đợt (List) hoặc báo cáo một đợt.
type EvalReportResult struct {
	Batches []eval.Batch
	Report  eval.Report
}

// EvalReport dựng báo cáo một đợt (mặc định đợt mới nhất) hoặc liệt kê tối đa 50 đợt.
func EvalReport(ctx context.Context, e *EvalWorkspace, opts EvalReportOptions) (*EvalReportResult, error) {
	if opts.List {
		bs, err := eval.ListBatches(ctx, e.Store.DB, 50)
		if err != nil {
			return nil, err
		}
		return &EvalReportResult{Batches: bs}, nil
	}
	id := opts.BatchID
	if id == "" {
		id = "latest"
	}
	b, err := eval.GetBatch(ctx, e.Store.DB, id)
	if err != nil {
		return nil, err
	}
	current := map[string]*eval.Case{}
	if cs, err := e.Cases.Cases(); err == nil {
		for _, c := range cs {
			current[c.ID] = c
		}
	}
	rep, err := eval.BuildReport(ctx, e.Store.DB, b, opts.Baseline, current)
	if err != nil {
		return nil, err
	}
	return &EvalReportResult{Report: rep}, nil
}

// ---- eval bootstrap ----

// EvalBootstrapOptions: `bint eval bootstrap`.
type EvalBootstrapOptions struct {
	EvalSelection
	BudgetUSD    *float64 `flag:"budget"`
	AgentCommand string   `flag:"agent-command" form:"advanced"`
	Model        string   `flag:"model" form:"advanced"`
	Force        bool     `flag:"force"`
	KeepWorkdirs bool     `flag:"keep-workdirs" form:"advanced"`
	List         bool     `flag:"list" form:"hidden"`
	Yes          bool     `flag:"yes" form:"hidden"`
	BintVersion  string
	// BintPath là tệp bint đưa vào PATH của agent; rỗng = tệp đang chạy.
	BintPath string

	Confirm ConfirmFunc
	// OnPlan sau khi biết số commit gốc cần dựng (trước lời hỏi xác nhận).
	OnPlan func(cases, bases, todo int)
	// OnStep báo từng commit gốc: Stopped = dừng vì chạm ngân sách (Spent, Budget, Remaining).
	OnStep func(EvalBootstrapStep)
}

// EvalBootstrapStep là một mốc của vòng dựng tri thức.
type EvalBootstrapStep struct {
	Index, Total int // Index tính từ 1
	Base         string
	Meta         eval.SnapshotMeta
	Err          error
	// Stopped: vòng dừng trước commit này vì chạm ngân sách.
	Stopped        bool
	Spent, Budget  float64
	RemainingBases int
}

// EvalBootstrapDefaults: ngân sách theo cấu hình.
func EvalBootstrapDefaults(cfg config.Config) EvalBootstrapOptions {
	return EvalBootstrapOptions{BudgetUSD: cfg.Eval.BudgetUSD}
}

func (o EvalBootstrapOptions) Validate() error { return o.EvalSelection.Validate() }

// EvalSnapshotInfo là một dòng của `--list`: ảnh chụp tri thức của một commit gốc.
type EvalSnapshotInfo struct {
	Base string
	Err  error
	Has  bool
	Meta eval.SnapshotMeta
}

// EvalBootstrapResult là kết quả. Với List chỉ có Snapshots; ngược lại Cases/Bases/Todo/Spent/Failed.
type EvalBootstrapResult struct {
	Snapshots          []EvalSnapshotInfo
	Cases, Bases, Todo int
	Spent              float64
	Failed             int
	ReportsCost        bool
}

// EvalBootstrap dựng tri thức tại commit gốc của từng tác vụ (b-bootstrap) cho nhánh minimal ở
// chế độ strict.
func EvalBootstrap(ctx context.Context, e *EvalWorkspace, opts EvalBootstrapOptions) (*EvalBootstrapResult, error) {
	cases, err := opts.EvalSelection.Pick(e)
	if err != nil {
		return nil, err
	}
	var bases []string
	for _, cs := range cases {
		if !slices.Contains(bases, cs.Source.Base) {
			bases = append(bases, cs.Source.Base)
		}
	}
	res := &EvalBootstrapResult{Cases: len(cases), Bases: len(bases)}
	if opts.List {
		for _, b := range bases {
			meta, _, ok, err := eval.ReadSnapshot(e.Env.WorkRoot, b)
			res.Snapshots = append(res.Snapshots, EvalSnapshotInfo{Base: b, Err: err, Has: ok, Meta: meta})
		}
		return res, nil
	}
	var todo []string
	for _, b := range bases {
		if _, _, ok, _ := eval.ReadSnapshot(e.Env.WorkRoot, b); ok && !opts.Force {
			continue
		}
		todo = append(todo, b)
	}
	res.Todo = len(todo)
	if opts.OnPlan != nil {
		opts.OnPlan(len(cases), len(bases), len(todo))
	}
	if len(todo) == 0 {
		return res, nil
	}
	var agent eval.Agent
	if opts.AgentCommand != "" {
		agent = &eval.CommandAgent{Command: opts.AgentCommand}
	} else {
		agent = &eval.ClaudeAgent{Cfg: e.WS.Config.Eval.Agent, Model: opts.Model, MaxCostUSD: e.WS.Config.Limits.PerRun.MaxCostUSD}
	}
	res.ReportsCost = agent.ReportsCost()
	bud := opts.BudgetUSD
	if agent.ReportsCost() && bud == nil {
		return res, Usagef("cần ngân sách: --budget <USD> hoặc eval.budget_usd")
	}
	if err := ConfirmOrFail(opts.Yes, opts.Confirm, ConfirmRequest{Key: ConfirmEvalBootstrapKey,
		Text: fmt.Sprintf("Chạy b-bootstrap bằng %s cho %d commit gốc?", agent.Name(), len(todo))}); err != nil {
		return res, err
	}
	self := opts.BintPath
	if self == "" {
		self, _ = os.Executable()
	}
	for i, b := range todo {
		if bud != nil && res.Spent >= *bud {
			if opts.OnStep != nil {
				opts.OnStep(EvalBootstrapStep{Index: i + 1, Total: len(todo), Base: b, Stopped: true, Spent: res.Spent, Budget: *bud, RemainingBases: len(todo) - i})
			}
			break
		}
		meta, err := eval.BootstrapSnapshot(ctx, e.Env, b, eval.BootstrapOptions{Agent: agent, BintPath: self, BintVersion: opts.BintVersion, Keep: opts.KeepWorkdirs})
		if meta.CostUSD != nil {
			res.Spent += *meta.CostUSD
		}
		if opts.OnStep != nil {
			opts.OnStep(EvalBootstrapStep{Index: i + 1, Total: len(todo), Base: b, Meta: meta, Err: err})
		}
		if err != nil {
			res.Failed++
			if ctx.Err() != nil {
				return res, fmt.Errorf("đã dừng theo yêu cầu")
			}
		}
	}
	return res, nil
}
