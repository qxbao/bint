package workflow

import (
	"context"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/task"
)

// Đợt 7: lượt b-feat LÕI của B′ nối tiếp phiên b-req nên nhận planHandoffPrompt, mà đề bài đó thiếu khối "LÕI trước" và khối kiểm hẹp —
// lượt lõi 29 phút chạm 59 tệp và không gọi `bint task check` lần nào. Đề bài nối tiếp phải mang đủ các khối B′ như đề bài đầy đủ.
func TestBPrimeHandoffPromptCarriesCoreBlocks(t *testing.T) {
	procSetup(t)
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) }})
	h.claudeLike(nil)
	res, err := handoffRunner(h).Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	_, feat := planReqs(h)
	if feat.ResumeSessionID == "" {
		t.Fatalf("b-feat lõi phải nối tiếp phiên b-req (kiểm thử này đo đề bài nối tiếp): %+v notes=%v", feat, res.Notes)
	}
	for _, want := range []string{"LÕI trước", "kiểm hẹp theo mốc", "bint task check " + res.TaskID} {
		if !strings.Contains(feat.Prompt, want) {
			t.Errorf("đề bài nối tiếp của lượt lõi thiếu %q:\n%s", want, feat.Prompt)
		}
	}
	if strings.Contains(feat.Prompt, "lượt MỞ RỘNG (duy nhất)") {
		t.Errorf("lượt lõi không được nhận khối mở rộng:\n%s", feat.Prompt)
	}
}

// workflow.probe_confirm: sau khi thước đo thăm dò đóng băng, Bint dừng hỏi người còn thiếu loại đầu vào nào; câu trả lời đi vào đề bài lượt mở rộng.
func TestBPrimeProbeConfirmGate(t *testing.T) {
	ctx := context.Background()
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) },
		cfg: func(h *harness) { h.cfg.Workflow.ProbeConfirm = true }})
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	tk := loadTask(t, h, res.TaskID)
	if res.Status != task.StatusBlocked || !tk.State.AwaitingAnswer || tk.State.BPrimePhase != bprimePhaseExtend {
		t.Fatalf("phải dừng chờ người sau khi đóng băng thước đo: %s (%s), pha %q, bước %v", res.Status, res.Reason, tk.State.BPrimePhase, h.calls)
	}
	if !strings.Contains(res.Reason, probeMarker) || !strings.Contains(res.Reason, roProbeRel) {
		t.Errorf("câu hỏi phải liệt kê dòng `%s` của thước đo: %s", probeMarker, res.Reason)
	}
	res, err = h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, Answer: "thêm khoá PEM"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusDone {
		t.Fatalf("resume --answer phải chạy lượt mở rộng rồi review: %s (%s) bước %v", res.Status, res.Reason, h.calls)
	}
	ext, _, _ := bpReq(t, h, StepFeat, 1)
	if !strings.Contains(ext, "thêm khoá PEM") || !strings.Contains(ext, "lượt MỞ RỘNG (duy nhất)") {
		t.Errorf("đề bài lượt mở rộng phải mang câu trả lời của người:\n%s", ext)
	}
}
