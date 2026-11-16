package workflow

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/task"
)

// failFeat đặt handle phiên cho mọi bước và chỉnh kết quả của b-feat.
func (h *harness) failFeat(sess string, f func(res *eval.AgentResult)) {
	h.mutate = func(step string, res *eval.AgentResult) {
		res.SessionID = sess
		if step == StepFeat {
			f(res)
		}
	}
}

func runDo(t *testing.T, h *harness) (*Result, *task.Task) {
	t.Helper()
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	return res, loadTask(t, h, res.TaskID)
}

func TestLastStepRecordsStopClass(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mut         func(res *eval.AgentResult)
		class       string
		continuable bool
	}{
		{"hạn mức", func(r *eval.AgentResult) {
			r.ExitCode, r.Error, r.Infra, r.QuotaHit = 1, "429", "hết hạn mức", true
		}, StopExternal, true},
		{"API 5xx", func(r *eval.AgentResult) { r.ExitCode, r.Error, r.Infra = 1, "api", "lỗi API (mã 503)" }, StopExternal, true},
		{"không có kết quả", func(r *eval.AgentResult) { r.ExitCode, r.Error, r.NoResult = 1, "chết", true }, StopExternal, true},
		{"quá giờ", func(r *eval.AgentResult) { r.TimedOut, r.ExitCode, r.Error = true, -1, "" }, StopTimeout, true},
		{"lỗi nội tại", func(r *eval.AgentResult) { r.ExitCode, r.Error = 1, "test đỏ" }, StopIntrinsic, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.failFeat("sess-f", tc.mut)
			res, tk := runDo(t, h)
			ls := tk.State.LastStep
			if ls == nil || ls.Step != StepFeat || ls.StopClass != tc.class || ls.Continuable != tc.continuable || ls.SessionID != "sess-f" ||
				ls.Fingerprint == "" || ls.RunID == "" || ls.Harness != "fake" {
				t.Fatalf("LastStep: %+v", ls)
			}
			var feat *StepResult
			for i := range res.Steps {
				if res.Steps[i].Step == StepFeat {
					feat = &res.Steps[i]
				}
			}
			if feat == nil || feat.StopClass != tc.class || feat.Continuable != tc.continuable || feat.SessionID != "sess-f" {
				t.Fatalf("StepResult: %+v", feat)
			}
		})
	}
}

func TestLastStepInterruptedAndCleared(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.mutate = func(step string, res *eval.AgentResult) {
		res.SessionID = "sess-i"
		if step == StepFeat {
			cancel()
			res.ExitCode, res.Error = -1, "context canceled"
		}
	}
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	ls := loadTask(t, h, res.TaskID).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || !ls.Continuable || ls.SessionID != "sess-i" {
		t.Fatalf("huỷ: %+v", ls)
	}
	// Resume hoàn thành: bước mới bắt đầu xoá LastStep cũ, bước hoàn thành không ghi LastStep mới.
	h.mutate = nil
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, res.TaskID); got.State.LastStep != nil {
		t.Fatalf("LastStep phải bị xoá khi bước mới bắt đầu: %+v", got.State.LastStep)
	}
}

func TestLastStepAwaitingHumanOpenQuestion(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reqWithQuestion(openQ)
	h.mutate = func(step string, res *eval.AgentResult) { res.SessionID = "sess-r" }
	res, tk := runDo(t, h)
	ls := tk.State.LastStep
	if tk.State.Status != task.StatusBlocked || ls == nil || ls.Step != StepReq || ls.StopClass != StopAwaitingHuman || !ls.Continuable || ls.SessionID != "sess-r" {
		t.Fatalf("câu hỏi mở: %+v", ls)
	}
	// Resume khi chưa trả lời: không có bước nào chạy, LastStep giữ nguyên.
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, res.TaskID).State.LastStep; got == nil || got.RunID != ls.RunID || got.StopClass != StopAwaitingHuman {
		t.Fatalf("LastStep phải còn: %+v", got)
	}
	// Trả lời rồi resume: b-feat bắt đầu xoá LastStep.
	if _, _, err := h.runner().WriteArtifact(res.TaskID, "requirement", validDocs("app.txt ghi v2", answeredQ)["requirement"]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, res.TaskID).State.LastStep; got != nil && got.StopClass == StopAwaitingHuman {
		t.Fatalf("LastStep awaiting_human phải bị xoá: %+v", got)
	}
}

func TestLastStepProtectedPathAwaitingHuman(t *testing.T) {
	h := newHarness(t)
	h.denyNext = []string{"migrations/001.sql"}
	h.mutate = func(step string, res *eval.AgentResult) { res.SessionID = "sess-p" }
	_, tk := runDo(t, h)
	ls := tk.State.LastStep
	if tk.State.Status != task.StatusBlocked || ls == nil || ls.Step != StepFeat || ls.StopClass != StopAwaitingHuman || ls.SessionID != "sess-p" || !ls.Continuable {
		t.Fatalf("đường dẫn bảo vệ: status=%s %+v", tk.State.Status, ls)
	}
}

func TestLastStepSessionDriver(t *testing.T) {
	h := newHarness(t)
	advance := fakeLeaseClock(t)
	r := h.runner()
	ctx := context.Background()

	// Hết hạn thuê, đóng bằng next: không có agent_id, không nối tiếp được.
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	advance(time.Duration(h.cfg.Workflow.TimeoutMinutes+1) * time.Minute)
	if _, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	ls := loadTask(t, h, tk.State.ID).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || ls.Continuable || ls.SessionID != "" || ls.Step != StepFeat {
		t.Fatalf("hết hạn thuê: %+v", ls)
	}

	// Agent báo thất bại kèm agent_id: lớp intrinsic, handle là agent_id, không nối tiếp.
	tk2 := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a2, err := r.Next(ctx, NextOptions{TaskRef: tk2.State.ID})
	if err != nil || a2.Action != ActionAgent {
		t.Fatal(a2, err)
	}
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: tk2.State.ID, RunID: a2.RunID, AgentID: "ag-1", Failed: "hết lượt"}); err != nil {
		t.Fatal(err)
	}
	ls = loadTask(t, h, tk2.State.ID).State.LastStep
	if ls == nil || ls.StopClass != StopIntrinsic || ls.SessionID != "ag-1" || ls.Continuable {
		t.Fatalf("agent báo thất bại: %+v", ls)
	}
}

func TestLastStepSessionLeaseExpiredWithAgentID(t *testing.T) {
	h := newHarness(t)
	advance := fakeLeaseClock(t)
	r := h.runner()
	ctx := context.Background()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	advance(time.Duration(h.cfg.Workflow.TimeoutMinutes+1) * time.Minute)
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "ag-2"}); err != nil {
		t.Fatal(err)
	}
	ls := loadTask(t, h, tk.State.ID).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || ls.SessionID != "ag-2" || !ls.Continuable {
		t.Fatalf("hết hạn thuê có agent_id: %+v", ls)
	}
}

func TestLastStepSessionAbort(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	ctx := context.Background()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	if _, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Abort(ctx, tk.State.ID, "orchestrator chết"); err != nil {
		t.Fatal(err)
	}
	ls := loadTask(t, h, tk.State.ID).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || ls.Continuable || ls.SessionID != "" {
		t.Fatalf("abort: %+v", ls)
	}
}

func TestHappyRunHasNoLastStep(t *testing.T) {
	h := newHarness(t)
	_, tk := runDo(t, h)
	if tk.State.LastStep != nil {
		t.Fatalf("tác vụ chạy thành công không có LastStep: %+v", tk.State.LastStep)
	}
}

// Driver process: bước harness claude nhận AgentRequest.SessionID là UUID do Bint cấp, và khi bước dừng không có JSON kết
// quả (NoResult) thì LastStep.SessionID chính là handle đó.
func TestProcessClaudeStepGetsSessionIDAndRecordsHandle(t *testing.T) {
	h := newHarness(t)
	h.agentName = "claude"
	h.mutate = func(step string, res *eval.AgentResult) {
		if step == StepFeat {
			res.ExitCode, res.Error, res.NoResult, res.SessionID = 1, "chết trước khi in JSON", true, ""
		}
	}
	_, tk := runDo(t, h)
	var got string
	for i, c := range h.calls {
		if c == StepFeat {
			got = h.reqs[i].SessionID
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(got) {
		t.Fatalf("AgentRequest.SessionID = %q, muốn UUID v4", got)
	}
	ls := tk.State.LastStep
	if ls == nil || ls.Step != StepFeat || ls.StopClass != StopExternal || ls.SessionID != got {
		t.Fatalf("LastStep.SessionID phải là %q: %+v", got, ls)
	}
}

// LastStep chỉ bị xoá khi bước thật sự bắt đầu: grantPending lỗi (tệp hợp đồng thuê hỏng) thì phiếu còn nguyên LastStep;
// grantPending thành công xoá nó cùng lần ghi PendingStep.
func TestGrantPendingKeepsLastStepOnFailure(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	failFeatSession(t, h, r, id, "A1")
	before := loadTask(t, h, id)
	if before.State.LastStep == nil {
		t.Fatal("thiếu LastStep sau bước thất bại")
	}
	ps := &task.PendingStep{RunID: "run-x", Step: StepFeat, Mode: leaseModeName(LockExclusive), StartedAt: "2026-01-01T00:00:00Z",
		LeaseUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	lockFile := lockPath(r.Tasks.Dir, r.Root)
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaseFilePath(lockFile), []byte("{hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	rs := &runState{t: before}
	if err := r.grantPending(rs, &stepRun{}, ps); err == nil {
		t.Fatal("grantPending phải lỗi khi tệp hợp đồng hỏng")
	}
	if got := loadTask(t, h, id); got.State.LastStep == nil || got.State.PendingStep != nil {
		t.Fatalf("grantPending lỗi phải giữ LastStep và không ghi PendingStep: %+v / %+v", got.State.LastStep, got.State.PendingStep)
	}
	// Gỡ tệp hỏng: cấp được, LastStep bị xoá cùng lần ghi PendingStep.
	if err := os.Remove(leaseFilePath(lockFile)); err != nil {
		t.Fatal(err)
	}
	if err := r.grantPending(&runState{t: before}, &stepRun{}, ps); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, id); got.State.LastStep != nil || got.State.PendingStep == nil || got.State.PendingStep.RunID != "run-x" {
		t.Fatalf("grantPending thành công phải xoá LastStep: %+v / %+v", got.State.LastStep, got.State.PendingStep)
	}
}
