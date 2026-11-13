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
	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/testutil"
	"bint/internal/workflow"
)

// taskRepo dựng repo đã init kèm một tác vụ (có lệnh kiểm chứng) và mở môi trường tác vụ với tư cách người.
func taskRepo(t *testing.T) (*TaskWS, string, string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	ctx := context.Background()
	ws, err := OpenTasks(ctx, root, "test")
	if err != nil {
		t.Fatal(err)
	}
	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.VerifyCommand, o.Criteria = "Sửa hàm A trong a.go", "git --version", []string{"A đúng"}
	c, err := NewTask(ctx, ws, o)
	if err != nil {
		t.Fatal(err)
	}
	return ws, root, c.Task.State.ID
}

func TestOpenTasksNeedsInit(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	if _, err := OpenTasks(context.Background(), root, "x"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("repo chưa init phải trả ErrNotInitialized: %v", err)
	}
}

func TestTaskOptionsValidateTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // "" = hợp lệ; khác: chuỗi con của thông báo UsageError
	}{
		{"new ok", TaskNewOptions{Request: "làm X", Workflow: "b-do", Depth: "auto"}.Validate(), ""},
		{"new thiếu mô tả", TaskNewOptions{Request: "  "}.Validate(), "thiếu mô tả"},
		{"new plan+no-plan", TaskNewOptions{Request: "x", Plan: true, NoPlan: true}.Validate(), "loại trừ"},
		{"new depth deep + no-plan", TaskNewOptions{Request: "x", NoPlan: true, Depth: "deep"}.Validate(), "--no-plan"},
		{"new depth auto + no-plan ok", TaskNewOptions{Request: "x", NoPlan: true, Depth: "auto"}.Validate(), ""},
		{"new depth sai giá trị", TaskNewOptions{Request: "x", Depth: "sâu"}.Validate(), "--depth"},
		{"new depth với b-feat", TaskNewOptions{Request: "x", Workflow: "b-feat", Depth: "deep"}.Validate(), "b-do hoặc b-req"},
		{"run thiếu workflow", RunWorkflowOptions{}.Validate(), "thiếu workflow"},
		{"run ok", RunWorkflowOptions{Workflow: "b-do", Request: "x", Depth: "auto"}.Validate(), ""},
		{"run plan+no-plan", RunWorkflowOptions{Workflow: "b-do", Plan: true, NoPlan: true}.Validate(), "loại trừ"},
		{"run harness lạ", RunWorkflowOptions{Workflow: "b-do", Harness: "bogus"}.Validate(), "harness không hỗ trợ"},
		{"run role sai cú pháp", RunWorkflowOptions{Workflow: "b-do", Roles: []string{"implementer"}}.Validate(), "role=harness"},
		{"run agent-command + harness", RunWorkflowOptions{Workflow: "b-do", AgentCommand: "x", Harness: "claude"}.Validate(), "--agent-command"},
		{"run agent-command + role", RunWorkflowOptions{Workflow: "b-do", AgentCommand: "x", Roles: []string{"implementer=codex"}}.Validate(), "--agent-command"},
		{"run role hợp lệ", RunWorkflowOptions{Workflow: "b-do", Roles: []string{"implementer=codex"}}.Validate(), ""},
		{"resume thiếu id", RunResumeOptions{}.Validate(), "cần ID"},
		{"resume agent-command + harness", RunResumeOptions{Ref: "x", AgentCommand: "x", Harness: "claude"}.Validate(), "--agent-command"},
		{"criteria số 0", TaskCriteriaOptions{Index: 0, Result: "met"}.Validate(), "số tiêu chí"},
		{"criteria kết quả sai", TaskCriteriaOptions{Index: 1, Result: "ok"}.Validate(), "met hoặc unmet"},
		{"criteria ok", TaskCriteriaOptions{Index: 2, Result: "UNMET"}.Validate(), ""},
		{"set thiếu id", TaskSetOptions{}.Validate(), "cần ID"},
		{"approve thiếu id", TaskApproveOptions{}.Validate(), "cần ID"},
		{"runs thiếu id", TaskRunsOptions{}.Validate(), "cần ID"},
	}
	for _, c := range cases {
		switch {
		case c.want == "" && c.err != nil:
			t.Errorf("%s: không mong lỗi, được %v", c.name, c.err)
		case c.want != "" && c.err == nil:
			t.Errorf("%s: mong lỗi chứa %q", c.name, c.want)
		case c.want != "":
			if !IsUsage(c.err) {
				t.Errorf("%s: phải là UsageError (mã thoát 2): %v", c.name, c.err)
			}
			if !strings.Contains(c.err.Error(), c.want) {
				t.Errorf("%s: %q không chứa %q", c.name, c.err, c.want)
			}
		}
	}
}

func TestTaskDefaults(t *testing.T) {
	cfg := config.Default()
	if d := TaskNewDefaults(cfg); d.Workflow != workflow.StepDo || d.Depth != task.DepthAuto {
		t.Fatalf("mặc định task new: %+v", d)
	}
	if d := RunWorkflowDefaults(cfg); d.Depth != task.DepthAuto {
		t.Fatalf("mặc định run: %+v", d)
	}
}

// Thao tác chỉ dành cho người bị chặn ở dịch vụ khi có biến agent; trở lại là người thì chạy được.
func TestTaskHumanOnlyOperations(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()

	asAgent(t)
	auto := "auto"
	cases := map[string]func() error{
		"approve": func() error { _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); return err },
		"status --force": func() error {
			_, err := SetTaskStatus(ctx, ws, TaskStatusOptions{Ref: id, To: task.StatusDone, Force: true})
			return err
		},
		"new autonomy auto": func() error {
			o := TaskNewDefaults(ws.K.Config)
			o.Request, o.Autonomy = "việc khác", auto
			_, err := NewTask(ctx, ws, o)
			return err
		},
		"set autonomy auto": func() error { _, err := SetTask(ctx, ws, TaskSetOptions{Ref: id, Autonomy: auto}); return err },
		"run autonomy auto": func() error {
			_, err := RunWorkflow(ctx, ws, RunWorkflowOptions{Workflow: "b-feat", Task: id, Autonomy: auto})
			return err
		},
	}
	for name, f := range cases {
		if err := f(); !errors.Is(err, ErrAgentForbidden) {
			t.Errorf("%s trong phiên agent phải trả ErrAgentForbidden: %v", name, err)
		}
	}
	// Tác vụ không đổi vì thao tác bị chặn.
	if tt, _ := ws.Tasks.Load(id); tt.State.VerifyApproval != nil || tt.Ticket.Autonomy == auto {
		t.Fatalf("thao tác bị chặn vẫn ghi: %+v", tt.State.VerifyApproval)
	}

	asHuman(t)
	ws.Human = true
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatalf("người duyệt phải được: %v", err)
	}
	if _, err := SetTask(ctx, ws, TaskSetOptions{Ref: id, Autonomy: auto}); err != nil {
		t.Fatalf("người đặt autonomy auto phải được: %v", err)
	}
}

// PlanTaskApprove chỉ đọc: không đổi state.json; liệt kê đúng lệnh kiểm chứng và đường dẫn chờ duyệt.
func TestPlanTaskApproveIsReadOnly(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	// Một đường dẫn chờ duyệt do Runner ghi.
	if _, err := ws.Tasks.Update(id, ws.Now(), func(tt *task.Task) error {
		tt.State.PendingPaths = []string{"migrations/001.sql"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tt, _ := ws.Tasks.Load(id)
	statePath := filepath.Join(tt.Dir, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTaskApprove(ctx, ws, TaskApproveOptions{Ref: id, Paths: []string{"x/y.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.VerifyCommand != "git --version" {
		t.Fatalf("lệnh sẽ xác nhận: %q", plan.VerifyCommand)
	}
	if strings.Join(plan.Paths, ",") != "migrations/001.sql,x/y.txt" {
		t.Fatalf("đường dẫn sẽ duyệt: %v", plan.Paths)
	}
	if plan.Empty() {
		t.Fatal("không rỗng")
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("PlanTaskApprove không được ghi state.json")
	}
	// Sau khi duyệt thật, kế hoạch rỗng phần lệnh kiểm chứng.
	if _, a, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil || a.VerifyCommand != "git --version" {
		t.Fatalf("approve: %v %+v", err, a)
	}
	plan, _ = PlanTaskApprove(ctx, ws, TaskApproveOptions{Ref: id})
	if plan.VerifyCommand != "" || len(plan.Paths) != 0 || !plan.Empty() {
		t.Fatalf("sau duyệt không còn gì chờ: %+v", plan)
	}
}

func TestShowTaskAndList(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	if _, err := WriteTaskArtifact(ctx, ws, TaskWriteOptions{Ref: id, Name: "requirement", Content: "# Yêu cầu\n"}); err != nil {
		t.Fatal(err)
	}
	d, err := ShowTask(ctx, ws, TaskShowOptions{Ref: id})
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.State.ID != id || len(d.Artifacts) != 3 || !d.Artifacts[0].Exists || d.Artifacts[1].Exists || d.Lint == nil {
		t.Fatalf("chi tiết: %+v", d)
	}
	if txt, _ := ReadTaskArtifact(d.Artifacts[0]); !strings.Contains(txt, "Yêu cầu") {
		t.Fatalf("nội dung tài liệu: %q", txt)
	}
	if txt, _ := ReadTaskArtifact(d.Artifacts[1]); txt != "" {
		t.Fatal("tài liệu chưa có phải rỗng")
	}
	l, err := ListTasks(ctx, ws, TaskListOptions{})
	if err != nil || len(l.Tasks) != 1 {
		t.Fatalf("danh sách: %v %+v", err, l)
	}
}

// FromReview điền tiêu đề/nội dung/bằng chứng/nhãn từ phát hiện review; người ghi verified, agent ghi candidate;
// ID sai là lỗi dùng sai.
func TestAddFromReview(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	if _, err := SubmitTaskReview(ctx, ws, TaskReviewOptions{Ref: id, JSON: `{"summary":"ổn","findings":[{"kind":"risk","severity":"medium",
		"title":"Hàm A rỗng","path":"a.go","lines":"3","evidence":"a.go:3 không có thân","body":"Thêm thân hàm."}]}`}); err != nil {
		t.Fatal(err)
	}
	fid := id + ".1"

	// Điền sẵn (không ghi): --type vắng thì mặc định warning.
	o := AddOptions{FromReview: fid}
	if err := o.FillFromReview(ws.K); err != nil {
		t.Fatal(err)
	}
	if o.Type != knowledge.TypeWarning || o.Title != "Hàm A rỗng" || len(o.Evidence) != 1 || o.Evidence[0] != "a.go:3" ||
		len(o.Paths) != 1 || o.Paths[0] != "a.go" || !strings.Contains(o.Body, "Thêm thân hàm.") || !strings.Contains(o.Body, fid) {
		t.Fatalf("điền sẵn: %+v", o)
	}
	if len(o.Tags) != 1 || o.Tags[0] != "review" {
		t.Fatalf("nhãn: %v", o.Tags)
	}
	if err := o.FillFromReview(ws.K); err != nil || len(o.Tags) != 1 || len(o.Inputs) != 1 {
		t.Fatalf("gọi lại không được nhân đôi: %v %+v", err, o)
	}

	// Người: verified ngay; AddKnowledge tự áp FromReview.
	res, err := AddKnowledge(ctx, ws.K, AddOptions{FromReview: fid})
	if err != nil || !res.Saved || res.Item.Status != knowledge.StatusVerified {
		t.Fatalf("người: %v %+v", err, res)
	}
	if !strings.Contains(strings.Join(res.Item.Scope.Tags, ","), "review") {
		t.Fatalf("thiếu nhãn review: %+v", res.Item.Scope)
	}
	// Agent: candidate.
	asAgent(t)
	res, err = AddKnowledge(ctx, ws.K, AddOptions{FromReview: fid, Title: "Tiêu đề khác", Body: "Nội dung riêng do agent viết, khác mục của người."})
	if err != nil || !res.Saved || res.Item.Status != knowledge.StatusCandidate || res.Item.Title != "Tiêu đề khác" {
		t.Fatalf("agent: %v %+v", err, res)
	}
	// ID sai và loại không hợp lệ.
	for _, bad := range []AddOptions{{FromReview: "khong-hop-le"}, {FromReview: fid, Type: knowledge.TypeRule}} {
		if _, err := AddKnowledge(ctx, ws.K, bad); !IsUsage(err) {
			t.Errorf("%+v phải là UsageError: %v", bad, err)
		}
	}
	if _, err := AddKnowledge(ctx, ws.K, AddOptions{FromReview: id + ".9"}); err == nil || !strings.Contains(err.Error(), "không tìm thấy phát hiện") {
		t.Errorf("phát hiện không tồn tại: %v", err)
	}
}

func TestEditorArgvAt(t *testing.T) {
	cases := []struct {
		editor string
		line   int
		want   string
	}{
		{"vim", 12, "vim +12 /x/a.go"}, {"nvim -u NONE", 3, "nvim -u NONE +3 /x/a.go"}, {"/usr/bin/nano", 7, "/usr/bin/nano +7 /x/a.go"},
		{"emacs", 1, "emacs +1 /x/a.go"}, {"hx", 5, "hx /x/a.go:5"}, {"code --wait", 9, "code --wait /x/a.go"}, {"vim", 0, "vim /x/a.go"},
	}
	for _, c := range cases {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", c.editor)
		if got := strings.Join(EditorArgvAt("/x/a.go", c.line), " "); got != c.want {
			t.Errorf("%q dòng %d: %q, muốn %q", c.editor, c.line, got, c.want)
		}
	}
}

// fakeStepAgent là agent giả: b-feat sửa app.txt, các bước khác không làm gì.
type fakeStepAgent struct{ root string }

func (a fakeStepAgent) Name() string                   { return "fake" }
func (a fakeStepAgent) Version(context.Context) string { return "" }
func (a fakeStepAgent) ReportsCost() bool              { return false }
func (a fakeStepAgent) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	if strings.Contains(req.Prompt, `step="b-feat"`) {
		os.WriteFile(filepath.Join(a.root, "a.go"), []byte("package a\n\nfunc A() { _ = 1 }\n"), 0o644)
	}
	return eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
}

// RunWorkflow qua dịch vụ: log, bước và kết quả đi qua callback; trong phiên agent Confirm/Ask không được cài; TaskRuns
// trả lượt cha + lượt con và tra được theo ID lượt chạy.
func TestRunWorkflowServiceCallbacksAndRuns(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	var logs []string
	var steps []workflow.StepEvent
	cb := RunCallbacks{Log: func(s string) { logs = append(logs, s) }, OnStep: func(e workflow.StepEvent) { steps = append(steps, e) },
		Agent:   func(workflow.StepSpec, *float64, *int) (eval.Agent, error) { return fakeStepAgent{root}, nil },
		Confirm: func(workflow.Question) bool { t.Error("không có gì cần hỏi"); return false }}
	o := RunWorkflowDefaults(ws.K.Config)
	o.Workflow, o.Task, o.NoReview = "b-do", id, true
	o.RunCallbacks = cb
	res, err := RunWorkflow(ctx, ws, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskID != id || res.Status != task.StatusBlocked { // lệnh kiểm chứng chưa được người xác nhận
		t.Fatalf("kết quả: %+v", res)
	}
	if len(steps) != 2 || steps[0].Step != "b-feat" || steps[0].Done || !steps[1].Done || steps[1].Status != "completed" {
		t.Fatalf("OnStep: %+v", steps)
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "▶ b-feat") || strings.HasSuffix(logs[0], "\n") {
		t.Fatalf("Log phải nhận từng dòng đã định dạng, không có xuống dòng cuối: %q", logs)
	}
	tr, err := TaskRuns(ctx, ws, TaskRunsOptions{Ref: id})
	if err != nil || tr.Task.State.ID != id || len(tr.Runs) != 2 {
		t.Fatalf("TaskRuns: %v %+v", err, tr)
	}
	byRun, err := TaskRuns(ctx, ws, TaskRunsOptions{Ref: res.RunID, NoIngest: true})
	if err != nil || byRun.Task.State.ID != id {
		t.Fatalf("tra theo ID lượt chạy: %v %+v", err, byRun)
	}
	if _, err := TaskRuns(ctx, ws, TaskRunsOptions{Ref: "khong-co"}); err == nil {
		t.Fatal("ID lạ phải lỗi")
	}

	// Không chạy lồng trong một bước workflow khác.
	ws.RunID = "01NESTED"
	if _, err := RunWorkflow(ctx, ws, o); err == nil || !strings.Contains(err.Error(), "lồng") {
		t.Fatalf("lồng: %v", err)
	}
	ws.RunID = ""

	// Trong phiên agent: Confirm/Ask bị bỏ (Runner không tương tác); autonomy auto bị chặn.
	asAgent(t)
	ws.Human = false
	o.Autonomy = "auto"
	if _, err := RunWorkflow(ctx, ws, o); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("agent đặt autonomy auto: %v", err)
	}
}
