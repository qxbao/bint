package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/task"
)

// noEarlyStop tắt dừng sớm của B′ cho các ca đo nhánh có lượt mở rộng (thăm dò giả của chúng đã xanh ngay trên lõi).
func noEarlyStop(t *testing.T) {
	t.Helper()
	old := bprimeEarlyStop
	bprimeEarlyStop = false
	t.Cleanup(func() { bprimeEarlyStop = old })
}

// Đợt 8: B′ đạt đủ test ẩn ở phút 60 nhưng vẫn chạy lượt mở rộng, review rồi vòng sửa tới phút 134. Thước đo vừa đóng băng đã xanh trên lõi
// và kiểm chứng không đỏ thì bỏ lượt mở rộng, kiểm chứng lại một lần trên cây đã gộp thăm dò, rồi vào review.
func TestBPrimeEarlyStopSkipsExtension(t *testing.T) {
	featN := 0
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) },
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			featN++
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			return nil
		}})
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	tk := loadTask(t, h, res.TaskID)
	if res.Status != task.StatusDone {
		t.Fatalf("trạng thái = %s (%s), muốn done; bước %v; ghi chú %v", res.Status, res.Reason, h.calls, res.Notes)
	}
	if got := roStepNames(res); got != "b-req,b-feat,b-goal,b-review" {
		t.Errorf("thứ tự bước = %s, muốn b-req,b-feat,b-goal,b-review (không có lượt mở rộng)", got)
	}
	if featN != 1 {
		t.Errorf("b-feat chạy %d lần, muốn 1", featN)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "bỏ lượt mở rộng") {
		t.Errorf("ghi chú phải nói bỏ lượt mở rộng: %v", res.Notes)
	}
	if tk.State.GoalTest == nil || tk.State.BPrimePhase != bprimePhaseDone {
		t.Errorf("thước đo phải đóng băng và pha done: %+v pha %q", tk.State.GoalTest, tk.State.BPrimePhase)
	}
	// Review vẫn soát độ phủ của thước đo (thước đo xanh quá dễ phải bị bắt ở đây).
	if rp := h.prompts[StepReview]; !strings.Contains(rp, "SOÁT ĐỘ PHỦ CỦA THƯỚC ĐO") {
		t.Errorf("review sau dừng sớm phải có khối soát độ phủ")
	}
}

// Thăm dò đỏ trên lõi thì vẫn có lượt mở rộng (dừng sớm chỉ khi thước đo đã xanh).
func TestBPrimeEarlyStopNotWhenProbeRed(t *testing.T) {
	featN := 0
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime,
		goal: func(_ context.Context, e *Env, _ string) error {
			if err := roWriteProbe(e); err != nil {
				return err
			}
			// Thăm dò đòi ext.txt, chỉ lượt mở rộng mới tạo. Cổng mục tiêu của harness chạy `sh {test_files}` nên chỉ tệp đầu (helper.sh) được thực thi.
			return os.WriteFile(filepath.Join(e.Root, filepath.FromSlash(roHelperRel)), []byte("test -f ext.txt\n"), 0o644)
		},
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			featN++
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			if featN >= 2 {
				h.write("ext.txt", "ok\n")
			}
			return nil
		}})
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(res.Notes, "\n"), "bỏ lượt mở rộng") {
		t.Errorf("thước đo đỏ trên lõi mà vẫn bỏ lượt mở rộng: %v", res.Notes)
	}
	if featN < 2 {
		t.Errorf("b-feat chạy %d lần, muốn ≥ 2 (lõi + mở rộng); bước %v", featN, h.calls)
	}
}

// Đợt 8: vòng sửa sau review bị cắt ở trần lát 30 phút khi ngân sách tổng còn ~35 phút, và lý do dừng ghi "quá 180 phút".
func TestBPrimeFixRoundUsesRemainingBudget(t *testing.T) {
	h := newHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng", "").TaskID
	r := h.runner()
	clock := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	r.Env.Now = func() time.Time { return clock }
	r.Cfg.Workflow.BudgetMinutes, r.Cfg.Workflow.SliceTimeoutMinutes, r.Cfg.Workflow.TimeoutMinutes = 170, 30, 180
	tk, err := r.Tasks.Update(id, clock, func(t *task.Task) error {
		if t.State.Classification == nil {
			t.State.Classification = &task.Classification{}
		}
		t.State.Classification.Variant = task.VariantBPrime
		t.State.BudgetStart = task.Now(clock)
		t.State.FastFixed = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := &runState{t: tk, res: &Result{}}
	clock = clock.Add(135 * time.Minute)
	if d := r.sliceTimeoutFor(rs, true); d != 35*time.Minute {
		t.Errorf("vòng sửa sau review: trần = %v, muốn 35m (phần còn của 170 phút)", d)
	}
	// Ngân sách tổng gần hết: vẫn giữ trần lát (không rút xuống dưới nó).
	clock = clock.Add(30 * time.Minute)
	if d := r.sliceTimeoutFor(rs, true); d != 30*time.Minute {
		t.Errorf("ngân sách còn 5 phút: trần = %v, muốn trần lát 30m", d)
	}
	sr := &stepRun{timeout: 35 * time.Minute}
	if s := r.timeoutReason(sr); !strings.Contains(s, "35m0s") || strings.Contains(s, "quá workflow.timeout_minutes (180") {
		t.Errorf("lý do dừng phải nêu trần thật 35 phút: %q", s)
	}
	if s := r.timeoutReason(&stepRun{timeout: 180 * time.Minute}); !strings.Contains(s, "workflow.timeout_minutes (180 phút)") {
		t.Errorf("trần bằng timeout_minutes thì giữ lời cũ: %q", s)
	}
}
