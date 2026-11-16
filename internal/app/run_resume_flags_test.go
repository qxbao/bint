package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --accept và --fix-round là quyết định của người dùng: phiên agent bị từ chối ở tầng dịch vụ (cả resume lẫn next --resume);
// người (asHuman) qua Validate. --answer thì agent điều phối cũng dùng được (chuyển câu trả lời của người).
func TestResumeAcceptAndFixRoundAreHumanOnly(t *testing.T) {
	asAgent(t)
	for _, o := range []RunResumeOptions{{Ref: "T", Accept: true}, {Ref: "T", FixRound: true}} {
		if err := o.Validate(); !errors.Is(err, ErrAgentForbidden) {
			t.Errorf("resume %+v trong phiên agent: %v", o, err)
		}
	}
	for _, o := range []RunNextOptions{{Ref: "T", Resume: true, Accept: true}, {Ref: "T", Resume: true, FixRound: true}} {
		if err := o.Validate(); !errors.Is(err, ErrAgentForbidden) {
			t.Errorf("next %+v trong phiên agent: %v", o, err)
		}
	}
	if err := (RunResumeOptions{Ref: "T", Answer: "30 ngày"}).Validate(); err != nil {
		t.Errorf("--answer phải dùng được trong phiên agent: %v", err)
	}
	if err := (RunNextOptions{Ref: "T", Resume: true, Answer: "30 ngày"}).Validate(); err != nil {
		t.Errorf("next --resume --answer: %v", err)
	}

	asHuman(t)
	for _, o := range []RunResumeOptions{{Ref: "T", Accept: true}, {Ref: "T", FixRound: true}} {
		if err := o.Validate(); err != nil {
			t.Errorf("người chạy %+v: %v", o, err)
		}
	}
	if err := (RunResumeOptions{Ref: "T", Accept: true, FixRound: true}).Validate(); err == nil {
		t.Error("--accept và --fix-round loại trừ nhau")
	}
	if err := (RunNextOptions{Ref: "T", Accept: true}).Validate(); err == nil {
		t.Error("next --accept không kèm --resume phải bị từ chối")
	}
	if err := (RunNextOptions{Ref: "T", Answer: "x"}).Validate(); err == nil {
		t.Error("next --answer không kèm --resume phải bị từ chối")
	}
}

// ResumeWorkflow chuyển cờ sang lõi: tác vụ chưa hết vòng sửa mà --accept thì lõi từ chối (không đổi gì).
func TestResumeWorkflowAcceptRejectedWhenNotExhausted(t *testing.T) {
	ws, _, _ := taskRepo(t)
	ctx := context.Background()
	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.NoPlan = "Sửa hàm A trong a.go", true
	c, err := NewTask(ctx, ws, o)
	tk := c.Task
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResumeWorkflow(ctx, ws, RunResumeOptions{Ref: tk.State.ID, Accept: true})
	if err == nil || !strings.Contains(err.Error(), "--accept/--fix-round") {
		t.Fatalf("muốn lỗi nêu --accept/--fix-round, được %v", err)
	}
}

// bint task error --question ghi câu hỏi (không chỉ lỗi); chỉ nội dung hoặc chỉ --question đều được.
func TestRecordTaskErrorQuestion(t *testing.T) {
	ws, _, _ := taskRepo(t)
	ctx := context.Background()
	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.NoPlan = "Sửa hàm A trong a.go", true
	c, err := NewTask(ctx, ws, o)
	tk := c.Task
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	if err := (TaskErrorOptions{Ref: id}).Validate(); err == nil {
		t.Fatal("thiếu cả nội dung lẫn --question phải là lỗi dùng")
	}
	if _, err := RecordTaskError(ctx, ws, TaskErrorOptions{Ref: id, Step: "b-feat", Question: "Giữ bao nhiêu ngày?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordTaskError(ctx, ws, TaskErrorOptions{Ref: id, Step: "b-feat", Message: "đã thử WAL, vẫn hỏng"}); err != nil {
		t.Fatal(err)
	}
	got, err := ws.Tasks.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	qs := got.State.OpenQuestions("b-feat")
	if len(qs) != 1 || qs[0].Text != "Giữ bao nhiêu ngày?" || len(got.State.Errors) != 1 || got.State.Errors[0].Message != "đã thử WAL, vẫn hỏng" {
		t.Fatalf("câu hỏi/lỗi: %+v / %+v", got.State.Questions, got.State.Errors)
	}
}
