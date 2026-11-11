package evolvesvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/store"
)

// partialResult dựng kết quả của một đợt dừng dở với kết luận cho trước.
func partialResult(verdict string, runErr error) *EvaluateResult {
	return &EvaluateResult{Verdict: evolve.Verdict{Verdict: verdict, Detail: "đợt dừng dở"}, RunErr: runErr}
}

func saveProposed(t *testing.T, e *Workspace, id, status string) {
	t.Helper()
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: id, Level: "L1", TargetType: "knowledge", Action: evolve.ActionCreate,
		Opportunity: "op-" + id, Reason: "dựng sẵn", Generator: "agent:fake",
		Changes: []evolve.Change{{Op: evolve.ActionCreate, ItemID: "01M74BAFEAP3EAS71818R7KQ0T", Reason: "thử",
			Draft: &evolve.Draft{Type: "warning", Title: "Dựng sẵn " + id, Body: "Nội dung."}}},
		Risk: evolve.RiskLow, Quantitative: true, Status: status, CreatedAt: stamp, UpdatedAt: stamp,
		Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.1, Margin: 0.05}}
	if err := evolve.SaveProposal(context.Background(), e.Store.DB, p); err != nil {
		t.Fatal(err)
	}
}

func stepsOf(rep CycleReport, name string) []CycleStep {
	var out []CycleStep
	for _, s := range rep.Steps {
		if s.Step == name {
			out = append(out, s)
		}
	}
	return out
}

// Bước Probe: đợt đối chiếu dừng dở vì ErrBudget/ErrQuota (kèm kết quả) vẫn cho kết luận; vòng không
// hỏng và không ghi "lỗi:" (như `evolve evaluate --json` cũ, thoát 0 khi đã có đợt).
func TestCycleProbeUsesVerdictOfPartialBatch(t *testing.T) {
	for _, runErr := range []error{eval.ErrBudget, eval.ErrQuota} {
		t.Run(runErr.Error(), func(t *testing.T) {
			e, _ := fixture(t)
			saveProposed(t, e, "ep_partial", evolve.StatusProposed)
			fr := &fakeRunner{evaluate: func(EvaluateOptions) (*EvaluateResult, error) {
				return partialResult(evolve.VerdictSupported, runErr), runErr
			}}
			rep, err := Cycle(context.Background(), e, CycleOptions{BudgetUSD: 10, NoPropose: true, MaxProposals: 1, Parallel: 1, Runner: fr})
			if err != nil {
				t.Fatalf("vòng không được hỏng vì đợt dừng dở: %v (%s)", err, rep.StopReason)
			}
			vp, tp := stepsOf(rep, "probe:"+eval.SplitValidation), stepsOf(rep, "probe:"+eval.SplitTest)
			if len(vp) != 1 || len(tp) != 1 {
				t.Fatalf("phải đối chiếu cả hai tập tiếp sau đợt dở: %+v", rep.Steps)
			}
			for _, s := range append(vp, tp...) {
				if strings.HasPrefix(s.Outcome, "lỗi") || !strings.HasPrefix(s.Outcome, evolve.VerdictSupported) {
					t.Fatalf("bước phải mang verdict của đợt dở: %+v", s)
				}
			}
		})
	}
}

// Kết luận not_supported của đợt dở vẫn được dùng (dừng thử tập kế của đề xuất đó, không phải lỗi).
func TestCycleProbeNotSupportedFromPartialBatch(t *testing.T) {
	e, _ := fixture(t)
	saveProposed(t, e, "ep_partial", evolve.StatusProposed)
	fr := &fakeRunner{evaluate: func(EvaluateOptions) (*EvaluateResult, error) {
		return partialResult(evolve.VerdictNotSupported, eval.ErrBudget), eval.ErrBudget
	}}
	rep, err := Cycle(context.Background(), e, CycleOptions{BudgetUSD: 10, NoPropose: true, MaxProposals: 1, Parallel: 1, Runner: fr})
	if err != nil {
		t.Fatal(err)
	}
	ps := stepsOf(rep, "probe:"+eval.SplitValidation)
	if len(ps) != 1 || !strings.HasPrefix(ps[0].Outcome, evolve.VerdictNotSupported) {
		t.Fatalf("%+v", rep.Steps)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("not_supported thì không thử tập test: %v", fr.calls)
	}
}

// Bị huỷ giữa đợt vẫn dừng vòng bằng lỗi (khác với hết ngân sách/hạn mức).
func TestCycleProbeCancelledStillStops(t *testing.T) {
	e, _ := fixture(t)
	saveProposed(t, e, "ep_partial", evolve.StatusProposed)
	ctx, cancel := context.WithCancel(context.Background())
	fr := &fakeRunner{evaluate: func(EvaluateOptions) (*EvaluateResult, error) {
		cancel()
		return partialResult(evolve.VerdictSupported, context.Canceled), context.Canceled
	}}
	_, err := Cycle(ctx, e, CycleOptions{BudgetUSD: 10, NoPropose: true, MaxProposals: 1, Parallel: 1, Runner: fr})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("huỷ phải dừng vòng: %v", err)
	}
}

// Bước hồi quy: đề xuất đã áp dụng với mô hình khác; đợt hồi quy dừng dở (ErrBudget) vẫn cho kết luận
// not_supported → vòng đề xuất quay lui (người áp dụng) chứ không thoát lỗi mã 1.
func TestCycleRegressionUsesVerdictOfPartialBatch(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	saveProposed(t, e, "ep_applied", evolve.StatusApplied)
	old, cur := "model-cu", "model-moi"
	e.K.Config.Eval.Agent.Model = &cur
	_, err := e.Store.DB.ExecContext(ctx, `INSERT INTO evolution_adoptions (adoption_id, proposal_id, action, actor, verified,
		evaluation_id, harness, model, files, reason, created_at) VALUES ('ad_1', 'ep_applied', 'apply', 'nguoi', 1, NULL, NULL, ?, '[]', 'thử', ?)`,
		old, store.FormatTime(time.Now().Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{evaluate: func(o EvaluateOptions) (*EvaluateResult, error) {
		if !o.Regression {
			t.Errorf("chỉ mong đợt hồi quy: %+v", o)
		}
		return partialResult(evolve.VerdictNotSupported, eval.ErrBudget), eval.ErrBudget
	}}
	rep, err := Cycle(ctx, e, CycleOptions{BudgetUSD: 10, NoPropose: true, MaxProposals: 1, Parallel: 1, Runner: fr})
	if err != nil {
		t.Fatalf("hết ngân sách giữa đợt hồi quy không được làm vòng hỏng: %v (%s)", err, rep.StopReason)
	}
	if rs := stepsOf(rep, "regression"); len(rs) != 1 || !strings.HasPrefix(rs[0].Outcome, evolve.VerdictNotSupported) {
		t.Fatalf("bước hồi quy phải mang verdict của đợt dở: %+v", rep.Steps)
	}
	if len(rep.RollbackSuggested) != 1 || rep.RollbackSuggested[0] != "ep_applied" {
		t.Fatalf("hồi quy phải dẫn tới đề xuất quay lui: %+v", rep)
	}
}
