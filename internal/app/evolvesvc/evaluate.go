package evolvesvc

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

// ConfirmEvaluateKey là khoá i18n của lời hỏi xác nhận trước khi đối chiếu.
const ConfirmEvaluateKey = "evolve.evaluate.confirm"

// EvaluateOptions: `bint evolve evaluate <id>`.
type EvaluateOptions struct {
	ID    string
	Split string `flag:"split"`
	// Repeats > 0 cố định số lần lặp mỗi nhánh; 0 = do bộ lập kế hoạch Probe chọn.
	// RepeatsExplicit: người dùng chỉ định (khi đó phải >= 1).
	Repeats         int `flag:"repeats"`
	RepeatsExplicit bool
	Force           bool     `flag:"force"`
	BudgetUSD       *float64 `flag:"budget"`
	Limit           int      `flag:"limit"`
	Yes             bool     `flag:"yes"`
	Harness         string   `flag:"harness"`
	AgentCommand    string   `flag:"agent-command"`
	Model           string   `flag:"model"`
	LeakageMode     string   `flag:"leakage-mode"`
	Delivery        string   `flag:"delivery"`
	// DeliveryExplicit: người dùng chỉ định cách đưa tri thức (đề xuất quy trình chỉ chấp nhận bint).
	DeliveryExplicit bool
	Parallel         int    `flag:"parallel"`
	Seed             uint64 `flag:"seed"`
	KeepWorkdirs     bool   `flag:"keep-workdirs"`
	Bare             bool   `flag:"bare"`
	Resume           string `flag:"resume"`
	Regression       bool   `flag:"regression"`
	Prospective      bool   `flag:"prospective"`
	BintVersion      string

	// Confirm hỏi "Bắt đầu đối chiếu?" (nil và không Yes: lỗi dùng sai).
	Confirm app.ConfirmFunc
	// OnSelect báo tập tác vụ đã chọn; OnPlan báo kế hoạch Probe; OnEstimate báo ước tính chi phí
	// (trước các kiểm tra ngân sách); Progress báo từng lượt chạy.
	OnSelect   func(EvaluateSelection)
	OnPlan     func(pp eval.ProbePlan, vm eval.VarModel, in eval.PlanInput, sequential bool)
	OnEstimate func(est eval.Estimate, arms []string, repeats int, budget *float64)
	Progress   func(r eval.AttemptRow, n, total int)
}

// EvaluateSelection mô tả việc chọn tác vụ cho lần đối chiếu.
type EvaluateSelection struct {
	ProposalID string
	Split      string
	// Prospective: chế độ tiến cứu; Older = số tác vụ bị bỏ vì có commit trước khi đề xuất được tạo.
	Prospective bool
	Older       int
	Pool        int // tác vụ đã kiểm chứng trong tập
	Unverified  int // tác vụ chưa kiểm chứng bị bỏ
	Chosen      int // tác vụ chịu ảnh hưởng của đề xuất
	// Dropped: lý do loại tác vụ → số lượng.
	Dropped map[string]int
}

// EvaluateDefaults: giá trị theo evolution.evaluation.
func EvaluateDefaults(cfg config.Config) EvaluateOptions {
	ev := cfg.Evolution.Evaluation
	return EvaluateOptions{Split: eval.SplitValidation, Repeats: ev.Repeats, BudgetUSD: ev.BudgetUSD, Harness: cfg.Eval.Harness,
		LeakageMode: ev.LeakageMode, Delivery: ev.Delivery, Parallel: 1}
}

// Validate: ràng buộc mức giá trị (loại trừ nhau và khoảng giá trị không cần biết đề xuất).
func (o EvaluateOptions) Validate() error {
	if o.ID == "" {
		return app.Usagef("cần ID đề xuất")
	}
	if o.Delivery != "" && o.Delivery != eval.ArmBint && o.Delivery != eval.ArmMinimal {
		return app.Usagef("--delivery phải là bint | minimal")
	}
	if o.LeakageMode != "" && o.LeakageMode != eval.LeakStrict && o.LeakageMode != eval.LeakEconomy {
		return app.Usagef("--leakage-mode phải là strict | economy")
	}
	if o.Resume == "" {
		if !o.Regression && o.Split != eval.SplitValidation && o.Split != eval.SplitTest {
			return app.Usagef("--split phải là validation | test (tập build dùng để xây đề xuất, mục 12.2)")
		}
		if o.RepeatsExplicit && o.Repeats < 1 {
			return app.Usagef("--repeats phải >= 1")
		}
	}
	return nil
}

// EvaluateResult là kết quả một lần đối chiếu đã có đợt chạy.
type EvaluateResult struct {
	Proposal   *evolve.Proposal
	Record     evolve.Evaluation
	Run        eval.RunResult
	Verdict    evolve.Verdict
	Report     eval.Report
	Activation []evolve.SkillActivation
	// RunErr là lỗi của đợt chạy (hết ngân sách, hết hạn mức, bị huỷ...), nếu có: đợt vẫn được
	// kết luận trên phần đã chạy.
	RunErr error
}

// Evaluate đối chiếu base/candidate (Probe) trên tác vụ chịu ảnh hưởng của đề xuất. Trả
// (nil, lỗi) khi lỗi xảy ra trước khi có đợt chạy; trả (kết quả, nil) khi đã có đợt (lỗi của đợt
// nằm ở EvaluateResult.RunErr).
func Evaluate(ctx context.Context, e *Workspace, opts EvaluateOptions) (*EvaluateResult, error) {
	cfg := e.K.Config
	ev := cfg.Evolution.Evaluation
	if !ev.Enabled {
		return nil, fmt.Errorf("evolution.evaluation.enabled = false")
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	p, err := evolve.GetProposal(ctx, e.Store.DB, opts.ID)
	if err != nil {
		return nil, err
	}
	regression := opts.Regression
	if regression {
		if p.Status != evolve.StatusApplied {
			return nil, app.Usagef("--regression chỉ dùng cho đề xuất đã áp dụng (đề xuất %s: %s)", p.ID, p.Status)
		}
	} else {
		if p.Status != evolve.StatusProposed {
			return nil, fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ đối chiếu đề xuất đang chờ", p.ID, p.Status)
		}
		if st, probs := e.ProposalState(p); st == "outdated" {
			return nil, fmt.Errorf("đề xuất %s không còn hợp lệ: %s: %s", p.ID, probs[0].ID, probs[0].Detail)
		}
	}
	delivery := opts.Delivery
	if delivery == "" {
		delivery = ev.Delivery
	}
	if delivery != eval.ArmBint && delivery != eval.ArmMinimal {
		return nil, app.Usagef("--delivery phải là bint | minimal")
	}
	if p.IsHarness() {
		// Hook, MCP và guardrail chỉ có ở nhánh bint: thành phần quy trình không đo được bằng minimal.
		if opts.DeliveryExplicit && delivery != eval.ArmBint {
			return nil, app.Usagef("đề xuất quy trình chỉ đo được với --delivery bint (guardrail và chỉ dẫn đi qua hook/MCP của nhánh bint)")
		}
		if regression {
			return nil, app.Usagef("--regression chưa hỗ trợ cho đề xuất quy trình")
		}
		delivery = eval.ArmBint
	}
	mode := ev.LeakageMode
	if opts.LeakageMode != "" {
		mode = opts.LeakageMode
	}
	if mode != eval.LeakStrict && mode != eval.LeakEconomy {
		return nil, app.Usagef("--leakage-mode phải là strict | economy")
	}
	now := time.Now()
	// Bản sao Env: Variant là trạng thái của lần đối chiếu này, không rò sang lần khác.
	env := e.Env
	var skillBefore, skillAfter evolve.SkillSet
	var harnessBefore, harnessAfter hcomp.Set
	if p.IsHarness() {
		// Base = tập thành phần đang có hiệu lực; candidate = tập đó sau khi áp đề xuất (thành
		// phần mới coi như đã duyệt). Cùng tập tri thức, cùng nhánh bint.
		harnessBefore, harnessAfter, err = evolve.HarnessSets(hcomp.Load(e.K.Root), p)
		if err != nil {
			return nil, err
		}
		env.Variant = evolve.HarnessVariant(p, harnessBefore, harnessAfter)
	} else if p.IsSkill() {
		list, _, err := skills.LoadEvolved(e.K.Root)
		if err != nil {
			return nil, err
		}
		if regression {
			// Skill đã áp dụng: base = trước đề xuất (dựng lại), candidate = hiện tại.
			if skillBefore, err = evolve.ReverseSet(e.K.Root, p, list); err != nil {
				return nil, err
			}
			skillAfter = evolve.CurrentSet(list)
		} else {
			skillBefore = evolve.CurrentSet(list)
			if skillAfter, err = evolve.ApplyToSet(p, skillBefore, e.K.Source(), cfg.Context.Freshness.Fingerprint); err != nil {
				return nil, err
			}
		}
		env.Variant = evolve.SkillVariant(p, delivery, e.WS.Repo.Toplevel, skillBefore, skillAfter)
	} else if regression {
		// Tri thức đã áp dụng: base = tập trước đề xuất (dựng lại từ bản ghi kiểm toán).
		bi, err := evolve.BeforeItems(ctx, e.Store.DB, p)
		if err != nil {
			return nil, err
		}
		cur, _, err := e.K.Items()
		if err != nil {
			return nil, err
		}
		touchedNow := map[string]*knowledge.Item{}
		for _, it := range cur {
			if _, ok := bi[it.ID]; ok {
				touchedNow[it.ID] = it
			}
		}
		env.Variant = evolve.RegressionVariant(p, delivery, e.WS.Repo.Toplevel, bi, touchedNow)
	} else {
		env.Variant = evolve.Variant(p, delivery, e.WS.Repo.Toplevel, e.K.Source(), cfg.Context.Freshness.Fingerprint, now)
	}

	var agent eval.Agent
	if opts.AgentCommand != "" {
		agent = &eval.CommandAgent{Command: opts.AgentCommand}
	} else {
		h := opts.Harness
		if h == "" {
			h = cfg.Eval.Harness
		}
		if agent, err = newAgent(cfg, h, opts.Model, opts.Bare); err != nil {
			return nil, err
		}
	}
	ro := eval.RunOptions{Agent: agent, RequestedModel: opts.Model, Parallel: opts.Parallel, Seed: opts.Seed, KeepWorkdirs: opts.KeepWorkdirs,
		BintVersion: opts.BintVersion, ResumeBatch: opts.Resume, PriorRunUSD: cfg.Eval.PriorRunUSD, RepeatMajor: true,
		BudgetUSD: opts.BudgetUSD}
	curBatch := opts.Resume // đợt đang chạy, cho callback Look
	allCases, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	current := map[string]*eval.Case{}
	for _, c := range allCases {
		current[c.ID] = c
	}
	var record evolve.Evaluation
	split, seed, limit := opts.Split, opts.Seed, opts.Limit
	var repeats int // số lần lặp mỗi nhánh của lần đối chiếu mới (do --repeats hoặc bộ lập kế hoạch Probe)
	if opts.Resume != "" {
		record, err = evolve.EvaluationByBatch(ctx, e.Store.DB, opts.Resume)
		if err != nil {
			return nil, fmt.Errorf("đợt %s không thuộc lần đối chiếu đề xuất nào: %w", opts.Resume, err)
		}
		if record.ProposalID != p.ID || record.BaseKey != p.BaseKey() {
			return nil, fmt.Errorf("đợt %s thuộc đề xuất/phiên bản gốc khác", opts.Resume)
		}
		env.Variant.Delivery = record.Delivery
		ro.Cases = allCases
	} else {
		if !regression && split != eval.SplitValidation && split != eval.SplitTest {
			return nil, app.Usagef("--split phải là validation | test (tập build dùng để xây đề xuất, mục 12.2)")
		}
		candidates := app.FilterSplit(allCases, split)
		if regression {
			// Tập nhỏ đại diện: chính các tác vụ đã làm căn cứ áp dụng đề xuất (mục 12.6).
			ids, err := evolve.OriginalCases(ctx, e.Store.DB, p)
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				return nil, fmt.Errorf("đề xuất %s không có lần đối chiếu supported nào để lấy tập đại diện", p.ID)
			}
			candidates = nil
			for _, c := range allCases {
				if slices.Contains(ids, c.ID) {
					candidates = append(candidates, c)
				}
			}
			if limit == 0 {
				limit = 5
			}
		}
		var pool []*eval.Case
		sel := EvaluateSelection{ProposalID: p.ID, Split: split, Prospective: opts.Prospective, Dropped: map[string]int{}}
		for _, c := range candidates {
			if !c.Verified() {
				sel.Unverified++
				continue
			}
			if opts.Prospective && !evolve.Prospective(p, []string{c.ID}, map[string]*eval.Case{c.ID: c}) {
				sel.Older++
				continue
			}
			pool = append(pool, c)
		}
		sel.Pool = len(pool)
		// Mọi lượt căn cứ của đề xuất, kể cả ngoài cửa sổ phân tích.
		hist, err := evolve.LoadHistory(ctx, e.Store.DB, now, 10*365*24*time.Hour)
		if err != nil {
			return nil, err
		}
		var impacts []evolve.CaseImpact
		switch {
		case regression:
			for _, c := range pool {
				impacts = append(impacts, evolve.CaseImpact{CaseID: c.ID, Split: c.Split, Case: c, Affected: true, Reason: "tập đại diện"})
			}
		case p.IsHarness():
			impacts, err = evolve.SelectHarnessCases(ctx, env, mode, p, hist, pool, harnessBefore, harnessAfter)
		case p.IsSkill():
			impacts, err = evolve.SelectSkillCases(ctx, env, mode, p, hist, pool, skillBefore, skillAfter)
		default:
			impacts, err = evolve.SelectCases(ctx, env, mode, p, hist, pool)
		}
		if err != nil {
			return nil, err
		}
		var chosen []*eval.Case
		for _, ci := range impacts {
			if ci.Affected {
				chosen = append(chosen, ci.Case)
			} else {
				sel.Dropped[firstClause(ci.Reason)]++
			}
		}
		slices.SortFunc(chosen, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
		if limit > 0 && len(chosen) > limit {
			chosen = chosen[:limit]
		}
		sel.Chosen = len(chosen)
		if opts.OnSelect != nil {
			opts.OnSelect(sel)
		}
		if len(chosen) == 0 {
			return nil, fmt.Errorf("không tác vụ nào thuộc tập %s chịu ảnh hưởng của đề xuất; đề xuất không kiểm chứng định lượng được trên tập này", split)
		}
		if seed == 0 {
			seed = uint64(now.UnixNano())
		}
		// R cố định bởi --repeats hoặc evolution.evaluation.repeats > 0; ngược lại do PlanProbe chọn.
		fixed := 0
		if opts.Repeats > 0 {
			fixed = opts.Repeats
		}
		sequential := !regression && evolve.Sequential(p.Plan)
		var seqNote string
		if regression {
			// Tập đại diện nhỏ (không nhằm phát hiện hiệu ứng): giữ cách cũ, không lập kế hoạch.
			if repeats = fixed; repeats < 1 {
				repeats = max(cfg.Eval.Repeats, 1)
			}
		} else {
			effect := p.Plan.MinEffect
			if p.Plan.Kind == "non_inferiority" {
				effect = p.Plan.Margin
			}
			vm, err := eval.HistoricalVariance(ctx, e.Store.DB, agent.Name(), ev.EffectSD)
			if err != nil {
				return nil, err
			}
			in := eval.PlanInput{Available: len(chosen), Effect: effect, SigmaB: vm.SigmaB, SigmaBMeasured: vm.SigmaMeasured, W: vm.W, MaxRepeats: ev.MaxRepeats, FixedRepeats: fixed}
			if avg, n, err := eval.HistoricalCost(ctx, e.Store.DB, agent.Name(), opts.Model); err != nil {
				return nil, err
			} else if n > 0 {
				in.CostPerRun = avg
			}
			pp := eval.PlanProbe(in)
			if opts.OnPlan != nil {
				opts.OnPlan(pp, vm, in, sequential)
			}
			if !pp.Feasible && !opts.Force {
				return nil, fmt.Errorf("kế hoạch không khả thi: với %d tác vụ và tối đa %d lần lặp chỉ phát hiện được chênh lệch từ %.1f điểm %% (cần %.1f); thêm tác vụ, nâng evolution.evaluation.min_effect/non_inferiority_margin, hoặc dùng --force để chạy vẫn",
					len(chosen), pp.Repeats, 100*pp.MinDetectable, 100*effect)
			}
			repeats = pp.Repeats
			if pp.Feasible && pp.Cases < len(chosen) {
				// Chỉ chạy đủ số tác vụ cần: chọn ngẫu nhiên theo hạt giống để không thiên về ID.
				rng := rand.New(rand.NewPCG(seed, 0x70726f62))
				rng.Shuffle(len(chosen), func(i, j int) { chosen[i], chosen[j] = chosen[j], chosen[i] })
				chosen = chosen[:pp.Cases]
				slices.SortFunc(chosen, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
			}
			seqNote = fmt.Sprintf("%d tác vụ × %d lần lặp × 2 nhánh, tối đa %d lượt, %d điểm nhìn ở lần lặp %v",
				len(chosen), repeats, len(chosen)*2*repeats, len(evolve.SeqLooks(repeats)), evolve.SeqLooks(repeats))
		}
		arms := []string{eval.ArmBase, eval.ArmCandidate}
		ro.Cases, ro.Arms, ro.Repeats, ro.LeakageMode, ro.Seed = chosen, arms, repeats, mode, seed
		ro.Notes = append(app.EvalRunNotes(chosen, mode, agent, opts.Bare), fmt.Sprintf("đối chiếu đề xuất %s (%s), cách đưa tri thức: %s, tập %s", p.ID, p.Action, delivery, split))
		if sequential {
			ro.Notes = append(ro.Notes, evolve.SeqNotePrefix+seqNote)
		}
		est, err := eval.EstimateRun(ctx, e.Store.DB, eval.EstimateInput{Cases: len(chosen), Arms: arms, Repeats: repeats,
			Harness: agent.Name(), Model: opts.Model, Effect: p.Plan.MinEffect, EffectSD: ev.EffectSD})
		if err != nil {
			return nil, err
		}
		if opts.OnEstimate != nil {
			opts.OnEstimate(est, arms, repeats, ro.BudgetUSD)
		}
		ro.EstimatedUSD = est.TotalCost
		if agent.ReportsCost() {
			if ro.BudgetUSD == nil {
				return nil, app.Usagef("cần ngân sách: --budget <USD> hoặc evolution.evaluation.budget_usd (mục 11.2: duyệt ngân sách trước khi chạy)")
			}
			if est.TotalCost != nil && *est.TotalCost > *ro.BudgetUSD {
				return nil, fmt.Errorf("%w: ước tính $%.2f > ngân sách $%.2f", eval.ErrBudget, *est.TotalCost, *ro.BudgetUSD)
			}
		}
		record = evolve.Evaluation{ProposalID: p.ID, Split: split, BaseKey: p.BaseKey(), Delivery: delivery, CreatedAt: nowISO(now)}
		if regression {
			record.Kind, record.Split = evolve.EvalRegression, ""
			ro.Notes = append(ro.Notes, "kiểm tra hồi quy mô hình (mục 12.6): kết quả không dùng làm căn cứ áp dụng")
		}
		for _, c := range chosen {
			record.CaseIDs = append(record.CaseIDs, c.ID)
		}
		ro.OnBatch = func(id string) error {
			curBatch = id
			record.BatchID = id
			return evolve.RecordEvaluation(ctx, e.Store.DB, &record)
		}
	}
	if err := app.ConfirmOrFail(opts.Yes, opts.Confirm, app.ConfirmRequest{Key: ConfirmEvaluateKey, Text: "Bắt đầu đối chiếu?"}); err != nil {
		return nil, err
	}
	ro.Progress = opts.Progress
	// Điểm nhìn chỉ cho đợt tuần tự: đợt mới (đánh dấu ở ghi chú) hoặc đợt tiếp tục có đánh dấu đó.
	if !regression && evolve.Sequential(p.Plan) && record.Kind != evolve.EvalRegression {
		resumed := false
		if opts.Resume != "" {
			if b, err := eval.GetBatch(ctx, e.Store.DB, opts.Resume); err == nil {
				resumed = evolve.IsSequential(b)
			}
		}
		if opts.Resume == "" || resumed {
			ro.Look = evolve.NewLook(ctx, e.Store.DB, p.Plan, func() string { return curBatch }, current)
		}
	}
	res, runErr := eval.Run(ctx, env, e.Store.DB, ro)
	if res.BatchID == "" {
		return nil, runErr
	}
	if record.ID == "" {
		// Tiếp tục: bản ghi đã có từ lần đầu.
		if record, err = evolve.EvaluationByBatch(ctx, e.Store.DB, res.BatchID); err != nil {
			return nil, err
		}
	}
	v, rep, err := evolve.Finalize(context.WithoutCancel(ctx), e.Store.DB, p, record, current)
	if err != nil {
		return nil, err
	}
	out := &EvaluateResult{Proposal: p, Record: record, Run: res, Verdict: v, Report: rep, RunErr: runErr}
	// Kích hoạt skill thực tế trong các lượt đối chiếu (đọc từ đầu ra của agent, không qua hook).
	if len(p.SkillChanges) > 0 {
		var names []string
		for _, c := range p.SkillChanges {
			if c.Op == evolve.ActionCreate || c.Op == evolve.ActionUpdate {
				names = append(names, c.Name)
			}
		}
		if rows, aerr := eval.Attempts(ctx, e.Store.DB, "batch_id = ?", res.BatchID); aerr == nil {
			out.Activation = evolve.ProbeActivation(rows, names)
		}
	}
	return out, nil
}

func firstClause(s string) string {
	// Gom lý do theo phần trước dấu ":" thứ hai để không liệt kê từng mục.
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) <= 110 {
		return s
	}
	return string(r[:109]) + "…"
}

func nowISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
