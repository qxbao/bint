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
	edit   func(h *harness)
	wait   bool
	check  bool
	pause  time.Duration // chờ sau khi sửa (cho kiểm nền kịp chạy)
	cancel bool          // huỷ ngữ cảnh của Run sau khi sửa
}

// bvRun chạy b-do (B′ hoặc classic) với b-feat giả làm các mốc ms. Module Go (a, c) đã commit; verify.command đếm mỗi lần chạy vào
// tệp đếm (ghi thư mục chạy); affected_command ghi aff.txt và đỏ khi red.txt tồn tại. Đồng hồ giả tiến 5 phút trước mỗi mốc và 15 phút sau.
func bvRun(t *testing.T, variant string, ms []bvMilestone) (h *harness, res *Result, counter string, checks []CheckResult) {
	t.Helper()
	return bvRunOpt(t, bvOpts{variant: variant}, ms)
}

// bvOpts chỉnh bvRun: needLocal làm test Go đòi tệp local.env bị gitignore (có ở cây chính, không có ở checkout sạch: kiểm nền đỏ, kiểm hẹp xanh),
// slow làm test Go chậm và ghi S/E vào <counter>.log ở lần chạy nền (đo hai lần chạy chồng nhau).
type bvOpts struct {
	variant   string
	needLocal bool
	slow      bool
}

// bvTestFile là tệp test Go của gói pkg cho bộ kiểm nền: đỏ khi tệp cờ BV_RED tồn tại; với BV_NEEDLOCAL đỏ khi thiếu local.env ở gốc module; ở lần
// chạy nền (gốc module khác BV_ROOT) ghi gốc module vào BV_COUNTER, và với BV_SLOW ghi S/E kèm pid vào <BV_COUNTER>.log quanh một lần ngủ 0,2 giây.
func bvTestFile(pkg, name string) string {
	return "package " + pkg + `

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ` + name + `(t *testing.T) {
	if _, err := os.Stat(os.Getenv("BV_RED")); err == nil {
		t.Fatal("đỏ")
	}
	wd, _ := os.Getwd()
	root := filepath.Dir(wd)
	if os.Getenv("BV_NEEDLOCAL") != "" {
		if _, err := os.Stat(filepath.Join(root, "local"+".env")); err != nil {
			t.Fatal("thiếu tệp cục bộ")
		}
	}
	if strings.EqualFold(filepath.Clean(root), filepath.Clean(os.Getenv("BV_ROOT"))) {
		return
	}
	appendLine := func(p, s string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		fmt.Fprintln(f, s)
	}
	c := os.Getenv("BV_COUNTER")
	if os.Getenv("BV_SLOW") != "" {
		appendLine(c+".log", fmt.Sprint("S", os.Getpid()))
		time.Sleep(200 * time.Millisecond)
		defer appendLine(c+".log", fmt.Sprint("E", os.Getpid()))
	}
	appendLine(c, filepath.ToSlash(root))
}
`
}

// bvSetRed bật/tắt cờ làm test Go đỏ (ngoài repo, không phải thay đổi của cây).
func bvSetRed(t *testing.T, on bool) {
	p := os.Getenv("BV_RED")
	if on {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	os.Remove(p)
}

func bvRunOpt(t *testing.T, o bvOpts, ms []bvMilestone) (h *harness, res *Result, counter string, checks []CheckResult) {
	t.Helper()
	variant := o.variant
	runCtx, cancelRun := context.WithCancel(context.Background())
	t.Cleanup(cancelRun)
	old, oldQuiet := bgPollInterval, autoCheckQuiet
	bgPollInterval = 10 * time.Millisecond
	autoCheckQuiet = 24 * time.Hour // các ca này đếm đúng số lần kiểm hẹp do agent gọi; kiểm hẹp tự động có test riêng (TestAutoCheckWhenTreeQuiet)
	t.Cleanup(func() { bgPollInterval, autoCheckQuiet = old, oldQuiet })
	counter = filepath.ToSlash(filepath.Join(t.TempDir(), "count.txt"))
	// Test Go giả (bvTestFile) ghi vào counter thư mục gốc của mỗi lần chạy NỀN (checkout sạch ngoài cây chính); kiểm hẹp và kiểm chứng tuần tự
	// chạy trong cây chính nên không đếm. -p=1: các gói của một lần chạy nối tiếp nhau (để log S/E đo chồng lấn giữa các lần nền).
	t.Setenv("GOFLAGS", "-count=1 -p=1")
	t.Setenv("BV_COUNTER", counter)
	t.Setenv("BV_RED", filepath.Join(t.TempDir(), "red"))
	t.Setenv("BV_SLOW", map[bool]string{true: "1"}[o.slow])
	t.Setenv("BV_NEEDLOCAL", map[bool]string{true: "1"}[o.needLocal])
	clock := &fakeClock{t: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)}
	h = bpHarness(t, bpOpts{variant: variant, goal: func(context.Context, *Env, string) error { return nil },
		cfg: func(h *harness) {
			full := "test -f done.txt"
			h.cfg.Eval.TestCommand = &full
		},
		feat: func(ctx context.Context, h *harness, e *Env, id string) error {
			e.Now = clock.Now // Env của agent giả là bản riêng: dùng cùng đồng hồ giả để At của kiểm hẹp tính theo mốc
			waits := 0
			for _, m := range ms {
				clock.Add(5 * time.Minute)
				m.edit(h)
				if m.pause > 0 {
					time.Sleep(m.pause)
				}
				if m.cancel {
					cancelRun()
				}
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
	t.Setenv("BV_ROOT", h.root)
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", bvTestFile("a", "TestA"))
	h.write("c/c.go", "package c\n\nfunc C() {}\n")
	h.write("c/c_test.go", bvTestFile("c", "TestC"))
	h.write(".gitignore", "done.txt\nlocal.env\n")
	h.write("local.env", "x\n") // bị gitignore: có ở cây chính, không có ở checkout sạch của kiểm nền
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	r := h.runner()
	r.Env.Now = clock.Now
	var err error
	res, err = r.Run(runCtx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// agentChecks là các lần kiểm hẹp do agent gọi (bỏ lần Bint chạy làm cổng lõi sau lượt b-feat: check.go coreGate).
func agentChecks(tk *task.Task) []task.Verification {
	var out []task.Verification
	for _, c := range tk.State.Checks {
		if !strings.HasPrefix(c.Reason, checkGateReason) {
			out = append(out, c)
		}
	}
	return out
}

// counted: số lần chạy nền (mỗi lần chạy nền dùng một checkout tạm riêng; các gói của cùng lần ghi cùng gốc).
func counted(counter string) int {
	b, err := os.ReadFile(counter)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	for _, l := range strings.Fields(string(b)) {
		seen[l] = true
	}
	return len(seen)
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
		if len(agentChecks(tk)) != 3 {
			t.Errorf("(L1×N1) state.checks = %d, muốn 3", len(agentChecks(tk)))
		}
		if n := len(tk.State.BackgroundVerifications); n < 3 {
			t.Errorf("(L4×N2) state.background_verifications = %d, muốn ≥ 3", n)
		}
		for i, c := range agentChecks(tk) {
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
				bvSetRed(t, true)
				h.write("c/c.go", "package c\n\nfunc C() int { return 2 }\n")
			}, check: true},
			{edit: func(h *harness) { bvSetRed(t, false); editA(5)(h) }, check: true},
		}
		h, res, _, _ := bvRun(t, config.VariantBPrime, ms)
		tk := h.load(res.TaskID)
		if len(agentChecks(tk)) != 3 || agentChecks(tk)[1].Passed {
			t.Fatalf("(L2×N1) checks: %+v", agentChecks(tk))
		}
		for _, b := range tk.State.BackgroundVerifications {
			if b.Tree == agentChecks(tk)[1].Tree {
				t.Errorf("(L2×N2) kiểm nền chạy trên cây của kiểm hẹp đỏ")
			}
		}
	})

	// L3 (manifest đổi: bgraph nới tới cả module) × N1/N3: kiểm hẹp vẫn chạy (cả module), không bị bỏ qua; kiểm chứng của tác vụ xanh.
	t.Run("nới module", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: func(h *harness) { h.write("go.mod", "module example.com/m\n\ngo 1.22\n") }, check: true},
		}
		h, res, _, crs := bvRun(t, config.VariantBPrime, ms)
		tk := h.load(res.TaskID)
		if len(crs) != 2 || crs[1].Skipped != "" || crs[1].Check.Scope != "module" || !crs[1].Check.Passed {
			t.Fatalf("(L3×N1) kiểm hẹp sau khi go.mod đổi phải chạy cả module và xanh: %+v", crs)
		}
		if vs := tk.State.Verifications; len(vs) != 1 || !vs[0].Passed || !vs[0].Complete() {
			t.Errorf("(L3×N3) kiểm chứng của tác vụ: %+v", vs)
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
		if vs := tk.State.Verifications; len(vs) == 0 {
			t.Errorf("(L5×N3) cây đã đổi: phải có kiểm chứng tuần tự, state.verifications rỗng")
		} else if strings.Contains(vs[0].Reason, "chạy nền") {
			t.Errorf("(L5×N3) cây đã đổi: phải kiểm chứng tuần tự trước, có %d lần, đầu: %q", len(vs), vs[0].Reason)
		}
	})

	// L7 (kiểm nền ĐỎ) × N2/N3/N6: ghi background_verifications Passed=false, không dùng lại, kiểm chứng sau b-feat chạy tuần tự;
	// lệnh `bint task check` kế tiếp (kiểm hẹp xanh) ghi chú checkout sạch thay vì bảo agent sửa.
	t.Run("kiểm nền đỏ", func(t *testing.T) {
		var h *harness
		var res *Result
		var crs []CheckResult
		ms := []bvMilestone{
			{edit: editA(2), check: true, wait: true},
			{edit: editA(3), check: true},
		}
		h, res, _, crs = bvRunOpt(t, bvOpts{variant: config.VariantBPrime, needLocal: true}, ms)
		tk := h.load(res.TaskID)
		bgs := tk.State.BackgroundVerifications
		if len(bgs) == 0 || bgs[0].Passed {
			t.Fatalf("(L7×N2) lần nền đầu phải đỏ và được ghi: %+v", bgs)
		}
		if len(crs) != 2 || !crs[1].Check.Passed {
			t.Fatalf("(L7×N6) kiểm hẹp thứ hai phải xanh: %+v", crs)
		}
		if len(crs[1].Note) == 0 && bgRedNote(tk) == "" && !(len(bgs) > 1 && bgs[len(bgs)-1].Passed) {
			t.Errorf("(L7×N6) nền đỏ mà kiểm hẹp xanh phải có ghi chú checkout sạch")
		}
		if n := crs[1].Note; n != "" && !strings.Contains(n, "checkout sạch") {
			t.Errorf("(L7×N6) ghi chú phải nêu checkout sạch: %q", n)
		}
		for _, b := range bgs {
			if !b.Passed && tk.State.Verifications != nil {
				for _, v := range tk.State.Verifications {
					if v.Tree == b.Tree && strings.Contains(v.Reason, "chạy nền") {
						t.Errorf("(L7×N3) lần nền đỏ không được dùng làm kiểm chứng: %+v", v)
					}
				}
			}
		}
		if vs := tk.State.Verifications; len(vs) == 0 || !vs[0].Passed {
			t.Errorf("(L7×N3) kiểm chứng sau b-feat phải xanh trên cây đã sửa: %+v", vs)
		}
	})

	// L8 (nhiều kiểm hẹp xanh dồn dập) × N2: tối đa 1 lần chạy nền đồng thời (log S/E không bao giờ có hai S liền nhau).
	t.Run("tối đa một lần nền đồng thời", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, pause: 40 * time.Millisecond},
			{edit: func(h *harness) { h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n") }, check: true, pause: 40 * time.Millisecond},
			// wait: máy chậm (CI -race) thì lần nền đầu chưa xong khi b-feat kết thúc, bị huỷ vì cây đã đổi, và không còn lần nào được ghi.
			{edit: editA(3), check: true, pause: 40 * time.Millisecond, wait: true},
		}
		h, res, counter, _ := bvRunOpt(t, bvOpts{variant: config.VariantBPrime, slow: true}, ms)
		tk := h.load(res.TaskID)
		if len(tk.State.BackgroundVerifications) < 1 {
			t.Fatalf("(L8×N2) cần ≥ 1 kiểm nền")
		}
		b, _ := os.ReadFile(counter + ".log")
		if ov := overlappingRuns(strings.Fields(string(b))); ov != "" {
			t.Errorf("(L8×N2) hai lần nền chạy chồng nhau: %s (log: %q)", ov, strings.Fields(string(b)))
		}
	})

	// L9 (huỷ giữa kiểm nền) × N2: huỷ ngữ cảnh khi kiểm nền đang chạy thì không sót worktree tạm.
	t.Run("huỷ giữa kiểm nền dọn worktree tạm", func(t *testing.T) {
		ms := []bvMilestone{
			{edit: editA(2), check: true, pause: 80 * time.Millisecond, cancel: true},
		}
		h, _, _, _ := bvRunOpt(t, bvOpts{variant: config.VariantBPrime, slow: true}, ms)
		if n := roWorktrees(t, h.root); n != 1 {
			t.Errorf("(L9×N2) git worktree list còn %d mục sau huỷ, muốn 1", n)
		}
		// Thư mục cha chung (Windows: %TEMP%/bv dùng chung mọi tiến trình): chỉ đếm checkout (có .git) thuộc kho của test này.
		base := verifyTempBase(h.tasks.Dir)
		ents, _ := os.ReadDir(base)
		for _, e := range ents {
			gitFile, err := os.ReadFile(filepath.Join(base, e.Name(), ".git"))
			if err == nil && strings.Contains(filepath.ToSlash(string(gitFile)), filepath.ToSlash(h.root)) {
				t.Errorf("(L9×N2) checkout tạm %s của kho test còn sót sau huỷ", e.Name())
			}
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
	if len(agentChecks(tk)) < 2 {
		t.Fatalf("(L1×N1) cần ≥ 2 kiểm hẹp: %d", len(agentChecks(tk)))
	}
	first, _ := time.Parse(time.RFC3339, agentChecks(tk)[0].At)
	second, _ := time.Parse(time.RFC3339, agentChecks(tk)[1].At)
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

// overlappingRuns tìm hai lần chạy chồng nhau trong log "S<pid>"/"E<pid>": một lần chạy trọn (có cả S và E) mà giữa S và E của nó có S của lần khác.
// Lần bị huỷ hợp lệ (cây đổi, finish huỷ kiểm nền khác cây cuối) chỉ có S, không có E, nên không bị tính là chồng (CI race: "S E S S E" là huỷ rồi chạy lại).
func overlappingRuns(lines []string) string {
	for i, l := range lines {
		if !strings.HasPrefix(l, "S") {
			continue
		}
		id := l[1:]
		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "E"+id {
				for k := i + 1; k < j; k++ {
					if strings.HasPrefix(lines[k], "S") {
						return "lần " + lines[k][1:] + " bắt đầu khi lần " + id + " chưa xong"
					}
				}
				break
			}
		}
	}
	return ""
}
