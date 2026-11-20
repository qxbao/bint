package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/task"
)

// leanDocs là bộ tài liệu gọn của luồng B′: chỉ mục tiêu + danh sách "xong là gì", không có các mục mở rộng của luồng hiện tại.
func leanDocs(acs ...string) map[string]string {
	var req, tsk strings.Builder
	req.WriteString("# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n")
	for i, a := range acs {
		n := i + 1
		req.WriteString("- [ ] AC-" + itoa(n) + ": " + a + "\n")
		tsk.WriteString("- [ ] TSK-" + itoa(n) + " [AC-" + itoa(n) + "]: làm " + a + " — xong khi: " + a + "\n")
	}
	return map[string]string{
		"requirement": req.String(),
		"plan":        "# Kế hoạch\nSửa app.txt\nGhi v2\n",
		"tasks":       tsk.String(),
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

// bprimePlanner: b-req ghi bộ tài liệu gọn (2 AC, 0 câu hỏi); các bước khác như harness mặc định.
func (h *harness) bprimePlanner() func(context.Context, string, *Env, string) error {
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step != StepReq {
			return h.happy(ctx, step, e, id)
		}
		crit := []string{"app.txt ghi v2", "done.txt tồn tại"}
		if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: crit, Allow: []string{"app.txt", "done.txt"}}); err != nil {
			return err
		}
		for n, body := range leanDocs(crit...) {
			if _, _, err := e.WriteArtifact(id, n, body); err != nil {
				return err
			}
		}
		return nil
	}
}

// Kiểm tra mục tiêu của tác vụ 01M7VM1BHGSDG01DKJXRG8RSZS (luồng B′, giai đoạn 1): xem requirement.md mục "Kiểm tra mục tiêu".
// Mỗi ô (a)–(e) báo riêng bằng t.Errorf.
func TestBPrimeLeanPlanningGoal(t *testing.T) {
	ctx := context.Background()
	const request = vagueGoalReq
	headings := []string{"## Bề mặt bị ảnh hưởng", "## Giả định thay người dùng", "## Cỡ kế hoạch"}

	// (a) mặc định là classic.
	if v := config.Default().Workflow.Variant; v != config.VariantClassic {
		t.Errorf("(a) Default().Workflow.Variant = %q, muốn %q", v, config.VariantClassic)
	}

	run := func(t *testing.T, variant string, switchBack bool) (*harness, *task.Task, string) {
		t.Helper()
		h := newHarness(t)
		h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
		h.cfg.Workflow.Variant = variant
		h.behave = h.bprimePlanner()
		var res *Result
		if switchBack {
			// Lập phiếu ở bprime, đổi cấu hình về classic rồi mới chạy: biến thể đã chốt lúc lập phiếu.
			tk, _, err := h.runner().Env.NewTask(ctx, NewTaskOptions{Request: request})
			if err != nil {
				t.Fatal(err)
			}
			h.cfg.Workflow.Variant = config.VariantClassic
			r, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
			if err != nil {
				t.Fatal(err)
			}
			res = r
		} else {
			r, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: request})
			if err != nil {
				t.Fatal(err)
			}
			res = r
		}
		return h, loadTask(t, h, res.TaskID), res.Reason
	}

	t.Run("bprime", func(t *testing.T) {
		h, tk, reason := run(t, config.VariantBPrime, true)
		// (b)
		if c := tk.State.Classification; c == nil || c.Variant != task.VariantBPrime {
			t.Errorf("(b) classification.variant phải là bprime, được %+v", c)
		}
		// (c)
		p := h.prompts[StepReq]
		if !strings.Contains(p, bprimeReqMarker) {
			t.Errorf("(c) đề bài b-req thiếu khối B′ %q", bprimeReqMarker)
		}
		for _, hd := range headings {
			if strings.Contains(p, hd) {
				t.Errorf("(c) đề bài b-req B′ không được đòi %q", hd)
			}
		}
		// (d)
		rep := task.LintPlanningTask(tk, true)
		if len(rep.Errors) != 0 || len(rep.Warnings) != 0 {
			t.Errorf("(d) lint B′ phải sạch: lỗi=%v cảnh báo=%v", rep.Errors, rep.Warnings)
		}
		if len(rep.OpenQuestions) != 0 {
			t.Errorf("(d) không được có câu hỏi mở: %v", rep.OpenQuestions)
		}
		// (e)
		if !slices.Contains(h.calls, StepFeat) {
			t.Errorf("(e) b-feat phải chạy ngay sau b-req: calls=%v (%s)", h.calls, reason)
		}
		if strings.HasPrefix(reason, "kế hoạch lớn:") {
			t.Errorf("(e) không được blocked kế hoạch lớn: %s", reason)
		}
	})

	t.Run("classic", func(t *testing.T) {
		h, tk, reason := run(t, config.VariantClassic, false)
		// (b)
		if c := tk.State.Classification; c == nil || c.Variant != "" {
			t.Errorf("(b) classic không được có variant: %+v", c)
		}
		// (c)
		p := h.prompts[StepReq]
		if strings.Contains(p, bprimeReqMarker) {
			t.Errorf("(c) đề bài classic không được có khối B′")
		}
		for _, hd := range headings {
			if !strings.Contains(p, hd) {
				t.Errorf("(c) đề bài classic phải chứa thân skill b-req đầy đủ (thiếu %q)", hd)
			}
		}
		// (d)
		if rep := task.LintPlanningTask(tk, true); len(rep.Errors) == 0 {
			t.Errorf("(d) bộ tài liệu gọn ở classic phải bị lint chặn")
		}
		// (e)
		if slices.Contains(h.calls, StepFeat) {
			t.Errorf("(e) classic: b-feat không được chạy với tài liệu gọn: calls=%v (%s)", h.calls, reason)
		}
	})
}
