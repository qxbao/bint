package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

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

type cycleOpts struct {
	budget, proposeBudget float64
	maxProposals          int
	parallel              int
	model                 string
	noPropose             bool
	keepFailed            bool
	asJSON                bool
}

func newEvolveCycleCmd(stdio IO) *cobra.Command {
	var o cycleOpts
	c := &cobra.Command{
		Use:   "cycle",
		Short: "Một vòng tiến hoá tự động Propose–Probe–Compose trong giới hạn ngân sách (P8)",
		Long: `Làm trọn một vòng: kiểm tra hồi quy của đề xuất đã áp dụng → tạo đề xuất → đối chiếu
trên validation rồi test → tự áp dụng tối đa một đề xuất rủi ro thấp (chỉ tạo mục mới, mục
vào trạng thái candidate; cần evolution.adoption.auto_low_risk: true). Đề xuất khác đã đạt
được liệt kê chờ người duyệt (bint evolve apply). Mọi chi phí (tạo ứng viên, đối chiếu) cộng
vào --budget; hết ngân sách hoặc agent hết hạn mức thì vòng dừng, gọi lại để làm tiếp.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.budget <= 0 {
				return fmt.Errorf("cần --budget > 0")
			}
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			run := &cycleRunner{exe: exe, dir: e.k.Root, log: io.Discard}
			if !o.asJSON {
				run.log = stdio.Out
			}
			rep, err := runCycle(cmd.Context(), e, run, o, time.Now)
			if p := filepath.Join(e.k.Paths.Runtime, "evolve", "cycles", rep.ID+".json"); rep.ID != "" {
				data, _ := json.MarshalIndent(rep, "", "  ")
				fsafe.WriteAtomic(p, append(data, '\n'), 0o600)
			}
			if o.asJSON {
				if werr := writeJSON(stdio.Out, rep); werr != nil {
					return werr
				}
				return err
			}
			printCycle(stdio.Out, rep)
			return err
		},
	}
	f := c.Flags()
	f.Float64Var(&o.budget, "budget", 0, "ngân sách USD cho cả vòng (tạo ứng viên + đối chiếu)")
	f.Float64Var(&o.proposeBudget, "propose-budget", 1, "ngân sách USD cho agent evolver trong vòng")
	f.IntVar(&o.maxProposals, "max", 2, "số đề xuất tối đa được đối chiếu trong vòng")
	f.IntVarP(&o.parallel, "parallel", "j", 3, "số lượt chạy song song khi đối chiếu")
	f.StringVar(&o.model, "model", "", "mô hình cho evolver và các lượt đối chiếu")
	f.BoolVar(&o.noPropose, "no-propose", false, "không tạo đề xuất mới, chỉ xử lý đề xuất đang chờ")
	f.BoolVar(&o.keepFailed, "keep-failed", false, "không tự bác bỏ đề xuất đã kết luận not_supported")
	f.BoolVar(&o.asJSON, "json", false, "xuất JSON")
	return c
}

// cycleRunner gọi các lệnh con của chính bint (cùng tệp thực thi, cùng thư mục) với --json.
type cycleRunner struct {
	exe, dir string
	log      io.Writer
	// call thay thế được trong kiểm thử.
	call func(ctx context.Context, args ...string) ([]byte, error)
}

func (r *cycleRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	if r.call != nil {
		return r.call(ctx, args...)
	}
	c := exec.CommandContext(ctx, r.exe, args...)
	c.Dir = r.dir
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return out.Bytes(), fmt.Errorf("bint %s: %v: %s", strings.Join(args[:min(2, len(args))], " "), err, msg)
	}
	return out.Bytes(), nil
}

func runCycle(ctx context.Context, e *evolveCtx, r *cycleRunner, o cycleOpts, now func() time.Time) (CycleReport, error) {
	start := now()
	rep := CycleReport{ID: "ec_" + ulid.At(start), StartedAt: start.UTC().Format(time.RFC3339), BudgetUSD: o.budget,
		Steps: []CycleStep{}, Ready: []string{}, AutoApplied: []string{}, RolledBack: []string{}, RollbackSuggested: []string{}, AutoRejected: []string{}}
	defer func() { rep.FinishedAt = now().UTC().Format(time.RFC3339) }()
	remaining := func() float64 { return o.budget - rep.SpentUSD }
	logf := func(f string, a ...any) { fmt.Fprintf(r.log, f+"\n", a...) }
	db := e.st.DB
	cfg := e.k.Config
	modelArgs := func(args []string) []string {
		if o.model != "" {
			args = append(args, "--model", o.model)
		}
		return args
	}

	// probe chạy một lần đối chiếu và trả về kết luận.
	probe := func(id, split string, extra ...string) (string, error) {
		bud := remaining()
		if bud < minProbeUSD {
			return "", errBudgetLeft
		}
		t0 := now()
		args := modelArgs(append([]string{"evolve", "evaluate", id, "--split", split, "-y", "--json",
			"--budget", strconv.FormatFloat(bud, 'f', 2, 64), "-j", strconv.Itoa(max(o.parallel, 1))}, extra...))
		logf("→ đối chiếu %s trên %s (còn $%.2f)", id, split, bud)
		out, err := r.run(ctx, args...)
		var res struct {
			BatchID string         `json:"batch_id"`
			Verdict evolve.Verdict `json:"verdict"`
		}
		json.Unmarshal(lastJSON(out), &res)
		cost := batchCost(ctx, db, res.BatchID)
		rep.SpentUSD += cost
		rep.ProbeUSD += cost
		step := CycleStep{Step: "probe:" + split, Target: id, CostUSD: cost, BatchID: res.BatchID, Duration: now().Sub(t0).Round(time.Second).String()}
		if len(extra) > 0 {
			step.Step = "regression"
		}
		if err != nil {
			step.Outcome = "lỗi: " + err.Error()
			rep.Steps = append(rep.Steps, step)
			return "", err
		}
		step.Outcome = res.Verdict.Verdict
		if res.Verdict.Detail != "" {
			step.Outcome += " — " + res.Verdict.Detail
		}
		rep.Steps = append(rep.Steps, step)
		logf("  %s: %s ($%.2f)", id, res.Verdict.Verdict, cost)
		return res.Verdict.Verdict, nil
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
		if o.keepFailed || ev == nil || ev.Verdict != evolve.VerdictNotSupported {
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
	hist, err := evolve.HistoryOf(ctx, db, cur, cfg.Evolution.Adoption.BindToModelVersion, e.caseMap())
	if err != nil {
		return rep, err
	}
	for _, h := range activeAdoptions(ctx, e, hist) {
		switch {
		case strings.HasPrefix(h.ModelStatus, "chưa xác thực với mô hình hiện tại"):
			v, err := probe(h.ProposalID, eval.SplitValidation, "--regression")
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
		if _, err := evolve.Rollback(ctx, evolve.RollbackOptions{K: e.k, DB: db, Ref: h.ProposalID, Auto: true,
			Reason: "vòng tiến hoá phát hiện hồi quy với mô hình " + cur}); err != nil {
			rep.Steps = append(rep.Steps, CycleStep{Step: "rollback", Target: h.ProposalID, Outcome: "lỗi: " + err.Error()})
			continue
		}
		rep.RolledBack = append(rep.RolledBack, h.ProposalID)
		rep.Steps = append(rep.Steps, CycleStep{Step: "rollback", Target: h.ProposalID, Outcome: "đã tự quay lui"})
	}

	// 2. Propose.
	if !o.noPropose && remaining() >= o.proposeBudget+minProbeUSD {
		logf("→ tạo đề xuất (ngân sách evolver $%.2f)", o.proposeBudget)
		out, err := r.run(ctx, modelArgs([]string{"evolve", "propose", "--json", "--max", strconv.Itoa(max(o.maxProposals, 1)),
			"--budget", strconv.FormatFloat(o.proposeBudget, 'f', 2, 64)})...)
		var pr evolve.ProposeReport
		json.Unmarshal(lastJSON(out), &pr)
		rep.SpentUSD += pr.SpentUSD
		rep.GenerationUSD += pr.SpentUSD
		step := CycleStep{Step: "propose", CostUSD: pr.SpentUSD}
		var made []string
		for _, oc := range pr.Outcomes {
			if oc.Proposal != nil {
				made = append(made, oc.Proposal.ID)
			}
		}
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
	type readyP struct {
		p    *evolve.Proposal
		gain float64
	}
	var ready []readyP
	probed := 0
	for _, p := range ps {
		if !p.Quantitative {
			continue
		}
		if st, _ := proposalState(e, p); st != evolve.StatusProposed {
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
			if probed >= o.maxProposals*2 {
				break
			}
			probed++
			v, err := probe(p.ID, split)
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
			ready = append(ready, readyP{p, probeGain(ctx, db, t.BatchID)})
		}
	}

	// 4. Compose: tự áp dụng tối đa một đề xuất (lợi ích lớn nhất trước).
	sort.SliceStable(ready, func(i, j int) bool { return ready[i].gain > ready[j].gain })
	applied := false
	for _, rp := range ready {
		if ok, why := composeDecision(rp.p, cfg.Evolution.Adoption.AutoLowRisk, applied); !ok {
			rep.Ready = append(rep.Ready, rp.p.ID)
			rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: why})
			continue
		}
		res, err := evolve.Apply(ctx, evolve.ApplyOptions{K: e.k, DB: db, Ref: rp.p.ID, Auto: true, Cases: e.caseMap(),
			Reason: "tự áp dụng rủi ro thấp (bint evolve cycle " + rep.ID + ")"})
		if err != nil {
			rep.Ready = append(rep.Ready, rp.p.ID)
			rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: "không tự áp dụng được: " + err.Error()})
			continue
		}
		applied = true
		rep.AutoApplied = append(rep.AutoApplied, rp.p.ID)
		rep.Steps = append(rep.Steps, CycleStep{Step: "compose", Target: rp.p.ID, Outcome: "đã tự áp dụng, bản ghi " + res.AdoptionID})
	}
	return rep, nil
}

var errBudgetLeft = fmt.Errorf("hết ngân sách vòng")

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
func activeAdoptions(ctx context.Context, e *evolveCtx, hs []evolve.HistoryEntry) []evolve.HistoryEntry {
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
		if p, err := evolve.GetProposal(ctx, e.st.DB, h.ProposalID); err == nil && p.Status == evolve.StatusApplied {
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

// lastJSON lấy đối tượng JSON cuối trong đầu ra (bỏ dòng tiến độ phía trước nếu có).
func lastJSON(out []byte) []byte {
	out = bytes.TrimSpace(out)
	if i := bytes.Index(out, []byte("\n{")); i >= 0 && !bytes.HasPrefix(out, []byte("{")) {
		return out[i+1:]
	}
	return out
}

func printCycle(w io.Writer, r CycleReport) {
	fmt.Fprintf(w, "\nVòng %s: đã tiêu $%.2f / $%.2f (tạo ứng viên $%.2f, đối chiếu $%.2f)\n", r.ID, r.SpentUSD, r.BudgetUSD, r.GenerationUSD, r.ProbeUSD)
	for _, s := range r.Steps {
		cost := ""
		if s.CostUSD > 0 {
			cost = fmt.Sprintf(" ($%.2f)", s.CostUSD)
		}
		fmt.Fprintf(w, "  %-11s %-32s %s%s\n", s.Step, s.Target, s.Outcome, cost)
	}
	if len(r.AutoApplied) > 0 {
		fmt.Fprintf(w, "Tự áp dụng: %s (mục mới ở candidate; quay lui: bint evolve rollback <id>)\n", strings.Join(r.AutoApplied, ", "))
	}
	if len(r.Ready) > 0 {
		fmt.Fprintf(w, "Chờ người duyệt: %s (bint evolve show/apply)\n", strings.Join(r.Ready, ", "))
	}
	if len(r.AutoRejected) > 0 {
		fmt.Fprintf(w, "Tự bác bỏ (không đạt): %s\n", strings.Join(r.AutoRejected, ", "))
	}
	if len(r.RollbackSuggested) > 0 {
		fmt.Fprintf(w, "Đề xuất quay lui (hồi quy): %s\n", strings.Join(r.RollbackSuggested, ", "))
	}
	if r.StopReason != "" {
		fmt.Fprintf(w, "Dừng: %s\n", r.StopReason)
	}
}

// cycleTotals cộng các báo cáo vòng đã lưu (theo dõi chi phí tiến hoá, mục 12.8).
func cycleTotals(runtime string) (int, CycleReport) {
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
