package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Kiểm tra mục tiêu của tác vụ 01M7VM1BY6PR7FGV36EK8TA5BW (luồng B′, giai đoạn 3: đảo kiểm chứng): xem requirement.md mục "Kiểm tra mục tiêu".
// Mỗi ô (loại, nơi) báo riêng bằng t.Errorf với nhãn Lx×Ny.

// fakeClock là đồng hồ giả của Env: tiến theo lệnh của test.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// bvMilestone là một mốc của b-feat giả: sửa tệp, rồi (check) gọi Env.Check, (wait) chờ kiểm nền của mốc xong.
type bvMilestone struct {
	edit  func(h *harness)
	wait  bool
	check bool
}

// bvRun chạy b-do (B′ hoặc classic) với b-feat giả làm các mốc ms. Module Go (a, c) đã commit; verify.command đếm mỗi lần chạy vào
// tệp đếm (ghi thư mục chạy); affected_command ghi aff.txt và đỏ khi red.txt tồn tại. Đồng hồ giả tiến 5 phút trước mỗi mốc và 15 phút sau.
func bvRun(t *testing.T, variant string, ms []bvMilestone) (h *harness, res *Result, counter string, checks []CheckResult) {
	t.Helper()
	old := bgPollInterval
	bgPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { bgPollInterval = old })
	counter = filepath.ToSlash(filepath.Join(t.TempDir(), "count.txt"))
	clock := &fakeClock{t: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)}
	h = bpHarness(t, bpOpts{variant: variant, goal: func(context.Context, *Env, string) error { return nil },
		cfg: func(h *harness) {
			full := "test -f done.txt && echo \"$PWD\" >> " + counter
			h.cfg.Eval.TestCommand = &full
			h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeAffected,
				AffectedCommand: "echo AFFECTED {test_dirs} >> aff.txt; test ! -f red.txt"}
		},
		feat: func(ctx context.Context, h *harness, e *Env, id string) error {
			e.Now = clock.Now // Env của agent giả là bản riêng: dùng cùng đồng hồ giả để At của kiểm hẹp tính theo mốc
			waits := 0
			for _, m := range ms {
				clock.Add(5 * time.Minute)
				m.edit(h)
				if m.check {
					_, cr, err := e.Check(ctx, id)
					if err != nil {
						return err
					}
					checks = append(checks, cr)
				}
				if m.wait {
					waits++
					waitBG(t, h, id, waits)
				}
				clock.Add(15 * time.Minute)
			}
			return nil
		}})
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	h.write("c/c.go", "package c\n\nfunc C() {}\n")
	h.write("c/c_test.go", "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	r := h.runner()
	r.Env.Now = clock.Now
	var err error
	res, err = r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func counted(counter string) int {
	b, err := os.ReadFile(counter)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(b)))
}

func waitBG(t *testing.T, h *harness, id string, n int) {
	t.Helper()
	for i := 0; i < 6000; i++ { // hạn chờ chỉ để chặn treo (máy bận)
		if len(h.load(id).State.BackgroundVerifications) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("kiểm nền chưa đạt %d lần", n)
}

func editA(v int) func(h *harness) {
	return func(h *harness) {
		h.write("a/a.go", fmt.Sprintf("package a\n\nfunc A() int { return %d }\n", v))
		h.write("done.txt", "ok\n")
	}
}

func TestBPrimeBackgroundVerify(t *testing.T) {
	// L1/L4 (kiểm hẹp xanh, kiểm nền xanh trên cây cuối) × N1/N2/N3: 3 mốc xanh.
	t.Run("xanh trên cây cuối", func(t *testing.T) {
		var h *harness
		var counter, id string
		var res *Result
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: func(h *harness) { h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n") }, check: true, wait: true},
			{edit: editA(3), check: true},
		}
		h, res, counter, _ = bvRun(t, config.VariantBPrime, ms)
		id = res.TaskID
		tk := h.load(id)
		if len(tk.State.Checks) != 3 {
			t.Errorf("(L1×N1) state.checks = %d, muốn 3", len(tk.State.Checks))
		}
		if n := len(tk.State.BackgroundVerifications); n < 3 {
			t.Errorf("(L4×N2) state.background_verifications = %d, muốn ≥ 3", n)
		}
		for i, c := range tk.State.Checks {
			if c.Scope != "graph" || !c.Passed || c.Tree == "" {
				t.Errorf("(L1×N1) check %d: %+v", i, c)
			}
		}
		if n := len(tk.State.Verifications); n != 1 || !strings.Contains(tk.State.Verifications[0].Reason, "chạy nền") || !tk.State.Verifications[0].Passed {
			t.Errorf("(L4×N3) state.verifications muốn đúng 1 lần dùng lại kiểm nền, có %+v", tk.State.Verifications)
		}
		if got := counted(counter); got != len(tk.State.BackgroundVerifications) {
			t.Errorf("(L4×N3) verify.command chạy %d lần, muốn %d (đúng số lần nền, 0 lần tuần tự)", got, len(tk.State.BackgroundVerifications))
		}
	})

	// L2 (kiểm hẹp đỏ) × N1/N2: kiểm hẹp đỏ không khởi chạy kiểm nền cho cây đó.
	t.Run("kiểm hẹp đỏ", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: func(h *harness) {
				h.write("red.txt", "x\n")
				h.write("c/c.go", "package c\n\nfunc C() int { return 2 }\n")
			}, check: true},
			{edit: func(h *harness) { os.Remove(filepath.Join(h.root, "red.txt")) }, check: true},
		}
		h, res, _, _ := bvRun(t, config.VariantBPrime, ms)
		tk := h.load(res.TaskID)
		if len(tk.State.Checks) != 3 || tk.State.Checks[1].Passed {
			t.Fatalf("(L2×N1) checks: %+v", tk.State.Checks)
		}
		for _, b := range tk.State.BackgroundVerifications {
			if b.Tree == tk.State.Checks[1].Tree {
				t.Errorf("(L2×N2) kiểm nền chạy trên cây của kiểm hẹp đỏ")
			}
		}
	})

	// L3 (thay đổi buộc nới full) × N1/N3: kiểm hẹp không chạy toàn repo, kiểm chứng đi đường cũ.
	t.Run("nới full", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: func(h *harness) { h.write("go.mod", "module example.com/m\n\ngo 1.22\n") }, check: true},
		}
		h, res, counter, crs := bvRun(t, config.VariantBPrime, ms)
		tk := h.load(res.TaskID)
		if len(crs) != 2 || crs[1].Skipped == "" || !strings.Contains(crs[1].Skipped, "full") {
			t.Fatalf("(L3×N1) kiểm hẹp mốc full phải bị bỏ qua: %+v", crs)
		}
		if len(tk.State.Checks) != 1 {
			t.Errorf("(L3×N1) state.checks = %d, muốn 1 (mốc full không ghi)", len(tk.State.Checks))
		}
		if n := len(tk.State.Verifications); n != 1 || strings.Contains(tk.State.Verifications[0].Reason, "chạy nền") || tk.State.Verifications[0].Scope != "full" {
			t.Errorf("(L3×N3) cây cuối đổi: phải kiểm chứng tuần tự full, có %+v", tk.State.Verifications)
		}
		if got, want := counted(counter), len(tk.State.BackgroundVerifications)+1; got != want {
			t.Errorf("(L3×N3) verify.command chạy %d lần, muốn %d (nền + 1 tuần tự)", got, want)
		}
	})

	// L5 (cây đổi sau kiểm nền) × N3: sửa thêm sau mốc cuối mà không kiểm → không dùng lại kiểm nền.
	t.Run("cây đổi sau kiểm nền", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: editA(9)},
		}
		h, res, _, _ := bvRun(t, config.VariantBPrime, ms)
		tk := h.load(res.TaskID)
		if vs := tk.State.Verifications; len(vs) == 0 || strings.Contains(vs[0].Reason, "chạy nền") {
			t.Errorf("(L5×N3) cây đã đổi: phải kiểm chứng tuần tự trước, có %d lần, đầu: %q", len(tk.State.Verifications), tk.State.Verifications[0].Reason)
		}
	})

	// L6 × N4/N5: classic không có kiểm nền, kiểm hẹp, khối đề bài.
	t.Run("classic không đổi", func(t *testing.T) {
		ms := []bvMilestone{{edit: editA(2), check: false}}
		h, res, _, _ := bvRun(t, config.VariantClassic, ms)
		tk := h.load(res.TaskID)
		if len(tk.State.BackgroundVerifications) != 0 || len(tk.State.Checks) != 0 {
			t.Errorf("(L6×N4) classic: nền=%d checks=%d, muốn 0", len(tk.State.BackgroundVerifications), len(tk.State.Checks))
		}
		if strings.Contains(h.prompts[StepFeat], "kiểm hẹp theo mốc") {
			t.Errorf("(L6×N5) đề bài classic không được có khối kiểm hẹp")
		}
	})
}

var pctRE = regexp.MustCompile(`ở (\d+) % ngân sách`)

func TestBPrimeEarlyVerification(t *testing.T) {
	ms := []bvMilestone{
		{edit: editA(2), check: true, wait: true},
		{edit: func(h *harness) { h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n") }, check: true, wait: true},
		{edit: editA(3), check: true},
	}
	h, res, _, _ := bvRun(t, config.VariantBPrime, ms)
	tk := h.load(res.TaskID)
	if !strings.Contains(h.prompts[StepFeat], "bint task check "+res.TaskID) {
		t.Errorf("(L1×N5) đề bài b-feat B′ thiếu khối kiểm hẹp theo mốc")
	}
	// Mốc 1 ở +5 phút, mốc 2 ở +25 phút: kết quả đầu tiên phải đến trước mốc 2 và ≤ 34 % ngân sách (60 phút).
	if len(tk.State.Checks) < 2 {
		t.Fatalf("(L1×N1) cần ≥ 2 kiểm hẹp: %d", len(tk.State.Checks))
	}
	first, _ := time.Parse(time.RFC3339, tk.State.Checks[0].At)
	second, _ := time.Parse(time.RFC3339, tk.State.Checks[1].At)
	if !first.Before(second) {
		t.Errorf("(L1×N1) kết quả đầu (%s) phải trước mốc 2 (%s)", first, second)
	}
	var pct int = -1
	for _, n := range res.Notes {
		if m := pctRE.FindStringSubmatch(n); m != nil {
			pct, _ = strconv.Atoi(m[1])
		}
	}
	if pct < 0 || pct > 34 {
		t.Errorf("(L1×N4) ghi chú %% ngân sách của kết quả đầu = %d (muốn ≤ 34); notes=%v", pct, res.Notes)
	}
	if n := len(tk.State.BackgroundVerifications); n < 1 {
		t.Errorf("(L4×N2) cần ≥ 1 kiểm nền, có %d", n)
	}
	var _ task.Verification
}
