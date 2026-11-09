package eval

import (
	"math"
	"strings"
	"testing"
)

func TestPatterns(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"*_test.go", "internal/db/db_test.go", true},
		{"*_test.go", "internal/db/db.go", false},
		{"**/testdata/**", "a/testdata/x.golden", true},
		{"**/testdata/**", "testdata/x.golden", true},
		{"**/tests/**", "tests/run.sh", true},
		{"**/tests/**", "src/tests/a/b.py", true},
		{"**/tests/**", "src/testsuite/a.py", false},
		{"docs/**", "docs/a/b.md", true},
		{"docs/**", "src/docs/a.md", false},
		{"*.spec.ts", "web/a.spec.ts", true},
		{"migrations/**", "migrations/0001.sql", true},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
	}
	for _, c := range cases {
		if got := CompilePattern(c.pat).Match(c.path); got != c.want {
			t.Errorf("%q ~ %q = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
	cls := Classifier{Test: DefaultTestPatterns, Ignore: DefaultIgnorePatterns}
	for path, want := range map[string]string{
		"pkg/a_test.go": "test", "pkg/a.go": "source", "README.md": "aux", "go.sum": "aux",
		"web/src/__tests__/x.tsx": "test", "src/x.ts": "source", ".bint/config.yml": "aux",
	} {
		if got := cls.Kind(path); got != want {
			t.Errorf("Kind(%q) = %s, want %s", path, got, want)
		}
	}
}

func TestDraftPrompt(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	msg := "Fix lock contention in store (0123456789a)\n\nRetry when SQLite reports busy.\n\n```go\ndb.Exec(\"PRAGMA busy_timeout=5000\")\n```\n\ndiff --git a/x b/x\n\nSee deadbeef1 for context.\n\nSigned-off-by: A <a@b>\nCo-Authored-By: Bot <b@c>\n🤖 Generated with [Claude Code](https://x)\n"
	got := DraftPrompt(msg, sha)
	for _, bad := range []string{"PRAGMA", "Signed-off-by", "Co-Authored-By", "0123456789a", "diff --git", "Generated with", "```"} {
		if strings.Contains(got, bad) {
			t.Errorf("prompt still contains %q:\n%s", bad, got)
		}
	}
	for _, keep := range []string{"Fix lock contention in store", "Retry when SQLite reports busy.", "deadbeef1"} {
		if !strings.Contains(got, keep) {
			t.Errorf("prompt lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank runs not collapsed:\n%q", got)
	}
}

func TestCheckLeak(t *testing.T) {
	in := LeakInput{
		Commit: "abcdef1234567890",
		AddedLines: []string{
			`	if err := retryBusy(db, maxAttempts); err != nil {`,
			`	// Retry when SQLite reports that the database is busy`,
			`}`,
		},
		NewIdents:  map[string]bool{"retryBusy": true, "maxAttempts": true, "Retry": true},
		TestIdents: map[string]bool{"maxAttempts": true},
	}
	clean := CheckLeak("Make the store retry when the database is busy.", in)
	if clean.Status != "pass" {
		t.Fatalf("clean prompt: %+v", clean)
	}
	code := CheckLeak("Do this:\n  if err := retryBusy(db,   maxAttempts); err != nil {\n", in)
	if code.Status != "fail" || !hasKind(code, "solution_code") {
		t.Fatalf("code leak not detected: %+v", code)
	}
	ident := CheckLeak("Add retryBusy and honour maxAttempts.", in)
	if ident.Status != "warn" || !hasKind(ident, "new_identifier") || len(ident.Findings) != 1 {
		t.Fatalf("identifier warn: %+v", ident)
	}
	comment := CheckLeak("Retry when SQLite reports that the database is busy", in)
	if comment.Status != "warn" || !hasKind(comment, "solution_comment") {
		t.Fatalf("comment warn: %+v", comment)
	}
	sha := CheckLeak("Like abcdef12 did.", in)
	if sha.Status != "fail" || !hasKind(sha, "solution_commit") {
		t.Fatalf("commit ref: %+v", sha)
	}
}

func hasKind(r LeakReport, kind string) bool {
	for _, f := range r.Findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func TestStats(t *testing.T) {
	xs := []float64{0, 1, 1, 0, 1, 1, 1, 0, 1, 1}
	ci := BootstrapMeanCI(xs, 5000, 0.05, 1)
	if !(ci.Lo < 0.7 && ci.Hi > 0.7 && ci.Lo >= 0 && ci.Hi <= 1) {
		t.Fatalf("ci = %+v", ci)
	}
	if again := BootstrapMeanCI(xs, 5000, 0.05, 1); again != ci {
		t.Fatal("bootstrap not reproducible with the same seed")
	}
	if c := BootstrapMeanCI([]float64{1}, 100, 0.05, 1); c.Valid() {
		t.Fatal("CI from one point must be invalid")
	}
	// ((1.96+0.8416)*0.5/0.1)^2 = 196.2 → 197
	if n := PairedSampleSize(0.5, 0.1); n != 197 {
		t.Fatalf("sample size = %d", n)
	}
	if !math.IsNaN(SD([]float64{1})) || math.Abs(SD([]float64{0, 1})-math.Sqrt(0.5)) > 1e-9 {
		t.Fatal("SD")
	}
}

func TestPlanDeterministicAndComplete(t *testing.T) {
	cs := []*Case{{ID: "c1"}, {ID: "c2"}}
	p1 := Plan(cs, []string{"none", "minimal"}, 3, 42)
	p2 := Plan(cs, []string{"none", "minimal"}, 3, 42)
	if len(p1) != 12 {
		t.Fatalf("plan size %d", len(p1))
	}
	seen := map[string]bool{}
	for i := range p1 {
		if p1[i].key() != p2[i].key() {
			t.Fatal("plan not deterministic")
		}
		seen[p1[i].key()] = true
	}
	if len(seen) != 12 {
		t.Fatal("plan has duplicates")
	}
}

func TestExpandTestCommand(t *testing.T) {
	got := ExpandTestCommand("go test {test_dirs}", []string{"a/b/x_test.go", "a/b/y_test.go", "root_test.go", "dir with space/z_test.go"})
	if got != `go test ./a/b . "./dir with space"` {
		t.Fatalf("got %q", got)
	}
	if ExpandTestCommand("make test", []string{"a"}) != "make test" {
		t.Fatal("unchanged command altered")
	}
}

func TestParseClaudeJSON(t *testing.T) {
	out := []byte("some log line\n" + `{"type":"result","subtype":"success","is_error":false,"duration_ms":1000,"num_turns":7,` +
		`"total_cost_usd":0.42,"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":50},` +
		`"modelUsage":{"claude-haiku-x":{"costUSD":0.01},"claude-opus-x":{"costUSD":0.41}}}`)
	var r AgentResult
	parseClaudeJSON(out, &r)
	if r.NumTurns == nil || *r.NumTurns != 7 || r.CostUSD == nil || *r.CostUSD != 0.42 {
		t.Fatalf("%+v", r)
	}
	if *r.InputTokens != 1110 || *r.OutputTokens != 50 {
		t.Fatalf("tokens %d/%d", *r.InputTokens, *r.OutputTokens)
	}
	if r.Model != "claude-opus-x,claude-haiku-x" {
		t.Fatalf("model %q", r.Model)
	}
	var empty AgentResult
	parseClaudeJSON([]byte("not json"), &empty)
	if empty.CostUSD != nil || empty.NumTurns != nil {
		t.Fatal("unknown values must stay nil")
	}
}

func TestClaudeArgs(t *testing.T) {
	cost := 2.5
	m := "opus"
	a := &ClaudeAgent{Cfg: (&ClaudeAgent{}).Cfg, MaxCostUSD: &cost}
	a.Cfg.PermissionMode = "acceptEdits"
	a.Cfg.AllowedTools = []string{"Read", "Edit"}
	a.Cfg.SettingSources = "project,local"
	a.Cfg.Model = &m
	got := strings.Join(a.Args(), " ")
	for _, want := range []string{"-p", "--output-format json", "--no-session-persistence", "--permission-mode acceptEdits",
		"--allowedTools Read,Edit", "--setting-sources project,local", "--model opus", "--max-budget-usd 2.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
}
