package workflow

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/runlog"
	"bint/internal/store"
	"bint/internal/task"
	"bint/internal/testutil"
)

var stepRe = regexp.MustCompile(`step="([a-z-]+)"`)

// fakeAgent mô phỏng agent: đọc bước từ đề bài, gọi thao tác tác vụ như khi chạy
// `bint task …` với BINT_RUN_ID/BINT_TASK_ID của bước.
type fakeAgent struct {
	h    *harness
	cost *float64
}

func (a *fakeAgent) Name() string                   { return "fake" }
func (a *fakeAgent) Version(context.Context) string { return "" }
func (a *fakeAgent) ReportsCost() bool              { return a.cost != nil }

func (a *fakeAgent) Run(ctx context.Context, req eval.AgentRequest) eval.AgentResult {
	m := stepRe.FindStringSubmatch(req.Prompt)
	envs := map[string]string{}
	for _, kv := range req.Env {
		k, v, _ := strings.Cut(kv, "=")
		envs[k] = v
	}
	step := m[1]
	a.h.calls = append(a.h.calls, step)
	a.h.prompts[step] = req.Prompt
	agentEnv := &Env{Root: a.h.root, Cfg: a.h.cfg, Tasks: a.h.tasks, Spool: a.h.spool, Actor: "agent", RunID: envs[EnvRunID]}
	res := eval.AgentResult{CostUSD: a.cost, Duration: time.Millisecond, Model: "fake-model"}
	if err := a.h.behave(ctx, step, agentEnv, envs[EnvTaskID]); err != nil {
		res.ExitCode, res.Error = 1, err.Error()
	}
	if ctx.Err() != nil {
		res.ExitCode, res.Error = -1, ctx.Err().Error()
	}
	return res
}

type harness struct {
	t       *testing.T
	root    string
	cfg     config.Config
	tasks   task.Store
	spool   string
	db      string
	calls   []string
	prompts map[string]string
	behave  func(ctx context.Context, step string, e *Env, taskID string) error
	limits  []*float64
	cost    *float64
}

func newHarness(t *testing.T) *harness {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	st := t.TempDir()
	cfg := config.Default()
	cmd := "test -f done.txt"
	cfg.Eval.TestCommand = &cmd
	h := &harness{t: t, root: root, cfg: cfg, tasks: task.NewStore(filepath.Join(st, "runtime")), spool: filepath.Join(st, "spool"),
		db: filepath.Join(st, "state.sqlite"), prompts: map[string]string{}}
	h.behave = h.happy
	os.MkdirAll(h.spool, 0o700)
	return h
}

func (h *harness) write(name, body string) {
	p := filepath.Join(h.root, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) happy(_ context.Context, step string, e *Env, id string) error {
	switch step {
	case StepReq:
		for _, n := range task.ArtifactNames() {
			if _, _, err := e.WriteArtifact(id, n, "# "+n+"\n"); err != nil {
				return err
			}
		}
	case StepFeat:
		h.write("app.txt", "v2\n")
		h.write("done.txt", "ok\n")
	case StepReview:
		_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[{"kind":"suggestion","severity":"low","title":"đặt tên","path":"app.txt","lines":"1","evidence":"tên chưa rõ"}]}`))
		return err
	}
	return nil
}

func (h *harness) runner() *Runner {
	env := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool, Human: true, Actor: "người duyệt", Version: "test"}
	return &Runner{Env: env, Agent: func(step string, maxCost *float64, _ *int) (eval.Agent, error) {
		h.limits = append(h.limits, maxCost)
		return &fakeAgent{h: h, cost: h.cost}, nil
	}}
}

func (h *harness) ingest() *store.Store {
	s, err := store.Open(context.Background(), h.db)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { s.Close() })
	if _, err := runlog.Ingest(context.Background(), s, h.spool, time.Now(), 0); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func TestBDoHappyPathLinksRunsByTask(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-feat,b-review" {
		t.Fatalf("kết quả: %+v, bước %v", res, h.calls)
	}
	// Đề bài: nội dung skill + khối tác vụ + phạm vi.
	p := h.prompts[StepFeat]
	if !strings.Contains(p, "# b-feat — thực thi kế hoạch") || !strings.Contains(p, "tác vụ "+res.TaskID) ||
		!strings.Contains(p, "app.txt") || !strings.Contains(p, "plan: ") {
		t.Fatalf("đề bài b-feat:\n%s", p)
	}
	tk, _ := h.tasks.Load(res.TaskID)
	if v := tk.State.LastVerification(); v == nil || !v.Passed || !v.Confirmed || tk.State.Runner != nil {
		t.Fatalf("kiểm chứng / runner: %+v", tk.State)
	}

	s := h.ingest()
	runs, err := runlog.RunsForTask(ctx, s.DB, res.TaskID)
	if err != nil || len(runs) != 4 {
		t.Fatalf("lượt chạy theo task: %d %v", len(runs), err)
	}
	var parent runlog.Run
	children := 0
	for _, r := range runs {
		m, _ := runlog.GetRunMeta(ctx, s.DB, r.RunID)
		if r.Workflow.String == StepDo {
			parent = r
			continue
		}
		children++
		if m.ParentRunID.String != res.RunID || r.Status != "completed" || r.Model.String != "fake-model" {
			t.Fatalf("lượt con: %+v %+v", r, m)
		}
	}
	if parent.RunID != res.RunID || parent.Status != "completed" || children != 3 {
		t.Fatalf("lượt cha: %+v", parent)
	}
	steps, _ := runlog.RunSteps(ctx, s.DB, res.RunID)
	if len(steps) != 3 || steps[0].Name != StepReq || steps[2].Status != "completed" {
		t.Fatalf("run_steps: %+v", steps)
	}
	sigs, _ := runlog.RunSignals(ctx, s.DB, res.RunID)
	if label, _ := runlog.Label(sigs); label != runlog.LabelSuccess {
		t.Fatalf("nhãn: %s %+v", label, sigs)
	}
	m, _ := runlog.GetRunMeta(ctx, s.DB, res.RunID)
	if m.VerificationStatus.String != "passed" {
		t.Fatalf("verification_status: %+v", m)
	}
}

func TestProtectedPathBlocksThenResumeWithoutRerunningFeat(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			h.write("migrations/002.sql", "ALTER TABLE x;\n")
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "Đổi giá trị trong app.txt", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := h.tasks.Load(res.TaskID)
	if res.Status != task.StatusBlocked || len(tk.State.PendingPaths) != 1 || tk.State.PendingPaths[0] != "migrations/002.sql" || !tk.State.AwaitingCheck {
		t.Fatalf("phải chặn chờ duyệt: %+v %+v", res, tk.State)
	}
	// Agent không duyệt được; done bị chặn.
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	if _, _, err := agent.Approve(res.TaskID); err == nil {
		t.Fatal("agent không được duyệt")
	}
	if _, _, err := agent.SetStatus(ctx, res.TaskID, task.StatusDone, TransitionOptions{}); err == nil {
		t.Fatal("done phải bị chặn")
	}
	r := h.runner()
	if _, a, err := r.Approve(res.TaskID); err != nil || len(a.Paths) != 1 {
		t.Fatalf("duyệt: %+v %v", a, err)
	}
	res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review" {
		t.Fatalf("tiếp tục: %+v, bước %v", res2, h.calls)
	}
	s := h.ingest()
	m, _ := runlog.GetRunMeta(ctx, s.DB, res2.RunID)
	if m.ParentRunID.String == "" {
		t.Fatal("lượt tiếp tục phải có parent_run_id")
	}
}

func TestReviewLoopStopsAfterMaxRounds(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`))
			return err
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review" ||
		!strings.Contains(res.Reason, "sau 2 vòng review") {
		t.Fatalf("%+v %v", res, h.calls)
	}
	if !strings.Contains(h.prompts[StepFeat], "Phát hiện review vòng 1 cần xử lý") {
		t.Fatalf("b-feat vòng 2 phải nhận phát hiện:\n%s", h.prompts[StepFeat])
	}
}

func TestBudgetLimitStopsBeforeNextStep(t *testing.T) {
	h := newHarness(t)
	one := 1.0
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxCostUSD: &one}}
	c := 0.6
	h.cost = &c
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng mới"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req,b-feat" || !strings.Contains(res.Reason, "hết ngân sách") {
		t.Fatalf("%+v %v", res, h.calls)
	}
	if h.limits[0] == nil || *h.limits[0] != 1.0 || h.limits[1] == nil || *h.limits[1] < 0.39 || *h.limits[1] > 0.41 {
		t.Fatalf("giới hạn chi phí từng bước: %v %v", *h.limits[0], *h.limits[1])
	}
	if res.CostUSD == nil || *res.CostUSD != 1.2 {
		t.Fatalf("chi phí: %v", res.CostUSD)
	}
}

func TestInterruptAndFailure(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.behave = func(_ context.Context, step string, e *Env, id string) error {
		cancel()
		return nil
	}
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := h.tasks.Load(res.TaskID)
	if res.Status != task.StatusInterrupted || tk.State.ResumeFrom != task.StatusInProgress || len(tk.State.Errors) == 0 {
		t.Fatalf("%+v %+v", res, tk.State)
	}
	s := h.ingest()
	if r, _ := runlog.GetRun(context.Background(), s.DB, res.RunID); r.Status != "interrupted" {
		t.Fatalf("lượt cha: %s", r.Status)
	}

	// Không tiếp tục khi chưa resume.
	r := h.runner()
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID}); err == nil {
		t.Fatal("tác vụ interrupted cần resume")
	}
	// Resume: agent lỗi → failed.
	h.behave = func(context.Context, string, *Env, string) error { return os.ErrPermission }
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil || res2.Status != task.StatusFailed {
		t.Fatalf("%+v %v", res2, err)
	}
}

func TestReviewerMustSubmit(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			return nil
		}
		return base(ctx, step, e, id)
	}
	res, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false)})
	if res.Status != task.StatusFailed || !strings.Contains(res.Reason, "không ghi kết quả") {
		t.Fatalf("%+v", res)
	}
}

func TestDoneNeedsEvidenceWithoutVerifyCommand(t *testing.T) {
	h := newHarness(t)
	h.cfg.Eval.TestCommand = nil
	res, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false)})
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "không có bằng chứng") {
		t.Fatalf("%+v", res)
	}
	// Người --force được, agent thì không.
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks}
	if _, _, err := agent.SetStatus(context.Background(), res.TaskID, task.StatusDone, TransitionOptions{Force: true}); err == nil {
		t.Fatal("agent không --force được")
	}
	human := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Human: true}
	tk, _, err := human.SetStatus(context.Background(), res.TaskID, task.StatusDone, TransitionOptions{Force: true, Reason: "đã xem"})
	if err != nil || tk.State.Status != task.StatusDone || !strings.Contains(tk.State.Reason, "thiếu bằng chứng") {
		t.Fatalf("%+v %v", tk.State, err)
	}
}

func TestAgentArgs(t *testing.T) {
	cfg := config.Default()
	n := 30
	a := AgentArgs(cfg, StepReview, &n)
	args := strings.Join((&eval.ClaudeAgent{Cfg: a}).Args(), " ")
	if !strings.Contains(args, "--disallowedTools Edit,Write,MultiEdit,NotebookEdit") || !strings.Contains(args, "--max-turns 30") ||
		!strings.Contains(args, "Bash(bint:*)") || strings.Contains(args, "--setting-sources") {
		t.Fatalf("%s", args)
	}
	if args := strings.Join((&eval.ClaudeAgent{Cfg: AgentArgs(cfg, StepFeat, nil)}).Args(), " "); strings.Contains(args, "disallowedTools") {
		t.Fatalf("b-feat được sửa tệp: %s", args)
	}
	cfg.Roles.Implementer = "codex"
	if _, err := DefaultAgents(cfg, "", "")(StepFeat, nil, nil); err == nil {
		t.Fatal("harness chưa có adapter phải lỗi")
	}
}

func ptr[T any](v T) *T { return &v }
