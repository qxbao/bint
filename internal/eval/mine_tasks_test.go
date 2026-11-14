package eval

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// taskFixture dựng repo: c0 (gốc), một commit "scratch" (BaseTree khác cây của Base), rồi cây "sau" gồm
// lời giải mul + test_mul + sửa README (README không thuộc phiếu).
func taskFixture(t *testing.T) (root string, ts TaskSource) {
	t.Helper()
	root = testutil.NewRepo(t, map[string]string{
		"README.md":         "# calc\n",
		"lib/calc.sh":       calcV1,
		"tests/run.sh":      runner,
		"tests/test_add.sh": testAdd,
	})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	commit(t, root, "scratch", map[string]string{"NOTES.txt": "scratch\n"})
	baseTree := testutil.Git(t, root, "rev-parse", "HEAD^{tree}")
	commit(t, root, "solution", map[string]string{"lib/calc.sh": calcV2, "tests/test_mul.sh": testMul, "README.md": "# calc\n\nchanged\n"})
	after := testutil.Git(t, root, "rev-parse", "HEAD^{tree}")
	testutil.Git(t, root, "reset", "-q", "--hard", base)
	ts = TaskSource{
		ID: "01M7TASKAAAAAAAAAAAAAAAAAA", Goal: "Add multiplication helper", Request: "Add a mul function to the calc library so callers can compute products.",
		Criteria: []string{"mul 4 5 prints 20", "```go\nsecret()\n```"}, Requirement: "# req\nlonger text",
		VerifyCommand: "sh tests/run.sh", Base: base, BaseTree: baseTree, AfterTree: after,
		Files: []string{"lib/calc.sh", "tests/test_mul.sh"}, Runs: []string{"run1", "run2"}, Author: "dev", DoneAt: "2026-11-10T00:00:00Z",
	}
	return root, ts
}

func TestMineTasksBuildsCandidate(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, ts := taskFixture(t)
	env, st, _ := testEnv(t, root)

	res, err := MineTasks(ctx, root, env.Cfg.Eval.Mine, st, []TaskSource{ts}, MineOptions{})
	if err != nil || len(res.New) != 1 {
		t.Fatalf("mine: %+v %v", res, err)
	}
	c := res.New[0]
	if c.ID != "t01m7taskaaaaaaaaaaaaaaaaaa" || c.Source.Task != ts.ID || !slices.Equal(c.Source.TaskRuns, ts.Runs) {
		t.Fatalf("source: %+v", c)
	}
	if c.TestCommand != "sh tests/run.sh" {
		t.Fatalf("test_command = %q", c.TestCommand)
	}
	if !strings.Contains(c.Prompt, "products") || !strings.Contains(c.Prompt, "mul 4 5 prints 20") || strings.Contains(c.Prompt, "secret") || strings.Contains(c.Prompt, "```") {
		t.Fatalf("prompt: %q", c.Prompt)
	}
	if !strings.Contains(c.OriginalMessage, "requirement.md") || !strings.Contains(c.OriginalMessage, "secret()") || c.Spec == nil {
		t.Fatalf("original/spec: %q %+v", c.OriginalMessage, c.Spec)
	}
	if !slices.Equal(c.TestFiles, []string{"tests/test_mul.sh"}) || !slices.Equal(c.SourceFiles, []string{"lib/calc.sh"}) {
		t.Fatalf("files: %v %v", c.TestFiles, c.SourceFiles)
	}
	// Diff giữa hai commit tổng hợp đúng tập tệp của phiếu (README không lẫn vào); gốc mang BaseTree.
	names := strings.Fields(testutil.Git(t, root, "diff", "--name-only", c.Source.Base, c.Source.Commit))
	if !slices.Equal(names, []string{"lib/calc.sh", "tests/test_mul.sh"}) {
		t.Fatalf("diff files: %v", names)
	}
	if testutil.Git(t, root, "rev-parse", c.Source.Base+"^{tree}") != ts.BaseTree {
		t.Fatal("base commit must carry BaseTree")
	}
	for _, name := range []string{"base", "commit"} {
		if testutil.Git(t, root, "rev-parse", TaskRefPrefix+c.ID+"/"+name) == "" {
			t.Fatalf("missing ref %s", name)
		}
	}
	// Chạy lại: không tạo trùng; dựng lại cho cùng hash (tất định).
	again, err := MineTasks(ctx, root, env.Cfg.Eval.Mine, st, []TaskSource{ts}, MineOptions{})
	if err != nil || len(again.New) != 0 || again.Skipped[SkipKnown] != 1 {
		t.Fatalf("re-mine: %+v %v", again, err)
	}
	b2, s2, ok, err := freezeSolution(ctx, root, ts)
	if err != nil || !ok || b2 != c.Source.Base || s2 != c.Source.Commit {
		t.Fatalf("not deterministic: %s %s %v %v", b2, s2, ok, err)
	}
	if v := VerifyCase(ctx, env, c, 1, testNow()); v.Status != "verified" || !v.FailBefore || !v.PassAfter {
		t.Fatalf("verify: %+v", v)
	}
}

func TestMineTasksSkips(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, ts := taskFixture(t)
	env, st, _ := testEnv(t, root)

	gone := ts
	gone.ID, gone.AfterTree = "01M7TASKGONE", strings.Repeat("a", 40)
	noTest := ts
	noTest.ID, noTest.Files = "01M7TASKNOTEST", []string{"lib/calc.sh"}
	noFiles := ts
	noFiles.ID, noFiles.Files = "01M7TASKNOFILES", nil
	unconfirmed := ts
	unconfirmed.ID, unconfirmed.VerifyCommand = "01M7TASKUNCONF", ""
	res, err := MineTasks(ctx, root, env.Cfg.Eval.Mine, st, []TaskSource{gone, noTest, noFiles, unconfirmed}, MineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped[SkipNoSolution] != 2 || res.Skipped[SkipNoTests] != 1 || len(res.New) != 1 || res.Scanned != 4 {
		t.Fatalf("skips: %+v", res)
	}
	if res.New[0].TestCommand != "" {
		t.Fatalf("unconfirmed verify command must leave test_command empty: %q", res.New[0].TestCommand)
	}
}

func TestMineTaskGitDuplicatesBothWays(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	gitMsg := "Add multiplication helper to the calc library\n\nCallers need products as well as sums."
	gitFiles := map[string]string{"lib/calc.sh": calcV2, "tests/test_mul.sh": testMul}

	// Phiếu trước, rồi commit Git cùng thay đổi kiểm thử: Mine bỏ.
	root, ts := taskFixture(t)
	env, st, _ := testEnv(t, root)
	if res, err := MineTasks(ctx, root, env.Cfg.Eval.Mine, st, []TaskSource{ts}, MineOptions{}); err != nil || len(res.New) != 1 {
		t.Fatalf("tasks: %+v %v", res, err)
	}
	commit(t, root, gitMsg, gitFiles)
	res, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{})
	if err != nil || len(res.New) != 0 || res.Skipped[SkipDuplicate] != 1 {
		t.Fatalf("git after task: %+v %v", res, err)
	}

	// Chiều ngược: Git trước, phiếu sau: MineTasks bỏ.
	root2, ts2 := taskFixture(t)
	env2, st2, _ := testEnv(t, root2)
	commit(t, root2, gitMsg, gitFiles)
	if res, err := Mine(ctx, root2, env2.Cfg.Eval.Mine, st2, MineOptions{}); err != nil || len(res.New) != 1 {
		t.Fatalf("git: %+v %v", res, err)
	}
	res, err = MineTasks(ctx, root2, env2.Cfg.Eval.Mine, st2, []TaskSource{ts2}, MineOptions{})
	if err != nil || len(res.New) != 0 || res.Skipped[SkipDuplicate] != 1 {
		t.Fatalf("task after git: %+v %v", res, err)
	}
}

func TestCaseExcludesTaskKnowledge(t *testing.T) {
	c := &Case{ID: "tabc", Source: Source{Task: "01M7TASKID", TaskRuns: []string{"run1"}}}
	gitCase := &Case{ID: "cabc"}
	mk := func(title, body, run, evalCase string) *knowledge.Item {
		it := &knowledge.Item{Title: title, Body: body}
		it.Origin.RunID = run
		it.Origin.EvalCase = evalCase
		return it
	}
	for _, tc := range []struct {
		name string
		c    *Case
		it   *knowledge.Item
		want bool
	}{
		{"from eval case", c, mk("x", "y", "", "tabc"), true},
		{"task run", c, mk("x", "y", "run1", ""), true},
		{"task id in body (case-insensitive)", c, mk("x", "see 01m7taskid for context", "", ""), true},
		{"task id in title", c, mk("Lesson from 01M7TASKID", "y", "", ""), true},
		{"unrelated", c, mk("x", "y", "run9", ""), false},
		{"git case ignores runs", gitCase, mk("x", "y", "run1", ""), false},
	} {
		if got := tc.c.excludes(tc.it); got != tc.want {
			t.Errorf("%s: excludes=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestCaseTaskFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir, filepath.Join(dir, ".bint"))
	if err := os.MkdirAll(st.CandidatesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*Case{
		{Schema: 1, ID: "cgit", Status: StatusCandidate, Source: Source{Commit: "a", Base: "b"}},
		{Schema: 1, ID: "ttask", Status: StatusCandidate, Source: Source{Commit: "a", Base: "b", Task: "T", TaskRuns: []string{"r"}}},
	} {
		if err := st.SaveCandidate(c); err != nil {
			t.Fatal(err)
		}
		got, err := readCase(st.CandidatePath(c.ID))
		if err != nil || got.Source.Task != c.Source.Task || !slices.Equal(got.Source.TaskRuns, c.Source.TaskRuns) {
			t.Fatalf("%s: %+v %v", c.ID, got, err)
		}
	}
}
