package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/task"
)

// Hồi quy từ benchmark lần 2 (docs/evals/bench-bint-vs-plain-v2-20261116): cổng không-đổi-tệp, câu hỏi của b-feat,
// ba lựa chọn khi hết vòng sửa, nối tiếp phiên b-feat ở vòng sửa, đề bài review kèm diff và log scope none.

const vagueReq = "Thêm tính năng phiên bản v2 cho app.txt"

func choiceCommands(cs []Choice) string {
	var s []string
	for _, c := range cs {
		s = append(s, c.Command)
	}
	return strings.Join(s, "\n")
}

// b-feat không đổi tệp nào: không kiểm chứng, không review, không done; blocked kèm lệnh trả lời và huỷ, nêu điều b-feat đã ghi.
func TestNoChangeBlocksBeforeVerifyAndReview(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			_, err := e.RecordError(id, StepFeat, "chưa rõ cần dọn loại dữ liệu nào")
			return err
		}
		return h.happy(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.TaskID
	tk := loadTask(t, h, id)
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat" || len(tk.State.Verifications) != 0 || tk.State.ReviewRounds != 0 {
		t.Fatalf("phải blocked, 1 lần b-feat, 0 kiểm chứng, 0 review: %+v calls=%v ver=%d", res, h.calls, len(tk.State.Verifications))
	}
	for _, want := range []string{"không đổi tệp nào", "chưa rõ cần dọn loại dữ liệu nào", "bint run resume " + id + ` --answer "<nội dung>"`, "bint task cancel " + id} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if got := choiceCommands(res.Choices); !strings.Contains(got, "--answer") || !strings.Contains(got, "bint task cancel "+id) {
		t.Fatalf("choices: %v", res.Choices)
	}
	if ls := tk.State.LastStep; ls == nil || ls.StopClass != StopAwaitingHuman || ls.Step != StepFeat || !tk.State.AwaitingAnswer {
		t.Fatalf("LastStep/AwaitingAnswer: %+v", tk.State.LastStep)
	}
}

// Có thay đổi tệp thì cổng không chặn (giữ hành vi cũ cho việc rõ).
func TestChangedFilesPassGate(t *testing.T) {
	h := newHarness(t)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
}

// Driver session: cùng cổng, lệnh dạng `bint run next <id> --resume --answer`.
func TestSessionNoChangeBlocksWithNextResumeAnswer(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			return nil
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat" || last.Action != ActionBlocked {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
	if !strings.Contains(last.Reason, "bint run next "+tk.State.ID+` --resume --answer "<nội dung>"`) || len(last.Choices) != 2 {
		t.Fatalf("lý do/lựa chọn: %+v", last)
	}
}

// Câu hỏi của b-feat (task error --question): blocked chờ người (awaiting_human), lý do nguyên văn câu hỏi + lệnh trả lời;
// resume --answer chạy lại b-feat trên chính tác vụ, đề bài có nguyên văn câu hỏi và câu trả lời; đề bài review cũng mang Q/A.
func TestFeatQuestionBlocksThenAnswerRerunsFeat(t *testing.T) {
	h := newHarness(t)
	asked := false
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat && !asked {
			asked = true
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			_, err := e.RecordQuestion(id, StepFeat, "Giữ nhật ký bao nhiêu ngày?")
			return err
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.TaskID
	tk := loadTask(t, h, id)
	if res.Status != task.StatusBlocked || len(tk.State.Verifications) != 0 || strings.Join(h.calls, ",") != "b-feat" {
		t.Fatalf("câu hỏi phải dừng trước kiểm chứng/review: %+v calls=%v", res, h.calls)
	}
	if ls := tk.State.LastStep; ls == nil || ls.StopClass != StopAwaitingHuman {
		t.Fatalf("LastStep: %+v", tk.State.LastStep)
	}
	for _, want := range []string{`"Giữ nhật ký bao nhiêu ngày?"`, "bint run resume " + id + ` --answer`} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if len(res.Questions) != 1 || res.Questions[0] != "Giữ nhật ký bao nhiêu ngày?" {
		t.Fatalf("questions: %v", res.Questions)
	}
	// --answer chỉ hợp lệ khi tác vụ đang chờ b-feat; sai chỗ bị từ chối.
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Accept: true}); err == nil {
		t.Fatal("--accept khi chưa hết vòng sửa phải bị từ chối")
	}
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Answer: "30 ngày"})
	if err != nil || res2.Status != task.StatusDone || res2.TaskID != id {
		t.Fatalf("resume --answer: %+v %v", res2, err)
	}
	if strings.Join(h.calls, ",") != "b-feat,b-feat,b-review" {
		t.Fatalf("b-feat phải chạy lại trên chính tác vụ: %v", h.calls)
	}
	for _, step := range []string{StepFeat, StepReview} {
		p := h.prompts[step]
		if !strings.Contains(p, "Hỏi: Giữ nhật ký bao nhiêu ngày?") || !strings.Contains(p, "Trả lời: 30 ngày") {
			t.Errorf("đề bài %s thiếu câu hỏi/trả lời nguyên văn:\n%s", step, p)
		}
	}
	got := loadTask(t, h, id)
	if got.State.AwaitingAnswer || len(got.State.OpenQuestions("")) != 0 {
		t.Fatalf("cờ chờ trả lời phải được xoá: %+v", got.State)
	}
}

// Agent ghi câu hỏi mà quên --step: câu hỏi vẫn chặn sau bước (gán cho bước đang chạy) và được đóng khi người trả lời
// (review 01M7JFYDZ07RFN10HF750YZ1VP.2).
func TestFeatQuestionWithoutStepStillBlocks(t *testing.T) {
	h := newHarness(t)
	asked := false
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat && !asked {
			asked = true
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			_, err := e.RecordQuestion(id, "", "Giữ nhật ký bao nhiêu ngày?")
			return err
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.TaskID
	tk := loadTask(t, h, id)
	if res.Status != task.StatusBlocked || len(tk.State.Verifications) != 0 || strings.Join(h.calls, ",") != "b-feat" {
		t.Fatalf("câu hỏi thiếu --step vẫn phải dừng trước kiểm chứng/review: %+v calls=%v", res, h.calls)
	}
	if q := tk.State.Questions; len(q) != 1 || q[0].Step != StepFeat {
		t.Fatalf("câu hỏi phải được gán cho bước đang chạy: %+v", q)
	}
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Answer: "30 ngày"})
	if err != nil || res2.Status != task.StatusDone {
		t.Fatalf("resume --answer: %+v %v", res2, err)
	}
	if got := loadTask(t, h, id); len(got.State.OpenQuestions("")) != 0 {
		t.Fatalf("câu hỏi phải được trả lời: %+v", got.State.Questions)
	}
}

// Câu hỏi không gắn bước (dữ liệu cũ hoặc ghi khi chưa biết bước) vẫn được coi là câu hỏi mở của bước implement.
func TestStepQuestionWithoutStepMatchesAnyStep(t *testing.T) {
	s := task.State{Questions: []task.StepQuestion{{Text: "q"}}}
	if len(s.OpenQuestions(StepFeat)) != 1 {
		t.Fatal("câu hỏi không gắn bước phải thuộc mọi bước")
	}
}

// --answer chỉ dùng khi tiếp tục tác vụ đang chờ.
func TestAnswerRejectedWhenNotWaiting(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: vagueReq, Workflow: StepDo, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Answer: "x"}); err == nil {
		t.Fatal("--answer không kèm resume phải bị từ chối")
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Resume: true, Answer: "x"}); err == nil {
		t.Fatal("--answer khi tác vụ không chờ câu trả lời phải bị từ chối")
	}
}

// Nối tiếp phiên b-feat sau câu trả lời (driver process, harness giả hỗ trợ): ResumeSessionID là phiên của lượt hỏi, lời nhắn
// nối tiếp chứa nguyên văn câu hỏi và câu trả lời.
func TestAnswerResumesFeatSession(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	asked := false
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat && !asked {
			asked = true
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			_, err := e.RecordQuestion(id, StepFeat, "Giữ nhật ký bao nhiêu ngày?")
			return err
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("%+v %v", res, err)
	}
	first := lastFeatReq(h)
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, Answer: "30 ngày"}); err != nil {
		t.Fatal(err)
	}
	second := lastFeatReq(h)
	if second.ResumeSessionID != first.SessionID || first.SessionID == "" {
		t.Fatalf("phải nối tiếp phiên %q: %+v", first.SessionID, second)
	}
	if !strings.Contains(second.Prompt, "Tiếp tục bước "+StepFeat) || !strings.Contains(second.Prompt, "Hỏi: Giữ nhật ký bao nhiêu ngày?") ||
		!strings.Contains(second.Prompt, "Trả lời: 30 ngày") {
		t.Fatalf("lời nhắn nối tiếp:\n%s", second.Prompt)
	}
}

// Hết vòng sửa: lý do nêu đúng số vòng review và vòng sửa; ba lựa chọn; --fix-round chạy thêm đúng một b-feat + một b-review;
// --accept vào thẳng finalize (done) không chạy bước nào.
func TestReviewExhaustedChoicesAcceptAndFixRound(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	h.behave = h.reviewCounts(9, 8, 8) // vòng 3 không giảm: hết ngân sách, không gia hạn
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.TaskID
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review,b-feat,b-review" {
		t.Fatalf("2 vòng sửa rồi dừng: %+v calls=%v", res, h.calls)
	}
	for _, want := range []string{"3 vòng review", "2 vòng sửa", "tối đa 2 vòng sửa", "workflow.max_review_rounds = 2",
		"bint run resume " + id + " --accept", "bint run resume " + id + " --fix-round", "bint task cancel " + id, "review.md"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if got := choiceCommands(res.Choices); len(res.Choices) != 3 || !strings.Contains(got, "--accept") || !strings.Contains(got, "--fix-round") || !strings.Contains(got, "bint task cancel") {
		t.Fatalf("choices: %v", res.Choices)
	}
	if !loadTask(t, h, id).State.ReviewExhausted {
		t.Fatal("ReviewExhausted phải bật")
	}
	// Cờ loại trừ nhau.
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Accept: true, FixRound: true}); err == nil {
		t.Fatal("--accept và --fix-round loại trừ nhau")
	}
	// --fix-round: đúng thêm một b-feat và một b-review, rồi dừng lại với số mới.
	n := len(h.calls)
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, FixRound: true})
	if err != nil || res2.Status != task.StatusBlocked {
		t.Fatalf("--fix-round: %+v %v", res2, err)
	}
	if strings.Join(h.calls[n:], ",") != "b-feat,b-review" {
		t.Fatalf("--fix-round phải chạy đúng b-feat rồi b-review: %v", h.calls[n:])
	}
	if !strings.Contains(res2.Reason, "4 vòng review") || !strings.Contains(res2.Reason, "3 vòng sửa") || !strings.Contains(res2.Reason, "tối đa 3 vòng sửa") {
		t.Fatalf("lý do sau vòng thêm: %s", res2.Reason)
	}
	// --accept: vào thẳng finalize.
	n = len(h.calls)
	res3, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Accept: true})
	if err != nil || res3.Status != task.StatusDone || len(h.calls) != n {
		t.Fatalf("--accept phải done không chạy bước nào: %+v %v calls=%v", res3, err, h.calls[n:])
	}
	done := loadTask(t, h, id)
	found := false
	for _, tr := range done.State.History {
		if strings.Contains(tr.Reason, "người chấp nhận") {
			found = true
		}
	}
	if !found {
		t.Fatalf("lịch sử phải ghi người chấp nhận: %+v", done.State.History)
	}
}

// Driver session: cùng ba lựa chọn, --accept qua `next --resume`.
func TestSessionReviewExhaustedAccept(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.behave = h.reviewCounts(9, 9) // vòng 2 không giảm: hết ngân sách, không gia hạn
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat,b-review,b-feat,b-review" || last.Action != ActionBlocked {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
	if !strings.Contains(last.Reason, "2 vòng review") || !strings.Contains(last.Reason, "1 vòng sửa") || len(last.Choices) != 3 ||
		!strings.Contains(choiceCommands(last.Choices), "bint run next "+tk.State.ID+" --resume --accept") {
		t.Fatalf("lý do/lựa chọn: %+v", last)
	}
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true, Accept: true})
	if err != nil || act.Action != ActionDone {
		t.Fatalf("--accept: %+v %v", act, err)
	}
	if _, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Accept: true}); err == nil {
		t.Fatal("--accept không kèm --resume phải bị từ chối")
	}
}

// Vòng sửa sau review nối tiếp phiên b-feat trước (driver process, harness giả hỗ trợ); --fresh thì không và ghi lý do.
func TestReviewFixRoundResumesFeatSession(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		procSetup(t)
		h := newHarness(t)
		h.claudeLike(nil)
		h.cfg.Workflow.MaxReviewRounds = 1
		h.behave = h.blockingReview(1)
		res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false), Fresh: fresh})
		if err != nil || res.Status != task.StatusDone {
			t.Fatalf("fresh=%v: %+v %v", fresh, res, err)
		}
		var feats []eval.AgentRequest
		for i, c := range h.calls {
			if c == StepFeat {
				feats = append(feats, h.reqs[i])
			}
		}
		if len(feats) != 2 {
			t.Fatalf("fresh=%v: %d lần b-feat", fresh, len(feats))
		}
		if fresh {
			if feats[1].ResumeSessionID != "" || !strings.Contains(feats[1].Prompt, "<bint-task") {
				t.Fatalf("--fresh phải chạy đề bài đầy đủ: %+v", feats[1])
			}
			if !strings.Contains(strings.Join(res.Notes, "\n"), "--fresh") {
				t.Fatalf("notes phải nêu lý do: %v", res.Notes)
			}
			continue
		}
		if feats[1].ResumeSessionID != feats[0].SessionID || feats[0].SessionID == "" {
			t.Fatalf("vòng sửa phải nối tiếp phiên %q: %+v", feats[0].SessionID, feats[1])
		}
		p := feats[1].Prompt
		if strings.Contains(p, "<bint-task") || !strings.Contains(p, "Tiếp tục bước "+StepFeat) || !strings.Contains(p, "Phát hiện review vòng 1 cần xử lý") ||
			!strings.Contains(p, "v2 sai") {
			t.Fatalf("lời nhắn nối tiếp vòng sửa:\n%s", p)
		}
	}
}

// Không có phiên để nối tiếp (agent giả không báo handle): vòng sửa chạy đề bài đầy đủ như cũ, ghi lý do vào notes.
func TestReviewFixRoundWithoutSessionFallsBack(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.behave = h.blockingReview(1)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(h.prompts[StepFeat], "<bint-task") || !strings.Contains(strings.Join(res.Notes, "\n"), "vòng sửa chạy với đề bài đầy đủ") {
		t.Fatalf("phải chạy đề bài đầy đủ kèm ghi chú: %v", res.Notes)
	}
}

// Đề bài review: kèm sẵn danh sách tệp, thống kê và diff (nhỏ), plan.md, nhiệm vụ tìm chỗ bỏ sót; không còn câu giới hạn việc review vào
// "chỉ các tệp tác vụ này đã sửa"; vòng 2 nêu phát hiện nghiêm trọng của vòng 1.
func TestReviewPromptCarriesDiffPlanAndOmissionTask(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.behave = h.blockingReview(1)
	reviewPrompts := []string{}
	inner := h.behave
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			h.mu.Lock()
			reviewPrompts = append(reviewPrompts, h.prompts[StepReview])
			h.mu.Unlock()
		}
		return inner(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq})
	if err != nil || res.Status != task.StatusDone || len(reviewPrompts) != 2 {
		t.Fatalf("%+v %v prompts=%d", res, err, len(reviewPrompts))
	}
	p := reviewPrompts[0]
	for _, want := range []string{"Diff của tác vụ (đã có sẵn, 2 tệp", "- app.txt", "- done.txt", "Thống kê:", "Diff đầy đủ:", "+v2", "Kế hoạch của tác vụ (plan.md",
		"Sửa app.txt", "BỊ BỎ SÓT", "YÊU CẦU GỐC", "Bề mặt bị ảnh hưởng", "tệp ngoài diff", "bint task diff " + res.TaskID} {
		if !strings.Contains(p, want) {
			t.Errorf("đề bài review thiếu %q", want)
		}
	}
	if strings.Contains(p, "chỉ các tệp tác vụ này đã sửa") {
		t.Errorf("đề bài review còn câu giới hạn phạm vi:\n%s", p)
	}
	if strings.Contains(p, "Phát hiện nghiêm trọng của vòng review") {
		t.Errorf("vòng 1 chưa có phát hiện trước đó")
	}
	if !strings.Contains(reviewPrompts[1], "Phát hiện nghiêm trọng của vòng review 1") || !strings.Contains(reviewPrompts[1], "sai") {
		t.Errorf("vòng 2 phải nêu phát hiện nghiêm trọng của vòng 1:\n%s", reviewPrompts[1])
	}
}

// Diff vượt ngưỡng: chỉ thống kê kèm lệnh `bint task diff`; plan.md dài bị cắt.
func TestReviewPromptLargeDiffAndPlanTruncated(t *testing.T) {
	h := newHarness(t)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepFeat:
			h.write("big.txt", strings.Repeat("dòng dài của tệp lớn để vượt ngưỡng diff\n", 2000))
			h.write("done.txt", "ok\n")
			return nil
		case StepReq:
			if err := h.writePlan(e, id, nil); err != nil {
				return err
			}
			_, _, err := e.WriteArtifact(id, "plan", "# Kế hoạch\n"+strings.Repeat("ý rất dài trong kế hoạch\n", 2000))
			return err
		}
		return h.happy(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	p := h.prompts[StepReview]
	if strings.Contains(p, "Diff đầy đủ:\n```diff") || !strings.Contains(p, "quá ngưỡng") || !strings.Contains(p, "bint task diff "+res.TaskID) ||
		!strings.Contains(p, "big.txt") || !strings.Contains(p, "Thống kê:") {
		t.Fatalf("diff lớn chỉ kèm thống kê và lệnh:\n%.1500s", p)
	}
	if !strings.Contains(p, "đã cắt") || len(p) > 80<<10 {
		t.Fatalf("plan.md phải bị cắt, đề bài %d byte", len(p))
	}
}

// Kiểm chứng scope none không còn in dòng lệnh như thể đã chạy.
func TestScopeNoneLogDoesNotClaimCommandRan(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() {
		h.write("docs/readme.md", "đổi\n")
		h.write("done.txt", "ok\n")
	})
	var logs []string
	r := h.runner()
	r.Logf = func(format string, a ...any) { logs = append(logs, strings.TrimSpace(fmt.Sprintf(format, a...))) }
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, Request: "Thêm tính năng cho gói a"}); err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range logs {
		if strings.Contains(l, "kiểm chứng (scope none)") {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "không chạy lệnh") || strings.Contains(line, "→ qua") || strings.Contains(line, "→ không qua") {
		t.Fatalf("dòng log scope none: %q (logs=%v)", line, logs)
	}
}

// Driver session: vòng sửa sau review nối tiếp subagent b-feat trước (continue_agent_id, lời nhắn có phát hiện nghiêm trọng).
func TestSessionReviewFixRoundContinuesFeatSubagent(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.behave = h.blockingReview(1)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	var feats []*SessionAction
	for i := 0; i < 12; i++ {
		act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
		if err != nil {
			t.Fatal(err)
		}
		if act.Action != ActionAgent {
			if act.Action != ActionDone {
				t.Fatalf("dừng: %+v", act)
			}
			break
		}
		if act.Step == StepFeat {
			feats = append(feats, act)
		}
		h.subagent(act)
		if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: act.RunID, AgentID: "agent-" + act.Step}); err != nil {
			t.Fatal(err)
		}
	}
	if len(feats) != 2 {
		t.Fatalf("%d lần b-feat", len(feats))
	}
	if feats[0].ContinueAgentID != "" || feats[1].ContinueAgentID != "agent-b-feat" {
		t.Fatalf("vòng sửa phải nối tiếp agent cũ: %q / %q", feats[0].ContinueAgentID, feats[1].ContinueAgentID)
	}
	if p := feats[1].Prompt; !strings.Contains(p, "Tiếp tục bước b-feat") || !strings.Contains(p, "Phát hiện review vòng 1 cần xử lý") || strings.Contains(p, "<bint-task") {
		t.Fatalf("lời nhắn nối tiếp:\n%s", p)
	}
}

// Cây đổi giữa lúc b-feat kết thúc và lúc kiểm chứng (người sửa tệp giữa lần kiểm chứng bị ngắt và lần resume): vòng sửa không
// được nối tiếp phiên b-feat cũ (review 01M7JFYDZ07RFN10HF750YZ1VP.5).
func TestRefreshLastImplementDropsFingerprintWhenTreeChanged(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fp, err := r.stopFingerprint(ctx)
	if err != nil || fp == "" {
		t.Fatalf("fingerprint: %q %v", fp, err)
	}
	for _, tc := range []struct {
		pre  string
		want bool // còn nối tiếp được
	}{{fp, true}, {"cây-khác", false}} {
		tk, err := r.Tasks.Update(res.TaskID, r.now(), func(t *task.Task) error {
			t.State.LastImplement = &task.StepStop{Step: StepFeat, RunID: "r1", SessionID: "s1", Fingerprint: fp, Continuable: true}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		rs := &runState{t: tk}
		r.refreshLastImplement(ctx, rs, tc.pre)
		li := loadTask(t, h, res.TaskID).State.LastImplement
		if got := li.Fingerprint != "" && li.Continuable; got != tc.want {
			t.Fatalf("pre=%q: nối tiếp=%v, muốn %v (%+v)", tc.pre, got, tc.want, li)
		}
	}
}

// Nội dung có sẵn ``` (SKILL.md, plan.md) không được đóng rào sớm (review 01M7JFYDZ07RFN10HF750YZ1VP.6).
func TestFencedOutlastsInnerBackticks(t *testing.T) {
	body := "a\n```sh\nrm -rf x\n```\nb ````` c\n"
	got := fenced("diff", body)
	if !strings.HasPrefix(got, "``````diff\n") || !strings.HasSuffix(got, "\n``````\n") {
		t.Fatalf("rào phải dài hơn chuỗi backtick dài nhất (5):\n%s", got)
	}
	if got := fenced("", "x"); got != "```\nx\n```\n" {
		t.Fatalf("rào tối thiểu ```: %q", got)
	}
}
