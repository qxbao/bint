package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/flow"
	"bint/internal/task"
)

// Test của flow gốc b-debug: b-req (optional) → b-repro (implement, expect: fail) → b-fix → b-review (fix: b-fix).
// Lệnh kiểm chứng của harness là `test -f done.txt`: b-repro không ghi done.txt nên kiểm chứng hỏng (cổng đỏ qua),
// b-fix ghi done.txt nên kiểm chứng qua.

const debugRequest = "Sửa lỗi app.txt ghi sai phiên bản"

// debugBehave dựng hành vi agent giả. blockingRounds: số vòng review đầu có phát hiện nghiêm trọng. reproPasses: số lần
// b-repro đầu tiên viết thừa done.txt (kiểm chứng vẫn qua, test chưa tái hiện lỗi); lần sau xoá nó và hỏng đúng.
func (h *harness) debugBehave(blockingRounds, reproPasses int) func(context.Context, string, *Env, string) error {
	rounds, repros := 0, 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case "b-req":
			return h.writePlan(e, id, nil)
		case "b-repro":
			repros++
			if err := os.WriteFile(filepath.Join(e.Root, "repro_test.txt"), []byte("đỏ\n"), 0o644); err != nil {
				return err
			}
			done := filepath.Join(e.Root, "done.txt")
			if repros <= reproPasses {
				return os.WriteFile(done, []byte("ok\n"), 0o644)
			}
			if err := os.Remove(done); err != nil && !os.IsNotExist(err) {
				return err
			}
		case "b-fix":
			if err := os.WriteFile(filepath.Join(e.Root, "app.txt"), []byte("v2\n"), 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(e.Root, "done.txt"), []byte("ok\n"), 0o644)
		case "b-review":
			rounds++
			body := `{"summary":"ổn","findings":[]}`
			if rounds <= blockingRounds {
				body = `{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai","repro":"app.txt:1"}]}`
			}
			_, _, err := e.SubmitReview(id, []byte(body))
			return err
		}
		return nil
	}
}

func (h *harness) runDebug(o RunOptions) (*Result, error) {
	h.t.Helper()
	o.Workflow = "b-debug"
	return h.runner().Run(context.Background(), o)
}

// Chạy tới done bằng driver process; vòng sửa sau review quay về b-fix (không về b-repro); kiểm chứng sau b-repro hỏng.
func TestDebugFlowProcessToDone(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(1, 0)
	res, err := h.runDebug(RunOptions{Request: debugRequest, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run b-debug: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "b-req,b-repro,b-fix,b-review,b-fix,b-review"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
	tk := loadTask(t, h, res.TaskID)
	if tk.State.Workflow != "b-debug" || tk.State.FlowSHA != "" {
		t.Fatalf("phiếu: workflow=%q flow_sha=%q", tk.State.Workflow, tk.State.FlowSHA)
	}
	vs := tk.State.Verifications
	if len(vs) != 3 || vs[0].Passed || !vs[1].Passed || !vs[2].Passed {
		t.Fatalf("kiểm chứng: muốn hỏng (sau b-repro) rồi qua, qua; có %d lần: %+v", len(vs), vs)
	}
	// b-req và b-review chỉ đọc; hai bước implement ghi được.
	for i, step := range h.calls {
		wantRO := step == "b-req" || step == "b-review"
		if h.reqs[i].ReadOnly != wantRO {
			t.Errorf("bước %s: ReadOnly = %v, muốn %v", step, h.reqs[i].ReadOnly, wantRO)
		}
	}
}

// Cổng đỏ nhánh qua: sau b-repro kiểm chứng vẫn qua ⇒ blocked, nêu test chưa tái hiện lỗi; resume chạy lại b-repro
// (không sang b-fix) rồi đi tiếp tới done.
func TestDebugRedGateBlocksWhenReproPassesThenResume(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(0, 1)
	res, err := h.runDebug(RunOptions{Request: debugRequest, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chưa tái hiện lỗi") {
		t.Fatalf("muốn blocked vì test chưa tái hiện lỗi: %+v", res)
	}
	if got := strings.Join(h.calls, ","); got != "b-repro" {
		t.Fatalf("calls = %s, muốn chỉ b-repro", got)
	}
	tk := loadTask(t, h, res.TaskID)
	if tk.State.AwaitingCheck || tk.State.FlowStep != "b-repro" || len(tk.State.Verifications) != 1 || !tk.State.Verifications[0].Passed {
		t.Fatalf("trạng thái sau khi chặn: awaiting=%v flow_step=%q verifications=%+v", tk.State.AwaitingCheck, tk.State.FlowStep, tk.State.Verifications)
	}

	res, err = h.runDebug(RunOptions{TaskRef: res.TaskID, Resume: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "b-repro,b-repro,b-fix,b-review"; got != want {
		t.Fatalf("calls = %s, muốn %s", got, want)
	}
}

// Cổng đỏ nhánh hỏng: sang b-fix ngay, không chặn.
func TestDebugRedGateFailingVerifyContinues(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(0, 0)
	res, err := h.runDebug(RunOptions{Request: debugRequest, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "b-repro,b-fix,b-review"; got != want {
		t.Fatalf("calls = %s, muốn %s", got, want)
	}
}

// Trước b-repro: phiếu thiếu lệnh kiểm chứng, hoặc lệnh chưa được người xác nhận ⇒ blocked, không gọi agent.
func TestDebugPreflightBlocksBeforeAgent(t *testing.T) {
	t.Run("thiếu verify.command", func(t *testing.T) {
		h := newHarness(t)
		h.cfg.Eval.TestCommand = nil
		h.behave = h.debugBehave(0, 0)
		res, err := h.runDebug(RunOptions{Request: debugRequest, Plan: ptr(false)})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "verify-command") || len(h.calls) != 0 {
			t.Fatalf("muốn blocked trước khi gọi agent: %+v calls=%v", res, h.calls)
		}
	})
	t.Run("lệnh chưa xác nhận", func(t *testing.T) {
		h := newHarness(t)
		h.behave = h.debugBehave(0, 0)
		r := h.runner()
		r.Human = false // agent đề xuất lệnh: không tự xác nhận được
		tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: debugRequest, Workflow: "b-debug", Plan: ptr(false),
			Edit: TicketEdit{VerifyCommand: ptr("test -f done.txt && true")}})
		if err != nil {
			t.Fatal(err)
		}
		if r.VerifyConfirmed(tk) {
			t.Fatal("lệnh do agent đề xuất không được coi là đã xác nhận")
		}
		res, err := h.runner().Run(context.Background(), RunOptions{Workflow: "b-debug", TaskRef: tk.State.ID})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "bint task approve") || len(h.calls) != 0 {
			t.Fatalf("muốn blocked trước khi gọi agent: %+v calls=%v", res, h.calls)
		}
	})
}

// Driver session (bint run next): tới done, vòng sửa sau review quay về b-fix.
func TestDebugFlowSessionToDone(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(1, 0)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "b-debug", Request: debugRequest, Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if got, want := strings.Join(steps, ","), "b-repro,b-fix,b-review,b-fix,b-review"; got != want || last.Action != ActionDone {
		t.Fatalf("bước %v (muốn %s), dừng: %+v", steps, want, last)
	}
}

// Driver session: cổng đỏ nhánh qua chặn tác vụ, resume chạy lại b-repro.
func TestDebugFlowSessionRedGate(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(0, 1)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "b-debug", Request: debugRequest, Plan: ptr(false)})
	steps, last := h.drive(r, tk.State.ID, false)
	if strings.Join(steps, ",") != "b-repro" || last.Action != ActionBlocked || !strings.Contains(last.Reason, "chưa tái hiện lỗi") {
		t.Fatalf("bước %v, dừng: %+v", steps, last)
	}
	steps, last = h.drive(r, tk.State.ID, true)
	if got, want := strings.Join(steps, ","), "b-repro,b-fix,b-review"; got != want || last.Action != ActionDone {
		t.Fatalf("sau resume: bước %v (muốn %s), dừng: %+v", steps, want, last)
	}
}

// Phiếu b-debug chỉ chạy bằng flow của nó, và ngược lại.
func TestCheckFlowSwitchBoundDebug(t *testing.T) {
	mk := func(wf string) *task.Task { return &task.Task{State: task.State{ID: "T1", Workflow: wf}} }
	for _, c := range []struct {
		name, tw, run string
		wantErr       bool
	}{
		{"b-debug bằng b-debug", "b-debug", "b-debug", false},
		{"b-debug bằng b-do", "b-debug", "b-do", true},
		{"b-debug bằng b-feat", "b-debug", "b-feat", true},
		{"b-debug bằng b-review", "b-debug", "b-review", true},
		{"b-debug bằng b-req", "b-debug", "b-req", true},
		{"b-do bằng b-debug", "b-do", "b-debug", true},
		{"b-feat bằng b-debug", "b-feat", "b-debug", true},
		{"phiếu chỉ lập kế hoạch bằng b-debug", "b-req", "b-debug", false},
		{"b-do bằng b-feat (giữ hành vi cũ)", "b-do", "b-feat", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkFlowSwitch(mk(c.tw), c.run)
			if (err != nil) != c.wantErr {
				t.Fatalf("checkFlowSwitch(%s, %s) = %v, muốn lỗi=%v", c.tw, c.run, err, c.wantErr)
			}
		})
	}
}

// Chạy phiếu b-debug bằng b-do/b-feat/b-review bị từ chối; --step đúng bước kế tiếp thì chạy từng bước tới done.
func TestDebugRunStepAndWorkflowGuards(t *testing.T) {
	h := newHarness(t)
	h.behave = h.debugBehave(0, 0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: debugRequest, Workflow: "b-debug", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	for _, wf := range []string{"b-do", "b-feat", "b-review"} {
		if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: wf, TaskRef: id}); err == nil || !strings.Contains(err.Error(), "thuộc flow b-debug") {
			t.Errorf("run %s --task trên phiếu b-debug: lỗi = %v", wf, err)
		}
	}
	if _, err := h.runDebug(RunOptions{TaskRef: id, Step: "b-fix"}); err == nil || !strings.Contains(err.Error(), "bước kế tiếp") {
		t.Fatalf("--step b-fix khi bước kế tiếp là b-repro: lỗi = %v", err)
	}
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Step: "b-repro"}); err == nil {
		t.Fatal("--step với workflow gốc b-do phải bị từ chối")
	}
	for _, step := range []string{"b-repro", "b-fix", "b-review"} {
		res, err := h.runDebug(RunOptions{TaskRef: id, Step: step})
		if err != nil {
			t.Fatalf("--step %s: %v", step, err)
		}
		if len(res.Steps) != 1 || res.Steps[0].Step != step {
			t.Fatalf("--step %s chạy %+v", step, res.Steps)
		}
	}
	if got := loadTask(t, h, id).State.Status; got != task.StatusDone {
		t.Fatalf("sau ba bước --step: %s, muốn done", got)
	}
}

// processCommand: bốn workflow gốc ban đầu chạy theo tên bước; b-debug và flow tuỳ biến dùng --step.
func TestProcessCommandDebug(t *testing.T) {
	do, _ := flow.GetBuiltin("b-do")
	dbg, _ := flow.GetBuiltin("b-debug")
	if got := processCommand(do, "b-feat", "T1", false); got != "bint run b-feat --task T1 --json" {
		t.Errorf("b-do: %q", got)
	}
	if got := processCommand(dbg, "b-fix", "T1", false); got != "bint run b-debug --task T1 --step b-fix --json" {
		t.Errorf("b-debug: %q", got)
	}
	if got := processCommand(tddDef(t), "green", "T1", false); got != "bint run tdd --task T1 --step green --json" {
		t.Errorf("tdd: %q", got)
	}
	if got := processCommand(dbg, "b-fix", "T1", true); got != "bint run b-debug --task T1 --step b-fix --fresh --json" {
		t.Errorf("b-debug --fresh: %q", got)
	}
}
