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
	"bint/internal/skills"
	"bint/internal/store"
	"bint/internal/testutil"
)

// TestEvaluateSkillProposal: skill đề xuất được đối chiếu base/candidate trên tác vụ thật;
// agent giả chỉ giải được khi skill có trong thư mục skill của harness. Sau khi áp dụng,
// kiểm tra hồi quy chạy lại với base = trước đề xuất (dựng lại) mà không thành căn cứ áp dụng.
func TestEvaluateSkillProposal(t *testing.T) {
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
	fns := map[string]string{"mul": "mul() { echo $(($1 * $2)); }\n", "sub": "sub() { echo $(($1 - $2)); }\n", "neg": "neg() { echo $((0 - $1)); }\n"}
	tests := map[string]string{"mul": "[ \"$(mul 4 5)\" = 20 ]", "sub": "[ \"$(sub 9 4)\" = 5 ]", "neg": "[ \"$(neg 3)\" = -3 ]"}
	body := add
	var cases []*eval.Case
	for _, fn := range []string{"mul", "sub", "neg"} {
		base := testutil.Git(t, root, "rev-parse", "HEAD")
		body += fns[fn]
		c := commitFiles(t, root, "Add "+fn, map[string]string{"lib/calc.sh": body, "tests/test_" + fn + ".sh": ". lib/calc.sh\n" + tests[fn] + "\n"})
		cases = append(cases, &eval.Case{Schema: 1, ID: eval.CaseID(c), Status: eval.StatusApproved, Split: eval.SplitTest,
			Source: eval.Source{Commit: c, Base: base, Subject: "Add " + fn}, Prompt: "Thêm hàm " + fn + " vào thư viện tính toán.",
			TestFiles: []string{"tests/test_" + fn + ".sh"}, SourceFiles: []string{"lib/calc.sh"}, Verification: &eval.Verification{Status: "verified"}})
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

	d := &SkillDraft{Name: "calc-functions", Description: "Dùng khi thêm hàm số học mới vào lib/calc.sh: viết theo mẫu của add trong cùng tệp.",
		Body: "1. Mở lib/calc.sh.\n2. Thêm hàm theo mẫu add.", Triggers: skills.Triggers{Paths: []string{"lib/**"}, Keywords: []string{"hàm"}},
		Evidence: []string{"lib/calc.sh:1"}}
	in := ProposeInput{K: k, DB: db, Scan: ScanResult{Prerequisites: Prerequisites{Quantitative: true}}, History: &History{ByID: map[string]*Run{}}, Now: now}
	p, err := newSkillProposal(in, Opportunity{Key: "skill:lib"}, ActionCreate, nil, []SkillChange{{Op: ActionCreate, Name: d.Name, Draft: d}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveProposal(ctx, db, p); err != nil {
		t.Fatal(err)
	}
	before := CurrentSet(nil)
	after, err := ApplyToSet(p, before, k.Source(), "symbol")
	if err != nil || len(after) != 1 {
		t.Fatalf("%v %v", err, after)
	}
	env := eval.Env{RepoDir: root, WorkRoot: k.Paths.EvalWork, BintRoot: root, Cfg: cfg}
	env.Variant = SkillVariant(p, eval.ArmMinimal, root, before, after)
	impacts, err := SelectSkillCases(ctx, env, eval.LeakEconomy, p, nil, cases, before, after)
	if err != nil {
		t.Fatal(err)
	}
	for _, ci := range impacts {
		if !ci.Affected {
			t.Fatalf("tác vụ %s phải chịu ảnh hưởng: %s", ci.CaseID, ci.Reason)
		}
	}
	// Skill có mã lời giải bị loại khỏi nhánh candidate (không lọt lời giải).
	leaky := *p
	leaky.SkillChanges = []SkillChange{{Op: ActionCreate, Name: "calc-functions"}}
	leakySet := SkillSet{"calc-functions": {Name: "calc-functions", Triggers: d.Triggers, Evidence: after["calc-functions"].Evidence,
		Content: skills.BuildContent("calc-functions", d.Description, "Dùng đúng:\nmul() { echo $(($1 * $2)); }")}}
	lenv := env
	lenv.Variant = SkillVariant(&leaky, eval.ArmMinimal, root, SkillSet{}, leakySet)
	if li, _ := SelectSkillCases(ctx, lenv, eval.LeakEconomy, &leaky, nil, cases[:1], SkillSet{}, leakySet); li[0].Affected || !strings.Contains(li[0].Reason, "lời giải") {
		t.Fatalf("skill chứa lời giải phải bị loại: %+v", li[0])
	}

	script := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(script, []byte(`
if [ -f .claude/skills/calc-functions/SKILL.md ] && [ -f .agents/skills/calc-functions/SKILL.md ]; then
  printf '%s\n' 'mul() { echo $(($1 * $2)); }' 'sub() { echo $(($1 - $2)); }' 'neg() { echo $((0 - $1)); }' >> lib/calc.sh
fi
echo '{"type":"result","num_turns":2}'
`), 0o644)
	run := func(kind string) (Evaluation, Verdict) {
		rec := Evaluation{ProposalID: p.ID, Split: eval.SplitTest, BaseKey: p.BaseKey(), Delivery: eval.ArmMinimal, CreatedAt: store.FormatTime(now), Kind: kind}
		for _, c := range cases {
			rec.CaseIDs = append(rec.CaseIDs, c.ID)
		}
		res, err := eval.Run(ctx, env, db, eval.RunOptions{Cases: cases, Arms: []string{eval.ArmBase, eval.ArmCandidate}, Repeats: 2,
			LeakageMode: eval.LeakEconomy, Agent: &eval.CommandAgent{Command: "sh " + filepath.ToSlash(script)}, Seed: 3, BintVersion: "test",
			OnBatch: func(id string) error { rec.BatchID = id; return RecordEvaluation(ctx, db, &rec) }})
		if err != nil || res.Status != "completed" || res.Done != 12 {
			t.Fatalf("run: %v %+v", err, res)
		}
		current := map[string]*eval.Case{}
		for _, c := range cases {
			current[c.ID] = c
		}
		v, _, err := Finalize(ctx, db, p, rec, current)
		if err != nil {
			t.Fatal(err)
		}
		return rec, v
	}
	rec, v := run("")
	if v.Verdict != VerdictSupported || v.PassDiff == nil || v.PassDiff.Mean != 1 {
		t.Fatalf("verdict: %+v", v)
	}
	rows, _ := eval.Attempts(ctx, db, "batch_id = ?", rec.BatchID)
	for _, r := range rows {
		if r.Arm == eval.ArmCandidate && !strings.Contains(r.ContextSource, "skills[calc-functions@") {
			t.Fatalf("nguồn ngữ cảnh phải ghi skill: %s", r.ContextSource)
		}
		if r.Arm == eval.ArmBase && strings.Contains(r.ContextSource, "calc-functions") {
			t.Fatalf("base không được có skill: %s", r.ContextSource)
		}
	}
	evs, _ := Evaluations(ctx, db, p.ID)
	if e, ok := VerifiedBy(evs, p); !ok || e.BatchID != rec.BatchID {
		t.Fatalf("verified: %+v", evs)
	}
	ar, err := Apply(ctx, ApplyOptions{K: k, DB: db, Ref: p.ID, Now: now})
	if err != nil || !ar.Verified {
		t.Fatalf("apply: %v %+v", err, ar)
	}
	ev, _ := skills.LoadEvolvedOne(root, "calc-functions")
	if last := ev.Last(); last == nil || !last.Verified || last.Evaluation != rec.ID {
		t.Fatalf("phiên bản phải ghi kiểm chứng: %+v", ev.Meta.Versions)
	}

	// Kiểm tra hồi quy: base dựng lại từ trạng thái trước đề xuất; không phải căn cứ áp dụng.
	list, _, _ := skills.LoadEvolved(root)
	rb, err := ReverseSet(root, p, list)
	if err != nil || len(rb) != 0 {
		t.Fatalf("trước đề xuất tạo không có skill: %v %v", err, rb)
	}
	env.Variant = SkillVariant(p, eval.ArmMinimal, root, rb, CurrentSet(list))
	rrec, rv := run(EvalRegression)
	if rv.Verdict != VerdictSupported {
		t.Fatalf("hồi quy: %+v", rv)
	}
	if ids, _ := OriginalCases(ctx, db, p); len(ids) != 3 {
		t.Fatalf("tập đại diện lấy từ lần đối chiếu gốc: %v", ids)
	}
	evs, _ = Evaluations(ctx, db, p.ID)
	if got := evs[len(evs)-1]; got.ID != rrec.ID || got.Kind != EvalRegression {
		t.Fatalf("loại lần đối chiếu: %+v", got)
	}
}
