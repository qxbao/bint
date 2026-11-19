package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/task"
)

// Đường nhanh của b-do (GĐ1 nhóm 1): yêu cầu ngắn mơ hồ rủi ro thấp bỏ b-req, b-feat một phiên, cổng kiểm chứng, đúng một
// vòng b-review; còn phát hiện nghiêm trọng thì một vòng sửa rồi finalize không review lại.

const fastReq = "Làm cho nó gọn gàng hơn một chút"

func TestFastPathProcessCleanReviewIsDoneAfterOneReview(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reviewCounts(0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review" {
		t.Fatalf("phải b-feat → b-review → done: %+v %v calls=%v", res, err, h.calls)
	}
	cls := loadTask(t, h, res.TaskID).State.Classification
	if cls == nil || !cls.Fast || cls.Plan || !strings.Contains(cls.Reason, "đường nhanh") {
		t.Fatalf("Classification phải ghi đường nhanh: %+v", cls)
	}
	if !strings.Contains(h.prompts[StepFeat], "Đường nhanh (không có b-req)") {
		t.Fatalf("đề bài b-feat thiếu khối đường nhanh:\n%s", h.prompts[StepFeat])
	}
}

func TestFastPathProcessOneFixRoundThenFinalizeWithoutReReview(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reviewCounts(2, 0) // nếu review lại, vòng 2 sẽ sạch: không được chạy
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat" {
		t.Fatalf("phải b-feat → b-review → b-feat (sửa) → done, không review lại: %+v %v calls=%v", res, err, h.calls)
	}
	notes := strings.Join(res.Notes, "\n")
	for _, want := range []string{"đường nhanh", "đã qua một vòng sửa nhưng chưa được review lại", loadTask(t, h, res.TaskID).State.ID + ".1"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes thiếu %q:\n%s", want, notes)
		}
	}
	if !loadTask(t, h, res.TaskID).State.FastFixed {
		t.Fatal("FastFixed phải bật")
	}
}

// Sau vòng sửa kiểm chứng đỏ: tác vụ không thành done và không review lại (cổng done chặn ở finalize).
func TestFastPathFixRoundVerifyRedBlocksAtFinalize(t *testing.T) {
	h := newHarness(t)
	feats := 0
	rc := h.reviewCounts(1, 0)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			feats++
			if feats == 2 { // vòng sửa làm hỏng cổng kiểm chứng (test -f done.txt)
				h.write("app.txt", "v3\n")
				return os.Remove(filepath.Join(h.root, "done.txt"))
			}
		}
		return rc(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status == task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat" {
		t.Fatalf("kiểm chứng đỏ sau vòng sửa phải chặn done, không review lại: %+v %v calls=%v", res, err, h.calls)
	}
}

func TestFastPathSessionDriver(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts []int
		steps  string
	}{
		{"sạch", []int{0}, "b-feat,b-review"},
		{"một vòng sửa", []int{2, 0}, "b-feat,b-review,b-feat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.behave = h.reviewCounts(tc.counts...)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{Request: fastReq, Fast: true})
			if c := tk.State.Classification; c == nil || !c.Fast {
				t.Fatalf("Classification: %+v", c)
			}
			steps, last := h.drive(r, tk.State.ID, false)
			if strings.Join(steps, ",") != tc.steps || last.Action != ActionDone {
				t.Fatalf("bước %v, dừng %+v", steps, last)
			}
		})
	}
}

// Scope sửa sau lúc lập phiếu (bint run --task … --allow) chạm đường dẫn bảo vệ: bỏ đường nhanh, buộc b-req.
func TestFastPathDroppedWhenScopeEditedToProtected(t *testing.T) {
	h := newHarness(t)
	h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
	h.behave = h.reviewCounts(0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true})
	if err != nil || !tk.State.Classification.Fast {
		t.Fatalf("phải lập đường nhanh: %v %+v", err, tk.State.Classification)
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Edit: TicketEdit{Allow: []string{"migrations/**"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) == 0 || h.calls[0] != StepReq {
		t.Fatalf("scope chạm đường dẫn bảo vệ phải chạy b-req trước: %v (%s)", h.calls, res.Reason)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Fast || !c.Plan {
		t.Fatalf("Classification sau sửa scope: %+v", c)
	}
}

// bint task set --allow <đường dẫn bảo vệ> trên phiếu created cũng bỏ đường nhanh.
func TestFastPathDroppedBySetTicketToProtected(t *testing.T) {
	h := newHarness(t)
	h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true})
	if err != nil || !tk.State.Classification.Fast {
		t.Fatalf("phải lập đường nhanh: %v %+v", err, tk.State.Classification)
	}
	if _, _, err := r.SetTicket(tk.State.ID, TicketEdit{Allow: []string{"migrations/**"}}); err != nil {
		t.Fatal(err)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Fast || !c.Plan || c.Depth == "" {
		t.Fatalf("Classification: %+v", c)
	}
}

// max_review_rounds = 0: đường nhanh không tự sửa, dừng chờ người như đường đầy đủ.
func TestFastPathNoFixWhenMaxReviewRoundsZero(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 0
	h.behave = h.reviewCounts(2, 0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review" {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
}

// Finding .1 của GĐ1 (a): fastFix chỉ chạy ở review vòng 1. max_review_rounds = 0 → dừng ở vòng 1; --fix-round cho đúng một vòng sửa rồi
// review lại theo luật thường (không mở thêm vòng nhanh), vòng 2 vẫn còn phát hiện thì dừng lại hỏi người.
func TestFastPathFixRoundAfterNoBudgetFollowsNormalRules(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 0
	h.behave = h.reviewCounts(2, 2)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review" || len(res.Choices) != 3 {
		t.Fatalf("vòng 1 phải dừng với ba lựa chọn: %+v %v calls=%v", res, err, h.calls)
	}
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, FixRound: true})
	if err != nil || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review" || res2.Status != task.StatusBlocked {
		t.Fatalf("--fix-round phải chạy b-feat rồi b-review (không phải đường nhanh một vòng): %+v %v calls=%v", res2, err, h.calls)
	}
	if st := loadTask(t, h, res.TaskID).State; st.FastFixed || !st.ReviewExhausted {
		t.Fatalf("vòng review 2 còn phát hiện phải dừng với ba lựa chọn, không FastFixed: %+v", st)
	}
}

// Finding .3 (c): kiểm chứng đỏ sau vòng sửa duy nhất của đường nhanh dừng với ba lựa chọn; --fix-round chạy b-feat rồi b-review lại.
func TestFastPathFixRoundVerifyRedOffersThreeChoices(t *testing.T) {
	h := newHarness(t)
	feats := 0
	rc := h.reviewCounts(1, 0)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			feats++
			if feats == 2 { // vòng sửa làm hỏng cổng kiểm chứng (test -f done.txt)
				h.write("app.txt", "v3\n")
				return os.Remove(filepath.Join(h.root, "done.txt"))
			}
		}
		return rc(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat" {
		t.Fatalf("kiểm chứng đỏ sau vòng sửa phải dừng chờ người, không review lại: %+v %v calls=%v", res, err, h.calls)
	}
	for _, want := range []string{"đường nhanh: kiểm chứng sau vòng sửa duy nhất không qua", "--accept", "--fix-round", "bint task cancel"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if len(res.Choices) != 3 || !loadTask(t, h, res.TaskID).State.ReviewExhausted {
		t.Fatalf("phải có ba lựa chọn: %v", res.Choices)
	}
	n := len(h.calls)
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, FixRound: true})
	if err != nil || strings.Join(h.calls[n:], ",") != "b-feat,b-review" || res2.Status != task.StatusDone {
		t.Fatalf("--fix-round phải chạy b-feat rồi b-review lại (vòng 2 sạch, kiểm chứng xanh): %+v %v %v", res2, err, h.calls[n:])
	}
}

// Finding .4: bint run b-req --task trên phiếu created của đường nhanh bỏ cờ Fast (b-req là đường đầy đủ).
func TestFastPathDroppedWhenBReqRunsOnCreatedTask(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true})
	if err != nil || !tk.State.Classification.Fast {
		t.Fatalf("phải lập đường nhanh: %v %+v", err, tk.State.Classification)
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepReq, TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	if c := loadTask(t, h, tk.State.ID).State.Classification; c.Fast {
		t.Fatalf("b-req trên phiếu created phải bỏ Fast: %+v", c)
	}
}

// Review sạch nhưng kiểm chứng đỏ ở đường nhanh: dùng đúng vòng sửa nhanh (b-feat lần 2 làm kiểm chứng xanh) rồi finalize.
func TestFastPathCleanReviewVerifyRedUsesFastFix(t *testing.T) {
	h := newHarness(t)
	feats := 0
	rc := h.reviewCounts(0)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			feats++
			if feats == 1 { // đổi tệp nhưng chưa làm xanh kiểm chứng
				h.write("app.txt", "v2\n")
				return nil
			}
		}
		return rc(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat" {
		t.Fatalf("review sạch + kiểm chứng đỏ phải sửa một vòng rồi done: %+v %v calls=%v", res, err, h.calls)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "review vòng 1 sạch nhưng kiểm chứng không qua") {
		t.Errorf("notes thiếu ghi chú chưa review lại:\n%s", strings.Join(res.Notes, "\n"))
	}
}
