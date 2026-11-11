package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

func countCalls(h *harness, step string) int {
	n := 0
	for _, c := range h.calls {
		if c == step {
			n++
		}
	}
	return n
}

func loadTask(t *testing.T, h *harness, id string) *task.Task {
	t.Helper()
	tk, err := h.tasks.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestPlanningContractVersionAtCreation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	env := h.runner().Env
	cases := []struct {
		name string
		o    NewTaskOptions
		want int
	}{
		{"b-do có plan", NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: "b-do"}, 1},
		{"b-do --no-plan", NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do", Plan: ptr(false)}, 0},
		{"b-req trực tiếp", NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-req"}, 1},
		{"b-feat trực tiếp", NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-feat"}, 0},
	}
	for _, c := range cases {
		tk, _, err := env.NewTask(ctx, c.o)
		if err != nil {
			t.Fatal(err)
		}
		if got := tk.State.PlanningContractVersion; got != c.want {
			t.Errorf("%s: version = %d, muốn %d", c.name, got, c.want)
		}
	}
}

func TestPlanningContractPlanOverrideOnCreated(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	env := h.runner().Env
	tk, _, _ := env.NewTask(ctx, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	if tk.State.PlanningContractVersion != 0 {
		t.Fatalf("version = %d", tk.State.PlanningContractVersion)
	}
	// b-do --task <created> --plan nâng lên 1 (b-do dừng ở bước b-req vì agent không ghi gì).
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Plan: ptr(true)})
	if v := loadTask(t, h, tk.State.ID).State.PlanningContractVersion; v != 1 {
		t.Fatalf("--plan trên created phải nâng lên 1, được %d", v)
	}
	// --no-plan trên task created hạ về 0.
	tk2, _, _ := env.NewTask(ctx, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do"})
	if tk2.State.PlanningContractVersion != 1 {
		t.Fatal("tk2 phải v1")
	}
	h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk2.State.ID, Plan: ptr(false)})
	if v := loadTask(t, h, tk2.State.ID).State.PlanningContractVersion; v != 0 {
		t.Fatalf("--no-plan trên created phải hạ về 0, được %d", v)
	}
}

// Agent bỏ riêng plan.md: dừng blocked (trước đó planned) trước b-feat; sửa rồi resume không chạy lại b-req.
func TestPlanningContractMissingArtifactBlocksThenResume(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.behave = func(c context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			return h.writePlan(e, id, map[string]bool{"plan": true})
		}
		return h.happy(c, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	tk := loadTask(t, h, res.TaskID)
	if res.Status != task.StatusBlocked || tk.State.ResumeFrom != task.StatusPlanned || strings.Join(h.calls, ",") != "b-req" {
		t.Fatalf("phải blocked trước b-feat: %+v %v resume=%s", res, h.calls, tk.State.ResumeFrom)
	}
	if len(res.PlanningIssues) != 1 || res.PlanningIssues[0].Code != "artifact_missing" || !strings.Contains(res.Reason, "task lint") {
		t.Fatalf("issues: %+v reason=%s", res.PlanningIssues, res.Reason)
	}
	// Người ghi lại plan.md rồi resume: đi thẳng b-feat, không b-req mới.
	if _, _, err := r.WriteArtifact(res.TaskID, "plan", "# Kế hoạch\nSửa app.txt\n"); err != nil {
		t.Fatal(err)
	}
	res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != task.StatusDone || countCalls(h, StepReq) != 1 || countCalls(h, StepFeat) != 1 {
		t.Fatalf("resume: %+v %v", res2, h.calls)
	}
}

func TestPlanningContractReportsAllErrors(t *testing.T) {
	h := newHarness(t)
	h.behave = func(c context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			if _, _, err := e.SetTicket(id, TicketEdit{Criteria: []string{"a", "b"}}); err != nil {
				return err
			}
			for n, body := range validDocs("a", "") {
				if n == "tasks" {
					body = "- [ ] TSK-1 [AC-1]: x\n"
				}
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	res, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng v2"})
	got := map[string]bool{}
	for _, i := range res.PlanningIssues {
		got[i.Code] = true
	}
	if !got["criteria_count_mismatch"] || !got["task_incomplete"] || len(res.PlanningIssues) < 2 || res.Status != task.StatusBlocked {
		t.Fatalf("phải nêu mọi mã lỗi: %+v", res)
	}
	for c := range got {
		if !strings.Contains(res.Reason, c) {
			t.Fatalf("reason thiếu %s: %s", c, res.Reason)
		}
	}
}

func TestPlanningContractPreflightCatchesLaterEdits(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepReq, Request: "Thêm tính năng v2"})
	if err != nil || res.Status != task.StatusPlanned {
		t.Fatalf("%+v %v", res, err)
	}
	// Sửa criterion trong task.yml sau khi plan xong.
	if _, _, err := r.SetTicket(res.TaskID, TicketEdit{ClearCriteria: true, Criteria: []string{"khác"}}); err != nil {
		t.Fatal(err)
	}
	res2, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: res.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != task.StatusBlocked || countCalls(h, StepFeat) != 0 || len(res2.PlanningIssues) == 0 || res2.PlanningIssues[0].Code != "criterion_mismatch" {
		t.Fatalf("preflight: %+v %v", res2, h.calls)
	}
	// Xóa artifact → cũng bị chặn (sau khi khôi phục criterion).
	r.SetTicket(res.TaskID, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}})
	tk := loadTask(t, h, res.TaskID)
	os.Remove(tk.ArtifactPath("tasks"))
	res3, _ := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: res.TaskID, Resume: true})
	if res3.Status != task.StatusBlocked || countCalls(h, StepFeat) != 0 {
		t.Fatalf("xóa tasks.md: %+v %v", res3, h.calls)
	}
}

func TestPlanningContractLegacyAndDirectFeatPass(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	// Task legacy: v0, đang planned, không có artifact → b-feat vẫn chạy.
	tk, _, _ := r.NewTask(ctx, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error { t.State.Status = task.StatusPlanned; return nil })
	res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusInReview || countCalls(h, StepFeat) != 1 {
		t.Fatalf("legacy: %+v %v %v", res, err, h.calls)
	}
	// Task v1 còn created gọi thẳng b-feat: không bị gate.
	tk2, _, _ := r.NewTask(ctx, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do"})
	if tk2.State.PlanningContractVersion != 1 || tk2.State.Status != task.StatusCreated {
		t.Fatalf("%+v", tk2.State)
	}
	res2, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk2.State.ID})
	if err != nil || res2.Status != task.StatusInReview {
		t.Fatalf("v1 created: %+v %v", res2, err)
	}
}

func TestPlanningContractManualPlannedGate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	h.behave = func(c context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			return h.writePlan(e, id, map[string]bool{"tasks": true})
		}
		return nil
	}
	res, _ := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng v2"})
	if res.Status != task.StatusBlocked {
		t.Fatalf("%+v", res)
	}
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	// blocked → planned thủ công trên task v1 lỗi bị từ chối.
	_, _, err := agent.SetStatus(ctx, res.TaskID, task.StatusPlanned, TransitionOptions{})

	if err == nil || !strings.Contains(err.Error(), "artifact_missing") {
		t.Fatalf("phải bị lint chặn: %v", err)
	}

	// Sửa xong thì chuyển được.
	r.WriteArtifact(res.TaskID, "tasks", validDocs("app.txt ghi v2", "")["tasks"])
	if _, _, err := agent.SetStatus(ctx, res.TaskID, task.StatusPlanned, TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
}

// b-req --task trên task legacy planned mà agent lỗi giữa chừng vẫn là v0; thành công thì lên v1.
func TestPlanningContractLegacyPlannedRerunUpgradesAfterAgent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	tk, _, _ := r.NewTask(ctx, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error { t.State.Status = task.StatusPlanned; return nil })
	h.behave = func(context.Context, string, *Env, string) error { return os.ErrPermission }
	res, _ := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: tk.State.ID})
	if res.Status != task.StatusFailed || loadTask(t, h, tk.State.ID).State.PlanningContractVersion != 0 {
		t.Fatalf("agent lỗi không được nâng version: %+v v=%d", res, loadTask(t, h, tk.State.ID).State.PlanningContractVersion)
	}
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error { t.State.Status = task.StatusPlanned; return nil })
	h.behave = h.happy
	res, _ = r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: tk.State.ID})
	if res.Status != task.StatusPlanned || loadTask(t, h, tk.State.ID).State.PlanningContractVersion != 1 {
		t.Fatalf("%+v", res)
	}
}

// --no-plan trên task v1 đã planned vẫn bị gate (không hạ version).
func TestPlanningContractNoPlanDoesNotDowngradePlanned(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	res, _ := r.Run(ctx, RunOptions{Workflow: StepReq, Request: "Thêm tính năng v2"})
	tk := loadTask(t, h, res.TaskID)
	os.Remove(tk.ArtifactPath("plan"))
	res2, _ := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Plan: ptr(false)})
	if res2.Status != task.StatusBlocked || countCalls(h, StepFeat) != 0 {
		t.Fatalf("%+v %v", res2, h.calls)
	}
}

// Workflow b-req chạy riêng: artifact lỗi → task ở lại planned, Result nêu lỗi.
func TestPlanningContractStandaloneReqStaysPlanned(t *testing.T) {
	h := newHarness(t)
	h.behave = func(c context.Context, step string, e *Env, id string) error {
		return h.writePlan(e, id, map[string]bool{"plan": true})
	}
	res, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Thêm tính năng v2"})
	if res.Status != task.StatusPlanned || len(res.PlanningIssues) == 0 || res.Reason == "" {
		t.Fatalf("%+v", res)
	}
}
