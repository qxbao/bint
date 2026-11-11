package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// asHuman xoá mọi biến môi trường báo phiên agent: go test có thể đang chạy trong phiên agent
// (CLAUDECODE=1) nên kiểm thử đóng vai người phải đặt rỗng từng biến.
func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range agentEnvVars {
		t.Setenv(k, "")
	}
}

// asAgent đặt một biến agent để thao tác chỉ-dành-cho-người bị khoá.
func asAgent(t *testing.T) {
	t.Helper()
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
}

func TestUsageErrorWrapsAndIsDetected(t *testing.T) {
	base := errors.New("sai")
	err := fmt.Errorf("bọc: %w", UsageError{Err: base})
	if !IsUsage(err) || !errors.Is(err, base) {
		t.Fatalf("IsUsage/Unwrap: %v", err)
	}
	if IsUsage(base) || IsUsage(nil) {
		t.Fatal("lỗi thường không phải UsageError")
	}
	if got := Usagef("cần %s", "x"); got.Error() != "cần x" || !IsUsage(got) {
		t.Fatalf("%v", got)
	}
}

func TestConfirmOrFail(t *testing.T) {
	req := ConfirmRequest{Key: "k", Text: "hỏi?"}
	yesFn := func(ConfirmRequest) bool { return true }
	noFn := func(ConfirmRequest) bool { return false }
	asked := 0
	counting := func(ConfirmRequest) bool { asked++; return true }
	cases := []struct {
		name    string
		yes     bool
		confirm ConfirmFunc
		check   func(error) bool
	}{
		{"yes bỏ qua lời hỏi", true, noFn, func(e error) bool { return e == nil }},
		{"đồng ý", false, yesFn, func(e error) bool { return e == nil }},
		{"từ chối", false, noFn, func(e error) bool { return errors.Is(e, ErrCancelled) }},
		{"không ai trả lời được", false, nil, func(e error) bool { return IsUsage(e) }},
	}
	for _, tc := range cases {
		if err := ConfirmOrFail(tc.yes, tc.confirm, req); !tc.check(err) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if err := ConfirmOrFail(true, counting, req); err != nil || asked != 0 {
		t.Fatalf("yes không được hỏi: asked=%d err=%v", asked, err)
	}
}

func TestReviewQueueOptionsValidate(t *testing.T) {
	if err := ReviewQueueDefaults().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ReviewQueueOptions{Statuses: []string{"candidate", "nhảm"}}).Validate(); !IsUsage(err) {
		t.Fatalf("trạng thái lạ phải là UsageError: %v", err)
	}
}

// Duyệt theo lô chỉ dành cho người: trong phiên agent bị khoá trước cả khi mở kho.
func TestReviewQueueHumanOnly(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	ctx := context.Background()
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeWarning, Title: "Cẩn thận A", Body: "A chạy trên luồng chính.",
		Paths: []string{"a.go"}, Evidence: []string{"a.go:3"}, Origin: knowledge.OriginAgent}); err != nil {
		t.Fatal(err)
	}

	asAgent(t)
	if _, err := ReviewQueue(ctx, root, ReviewQueueDefaults()); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("agent phải bị khoá: %v", err)
	}
	// Khoá xảy ra trước khi mở kho: thư mục không phải repository vẫn cho ErrAgentForbidden.
	if _, err := ReviewQueue(ctx, t.TempDir(), ReviewQueueDefaults()); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("kiểm tra chỉ-dành-cho-người phải ở trước: %v", err)
	}

	asHuman(t)
	rs, err := ReviewQueue(ctx, root, ReviewQueueDefaults())
	if err != nil || len(rs.Pending) != 1 {
		t.Fatalf("người duyệt phải thấy 1 mục chờ: %v %+v", err, rs)
	}
	if rs, err := ReviewQueue(ctx, root, ReviewQueueOptions{Statuses: []string{knowledge.StatusVerified}}); err != nil || len(rs.Pending) != 0 {
		t.Fatalf("lọc theo trạng thái: %v %+v", err, rs)
	}
	if _, err := ReviewQueue(ctx, root, ReviewQueueOptions{Statuses: []string{"nhảm"}}); !IsUsage(err) {
		t.Fatalf("Validate phải chạy: %v", err)
	}
}

// Confirm trả false thì bootstrap publish không ghi AGENTS.md; trả true thì ghi.
func TestPublishConfirmDeclinedWritesNothing(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	ctx := context.Background()
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeRule, Title: "Luật A", Body: "Luôn gọi A.", Paths: []string{"a.go"},
		Evidence: []string{"a.go:3"}, Origin: knowledge.OriginHuman})
	if err != nil || !res.Saved {
		t.Fatalf("add: %v %+v", err, res)
	}
	agents := filepath.Join(root, "AGENTS.md")
	before, _ := os.ReadFile(agents)

	var got ConfirmRequest
	rep, err := PublishAgents(ctx, PublishOptions{Dir: root, Confirm: func(r ConfirmRequest) bool { got = r; return false }})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Written || got.Key != ConfirmPublishKey || got.Detail == "" || got.Detail != rep.Diff {
		t.Fatalf("lời hỏi phải mang bản thay đổi và không ghi: written=%v req=%+v", rep.Written, got)
	}
	after, _ := os.ReadFile(agents)
	if string(before) != string(after) {
		t.Fatalf("AGENTS.md bị ghi dù từ chối:\n%s", after)
	}
	if _, err := PublishAgents(ctx, PublishOptions{}); !IsUsage(err) {
		t.Fatalf("thiếu thư mục phải là UsageError: %v", err)
	}
	// Không Confirm và không Yes: lỗi dùng sai, không ghi; DryRun thì không cần; Yes ghi luôn.
	if _, err := PublishAgents(ctx, PublishOptions{Dir: root}); !IsUsage(err) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("không Confirm/Yes phải là UsageError cần --yes: %v", err)
	}
	if after, _ := os.ReadFile(agents); string(before) != string(after) {
		t.Fatal("AGENTS.md bị ghi dù thiếu xác nhận")
	}
	if rep, err := PublishAgents(ctx, PublishOptions{Dir: root, DryRun: true}); err != nil || rep.Written || !rep.Changed {
		t.Fatalf("DryRun không cần xác nhận và không ghi: %v %+v", err, rep)
	}
	rep, err = PublishAgents(ctx, PublishOptions{Dir: root, Confirm: func(ConfirmRequest) bool { return true }})
	if err != nil || !rep.Written {
		t.Fatalf("đồng ý thì phải ghi: %v %+v", err, rep)
	}
}

func TestEvalOptionsValidate(t *testing.T) {
	cfg := config.Default()
	bad := func(err error) bool { return IsUsage(err) }
	if err := EvalVerifyDefaults(cfg).Validate(); !bad(err) {
		t.Errorf("verify không ID, không --all: %v", err)
	}
	if err := (EvalVerifyOptions{All: true, IDs: []string{"x"}}).Validate(); !bad(err) {
		t.Errorf("verify --all kèm ID: %v", err)
	}
	if err := (EvalVerifyOptions{IDs: []string{"x"}}).Validate(); err != nil {
		t.Errorf("verify ID: %v", err)
	}
	if err := EvalSpecDefaults(cfg).Validate(); !bad(err) {
		t.Errorf("spec: %v", err)
	}
	if err := (EvalSpecOptions{All: true, IDs: []string{"x"}}).Validate(); !bad(err) {
		t.Errorf("spec --all kèm ID: %v", err)
	}
	if err := (EvalApproveOptions{IDs: []string{"x"}, Split: "nhảm"}).Validate(); !bad(err) {
		t.Errorf("approve split lạ: %v", err)
	}
	if err := (EvalApproveOptions{IDs: []string{"x"}, Split: eval.SplitTest}).Validate(); err != nil {
		t.Errorf("approve split test: %v", err)
	}
	run := EvalRunDefaults(cfg)
	if err := run.Validate(); err != nil {
		t.Errorf("run mặc định: %v", err)
	}
	for name, mod := range map[string]func(*EvalRunOptions){
		"repeats 0":  func(o *EvalRunOptions) { o.Repeats = 0 },
		"leakage lạ": func(o *EvalRunOptions) { o.LeakageMode = "x" },
		"split lạ":   func(o *EvalRunOptions) { o.Split = "x" },
	} {
		o := EvalRunDefaults(cfg)
		mod(&o)
		if err := o.Validate(); !bad(err) {
			t.Errorf("run %s: %v", name, err)
		}
	}
	// Tiếp tục đợt cũ: không kiểm các tham số chọn tác vụ.
	o := EvalRunDefaults(cfg)
	o.Resume, o.Repeats = "eb_1", 0
	if err := o.Validate(); err != nil {
		t.Errorf("run --resume: %v", err)
	}
}

func TestEvalDefaultsFollowConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Eval.Repeats = 7
	cfg.Eval.Arms = []string{"none"}
	cfg.Eval.LeakageMode = eval.LeakEconomy
	budget := 12.5
	cfg.Eval.BudgetUSD = &budget
	o := EvalRunDefaults(cfg)
	if o.Repeats != 7 || len(o.Arms) != 1 || o.LeakageMode != eval.LeakEconomy || o.BudgetUSD == nil || *o.BudgetUSD != 12.5 || o.Harness != cfg.Eval.Harness {
		t.Fatalf("%+v", o)
	}
	if e := EvalEstimateDefaults(cfg); e.Repeats != 7 || e.Effect != 0.10 || e.Harness != "claude" {
		t.Fatalf("%+v", e)
	}
	if b := EvalBootstrapDefaults(cfg); b.BudgetUSD == nil || *b.BudgetUSD != 12.5 {
		t.Fatalf("%+v", b)
	}
	if r := EvalRejectDefaults(cfg); r.Reason != "người duyệt loại" {
		t.Fatalf("%+v", r)
	}
}

// evalFixture dựng repo đã init kèm một tác vụ đánh giá đã duyệt.
func evalFixture(t *testing.T) (*EvalWorkspace, string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	e, err := OpenEval(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	c := &eval.Case{Schema: eval.CaseSchemaVersion, ID: "case-1", Status: eval.StatusApproved, Split: eval.SplitValidation,
		Source: eval.Source{Base: "aaaa", Commit: "bbbb"}, SourceFiles: []string{"a.go"}}
	if err := e.Cases.SaveCase(c); err != nil {
		t.Fatal(err)
	}
	return e, root
}

// Confirm trả false: eval run không tạo đợt nào; không Confirm và không Yes: UsageError.
func TestEvalRunConfirmDeclinedCreatesNoBatch(t *testing.T) {
	e, _ := evalFixture(t)
	ctx := context.Background()
	opts := EvalRunDefaults(e.WS.Config)
	opts.Arms, opts.ArmsExplicit = []string{eval.ArmNone}, true
	opts.AgentCommand = "true"
	opts.Repeats = 1

	if _, err := EvalRun(ctx, e, opts); !IsUsage(err) {
		t.Fatalf("không Confirm, không Yes: %v", err)
	}
	var asked ConfirmRequest
	opts.Confirm = func(r ConfirmRequest) bool { asked = r; return false }
	if _, err := EvalRun(ctx, e, opts); !errors.Is(err, ErrCancelled) {
		t.Fatalf("từ chối phải là ErrCancelled: %v", err)
	}
	if asked.Key != ConfirmEvalRunKey || asked.Text == "" {
		t.Fatalf("lời hỏi: %+v", asked)
	}
	bs, err := eval.ListBatches(ctx, e.Store.DB, 10)
	if err != nil || len(bs) != 0 {
		t.Fatalf("không được tạo đợt: %v %v", err, bs)
	}
	// Tiến độ ước tính được báo trước lời hỏi.
	var estimated bool
	opts.OnEstimate = func(eval.Estimate, []string, int, *float64) { estimated = true }
	EvalRun(ctx, e, opts)
	if !estimated {
		t.Fatal("OnEstimate phải chạy trước lời hỏi")
	}
}

// eval bootstrap: Confirm false thì không dựng gì; không Confirm và không Yes: UsageError.
func TestEvalBootstrapConfirmDeclined(t *testing.T) {
	e, _ := evalFixture(t)
	ctx := context.Background()
	opts := EvalBootstrapDefaults(e.WS.Config)
	opts.AgentCommand = "true"
	if _, err := EvalBootstrap(ctx, e, opts); !IsUsage(err) {
		t.Fatalf("không Confirm, không Yes: %v", err)
	}
	opts.Confirm = func(ConfirmRequest) bool { return false }
	res, err := EvalBootstrap(ctx, e, opts)
	if !errors.Is(err, ErrCancelled) || res.Todo != 1 {
		t.Fatalf("từ chối: %v %+v", err, res)
	}
	if _, _, ok, _ := eval.ReadSnapshot(e.Env.WorkRoot, "aaaa"); ok {
		t.Fatal("không được dựng ảnh chụp khi từ chối")
	}
}

func TestEvalSelectionPick(t *testing.T) {
	e, _ := evalFixture(t)
	if cs, err := (EvalSelection{}).Pick(e); err != nil || len(cs) != 1 {
		t.Fatalf("%v %v", err, cs)
	}
	if _, err := (EvalSelection{Split: eval.SplitTest}).Pick(e); err == nil {
		t.Fatal("tập test rỗng phải báo không có tác vụ")
	}
	if _, err := (EvalSelection{CaseIDs: []string{"case-1"}, Split: "nhảm"}).Pick(e); !IsUsage(err) {
		t.Fatalf("split lạ: %v", err)
	}
}

// OnPlan chạy trước lời hỏi xác nhận (đợt mới: đúng tác vụ, nhánh, lần lặp; chưa có Done); trả không thì không có đợt.
func TestEvalRunOnPlanBeforeConfirm(t *testing.T) {
	e, _ := evalFixture(t)
	ctx := context.Background()
	opts := EvalRunDefaults(e.WS.Config)
	opts.Arms, opts.ArmsExplicit = []string{eval.ArmNone}, true
	opts.AgentCommand = "true"
	opts.Repeats = 2
	var order []string
	var plan EvalRunPlan
	opts.OnPlan = func(p EvalRunPlan) { order, plan = append(order, "plan"), p }
	opts.Confirm = func(ConfirmRequest) bool { order = append(order, "confirm"); return false }
	if _, err := EvalRun(ctx, e, opts); !errors.Is(err, ErrCancelled) {
		t.Fatalf("từ chối: %v", err)
	}
	if len(order) != 2 || order[0] != "plan" || order[1] != "confirm" {
		t.Fatalf("thứ tự: %v", order)
	}
	if len(plan.CaseIDs) != 1 || plan.CaseIDs[0] != "case-1" || len(plan.Arms) != 1 || plan.Arms[0] != eval.ArmNone || plan.Repeats != 2 ||
		plan.BatchID != "" || len(plan.Done) != 0 {
		t.Fatalf("kế hoạch: %+v", plan)
	}
	if bs, err := eval.ListBatches(ctx, e.Store.DB, 10); err != nil || len(bs) != 0 {
		t.Fatalf("không được tạo đợt: %v %v", err, bs)
	}
}

// Tiếp tục đợt cũ: OnPlan báo tác vụ/nhánh/lần lặp của đợt và các lượt đã có kết quả (bỏ lượt dở và lỗi hạ tầng).
func TestEvalRunOnPlanResumeReportsDone(t *testing.T) {
	e, _ := evalFixture(t)
	ctx := context.Background()
	db := e.Store.DB
	if _, err := db.Exec(`INSERT INTO eval_batches(batch_id, status, harness, leakage_mode, arms, repeats, case_ids, prompt_version, seed, started_at, notes)
		VALUES ('eb_x','cancelled','command','economy','["none"]',3,'["case-1"]',1,1,'2026-01-01T00:00:00Z','[]')`); err != nil {
		t.Fatal(err)
	}
	ins := func(id string, rep int, status, errMsg string) {
		if _, err := db.Exec(`INSERT INTO evaluation_runs(attempt_id, batch_id, case_id, case_fingerprint, arm, repeat_index, status, harness, started_at, error)
			VALUES (?, 'eb_x', 'case-1', 'f', 'none', ?, ?, 'command', '2026-01-01T00:00:00Z', ?)`, id, rep, status, errMsg); err != nil {
			t.Fatal(err)
		}
	}
	ins("a1", 1, "passed", "")
	ins("a2", 2, "running", "")
	ins("a3", 3, "error", eval.InfraErrorPrefix+"hết hạn mức")
	opts := EvalRunDefaults(e.WS.Config)
	opts.Resume, opts.AgentCommand = "eb_x", "true"
	var plan EvalRunPlan
	called := 0
	opts.OnPlan = func(p EvalRunPlan) { called++; plan = p }
	opts.Confirm = func(ConfirmRequest) bool { return false }
	if _, err := EvalRun(ctx, e, opts); !errors.Is(err, ErrCancelled) {
		t.Fatalf("từ chối: %v", err)
	}
	if called != 1 || plan.BatchID != "eb_x" || plan.Repeats != 3 || len(plan.CaseIDs) != 1 || len(plan.Arms) != 1 {
		t.Fatalf("kế hoạch: %d %+v", called, plan)
	}
	if len(plan.Done) != 1 || plan.Done[0].Repeat != 1 || plan.Done[0].Status != "passed" {
		t.Fatalf("Done: %+v", plan.Done)
	}
}
