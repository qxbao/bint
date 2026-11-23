package workflow

import (
	"slices"
	"testing"
	"time"

	"bint/internal/affected"
	"bint/internal/task"
)

func TestGoTestFlags(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
		ok   bool
	}{
		{"go test -timeout 30m ./...", []string{"-timeout=30m"}, true},
		{"go test -timeout=30m -race -count 1 {test_dirs}", []string{"-timeout=30m", "-race", "-count=1"}, true},
		{"go test -run TestX -coverprofile c.out -p 2 ./...", []string{"-p=2"}, true},
		{"go test --tags=integration ./...", []string{"-tags=integration"}, true},
		{"go test ./...", nil, true},
		{"cd x && go test -timeout 30m ./...", nil, false},
		{"make test", nil, false},
		{"go test -run 'A|B' ./...", nil, false},
	}
	for _, c := range cases {
		got, ok := goTestFlags(c.cmd)
		if ok != c.ok || !slices.Equal(got, c.want) {
			t.Errorf("goTestFlags(%q) = %v, %v; muốn %v, %v", c.cmd, got, ok, c.want, c.ok)
		}
	}
}

func goSel() affected.Selection {
	return affected.Selection{Scope: affected.TierGraph, Commands: []affected.Command{
		{Runner: "go", Argv: []string{"go", "test", "./internal/a", "./internal/b"}},
		{Runner: "go", Argv: []string{"go", "test", "-count=1", "./internal/c"}},
		{Runner: "jest", Argv: []string{"npx", "jest", "x.test.js"}},
	}}
}

// Hồi quy: kiểm hẹp chạy `go test <gói>` không có -timeout của lệnh kiểm chứng nên gói chậm chết ở trần mặc định 10 phút.
func TestTestRunInheritsVerifyFlags(t *testing.T) {
	sel := goSel()
	ran, run := testRun(sel, "go test -timeout 30m -count 2 ./...", nil)
	if !run {
		t.Fatal("phải chạy")
	}
	want := "rc=0; (go test -timeout=30m -count=2 ./internal/a ./internal/b) || rc=1; (go test -timeout=30m -count=1 ./internal/c) || rc=1; " +
		"(npx jest x.test.js) || rc=1; exit $rc"
	if ran != want {
		t.Fatalf("ran =\n%s\nmuốn\n%s", ran, want)
	}
	if !slices.Equal(sel.Commands[0].Argv, []string{"go", "test", "./internal/a", "./internal/b"}) {
		t.Fatalf("không được sửa phần chọn gốc: %v", sel.Commands[0].Argv)
	}
	// Lệnh kiểm chứng không phải go test thuần, không có runner_args: giữ nguyên lệnh của bgraph.
	ran, _ = testRun(sel, "make test", nil)
	if want := "rc=0; (go test ./internal/a ./internal/b) || rc=1; (go test -count=1 ./internal/c) || rc=1; (npx jest x.test.js) || rc=1; exit $rc"; ran != want {
		t.Fatalf("ran =\n%s\nmuốn\n%s", ran, want)
	}
}

// workflow.verify.runner_args: runner khác go nối vào cuối; go có khoá thì dùng khoá thay cho cờ của verify.command, `go: []` tắt việc chép.
func TestTestRunRunnerArgs(t *testing.T) {
	sel := goSel()
	args := map[string][]string{"go": {"-timeout=1h", "-count", "1"}, "jest": {"--runInBand", "--config", "jest.ci.js"}}
	ran, _ := testRun(sel, "npm test", args)
	want := "rc=0; (go test -timeout=1h -count 1 ./internal/a ./internal/b) || rc=1; (go test -timeout=1h -count=1 ./internal/c) || rc=1; " +
		"(npx jest x.test.js --runInBand --config jest.ci.js) || rc=1; exit $rc"
	if ran != want {
		t.Fatalf("ran =\n%s\nmuốn\n%s", ran, want)
	}
	ran, _ = testRun(sel, "go test -timeout 30m ./...", map[string][]string{"go": {}})
	if want := "rc=0; (go test ./internal/a ./internal/b) || rc=1; (go test -count=1 ./internal/c) || rc=1; (npx jest x.test.js) || rc=1; exit $rc"; ran != want {
		t.Fatalf("go: [] phải tắt việc chép cờ:\n%s", ran)
	}
}

func TestGoFlagsTimeoutOf(t *testing.T) {
	cases := []struct {
		v    task.Verification
		want time.Duration
	}{
		{task.Verification{Command: "go test -timeout 30m ./..."}, 30 * time.Minute},
		{task.Verification{Command: "go test -timeout 30m ./...", Ran: "go test -timeout=30m ./internal/a"}, 30 * time.Minute},
		{task.Verification{Command: "go test -timeout 30m ./...", Ran: "rc=0; (go test -timeout=30m ./a) || rc=1; exit $rc"}, 30 * time.Minute},
		{task.Verification{Command: "go test ./..."}, triageDefaultTimeout},
		{task.Verification{Command: "make test"}, triageDefaultTimeout},
	}
	for _, c := range cases {
		if _, got := goFlagsTimeoutOf(c.v); got != c.want {
			t.Errorf("goFlagsTimeoutOf(%+v) = %v; muốn %v", c.v, got, c.want)
		}
	}
}
