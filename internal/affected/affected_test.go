package affected

import "testing"

func TestScriptQuotingAndExitCode(t *testing.T) {
	s := Selection{Scope: TierGraph, Commands: []Command{
		{Runner: "pytest", Project: "py dir", Argv: []string{"python", "-m", "pytest", "tests/it's.py"}},
		{Runner: "go", Project: ".", Argv: []string{"go", "test", "./a"}, Env: []string{"GOFLAGS=-count=1 -v"}},
	}}
	got := s.Script()
	want := `rc=0; (cd 'py dir' && python -m pytest 'tests/it'\''s.py') || rc=1; (GOFLAGS='-count=1 -v' go test ./a) || rc=1; exit $rc`
	if got != want {
		t.Fatalf("script:\n got %s\nwant %s", got, want)
	}
}
