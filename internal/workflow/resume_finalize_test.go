package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// Finalize bị chặn vì lần kiểm chứng cuối không qua (sau khi review đã duyệt): resume khi worktree không đổi không mở
// b-review mới mà chạy lại lệnh kiểm chứng đúng một lần rồi áp cổng done. Worktree đổi hoặc --rerun thì review lại.

// flagCommand dựng lệnh kiểm chứng hỏng khi tệp cờ (ngoài worktree) tồn tại: mô phỏng sự cố ngoài mã.
func (h *harness) flagCommand() (cmd, flag string) {
	flag = filepath.ToSlash(filepath.Join(h.t.TempDir(), "outage"))
	cmd = "test -f done.txt && ! test -f " + flag
	h.cfg.Eval.TestCommand = &cmd
	return cmd, flag
}

func setFlag(t *testing.T, flag string, on bool) {
	t.Helper()
	if on {
		if err := os.WriteFile(flag, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	os.Remove(flag)
}

func stepNames(res *Result) []string {
	var out []string
	for _, s := range res.Steps {
		out = append(out, s.Step)
	}
	return out
}

func TestProcessResumeAfterFinalizeBlockSkipsReview(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, flag := h.flagCommand()
	id := h.finishFeat()
	setFlag(t, flag, true)
	if _, vr, err := h.runner().VerifyWith(ctx, id, "sự cố ngoài mã"); err != nil || vr.Verification.Passed {
		t.Fatalf("cần một lần kiểm chứng hỏng: %+v %v", vr, err)
	}
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "lần kiểm chứng cuối không qua") || !slices.Contains(stepNames(res), StepReview) {
		t.Fatalf("lượt đầu phải review rồi chặn ở finalize: %+v steps=%v", res, stepNames(res))
	}
	if h.load(id).State.FinalizeFP == "" {
		t.Fatal("finalize bị chặn phải nhớ vân tay worktree")
	}
	nv := len(h.load(id).State.Verifications)
	setFlag(t, flag, false)

	res = h.resumeDo(id)
	if res.Status != task.StatusDone || slices.Contains(stepNames(res), StepReview) {
		t.Fatalf("resume phải done mà không review lại: %+v steps=%v", res, stepNames(res))
	}
	got := h.load(id)
	if len(got.State.Verifications) != nv+1 || !got.State.LastVerification().Passed ||
		!strings.Contains(got.State.LastVerification().Reason, "finalize bị chặn") {
		t.Fatalf("đúng một lần kiểm chứng lại: %+v", got.State.Verifications)
	}
	if got.State.FinalizeFP != "" {
		t.Fatalf("kiểm chứng chạy lại xong phải xoá dấu vân tay: %q", got.State.FinalizeFP)
	}
}

// FinalizeFP bị xoá khi tác vụ bị chặn lại vì lý do KHÁC lần kiểm chứng hỏng (ở đây: tiêu chí bị đánh dấu chưa đạt), để
// lần resume sau không bỏ review nhầm; còn kiểm chứng lại vẫn hỏng thì dấu vân tay được đặt lại.
func TestFinalizeFPClearedWhenBlockedForOtherReason(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, flag := h.flagCommand()
	id := h.finishFeat()
	setFlag(t, flag, true)
	h.runner().VerifyWith(ctx, id, "")
	if res := h.resumeDo(id); res.Status != task.StatusBlocked || h.load(id).State.FinalizeFP == "" {
		t.Fatalf("lượt đầu phải chặn và nhớ vân tay: %+v", res)
	}
	// Kiểm chứng lại vẫn hỏng: chặn lại, dấu vân tay còn (worktree không đổi).
	if res := h.resumeDo(id); res.Status != task.StatusBlocked || slices.Contains(stepNames(res), StepReview) || h.load(id).State.FinalizeFP == "" {
		t.Fatalf("resume lần hai vẫn phải chặn không review, còn dấu vân tay: %+v", res)
	}
	// Kiểm chứng qua nhưng tiêu chí chưa đạt: chặn vì lý do khác, dấu vân tay bị xoá.
	setFlag(t, flag, false)
	setCriteriaRaw(t, h.runner(), id, "app.txt ghi v2")
	if _, err := h.runner().CheckCriterion(id, 1, false, "chưa xong"); err != nil {
		t.Fatal(err)
	}
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "tiêu chí 1 chưa đạt") {
		t.Fatalf("phải chặn vì tiêu chí: %+v", res)
	}
	if fp := h.load(id).State.FinalizeFP; fp != "" {
		t.Fatalf("chặn vì lý do khác phải xoá dấu vân tay: %q", fp)
	}
}

func TestProcessResumeReviewsAgainWhenWorktreeChangedOrRerun(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []string{"worktree đổi", "rerun"} {
		t.Run(tc, func(t *testing.T) {
			h := newHarness(t)
			_, flag := h.flagCommand()
			id := h.finishFeat()
			setFlag(t, flag, true)
			h.runner().VerifyWith(ctx, id, "")
			if res := h.resumeDo(id); res.Status != task.StatusBlocked {
				t.Fatalf("%+v", res)
			}
			setFlag(t, flag, false)
			o := RunOptions{Workflow: StepDo, TaskRef: id, Resume: true}
			if tc == "rerun" {
				o.Rerun = true
			} else {
				h.write("docs/other.md", "của người khác\n")
			}
			res, err := h.runner().Run(ctx, o)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(stepNames(res), StepReview) {
				t.Fatalf("phải review lại: %+v steps=%v", res, stepNames(res))
			}
		})
	}
}

func TestSessionResumeAfterFinalizeBlock(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*harness, *Runner, string, string) {
		h := newHarness(t)
		_, flag := h.flagCommand()
		r := h.runner()
		id := h.newSession(r, NewTaskOptions{}).State.ID
		for _, want := range []string{StepReq, StepFeat} {
			act, err := r.Next(ctx, NextOptions{TaskRef: id})
			if err != nil || act.Step != want {
				t.Fatalf("next: %+v %v", act, err)
			}
			h.subagent(act)
			if _, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "a-" + want}); err != nil {
				t.Fatal(err)
			}
		}
		setFlag(t, flag, true)
		if _, vr, err := r.VerifyWith(ctx, id, "sự cố ngoài mã"); err != nil || vr.Verification.Passed {
			t.Fatalf("cần một lần kiểm chứng hỏng: %+v %v", vr, err)
		}
		steps, last := h.drive(r, id, false)
		if last.Action != ActionBlocked || !slices.Contains(steps, StepReview) {
			t.Fatalf("phải review rồi chặn ở finalize: steps=%v %+v", steps, last)
		}
		setFlag(t, flag, false)
		return h, r, id, flag
	}
	t.Run("không đổi: không review lại", func(t *testing.T) {
		h, r, id, _ := setup(t)
		nv := len(loadTask(t, h, id).State.Verifications)
		steps, last := h.drive(r, id, true)
		if last.Action != ActionDone || len(steps) != 0 {
			t.Fatalf("resume phải done không review: steps=%v %+v", steps, last)
		}
		got := loadTask(t, h, id)
		if len(got.State.Verifications) != nv+1 || !got.State.LastVerification().Passed {
			t.Fatalf("đúng một lần kiểm chứng lại: %+v", got.State.Verifications)
		}
		if got.State.FinalizeFP != "" {
			t.Fatalf("kiểm chứng chạy lại xong phải xoá dấu vân tay: %q", got.State.FinalizeFP)
		}
	})
	t.Run("worktree đổi: review lại", func(t *testing.T) {
		h, r, id, _ := setup(t)
		h.write("docs/other.md", "của người khác\n")
		steps, _ := h.drive(r, id, true)
		if !slices.Contains(steps, StepReview) {
			t.Fatalf("phải review lại: %v", steps)
		}
	})
	t.Run("rerun: review lại", func(t *testing.T) {
		h, r, id, _ := setup(t)
		act, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true, Rerun: true})
		if err != nil || act.Action != ActionAgent || act.Step != StepReview {
			t.Fatalf("--rerun phải mở b-review: %+v %v", act, err)
		}
		_ = h
	})
}

// Env.VerifyWith chạy lệnh kiểm chứng với môi trường đã bỏ biến định danh theo lượt (BINT_*, CLAUDE_CODE_*, GIT_*), giữ
// biến đánh dấu agent. Biến GIT_* chỉ được đưa vào môi trường nguồn của lệnh kiểm chứng (verifyEnviron), không vào tiến
// trình kiểm thử: đặt thật sẽ làm hỏng git của harness.
func TestVerifyWithEnvironment(t *testing.T) {
	stripped := []string{"BINT_RUN_ID", "BINT_TASK_ID", "BINT_APPROVED_PATHS", "BINT_RUNNER_PID", "CLAUDE_CODE_SESSION_ID",
		"CLAUDE_CODE_SSE_PORT", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"}
	kept := []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "GEMINI_CLI"}
	h := newHarness(t)
	old := verifyEnviron
	verifyEnviron = func() []string {
		env := old()
		for _, k := range stripped {
			env = append(env, k+"=leak")
		}
		for _, k := range kept {
			env = append(env, k+"=1")
		}
		return env
	}
	t.Cleanup(func() { verifyEnviron = old })
	var parts []string
	for _, k := range stripped {
		parts = append(parts, `test -z "$`+k+`"`)
	}
	for _, k := range kept {
		parts = append(parts, `test "$`+k+`" = 1`)
	}
	cmd := strings.Join(parts, " && ")
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat() // finishFeat chạy lệnh kiểm chứng ở b-feat
	if v := h.load(id).State.LastVerification(); v == nil || !v.Passed {
		t.Fatalf("tiến trình con của lệnh kiểm chứng thấy biến sai: %+v", v)
	}
}
