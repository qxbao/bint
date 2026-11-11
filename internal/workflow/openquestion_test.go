package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

const openQ = "- [ ] Q-1: dùng bảng nào?\n"
const answeredQ = "- [x] Q-1: dùng bảng nào? — Trả lời: bảng users\n"

// reqWithQuestion: agent b-req ghi bộ artifact hợp lệ kèm câu hỏi (nếu extra != "").
func (h *harness) reqWithQuestion(extra string) func(context.Context, string, *Env, string) error {
	return func(c context.Context, step string, e *Env, id string) error {
		if step != StepReq {
			return h.happy(c, step, e, id)
		}
		if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}}); err != nil {
			return err
		}
		for n, body := range validDocs("app.txt ghi v2", extra) {
			if _, _, err := e.WriteArtifact(id, n, body); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestOpenQuestionBlocksFeatThenResumeSkipsReq(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.behave = h.reqWithQuestion(openQ)
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	tk := loadTask(t, h, res.TaskID)
	if res.Status != task.StatusBlocked || tk.State.ResumeFrom != task.StatusPlanned || strings.Join(h.calls, ",") != "b-req" {
		t.Fatalf("phải blocked trước b-feat: %+v %v resume=%s", res, h.calls, tk.State.ResumeFrom)
	}
	if len(res.Questions) != 1 || !strings.Contains(res.Questions[0], "Q-1") || !strings.Contains(res.Reason, "requirement.md") ||
		!strings.Contains(res.Reason, "task lint "+res.TaskID) {
		t.Fatalf("%+v", res)
	}
	// Chưa trả lời mà resume: vẫn bị chặn trước khi gọi agent b-feat.
	res1, _ := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if res1.Status != task.StatusBlocked || countCalls(h, StepFeat) != 0 || countCalls(h, StepReq) != 1 {
		t.Fatalf("resume khi chưa trả lời: %+v %v", res1, h.calls)
	}
	// Ghi câu trả lời, resume: b-feat → review → done; b-req không chạy lại.
	if _, _, err := r.WriteArtifact(res.TaskID, "requirement", validDocs("app.txt ghi v2", answeredQ)["requirement"]); err != nil {
		t.Fatal(err)
	}
	res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Status != task.StatusDone || countCalls(h, StepReq) != 1 || countCalls(h, StepFeat) != 1 || countCalls(h, StepReview) != 1 {
		t.Fatalf("sau trả lời: %+v %v", res2, h.calls)
	}
}

func TestOpenQuestionCheckedWithEmptyAnswerStillBlocks(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reqWithQuestion("- [x] Q-1: dùng bảng nào? — Trả lời:   \n")
	res, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng v2"})
	if res.Status != task.StatusBlocked || len(res.Questions) != 1 || countCalls(h, StepFeat) != 0 {
		t.Fatalf("%+v %v", res, h.calls)
	}
}

// Workflow b-req chạy riêng: task ở lại planned (resume sẽ gọi lại agent b-req), Questions có Q.
func TestOpenQuestionStandaloneReqStaysPlanned(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reqWithQuestion(openQ)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Thêm tính năng v2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusPlanned || len(res.Questions) != 1 || res.Reason == "" {
		t.Fatalf("%+v", res)
	}
	// b-feat chạy riêng sau đó vẫn bị chặn bởi preflight.
	res2, _ := h.runner().Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: res.TaskID})
	if res2.Status != task.StatusBlocked || countCalls(h, StepFeat) != 0 || len(res2.Questions) != 1 {
		t.Fatalf("%+v %v", res2, h.calls)
	}
}

func TestOpenQuestionNotAppliedToLegacyOrNoPlan(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	// Task legacy (v0) đang planned, requirement có Q mở: b-feat vẫn chạy.
	tk, _, _ := r.NewTask(ctx, NewTaskOptions{Request: "Đổi app.txt", Workflow: "b-do", Plan: ptr(false)})
	tk.Dir = loadTask(t, h, tk.State.ID).Dir
	if _, _, err := r.WriteArtifact(tk.State.ID, "requirement", "# Yêu cầu\n## Câu hỏi mở\n"+openQ); err != nil {
		t.Fatal(err)
	}
	r.Tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error { t.State.Status = task.StatusPlanned; return nil })
	res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusInReview || countCalls(h, StepFeat) != 1 {
		t.Fatalf("legacy: %+v %v %v", res, err, h.calls)
	}
}

func TestOpenQuestionManualStatusGate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.behave = h.reqWithQuestion(openQ)
	r := h.runner()
	res, _ := r.Run(ctx, RunOptions{Workflow: StepReq, Request: "Thêm tính năng v2"})
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	// created→planned không bị chặn bởi Q (đã planned sau b-req); planned→in_progress thì bị chặn.
	_, _, err := agent.SetStatus(ctx, res.TaskID, task.StatusInProgress, TransitionOptions{})
	if err == nil || !strings.Contains(err.Error(), "Q-1") {
		t.Fatalf("planned→in_progress với Q mở phải lỗi: %v", err)
	}
	r.WriteArtifact(res.TaskID, "requirement", validDocs("app.txt ghi v2", answeredQ)["requirement"])
	if _, _, err := agent.SetStatus(ctx, res.TaskID, task.StatusInProgress, TransitionOptions{}); err != nil {
		t.Fatalf("đã trả lời thì được chuyển: %v", err)
	}
	// created→planned thủ công không bị Q chặn.
	tk2, _, _ := r.NewTask(ctx, NewTaskOptions{Request: "Thêm tính năng v2", Workflow: "b-do"})
	r.Tasks.Update(tk2.State.ID, time.Now(), func(t *task.Task) error { t.Ticket.Verify.Criteria = []string{"app.txt ghi v2"}; return nil })
	for n, body := range validDocs("app.txt ghi v2", openQ) {
		r.WriteArtifact(tk2.State.ID, n, body)
	}
	if _, _, err := agent.SetStatus(ctx, tk2.State.ID, task.StatusPlanned, TransitionOptions{}); err != nil {
		t.Fatalf("created→planned không được chặn bởi Q mở: %v", err)
	}
}
