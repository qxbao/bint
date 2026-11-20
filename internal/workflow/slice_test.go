package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

func TestFailSet(t *testing.T) {
	goTest := "--- FAIL: TestGoal (0.00s)\n    --- FAIL: TestGoal/c2 (0.00s)\n    --- FAIL: TestGoal/c1 (0.00s)\n--- FAIL: TestOld (0.00s)\nFAIL\n"
	for _, tc := range []struct {
		name     string
		text     string
		exit     int
		timedOut bool
		want     []string
		measured bool
	}{
		{"mã thoát 0 là tập rỗng", "bất kỳ", 0, false, []string{}, true},
		{"quá giờ không đo được", goTest, 1, true, nil, false},
		{"lá, không lấy dòng cha", goTest, 1, false, []string{"TestGoal/c1", "TestGoal/c2", "TestOld"}, true},
		{"hỏng biên dịch", "FAIL bint/x [build failed]\n--- FAIL: TestA\n", 1, false, nil, false},
		{"hỏng khởi tạo", "FAIL bint/x [setup failed]\n", 1, false, nil, false},
		{"panic", "--- FAIL: TestA (0.00s)\npanic: boom\n", 2, false, nil, false},
		{"thoát khác 0 không có dòng FAIL", "lỗi lạ\n", 1, false, nil, false},
		{"cha và con trùng tên một phần", "--- FAIL: TestA\n--- FAIL: TestAB\n    --- FAIL: TestA/x\n", 1, false, []string{"TestA/x", "TestAB"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := failSet(tc.text, tc.exit, tc.timedOut)
			if ok != tc.measured || !slices.Equal(got, tc.want) {
				t.Errorf("failSet = %v, %v; muốn %v, %v", got, ok, tc.want, tc.measured)
			}
		})
	}
}

func TestFailSetOfReadsLog(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "goal-1.log")
	if err := os.WriteFile(log, []byte("$ sh -c x\n    --- FAIL: TestGoal/c1 (0.00s)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := failSetOf(&task.Verification{Log: log, ExitCode: 1})
	if !ok || !slices.Equal(got, []string{"TestGoal/c1"}) {
		t.Errorf("failSetOf = %v, %v", got, ok)
	}
	if _, ok := failSetOf(&task.Verification{Log: filepath.Join(dir, "không-có.log"), ExitCode: 1}); ok {
		t.Errorf("log không đọc được mà mã thoát 1 phải là không đo được")
	}
	if got, ok := failSetOf(&task.Verification{Log: "", ExitCode: 0, Passed: true}); !ok || len(got) != 0 {
		t.Errorf("mã thoát 0 không cần log: %v, %v", got, ok)
	}
	if _, ok := failSetOf(nil); ok {
		t.Errorf("nil là không đo được")
	}
}

func TestSliceVerdict(t *testing.T) {
	c := func(n ...string) []string { return n }
	for _, tc := range []struct {
		name     string
		prev     task.SliceGreen
		red      []string
		measured bool
		oldOK    bool
		want     bool
		reason   string
	}{
		{"giảm thực sự", task.SliceGreen{Measured: true, Red: c("a", "b", "c")}, c("b", "c"), true, true, true, "3→2"},
		{"giảm về 0", task.SliceGreen{Measured: true, Red: c("a")}, c(), true, true, true, "1→0"},
		{"không giảm", task.SliceGreen{Measured: true, Red: c("a", "b")}, c("a", "b"), true, true, false, "không giảm"},
		{"giảm số lượng nhưng ô xanh chuyển đỏ", task.SliceGreen{Measured: true, Red: c("a", "b", "c")}, c("a", "z"), true, true, false, "chuyển đỏ: z"},
		{"điểm xanh 0 ô đỏ phải giữ 0", task.SliceGreen{Measured: true}, c("x"), true, true, false, "0 ô đỏ"},
		{"điểm xanh 0 ô đỏ giữ 0", task.SliceGreen{Measured: true}, c(), true, true, true, "giữ 0"},
		{"điểm xanh chưa đo được, nay đo được", task.SliceGreen{}, c("a", "b"), true, true, true, "chưa đo được"},
		{"lát không đo được", task.SliceGreen{Measured: true, Red: c("a")}, nil, false, true, false, "không đo được"},
		{"điểm xanh chưa đo, lát cũng không", task.SliceGreen{}, nil, false, true, false, "không đo được"},
		{"test cũ đỏ thắng số đo", task.SliceGreen{Measured: true, Red: c("a", "b")}, c("a"), true, false, false, "test cũ đỏ: TestOld"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := sliceVerdict(&tc.prev, tc.red, tc.measured, tc.oldOK, "test cũ đỏ: TestOld")
			if ok != tc.want || !strings.Contains(why, tc.reason) {
				t.Errorf("sliceVerdict = %v %q, muốn %v chứa %q", ok, why, tc.want, tc.reason)
			}
		})
	}
}

func TestOldTestsGreen(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	goal := map[string]bool{"TestGoal": true}
	mixed := write("mixed.log", "    --- FAIL: TestGoal/c1\n--- FAIL: TestOld\n")
	onlyGoal := write("goal.log", "    --- FAIL: TestGoal/c1\n")
	build := write("build.log", "FAIL x [build failed]\n")
	for _, tc := range []struct {
		name string
		v    *task.Verification
		want bool
		why  string
	}{
		{"không có lần kiểm chứng", nil, true, ""},
		{"qua", &task.Verification{Passed: true}, true, ""},
		{"scope none", &task.Verification{Scope: "none"}, true, ""},
		{"chỉ ô mục tiêu đỏ", &task.Verification{Log: onlyGoal, ExitCode: 1}, true, ""},
		{"test cũ đỏ", &task.Verification{Log: mixed, ExitCode: 1}, false, "TestOld"},
		{"không đo được", &task.Verification{Log: build, ExitCode: 1}, false, "không đo được"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := oldTestsGreen(tc.v, goal, nil)
			if ok != tc.want || !strings.Contains(why, tc.why) {
				t.Errorf("oldTestsGreen = %v %q, muốn %v chứa %q", ok, why, tc.want, tc.why)
			}
		})
	}
	// Test cũ đỏ người đã chấp nhận (--slice keep ở lát trước) không giữ lát lại; test cũ MỚI đỏ thì vẫn giữ.
	mv := &task.Verification{Log: mixed, ExitCode: 1}
	red, _ := failSetOf(mv)
	acc := oldRedOf(red, goal)
	if ok, why := oldTestsGreen(mv, goal, acc); !ok {
		t.Errorf("test cũ đỏ đã chấp nhận vẫn giữ lát: %q", why)
	}
	if len(acc) > 0 {
		if ok, _ := oldTestsGreen(mv, goal, acc[1:]); ok {
			t.Errorf("test cũ mới đỏ (ngoài tập đã chấp nhận) phải giữ lát")
		}
	}
}

func TestSliceSummaryAndBlock(t *testing.T) {
	tk := &task.Task{}
	tk.State.Slices = []task.Slice{
		{N: 1, Text: "A một — AC-1", Status: task.SliceAccepted},
		{N: 2, Text: "B hai — AC-2", Status: task.SliceDiscarded, Reason: "ô đỏ 3→3 không giảm", Backup: "refs/bint/tasks/X/slice-2-discarded"},
		{N: 3, Text: "C ba — AC-3", Status: task.SliceActive},
		{N: 4, Text: "D bốn — AC-4", Status: task.SlicePending},
	}
	s := sliceSummary(tk)
	for _, want := range []string{"đã chốt 1/4", "đã bỏ: lát 2/4", "ô đỏ 3→3 không giảm", "slice-2-discarded", "chưa làm: lát 3/4", "lát 4/4"} {
		if !strings.Contains(s, want) {
			t.Errorf("sliceSummary thiếu %q: %s", want, s)
		}
	}
	b := sliceBlock(tk)
	for _, want := range []string{"lát 3/4", "C ba — AC-3", "Đã chốt, đừng làm lại: lát 1/4", "Đã bỏ", "Lát sau, đừng làm trước: lát 4/4"} {
		if !strings.Contains(b, want) {
			t.Errorf("sliceBlock thiếu %q: %s", want, b)
		}
	}
	if sliceBlock(&task.Task{}) != "" || sliceSummary(&task.Task{}) != "" {
		t.Errorf("không chia lát thì không có đề bài/tóm tắt lát")
	}
}
