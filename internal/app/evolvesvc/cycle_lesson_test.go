package evolvesvc

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// TestCycleProposesAndProbesLesson (TSK-9): đề xuất bài học (create lesson, định lượng) đi qua vòng
// `bint evolve cycle`: bước propose nhận đề xuất, và bước probe đối chiếu trên validation cho đúng
// đề xuất đó. Lệnh con được thay bằng hàm giả; không gọi mô hình.
func TestCycleProposesAndProbesLesson(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "internal/core/x.go": "package core\n\nfunc X() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	off := false
	if _, err := app.Init(ctx, app.InitOptions{Dir: root, Hooks: &off, NoSkills: true, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := app.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := k.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := &Workspace{EvalWorkspace: &app.EvalWorkspace{WS: k.Workspace, Store: st, Cases: eval.Store{}}, K: k}

	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: "ep_lesson1", Level: "L1", TargetType: "knowledge",
		Action: evolve.ActionCreate, Opportunity: "lesson:signal:no_test", Reason: "nhóm thử", Generator: "agent:fake",
		Changes: []evolve.Change{{Op: evolve.ActionCreate, ItemID: "01M74BAFEAP3EAS71818R7KQ0T", Reason: "thử",
			Draft: &evolve.Draft{Type: knowledge.TypeLesson, Title: "Chạy test trước khi kết thúc", Body: "Khi: sửa mã\nSai: bỏ test\nNên: chạy test",
				Scope: knowledge.Scope{}, Lesson: &evolve.LessonMeta{Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1", "ea_2", "ea_3"}}}}},
		Risk: evolve.RiskLow, Quantitative: true, Status: evolve.StatusProposed,
		CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	if err := evolve.SaveProposal(ctx, st.DB, p); err != nil {
		t.Fatal(err)
	}

	r := &fakeRunner{
		propose: func(ProposeOptions) (*ProposeResult, error) {
			return &ProposeResult{ProposeReport: evolve.ProposeReport{Outcomes: []evolve.ProposeOutcome{
				{Opportunity: "op-x", Proposal: &evolve.Proposal{ID: "ep_lesson1"}}}}}, nil
		},
		evaluate: func(EvaluateOptions) (*EvaluateResult, error) {
			return &EvaluateResult{Verdict: evolve.Verdict{Verdict: "inconclusive"}}, nil
		},
	}
	rep, err := runCycle(ctx, e, CycleOptions{BudgetUSD: 10, ProposeBudget: 1, MaxProposals: 2, Parallel: 1, Runner: r}, func() time.Time { return now })
	calls := r.calls
	if err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	var proposed, probed bool
	for _, s := range rep.Steps {
		if s.Step == "propose" && strings.Contains(s.Outcome, "ep_lesson1") {
			proposed = true
		}
		if s.Step == "probe:"+eval.SplitValidation && s.Target == "ep_lesson1" {
			probed = true
		}
	}
	if !proposed || !probed {
		t.Fatalf("vòng phải đưa đề xuất bài học qua propose rồi probe validation: steps=%+v calls=%v", rep.Steps, calls)
	}
}
