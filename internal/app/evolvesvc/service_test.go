package evolvesvc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/spool"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

// agentEnv: các biến báo phiên agent (chép tay như internal/evolve/evolve_test.go: thêm biến vào
// app.agentEnvVars thì sửa cả ở đây).
var agentEnv = []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "GEMINI_CLI"}

func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range agentEnv {
		t.Setenv(k, "")
	}
}

func asAgent(t *testing.T) {
	t.Helper()
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
}

// fixture dựng repo đã init và không gian tiến hoá của nó.
func fixture(t *testing.T) (*Workspace, string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "internal/core/x.go": "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	off := false
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, Hooks: &off, NoSkills: true, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	e, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, root
}

// Propose và Process là thao tác chỉ dành cho người; Rollback/Apply/Reject khoá trong evolve.
func TestProposeProcessHumanOnly(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	asAgent(t)
	if _, err := Propose(ctx, e, ProposeDefaults(e.K.Config)); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("propose trong phiên agent: %v", err)
	}
	if _, err := Process(ctx, e, ProcessDefaults(e.K.Config)); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("process trong phiên agent: %v", err)
	}
	if _, err := Apply(ctx, e, ApplyOptions{Ref: "ep_x"}); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("apply trong phiên agent: %v", err)
	}
	if _, err := Reject(ctx, e, RejectOptions{Ref: "ep_x"}); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("reject trong phiên agent: %v", err)
	}
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: "ep_x"}); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("rollback trong phiên agent: %v", err)
	}

	asHuman(t)
	opts := ProposeDefaults(e.K.Config)
	opts.NoAgent = true
	res, err := Propose(ctx, e, opts)
	if err != nil || res == nil {
		t.Fatalf("người dùng chạy propose --no-agent: %v", err)
	}
}

func TestEvolveOptionsValidate(t *testing.T) {
	cfg := config.Default()
	bad := app.IsUsage
	if err := EvaluateDefaults(cfg).Validate(); !bad(err) {
		t.Errorf("thiếu ID: %v", err)
	}
	ok := EvaluateDefaults(cfg)
	ok.ID = "ep_1"
	if err := ok.Validate(); err != nil {
		t.Errorf("mặc định hợp lệ: %v", err)
	}
	for name, mod := range map[string]func(*EvaluateOptions){
		"delivery lạ":        func(o *EvaluateOptions) { o.Delivery = "x" },
		"leakage lạ":         func(o *EvaluateOptions) { o.LeakageMode = "x" },
		"split build":        func(o *EvaluateOptions) { o.Split = eval.SplitBuild },
		"repeats 0 chỉ định": func(o *EvaluateOptions) { o.Repeats, o.RepeatsExplicit = 0, true },
	} {
		o := ok
		mod(&o)
		if err := o.Validate(); !bad(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Hồi quy dùng tập đại diện; tiếp tục đợt cũ không chọn lại tác vụ.
	reg := ok
	reg.Regression, reg.Split = true, eval.SplitBuild
	if err := reg.Validate(); err != nil {
		t.Errorf("regression: %v", err)
	}
	res := ok
	res.Resume, res.Split = "eb_1", ""
	if err := res.Validate(); err != nil {
		t.Errorf("resume: %v", err)
	}
	if err := (CycleOptions{}).Validate(); err == nil || bad(err) {
		t.Errorf("cycle thiếu ngân sách phải là lỗi thường (mã thoát 1): %v", err)
	}
	if err := (ApplyOptions{}).Validate(); !bad(err) {
		t.Errorf("apply thiếu ID: %v", err)
	}
}

func TestEvolveDefaultsFollowConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Evolution.Evaluation.Repeats = 4
	cfg.Evolution.Evaluation.Delivery = eval.ArmMinimal
	cfg.Evolution.Evaluation.LeakageMode = eval.LeakEconomy
	b := 9.0
	cfg.Evolution.Evaluation.BudgetUSD = &b
	d := EvaluateDefaults(cfg)
	if d.Repeats != 4 || d.Delivery != eval.ArmMinimal || d.LeakageMode != eval.LeakEconomy || d.BudgetUSD == nil || *d.BudgetUSD != 9 || d.Split != eval.SplitValidation {
		t.Fatalf("%+v", d)
	}
	if c := CycleDefaults(cfg); c.ProposeBudget != 1 || c.MaxProposals != 2 || c.Parallel != 3 {
		t.Fatalf("%+v", c)
	}
}

// evaluate: không Confirm và không Yes ⇒ UsageError; Confirm false ⇒ ErrCancelled, không tạo đợt.
func TestEvaluateConfirmGate(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	now := time.Now()
	stamp := now.UTC().Format("2006-01-02T15:04:05.000Z")
	// Tác vụ đã duyệt và kiểm chứng; tiêu chí chọn tác vụ (phạm vi mục) có thể loại hết, nên chỉ kiểm
	// khi tới được lời hỏi: nếu không còn tác vụ nào chịu ảnh hưởng thì lỗi "không tác vụ nào" là kết quả hợp lệ.
	root := e.WS.Repo.Toplevel
	baseSHA := strings.TrimSpace(testutil.Git(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "internal/core/x.go"), []byte("package core\n\nfunc Retry() { Reset() }\n\nfunc Reset() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, root, "commit", "-q", "-am", "sửa Retry")
	commitSHA := strings.TrimSpace(testutil.Git(t, root, "rev-parse", "HEAD"))
	c := &eval.Case{Schema: eval.CaseSchemaVersion, ID: "case-1", Status: eval.StatusApproved, Split: eval.SplitValidation,
		Source: eval.Source{Base: baseSHA, Commit: commitSHA}, SourceFiles: []string{"internal/core/x.go"},
		Verification: &eval.Verification{Status: "verified", FailBefore: true, PassAfter: true}}
	if err := e.Cases.SaveCase(c); err != nil {
		t.Fatal(err)
	}
	p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: "ep_gate", Level: evolve.LevelL2, TargetType: evolve.TargetHarness, Action: evolve.ActionCreate,
		Opportunity: "harness:x", Reason: "thử", Generator: "agent:fake", Risk: evolve.RiskLow, Quantitative: true, Status: evolve.StatusProposed,
		CreatedAt: stamp, UpdatedAt: stamp, Base: []evolve.BaseVersion{}, SupportingRuns: []evolve.RunRef{},
		Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.1, Margin: 0.05},
		HarnessChanges: []evolve.HarnessChange{{Op: evolve.ActionCreate, Kind: hcomp.KindGuidance, ID: "gate",
			Draft: &evolve.HarnessDraft{Kind: hcomp.KindGuidance, ID: "gate", Scope: "global", Body: "Chạy test."}}}}
	if err := evolve.SaveProposal(ctx, e.Store.DB, p); err != nil {
		t.Fatal(err)
	}
	opts := EvaluateDefaults(e.K.Config)
	opts.ID, opts.AgentCommand, opts.Force = "ep_gate", "true", true
	budget := 5.0
	opts.BudgetUSD = &budget
	var selected EvaluateSelection
	opts.OnSelect = func(s EvaluateSelection) { selected = s }

	_, err := Evaluate(ctx, e, opts)
	if !app.IsUsage(err) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("không Confirm, không Yes phải là UsageError: %v", err)
	}
	if selected.Chosen != 1 || selected.Pool != 1 {
		t.Fatalf("OnSelect phải báo tác vụ đã chọn trước lời hỏi: %+v", selected)
	}
	var asked app.ConfirmRequest
	opts.Confirm = func(r app.ConfirmRequest) bool { asked = r; return false }
	if _, err := Evaluate(ctx, e, opts); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("từ chối xác nhận: %v", err)
	}
	if asked.Key != ConfirmEvaluateKey {
		t.Fatalf("lời hỏi: %+v", asked)
	}
	if bs, _ := eval.ListBatches(ctx, e.Store.DB, 5); len(bs) != 0 {
		t.Fatalf("từ chối xác nhận không được tạo đợt: %v", bs)
	}
}

// session ghi các sự kiện của một phiên Claude Code vào spool, như hook làm.
func session(t *testing.T, spoolDir, id string, at time.Time, rating string, modified []string) {
	t.Helper()
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: id, Payload: p}
	}
	evs := []spool.Event{ev(spool.SessionStarted, at, map[string]any{"model": "claude-opus-5-5"})}
	for _, f := range modified {
		evs = append(evs, ev(spool.FileModified, at.Add(time.Minute), map[string]any{"path": f}))
	}
	if rating != "" {
		evs = append(evs, ev(spool.RunRated, at.Add(4*time.Minute), map[string]any{"rating": rating}))
	}
	evs = append(evs, ev(spool.SessionEnded, at.Add(3*time.Minute), map[string]any{"reason": "exit"}))
	if _, err := spool.Write(spoolDir, evs...); err != nil {
		t.Fatal(err)
	}
}

// agentRunner chạy Propose/Evaluate THẬT trong tiến trình (runner mặc định) nhưng thay agent
// evolver bằng lệnh giả, để không gọi mô hình.
type agentRunner struct {
	processRunner
	command string
}

func (r agentRunner) Propose(ctx context.Context, o ProposeOptions) (*ProposeResult, error) {
	o.AgentCommand = r.command
	return r.processRunner.Propose(ctx, o)
}

func (r agentRunner) Evaluate(ctx context.Context, o EvaluateOptions) (*EvaluateResult, error) {
	o.AgentCommand = "true"
	return r.processRunner.Evaluate(ctx, o)
}

// Vòng chạy trọn trong tiến trình: bước propose gọi evolve.Propose thật (agent giả), bước probe gọi
// Evaluate thật (không có tác vụ đánh giá nên báo lỗi riêng của đề xuất và vòng đi tiếp), và báo
// cáo vòng được lưu ở <runtime>/evolve/cycles.
func TestCycleRunsInProcessWithFakeAgent(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	sp := e.K.Paths.Spool
	base := time.Now().Add(-6 * time.Hour)
	core := []string{"internal/core/x.go"}
	session(t, sp, "s1", base, "good", core)
	session(t, sp, "s2", base.Add(time.Hour), "bad", core)
	session(t, sp, "s3", base.Add(2*time.Hour), "bad", core)
	// Ingest lại để thấy các phiên vừa ghi.
	if _, err := e.WS.Ingest(ctx, e.Store, time.Now()); err != nil {
		t.Fatal(err)
	}

	answer := `{"proposals":[{"type":"warning","title":"Gọi Reset trước Retry","body":"` + "`Retry`" + ` trong internal/core/x.go cần ` + "`Reset`" + ` trước.","paths":["internal/core/**"],"evidence":["internal/core/x.go:3-4"],"expected_benefit":"bớt lượt thất bại"}]}`
	resp, _ := json.Marshal(map[string]any{"type": "result", "result": answer})
	respFile := filepath.Join(t.TempDir(), "resp.json")
	if err := os.WriteFile(respFile, resp, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stamp := now.UTC().Format("2006-01-02T15:04:05.000Z")
	pre := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: "ep_probe1", Level: "L1", TargetType: "knowledge", Action: evolve.ActionCreate,
		Opportunity: "op-pre", Reason: "dựng sẵn", Generator: "agent:fake",
		Changes: []evolve.Change{{Op: evolve.ActionCreate, ItemID: "01M74BAFEAP3EAS71818R7KQ0T", Reason: "thử",
			Draft: &evolve.Draft{Type: "warning", Title: "Dựng sẵn", Body: "Nội dung."}}},
		Risk: evolve.RiskLow, Quantitative: true, Status: evolve.StatusProposed, CreatedAt: stamp, UpdatedAt: stamp,
		Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.1, Margin: 0.05}}
	if err := evolve.SaveProposal(ctx, e.Store.DB, pre); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	rep, err := Cycle(ctx, e, CycleOptions{BudgetUSD: 10, ProposeBudget: 1, MaxProposals: 2, Parallel: 1, Log: &log,
		Runner: agentRunner{processRunner{e}, "cat " + filepath.ToSlash(respFile)}})
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	var proposed, probed bool
	for _, s := range rep.Steps {
		if s.Step == "propose" && strings.Contains(s.Outcome, "ep_") {
			proposed = true
		}
		if s.Step == "probe:"+eval.SplitValidation && s.Target == "ep_probe1" {
			probed = true
		}
	}
	if !proposed {
		t.Fatalf("bước propose phải tạo đề xuất bằng evolve.Propose thật: %+v", rep.Steps)
	}
	if !probed {
		t.Fatalf("bước probe phải chạy Evaluate thật cho đề xuất vừa tạo: %+v", rep.Steps)
	}
	if !strings.Contains(log.String(), "→ tạo đề xuất") {
		t.Fatalf("log vòng: %q", log.String())
	}
	if n, _ := CycleTotals(e.K.Paths.Runtime); n != 1 {
		t.Fatalf("báo cáo vòng phải được lưu: %d", n)
	}
}

// Vòng trong phiên agent: bước propose ghi "lỗi: ..." (không chặn cả vòng), như khi tiến trình con.
func TestCycleProposeStepReportsAgentForbidden(t *testing.T) {
	e, _ := fixture(t)
	asAgent(t)
	rep, err := Cycle(context.Background(), e, CycleOptions{BudgetUSD: 10, ProposeBudget: 1, MaxProposals: 2, Parallel: 1})
	if err != nil {
		t.Fatalf("vòng không được hỏng vì propose bị khoá: %v", err)
	}
	found := false
	for _, s := range rep.Steps {
		if s.Step == "propose" && strings.HasPrefix(s.Outcome, "lỗi:") && strings.Contains(s.Outcome, "chỉ dành cho người dùng") {
			found = true
		}
	}
	if !found {
		t.Fatalf("thiếu bước propose báo lỗi: %+v", rep.Steps)
	}
}
