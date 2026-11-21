package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Đợt 9 (docs/evals, bench v9): lượt B′ kẹt ở cổng lõi vì kiểm hẹp đỏ do test nhạy tải và test hết giờ ngoài vùng sửa, lượt sửa lõi chạy tới hết ngân
// sách rồi tác vụ kết thúc failed mà chưa review lần nào. Các ca dưới đây khoá ba sửa: phân loại đỏ của cổng lõi, trần của lượt sửa lõi, và lượt b-feat
// bị cắt theo ngân sách thì đi tiếp tới review.

// v9Run chạy b-do B′ trên module Go (gói a đã commit) với affected_command là go test thật; core sửa cây ở lượt b-feat đầu, các lượt sau ghi done.txt.
func v9Run(t *testing.T, affected string, core func(h *harness)) (*harness, *Result, int) {
	t.Helper()
	noEarlyStop(t)
	old := autoCheckQuiet
	autoCheckQuiet = 24 * time.Hour
	t.Cleanup(func() { autoCheckQuiet = old })
	feats := 0
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) },
		cfg: func(h *harness) {
			full := "test -f done.txt"
			h.cfg.Eval.TestCommand = &full
			h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeAffected, AffectedCommand: affected}
		},
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			feats++
			if feats == 1 {
				core(h)
			}
			h.write("app.txt", "v2\n") // thước đo thăm dò (roWriteProbe) đòi app.txt ghi v2
			h.write("done.txt", "ok\n")
			return nil
		}})
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	h.write(".gitignore", "*.mark\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	return h, res, feats
}

// v9NoCoreFix: thứ tự bước trước review khi không có lượt sửa lõi (lõi, thăm dò, mở rộng). Sau review có thể còn vòng sửa vì thước đo thăm dò giả
// (tệp .sh) chạy bằng affected_command là go test nên luôn đỏ; điều đó không thuộc phạm vi các ca này.
const v9NoCoreFix = "b-req,b-feat,b-goal,b-feat,b-review"

func TestBPrimeGateTriage(t *testing.T) {
	t.Run("test hay đỏ ở cổng lõi: không mở lượt sửa lõi", func(t *testing.T) {
		mark := filepath.ToSlash(filepath.Join(t.TempDir(), "flaky.mark"))
		h, res, feats := v9Run(t, "go test -count=1 {test_dirs}", func(h *harness) {
			h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
			h.write("a/flaky_test.go", "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestFlaky(t *testing.T) {\n"+
				"\tif _, err := os.Stat(\""+mark+"\"); err != nil {\n\t\tos.WriteFile(\""+mark+"\", nil, 0o644)\n\t\tt.Fatal(\"lần đầu đỏ\")\n\t}\n}\n")
		})
		tk := h.load(res.TaskID)
		g := lastGateCheck(tk)
		if g == nil || g.Passed || g.RedClass != redFlaky || len(g.RedTests) != 1 || !strings.Contains(g.RedTests[0], "TestFlaky") {
			t.Fatalf("cổng lõi phải đỏ và được phân loại flaky ở TestFlaky: %+v", g)
		}
		if tk.State.BPrimeCoreFix != 0 || hasNote(res.Notes, "lõi chưa xanh") {
			t.Errorf("không được mở lượt sửa lõi vì test hay đỏ: core_fix %d, notes %v", tk.State.BPrimeCoreFix, res.Notes)
		}
		if steps := roStepNames(res); !strings.HasPrefix(steps, v9NoCoreFix) {
			t.Errorf("trước review muốn %s (không có lượt sửa lõi), có %s; %d lượt b-feat", v9NoCoreFix, steps, feats)
		}
		if !hasNote(res.Notes, "không mở lượt sửa lõi") {
			t.Errorf("thiếu ghi chú phân loại: %v", res.Notes)
		}
	})

	t.Run("chỉ hết giờ ở cổng lõi: không chạy lại, không mở lượt sửa lõi", func(t *testing.T) {
		_, res, feats := v9Run(t, "go test -count=1 -timeout=1s {test_dirs}", func(h *harness) {
			h.write("a/slow_test.go", "package a\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestSlow(t *testing.T) { time.Sleep(20 * time.Second) }\n")
		})
		if steps := roStepNames(res); !strings.HasPrefix(steps, v9NoCoreFix) || hasNote(res.Notes, "lõi chưa xanh") {
			t.Errorf("hết giờ không được mở lượt sửa lõi: %s (%d lượt b-feat), notes %v", steps, feats, res.Notes)
		}
		if !hasNote(res.Notes, "timeout") {
			t.Errorf("thiếu ghi chú lớp timeout: %v", res.Notes)
		}
	})

	t.Run("đỏ do mã ở cổng lõi: vẫn mở lượt sửa lõi", func(t *testing.T) {
		h, res, feats := v9Run(t, "go test -count=1 {test_dirs}", func(h *harness) {
			h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
			h.write("a/new_test.go", "package a\n\nimport \"testing\"\n\nfunc TestNew(t *testing.T) {\n\tif A() != 3 {\n\t\tt.Fatal(\"A sai\")\n\t}\n}\n")
		})
		tk := h.load(res.TaskID)
		if tk.State.BPrimeCoreFix != 1 || feats < 3 || !hasNote(res.Notes, "lõi chưa xanh") {
			t.Errorf("đỏ do mã phải mở một lượt sửa lõi: core_fix %d, %d lượt, notes %v", tk.State.BPrimeCoreFix, feats, res.Notes)
		}
		var code bool
		for _, c := range tk.State.Checks {
			code = code || c.RedClass == redCode
		}
		if !code {
			t.Errorf("phải có lần cổng lõi lớp code: %+v", tk.State.Checks)
		}
	})
}

// Lượt b-feat bị rút trần theo workflow.budget_minutes mà chạm trần: không đánh failed, chạy cổng kiểm rồi vào review.
func TestBPrimeBudgetCutTurnContinues(t *testing.T) {
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(context.Context, *Env, string) error { return nil },
		cfg: func(h *harness) {
			h.cfg.Workflow.TimeoutMinutes, h.cfg.Workflow.BudgetMinutes, h.cfg.Workflow.ReviewReserveMinutes = 180, 60, 30
		},
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			return nil
		}})
	cut := 0
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && cut == 0 {
			cut++
			if req.Timeout >= 180*time.Minute {
				t.Errorf("lượt b-feat trước review phải bị rút trần theo ngân sách: %v", req.Timeout)
			}
			res.TimedOut, res.ExitCode, res.Error = true, -1, "quá giờ"
		}
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == task.StatusFailed {
		t.Fatalf("lượt bị cắt theo ngân sách không được làm tác vụ failed: %s; notes %v", res.Reason, res.Notes)
	}
	if !strings.Contains(roStepNames(res), "b-review") {
		t.Errorf("phải tới review: %s; notes %v", roStepNames(res), res.Notes)
	}
	if !hasNote(res.Notes, "thay vì đánh failed") {
		t.Errorf("thiếu ghi chú đi tiếp: %v", res.Notes)
	}
}

// Vòng sửa duy nhất sau review (FastFixed) bị trần ngân sách cắt: không failed; kiểm chứng lại trên cây đã sửa rồi finalize.
func TestBPrimeFixRoundCutContinues(t *testing.T) {
	feats := 0
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(context.Context, *Env, string) error { return nil },
		cfg: func(h *harness) {
			full := "test -f fixed.txt"
			h.cfg.Eval.TestCommand = &full
			h.cfg.Workflow.TimeoutMinutes, h.cfg.Workflow.BudgetMinutes, h.cfg.Workflow.SliceTimeoutMinutes = 180, 60, 30
		},
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			feats++
			h.write("done.txt", "ok\n")
			if feats > 1 {
				h.write("fixed.txt", "ok\n") // vòng sửa làm xong việc rồi mới bị cắt
			}
			return nil
		}})
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && feats == 2 {
			if req.Timeout >= 180*time.Minute {
				t.Errorf("vòng sửa phải có trần nhỏ hơn timeout_minutes: %v", req.Timeout)
			}
			res.TimedOut, res.ExitCode, res.Error = true, -1, "quá giờ"
		}
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	if feats != 2 {
		t.Fatalf("muốn 2 lượt b-feat (lõi, vòng sửa), có %d; bước %v; notes %v", feats, h.calls, res.Notes)
	}
	if res.Status != task.StatusDone {
		t.Fatalf("vòng sửa bị cắt mà cây đã xanh phải done: %s (%s); notes %v", res.Status, res.Reason, res.Notes)
	}
	if !hasNote(res.Notes, "thay vì đánh failed") {
		t.Errorf("thiếu ghi chú đi tiếp: %v", res.Notes)
	}
}

// Lượt b-feat chạm workflow.timeout_minutes (không bị ngân sách rút) vẫn failed như cũ.
func TestBPrimeConfigTimeoutStillFails(t *testing.T) {
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(context.Context, *Env, string) error { return nil },
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			h.write("done.txt", "ok\n")
			return nil
		}})
	h.cfg.Workflow.BudgetMinutes = 0
	h.mutateReq = func(_ eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat {
			res.TimedOut, res.ExitCode, res.Error = true, -1, "quá giờ"
		}
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusFailed {
		t.Fatalf("quá workflow.timeout_minutes phải failed: %s", res.Status)
	}
}

// Ngân sách trước review đã hết sau lượt lõi: không mở lượt sửa lõi dù cổng lõi đỏ, vào review.
func TestBPrimeBudgetSpentSkipsCoreFix(t *testing.T) {
	old := autoCheckQuiet
	autoCheckQuiet = 24 * time.Hour
	t.Cleanup(func() { autoCheckQuiet = old })
	clock := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	feats := 0
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) },
		cfg: func(h *harness) {
			full := "test -f done.txt"
			h.cfg.Eval.TestCommand = &full
			h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeAffected, AffectedCommand: "test ! -f red.txt"}
			h.cfg.Workflow.BudgetMinutes, h.cfg.Workflow.ReviewReserveMinutes = 120, 30
		},
		feat: func(_ context.Context, h *harness, _ *Env, _ string) error {
			feats++
			h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
			h.write("done.txt", "ok\n")
			if feats == 1 {
				h.write("red.txt", "x\n")
				clock = clock.Add(100 * time.Minute) // lượt lõi ăn hết 90 phút trước review
			} else {
				_ = os.Remove(filepath.Join(h.root, "red.txt"))
			}
			return nil
		}})
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	r := h.runner()
	r.Env.Now = func() time.Time { return clock }
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	steps := roStepNames(res)
	if !strings.HasPrefix(steps, "b-req,b-feat,b-review") {
		t.Errorf("hết ngân sách sau lõi phải vào thẳng review (không sửa lõi, không mở rộng): %s; notes %v", steps, res.Notes)
	}
	if !hasNote(res.Notes, "hết workflow.budget_minutes trước review") {
		t.Errorf("thiếu ghi chú hết ngân sách: %v", res.Notes)
	}
}

func TestCoreFixCap(t *testing.T) {
	h := newHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng", "").TaskID
	r := h.runner()
	r.Cfg.Workflow.TimeoutMinutes, r.Cfg.Workflow.BudgetMinutes, r.Cfg.Workflow.SliceTimeoutMinutes = 180, 0, 0
	tk, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		if t.State.Classification == nil {
			t.State.Classification = &task.Classification{}
		}
		t.State.Classification.Variant = task.VariantBPrime
		t.State.BPrimeCoreFix = 1
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := &runState{t: tk, res: &Result{}}
	if d := r.sliceTimeoutFor(rs, true); d != 180*time.Minute {
		t.Errorf("không có lát và ngân sách: trần sửa lõi = %v, muốn 180m", d)
	}
	r.Cfg.Workflow.SliceTimeoutMinutes = 25
	if d := r.sliceTimeoutFor(rs, true); d != 25*time.Minute || !r.bprimeSoftCap(rs, d) {
		t.Errorf("trần sửa lõi = %v (soft %v), muốn 25m theo slice_timeout_minutes", d, r.bprimeSoftCap(rs, d))
	}
	r.Cfg.Workflow.SliceTimeoutMinutes, r.Cfg.Workflow.BudgetMinutes = 0, 400
	if c := r.coreFixCap(); c != 100*time.Minute {
		t.Errorf("không đặt lát: trần sửa lõi = %v, muốn 1/4 ngân sách", c)
	}
	rs.t.State.BPrimePhase = bprimePhaseExtend
	r.Cfg.Workflow.BudgetMinutes, r.Cfg.Workflow.SliceTimeoutMinutes = 0, 25
	if d := r.sliceTimeoutFor(rs, true); d != 180*time.Minute {
		t.Errorf("lượt mở rộng không bị trần sửa lõi: %v", d)
	}
}
