package evolvesvc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/fsafe"
	"bint/internal/ulid"
)

// Vòng Propose–Probe–Compose (P8, mục 16): một lần gọi `bint evolve cycle` làm trọn một
// vòng trong giới hạn ngân sách:
//
//  1. Hồi quy: đề xuất đã áp dụng mà kiểm chứng của nó không còn khớp mô hình hiện tại
//     được kiểm tra lại (--regression); hồi quy ở đề xuất do vòng tự áp dụng thì tự quay
//     lui, ở đề xuất do người áp dụng thì đề xuất quay lui.
//  2. Propose: tạo đề xuất cho cơ hội sẵn sàng (agent evolver).
//  3. Probe: mỗi đề xuất đang chờ đi qua validation rồi test; chỉ kết quả đối chiếu SAU lần
//     áp dụng gần nhất được tính (nền tri thức đã đổi thì phải đối chiếu lại).
//     Đề xuất mà lần đối chiếu trên nền hiện tại kết luận not_supported bị tự bác bỏ
//     (--keep-failed để giữ): nó không còn chặn cơ hội gốc, cơ hội mở lại khi có bằng chứng mới.
//  4. Compose: trong các đề xuất đã supported độc lập trên test, tự áp dụng tối đa MỘT đề
//     xuất rủi ro thấp mỗi vòng (evolution.adoption.auto_low_risk); các đề xuất còn lại
//     được đối chiếu lại trên nền mới ở vòng sau, nên tác động cộng dồn luôn được đo.
//
// Thay đổi workflow (đề xuất skill L2: tạo/sửa/gộp/bỏ skill) đi cùng đường Probe như tri
// thức — đo trên validation rồi test bằng nhánh base/candidate khác nhau ở tập skill — nhưng
// không bao giờ được tự áp dụng: đạt thì vào danh sách chờ người duyệt.

// CycleStep là một bước của vòng.
type CycleStep struct {
	Step     string  `json:"step"`
	Target   string  `json:"target,omitempty"`
	Outcome  string  `json:"outcome"`
	CostUSD  float64 `json:"cost_usd"`
	BatchID  string  `json:"batch_id,omitempty"`
	Duration string  `json:"duration,omitempty"`
}

// CycleReport là kết quả một vòng (lưu ở <runtime>/evolve/cycles/<id>.json để theo dõi chi phí).
type CycleReport struct {
	ID                string      `json:"cycle_id"`
	StartedAt         string      `json:"started_at"`
	FinishedAt        string      `json:"finished_at"`
	BudgetUSD         float64     `json:"budget_usd"`
	SpentUSD          float64     `json:"spent_usd"`
	GenerationUSD     float64     `json:"generation_usd"`
	ProbeUSD          float64     `json:"probe_usd"`
	Steps             []CycleStep `json:"steps"`
	Ready             []string    `json:"ready_for_human"`
	AutoApplied       []string    `json:"auto_applied"`
	RolledBack        []string    `json:"auto_rolled_back"`
	RollbackSuggested []string    `json:"rollback_suggested"`
	AutoRejected      []string    `json:"auto_rejected"`
	StopReason        string      `json:"stop_reason,omitempty"`
}

// minProbeUSD: dưới mức này không bắt đầu lần đối chiếu mới.
const minProbeUSD = 1.0

// CycleRunner là hai thao tác nặng của vòng. Bản mặc định gọi thẳng Evaluate/Propose trong tiến
// trình (huỷ được bằng context); kiểm thử thay bằng bản giả để không gọi mô hình.
type CycleRunner interface {
	// Evaluate trả (nil, lỗi) khi đối chiếu không chạy được; khi đợt chạy dở (hết hạn mức, hết
	// ngân sách...) thì trả cả kết quả dở dang lẫn lỗi của đợt. Vòng dùng kết luận của phần đã chạy
	// và bỏ lỗi đó (như `evolve evaluate --json` cũ: có đợt là thoát 0 kèm verdict); chỉ khi context
	// bị huỷ vòng mới dừng với lỗi.
	Evaluate(ctx context.Context, opts EvaluateOptions) (*EvaluateResult, error)
	Propose(ctx context.Context, opts ProposeOptions) (*ProposeResult, error)
}

// processRunner là CycleRunner mặc định: gọi dịch vụ trong tiến trình.
type processRunner struct{ e *Workspace }

func (r processRunner) Evaluate(ctx context.Context, opts EvaluateOptions) (*EvaluateResult, error) {
	return cycleEvaluateResult(Evaluate(ctx, r.e, opts))
}

// cycleEvaluateResult chuyển kết quả của Evaluate sang quy ước của CycleRunner: lỗi của đợt
// (EvaluateResult.RunErr: hết hạn mức, hết ngân sách...) đi kèm kết quả dở dang. Việc bỏ lỗi đó hay
// không do probe quyết định (bản cũ chạy `evolve evaluate --json` trả verdict và thoát 0 khi đã có
// đợt, nên ErrBudget/ErrQuota không làm vòng hỏng mà để errBudgetLeft dừng êm ở lần đối chiếu kế).
func cycleEvaluateResult(res *EvaluateResult, err error) (*EvaluateResult, error) {
	if res != nil {
		return res, res.RunErr
	}
	return nil, err
}

func (r processRunner) Propose(ctx context.Context, opts ProposeOptions) (*ProposeResult, error) {
	return Propose(ctx, r.e, opts)
}

// CycleOptions: `bint evolve cycle`.
type CycleOptions struct {
	BudgetUSD     float64 `flag:"budget" form:"required"`
	ProposeBudget float64 `flag:"propose-budget"`
	MaxProposals  int     `flag:"max"`
	Parallel      int     `flag:"parallel" form:"advanced"`
	Model         string  `flag:"model" form:"advanced"`
	NoPropose     bool    `flag:"no-propose"`
	KeepFailed    bool    `flag:"keep-failed" form:"advanced"`
	BintVersion   string
	// Log nhận các dòng tiến độ của vòng; nil = bỏ.
	Log io.Writer
	// Runner thay thế được trong kiểm thử; nil = chạy trong tiến trình.
	Runner CycleRunner
	// Now thay thế được trong kiểm thử; nil = time.Now.
	Now func() time.Time
}

// CycleDefaults: ngân sách cho evolver 1 USD, tối đa 2 đề xuất, 3 lượt song song.
func CycleDefaults(config.Config) CycleOptions {
	return CycleOptions{ProposeBudget: 1, MaxProposals: 2, Parallel: 3}
}

// Validate: cần ngân sách dương cho cả vòng. (Lỗi thường, mã thoát 1, như lệnh cũ.)
func (o CycleOptions) Validate() error {
	if o.BudgetUSD <= 0 {
		return fmt.Errorf("cần --budget > 0")
	}
	return nil
}

// Cycle làm trọn một vòng Propose–Probe–Compose trong giới hạn ngân sách và lưu báo cáo vòng
// (kể cả khi vòng dừng vì lỗi). Báo cáo luôn được trả về cùng lỗi (nếu có).
func Cycle(ctx context.Context, e *Workspace, opts CycleOptions) (CycleReport, error) {
	if err := opts.Validate(); err != nil {
		return CycleReport{}, err
	}
	if opts.Runner == nil {
		opts.Runner = processRunner{e}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rep, err := runCycle(ctx, e, opts, now)
	if rep.ID != "" {
		p := filepath.Join(e.K.Paths.Runtime, "evolve", "cycles", rep.ID+".json")
		data, _ := json.MarshalIndent(rep, "", "  ")
		fsafe.WriteAtomic(p, append(data, '\n'), 0o600)
	}
	return rep, err
}

func runCycle(ctx context.Context, e *Workspace, o CycleOptions, now func() time.Time) (CycleReport, error) {
	start := now()
	rep := CycleReport{ID: "ec_" + ulid.At(start), StartedAt: start.UTC().Format(time.RFC3339), BudgetUSD: o.BudgetUSD,
		Steps: []CycleStep{}, Ready: []string{}, AutoApplied: []string{}, RolledBack: []string{}, RollbackSuggested: []string{}, AutoRejected: []string{}}
	defer func() { rep.FinishedAt = now().UTC().Format(time.RFC3339) }()
	remaining := func() float64 { return o.BudgetUSD - rep.SpentUSD }
	logf := func(f string, a ...any) {
		if o.Log != nil {
			fmt.Fprintf(o.Log, f+"\n", a...)
		}
	}
	db := e.Store.DB
	cfg := e.K.Config

	// probe chạy một lần đối chiếu và trả về kết luận.
	probe := func(id, split string, regression bool) (string, error) {
		bud := remaining()
		if bud < minProbeUSD {
			return "", errBudgetLeft
		}
		t0 := now()
		eo := EvaluateDefaults(cfg)
		eo.ID, eo.Split, eo.Yes, eo.Regression = id, split, true, regression
		bud = roundCents(bud)
		eo.BudgetUSD = &bud
		eo.Parallel, eo.Model, eo.BintVersion = max(o.Parallel, 1), o.Model, o.BintVersion
		logf("→ đối chiếu %s trên %s (còn $%.2f)", id, split, bud)
		res, err := o.Runner.Evaluate(ctx, eo)
		if res != nil && err != nil && ctx.Err() == nil {
			// Đợt chạy dở (ErrBudget/ErrQuota...) vẫn có kết luận trên phần đã chạy: dùng nó như
			// bản cũ, không coi là lỗi. Hết ngân sách vòng thì lần đối chiếu kế trả errBudgetLeft.
			logf("  đợt %s dừng dở: %v", res.Run.BatchID, err)
			err = nil
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err() // bị huỷ giữa chừng: dừng vòng như khi tiến trình con bị kết thúc
		}
		var batchID, verdict, detail string
		if res != nil {
			batchID, verdict, detail = res.Run.BatchID, res.Verdict.Verdict, res.Verdict.Detail
		}
		cost := batchCost(ctx, db, batchID)
		rep.SpentUSD += cost
		rep.ProbeUSD += cost
		step := CycleStep{Step: "probe:" + split, Target: id, CostUSD: cost, BatchID: batchID, Duration: now().Sub(t0).Round(time.Second).String()}
		if regression {
			step.Step = "regression"
		}
		if err != nil {
			step.Outcome = "lỗi: " + err.Error()
			rep.Steps = append(rep.Steps, step)
			return "", err
		}
		step.Outcome = verdict
		if detail != "" {
			step.Outcome += " — " + detail
		}
		rep.Steps = append(rep.Steps, step)
		logf("  %s: %s ($%.2f)", id, verdict, cost)
		return verdict, nil
	}
	stop := func(err error) (CycleReport, error) {
		if err == errBudgetLeft {
			rep.StopReason = fmt.Sprintf("hết ngân sách vòng (còn $%.2f < $%.2f)", max(remaining(), 0), minProbeUSD)
			return rep, nil
		}
		rep.StopReason = err.Error()
		return rep, err
	}

	reject := func(p *evolve.Proposal, ev *evolve.Evaluation) {
		if o.KeepFailed || ev == nil || ev.Verdict != evolve.VerdictNotSupported {
			return
		}
		step := CycleStep{Step: "reject", Target: p.ID, Outcome: "tự bác bỏ: không đạt trên " + ev.Split}
		if err := evolve.AutoReject(ctx, db, p, ev, now()); err != nil {
			step.Outcome = "lỗi: " + err.Error()
		} else {
			rep.AutoRejected = append(rep.AutoRejected, p.ID)
		}
		rep.Steps = append(rep.Steps, step)
	}

	// 1. Hồi quy của đề xuất đã áp dụng.
	cur := evolve.CurrentModel(ctx, db, cfg.Eval.Agent.Model)
	hist, err := evolve.HistoryOf(ctx, db, cur, cfg.Evolution.Adoption.BindToModelVersion, e.CaseMap())
	if err != nil {
		return rep, err
	}
	for _, h := range activeAdoptions(ctx, e, hist) {
		switch {
		case strings.HasPrefix(h.ModelStatus, "chưa xác thực với mô hình hiện tại"):
			v, err := probe(h.ProposalID, eval.SplitValidation, true)
			if err != nil {
				return stop(err)
			}
			if v != evolve.VerdictNotSupported {
				continue
			}
		case strings.HasPrefix(h.ModelStatus, "HỒI QUY"):
		default:
			continue
		}
		if h.Actor != evolve.AutoActor {
			rep.RollbackSuggested = append(rep.RollbackSuggested, h.ProposalID)
			rep.Steps = append(rep.Steps, CycleStep{Step: "rollback", Target: h.ProposalID, Outcome: "đề xuất quay lui (người áp dụng): bint evolve rollback " + h.ProposalID})
			continue
		}
		if _, err := evolve.Rollback(ctx, evolve.RollbackOptions{K: e.K, DB: db, Ref: h.ProposalID, Auto: true,
			Reason: "vòng tiến hoá phát hiện hồi quy với mô hình " + cur}); err != nil {
			rep.Steps = append(rep.Steps, CycleStep{Step: "rollback", Target: h.ProposalID, Outcome: "lỗi: " + err.Error()})
			continue
		}
		rep.RolledBack = append(rep.RolledBack, h.ProposalID)
		rep.Steps = append(rep.Steps, CycleStep{Step: "rollback", Target: h.ProposalID, Outcome: "đã tự quay lui"})
	}

	// 2. Propose.
	if !o.NoPropose && remaining() >= o.ProposeBudget+minProbeUSD {
		logf("→ tạo đề xuất (ngân sách evolver $%.2f)", o.ProposeBudget)
		po := ProposeDefaults(cfg)
		po.Max, po.BudgetUSD, po.Model = max(o.MaxProposals, 1), o.ProposeBudget, o.Model
		pr, err := o.Runner.Propose(ctx, po)
		var made []string
		spent := 0.0
		if pr != nil {
			spent = pr.SpentUSD
			for _, oc := range pr.Outcomes {
				if oc.Proposal != nil {
					made = append(made, oc.Proposal.ID)
				}
			}
		}
		rep.SpentUSD += spent
		rep.GenerationUSD += spent
		step := CycleStep{Step: "propose", CostUSD: spent}
		step.Outcome = fmt.Sprintf("%d đề xuất mới %v", len(made), made)
		if err != nil {
			step.Outcome = "lỗi: " + err.Error()
		}
		rep.Steps = append(rep.Steps, step)
	}

	// 3. Probe theo giai đoạn. Chỉ tính kết quả đối chiếu sau lần áp dụng/quay lui gần nhất.
	since := lastAdoptionAt(hist)
	ps, err := evolve.ListProposals(ctx, db, evolve.StatusProposed)
	if err != nil {
		return rep, err
	}
	var ready []cycleReady
	probed := 0
	// --max đếm theo cơ hội: hai đề xuất anh em (chung/riêng) của cùng một cơ hội dùng chung một suất,
	// nên ngân sách vòng giữ nguyên trần dù mỗi cơ hội có thể cho hai ứng viên (H4).
	opps := map[string]bool{}
	for _, p := range ps {
		if !p.Quantitative {
			continue
		}

		if st, _ := e.ProposalState(p); st != evolve.StatusProposed {
			continue
		}
		evs, err := evolve.Evaluations(ctx, db, p.ID)
		if err != nil {
			return rep, err
		}
		val, test := latestProbe(evs, eval.SplitValidation, since), latestProbe(evs, eval.SplitTest, since)
		if val != nil && val.Verdict != evolve.VerdictSupported || test != nil && test.Verdict != evolve.VerdictSupported {
			reject(p, failedProbe(val, test)) // đã thua trên nền hiện tại; inconclusive thì giữ
			continue
		}
		for _, split := range []string{eval.SplitValidation, eval.SplitTest} {
			if (split == eval.SplitValidation && val != nil) || (split == eval.SplitTest && test != nil) {
				continue
			}
			if probed >= max(o.MaxProposals, 1)*4 { // 2 tập × tối đa 2 đề xuất mỗi cơ hội
				break
			}
			if !opps[p.Opportunity] && len(opps) >= max(o.MaxProposals, 1) {
				break // quá số cơ hội tối đa của vòng
			}
			opps[p.Opportunity] = true
			probed++
			v, err := probe(p.ID, split, false)
			if err != nil {
				// Hết ngân sách/hạn mức thì dừng cả vòng; lỗi riêng của đề xuất (kế hoạch không
				// khả thi, không có tác vụ) thì chuyển sang đề xuất kế.
				if err == errBudgetLeft || strings.Contains(err.Error(), "hạn mức") || ctx.Err() != nil {
					return stop(err)
				}
				break
			}
			evs, _ = evolve.Evaluations(ctx, db, p.ID)
			if v != evolve.VerdictSupported {
				reject(p, latestProbe(evs, split, since))
				break
			}
			test = latestProbe(evs, eval.SplitTest, since)
		}
		if t := latestProbe(evs, eval.SplitTest, since); t != nil && t.Verdict == evolve.VerdictSupported && t.Independent != nil && *t.Independent {
			ready = append(ready, cycleReady{p: p, gain: probeGain(ctx, db, t.BatchID), testBatch: t.BatchID})
		}
	}

	// 4a. Compose theo cặp chung/riêng (H4): trong mỗi cặp cùng đạt trên test chỉ giữ một bản.
	ready = composePairs(ctx, e, ready, cfg.Evolution.Evaluation.NonInferiorityMargin, o.KeepFailed, now, &rep)

	// 4b. Compose: tự áp dụng tối đa một đề xuất (lợi ích lớn nhất trước).
	sort.SliceStable(ready, func(i, j int) bool { return ready[i].gain > ready[j].gain })
	applied := false
	for _, rp := range ready {
		if ok, why := composeDecision(rp.p, cfg.Evolution.Adoption.AutoLowRisk, applied); !ok {
			rep.Ready = append(rep.Ready, rp.p.ID)
			rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: why})
			continue
		}
		res, err := evolve.Apply(ctx, evolve.ApplyOptions{K: e.K, DB: db, Ref: rp.p.ID, Auto: true, Cases: e.CaseMap(),
			Reason: "tự áp dụng rủi ro thấp (bint evolve cycle " + rep.ID + ")"})
		if err != nil {
			rep.Ready = append(rep.Ready, rp.p.ID)
			rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: "không tự áp dụng được: " + err.Error()})
			continue
		}
		applied = true
		rep.AutoApplied = append(rep.AutoApplied, rp.p.ID)
		rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: "đã tự áp dụng, bản ghi " + res.AdoptionID})
		rejectSiblingOf(ctx, e, rp.p, o.KeepFailed, now, &rep)
	}
	return rep, nil
}

var errBudgetLeft = fmt.Errorf("hết ngân sách vòng")

// roundCents làm tròn ngân sách còn lại xuống 2 chữ số thập phân, như khi truyền --budget cho lệnh con.
func roundCents(v float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	return r
}

// cycleReady là đề xuất đã supported độc lập trên test trong vòng, kèm đợt test để tính hiệu ứng.
type cycleReady struct {
	p         *evolve.Proposal
	gain      float64
	testBatch string
}

// composePairs áp quy tắc compose của H4 lên các đề xuất đã đạt: với mỗi cặp chung/riêng mà CẢ HAI
// nửa cùng đạt trên test, giữ một nửa theo evolve.ComposePair và bác bỏ nửa kia (trừ keepFailed).
// Nửa duy nhất đạt giữ nguyên. Không bao giờ gộp hai bản: bản gộp phải là đề xuất mới và đối chiếu
// lại. Hiệu ứng của mỗi bản tính trên tác vụ thuộc vùng của bản riêng, từ chính đợt test của nó.
func composePairs(ctx context.Context, e *Workspace, ready []cycleReady, margin float64, keepFailed bool, now func() time.Time, rep *CycleReport) []cycleReady {
	byKey := map[string]map[string]int{} // khoá cặp → biến thể → chỉ số trong ready
	for i, r := range ready {
		if r.p.Pair == nil {
			continue
		}
		if byKey[r.p.Pair.Key] == nil {
			byKey[r.p.Pair.Key] = map[string]int{}
		}
		byKey[r.p.Pair.Key][r.p.Pair.Variant] = i
	}
	drop := map[int]bool{}
	cases := e.CaseMap()
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		gi, hasG := byKey[key][evolve.PairGlobal]
		di, hasD := byKey[key][evolve.PairDomain]
		if !hasG || !hasD {
			continue
		}
		g, d := ready[gi], ready[di]
		area := evolve.PairAreaOf(d.p)
		gEff, _ := evolve.AreaEffect(ctx, e.Store.DB, g.testBatch, area, cases)
		dEff, _ := evolve.AreaEffect(ctx, e.Store.DB, d.testBatch, area, cases)
		keep, why := evolve.ComposePair(evolve.PairProbe{Supported: true, AreaEffect: gEff}, evolve.PairProbe{Supported: true, AreaEffect: dEff}, margin)
		kept, lost := g, d
		if keep == evolve.PairDomain {
			kept, lost = d, g
		}
		rep.Steps = append(rep.Steps, CycleStep{Step: "compose-pair", Target: kept.p.ID, Outcome: fmt.Sprintf("giữ bản %s, bỏ %s: %s", keep, lost.p.ID, why)})
		if !keepFailed {
			if err := evolve.RejectSibling(ctx, e.Store.DB, lost.p, kept.p.ID, why, now()); err != nil {
				rep.Steps = append(rep.Steps, CycleStep{Step: "compose-pair", Target: lost.p.ID, Outcome: "lỗi: " + err.Error()})
			} else {
				rep.AutoRejected = append(rep.AutoRejected, lost.p.ID)
			}
		}
		lostIdx := di
		if keep == evolve.PairDomain {
			lostIdx = gi
		}
		drop[lostIdx] = true
	}
	var out []cycleReady
	for i, r := range ready {
		if !drop[i] {
			out = append(out, r)
		}
	}
	return out
}

// rejectSiblingOf bác bỏ nửa cặp còn chờ sau khi một nửa đã được áp dụng: hai bản không bao giờ
// được áp dụng cùng nhau mà chưa đo bản gộp (đề xuất gộp phải là đề xuất mới).
func rejectSiblingOf(ctx context.Context, e *Workspace, p *evolve.Proposal, keepFailed bool, now func() time.Time, rep *CycleReport) {
	if p.Pair == nil || keepFailed {
		return
	}
	sib, err := evolve.GetProposal(ctx, e.Store.DB, p.Pair.Sibling)
	if err != nil || sib.Status != evolve.StatusProposed {
		return
	}
	if err := evolve.RejectSibling(ctx, e.Store.DB, sib, p.ID, "đã áp dụng bản anh em", now()); err != nil {
		rep.Steps = append(rep.Steps, CycleStep{Step: "compose-pair", Target: sib.ID, Outcome: "lỗi: " + err.Error()})
		return
	}
	rep.AutoRejected = append(rep.AutoRejected, sib.ID)
	rep.Steps = append(rep.Steps, CycleStep{Step: "compose-pair", Target: sib.ID, Outcome: "bác bỏ: bản anh em " + p.ID + " đã được áp dụng"})
}

// composeDecision: đề xuất đã supported độc lập trên test có được vòng tự áp dụng không;
// nếu không, lý do (hiện ở báo cáo vòng).
func composeDecision(p *evolve.Proposal, autoLowRisk, alreadyApplied bool) (bool, string) {
	if ok, why := evolve.AutoApplicable(p); !ok {
		if p.IsSkill() {
			return false, "thay đổi workflow (skill) đã đạt, cần người duyệt"
		}
		return false, "cần người duyệt: " + why
	}
	if !autoLowRisk {
		return false, "cần người duyệt: evolution.adoption.auto_low_risk = false"
	}
	if alreadyApplied {
		return false, "đối chiếu lại trên nền mới ở vòng sau (đã tự áp dụng một đề xuất trong vòng này)"
	}
	return true, ""
}

// failedProbe: lần đối chiếu đã kết luận not_supported (validation trước), nil nếu không có.
func failedProbe(val, test *evolve.Evaluation) *evolve.Evaluation {
	for _, ev := range []*evolve.Evaluation{val, test} {
		if ev != nil && ev.Verdict == evolve.VerdictNotSupported {
			return ev
		}
	}
	return nil
}

// activeAdoptions: lần áp dụng còn hiệu lực (đề xuất đang ở trạng thái applied), mỗi đề xuất một.
func activeAdoptions(ctx context.Context, e *Workspace, hs []evolve.HistoryEntry) []evolve.HistoryEntry {
	seen := map[string]bool{}
	var out []evolve.HistoryEntry
	for _, h := range hs { // mới nhất trước
		if seen[h.ProposalID] {
			continue
		}
		seen[h.ProposalID] = true
		if h.Action != "apply" {
			continue
		}
		if p, err := evolve.GetProposal(ctx, e.Store.DB, h.ProposalID); err == nil && p.Status == evolve.StatusApplied {
			out = append(out, h)
		}
	}
	return out
}

func lastAdoptionAt(hs []evolve.HistoryEntry) string {
	last := ""
	for _, h := range hs {
		if h.CreatedAt > last {
			last = h.CreatedAt
		}
	}
	return last
}

// latestProbe: lần đối chiếu (kind probe) mới nhất trên split, tạo sau since.
func latestProbe(evs []evolve.Evaluation, split, since string) *evolve.Evaluation {
	var best *evolve.Evaluation
	for i := range evs {
		ev := &evs[i]
		if ev.Split != split || (ev.Kind != "" && ev.Kind != evolve.EvalProbe) || ev.Verdict == "" || ev.CreatedAt <= since {
			continue
		}
		if best == nil || ev.CreatedAt > best.CreatedAt {
			best = ev
		}
	}
	return best
}

// batchCost: tổng chi phí harness báo của một đợt.
func batchCost(ctx context.Context, db *sql.DB, batch string) float64 {
	if batch == "" {
		return 0
	}
	rows, err := eval.Attempts(ctx, db, "batch_id = ?", batch)
	if err != nil {
		return 0
	}
	sum := 0.0
	for _, r := range rows {
		if r.CostUSD != nil {
			sum += *r.CostUSD
		}
	}
	return sum
}

// probeGain: tỷ lệ đạt candidate − base của một đợt đối chiếu (để xếp thứ tự áp dụng).
func probeGain(ctx context.Context, db *sql.DB, batch string) float64 {
	rows, err := eval.Attempts(ctx, db, "batch_id = ?", batch)
	if err != nil {
		return 0
	}
	pass, n := map[string]float64{}, map[string]float64{}
	for _, r := range rows {
		if r.TestPassed == nil {
			continue
		}
		n[r.Arm]++
		if *r.TestPassed {
			pass[r.Arm]++
		}
	}
	rate := func(a string) float64 {
		if n[a] == 0 {
			return 0
		}
		return pass[a] / n[a]
	}
	return rate(eval.ArmCandidate) - rate(eval.ArmBase)
}

// CycleTotals cộng các báo cáo vòng đã lưu (theo dõi chi phí tiến hoá, mục 12.8).
func CycleTotals(runtime string) (int, CycleReport) {
	var sum CycleReport
	files, _ := filepath.Glob(filepath.Join(runtime, "evolve", "cycles", "ec_*.json"))
	n := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		var r CycleReport
		if err != nil || json.Unmarshal(data, &r) != nil {
			continue
		}
		n++
		sum.SpentUSD += r.SpentUSD
		sum.GenerationUSD += r.GenerationUSD
		sum.ProbeUSD += r.ProbeUSD
		sum.AutoApplied = append(sum.AutoApplied, r.AutoApplied...)
		sum.RolledBack = append(sum.RolledBack, r.RolledBack...)
		sum.AutoRejected = append(sum.AutoRejected, r.AutoRejected...)
	}
	return n, sum
}
