package testreport

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func names(ts []Test) []string {
	out := []string{}
	for _, t := range ts {
		out = append(out, t.String())
	}
	return out
}

func TestGoTextFailuresPerPackage(t *testing.T) {
	log := strings.Join([]string{
		"--- FAIL: TestA (0.01s)",
		"    a_test.go:10: sai",
		"--- FAIL: TestB (0.00s)",
		"    --- FAIL: TestB/sub (0.00s)",
		"FAIL",
		"FAIL\tbint/internal/a\t1.250s",
		"ok  \tbint/internal/b\t0.300s",
		"ok  \tbint/internal/c\t(cached)",
		"?   \tbint/internal/d\t[no test files]",
		"FAIL\tbint/internal/e [build failed]",
		"FAIL",
	}, "\r\n")
	if !(GoText{}).Detect(log) {
		t.Fatal("không nhận ra log go test")
	}
	r := Parse(log)
	if r.Format != "gotest" || !r.Measured {
		t.Fatalf("format %q measured %v", r.Format, r.Measured)
	}
	if got, want := names(r.Failed), []string{"bint/internal/a TestA", "bint/internal/a TestB/sub"}; !reflect.DeepEqual(got, want) {
		t.Errorf("failed = %v, muốn %v", got, want)
	}
	if !reflect.DeepEqual(r.BuildFailed, []string{"bint/internal/e"}) {
		t.Errorf("build failed = %v", r.BuildFailed)
	}
	if len(r.Packages) != 3 || r.Packages[0].Duration != 1250*time.Millisecond || r.Packages[0].Passed || !r.Packages[2].Cached {
		t.Errorf("packages = %+v", r.Packages)
	}
	if r.Complete() {
		t.Error("có gói lỗi build mà vẫn Complete")
	}
	if fresh := r.FailedIn([]Test{{Name: "TestA"}}); len(fresh) != 1 || fresh[0].Name != "TestB/sub" {
		t.Errorf("FailedIn = %v", fresh)
	}
}

// Đợt lịch sử: 21 lần kiểm chứng đỏ chỉ vì `panic: test timed out after 10m0s`; phải tách khỏi test đỏ thật.
func TestGoTextTimeoutIsNotFailure(t *testing.T) {
	log := strings.Join([]string{
		"ok  \tbint/internal/app\t40.1s",
		"panic: test timed out after 10m0s",
		"\trunning tests:",
		"\t\tTestFlowGoldenRunnerBehaviour (9m58s)",
		"\t\tTestSlow/case (1m2s)",
		"",
		"goroutine 1 [running]:",
		"testing.(*M).startAlarm.func1()",
		"--- FAIL: TestSlow (600.00s)",
		"FAIL\tbint/internal/workflow\t600.123s",
		"FAIL",
	}, "\n")
	r := Parse(log)
	if !r.OnlyTimeouts() {
		t.Fatalf("muốn chỉ hết giờ: %+v", r)
	}
	if got, want := names(r.TimedOut), []string{"bint/internal/workflow TestFlowGoldenRunnerBehaviour", "bint/internal/workflow TestSlow/case"}; !reflect.DeepEqual(got, want) {
		t.Errorf("timed out = %v, muốn %v", got, want)
	}
	if len(r.Panicked) != 0 {
		t.Errorf("hết giờ bị tính là panic: %v", r.Panicked)
	}
	if slow := r.Slow(5 * time.Minute); len(slow) != 1 || slow[0].Name != "bint/internal/workflow" {
		t.Errorf("slow = %+v", slow)
	}
}

func TestGoTextOtherPanic(t *testing.T) {
	r := Parse("--- FAIL: TestX (0.00s)\npanic: runtime error: index out of range [recovered]\nFAIL\tbint/x\t0.1s\n")
	if !reflect.DeepEqual(r.Panicked, []string{"bint/x"}) || r.Complete() || r.OnlyTimeouts() {
		t.Errorf("panic: %+v", r)
	}
}

func TestParseUnknownFormat(t *testing.T) {
	if r := Parse("Ran 3 tests\nFAILED (failures=1)\n"); r.Measured {
		t.Errorf("log lạ mà Measured: %+v", r)
	}
}

func TestGoJSONInterleavedPackages(t *testing.T) {
	ev := func(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }
	lines := []string{
		ev(map[string]any{"Action": "run", "Package": "p/a", "Test": "TestA"}),
		ev(map[string]any{"Action": "run", "Package": "p/b", "Test": "TestB"}),
		ev(map[string]any{"Action": "output", "Package": "p/a", "Test": "TestA", "Output": "--- FAIL: TestA (0.00s)\n"}),
		ev(map[string]any{"Action": "output", "Package": "p/b", "Test": "TestB", "Output": "--- PASS: TestB (0.00s)\n"}),
		ev(map[string]any{"Action": "fail", "Package": "p/a", "Test": "TestA", "Elapsed": 0}),
		ev(map[string]any{"Action": "output", "Package": "p/b", "Output": "ok  \tp/b\t2.5s\n"}),
		ev(map[string]any{"Action": "pass", "Package": "p/b", "Elapsed": 2.5}),
		ev(map[string]any{"Action": "output", "Package": "p/a", "Output": "FAIL\n"}),
		ev(map[string]any{"Action": "output", "Package": "p/a", "Output": "FAIL\tp/a\t0.7s\n"}),
		ev(map[string]any{"Action": "fail", "Package": "p/a", "Elapsed": 0.7}),
		ev(map[string]any{"Action": "fail", "Package": "p/c", "Elapsed": 0, "FailedBuild": "p/c"}),
	}
	r := Parse(strings.Join(lines, "\n"))
	if r.Format != "gotest-json" {
		t.Fatalf("format = %q", r.Format)
	}
	if got := names(r.Failed); !reflect.DeepEqual(got, []string{"p/a TestA"}) {
		t.Errorf("failed = %v", got)
	}
	if !reflect.DeepEqual(r.BuildFailed, []string{"p/c"}) {
		t.Errorf("build failed = %v", r.BuildFailed)
	}
	if len(r.Packages) != 2 || r.Packages[0].Name != "p/a" || r.Packages[0].Duration != 700*time.Millisecond || r.Packages[1].Duration != 2500*time.Millisecond {
		t.Errorf("packages = %+v", r.Packages)
	}
}

func TestGoRerunCommand(t *testing.T) {
	got := GoRerunCommand([]string{"-race", "-count=3", "-timeout=10m"}, []Test{
		{Package: "bint/b", Name: "TestY/sub"}, {Package: "bint/a", Name: "TestX"}, {Package: "bint/b", Name: "TestY/other"},
	}, 20*time.Minute)
	want := "go test -count=1 -race -timeout=20m0s -run '^(TestX)$' bint/a && go test -count=1 -race -timeout=20m0s -run '^(TestY)$' bint/b"
	if got != want {
		t.Errorf("GoRerunCommand =\n%s\nmuốn\n%s", got, want)
	}
	if GoRerunCommand(nil, []Test{{Name: "TestX"}}, 0) != "" {
		t.Error("test không rõ gói mà vẫn dựng lệnh")
	}
}
