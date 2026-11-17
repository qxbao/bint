package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"bint/internal/task"
)

// reviewCounts: vòng review k có counts[k-1] phát hiện nghiêm trọng (0 = vòng sạch); hết danh sách thì lặp lại số cuối.
func (h *harness) reviewCounts(counts ...int) func(context.Context, string, *Env, string) error {
	rounds := 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step != StepReview {
			return h.happy(ctx, step, e, id)
		}
		rounds++
		n := counts[min(rounds, len(counts))-1]
		fs := make([]string, n)
		for i := range fs {
			fs[i] = fmt.Sprintf(`{"kind":"bug","severity":"high","title":"sai %d","path":"app.txt","lines":"1","evidence":"v2 sai"}`, i+1)
		}
		_, _, err := e.SubmitReview(id, []byte(`{"summary":"x","findings":[`+strings.Join(fs, ",")+`]}`))
		return err
	}
}

// D2: review không hội tụ thì b-do tự dừng ở nhánh ReviewExhausted, không đợi hết workflow.max_review_rounds.
func TestReviewStopsWhenBlockingCountNotDecreasing(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(3, 3)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review" {
		t.Fatalf("vòng 2 không giảm phải dừng: %+v calls=%v", res, h.calls)
	}
	for _, want := range []string{"review không hội tụ", "vòng review 2", "3 phát hiện nghiêm trọng", "vòng review 1 (3)", "--accept", "--fix-round", "bint task cancel", "review.md"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, res.Reason)
		}
	}
	if len(res.Choices) != 3 || !loadTask(t, h, res.TaskID).State.ReviewExhausted {
		t.Fatalf("phải dùng lối ra hết vòng sửa: %v", res.Choices)
	}
	// Người chọn --fix-round là quyết định tiếp tục: không dừng lại ngay vì cùng lý do.
	n := len(h.calls)
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, FixRound: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.calls[n:], ",") != "b-feat,b-review" {
		t.Fatalf("--fix-round phải chạy đúng một vòng rồi dừng lại: %v", h.calls[n:])
	}
}

// Số giảm thì tiếp tục sửa; vòng 1 không có vòng trước để so nên không bao giờ dừng vì không hội tụ.
func TestReviewContinuesWhenBlockingCountDecreases(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(3, 2, 0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review,b-feat,b-review" {
		t.Fatalf("3→2→0 phải tới done: %+v %v calls=%v", res, err, h.calls)
	}
}

func TestReviewRoundOneNeverStalls(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(2, 0)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review,b-feat,b-review" {
		t.Fatalf("vòng 1 có phát hiện vẫn phải sửa: %+v %v calls=%v", res, err, h.calls)
	}
}

// Driver session đi qua cùng reviewDecision.
func TestSessionReviewStopsWhenNotConverging(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 5
	h.behave = h.reviewCounts(2, 2)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat,b-review,b-feat,b-review" || last.Action != ActionBlocked ||
		!strings.Contains(last.Reason, "review không hội tụ") || len(last.Choices) != 3 {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
}

// Hội tụ (mục 3 của GĐ1 nhóm 1): vòng review giảm hẳn được sửa tiếp ngoài workflow.max_review_rounds tới trần
// 2 × max_review_rounds (+ vòng người cho thêm); không giảm thì dừng.
func TestReviewConvergingFixesBeyondBudgetUpToCeiling(t *testing.T) {
	const fix, rev = "b-feat,", "b-review,"
	cases := []struct {
		name   string
		counts []int
		status string
		rounds int    // số vòng review đã chạy
		reason string // đoạn lý do khi dừng
	}{
		{"5→4→3→2→0 tới done", []int{5, 4, 3, 2, 0}, task.StatusDone, 5, ""},
		{"5→4→3→2→1 dừng ở trần", []int{5, 4, 3, 2, 1}, task.StatusBlocked, 5, "chạm trần 4 vòng sửa"},
		{"4→3→3 dừng ở vòng review 3", []int{4, 3, 3}, task.StatusBlocked, 3, "còn 3 phát hiện nghiêm trọng sau 3 vòng review và 2 vòng sửa"},
		{"4→4 không hội tụ", []int{4, 4}, task.StatusBlocked, 2, "review không hội tụ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Workflow.MaxReviewRounds = 2
			h.behave = h.reviewCounts(tc.counts...)
			res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Repeat(fix+rev, tc.rounds)
			if got := strings.Join(h.calls, ",") + ","; got != want || res.Status != tc.status {
				t.Fatalf("calls=%s want %s, status %s: %s", got, want, res.Status, res.Reason)
			}
			if tc.status == task.StatusBlocked {
				if !strings.Contains(res.Reason, tc.reason) || len(res.Choices) != 3 || !loadTask(t, h, res.TaskID).State.ReviewExhausted {
					t.Fatalf("lý do/lựa chọn: %s %v", res.Reason, res.Choices)
				}
			}
		})
	}
}

// --fix-round nâng cả trần: 2 × 2 + 1 = 5 vòng sửa khi review vẫn giảm.
func TestReviewConvergingCeilingGrowsWithExtraFixRound(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	h.behave = h.reviewCounts(7, 6, 5, 4, 3, 2, 1)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusBlocked || len(h.calls) != 10 {
		t.Fatalf("phải dừng ở trần 4 vòng sửa: %+v %v calls=%v", res, err, h.calls)
	}
	n := len(h.calls)
	res2, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, FixRound: true})
	if err != nil {
		t.Fatal(err)
	}
	// --fix-round nâng trần thêm một (4 → 5 vòng sửa) nên được đúng một vòng sửa nữa (b-feat, b-review) rồi dừng ở trần mới.
	if got := strings.Join(h.calls[n:], ","); got != "b-feat,b-review" || res2.Status != task.StatusBlocked ||
		!strings.Contains(res2.Reason, "chạm trần 5 vòng sửa") {
		t.Fatalf("--fix-round: %v %s", h.calls[n:], res2.Reason)
	}
}

// Driver session đi qua cùng reviewDecision: hội tụ thì sửa tiếp ngoài ngân sách.
func TestSessionReviewConvergingFixesBeyondBudget(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.behave = h.reviewCounts(3, 2, 0)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-feat,b-review,b-feat,b-review,b-feat,b-review" || last.Action != ActionDone {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
}
