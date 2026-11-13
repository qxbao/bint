package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/task"
	"bint/internal/workflow"
)

const appTDDFlow = `schema: 1
name: tdd
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: b-feat, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

func tddFlowDef(t *testing.T) flow.Def {
	t.Helper()
	d, err := flow.Decode([]byte(appTDDFlow))
	if err != nil {
		t.Fatal(err)
	}
	return flow.Normalize(d)
}

// Ảnh chụp lựa chọn có khoá theo role thật của flow (kể cả role tuỳ biến): harness từ roles.<role>, model từ models.<role>,
// role chưa cấu hình rơi về role gốc theo kind.
func TestAgentChoicesCustomRoleSnapshot(t *testing.T) {
	fd := tddFlowDef(t)
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex"}
	cfg.Roles.Extra = map[string]string{"test-writer": "codex"}
	cfg.Harnesses.Codex.Models.Extra = map[string]string{"test-writer": "gpt-tw"}

	t.Run("role cấu hình", func(t *testing.T) {
		got, err := agentChoicesForRoles(cfg, nil, "", nil, "", rolesForRun(fd, nil, false), roleBases(fd))
		if err != nil {
			t.Fatal(err)
		}
		if c := got["test-writer"]; c.Harness != "codex" || c.Model != "gpt-tw" {
			t.Errorf("test-writer = %+v", c)
		}
		for _, role := range []string{"planner", "implementer", "reviewer"} {
			if got[role].Harness != "claude" {
				t.Errorf("%s = %+v", role, got[role])
			}
		}
	})
	t.Run("role chưa cấu hình ⇒ theo role gốc", func(t *testing.T) {
		c := cfg
		c.Roles.Extra, c.Harnesses.Codex.Models.Extra = nil, nil
		c.Roles.Implementer = "codex"
		c.Harnesses.Codex.Models.Implementer = "gpt-impl"
		got, err := agentChoicesForRoles(c, nil, "", nil, "", rolesForRun(fd, nil, false), roleBases(fd))
		if err != nil {
			t.Fatal(err)
		}
		if tw := got["test-writer"]; tw.Harness != "codex" || tw.Model != "gpt-impl" {
			t.Errorf("test-writer phải theo implementer: %+v", tw)
		}
	})
	t.Run("--model áp cho mọi role đang chạy kể cả role tuỳ biến", func(t *testing.T) {
		got, err := agentChoicesForRoles(cfg, nil, "", nil, "cli-model", rolesForRun(fd, nil, false), roleBases(fd))
		if err != nil || got["test-writer"].Model != "cli-model" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("resume giữ lựa chọn đã chụp của role tuỳ biến", func(t *testing.T) {
		prior := map[string]task.AgentChoice{"test-writer": {Harness: "claude", Model: "saved"}}
		got, err := agentChoicesForRoles(cfg, prior, "", nil, "", rolesForRun(fd, nil, false), roleBases(fd))
		if err != nil || got["test-writer"].Harness != "claude" || got["test-writer"].Model != "saved" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("chỉ chụp role đang chạy", func(t *testing.T) {
		// Tác vụ đã qua bước plan (in_progress): planner không chạy nữa nên không được chụp (Harness rỗng = chưa chụp).
		tk := &task.Task{State: task.State{Status: task.StatusInProgress}}
		got, err := agentChoicesForRoles(cfg, nil, "", nil, "", rolesForRun(fd, tk, false), roleBases(fd))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["planner"]; ok {
			t.Errorf("planner không chạy mà vẫn được chụp: %+v", got["planner"])
		}
		if got["test-writer"].Harness != "codex" || got["implementer"].Harness != "claude" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("harness chưa bật ⇒ lỗi", func(t *testing.T) {
		c := cfg
		c.Harnesses.Enabled = []string{"claude"}
		if _, err := agentChoicesForRoles(c, nil, "", nil, "", rolesForRun(fd, nil, false), roleBases(fd)); err == nil || !strings.Contains(err.Error(), "harnesses.enabled") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunWorkflowValidateStep(t *testing.T) {
	cases := []struct {
		name string
		o    RunWorkflowOptions
		want string // "" = hợp lệ
	}{
		{"flow tuỳ biến với --task", RunWorkflowOptions{Workflow: "tdd", Task: "x", Step: "red"}, ""},
		{"không có --task", RunWorkflowOptions{Workflow: "tdd", Step: "red"}, "--step chỉ dùng với --task"},
		{"workflow gốc", RunWorkflowOptions{Workflow: "b-feat", Task: "x", Step: "red"}, "--step chỉ dùng với flow tuỳ biến"},
		{"flow tuỳ biến, không --step", RunWorkflowOptions{Workflow: "tdd", Request: "x"}, ""},
		{"--depth với workflow gốc không có plan", RunWorkflowOptions{Workflow: "b-feat", Depth: "deep"}, "b-do hoặc b-req"},
		{"--depth với flow tuỳ biến hoãn tới Runner", RunWorkflowOptions{Workflow: "tdd", Depth: "deep"}, ""},
	}
	for _, c := range cases {
		err := c.o.Validate()
		var ue UsageError
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !errors.As(err, &ue) || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: err = %v, muốn lỗi dùng sai chứa %q", c.name, err, c.want)
		}
	}
}

// stepWriter là agent giả của flow tuỳ biến: red và green ghi a.go (đúng phạm vi); mọi bước chỉ đọc không ghi gì.
type stepWriter struct{ root string }

func (a stepWriter) Name() string                   { return "fake" }
func (a stepWriter) Version(context.Context) string { return "" }
func (a stepWriter) ReportsCost() bool              { return false }
func (a stepWriter) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	if strings.Contains(req.Prompt, `step="red"`) || strings.Contains(req.Prompt, `step="green"`) {
		os.WriteFile(filepath.Join(a.root, "a.go"), []byte("package a\n\nfunc A() { _ = 2 }\n"), 0o644)
	}
	return eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
}

// task new --workflow <flow> rồi bint run <flow> --task: phiếu mang flow và băm, ảnh chụp lựa chọn có role tuỳ biến, agent của
// từng bước nhận spec đúng (role, chỉ đọc theo kind).
func TestRunCustomFlowThroughService(t *testing.T) {
	ws, root, _ := taskRepo(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, ".bint", "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".bint", "flows", "tdd.yml"), []byte(appTDDFlow), 0o644); err != nil {
		t.Fatal(err)
	}
	ws.K.Config.Harnesses.Claude.Models.Extra = map[string]string{"test-writer": "haiku"}

	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.VerifyCommand, o.Criteria, o.Workflow, o.NoPlan = "Sửa hàm A trong a.go", "git --version", []string{"A đúng"}, "tdd", true
	c, err := NewTask(ctx, ws, o)
	if err != nil {
		t.Fatalf("task new --workflow tdd: %v", err)
	}
	id := c.Task.State.ID
	if c.Task.State.Workflow != "tdd" || !strings.HasPrefix(c.Task.State.FlowSHA, "sha256:") {
		t.Fatalf("phiếu: %+v", c.Task.State)
	}
	if _, err := NewTask(ctx, ws, TaskNewOptions{Request: "x", Workflow: "khong-co"}); err == nil || !errors.Is(err, workflow.ErrUnknownWorkflow) {
		t.Fatalf("flow lạ: %v", err)
	}

	type call struct {
		id, role string
		ro       bool
	}
	var calls []call
	cb := RunCallbacks{Agent: func(sp workflow.StepSpec, _ *float64, _ *int) (eval.Agent, error) {
		calls = append(calls, call{sp.ID, sp.Role, sp.ReadOnly})
		return stepWriter{root}, nil
	}}
	run := RunWorkflowDefaults(ws.K.Config)
	run.Workflow, run.Task, run.NoReview = "tdd", id, true
	run.RunCallbacks = cb
	res, err := RunWorkflow(ctx, ws, run)
	if err != nil {
		t.Fatal(err)
	}
	// Lệnh kiểm chứng chưa được người xác nhận ⇒ blocked sau bước implement cuối, nhưng cả hai bước implement đã chạy.
	if res.Status != task.StatusBlocked {
		t.Fatalf("kết quả: %+v", res)
	}
	want := []call{{"red", "test-writer", false}, {"green", "implementer", false}}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("spec các bước = %+v, muốn %+v", calls, want)
	}
	tk, err := ws.Tasks.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if c := tk.State.AgentChoices["test-writer"]; c.Harness != "claude" || c.Model != "haiku" {
		t.Fatalf("ảnh chụp lựa chọn: %+v", tk.State.AgentChoices)
	}
	if _, ok := tk.State.AgentChoices["reviewer"]; !ok {
		t.Logf("reviewer không chạy (--no-review): %+v", tk.State.AgentChoices)
	}
}

// bint doctor: cảnh báo khoá limits.workflows và role tuỳ biến không trỏ tới flow nào.
func TestDoctorFlowChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bint", "flows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".bint", "flows", "tdd.yml"), []byte(appTDDFlow), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	maxTurns := 5
	cfg.Limits.Workflows = map[string]config.PerRunLimits{"b-do": {MaxTurns: &maxTurns}, "tdd": {MaxTurns: &maxTurns},
		"red": {MaxTurns: &maxTurns}, "tdd-typo": {MaxTurns: &maxTurns}}
	cfg.Roles.Extra = map[string]string{"test-writer": "claude", "test-writter": "claude"}
	cfg.Harnesses.Codex.Models.Extra = map[string]string{"test-writter": "x", "test-writer": "y"}
	r := &DoctorReport{}
	flowChecks(r, root, cfg)
	var got []string
	for _, c := range r.Checks {
		if c.Level != LevelWarn {
			t.Errorf("mức %s: %+v", c.Level, c)
		}
		got = append(got, c.Name+": "+c.Detail)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{`khoá "tdd-typo"`, "roles.test-writter"} {
		if !strings.Contains(joined, want) {
			t.Errorf("thiếu cảnh báo %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{`"tdd"`, `"b-do"`, `"red"`, "roles.test-writer ", "test-writer không"} {
		if strings.Contains(joined, bad) {
			t.Errorf("cảnh báo thừa %q:\n%s", bad, joined)
		}
	}
	if len(r.Checks) != 2 {
		t.Errorf("muốn đúng 2 cảnh báo, được %d:\n%s", len(r.Checks), joined)
	}
}
