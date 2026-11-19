package testutil

import "testing"

func TestTrimResultLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"PASS\n", ""},
		{"--- FAIL: TestX (0.00s)\n    x_test.go:1: boom\nFAIL\n", "--- FAIL: TestX (0.00s)\n    x_test.go:1: boom\n"},
		{"=== RUN   TestA\n\x16=== NAME  \n\x16PASS\n", "=== RUN   TestA\n\x16=== NAME  \n"},
		{"PASS\r\n", ""},
		{"panic: x\nexit\n", "panic: x\nexit\n"},
	}
	for _, c := range cases {
		if got := string(trimResultLine([]byte(c.in))); got != c.want {
			t.Errorf("trimResultLine(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}

func TestShardProcsSkipsFilteredRuns(t *testing.T) {
	t.Setenv(envShardChild, "")
	t.Setenv(envShardProcs, "4")
	if got := shardProcs([]string{"-test.v=test2json", "-test.timeout=10m"}); got != 4 {
		t.Fatalf("không cờ lọc: %d, muốn 4", got)
	}
	for _, a := range []string{"-test.run=^TestA$", "-test.skip=X", "-test.coverprofile=c.out", "-test.cpuprofile=p", "-test.bench=."} {
		if got := shardProcs([]string{a}); got != 0 {
			t.Errorf("%s: %d, muốn 0 (không chia)", a, got)
		}
	}
	t.Setenv(envShardProcs, "1")
	if got := shardProcs(nil); got != 0 {
		t.Errorf("%s=1: %d, muốn 0", envShardProcs, got)
	}
	t.Setenv(envShardProcs, "4")
	t.Setenv(envShardChild, "1")
	if got := shardProcs(nil); got != 0 {
		t.Errorf("tiến trình con: %d, muốn 0", got)
	}
}

func TestSplitTestlog(t *testing.T) {
	rest, p := splitTestlog([]string{"-test.v=true", "-test.testlogfile=C:/o/testlog.txt", "-test.paniconexit0"})
	if p != "C:/o/testlog.txt" || len(rest) != 2 {
		t.Fatalf("rest=%v path=%q", rest, p)
	}
}
