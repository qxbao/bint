package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/workflow"
)

// dirAgent: b-feat sửa a.go trong thư mục chạy thật của bước (worktree riêng của tác vụ, nếu có).
type dirAgent struct{}

func (dirAgent) Name() string                   { return "fake" }
func (dirAgent) Version(context.Context) string { return "" }
func (dirAgent) ReportsCost() bool              { return false }
func (dirAgent) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	if strings.Contains(req.Prompt, `step="b-feat"`) {
		var tid string
		for _, kv := range req.Env {
			if v, ok := strings.CutPrefix(kv, workflow.EnvTaskID+"="); ok {
				tid = v
			}
		}
		// Nội dung khác nhau theo tác vụ: tác vụ sau luôn có thay đổi so với base dù cây gốc đã nhận bản của tác vụ trước.
		os.WriteFile(filepath.Join(req.Dir, "a.go"), []byte("package a\n\nfunc A() { _ = 2 }\n// "+tid+"\n"), 0o644)
	}
	return eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
}

func runFeat(t *testing.T, ws *TaskWS, id string) *workflow.Result {
	t.Helper()
	o := RunWorkflowDefaults(ws.K.Config)
	o.Workflow, o.Task = workflow.StepFeat, id
	o.RunCallbacks = RunCallbacks{Agent: func(string, *float64, *int) (eval.Agent, error) { return dirAgent{}, nil }}
	res, err := RunWorkflow(context.Background(), ws, o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestWorktreeFlagsValidate(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"new --worktree", TaskNewOptions{Request: "x", Worktree: true}.Validate(), ""},
		{"new --shared", TaskNewOptions{Request: "x", Shared: true}.Validate(), ""},
		{"new cả hai", TaskNewOptions{Request: "x", Worktree: true, Shared: true}.Validate(), "loại trừ"},
		{"run --worktree", RunWorkflowOptions{Workflow: "b-do", Request: "x", Worktree: true}.Validate(), ""},
		{"run cả hai", RunWorkflowOptions{Workflow: "b-do", Request: "x", Worktree: true, Shared: true}.Validate(), "loại trừ"},
		{"run --worktree + --task", RunWorkflowOptions{Workflow: "b-do", Task: "T", Worktree: true}.Validate(), "--task"},
		{"run --shared + --task", RunWorkflowOptions{Workflow: "b-do", Task: "T", Shared: true}.Validate(), "--task"},
		{"land thiếu id", TaskLandOptions{}.Validate(), "cần ID"},
		{"cancel thiếu id", TaskCancelOptions{}.Validate(), "cần ID"},
	}
	for _, c := range cases {
		switch {
		case c.want == "" && c.err != nil:
			t.Errorf("%s: %v", c.name, c.err)
		case c.want != "":
			var ue UsageError
			if !errors.As(c.err, &ue) || !strings.Contains(c.err.Error(), c.want) {
				t.Errorf("%s: muốn UsageError chứa %q, được %v", c.name, c.want, c.err)
			}
		}
	}
	if isolationFlag(true, false) != task.IsolationWorktree || isolationFlag(false, true) != task.IsolationShared || isolationFlag(false, false) != "" {
		t.Fatal("isolationFlag")
	}
}

// Thao tác chỉ-người: land --3way và cancel --discard từ phiên agent bị từ chối (ErrAgentForbidden) và không đổi gì;
// người thì làm được. Kiểm thử đóng vai người gọi asHuman(t), vai agent gọi asAgent(t).
func TestLandAndCancelHumanOnlyOperations(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.Classification.Isolation, tk.State.Classification.IsolationReason = task.IsolationWorktree, task.IsolationReasonUser
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil { // người xác nhận lệnh kiểm chứng
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview || res.Worktree == "" || res.Origin != root {
		t.Fatalf("b-feat: %+v", res)
	}
	wt := ws.WorktreePath(id)

	asAgent(t)
	if _, err := LandTask(ctx, ws, TaskLandOptions{Ref: id, ThreeWay: true}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("land --3way từ agent: %v", err)
	}
	if _, err := CancelTask(ctx, ws, TaskCancelOptions{Ref: id, Discard: true}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("cancel --discard từ agent: %v", err)
	}
	// Agent huỷ không --discard khi còn thay đổi chưa đưa về: bị chặn bởi ErrDiscardRequired, không phải ErrAgentForbidden.
	var dr *workflow.ErrDiscardRequired
	if _, err := CancelTask(ctx, ws, TaskCancelOptions{Ref: id}); !errors.As(err, &dr) {
		t.Fatalf("cancel không --discard: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree không được đổi: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.go")); strings.Contains(string(got), "_ = 2") {
		t.Fatal("cây gốc bị đụng")
	}

	asHuman(t)
	// DiffTask trước khi đưa về: đọc worktree.
	d, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true})
	if err != nil || strings.TrimSpace(d.Text) != "a.go" {
		t.Fatalf("diff trước land: %+v %v", d, err)
	}
	res, err := LandTask(ctx, ws, TaskLandOptions{Ref: id})
	if err != nil || len(res.Files) != 1 || res.Files[0] != "a.go" || !res.Removed {
		t.Fatalf("land: %+v %v", res, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.go")); !strings.Contains(string(got), "_ = 2") {
		t.Fatal("cây gốc không nhận thay đổi")
	}
	// Sau khi đưa về (worktree đã gỡ): diff và show dùng bản lưu, không lỗi ErrWorktreeGone.
	d, err = DiffTask(ctx, ws, TaskDiffOptions{Ref: id})
	if err != nil || !strings.Contains(d.Text, "+func A() { _ = 2 }") || !strings.Contains(d.Note, "đã đưa về") {
		t.Fatalf("diff sau land: %+v %v", d, err)
	}
	if sd, err := ShowTask(ctx, ws, TaskShowOptions{Ref: id}); err != nil || len(sd.Blockers) != 0 {
		t.Fatalf("show sau land: %+v %v", sd.Blockers, err)
	}
	if tk, err := ws.Tasks.Load(id); err != nil || tk.State.Landed == nil || tk.State.Landed.By == "" {
		t.Fatalf("Landed: %+v %v", tk, err)
	}
	// Người --discard huỷ được tác vụ worktree khác còn thay đổi chưa đưa về.
	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.Worktree, o.NoPlan = "Sửa hàm A trong a.go lần nữa", true, true
	c, err := NewTask(ctx, ws, o)
	if err != nil || c.Task.State.Classification.Isolation != task.IsolationWorktree || c.Task.State.Classification.IsolationReason != task.IsolationReasonUser {
		t.Fatalf("task new --worktree: %+v %v", c.Task, err)
	}
	id2 := c.Task.State.ID
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id2}); err != nil {
		t.Fatal(err)
	}
	runFeat(t, ws, id2)
	if _, err := CancelTask(ctx, ws, TaskCancelOptions{Ref: id2}); !errors.As(err, &dr) {
		t.Fatalf("người cancel không --discard: %v", err)
	}
	tk, err := CancelTask(ctx, ws, TaskCancelOptions{Ref: id2, Discard: true, Reason: "bỏ"})
	if err != nil || tk.State.Status != task.StatusCancelled {
		t.Fatalf("cancel --discard: %+v %v", tk, err)
	}
	if _, err := os.Stat(ws.WorktreePath(id2)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree còn sau cancel: %v", err)
	}
	// Tác vụ shared: cancel như thường, không cần --discard.
	o.Worktree, o.Shared, o.Request = false, true, "Việc shared khác trong a.go"
	c, err = NewTask(ctx, ws, o)
	if err != nil {
		t.Fatal(err)
	}
	if tk, err := CancelTask(ctx, ws, TaskCancelOptions{Ref: c.Task.State.ID}); err != nil || tk.State.Status != task.StatusCancelled {
		t.Fatalf("cancel shared: %+v %v", tk, err)
	}
	// Doctor không báo gì sau khi đã dọn.
	orphans, err := ws.OrphanWorktrees(ctx)
	if err != nil || len(orphans) != 0 {
		t.Fatalf("worktree mồ côi sau khi dọn: %+v %v", orphans, err)
	}
}

// Doctor báo worktree mồ côi (thư mục không ứng với tác vụ chưa kết thúc) kèm lệnh dọn, ở mức cảnh báo.
func TestDoctorReportsOrphanWorktree(t *testing.T) {
	ws, root, _ := taskRepo(t)
	stray := filepath.Join(ws.K.Paths.Worktrees, "01STRAY")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	rep := Doctor(context.Background(), root)
	found := false
	for _, c := range rep.Checks {
		if c.Name == "worktree mồ côi" {
			found = true
			if c.Level != LevelWarn || !strings.Contains(c.Detail, stray) || !strings.Contains(c.Detail, "worktree remove --force") {
				t.Fatalf("%+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("doctor không báo worktree mồ côi: %+v", rep.Checks)
	}
	if rep.Failed() {
		t.Fatal("worktree mồ côi chỉ là cảnh báo")
	}
}
