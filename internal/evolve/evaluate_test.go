package evolve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/store"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

func commitFiles(t *testing.T, root, msg string, files map[string]string) string {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", msg)
	return testutil.Git(t, root, "rev-parse", "HEAD")
}

// TestEvaluateCreateProposal: đề xuất tạo tri thức được đối chiếu base/candidate trên
// tác vụ thật; agent giả chỉ giải được khi tri thức đó có trong AGENTS.md.
func TestEvaluateCreateProposal(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("cần sh")
	}
	ctx := context.Background()
	testutil.Isolate(t)
	asHuman(t)
	add := "add() { echo $(($1 + $2)); }\n"
	root := testutil.NewRepo(t, map[string]string{
		"lib/calc.sh":       add,
		"tests/run.sh":      "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\n",
		"tests/test_add.sh": ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ]\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	fns := map[string]string{
		"mul": "mul() { echo $(($1 * $2)); }\n",
		"sub": "sub() { echo $(($1 - $2)); }\n",
		"neg": "neg() { echo $((0 - $1)); }\n",
	}
	tests := map[string]string{"mul": "[ \"$(mul 4 5)\" = 20 ]", "sub": "[ \"$(sub 9 4)\" = 5 ]", "neg": "[ \"$(neg 3)\" = -3 ]"}
	body := add
	var cases []*eval.Case
	for _, fn := range []string{"mul", "sub", "neg"} {
		base := testutil.Git(t, root, "rev-parse", "HEAD")
		body += fns[fn]
		c := commitFiles(t, root, "Add "+fn, map[string]string{"lib/calc.sh": body, "tests/test_" + fn + ".sh": ". lib/calc.sh\n" + tests[fn] + "\n"})
		cases = append(cases, &eval.Case{Schema: 1, ID: eval.CaseID(c), Status: eval.StatusApproved, Split: eval.SplitTest,
			Source: eval.Source{Commit: c, Base: base, Subject: "Add " + fn}, Prompt: "Thêm hàm " + fn + " vào thư viện tính toán.",
			TestFiles: []string{"tests/test_" + fn + ".sh"}, SourceFiles: []string{"lib/calc.sh"},
			Verification: &eval.Verification{Status: "verified"}})
	}
	f := false
	if _, err := app.Init(ctx, app.InitOptions{Dir: root, Hooks: &f, NoSkills: true, BintPath: "bint"}); err != nil {
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
	db := st.DB

	cfg := k.Config
	tc := "sh tests/run.sh"
	cfg.Eval.TestCommand, cfg.Eval.TimeoutMinutes = &tc, 2
	k.Config = cfg
	now := time.Now()
	p := &Proposal{Schema: ProposalSchema, ID: "ep_" + ulid.New(), Level: "L1", TargetType: "knowledge", Action: ActionCreate,
		Opportunity: "failure:lib", Status: StatusProposed, Generator: GenAgent, CreatedAt: store.FormatTime(now), UpdatedAt: store.FormatTime(now),
		Base: []BaseVersion{}, TargetIDs: []string{},
		Changes: []Change{{Op: ActionCreate, ItemID: ulid.New(), Draft: &Draft{Type: "warning", Title: "Hàm số học nằm trong lib/calc.sh",
			Body: "MARKER-CALC: mọi hàm số học mới viết trong lib/calc.sh theo mẫu của add.", Scope: knowledge.Scope{Paths: []string{"lib/**"}},
			Evidence: []string{"lib/calc.sh:1"}}}}}
	// Lượt R1 làm căn cứ của đề xuất đã tạo ra lời giải của tác vụ đầu (commit được gắn
	// qua quét Git): tác vụ đó phải bị loại khỏi đối chiếu.
	p.SupportingRuns = []RunRef{{RunID: "R1", Label: "failure"}}
	h := &History{ByID: map[string]*Run{"R1": {ID: "R1", Commits: []string{cases[0].Source.Commit[:10]}}}}
	p.Plan = defaultPlan(p.Action, cfg.Evolution.Evaluation)
	p.Risk, p.RiskReasons = assessRisk(p, nil)
	if err := SaveProposal(ctx, db, p); err != nil {
		t.Fatal(err)
	}

	env := eval.Env{RepoDir: root, WorkRoot: k.Paths.EvalWork, BintRoot: root, Cfg: cfg}
	env.Variant = Variant(p, eval.ArmMinimal, root, k.Source(), cfg.Context.Freshness.Fingerprint, now)
	impacts, err := SelectCases(ctx, env, eval.LeakEconomy, p, h, cases)
	if err != nil {
		t.Fatal(err)
	}
	if impacts[0].Affected || !strings.Contains(impacts[0].Reason, "lượt căn cứ R1") {
		t.Fatalf("tác vụ có lời giải từ lượt căn cứ phải bị loại: %+v", impacts[0])
	}
	for _, ci := range impacts[1:] {
		if !ci.Affected {
			t.Fatalf("tác vụ %s phải chịu ảnh hưởng: %s", ci.CaseID, ci.Reason)
		}
	}
	all := cases
	cases = cases[1:]

	agentDir := t.TempDir()
	script := filepath.Join(agentDir, "agent.sh")
	os.WriteFile(script, []byte(`
if grep -q MARKER-CALC AGENTS.md 2>/dev/null; then
  printf '%s\n' 'mul() { echo $(($1 * $2)); }' 'sub() { echo $(($1 - $2)); }' 'neg() { echo $((0 - $1)); }' >> lib/calc.sh
fi
echo '{"type":"result","num_turns":2}'
`), 0o644)
	rec := Evaluation{ProposalID: p.ID, Split: eval.SplitTest, BaseKey: p.BaseKey(), Delivery: eval.ArmMinimal, CreatedAt: store.FormatTime(now)}
	for _, c := range cases {
		rec.CaseIDs = append(rec.CaseIDs, c.ID)
	}
	res, err := eval.Run(ctx, env, db, eval.RunOptions{Cases: cases, Arms: []string{eval.ArmBase, eval.ArmCandidate}, Repeats: 2,
		LeakageMode: eval.LeakEconomy, Agent: &eval.CommandAgent{Command: "sh " + filepath.ToSlash(script)}, Seed: 3, BintVersion: "test",
		OnBatch: func(id string) error { rec.BatchID = id; return RecordEvaluation(ctx, db, &rec) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" || res.Done != 8 {
		t.Fatalf("run: %+v", res)
	}
	current := map[string]*eval.Case{}
	for _, c := range all {
		current[c.ID] = c
	}
	v, rep, err := Finalize(ctx, db, p, rec, current)
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != VerdictSupported || v.PassDiff == nil || v.PassDiff.Mean != 1 || !strings.Contains(strings.Join(v.Notes, ";"), "hồi cứu") {
		t.Fatalf("verdict: %+v\n%+v", v, rep.Arms)
	}
	rows, _ := eval.Attempts(ctx, db, "batch_id = ?", res.BatchID)
	for _, r := range rows {
		if r.Arm == eval.ArmCandidate && !strings.Contains(r.ContextSource, "candidate:"+p.ID) {
			t.Fatalf("nguồn ngữ cảnh: %s", r.ContextSource)
		}
	}
	evs, _ := Evaluations(ctx, db, p.ID)
	if e, ok := VerifiedBy(evs, p); !ok || e.BatchID != res.BatchID {
		t.Fatalf("verified: %+v", evs)
	}

	// Đề xuất khác cùng đối tượng dùng lại tác vụ test đã dùng → không độc lập.
	p2 := *p
	p2.ID = "ep_" + ulid.New()
	if err := SaveProposal(ctx, db, &p2); err != nil {
		t.Fatal(err)
	}
	if ok, why, _ := Independent(ctx, db, &p2, rec.CaseIDs[:1]); ok || !strings.Contains(why, p.ID) {
		t.Fatalf("independent: %v %s", ok, why)
	}

	// Áp dụng như đã kiểm chứng (không cần --unverified).
	ar, err := Apply(ctx, ApplyOptions{K: k, DB: db, Ref: p.ID, Now: now, Cases: current})
	if err != nil {
		t.Fatal(err)
	}
	if !ar.Verified || ar.Evaluation == nil || ar.EvidenceKind != KindRetrospective {
		t.Fatalf("apply: %+v", ar)
	}
	// Sau khi áp dụng, phiên bản gốc của đề xuất khác (create cùng ID) không còn hợp lệ.
	if probs, _ := p2.CheckBase(k.Store); len(probs) != 1 {
		t.Fatalf("base p2: %+v", probs)
	}

	// Kiểm tra hồi quy khi đổi mô hình cho đề xuất TRI THỨC đã áp dụng: base = tập trước đề xuất
	// (dựng lại từ bản ghi kiểm toán), candidate = hiện tại.
	bi, err := BeforeItems(ctx, db, p)
	if err != nil || len(bi) != 1 || bi[p.Changes[0].ItemID] != nil {
		t.Fatalf("trước đề xuất tạo: mục chưa tồn tại: %v %v", err, bi)
	}
	curItems, _, _ := k.Items()
	after := map[string]*knowledge.Item{}
	for _, it := range curItems {
		after[it.ID] = it
	}
	env.Variant = RegressionVariant(p, eval.ArmMinimal, root, bi, after)
	rrec := Evaluation{ProposalID: p.ID, BaseKey: p.BaseKey(), Delivery: eval.ArmMinimal, CreatedAt: store.FormatTime(now), Kind: EvalRegression}
	for _, c := range cases {
		rrec.CaseIDs = append(rrec.CaseIDs, c.ID)
	}
	rres, err := eval.Run(ctx, env, db, eval.RunOptions{Cases: cases, Arms: []string{eval.ArmBase, eval.ArmCandidate}, Repeats: 2,
		LeakageMode: eval.LeakEconomy, Agent: &eval.CommandAgent{Command: "sh " + filepath.ToSlash(script)}, Seed: 5, BintVersion: "test",
		OnBatch: func(id string) error { rrec.BatchID = id; return RecordEvaluation(ctx, db, &rrec) }})
	if err != nil || rres.Done != 8 {
		t.Fatalf("hồi quy: %v %+v", err, rres)
	}
	rv, _, err := Finalize(ctx, db, p, rrec, current)
	if err != nil || rv.Verdict != VerdictSupported {
		t.Fatalf("hồi quy phải supported (cải tiến vẫn có lợi): %v %+v", err, rv)
	}
}

func TestDerivedFromWindowAndKind(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	h := &History{ByID: map[string]*Run{"R": {ID: "R", StartedAt: store.FormatTime(start),
		EndedAt: store.FormatTime(start.Add(time.Hour)), Modified: []string{"lib/a.go"}}}}
	p := &Proposal{SupportingRuns: []RunRef{{RunID: "R"}}, CreatedAt: store.FormatTime(start.Add(48 * time.Hour)),
		Changes: []Change{{Op: ActionCreate, Draft: &Draft{}}}}
	mk := func(at time.Time, file string) *eval.Case {
		return &eval.Case{ID: "c" + at.Format("0102150405"), Source: eval.Source{Commit: "abc", Date: at.Format(time.RFC3339)}, SourceFiles: []string{file}}
	}
	for _, tc := range []struct {
		c    *eval.Case
		want bool
	}{
		{mk(start.Add(2*time.Hour), "lib/a.go"), true},   // trong cửa sổ, cùng tệp
		{mk(start.Add(30*time.Hour), "lib/a.go"), false}, // ngoài cửa sổ 24 giờ
		{mk(start.Add(2*time.Hour), "lib/b.go"), false},  // khác tệp
		{mk(start.Add(-time.Hour), "lib/a.go"), false},   // trước lượt
	} {
		if got := DerivedFrom(p, h, tc.c) != ""; got != tc.want {
			t.Errorf("%s %s: %v", tc.c.Source.Date, tc.c.SourceFiles[0], got)
		}
	}
	// Tiến cứu: đề xuất tạo lúc start+48h; lượt R kết thúc ngay trước đó.
	created := start.Add(48 * time.Hour)
	run := h.ByID["R"]
	run.EndedAt = store.FormatTime(created.Add(-time.Minute))
	after := mk(created.Add(time.Minute), "lib/a.go") // commit C sau đề xuất, cùng tệp, trong 24h
	// Kịch bản rò rỉ: R chưa gắn commit (công việc có thể chính là C) → phải loại.
	if DerivedFrom(p, h, after) == "" {
		t.Error("R chưa gắn commit, C sau đề xuất cùng tệp: phải bị loại")
	}
	// R đã gắn commit khác trước đề xuất → được dùng.
	run.Commits = []string{"def1234"}
	if DerivedFrom(p, h, after) != "" {
		t.Error("R đã gắn commit khác: tác vụ sau đề xuất phải được dùng")
	}
	// Commit đồng thời/trước đề xuất: giữ cửa sổ dù R đã gắn commit.
	for _, at := range []time.Time{created, created.Add(-30 * time.Minute)} {
		if DerivedFrom(p, h, mk(at, "lib/a.go")) == "" {
			t.Errorf("commit lúc %s phải bị loại", at)
		}
	}
	// R kết thúc sau đề xuất: không xác định được → giữ cửa sổ.
	run.EndedAt = store.FormatTime(created.Add(time.Minute))
	if DerivedFrom(p, h, mk(created.Add(2*time.Minute), "lib/a.go")) == "" {
		t.Error("R kết thúc sau đề xuất: phải giữ cửa sổ")
	}
	// Commit chính của lượt căn cứ vẫn bị loại.
	run.EndedAt = store.FormatTime(created.Add(-time.Minute))
	run.Commits = []string{"abc"}
	if DerivedFrom(p, h, after) == "" {
		t.Error("commit thuộc lượt căn cứ phải bị loại")
	}
	run.Commits, run.EndedAt = nil, store.FormatTime(start.Add(time.Hour))
	old, future := mk(start, "x"), mk(start.Add(72*time.Hour), "x")
	cases := map[string]*eval.Case{old.ID: old, future.ID: future}
	if k := EvidenceKind(p, []string{future.ID}, cases); k != KindProspective {
		t.Errorf("tiến cứu: %q", k)
	}
	if k := EvidenceKind(p, []string{old.ID, future.ID}, cases); k != KindRetrospective {
		t.Errorf("hồi cứu: %q", k)
	}
	dep := &Proposal{Changes: []Change{{Op: ActionDeprecate}}}
	if k := EvidenceKind(dep, []string{old.ID}, cases); k != "" {
		t.Errorf("loại bỏ không có nội dung mới: %q", k)
	}
}
