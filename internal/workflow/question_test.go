package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

// Câu hỏi duyệt phạm vi mang khoá i18n và danh sách tệp; trả "không" thì không có ProtectedApproval mới
// và tác vụ blocked chờ người duyệt; trả "có" thì ghi duyệt và chạy tiếp.
func TestScopeQuestionKeyAndAnswers(t *testing.T) {
	for _, yes := range []bool{false, true} {
		h := newHarness(t)
		base := h.happy
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepFeat {
				h.write("migrations/002.sql", "ALTER TABLE x;\n")
			}
			return base(ctx, step, e, id)
		}
		var asked []Question
		r := h.runner()
		r.Confirm = func(q Question) bool { asked = append(asked, q); return yes }
		res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi giá trị trong app.txt", Plan: ptr(false)})
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || asked[0].Key != QConfirmScope || !slices.Equal(asked[0].Paths, []string{"migrations/002.sql"}) ||
			!strings.Contains(asked[0].Text, "migrations/002.sql") {
			t.Fatalf("câu hỏi: %+v", asked)
		}
		tk, _ := h.tasks.Load(res.TaskID)
		if yes {
			if tk.State.ProtectedApproval == nil || len(tk.State.PendingPaths) != 0 || res.Status != task.StatusDone {
				t.Fatalf("trả có: %+v %+v", res, tk.State)
			}
			continue
		}
		// Trả không: không ghi duyệt; chỉ ghi PendingPaths và chuyển blocked (hành vi có sẵn của CLI).
		if tk.State.ProtectedApproval != nil || tk.State.VerifyApproval != nil || res.Status != task.StatusBlocked ||
			!slices.Equal(tk.State.PendingPaths, []string{"migrations/002.sql"}) {
			t.Fatalf("trả không: %+v %+v", res, tk.State)
		}
	}
}

// autonomy ask_always hỏi trước b-feat với khoá workflow.confirm.feat và tham số task/root.
func TestAskAlwaysQuestionKey(t *testing.T) {
	h := newHarness(t)
	var asked []Question
	r := h.runner()
	r.Confirm = func(q Question) bool { asked = append(asked, q); return false }
	always := task.AutonomyAskAlways
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false), Edit: TicketEdit{Autonomy: &always}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || len(asked) != 1 || asked[0].Key != QConfirmFeat ||
		asked[0].Params["task"] != res.TaskID || asked[0].Params["root"] != h.root || !strings.Contains(asked[0].Text, "ask_always") {
		t.Fatalf("%+v %+v", res, asked)
	}
	if len(h.calls) != 0 {
		t.Fatalf("trả không thì b-feat không được chạy: %v", h.calls)
	}
}

// Tác vụ rủi ro cao chưa có lệnh kiểm chứng: Ask nhận câu hỏi có khoá; câu trả lời thành lệnh đã xác nhận.
func TestAskVerifyQuestionKey(t *testing.T) {
	h := newHarness(t)
	h.cfg.Eval.TestCommand = nil
	var asked []Question
	r := h.runner()
	r.Ask = func(q Question) string { asked = append(asked, q); return "test -f done.txt" }
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Sửa migrations/001.sql", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Key != QAskVerifyCommand || asked[0].Params["question"] == "" ||
		!strings.Contains(asked[0].Text, "bỏ trống") {
		t.Fatalf("câu hỏi: %+v (kết quả %+v)", asked, res)
	}
	tk, _ := h.tasks.Load(res.TaskID)
	if tk.Ticket.Verify.Command != "test -f done.txt" || tk.State.VerifyApproval == nil {
		t.Fatalf("lệnh người gõ phải thành đã xác nhận: %+v", tk.Ticket.Verify)
	}
}

// OnStep báo bắt đầu rồi kết thúc từng bước kèm đường dẫn log của bước (cùng đường dẫn agent nhận).
func TestOnStepReportsLogPath(t *testing.T) {
	h := newHarness(t)
	var evs []StepEvent
	r := h.runner()
	r.OnStep = func(e StepEvent) { evs = append(evs, e) }
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Đổi app.txt", Plan: ptr(false), NoReview: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	if len(evs) != 2 || evs[0].Done || !evs[1].Done || evs[0].Step != StepFeat || evs[1].Status != "completed" ||
		evs[0].Log != evs[1].Log || evs[0].RunID != evs[1].RunID || evs[0].TaskID != res.TaskID {
		t.Fatalf("sự kiện bước: %+v", evs)
	}
	if !strings.Contains(evs[0].Log, evs[0].RunID) || !strings.HasSuffix(evs[0].Log, ".log") {
		t.Fatalf("đường dẫn log: %s", evs[0].Log)
	}
}

// Phát hiện .2 (tác vụ 01M774RD…): tệp cần duyệt thuộc Touched của tác vụ KHÁC cùng worktree thì câu hỏi QConfirmScope
// (Text) và lý do blocked nêu "thuộc tác vụ <id>" kèm trạng thái, thay vì khẳng định b-feat đã sửa.
func TestScopeQuestionNamesOwningTask(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ name, final, want string }{
		{"đang chạy", "", "(đang chạy)"},
		{"đã kết thúc", task.StatusCancelled, "(đã kết thúc)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
			if a.Status != task.StatusInReview {
				t.Fatalf("A: %+v", a)
			}
			// B (blocked: chưa kết thúc) sửa một tệp bảo vệ; A không sửa tệp đó.
			happy := h.behave
			h.behaveFeat(func() { h.write("migrations/002.sql", "-- của B\n") })
			b := h.runFeat("Việc khác trên migrations", "")
			if b.Status != task.StatusBlocked {
				t.Fatalf("B: %+v", b)
			}
			h.behave = happy
			if tc.final != "" {
				if _, _, err := h.runner().SetStatus(ctx, b.TaskID, tc.final, TransitionOptions{Reason: "kết thúc B"}); err != nil {
					t.Fatalf("B → %s: %v", tc.final, err)
				}
			}
			var asked []Question
			r := h.runner()
			r.Confirm = func(q Question) bool { asked = append(asked, q); return false }
			res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: a.TaskID, Resume: true})
			if err != nil {
				t.Fatal(err)
			}
			note := "migrations/002.sql thuộc tác vụ " + b.TaskID + " " + tc.want
			if len(asked) != 1 || asked[0].Key != QConfirmScope || !strings.Contains(asked[0].Text, note) ||
				strings.Contains(asked[0].Text, "b-feat đã sửa") {
				t.Fatalf("câu hỏi thiếu ghi chú %q: %+v", note, asked)
			}
			if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, note) {
				t.Fatalf("lý do blocked thiếu ghi chú %q: %+v", note, res)
			}
		})
	}
}

// Review .3 (tác vụ 01M7826Q…): tác vụ đã kết thúc TRƯỚC khi A được lập không được nêu là chủ sở hữu (thay đổi của nó
// đã nằm trong BaseTree của A); câu hỏi vẫn được hỏi cho tệp đó.
func TestScopeQuestionSkipsOwnerEndedBeforeTask(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	a := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if a.Status != task.StatusInReview {
		t.Fatalf("A: %+v", a)
	}
	happy := h.behave
	h.behaveFeat(func() { h.write("migrations/002.sql", "-- của B\n") })
	b := h.runFeat("Việc khác trên migrations", "")
	if b.Status != task.StatusBlocked {
		t.Fatalf("B: %+v", b)
	}
	h.behave = happy
	if _, _, err := h.runner().SetStatus(ctx, b.TaskID, task.StatusCancelled, TransitionOptions{Reason: "kết thúc B"}); err != nil {
		t.Fatal(err)
	}
	// Dời thời điểm lập A ra sau lúc B kết thúc.
	later := time.Now().Add(time.Hour)
	if _, err := h.tasks.Update(a.TaskID, later, func(tk *task.Task) error { tk.State.CreatedAt = task.Now(later); return nil }); err != nil {
		t.Fatal(err)
	}
	var asked []Question
	r := h.runner()
	r.Confirm = func(q Question) bool { asked = append(asked, q); return false }
	if _, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: a.TaskID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0].Text, "migrations/002.sql") || strings.Contains(asked[0].Text, "thuộc tác vụ") {
		t.Fatalf("tác vụ kết thúc trước A không được nêu: %+v", asked)
	}
}

// Tệp do chính tác vụ sửa không có ghi chú "thuộc tác vụ" (kể cả khi tác vụ khác tồn tại).
func TestScopeQuestionOwnFileHasNoOwnerNote(t *testing.T) {
	h := newHarness(t)
	h.behaveFeat(func() { h.write("migrations/003.sql", "ALTER TABLE y;\n") })
	var asked []Question
	r := h.runner()
	r.Confirm = func(q Question) bool { asked = append(asked, q); return false }
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, Request: "Sửa migrations"}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Key != QConfirmScope || !strings.Contains(asked[0].Text, "migrations/003.sql") ||
		strings.Contains(asked[0].Text, "thuộc tác vụ") {
		t.Fatalf("tệp của chính tác vụ không được ghi chú chủ sở hữu: %+v", asked)
	}
}
