package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"

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
