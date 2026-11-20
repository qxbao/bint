package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// GĐ2 nhóm 2: kiểm tra theo mục tiêu người dùng (b-req sinh, b-feat viết thành test và chạy tới khi xanh, b-review chấm theo mục tiêu)
// và không tự cắt phạm vi (plan.md chia giai đoạn G-N thì kết quả cuối ghi thẳng "mới xong 1/N giai đoạn").

const goalCheckSection = "`TestMucTieu` (internal/x/goal_test.go): cài bí mật giả vào mọi đường nhập rồi quét toàn bộ thư mục trạng thái và repo; đạt khi không còn tệp nào chứa dấu giả"

// goalDocs là bộ artifact hợp lệ của một tiêu chí, requirement.md có mục `## Kiểm tra mục tiêu` (khi goal != "") và plan.md có phần
// `## Đề xuất tách giai đoạn` (khi phases != "").
func goalDocs(goal, phases string) map[string]string {
	const crit = "app.txt ghi v2"
	req := "# Yêu cầu\n## Hiện trạng\n- app.txt: 1 tệp\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: " + crit + "\n"
	if goal != "" {
		req += "## Kiểm tra mục tiêu\n" + goal + "\n"
	}
	req += "## Ngoài phạm vi\nKhông có\n## Câu hỏi mở\n"
	plan := "# Kế hoạch\nSửa app.txt\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `app.txt`\n- `./app.txt`\n## Bề mặt bị ảnh hưởng\n- app.txt — AC-1\n## Giả định thay người dùng\nKhông có\n## Chiến lược thực thi\nChia lát: không — một nhóm\nKiểm tra mục tiêu độc lập (b-goal): có — đo từ ngoài\n"
	if phases != "" {
		plan += "## Đề xuất tách giai đoạn\n" + phases
	}
	return map[string]string{
		"requirement": req,
		"plan":        plan,
		"tasks":       "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: " + crit + "\n",
	}
}

// goalBehave: b-req ghi goalDocs; b-feat lần 1 đổi tệp nhưng chưa làm kiểm chứng xanh (đỏ), lần 2 làm xanh; review theo counts.
func (h *harness) goalBehave(docs map[string]string, counts ...int) func(context.Context, string, *Env, string) error {
	rc := h.reviewCounts(counts...)
	feats := 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepReq:
			if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}}); err != nil {
				return err
			}
			for n, body := range docs {
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
			return nil
		case StepFeat:
			feats++
			if feats == 1 {
				h.write("app.txt", "v2\n") // kiểm tra mục tiêu (test) còn đỏ: done.txt chưa có
				return nil
			}
		}
		return rc(ctx, step, e, id)
	}
}

// Kiểm tra mục tiêu của phiếu GĐ2 nhóm 2 (requirement.md, mục "Kiểm tra mục tiêu"): đề bài b-feat chép nguyên văn mục kiểm tra mục tiêu;
// review sạch mà kiểm chứng đỏ vẫn mở vòng sửa (b-feat, b-review, b-feat, b-review rồi done); notes cuối có "mới xong 1/3 giai đoạn".
func TestGoalCheckLoopAndPhaseNote(t *testing.T) {
	h := newHarness(t)
	phases := "- G-1: kiểm tra mục tiêu và review theo mục tiêu — internal/workflow/**\n- G-2: nhóm bề mặt log `internal/log/**` $HOME it's — internal/log/** — phụ thuộc G-1\n- G-3: nhóm bề mặt spool — internal/spool/** — phụ thuộc G-1\n"
	h.behave = h.goalBehave(goalDocs(goalCheckSection, phases), 0)
	r := h.runner()
	res0, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res0.Status != task.StatusBlocked || !strings.HasPrefix(res0.Reason, "kế hoạch lớn:") {
		t.Fatalf("kế hoạch có giai đoạn hoãn là lớn, phải dừng sau b-req: %+v %v calls=%v", res0, err, h.calls)
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res0.TaskID, Resume: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	res.Notes = append(slices.Clone(res0.Notes), res.Notes...) // ghi chú sau b-req (kế hoạch chia N giai đoạn) nằm ở lượt đầu
	if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-review,b-feat,b-review" {
		t.Fatalf("review sạch mà kiểm chứng đỏ phải mở vòng sửa: %s", got)
	}
	// Có b-goal: mục kiểm tra mục tiêu đi vào đề bài b-goal (requirement.md nguyên văn), b-feat chỉ nhận khối test đóng băng.
	if p := h.prompts[StepGoal]; !strings.Contains(p, goalCheckSection) {
		t.Errorf("đề bài b-goal phải chép nguyên văn requirement.md có mục kiểm tra mục tiêu:\n%s", p)
	}
	if p := h.prompts[StepFeat]; !strings.Contains(p, "ĐÃ ĐƯỢC VIẾT độc lập") || strings.Contains(p, "viết thành test tự động") {
		t.Errorf("đề bài b-feat phải nêu test đóng băng thay vì bảo tự viết:\n%s", p)
	}
	notes := strings.Join(res.Notes, "\n")
	for _, want := range []string{"mới xong 1/3 giai đoạn", "G-2: nhóm bề mặt log", "G-3: nhóm bề mặt spool", "bint task new", "--parent", "--depends-on"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes thiếu %q:\n%s", want, notes)
		}
	}
	// Lệnh gợi ý: tiêu đề chỉ là phần mục tiêu, trích dẫn nháy đơn (backtick, $, ' không bị shell diễn giải), không còn dấu nháy kép kiểu Go.
	if want := `bint task new 'nhóm bề mặt log ` + "`internal/log/**`" + ` $HOME it'\''s' --parent`; !strings.Contains(notes, want) {
		t.Errorf("lệnh lập phiếu con phải trích dẫn an toàn cho shell, muốn %q:\n%s", want, notes)
	}
	if strings.Contains(notes, `bint task new "`) || strings.Contains(notes, "it's — internal/log/** — phụ thuộc G-1' --parent") {
		t.Errorf("tiêu đề phiếu con không được kèm phạm vi/phụ thuộc hay nháy kép:\n%s", notes)
	}
	if !strings.Contains(notes, "chỉnh theo dòng G-N tương ứng trong plan.md") {
		t.Errorf("notes phải dặn chỉnh phụ thuộc/phạm vi theo plan.md:\n%s", notes)
	}
	if !strings.Contains(notes, "kế hoạch chia 3 giai đoạn, phiếu này chỉ làm G-1") {
		t.Errorf("notes sau b-req phải nêu kế hoạch chia 3 giai đoạn:\n%s", notes)
	}
}

// Không có dòng G-N (hoặc chỉ một) thì không có ghi chú giai đoạn; thiếu mục kiểm tra mục tiêu thì có note nhắc (cảnh báo goal_check_missing).
// Cả hai đều làm kế hoạch lớn (cổng cỡ kế hoạch): tác vụ dừng sau b-req, resume chạy b-feat.
func TestGoalCheckMissingNoteAndNoPhaseNote(t *testing.T) {
	h := newHarness(t)
	h.behave = h.goalBehave(goalDocs("", "- G-1: làm hết — internal/**\n"), 0)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req" || !strings.HasPrefix(res.Reason, "kế hoạch lớn:") {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil || res2.Status != task.StatusDone {
		t.Fatalf("%+v %v", res2, err)
	}
	notes := strings.Join(res.Notes, "\n") + "\n" + strings.Join(res2.Notes, "\n")
	if strings.Contains(notes, "mới xong") || strings.Contains(notes, "giai đoạn, phiếu này") {
		t.Errorf("một giai đoạn thì không báo mới xong:\n%s", notes)
	}
	if !strings.Contains(notes, "thiếu mục `## Kiểm tra mục tiêu`") {
		t.Errorf("notes phải nhắc thiếu mục kiểm tra mục tiêu:\n%s", notes)
	}
	if strings.Contains(h.prompts[StepFeat], "Kiểm tra mục tiêu (requirement.md") {
		t.Errorf("không có mục thì đề bài không có khối kiểm tra mục tiêu:\n%s", h.prompts[StepFeat])
	}
}

// Review không còn phát hiện nghiêm trọng nhưng kiểm chứng cuối đỏ: trong ngân sách mở vòng sửa; đề bài vòng sửa nêu log kiểm chứng đỏ.
func TestReviewCleanButVerifyRedOpensFixRound(t *testing.T) {
	h := newHarness(t)
	h.behave = h.goalBehave(goalDocs(goalCheckSection, ""), 0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review,b-feat,b-review" {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	p := h.prompts[StepFeat] // đề bài b-feat lần cuối = vòng sửa
	if !strings.Contains(p, "Kiểm chứng cuối không qua") || !strings.Contains(p, "test -f done.txt") {
		t.Errorf("đề bài vòng sửa phải nêu kiểm chứng đỏ:\n%s", p)
	}
}

// Ngoài ngân sách (max_review_rounds = 0): dừng với ba lựa chọn, ReviewExhausted, lý do nêu kiểm chứng cuối không qua (không phải "không hội tụ").
func TestReviewCleanButVerifyRedBlocksOutsideBudget(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 0
	h.behave = h.goalBehave(goalDocs(goalCheckSection, ""), 0)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review" {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	for _, want := range []string{"không còn phát hiện nghiêm trọng nhưng kiểm chứng cuối không qua", "test -f done.txt", "--accept", "--fix-round", "bint task cancel"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if strings.Contains(res.Reason, "không hội tụ") {
		t.Errorf("0 phát hiện không phải review đứng yên: %s", res.Reason)
	}
	if len(res.Choices) != 3 || !loadTask(t, h, res.TaskID).State.ReviewExhausted {
		t.Fatalf("phải có ba lựa chọn: %v", res.Choices)
	}
	// Người cho thêm một vòng sửa: b-feat lần 2 làm xanh, review sạch → done.
	n := len(h.calls)
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, FixRound: true})
	if err != nil || res2.Status != task.StatusDone || strings.Join(h.calls[n:], ",") != "b-feat,b-review" {
		t.Fatalf("--fix-round: %+v %v %v", res2, err, h.calls[n:])
	}
}

// Driver session đi qua cùng reviewDecision: review sạch + kiểm chứng đỏ mở vòng sửa.
func TestSessionReviewCleanButVerifyRedOpensFixRound(t *testing.T) {
	h := newHarness(t)
	h.behave = h.goalBehave(goalDocs(goalCheckSection, ""), 0)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(true)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-req,b-goal,b-feat,b-review,b-feat,b-review" || last.Action != ActionDone {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
}

// Đề bài đường nhanh (không có b-req) dặn b-feat tự viết kiểm tra mục tiêu trước khi sửa.
func TestFastPromptAsksForGoalCheck(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reviewCounts(0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	for _, want := range []string{"Kiểm tra mục tiêu: TRƯỚC khi sửa", "không còn X ở đâu", "giảm/dọn Y", "bộ test mà lệnh kiểm chứng"} {
		if !strings.Contains(h.prompts[StepFeat], want) {
			t.Errorf("đề bài đường nhanh thiếu %q:\n%s", want, h.prompts[StepFeat])
		}
	}
}

// Đề bài review chấm theo mục tiêu: luật high/low mới, mục kiểm tra mục tiêu chép nguyên văn, đường dẫn log khi kiểm chứng cuối đỏ.
func TestReviewPromptGradesByGoal(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 0
	rc := h.goalBehave(goalDocs(goalCheckSection, ""), 0)
	var reviewPrompt string
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			reviewPrompt = h.prompts[StepReview]
		}
		return rc(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("%+v %v", res, err)
	}
	for _, want := range []string{"chấm theo MỤC TIÊU của người dùng", "là `high`", "kể cả khi nằm ngoài kế hoạch", "chỉ giữ `low`", "suy đoán",
		"## Kiểm tra mục tiêu (requirement.md", goalCheckSection, "Kiểm chứng cuối KHÔNG QUA", ".log"} {
		if !strings.Contains(reviewPrompt, want) {
			t.Errorf("đề bài review thiếu %q:\n%s", want, reviewPrompt)
		}
	}
	if strings.Contains(reviewPrompt, "ngoài mọi AC chỉ là `low`") {
		t.Errorf("đề bài review còn luật cũ (ngoài kế hoạch chỉ là low):\n%s", reviewPrompt)
	}
}

// Lời nhắn nối tiếp b-req → b-feat chép nguyên văn mục kiểm tra mục tiêu.
func TestPlanHandoffCarriesGoalCheck(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	inner := h.goalBehave(goalDocs(goalCheckSection, ""), 0)
	h.behave = inner
	res := runPlanned(t, h, nil)
	_, feat := planReqs(h)
	if res.Status != task.StatusDone || feat.ResumeSessionID == "" || !strings.Contains(feat.Prompt, "ĐÃ ĐƯỢC VIẾT độc lập") {
		t.Fatalf("lời nhắn nối tiếp phải nêu kiểm tra mục tiêu đóng băng: %v\n%s", res.Status, feat.Prompt)
	}
}

// Flow nhiều bước review: vòng sửa mở vì kiểm chứng đỏ (các review của nó đều sạch) vẫn bị đếm vào workflow.max_review_rounds; vượt thì
// dừng với ba lựa chọn thay vì sửa mãi (review 01M7M91BMTZFW1THB5AWVZY033.1).
func TestMultiReviewFlowRedVerifyCountsFixRounds(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.writeFlow("duo", twoReviewFlow)
	inner := h.twoReviewBehave(func(string, int) bool { return false }) // mọi review sạch
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == "impl" { // đổi tệp nhưng kiểm chứng (test -f done.txt) luôn đỏ
			h.write("app.txt", "v2\n")
			return nil
		}
		return inner(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: "duo", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	if got, want := strings.Join(h.calls, ","), "impl,rev-a,rev-b,impl,rev-a,rev-b"; got != want {
		t.Fatalf("đúng một vòng sửa trong ngân sách 1: %s, muốn %s", got, want)
	}
	if !strings.Contains(res.Reason, "kiểm chứng cuối không qua") || len(res.Choices) != 3 || !loadTask(t, h, res.TaskID).State.ReviewExhausted {
		t.Fatalf("phải dừng với ba lựa chọn: %s %v", res.Reason, res.Choices)
	}
}

// Hết vòng sửa khi còn phát hiện nghiêm trọng và kiểm chứng cuối cũng đỏ: lý do nêu cả lệnh kiểm chứng đỏ (review 01M7M91BMTZFW1THB5AWVZY033.9).
func TestReviewBlockingAndVerifyRedReasonNamesVerification(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 0
	h.behave = h.goalBehave(goalDocs(goalCheckSection, ""), 2)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	for _, want := range []string{"còn 2 phát hiện nghiêm trọng", "Kiểm chứng cuối cũng không qua", "test -f done.txt", "mã thoát", "--accept"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
}
