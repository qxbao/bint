package actions_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/form"
	"bint/internal/tui/jobs"
)

// evalSeed dựng repo đã init có eval.test_command (sh), hai commit (một lời giải kèm kiểm thử mới, một commit chỉ thêm
// kiểm thử đã qua sẵn) và agent lệnh tuỳ chỉnh làm đúng lời giải. Trả thư mục, lệnh agent và ID hai ứng viên.
func evalSeed(t *testing.T) (root, agentCmd, goodID, badID string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("cần sh")
		}
	}
	testutil.Isolate(t)
	asHuman(t)
	calc := "add() { echo $(($1 + $2)); }\n"
	root = testutil.NewRepo(t, map[string]string{"lib/calc.sh": calc, "tests/run.sh": "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\n",
		"tests/test_add.sh": ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ]\n"})
	testutil.Git(t, root, "config", "user.name", "t")
	testutil.Git(t, root, "config", "user.email", "t@example.com")
	commit := func(msg string, files map[string]string) string {
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		testutil.Git(t, root, "add", "-A")
		testutil.Git(t, root, "commit", "-q", "-m", msg)
		return testutil.Git(t, root, "rev-parse", "HEAD")
	}
	good := commit("Add a multiplication helper to the calc library\n\nCallers need products, not only sums.",
		map[string]string{"lib/calc.sh": calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_mul.sh": ". lib/calc.sh\n[ \"$(mul 4 5)\" = 20 ]\n"})
	bad := commit("Add a regression test for add with small numbers",
		map[string]string{"lib/calc.sh": "# sums\n" + calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_add2.sh": ". lib/calc.sh\n[ \"$(add 1 1)\" = 2 ]\n"})
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, BintPath: "bint", Hooks: boolPtr(false), NoSkills: true}); err != nil {
		t.Fatal(err)
	}
	local := "eval:\n  test_command: \"sh tests/run.sh\"\n  mine:\n    min_source_lines: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(agent, []byte("echo 'mul() { echo $(($1 * $2)); }' >> lib/calc.sh\n"), 0o644)
	return root, "sh " + filepath.ToSlash(agent), "c" + good[:12], "c" + bad[:12]
}

func boolPtr(b bool) *bool { return &b }

// sameOptions so sánh hai Options coi slice nil và rỗng là một (Build trả slice rỗng cho ô danh sách trống).
func sameOptions(a, b any) bool {
	norm := func(v any) any {
		cp := reflect.New(reflect.TypeOf(v)).Elem()
		cp.Set(reflect.ValueOf(v))
		var walk func(reflect.Value)
		walk = func(x reflect.Value) {
			for i := 0; i < x.NumField(); i++ {
				f := x.Field(i)
				switch {
				case f.Kind() == reflect.Slice && f.IsNil():
					f.Set(reflect.MakeSlice(f.Type(), 0, 0))
				case f.Kind() == reflect.Struct && x.Type().Field(i).Anonymous:
					walk(f)
				}
			}
		}
		walk(cp)
		return cp.Interface()
	}
	return reflect.DeepEqual(norm(a), norm(b))
}

// Các hành động đọc và ghi của màn Đánh giá chạy với dịch vụ thật: khai thác, đặc tả, kiểm chứng (việc nền), duyệt, loại,
// liệt kê, xem, ước tính, báo cáo, dọn kho đệm.
func TestEvalActionsAgainstRealService(t *testing.T) {
	root, _, goodID, badID := evalSeed(t)
	env := actions.Env{Dir: root, Version: "test"}

	// mine (không kiểm chứng: việc thường).
	r := doAny(t, "eval.mine", env, app.EvalMineDefaults(config.Default()))
	mined, _ := r.Value.(*app.EvalMineResult)
	if r.Err != nil || mined == nil || len(mined.New) != 2 {
		t.Fatalf("mine: %v %+v", r.Err, r.Value)
	}
	// spec (Do, ghi).
	r = do(t, "eval.spec", env, app.EvalSpecOptions{IDs: []string{goodID}})
	if sp, _ := r.Value.(*app.EvalSpecResult); r.Err != nil || sp == nil || sp.Analyzed != 1 {
		t.Fatalf("spec: %v %+v", r.Err, r.Value)
	}
	// verify (Job): ứng viên tốt đạt fail→pass, ứng viên "đã qua sẵn" thì không; tiến độ gửi bằng DataMsg.
	ctx := context.Background()
	ch := make(chan tea.Msg, 64)
	a, _ := actions.Default().ByID("eval.verify")
	v, err := a.Job(ctx, env, app.EvalVerifyOptions{All: true, Runs: 1}, jobs.NewBridge(ctx, 1, ch))
	if vr, _ := v.(*app.EvalVerifyResult); err != nil || vr == nil || vr.Total != 2 || vr.Failed != 1 {
		t.Fatalf("verify: %v %+v", err, v)
	}
	var steps []actions.EvalVerifyStep
	for len(ch) > 0 {
		if d, ok := (<-ch).(jobs.DataMsg); ok {
			steps = append(steps, d.Value.(actions.EvalVerifyStep))
		}
	}
	if len(steps) != 4 || !steps[1].Done || steps[0].Done {
		t.Fatalf("mỗi mục báo bắt đầu rồi xong: %+v", steps)
	}

	// list: ứng viên tốt chờ duyệt; ứng viên hỏng đã bị loại tự động (chỉ hiện khi All).
	l, _ := do(t, "eval.list", env, app.EvalListOptions{}).Value.(*app.EvalListResult)
	if l == nil || len(l.Pending) != 1 || l.Pending[0].ID != goodID {
		t.Fatalf("list: %+v", l)
	}
	if l, _ := do(t, "eval.list", env, app.EvalListOptions{All: true}).Value.(*app.EvalListResult); l == nil || len(l.Pending) != 2 {
		t.Fatalf("list --all: %+v", l)
	}
	// show: lệnh chấm và đề bài.
	sh, _ := do(t, "eval.show", env, app.EvalShowOptions{ID: goodID}).Value.(*app.EvalShowResult)
	if sh == nil || sh.Case.ID != goodID || sh.TestCommand != "sh tests/run.sh" || !strings.Contains(sh.Case.Prompt, "Callers need products") {
		t.Fatalf("show: %+v", sh)
	}

	// approve: ứng viên đã loại bị từ chối nếu không --force; ứng viên tốt được duyệt.
	outs, _ := do(t, "eval.approve", env, app.EvalApproveOptions{IDs: []string{badID}}).Value.([]app.EvalApproveOutcome)
	if len(outs) != 1 || outs[0].Problem == "" || !strings.Contains(outs[0].Problem, "đã bị loại") {
		t.Fatalf("approve đã loại: %+v", outs)
	}
	outs, _ = do(t, "eval.approve", env, app.EvalApproveOptions{IDs: []string{goodID}, Split: "validation"}).Value.([]app.EvalApproveOutcome)
	if len(outs) != 1 || !outs[0].Approved || outs[0].Split != "validation" {
		t.Fatalf("approve: %+v", outs)
	}
	// reject: ứng viên đã duyệt không loại được (lỗi kèm phần đã loại: rỗng).
	if r := do(t, "eval.reject", env, app.EvalRejectOptions{IDs: []string{goodID}, Reason: "x"}); r.Err == nil {
		t.Fatal("tác vụ đã duyệt không loại được")
	}
	if r := do(t, "eval.reject", env, app.EvalRejectOptions{IDs: []string{badID}, Reason: "trùng"}); r.Err != nil || len(r.Value.([]string)) != 1 {
		t.Fatalf("reject: %v %+v", r.Err, r.Value)
	}

	// estimate (đọc, cần cơ sở dữ liệu): 1 tác vụ × 1 nhánh × 2 lần lặp = 2 lượt.
	eo := app.EvalEstimateDefaults(config.Default())
	eo.Arms, eo.Repeats = []string{"none"}, 2
	est, _ := do(t, "eval.estimate", env, eo).Value.(*app.EvalEstimateResult)
	if est == nil || est.Estimate.Runs != 2 || est.Estimate.AvailableCases != 1 {
		t.Fatalf("estimate: %+v", est)
	}
	// report --list (đọc): chưa có đợt nào.
	rp, _ := do(t, "eval.report", env, app.EvalReportOptions{List: true}).Value.(*app.EvalReportResult)
	if rp == nil || len(rp.Batches) != 0 {
		t.Fatalf("report: %+v", rp)
	}
	// clean (ghi): không có kho đệm thì xoá 0.
	if c, ok := do(t, "eval.clean", env, app.EvalCleanOptions{}).Value.(app.EvalCleanResult); !ok || c.Removed < 0 {
		t.Fatalf("clean: %+v", c)
	}
}

// reply đọc thông điệp của việc nền và trả lời câu hỏi giữa chừng; thu các thông điệp còn lại.
type reply struct {
	prompt  jobs.Prompt
	asked   bool
	logs    []string
	data    []any
	answer  bool
	onAsked func()
}

func (r *reply) drain(ch chan tea.Msg, done <-chan struct{}) {
	consume := func(m tea.Msg) {
		switch m := m.(type) {
		case jobs.PromptMsg:
			r.prompt, r.asked = m.Prompt, true
			if r.onAsked != nil {
				r.onAsked()
			}
			m.Reply <- jobs.Answer{Yes: r.answer}
		case jobs.LogMsg:
			r.logs = append(r.logs, m.Line)
		case jobs.DataMsg:
			r.data = append(r.data, m.Value)
		}
	}
	for {
		select {
		case m := <-ch:
			consume(m)
		case <-done:
			// The job has stopped producing, but its last data messages may
			// still be queued when select chooses done before ch.
			for len(ch) > 0 {
				consume(<-ch)
			}
			return
		}
	}
}

func runJob(t *testing.T, id string, env actions.Env, opts any, answer bool) (*reply, any, error) {
	t.Helper()
	a, _ := actions.Default().ByID(id)
	ch := make(chan tea.Msg, 256)
	r := &reply{answer: answer}
	done := make(chan struct{})
	drained := make(chan struct{})
	go func() { r.drain(ch, done); close(drained) }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	v, err := a.Job(ctx, env, opts, jobs.NewBridge(ctx, 1, ch))
	close(done)
	<-drained
	return r, v, err
}

// eval run (Job): kế hoạch ô và ước tính tới trước hộp xác nhận, hộp có Detail; trả không thì không tạo đợt; trả có thì
// chạy, báo từng lượt bằng DataMsg có kiểu và đợt completed.
func TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch(t *testing.T) {
	root, agentCmd, goodID, _ := evalSeed(t)
	env := actions.Env{Dir: root, Version: "test"}
	doAny(t, "eval.mine", env, app.EvalMineDefaults(config.Default()))
	do(t, "eval.approve", env, app.EvalApproveOptions{IDs: []string{goodID}, Unverified: true})
	o := app.EvalRunDefaults(config.Default())
	o.Arms, o.Repeats, o.AgentCommand, o.Yes = []string{"none"}, 2, agentCmd, true // Yes bị bỏ qua: TUI luôn hỏi

	r, v, err := runJob(t, "eval.run", env, o, false)
	if !errors.Is(err, app.ErrCancelled) || v == nil {
		t.Fatalf("trả không: %v %v", err, v)
	}
	if !r.asked || r.prompt.Key != app.ConfirmEvalRunKey || !strings.Contains(r.prompt.Detail, "eval.log.estimate") || !strings.Contains(r.prompt.Detail, "eval.log.note") {
		t.Fatalf("hộp xác nhận phải có ước tính và lưu ý: %+v", r.prompt)
	}
	var plan app.EvalRunPlan
	for _, d := range r.data {
		if p, ok := d.(app.EvalRunPlan); ok {
			plan = p
		}
	}
	if len(plan.CaseIDs) != 1 || plan.CaseIDs[0] != goodID || len(plan.Arms) != 1 || plan.Repeats != 2 {
		t.Fatalf("kế hoạch ô: %+v", plan)
	}
	e, _ := app.OpenEval(context.Background(), root, true)
	defer e.Close()
	if bs, _ := eval.ListBatches(context.Background(), e.Store.DB, 10); len(bs) != 0 {
		t.Fatalf("trả không thì không có đợt: %+v", bs)
	}

	r, v, err = runJob(t, "eval.run", env, o, true)
	res, _ := v.(*app.EvalRunResult)
	if err != nil || res == nil || res.Status != "completed" || res.Done != 2 {
		t.Fatalf("trả có: %v %+v", err, v)
	}
	attempts := 0
	for _, d := range r.data {
		if a, ok := d.(actions.EvalAttempt); ok {
			attempts++
			if a.Row.Status != "passed" || a.Total != 2 {
				t.Fatalf("lượt: %+v", a)
			}
		}
	}
	if attempts != 2 || !slices.ContainsFunc(r.logs, func(s string) bool { return strings.Contains(s, "eval.log.attempt") }) {
		t.Fatalf("mỗi lượt báo một thông điệp có kiểu và một dòng log: %d %v", attempts, r.logs)
	}
	if bs, _ := eval.ListBatches(context.Background(), e.Store.DB, 10); len(bs) != 1 || bs[0].Status != "completed" {
		t.Fatalf("đợt: %+v", bs)
	}

	// Chạy tiếp một đợt đã xong qua eval.resume: OnPlan báo các lượt đã có (Done), không còn gì để chạy.
	ro := app.EvalRunDefaults(config.Default())
	ro.Resume, ro.AgentCommand = res.BatchID, agentCmd
	r, _, _ = runJob(t, "eval.resume", env, ro, true)
	var rplan app.EvalRunPlan
	for _, d := range r.data {
		if p, ok := d.(app.EvalRunPlan); ok {
			rplan = p
		}
	}
	if rplan.BatchID != res.BatchID || len(rplan.Done) != 2 {
		t.Fatalf("kế hoạch chạy tiếp phải có hai lượt đã xong: %+v", rplan)
	}
}

// eval bootstrap (Job): hộp xác nhận kèm kế hoạch; trả không thì không dựng gì.
func TestEvalBootstrapJobAsksWithPlan(t *testing.T) {
	root, agentCmd, goodID, _ := evalSeed(t)
	env := actions.Env{Dir: root, Version: "test"}
	doAny(t, "eval.mine", env, app.EvalMineDefaults(config.Default()))
	do(t, "eval.approve", env, app.EvalApproveOptions{IDs: []string{goodID}, Unverified: true})
	o := app.EvalBootstrapDefaults(config.Default())
	o.AgentCommand = agentCmd
	r, v, err := runJob(t, "eval.bootstrap", env, o, false)
	br, _ := v.(*app.EvalBootstrapResult)
	if !errors.Is(err, app.ErrCancelled) || br == nil || br.Todo != 1 {
		t.Fatalf("trả không: %v %+v", err, v)
	}
	if !r.asked || r.prompt.Key != app.ConfirmEvalBootstrapKey || !strings.Contains(r.prompt.Detail, "eval.log.bootstrap_plan") {
		t.Fatalf("hộp xác nhận: %+v", r.prompt)
	}
}

// Writes của hành động eval khớp danh sách yêu cầu (mine, verify, spec, approve, reject, run, bootstrap, clean cùng hai
// hành động giao diện chạy tiếp và mở tệp); hành động đọc (list, show, estimate, report) không ghi; không hành động nào
// HumanOnly (CLI không chặn approve/reject theo phiên agent).
func TestEvalActionsWritesFlags(t *testing.T) {
	want := map[string]bool{"eval.mine": true, "eval.verify": true, "eval.spec": true, "eval.approve": true, "eval.reject": true, "eval.run": true,
		"eval.bootstrap": true, "eval.clean": true, "eval.resume": true, "eval.open_file": true,
		"eval.list": false, "eval.show": false, "eval.estimate": false, "eval.report": false, "eval.baseline": false, "eval.split": false,
		"eval.filter": false, "eval.refresh": false, "eval.next_view": false, "eval.prev_view": false}
	n := 0
	for _, a := range actions.Default().All() {
		if a.Screen != actions.Eval {
			continue
		}
		n++
		w, ok := want[a.ID]
		if !ok {
			t.Errorf("hành động %s chưa có trong danh sách kiểm", a.ID)
		}
		if a.Writes != w {
			t.Errorf("%s: Writes = %v, muốn %v", a.ID, a.Writes, w)
		}
		if a.HumanOnly {
			t.Errorf("%s không được là HumanOnly (giữ nguyên hành vi CLI)", a.ID)
		}
	}
	if n != len(want) {
		t.Errorf("số hành động eval = %d, muốn %d", n, len(want))
	}
	// Việc nặng chạy nền: run, resume, bootstrap, verify, mine (có kiểm chứng) đều là Job.
	for _, id := range []string{"eval.run", "eval.resume", "eval.bootstrap", "eval.verify", "eval.mine"} {
		if a, _ := actions.Default().ByID(id); a.Job == nil || a.JobInfo == nil {
			t.Errorf("%s phải chạy nền (Job)", id)
		}
	}
	if a, _ := actions.Default().ByID("eval.mine"); a.JobInfo(app.EvalMineOptions{Verify: true}).Kind != jobs.KindEval || a.JobInfo(app.EvalMineOptions{}).Kind == jobs.KindEval {
		t.Error("khai thác có kiểm chứng là việc nặng KindEval; không kiểm chứng là việc thường")
	}
}

// Mỗi form eval gửi đúng Options và hiện đúng dòng lệnh CLI tương đương; trường chỉ đặt qua hộp xác nhận (--yes, --force
// và --unverified của approve) không có ô.
func TestEvalFormsSendOptionsAndShowCLI(t *testing.T) {
	cfg := config.Default()
	key := func(s string) tea.KeyPressMsg {
		switch s {
		case "ctrl+u":
			return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
		case "left":
			return tea.KeyPressMsg{Code: tea.KeyLeft}
		}
		r := []rune(s)
		return tea.KeyPressMsg{Code: r[0], Text: s}
	}
	set := func(m *form.Model, field, text string) {
		t.Helper()
		if !m.FocusField(field) {
			t.Fatalf("không có ô %s", field)
		}
		m.Update(key("ctrl+u"))
		for _, r := range text {
			m.Update(key(string(r)))
		}
	}
	toggle := func(m *form.Model, field string) {
		t.Helper()
		if !m.FocusField(field) {
			t.Fatalf("không có ô %s", field)
		}
		m.Update(key("x"))
	}
	bud := 2.5
	cost := 0.4
	cases := []struct {
		id       string
		defaults any
		fill     func(m *form.Model)
		want     any
		cli      string
	}{
		{"eval.mine", app.EvalMineDefaults(cfg), func(m *form.Model) { set(m, "rev", "HEAD~3"); set(m, "limit", "5"); toggle(m, "verify") },
			app.EvalMineOptions{Rev: "HEAD~3", Limit: 5, Verify: true, VerifyRuns: 2}, "bint eval mine --rev 'HEAD~3' --limit 5 --verify"},
		{"eval.verify", app.EvalVerifyDefaults(cfg), func(m *form.Model) { set(m, "id", "c1, c2"); set(m, "runs", "3") },
			app.EvalVerifyOptions{IDs: []string{"c1", "c2"}, Runs: 3}, "bint eval verify --runs 3 c1 c2"},
		{"eval.list", app.EvalListDefaults(cfg), func(m *form.Model) { toggle(m, "all") }, app.EvalListOptions{All: true}, "bint eval list --all"},
		{"eval.show", app.EvalShowDefaults(cfg), func(m *form.Model) { set(m, "id", "c1") }, app.EvalShowOptions{ID: "c1"}, "bint eval show c1"},
		{"eval.spec", app.EvalSpecDefaults(cfg), func(m *form.Model) { set(m, "id", "c1"); toggle(m, "approved") },
			app.EvalSpecOptions{IDs: []string{"c1"}, Approved: true}, "bint eval spec --approved c1"},
		{"eval.approve", app.EvalApproveDefaults(cfg), func(m *form.Model) { set(m, "id", "c1"); m.FocusField("split"); m.Update(key("left")) },
			app.EvalApproveOptions{IDs: []string{"c1"}, Split: "test"}, "bint eval approve --split test c1"},
		{"eval.reject", app.EvalRejectDefaults(cfg), func(m *form.Model) { set(m, "id", "c1, c2"); set(m, "reason", "trùng") },
			app.EvalRejectOptions{IDs: []string{"c1", "c2"}, Reason: "trùng"}, "bint eval reject --reason 'trùng' c1 c2"},
		{"eval.estimate", app.EvalEstimateDefaults(cfg), func(m *form.Model) {
			set(m, "case", "c1")
			set(m, "limit", "3")
			set(m, "arms", "none")
			set(m, "repeats", "5")
			set(m, "effect", "0.2")
			set(m, "cost-per-run", "0.4")
			set(m, "model", "m1")
		}, func() any {
			o := app.EvalEstimateDefaults(cfg)
			o.CaseIDs, o.Limit, o.Arms, o.Repeats, o.Effect, o.CostPerRun, o.Model = []string{"c1"}, 3, []string{"none"}, 5, 0.2, &cost, "m1"
			return o
		}(), "bint eval estimate --case c1 --limit 3 --arms none --repeats 5 --effect 0.2 --cost-per-run 0.4 --model m1"},
		{"eval.run", app.EvalRunDefaults(cfg), func(m *form.Model) {
			set(m, "case", "c1")
			set(m, "arms", "none")
			set(m, "repeats", "2")
			set(m, "budget", "2.5")
			set(m, "agent-command", "sh a.sh")
			set(m, "seed", "7")
			toggle(m, "keep-workdirs")
			m.FocusField("leakage-mode")
			m.Update(key("x")) // ô chọn: x không đổi lựa chọn; giữ mặc định
		}, func() any {
			o := app.EvalRunDefaults(cfg)
			o.CaseIDs, o.Arms, o.Repeats, o.BudgetUSD, o.AgentCommand, o.Seed, o.KeepWorkdirs = []string{"c1"}, []string{"none"}, 2, &bud, "sh a.sh", 7, true
			return o
		}(), "bint eval run --case c1 --arms none --repeats 2 --budget 2.5 --agent-command 'sh a.sh' --seed 7 --keep-workdirs"},
		{"eval.report", app.EvalReportDefaults(cfg), func(m *form.Model) { set(m, "batch-id", "eb_1"); set(m, "baseline", "bint") },
			app.EvalReportOptions{BatchID: "eb_1", Baseline: "bint"}, "bint eval report --baseline bint eb_1"},
		{"eval.bootstrap", app.EvalBootstrapDefaults(cfg), func(m *form.Model) { set(m, "limit", "2"); set(m, "budget", "2.5"); toggle(m, "force") }, func() any {
			o := app.EvalBootstrapDefaults(cfg)
			o.Limit, o.BudgetUSD, o.Force = 2, &bud, true
			return o
		}(), "bint eval bootstrap --limit 2 --budget 2.5 --force"},
	}
	for _, tc := range cases {
		a, ok := actions.Default().ByID(tc.id)
		if !ok {
			t.Fatalf("không có hành động %s", tc.id)
		}
		m := form.New(a.FormNS(), a.CLIPath, tc.defaults)
		tc.fill(&m)
		got, err := m.Build()
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if !sameOptions(got, tc.want) {
			t.Errorf("%s: Options\n got  %+v\n want %+v", tc.id, got, tc.want)
		}
		if line := m.CLI(); line != tc.cli {
			t.Errorf("%s: dòng lệnh\n got  %s\n want %s", tc.id, line, tc.cli)
		}
	}
	// Trường chỉ đặt qua hộp xác nhận không có ô trong form.
	for _, c := range []struct {
		o    any
		name string
	}{{app.EvalApproveOptions{}, "force"}, {app.EvalApproveOptions{}, "unverified"}, {app.EvalRunOptions{}, "yes"}, {app.EvalRunOptions{}, "resume"},
		{app.EvalBootstrapOptions{}, "yes"}, {app.EvalBootstrapOptions{}, "list"}, {app.EvalReportOptions{}, "list"}} {
		for _, f := range form.Fields(c.o) {
			if f.Name == c.name {
				t.Errorf("%T: --%s không được có ô form", c.o, c.name)
			}
		}
	}
}
