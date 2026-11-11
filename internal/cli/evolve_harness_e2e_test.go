package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/testutil"
)

// Đề xuất quy trình đi qua CLI: show/list, đối chiếu chỉ với delivery bint, áp dụng bởi người (thành
// phần vào ở candidate), agent bị chặn, quay lui trả lại cây tệp.
func TestEvolveHarnessProposalEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "internal/core/x.go": "package core\n\nfunc X() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	ctx := context.Background()
	k, err := app.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := k.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: "ep_harness1", Level: evolve.LevelL2, TargetType: evolve.TargetHarness, Action: evolve.ActionCreate,
		Opportunity: "harness:signal:no_test", Reason: "3 tác vụ validation thất bại không chạy test", Generator: "agent:fake", Risk: evolve.RiskLow,
		RiskReasons: []string{"create guardrail remind (low)"}, Status: evolve.StatusProposed, Quantitative: false,
		CreatedAt: "2026-11-11T10:00:00.000Z", UpdatedAt: "2026-11-11T10:00:00.000Z", Base: []evolve.BaseVersion{}, SupportingRuns: []evolve.RunRef{},
		Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.1, Margin: 0.05, Hypothesis: "tăng tỷ lệ đạt"},
		HarnessChanges: []evolve.HarnessChange{{Op: evolve.ActionCreate, Kind: hcomp.KindGuardrail, ID: "test-before-stop", Reason: "agent bỏ test",
			Draft: &evolve.HarnessDraft{Kind: hcomp.KindGuardrail, ID: "test-before-stop", Scope: "global", On: hcomp.OnStop,
				When: hcomp.When{Edited: "internal/**"}, Unless: hcomp.Unless{Ran: hcomp.RanTest}, Action: hcomp.ActRemind, Message: "Chưa chạy test."}}},
		Pair: &evolve.Pair{Key: "harness:signal:no_test", Variant: evolve.PairGlobal, Sibling: "ep_harness2", Area: "internal/core/**"}}
	if err := evolve.SaveProposal(ctx, st.DB, p); err != nil {
		t.Fatal(err)
	}
	st.Close()

	out, code := run(t, root, "", "evolve", "show", "ep_harness1")
	for _, want := range []string{"cấp L2, đối tượng harness", "[create] guardrail test-before-stop", "on=stop when=edited:internal/** unless=ran:test action=remind",
		"Chưa chạy test.", "bản chung (anh em ep_harness2, vùng internal/core/**)", "rủi ro low"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("show thiếu %q (code %d):\n%s", want, code, out)
		}
	}
	if out, code = run(t, root, "", "evolve", "list"); code != 0 || !strings.Contains(out, "ep_harness1") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	// Quy trình chỉ đo được ở nhánh bint.
	if out, code = run(t, root, "", "evolve", "evaluate", "ep_harness1", "--delivery", "minimal", "--yes"); code == 0 || !strings.Contains(out, "chỉ đo được với --delivery bint") {
		t.Fatalf("evaluate minimal: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "evolve", "evaluate", "ep_harness1", "--split", "test", "--yes"); code == 0 || !strings.Contains(out, "không tác vụ nào") {
		t.Fatalf("evaluate không có tác vụ: %d\n%s", code, out)
	}

	// Agent không áp dụng được; người áp dụng → candidate.
	asAgent(t)
	if out, code = run(t, root, "", "evolve", "apply", "ep_harness1", "--unverified"); code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("agent apply: %d\n%s", code, out)
	}
	asHuman(t)
	out, code = run(t, root, "", "evolve", "apply", "ep_harness1", "--unverified", "-r", "đã đọc")
	if code != 0 || !strings.Contains(out, "+ .bint/harness/guardrails/test-before-stop.yml") || !strings.Contains(out, "ở candidate") {
		t.Fatalf("apply: %d\n%s", code, out)
	}
	if out, _ = run(t, root, "", "harness", "component", "list"); !strings.Contains(out, "test-before-stop") || !strings.Contains(out, "candidate") || !strings.Contains(out, "agent") {
		t.Fatalf("component list:\n%s", out)
	}
	// Candidate chưa có hiệu lực: hook không nhắc.
	if out, _ = run(t, root, "", "harness", "render"); strings.Contains(out, "test-before-stop [") {
		t.Fatalf("candidate chưa có hiệu lực:\n%s", out)
	}
	if out, code = run(t, root, "", "evolve", "rollback", "ep_harness1"); code != 0 {
		t.Fatalf("rollback: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".bint", "harness", "guardrails", "test-before-stop.yml")); err == nil {
		t.Fatal("quay lui phải xoá tệp")
	}
}
