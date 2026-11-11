package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

func newTaskWith(t *testing.T, h *harness, o NewTaskOptions) (*task.Task, task.Assessment) {
	t.Helper()
	tk, a, err := h.runner().Env.NewTask(context.Background(), o)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	return tk, a
}

func longRequest(n int) string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, "Mô tả ý số "+strings.Repeat("x", i+1))
	}
	return strings.Join(out, "\n")
}

// Review focus 1: "đổi tên" nhưng chạm migrations/**: auto chọn Plan=true, deep; --no-plan vẫn được tôn trọng.
func TestDepthSelectionProtectedPathsPromotesPlan(t *testing.T) {
	h := newHarness(t)
	req := "rename cột trong migrations/001.sql"
	tk, a := newTaskWith(t, h, NewTaskOptions{Request: req, Workflow: "b-do"})
	c := tk.State.Classification
	if a.Risk != "high" || !c.Plan || c.Depth != task.DepthDeep || c.DepthReason != task.DepthReasonProtected || tk.State.PlanningContractVersion != 1 {
		t.Fatalf("auto: %+v %+v v=%d", a, c, tk.State.PlanningContractVersion)
	}
	tk, _ = newTaskWith(t, h, NewTaskOptions{Request: req, Workflow: "b-do", Plan: ptr(false)})
	c = tk.State.Classification
	if c.Plan || c.Depth != "" || c.DepthReason != "" || tk.State.PlanningContractVersion != 0 {
		t.Fatalf("--no-plan phải được tôn trọng: %+v", c)
	}
}

// Orchestrator (agent) được tự quyết b-req, trừ việc bỏ b-req khi chạm đường dẫn bảo vệ.
func TestAgentNoPlanOnProtectedKeepsPlan(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	r.Human = false
	req := "rename cột trong migrations/001.sql"
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: req, Workflow: "b-do", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	c := tk.State.Classification
	if !c.Plan || c.Depth != task.DepthDeep || !strings.Contains(c.Reason, "agent không bỏ được b-req") || tk.State.PlanningContractVersion != 1 {
		t.Fatalf("agent --no-plan trên đường dẫn bảo vệ: %+v v=%d", c, tk.State.PlanningContractVersion)
	}
	// Không chạm đường dẫn bảo vệ: quyết định của orchestrator được tôn trọng, ghi rõ ai chỉ định.
	tk, _, _ = r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do", Plan: ptr(false)})
	if c := tk.State.Classification; c.Plan || c.Reason != "orchestrator chỉ định" {
		t.Fatalf("%+v", c)
	}
	// Agent hạ qua `bint run --task … --no-plan` trên tác vụ created cũng bị giữ b-req.
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	tk, _ = newTaskWith(t, h, NewTaskOptions{Request: req, Workflow: "b-do", Edit: TicketEdit{VerifyCommand: ptr("true")}})
	r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Plan: ptr(false)})
	if c := loadTask(t, h, tk.State.ID).State.Classification; !c.Plan {
		t.Fatalf("run --no-plan của agent: %+v", c)
	}
	if countCalls(h, StepReq) != 1 {
		t.Fatalf("b-req phải chạy: %v", h.calls)
	}
}

// Mô tả ngắn trỏ tới một tài liệu kế hoạch: cần b-req, tài liệu không vào phạm vi sửa.
func TestNewTaskReferencedPlanDoc(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Join(h.root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, "docs", "tui-plan.md"), []byte("# Kế hoạch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk, a := newTaskWith(t, h, NewTaskOptions{Request: "Thực hiện plan @docs/tui-plan.md", Workflow: "b-do"})
	if c := tk.State.Classification; !c.Plan || !strings.Contains(c.Reason, "docs/tui-plan.md") {
		t.Fatalf("%+v", c)
	}
	if len(tk.Ticket.Scope.Allow) != 0 || len(tk.Ticket.References) != 1 || tk.Ticket.References[0] != "docs/tui-plan.md" {
		t.Fatalf("scope %v refs %v", tk.Ticket.Scope.Allow, tk.Ticket.References)
	}
	if !strings.Contains(strings.Join(a.Notes, "\n"), "tài liệu đầu vào docs/tui-plan.md") {
		t.Fatalf("notes: %v", a.Notes)
	}
	// --clear-scope bỏ phạm vi tự đoán, chỉ giữ --allow.
	tk, _ = newTaskWith(t, h, NewTaskOptions{Request: "Sửa lỗi trong app.txt", Workflow: "b-do",
		Edit: TicketEdit{ClearScope: true, Allow: []string{"internal/**"}}})
	if got := tk.Ticket.Scope.Allow; len(got) != 1 || got[0] != "internal/**" {
		t.Fatalf("clear-scope: %v", got)
	}
}

// Review focus 2: mô tả dài nhưng không chạm đường dẫn bảo vệ → deep/complex_request, Risk vẫn low.
func TestDepthSelectionLongRequestDoesNotRaiseRisk(t *testing.T) {
	h := newHarness(t)
	tk, a := newTaskWith(t, h, NewTaskOptions{Request: longRequest(6), Workflow: "b-do"})
	c := tk.State.Classification
	if a.Risk != "low" || c.Depth != task.DepthDeep || c.DepthReason != task.DepthReasonComplex || !c.Plan {
		t.Fatalf("%+v %+v", a, c)
	}
	// 3 dòng (Plan=true) vẫn standard/default.
	tk, _ = newTaskWith(t, h, NewTaskOptions{Request: longRequest(3), Workflow: "b-do"})
	if c := tk.State.Classification; !c.Plan || c.Depth != task.DepthStandard || c.DepthReason != task.DepthReasonDefault {
		t.Fatalf("%+v", c)
	}
}

func TestDepthSelectionPlanAloneDoesNotForceDeep(t *testing.T) {
	h := newHarness(t)
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(true)})
	if c := tk.State.Classification; !c.Plan || c.Depth != task.DepthStandard {
		t.Fatalf("%+v", c)
	}
}

func TestDepthSelectionExplicitDepth(t *testing.T) {
	h := newHarness(t)
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Depth: "deep"})
	c := tk.State.Classification
	if !c.Plan || c.Depth != task.DepthDeep || c.DepthReason != task.DepthReasonUser || tk.State.PlanningContractVersion != 1 {
		t.Fatalf("%+v", c)
	}
	// auto = tự chọn, không ép Plan.
	tk, _ = newTaskWith(t, h, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Depth: "auto"})
	if c := tk.State.Classification; c.Plan || c.Depth != "" {
		t.Fatalf("--depth auto không đổi nghĩa: %+v", c)
	}
	env := h.runner().Env
	for name, o := range map[string]NewTaskOptions{
		"no-plan + deep":  {Request: "x", Workflow: "b-do", Depth: "deep", Plan: ptr(false)},
		"b-feat + deep":   {Request: "x", Workflow: "b-feat", Depth: "deep"},
		"b-review + deep": {Request: "x", Workflow: "b-review", Depth: "standard"},
		"giá trị sai":     {Request: "x", Workflow: "b-do", Depth: "shallow"},
	} {
		n := len(mustList(h))
		if _, _, err := env.NewTask(context.Background(), o); err == nil {
			t.Errorf("%s: phải lỗi", name)
		}
		if len(mustList(h)) != n {
			t.Errorf("%s: không được tạo tác vụ khi lỗi", name)
		}
	}
}

func mustList(h *harness) []*task.Task {
	ts, _ := h.tasks.List()
	return ts
}

// Review focus 7: smallWords ≤25 từ nhưng ≥3 dòng → Plan=false, không lưu depth.
func TestDepthSelectionNoDepthWhenPlanFalse(t *testing.T) {
	h := newHarness(t)
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "typo\nfix\nlint", Workflow: "b-do"})
	c := tk.State.Classification
	if c.Plan || c.Depth != "" || c.DepthReason != "" || c.EffectiveDepth() != task.DepthStandard {
		t.Fatalf("%+v", c)
	}
}

// Review focus 8: b-do --task <created, Plan=false> --depth deep → Plan=true, version=1, deep.
func TestDepthSelectionExplicitOnCreatedTask(t *testing.T) {
	h := newHarness(t)
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	if tk.State.PlanningContractVersion != 0 {
		t.Fatal("tiền điều kiện")
	}
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Depth: "deep"}); err != nil {
		t.Fatal(err)
	}
	got := loadTask(t, h, tk.State.ID)
	c := got.State.Classification
	if !c.Plan || c.Depth != task.DepthDeep || got.State.PlanningContractVersion != 1 {
		t.Fatalf("%+v v=%d", c, got.State.PlanningContractVersion)
	}
	if countCalls(h, StepReq) != 1 {
		t.Fatalf("Plan thành true thì b-req phải chạy: %v", h.calls)
	}
}

func TestDepthSelectionPlanFlagOnCreatedChoosesAuto(t *testing.T) {
	h := newHarness(t)
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Plan: ptr(true)})
	if c := loadTask(t, h, tk.State.ID).State.Classification; !c.Plan || c.Depth != task.DepthStandard || c.DepthReason != task.DepthReasonDefault {
		t.Fatalf("--plan một mình không ép deep: %+v", c)
	}
}

func TestDepthOverrideRestrictedByStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	set := func(id, status string) {
		r.Tasks.Update(id, time.Now(), func(t *task.Task) error { t.State.Status = status; return nil })
	}
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do"})
	// planned + b-do: bị từ chối, không đổi depth.
	set(tk.State.ID, task.StatusPlanned)
	_, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Depth: "deep"})
	if err == nil || !strings.Contains(err.Error(), "b-req --task") {
		t.Fatalf("planned + b-do --depth phải lỗi có hướng dẫn: %v", err)
	}
	if loadTask(t, h, tk.State.ID).State.Classification.Depth != task.DepthStandard {
		t.Fatal("depth không được đổi khi bị từ chối")
	}
	// in_progress: từ chối với cả b-req (b-req chỉ chạy created/planned) và b-do.
	set(tk.State.ID, task.StatusInProgress)
	if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Depth: "deep"}); err == nil {
		t.Fatal("in_progress phải từ chối")
	}
	// b-req riêng trên planned: được, đổi sang deep.
	set(tk.State.ID, task.StatusPlanned)
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: tk.State.ID, Depth: "deep"})
	if err != nil {
		t.Fatal(err)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Depth != task.DepthDeep || c.DepthReason != task.DepthReasonUser || res.Status != task.StatusPlanned {
		t.Fatalf("%+v %+v", c, res)
	}
	// --no-plan + depth, hay b-feat + depth: lỗi.
	if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Depth: "deep", Plan: ptr(false)}); err == nil {
		t.Fatal("--no-plan + depth phải lỗi")
	}
	if _, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID, Depth: "deep"}); err == nil {
		t.Fatal("b-feat + depth phải lỗi")
	}
}

// Review focus 4: task cũ không có depth, đã planned: resume theo standard, không phát sinh b-req mới.
func TestDepthLegacyTaskResumesAsStandard(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do", Plan: ptr(false)})
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
		t.State.Status = task.StatusBlocked
		t.State.ResumeFrom = task.StatusPlanned
		t.State.Classification = &task.Classification{Plan: true, Reason: "state cũ"}
		return nil
	})
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Resume: true})
	if err != nil || res.Status != task.StatusDone || countCalls(h, StepReq) != 0 {
		t.Fatalf("%+v %v %v", res, err, h.calls)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Depth != "" || c.EffectiveDepth() != task.DepthStandard {
		t.Fatalf("resume không được tính lại depth: %+v", c)
	}
}

// Depth chọn một lần: resume không tính lại theo mô tả đã đổi.
func TestDepthNotRecomputedOnResume(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	tk, _ := newTaskWith(t, h, NewTaskOptions{Request: longRequest(6), Workflow: "b-do"})
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
		t.State.Status = task.StatusBlocked
		t.State.ResumeFrom = task.StatusCreated
		t.Ticket.Request = "ngắn"
		return nil
	})
	if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Depth != task.DepthDeep || c.DepthReason != task.DepthReasonComplex {
		t.Fatalf("%+v", c)
	}
}

func TestDepthPromptForReq(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "rename cột trong migrations/001.sql", Edit: TicketEdit{VerifyCommand: ptr("true")}})
	if err != nil {
		t.Fatal(err)
	}
	p := h.prompts[StepReq]
	if !strings.Contains(p, "Phân tích: deep") || !strings.Contains(p, "Lý do: "+task.DepthReasonProtected) {
		t.Fatalf("prompt b-req thiếu depth/lý do:\n%s", p)
	}
	if strings.Contains(h.prompts[StepFeat], "Phân tích:") {
		t.Fatal("prompt b-feat không được thêm bước phân tích")
	}
	_ = res
	// State cũ thiếu field: vẫn có dòng Phân tích: standard.
	h2 := newHarness(t)
	r := h2.runner()
	tk, _ := newTaskWith(t, h2, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-req"})
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
		t.State.Classification = &task.Classification{Plan: true, Reason: "cũ"}
		return nil
	})
	if _, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	if p := h2.prompts[StepReq]; !strings.Contains(p, "Phân tích: standard") || !strings.Contains(p, "Lý do:") {
		t.Fatalf("state cũ:\n%s", p)
	}
}
