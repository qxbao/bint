package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	adapters "bint/internal/harness"
	"bint/internal/knowledge"
	"bint/internal/mcpgw"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/store"
	"bint/internal/task"
	"bint/internal/testutil"
)

func TestResumeInterruptedPlannerFromRealCodexState(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	ctx := context.Background()
	tk, _, err := r.NewTask(ctx, NewTaskOptions{Workflow: StepReq, Request: "Plan documenting Add"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/codex-interrupted-planner.json")
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Status     string `json:"status"`
		ResumeFrom string `json:"resume_from"`
	}
	if err = json.Unmarshal(b, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Status != task.StatusInterrupted || observed.ResumeFrom != task.StatusCreated {
		t.Fatal("fixture không phải planner bị Ctrl-C")
	}
	_, err = h.tasks.Update(tk.State.ID, time.Now(), func(tk *task.Task) error {
		tk.State.Status = observed.Status
		tk.State.ResumeFrom = observed.ResumeFrom
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: tk.State.ID, Resume: true})
	if err != nil || res.Status != task.StatusPlanned || strings.Join(h.calls, ",") != "b-req" {
		t.Fatalf("không tiếp tục được planner: %+v %v calls=%v", res, err, h.calls)
	}
}

func TestHookDenialIsBoundToChildRunAndCarriesProtectedPath(t *testing.T) {
	dir := t.TempDir()
	other := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "other", Type: spool.ToolDenied,
		Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: "other-run", Payload: map[string]any{"protected_paths": []any{"migrations/001.sql"}}}
	current := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "current", Type: spool.ToolDenied,
		Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: "child-run", Payload: map[string]any{
			"protected_paths": []any{"migrations/001.sql"}, "reason": "protected path denied"}}
	if err := spool.WriteDenial(dir, other); err != nil {
		t.Fatal(err)
	}
	if err := spool.WriteDenial(dir, current); err != nil {
		t.Fatal(err)
	}
	reason, paths, denied, _ := hookDenial(dir, "child-run")
	if !denied || reason != "protected path denied" || len(paths) != 1 || paths[0] != "migrations/001.sql" {
		t.Fatalf("denial not recovered: %q %v %v", reason, paths, denied)
	}
	if remaining, err := spool.Denials(dir, "child-run"); err != nil || len(remaining) != 0 {
		t.Fatalf("processed child denial journal was not cleared: %d events, %v", len(remaining), err)
	}
	if _, _, denied, _ = hookDenial(dir, "other-child"); denied {
		t.Fatal("denial from another child run leaked into this workflow")
	}
}

// Deny của guardrail (không có đường dẫn bảo vệ) là phản hồi cho agent: không chặn tác vụ, chỉ trả về để ghi log,
// và nhật ký vẫn được dọn.
func TestHookDenialGuardrailOnlyDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "guard", Type: spool.ToolDenied,
		Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: "child-run", Payload: map[string]any{
			"paths": []any{"src/a.go"}, "protected_paths": []any{}, "reason": "Không sửa mã."}}
	if err := spool.WriteDenial(dir, ev); err != nil {
		t.Fatal(err)
	}
	reason, paths, denied, guard := hookDenial(dir, "child-run")
	if denied || reason != "" || len(paths) != 0 || !slices.Equal(guard, []string{"Không sửa mã."}) {
		t.Fatalf("guardrail deny phải không chặn: %q %v %v %v", reason, paths, denied, guard)
	}
	if remaining, err := spool.Denials(dir, "child-run"); err != nil || len(remaining) != 0 {
		t.Fatalf("nhật ký deny chưa được dọn: %d, %v", len(remaining), err)
	}
}

func TestHookDenialSurvivesConcurrentSpoolIngest(t *testing.T) {
	h := newHarness(t)
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "denial-during-ingest", Type: spool.ToolDenied,
		Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: "child-run", Payload: map[string]any{
			"protected_paths": []any{"migrations/001.sql"}, "reason": "protected path denied"}}
	if err := spool.WriteDenial(h.spool, ev); err != nil {
		t.Fatal(err)
	}
	h.ingest()
	reason, paths, denied, _ := hookDenial(h.spool, "child-run")
	if !denied || reason != "protected path denied" || !slices.Equal(paths, []string{"migrations/001.sql"}) {
		t.Fatalf("ingest consumed active child denial: %q %v %v", reason, paths, denied)
	}
}

func TestRunPersistsEffectiveAgentChoicesInTaskState(t *testing.T) {
	h := newHarness(t)
	choices := map[string]task.AgentChoice{
		"planner":     {Harness: "codex", Model: "planner-model"},
		"implementer": {Harness: "codex", Model: "implementer-model"},
		"reviewer":    {Harness: "codex", Model: "reviewer-model"},
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Update app.txt", Plan: ptr(false), NoReview: true,
		AgentChoices: choices})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := h.tasks.Load(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tk.State.AgentChoices, choices) {
		t.Fatalf("saved choices = %#v, want %#v", tk.State.AgentChoices, choices)
	}
}

func TestBusyRunnerDoesNotPersistRejectedAgentChoices(t *testing.T) {
	h := newHarness(t)
	old := map[string]task.AgentChoice{"implementer": {Harness: "claude", Model: "old-model"}}
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Update app.txt", Workflow: StepFeat})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.tasks.Update(tk.State.ID, time.Now(), func(tk *task.Task) error {
		tk.State.AgentChoices = old
		tk.State.Runner = &task.Runner{PID: 1, Host: "another-host", RunID: "active"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID,
		AgentChoices: map[string]task.AgentChoice{"implementer": {Harness: "codex", Model: "rejected-model"}}})
	var busy *task.BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected busy runner error, got %v", err)
	}
	got, err := h.tasks.Load(tk.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.State.AgentChoices, old) {
		t.Fatalf("rejected run changed saved choices: %+v", got.State.AgentChoices)
	}
}

var stepRe = regexp.MustCompile(`step="([a-z-]+)"`)

// fakeAgent mô phỏng agent: đọc bước từ đề bài, gọi thao tác tác vụ như khi chạy
// `bint task …` với BINT_RUN_ID/BINT_TASK_ID của bước.
type fakeAgent struct {
	h    *harness
	cost *float64
	// turns: số lượt agent báo về mỗi bước.
	turns *int64
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
	a.h.mu.Lock()
	a.h.calls = append(a.h.calls, step)
	a.h.dirs = append(a.h.dirs, req.Dir)
	a.h.prompts[step] = req.Prompt
	a.h.reqs = append(a.h.reqs, req)
	if p, err := mcpgw.RunBindingPath(filepath.Join(filepath.Dir(a.h.spool), "runtime"), envs[EnvRunID]); err == nil {
		b, _ := os.ReadFile(p)
		a.h.bindings = append(a.h.bindings, b)
	}
	if req.MCPConfig != "" {
		body, _ := os.ReadFile(req.MCPConfig) // đọc ngay: Runner xoá tệp khi bước kết thúc
		a.h.mcpBodies = append(a.h.mcpBodies, body)
	}
	a.h.mu.Unlock()
	// Root = thư mục chạy thật của bước (cây gốc, hoặc worktree riêng của tác vụ).
	agentEnv := &Env{Root: req.Dir, Cfg: a.h.cfg, Tasks: a.h.tasks, Spool: a.h.spool, Actor: "agent", RunID: envs[EnvRunID]}
	res := eval.AgentResult{CostUSD: a.cost, NumTurns: a.turns, Duration: time.Millisecond, Model: "fake-model"}
	a.h.mu.Lock()
	var paths []string
	if step == StepFeat && len(a.h.denyNext) > 0 {
		paths = append([]string{}, a.h.denyNext...)
		a.h.denyNext = nil
	}
	a.h.mu.Unlock()
	if len(paths) > 0 {
		err := spool.WriteDenial(a.h.spool, spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "test-denial-" + envs[EnvRunID],
			Type: spool.ToolDenied, Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: envs[EnvRunID],
			Payload: map[string]any{"paths": paths, "protected_paths": paths, "reason": "Codex bị hook chặn: " + strings.Join(paths, ", ")}})
		if err != nil {
			res.ExitCode, res.Error = 1, err.Error()
		}
	} else if err := a.h.behave(ctx, step, agentEnv, envs[EnvTaskID]); err != nil {
		res.ExitCode, res.Error = 1, err.Error()
	}
	if ctx.Err() != nil {
		res.ExitCode, res.Error = -1, ctx.Err().Error()
	}
	return res
}

type harness struct {
	// mu bảo vệ calls/prompts/limits khi nhiều Runner chạy đồng thời trên cùng harness.
	mu    sync.Mutex
	t     *testing.T
	root  string
	cfg   config.Config
	tasks task.Store
	spool string
	db    string
	calls []string
	dirs  []string // Dir của từng lần gọi agent
	// reqs / mcpBodies: yêu cầu giao cho agent và nội dung tệp MCPConfig tại lúc gọi.
	reqs      []eval.AgentRequest
	mcpBodies [][]byte
	bindings  [][]byte // bản ghi lượt chạy (mcpgw.RunBinding) tại lúc gọi agent
	prompts   map[string]string
	behave    func(ctx context.Context, step string, e *Env, taskID string) error
	limits    []*float64
	cost      *float64
	denyNext  []string
	// costFor (nếu có) thay h.cost theo từng bước: mô phỏng harness khi báo khi không báo chi phí.
	costFor func(step string) *float64
	// turns / turnLimits: số lượt mỗi bước báo về và giới hạn lượt Runner truyền xuống.
	turns      *int64
	turnLimits []*int
}

func newHarness(t *testing.T) *harness {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	st := t.TempDir()
	cfg := config.Default()
	cmd := "test -f done.txt"
	cfg.Eval.TestCommand = &cmd
	// Các test cũ kiểm hành vi chế độ shared (nhiều tác vụ trên một cây); test worktree đặt isolation riêng.
	cfg.Workflow.Isolation = config.IsolationShared
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
		return h.writePlan(e, id, nil)
	case StepFeat:
		h.write("app.txt", "v2\n")
		h.write("done.txt", "ok\n")
	case StepReview:
		_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[{"kind":"suggestion","severity":"low","title":"đặt tên","path":"app.txt","lines":"1","evidence":"tên chưa rõ"}]}`))
		return err
	}
	return nil
}

// validDocs là bộ artifact hợp đồng v1 hợp lệ cho một tiêu chí; extra nối vào requirement.md.
func validDocs(criterion, extra string) map[string]string {
	return map[string]string{
		"requirement": "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: " + criterion + "\n## Ngoài phạm vi\nKhông có\n## Câu hỏi mở\n" + extra,
		"plan":        "# Kế hoạch\nSửa app.txt\n",
		"tasks":       "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: " + criterion + "\n",
	}
}

// writePlan mô phỏng agent b-req: lưu tiêu chí vào phiếu rồi ghi ba artifact khớp nó.
// skip là tên artifact bỏ qua.
func (h *harness) writePlan(e *Env, id string, skip map[string]bool) error {
	const crit = "app.txt ghi v2"
	if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{crit}}); err != nil {
		return err
	}
	for n, body := range validDocs(crit, "") {
		if skip[n] {
			continue
		}
		if _, _, err := e.WriteArtifact(id, n, body); err != nil {
			return err
		}
	}
	return nil
}

func (h *harness) runner() *Runner {
	env := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool, Human: true, Actor: "người duyệt", Version: "test"}
	return &Runner{Env: env, Agent: func(step string, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		h.mu.Lock()
		h.limits = append(h.limits, maxCost)
		h.turnLimits = append(h.turnLimits, maxTurns)
		h.mu.Unlock()
		cost := h.cost
		if h.costFor != nil {
			cost = h.costFor(step)
		}
		return &fakeAgent{h: h, cost: cost, turns: h.turns}, nil
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

func TestApproveUnwrittenPathRerunsFeat(t *testing.T) {
	h := newHarness(t)
	// Như Codex đã bị deny thật: bước kết thúc nhưng chưa có diff.
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	r := h.runner()
	ctx := context.Background()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Create a migration", Plan: ptr(false), NoReview: true})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("blocked: %+v %v", res, err)
	}
	if _, a, err := r.Approve(res.TaskID, "migrations/002.sql"); err != nil || len(a.Paths) != 1 {
		t.Fatalf("approve: %+v %v", a, err)
	}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		tk, _ := h.tasks.Load(id)
		if !strings.Contains(strings.Join(ApprovedPaths(tk), ","), "migrations/002.sql") {
			t.Fatal("mất duyệt đường dẫn")
		}
		h.write("migrations/002.sql", "SELECT 1;\n")
		return h.happy(ctx, step, e, id)
	}
	res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, NoReview: true})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-feat" {
		t.Fatalf("resume: %+v %v, calls=%v", res, err, h.calls)
	}
}

func TestCodexHookDenyBlocksBeforeDiffThenApprovalResumes(t *testing.T) {
	h := newHarness(t)
	h.denyNext = []string{"migrations/002.sql"}
	r := h.runner()
	ctx := context.Background()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Create a migration", Plan: ptr(false), NoReview: true})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("deny must block: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "migrations/002.sql")); !os.IsNotExist(err) {
		t.Fatalf("deny should happen before diff, stat err=%v", err)
	}
	tk, _ := h.tasks.Load(res.TaskID)
	if !slices.Equal(tk.State.PendingPaths, []string{"migrations/002.sql"}) || tk.State.AwaitingCheck || !strings.Contains(tk.State.Reason, "migrations/002.sql") {
		t.Fatalf("blocked state lost hook denial: %+v", tk.State)
	}
	if _, a, err := r.Approve(res.TaskID, "migrations/002.sql"); err != nil || !slices.Equal(a.Paths, []string{"migrations/002.sql"}) {
		t.Fatalf("approve exact path: %+v %v", a, err)
	}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		h.write("migrations/002.sql", "SELECT 1;\n")
		return h.happy(ctx, step, e, id)
	}
	resumed, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, NoReview: true})
	if err != nil || resumed.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-feat" {
		t.Fatalf("resume after exact approval: %+v %v calls=%v", resumed, err, h.calls)
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

// P5: bước chỉ đọc sửa cây làm việc thì thất bại, bất kể harness hứa gì.
func TestReadOnlyStepModifyingTreeFails(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			knowledge.NewStore(h.root).Save(newItem(knowledge.StatusCandidate)) // candidate mới do bint ghi: không tính
			h.write("app.txt", "sửa lén\n")
		}
		return h.happy(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	last := res.Steps[len(res.Steps)-1]
	if last.Step != StepReview || last.Status != "failed" || !strings.Contains(last.Error, "bước chỉ đọc") ||
		!strings.Contains(last.Error, "app.txt") || strings.Contains(last.Error, ".bint") {
		t.Fatalf("bước: %+v", res.Steps)
	}
	if res.Status != task.StatusFailed {
		t.Fatalf("tác vụ: %s", res.Status)
	}
}

// P5-02: bước chỉ đọc không được sửa cấu hình/chính sách trong .bint/.
func TestReadOnlyStepModifyingBintConfigFails(t *testing.T) {
	h := newHarness(t)
	h.write(".bint/config.yml", "security:\n  require_approval_for_commands: true\n")
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			h.write(".bint/config.yml", "security:\n  require_approval_for_commands: false\n")
		}
		return h.happy(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	last := res.Steps[len(res.Steps)-1]
	if last.Status != "failed" || !strings.Contains(last.Error, ".bint/config.yml") {
		t.Fatalf("bước: %+v", res.Steps)
	}
}

// P5: một worktree chỉ có một bước có quyền ghi tại một thời điểm; --no-wait thì dừng ngay.
func TestWriteLockSerializesWriteSteps(t *testing.T) {
	h := newHarness(t)
	lock, err := AcquireWriteLock(h.tasks.Dir, h.root, "phiên khác")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt", NoWait: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "giữ khoá") || !strings.Contains(res.Reason, "phiên khác") ||
		strings.Contains(strings.Join(h.calls, ","), StepFeat) {
		t.Fatalf("phải chặn b-feat: %+v %v", res, h.calls)
	}
	lock.Release()
	// Khoá trả xong: b-feat chạy được, tệp khoá được giữ lại.
	res, err = h.runner().Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: res.TaskID, Resume: true, NoWait: true})
	if err != nil || res.Status != task.StatusInReview {
		t.Fatalf("sau khi trả khoá: %+v %v", res, err)
	}
}

// P5: adapter không khả dụng → bước thất bại với lỗi rõ ràng, không để lại khoá.
func TestUnavailableAdapterFailsStep(t *testing.T) {
	h := newHarness(t)
	h.cfg.Harnesses.Enabled = []string{"claude", "codex"}
	h.cfg.Harnesses.Codex.Binary = filepath.Join(t.TempDir(), "khong-co")
	f := HarnessAgents(h.cfg, "", "", adapters.Selection{Roles: map[string]string{"implementer": "codex"}})
	if _, err := f(StepFeat, nil, nil); err == nil || !strings.Contains(err.Error(), "không khả dụng") {
		t.Fatalf("err = %v", err)
	}
	if _, err := HarnessAgents(h.cfg, "", "", adapters.Selection{All: "antigravity"})(StepReq, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "harnesses.enabled") {
		t.Fatalf("harness chưa bật: %v", err)
	}
	r := h.runner()
	r.Agent = func(step string, c *float64, n *int) (eval.Agent, error) {
		if step == StepFeat {
			return f(step, c, n)
		}
		return &fakeAgent{h: h}, nil
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusFailed || !strings.Contains(res.Reason, "không khả dụng") {
		t.Fatalf("%+v", res)
	}
	l, err := AcquireLock(h.tasks.Dir, h.root, "kiểm tra", LockExclusive)
	if err != nil {
		t.Fatalf("khoá còn sót: %v", err)
	}
	l.Release()
}

// A2: limits.workflows.b-do.max_turns là tổng mọi bước; truyền phần còn lại xuống bước
// và dừng khi hết.
func TestBDoMaxTurnsIsTotalBudget(t *testing.T) {
	h := newHarness(t)
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxTurns: ptr(3)}}
	h.turns = ptr(int64(2))
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng mới"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req,b-feat" || !strings.Contains(res.Reason, "max_turns") {
		t.Fatalf("%+v %v", res, h.calls)
	}
	if len(h.turnLimits) != 2 || h.turnLimits[0] == nil || *h.turnLimits[0] != 3 || h.turnLimits[1] == nil || *h.turnLimits[1] != 1 {
		t.Fatalf("giới hạn lượt từng bước: %v", h.turnLimits)
	}
}

// A2: không cấu hình riêng b-do thì per_run.max_turns vẫn là tổng của lượt điều phối.
func TestBDoMaxTurnsFromPerRun(t *testing.T) {
	h := newHarness(t)
	h.cfg.Limits.PerRun.MaxTurns = ptr(3)
	h.turns = ptr(int64(2))
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng mới"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req,b-feat" {
		t.Fatalf("%+v %v", res, h.calls)
	}
}

// A1: SetStatus(done) tự tính lại diff, không dựa vào PendingPaths (chỉ Runner điền).
func TestDoneRechecksProtectedPathsFromDiff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.cfg.Eval.TestCommand = nil
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepReq, Request: "Đổi app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	h.write("migrations/001.sql", "-- sửa\n")
	h.tasks.Update(res.TaskID, time.Now(), func(t *task.Task) error {
		t.State.PendingPaths = nil
		t.Ticket.Verify.Criteria = []string{"đã đổi"}
		t.State.Criteria = []task.CriterionCheck{{Index: 1, Met: true, Evidence: "agent tự nhận"}}
		return nil
	})
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	_, blockers, err := agent.SetStatus(ctx, res.TaskID, task.StatusDone, TransitionOptions{})
	var nd *NotDoneError
	if !errors.As(err, &nd) || !strings.Contains(strings.Join(blockers, ";"), "migrations/001.sql") {
		t.Fatalf("done phải bị chặn vì migrations/**: %v %v", err, blockers)
	}
	// Người duyệt xong thì không còn bị chặn bởi đường dẫn này.
	human := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool, Human: true, Actor: "người"}
	t2, _ := h.tasks.Load(res.TaskID)
	t2.State.ProtectedApproval = nil
	pend, err := human.UnapprovedChanges(ctx, t2)
	if err != nil || len(pend) != 1 {
		t.Fatalf("UnapprovedChanges: %v %v", pend, err)
	}
	h.tasks.Update(res.TaskID, time.Now(), func(t *task.Task) error {
		t.State.ProtectedApproval = &task.Approval{Paths: []string{"migrations/001.sql"}, By: "người", At: "x"}
		return nil
	})
	t3, _ := h.tasks.Load(res.TaskID)
	if pend, _ := human.UnapprovedChanges(ctx, t3); len(pend) != 0 {
		t.Fatalf("đã duyệt mà vẫn chặn: %v", pend)
	}
}

// Thực tế (Claude lập kế hoạch/review, Codex thực thi): Codex không báo chi phí. Tổng của
// b-do không được trình bày như tổng thật khi thiếu một bước; chi phí đã biết ghi riêng.
func TestBDoCostIsNullWhenAStepDoesNotReportCost(t *testing.T) {
	h := newHarness(t)
	h.costFor = func(step string) *float64 {
		if step == StepFeat {
			return nil
		}
		c := 0.3
		return &c
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng mới"})
	if err != nil {
		t.Fatal(err)
	}
	if res.CostUSD != nil {
		t.Fatalf("tổng một phần bị trình bày như tổng thật: %v", *res.CostUSD)
	}
	if res.CostPartialUSD == nil || *res.CostPartialUSD < 0.59 || *res.CostPartialUSD > 0.61 {
		t.Fatalf("chi phí đã biết: %v", res.CostPartialUSD)
	}
	for _, st := range res.Steps {
		if (st.Step == StepFeat) != (st.CostUSD == nil) {
			t.Fatalf("chi phí từng bước: %+v", st)
		}
	}
	files, _ := filepath.Glob(filepath.Join(h.spool, "*.jsonl"))
	seen := false
	for _, f := range files {
		evs, err := spool.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if ev.RunID == res.RunID && ev.Type == spool.WorkflowFinished {
				seen = true
				if _, ok := ev.Payload["cost_usd"]; ok {
					t.Fatalf("workflow.finished của b-do có cost_usd dù thiếu một bước: %v", ev.Payload)
				}
				if ev.Payload["cost_usd_partial"] == nil {
					t.Fatalf("thiếu cost_usd_partial: %v", ev.Payload)
				}
			}
		}
	}
	if !seen {
		t.Fatal("không thấy workflow.finished của b-do")
	}
}

// Thực tế (C2, Codex thật): sau khi được duyệt và chạy lại b-feat, agent làm xong phần còn
// thiếu nhưng không ghi lại tiêu chí 2 vì hiểu rộng "không gọi bint task status/approve/verify";
// tiêu chí giữ bằng chứng cũ "chưa tạo" và task kẹt blocked. Prompt b-feat chạy lại phải nêu
// tiêu chí còn unmet cùng bằng chứng cũ và nhắc ghi lại bằng `bint task criteria`.
func TestFeatPromptListsUnmetCriteriaOnRerun(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	calls := 0
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			calls++
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			if calls == 1 {
				if _, err := e.CheckCriterion(id, 1, true, "app.txt đã đổi"); err != nil {
					return err
				}
				_, err := e.CheckCriterion(id, 2, false, "tệp mới chưa tạo: cần duyệt")
				return err
			}
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Đổi app.txt và tạo tệp mới", Plan: ptr(false),
		Edit: TicketEdit{Criteria: []string{"app.txt đã đổi", "tệp mới tồn tại"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked {
		t.Fatalf("tiêu chí unmet phải chặn: %+v", res)
	}
	first := h.prompts[StepFeat]
	if strings.Contains(first, "Tiêu chí chưa đạt") {
		t.Fatalf("lượt đầu chưa có trạng thái tiêu chí:\n%s", first)
	}
	if _, err := r.Tasks.Update(res.TaskID, time.Now(), func(tk *task.Task) error { tk.State.ResumeFrom = task.StatusInProgress; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	p := h.prompts[StepFeat]
	for _, want := range []string{"Tiêu chí chưa đạt", "2. tệp mới tồn tại", "tệp mới chưa tạo: cần duyệt", "bint task criteria"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt b-feat chạy lại thiếu %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "1. app.txt đã đổi") {
		t.Fatalf("không liệt kê tiêu chí đã đạt:\n%s", p)
	}
}

func TestMergedHookDenyReleasesLockAndRecordsTouched(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		h.write("app.txt", "changed before denial\n")
		return spool.WriteDenial(h.spool, spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "denial-" + e.RunID, Type: spool.ToolDenied, Source: "hook:codex", Timestamp: time.Now().UTC(), RunID: e.RunID, Payload: map[string]any{"protected_paths": []string{"migrations/002.sql"}, "reason": "denied"}})
	}
	res := h.runFeat("Update app.txt", "")
	tk := h.load(res.TaskID)
	if res.Status != task.StatusBlocked || len(res.Steps) != 1 || res.Steps[0].Status != "blocked" || !slices.Equal(tk.State.PendingPaths, []string{"migrations/002.sql"}) || tk.State.AwaitingCheck || !slices.Contains(tk.State.Touched, "app.txt") {
		t.Fatalf("result=%+v state=%+v", res, tk.State)
	}
	l, err := AcquireLock(h.tasks.Dir, h.root, "another runner", LockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}
