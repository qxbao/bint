package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/testutil"
)

// pairCycle dựng vòng với một cặp chung/riêng cùng đã đạt validation và test (độc lập). Hiệu ứng
// của hai bản trên tác vụ vùng core do đợt test của từng bản quyết định.
func pairCycle(t *testing.T, globalCore, domainCore float64) (*evolveCtx, CycleReport, map[string]*evolve.Proposal) {
	t.Helper()
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
	t.Cleanup(func() { st.Close() })
	casesDir := t.TempDir()
	src := eval.Source{Base: "aaaa", Commit: "bbbb"}
	for _, c := range []*eval.Case{{Schema: eval.CaseSchemaVersion, ID: "core-1", Status: eval.StatusApproved, Split: eval.SplitTest, Source: src, SourceFiles: []string{"internal/core/x.go"}},
		{Schema: eval.CaseSchemaVersion, ID: "api-1", Status: eval.StatusApproved, Split: eval.SplitTest, Source: src, SourceFiles: []string{"internal/api/h.go"}}} {
		b, _ := yaml.Marshal(c)
		if err := os.WriteFile(filepath.Join(casesDir, c.ID+".yml"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := &evolveCtx{evalCtx: &evalCtx{st: st, cases: eval.Store{CasesDir: casesDir}}, k: k}
	nowT := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	stamp := nowT.Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")

	mk := func(id, scope, variant, sibling string) *evolve.Proposal {
		p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: id, Level: evolve.LevelL2, TargetType: evolve.TargetHarness, Action: evolve.ActionCreate,
			Opportunity: "harness:signal:no_test", Reason: "thử", Generator: "agent:fake", Risk: evolve.RiskLow, Quantitative: true,
			Status: evolve.StatusProposed, CreatedAt: stamp, UpdatedAt: stamp, Base: []evolve.BaseVersion{}, SupportingRuns: []evolve.RunRef{},
			Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.1, Margin: 0.05},
			HarnessChanges: []evolve.HarnessChange{{Op: evolve.ActionCreate, Kind: hcomp.KindGuidance, ID: id[3:],
				Draft: &evolve.HarnessDraft{Kind: hcomp.KindGuidance, ID: id[3:], Scope: scope, Body: "Chạy test."}}},
			Pair: &evolve.Pair{Key: "harness:signal:no_test", Variant: variant, Sibling: sibling, Area: "internal/core/**"}}
		if err := evolve.SaveProposal(ctx, st.DB, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	g := mk("ep_global", "global", evolve.PairGlobal, "ep_domain")
	d := mk("ep_domain", "domain:internal/core/**", evolve.PairDomain, "ep_global")

	attempt := 0
	batch := func(id string, coreEffect float64) {
		if _, err := st.DB.Exec(`INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
			prompt_version, seed, started_at) VALUES (?, 'completed', 'claude', 'strict', '["base","candidate"]', 10, '[]', 1, 1, ?)`, id, stamp); err != nil {
			t.Fatal(err)
		}
		// core-1: mười lượt mỗi nhánh; base đạt 0, candidate đạt coreEffect×10. api-1: không đổi.
		add := func(c, arm string, pass bool) {
			attempt++
			if _, err := st.DB.Exec(`INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, arm, repeat_index, status,
				harness, started_at, test_passed) VALUES (?, ?, ?, 'fp', ?, ?, 'passed', 'claude', ?, ?)`,
				fmt.Sprintf("ea-%d", attempt), id, c, arm, attempt, stamp, map[bool]int{true: 1, false: 0}[pass]); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 10; i++ {
			add("core-1", eval.ArmBase, false)
			add("core-1", eval.ArmCandidate, float64(i) < coreEffect*10)
			add("api-1", eval.ArmBase, true)
			add("api-1", eval.ArmCandidate, true)
		}
	}
	batch("eb-val-g", 0.5)
	batch("eb-test-g", globalCore)
	batch("eb-val-d", 0.5)
	batch("eb-test-d", domainCore)
	yes := true
	for _, ev := range []struct{ p, split, b string }{{"ep_global", "validation", "eb-val-g"}, {"ep_global", "test", "eb-test-g"},
		{"ep_domain", "validation", "eb-val-d"}, {"ep_domain", "test", "eb-test-d"}} {
		rec := &evolve.Evaluation{ProposalID: ev.p, BatchID: ev.b, Split: ev.split, BaseKey: map[string]*evolve.Proposal{"ep_global": g, "ep_domain": d}[ev.p].BaseKey(),
			Delivery: "bint", CaseIDs: []string{"core-1", "api-1"}, CreatedAt: stamp}
		if err := evolve.RecordEvaluation(ctx, st.DB, rec); err != nil {
			t.Fatal(err)
		}
		if err := evolve.SetVerdict(ctx, st.DB, rec.ID, evolve.Verdict{Verdict: evolve.VerdictSupported}, &yes); err != nil {
			t.Fatal(err)
		}
	}

	var calls []string
	r := &cycleRunner{log: io.Discard, call: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte(`{}`), nil
	}}
	rep, err := runCycle(ctx, e, r, cycleOpts{budget: 10, proposeBudget: 1, maxProposals: 2, parallel: 1, noPropose: true}, func() time.Time { return nowT })
	if err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "evaluate") {
			t.Fatalf("đã đối chiếu đủ trên cả hai tập, không đo lại: %v", calls)
		}
	}
	got := map[string]*evolve.Proposal{}
	for _, id := range []string{"ep_global", "ep_domain"} {
		if got[id], err = evolve.GetProposal(ctx, st.DB, id); err != nil {
			t.Fatal(err)
		}
	}
	return e, rep, got
}

// Cả hai nửa cặp đạt: bản chung kém bản riêng trên vùng quá margin → giữ bản riêng, bác bỏ bản
// chung; chỉ bản được giữ chờ người duyệt; không bao giờ cả hai.
func TestCyclePairKeepsDomainWhenGlobalWorseOnArea(t *testing.T) {
	_, rep, ps := pairCycle(t, 0.1, 0.9)
	if ps["ep_global"].Status != evolve.StatusRejected || !strings.Contains(ps["ep_global"].StatusReason, "giữ bản riêng") {
		t.Fatalf("bản chung phải bị bác bỏ: %+v", ps["ep_global"])
	}
	if ps["ep_domain"].Status != evolve.StatusProposed {
		t.Fatalf("bản riêng giữ lại: %+v", ps["ep_domain"])
	}
	if strings.Join(rep.Ready, ",") != "ep_domain" || strings.Join(rep.AutoRejected, ",") != "ep_global" || len(rep.AutoApplied) != 0 {
		t.Fatalf("báo cáo vòng: ready=%v rejected=%v applied=%v steps=%+v", rep.Ready, rep.AutoRejected, rep.AutoApplied, rep.Steps)
	}
	found := false
	for _, s := range rep.Steps {
		if s.Step == "compose-pair" && s.Target == "ep_domain" && strings.Contains(s.Outcome, "giữ bản domain") {
			found = true
		}
	}
	if !found {
		t.Fatalf("thiếu bước compose-pair: %+v", rep.Steps)
	}
}

// Bản chung không kém bản riêng quá margin → giữ bản chung.
func TestCyclePairKeepsGlobalWhenNotInferior(t *testing.T) {
	_, rep, ps := pairCycle(t, 0.9, 0.9)
	if ps["ep_domain"].Status != evolve.StatusRejected || ps["ep_global"].Status != evolve.StatusProposed {
		t.Fatalf("giữ bản chung: global=%s domain=%s", ps["ep_global"].Status, ps["ep_domain"].Status)
	}
	if strings.Join(rep.Ready, ",") != "ep_global" {
		t.Fatalf("ready=%v", rep.Ready)
	}
}
