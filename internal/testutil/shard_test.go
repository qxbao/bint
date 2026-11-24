package testutil

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

func TestCompactTestlogDropsTempAndDuplicates(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "mod", "pkg")
	tmp := filepath.Join(root, "tmp")
	scratch := filepath.Join(tmp, "TestX123", "repo", "a.txt")
	c := newTestlogCompactor(mod, tmp)
	batch := testlogMagic +
		"getenv HOME\n" +
		"getenv HOME\n" +
		"open testdata/golden.txt\n" +
		"stat " + scratch + "\n" +
		"open " + scratch + "\n" +
		"chdir " + filepath.Join(tmp, "TestX123") + "\n" +
		"stat repo/b.txt\n" +
		"chdir " + mod + "\n" +
		"open testdata/golden.txt\n"
	c.add(batch)
	c.add(batch) // mẻ thứ hai trùng hoàn toàn: không thêm dòng nào
	want := testlogMagic +
		"chdir " + mod + "\n" +
		"getenv HOME\n" +
		"open " + filepath.Join(mod, "testdata", "golden.txt") + "\n" +
		"chdir " + filepath.Join(tmp, "TestX123") + "\n"
	if got := c.buf.String(); got != want {
		t.Fatalf("nhật ký rút gọn:\n%s\nmuốn:\n%s", got, want)
	}
}

func TestCompactTestlogKeepsTempWhenModuleInTemp(t *testing.T) {
	tmp := t.TempDir()
	mod := filepath.Join(tmp, "mod")
	c := newTestlogCompactor(mod, tmp)
	c.add(testlogMagic + "open " + filepath.Join(tmp, "other", "x.txt") + "\n")
	if !strings.Contains(c.buf.String(), filepath.Join(tmp, "other", "x.txt")) {
		t.Fatalf("module nằm trong thư mục tạm thì không được bỏ dòng: %q", c.buf.String())
	}
}

func TestUnderDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "a")
	cases := []struct {
		path string
		want bool
	}{
		{base, true},
		{filepath.Join(base, "b", "c"), true},
		{filepath.Join(filepath.Dir(base), "ab"), false},
		{filepath.Dir(base), false},
	}
	for _, c := range cases {
		if got := underDir(c.path, base); got != c.want {
			t.Errorf("underDir(%q, %q) = %v, muốn %v", c.path, base, got, c.want)
		}
	}
}

func TestLongestFirstPutsUnmeasuredThenSlowest(t *testing.T) {
	known := map[string]float64{"TestA": 1, "TestB": 30, "TestC": 5}
	got := longestFirst([]string{"TestA", "TestNew1", "TestB", "TestC", "TestNew2"}, known)
	want := []string{"TestNew1", "TestNew2", "TestB", "TestC", "TestA"}
	if !slices.Equal(got, want) {
		t.Fatalf("longestFirst = %v, muốn %v", got, want)
	}
}
