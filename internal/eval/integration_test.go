package eval

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/store"
	"bint/internal/testutil"
)

func needSh(t *testing.T) {
	t.Helper()
	if prog, _ := shellCommand("true"); prog == "cmd" {
		t.Skip("cần sh (Git for Windows hoặc POSIX) để chạy kiểm thử tích hợp")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("cần git")
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if body == "" {
			os.Remove(p)
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func commit(t *testing.T, root, msg string, files map[string]string) string {
	t.Helper()
	writeFiles(t, root, files)
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", msg)
	return testutil.Git(t, root, "rev-parse", "HEAD")
}

const (
	runner  = "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\necho ALL OK\n"
	calcV1  = "add() { echo $(($1 + $2)); }\n"
	calcV2  = calcV1 + "mul() { echo $(($1 * $2)); }\n"
	testAdd = ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ] || { echo add failed; exit 1; }\n"
	testMul = ". lib/calc.sh\n[ \"$(mul 4 5)\" = 20 ] || { echo mul failed; exit 1; }\n"
)

// fixtureRepo dựng lịch sử:
//
//	c0 khởi tạo (có .bint/evals/minimal.md và AGENTS.md)
//	c1 thêm mul + kiểm thử           → tác vụ hợp lệ (fail→pass)
//	c2 chỉ sửa README                 → bỏ qua (không có kiểm thử)
//	c3 thêm kiểm thử cho add + chú thích → ứng viên nhưng kiểm thử đã qua ở commit cha
func fixtureRepo(t *testing.T) (root, solution, bad string) {
	root = testutil.NewRepo(t, map[string]string{
		"README.md":              "# calc\n",
		"lib/calc.sh":            calcV1,
		"tests/run.sh":           runner,
		"tests/test_add.sh":      testAdd,
		"AGENTS.md":              "# project rules (current)\n",
		".bint/evals/minimal.md": "Rules at base: shell functions live in lib/calc.sh\n",
	})
	solution = commit(t, root, "Add multiplication helper to the calc library\n\nCallers need to compute products as well as sums.\n\nSigned-off-by: Dev <d@x>",
		map[string]string{"lib/calc.sh": calcV2, "tests/test_mul.sh": testMul})
	commit(t, root, "Document usage in README for new contributors", map[string]string{"README.md": "# calc\n\nUsage: . lib/calc.sh\n"})
	bad = commit(t, root, "Clarify how add handles its two arguments in the library",
		map[string]string{"lib/calc.sh": "# add prints the sum of two integers.\n# Both arguments must be integers.\n# The result goes to stdout.\n" + calcV2, "tests/test_add2.sh": ". lib/calc.sh\n[ \"$(add 1 1)\" = 2 ] || exit 1\n"})
	// Sau commit lời giải, minimal.md bị sửa và chứa mã của lời giải: strict không được dùng bản này.
	commit(t, root, "Update minimal context", map[string]string{".bint/evals/minimal.md": "Hint: mul() { echo $(($1 * $2)); }\n"})
	return root, solution, bad
}

func testEnv(t *testing.T, root string) (Env, Store, *sql.DB) {
	t.Helper()
	cfg := config.Default()
	tc := "sh tests/run.sh"
	cfg.Eval.TestCommand = &tc
	cfg.Eval.TimeoutMinutes = 2
	cfg.Eval.Mine.MinSourceLines = 1 // lời giải của fixture chỉ có một dòng
	state := t.TempDir()
	env := Env{RepoDir: root, WorkRoot: filepath.Join(state, "eval-work"), Cfg: cfg}
	db, err := store.Open(context.Background(), filepath.Join(state, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return env, NewStore(state, filepath.Join(root, ".bint")), db.DB
}

func TestMineVerifyRunReport(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, bad := fixtureRepo(t)
	env, st, db := testEnv(t, root)

	res, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.New) != 2 || res.Skipped[SkipNoParent] != 1 || res.Skipped[SkipNoTests] < 1 {
		t.Fatalf("mine: new=%d skipped=%v", len(res.New), res.Skipped)
	}
	byCommit := map[string]*Case{}
	for _, c := range res.New {
		byCommit[c.Source.Commit] = c
	}
	good, badCase := byCommit[solution], byCommit[bad]
	if good == nil || badCase == nil {
		t.Fatalf("expected candidates for %s and %s", solution, bad)
	}
	if strings.Contains(good.Prompt, "Signed-off-by") || !strings.Contains(good.Prompt, "products") {
		t.Fatalf("prompt: %q", good.Prompt)
	}
	if good.Leak.Status != "pass" || len(good.TestFiles) != 1 || good.TestFiles[0] != "tests/test_mul.sh" {
		t.Fatalf("case: %+v", good)
	}
	// Khai thác lại không tạo trùng.
	again, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{})
	if err != nil || len(again.New) != 0 || again.Skipped[SkipKnown] != 2 {
		t.Fatalf("re-mine: %+v %v", again, err)
	}

	if v := VerifyCase(ctx, env, good, 2, testNow()); v.Status != "verified" || !v.FailBefore || !v.PassAfter {
		t.Fatalf("verify good: %+v", v)
	}
	if v := VerifyCase(ctx, env, badCase, 1, testNow()); v.Status != "failed" || v.FailBefore {
		t.Fatalf("verify bad must fail (tests already pass at parent): %+v", v)
	}
	good.Verification = &Verification{Status: "verified"}
	good.Split = SplitValidation

	// Agent giả lập: ghi lại những gì nó thấy, và chỉ "giải" khi có AGENTS.md (nhánh minimal).
	agentDir := t.TempDir()
	script := filepath.Join(agentDir, "agent.sh")
	writeFiles(t, agentDir, map[string]string{"agent.sh": `
{
  echo "arm=$BINT_EVAL_ARM"
  echo "commits=$(git log --all --format=%H | tr '\n' ' ')"
  echo "remotes=$(git remote | tr '\n' ' ')"
  ls tests | tr '\n' ' '; echo
  [ -f AGENTS.md ] && echo "agents=$(cat AGENTS.md)"
  [ -f .bint/evals/minimal.md ] && echo "bint-dir-present"
} > "$BINT_EVAL_PROMPT_FILE.seen"
cp "$BINT_EVAL_PROMPT_FILE.seen" "$(dirname "$BINT_EVAL_PROMPT_FILE")/seen.txt"
if [ -f AGENTS.md ]; then echo 'mul() { echo $(($1 * $2)); }' >> lib/calc.sh; fi
echo '{"type":"result","num_turns":3,"total_cost_usd":0.05}'
`})
	agent := &CommandAgent{Command: "sh " + filepath.ToSlash(script)}
	budget := 10.0
	out, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone, ArmMinimal}, Repeats: 2, LeakageMode: LeakStrict,
		Agent: agent, BudgetUSD: &budget, Seed: 7, BintVersion: "test", Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" || out.Total != 4 || out.Done != 4 {
		t.Fatalf("run: %+v", out)
	}
	rows, err := Attempts(ctx, db, "batch_id = ?", out.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		seen, err := os.ReadFile(filepath.Join(r.LogDir, "seen.txt"))
		if err != nil {
			t.Fatalf("%s/%s: %v", r.Arm, r.Status, err)
		}
		s := string(seen)
		// Toàn vẹn đánh giá: không thấy commit lời giải, không có remote, không thấy kiểm thử ẩn.
		if strings.Contains(s, solution) || strings.Contains(s, bad) {
			t.Fatalf("agent saw future commits:\n%s", s)
		}
		if !strings.Contains(s, "remotes= \n") && !strings.Contains(s, "remotes=\n") {
			t.Fatalf("remote left in workdir:\n%s", s)
		}
		if strings.Contains(s, "test_mul.sh") {
			t.Fatalf("hidden test visible to agent:\n%s", s)
		}
		if strings.Contains(s, "bint-dir-present") || strings.Contains(s, "project rules") {
			t.Fatalf("context not stripped:\n%s", s)
		}
		switch r.Arm {
		case ArmNone:
			if r.Status != "failed" || r.TestPassed == nil || *r.TestPassed || strings.Contains(s, "agents=") {
				t.Fatalf("none arm: %+v\n%s", r, s)
			}
		case ArmMinimal:
			// strict: dùng minimal.md tại commit gốc, không phải bản sau có chứa lời giải.
			if r.Status != "passed" || !strings.Contains(s, "Rules at base") || strings.Contains(s, "Hint") {
				t.Fatalf("minimal arm: %+v\n%s", r, s)
			}
			if !strings.HasPrefix(r.ContextSource, short(good.Source.Base)+":.bint/evals/minimal.md#") {
				t.Fatalf("context source %q", r.ContextSource)
			}
		}
		if r.CostUSD == nil || *r.CostUSD != 0.05 || r.NumTurns == nil || *r.NumTurns != 3 {
			t.Fatalf("metrics not parsed: %+v", r)
		}
		if r.FilesChanged == nil || r.CaseFingerprint != good.Fingerprint() || r.CaseSplit != SplitValidation {
			t.Fatalf("row: %+v", r)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(env.WorkRoot, "runs", out.BatchID)); len(entries) != 0 {
		t.Fatalf("workdirs not cleaned: %d left", len(entries))
	}

	b, err := GetBatch(ctx, db, "latest")
	if err != nil || b.BatchID != out.BatchID || b.Seed != 7 || b.LeakageMode != LeakStrict {
		t.Fatalf("batch: %+v %v", b, err)
	}
	rep, err := BuildReport(ctx, db, b, "", map[string]*Case{good.ID: good})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Arms) != 2 || len(rep.Paired) != 1 {
		t.Fatalf("report: %+v", rep)
	}
	p := rep.Paired[0]
	if p.Arm != ArmMinimal || p.Baseline != ArmNone || p.PassDiff.Mean != 1 || p.PassDiff.N != 1 {
		t.Fatalf("paired: %+v", p)
	}
	for _, a := range rep.Arms {
		if a.CasesWithRepeats != 1 || a.WithinCaseSD == nil || *a.WithinCaseSD != 0 || a.Graded != 2 {
			t.Fatalf("arm summary: %+v", a)
		}
	}
	if !hasDiag(rep, "ci_unavailable") {
		t.Fatalf("expected ci_unavailable with a single case: %+v", rep.Diagnostics)
	}

	// Ước tính dùng chi phí lịch sử: 3 tác vụ × 2 nhánh × 2 lần × $0.05.
	est, err := EstimateRun(ctx, db, EstimateInput{Cases: 3, Arms: []string{"none", "minimal"}, Repeats: 2, Harness: "command", Effect: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if est.Runs != 12 || est.TotalCost == nil || *est.TotalCost < 0.599 || *est.TotalCost > 0.601 {
		t.Fatalf("estimate: %+v", est)
	}
}

var testNow = time.Now

func hasDiag(r Report, kind string) bool {
	for _, d := range r.Diagnostics {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

func TestRunBudgetAndEconomyAndResume(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, _ := st.Candidates()
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	agentDir := t.TempDir()
	writeFiles(t, agentDir, map[string]string{"agent.sh": "echo '{\"type\":\"result\",\"total_cost_usd\":1.0}'\n"})
	agent := &CommandAgent{Command: "sh " + filepath.ToSlash(filepath.Join(agentDir, "agent.sh"))}

	// economy: minimal.md hiện tại chứa mã lời giải → nhánh bị bỏ qua cho tác vụ này.
	budget := 2.5
	out, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone, ArmMinimal}, Repeats: 3, LeakageMode: LeakEconomy,
		Agent: agent, BudgetUSD: &budget, Seed: 1, Now: testNow,
	})
	if err == nil || out.Status != "budget_exhausted" {
		t.Fatalf("expected budget stop, got %+v err=%v", out, err)
	}
	rows, _ := Attempts(ctx, db, "batch_id = ?", out.BatchID)
	spent, skipped := 0.0, 0
	for _, r := range rows {
		if r.CostUSD != nil {
			spent += *r.CostUSD
		}
		if r.Status == "skipped" {
			skipped++
			if r.Arm != ArmMinimal || !strings.Contains(r.Error, "lời giải") {
				t.Fatalf("unexpected skip: %+v", r)
			}
		}
	}
	if spent > budget || len(rows) >= 6 {
		t.Fatalf("budget not enforced: spent %.2f over %d rows", spent, len(rows))
	}

	// Tiếp tục với ngân sách mới: chỉ chạy các lượt còn thiếu.
	more := 100.0
	out2, err := Run(ctx, env, db, RunOptions{Cases: []*Case{good}, Agent: agent, BudgetUSD: &more, ResumeBatch: out.BatchID, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if out2.BatchID != out.BatchID || out2.Status != "completed" || out2.Done != 6 {
		t.Fatalf("resume: %+v", out2)
	}
	rows, _ = Attempts(ctx, db, "batch_id = ?", out.BatchID)
	if len(rows) != 6 {
		t.Fatalf("rows after resume: %d", len(rows))
	}
	b, _ := GetBatch(ctx, db, out.BatchID)
	rep, err := BuildReport(ctx, db, b, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(rep, "leakage_economy") {
		t.Fatalf("economy diagnostic missing: %+v", rep.Diagnostics)
	}
}
