package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Cổng theo pha của B′ (docs/bprime-budget-plan.md mục 1.2): sau lượt LÕI/SỬA LÕI, cổng chặn luồng là kiểm hẹp (State.Checks, lý do checkGateReason)
// thay cho kiểm chứng rộng; kiểm rộng chạy một lần ở lượt mở rộng, hoặc trước review khi không có lượt mở rộng.

type gateRec struct {
	mu    sync.Mutex
	turns []gateTurn // trạng thái phiếu lúc mỗi lượt b-feat bắt đầu
}

type gateTurn struct {
	verifs, gates int
	lastGate      *task.Verification
}

// gateRun chạy b-do B′ trên module Go (gói a, c đã commit). verify.command ghi một dòng vào count.txt mỗi lần chạy đầy đủ; affected_command đỏ khi
// red.txt tồn tại. probe: thăm dò để lại tệp có dòng `Loại đầu vào:` (có lượt mở rộng) hay không. turns[i] sửa cây ở lượt b-feat thứ i.
func gateRun(t *testing.T, probe bool, turns []func(h *harness)) (*harness, *Result, *gateRec, string) {
	t.Helper()
	old := autoCheckQuiet
	autoCheckQuiet = 24 * time.Hour // chỉ đo cổng; kiểm hẹp tự động có test riêng
	t.Cleanup(func() { autoCheckQuiet = old })
	counter := filepath.ToSlash(filepath.Join(t.TempDir(), "count.txt"))
	rec := &gateRec{}
	goal := func(context.Context, *Env, string) error { return nil }
	if probe {
		goal = func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) }
	}
	h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: goal,
		cfg: func(h *harness) {
			full := "test -f done.txt && test ! -f red.txt && echo run >> " + counter
			h.cfg.Eval.TestCommand = &full
			h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeAffected,
				AffectedCommand: "echo AFFECTED {test_dirs} >> aff.txt; test ! -f red.txt"}
		},
		feat: func(_ context.Context, h *harness, _ *Env, id string) error {
			tk := h.load(id)
			gates := 0
			for _, c := range tk.State.Checks {
				if strings.HasPrefix(c.Reason, checkGateReason) {
					gates++
				}
			}
			rec.mu.Lock()
			n := len(rec.turns)
			rec.turns = append(rec.turns, gateTurn{verifs: len(tk.State.Verifications), gates: gates, lastGate: lastGateCheck(tk)})
			rec.mu.Unlock()
			if n < len(turns) {
				turns[n](h)
			}
			return nil
		}})
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	h.write("c/c.go", "package c\n\nfunc C() {}\n")
	h.write("c/c_test.go", "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n")
	h.write(".gitignore", "aff.txt\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueGoalReq})
	if err != nil {
		t.Fatal(err)
	}
	return h, res, rec, counter
}

func gateEditA(v string, red bool) func(h *harness) {
	return func(h *harness) {
		h.write("a/a.go", "package a\n\nfunc A() int { return "+v+" }\n")
		h.write("done.txt", "ok\n")
		if red {
			h.write("red.txt", "x\n")
		} else {
			_ = os.Remove(filepath.Join(h.root, "red.txt"))
		}
	}
}

func TestBPrimeCoreGate(t *testing.T) {
	// Lõi xanh có lượt mở rộng: lõi chỉ qua kiểm hẹp, không có kiểm chứng rộng nào trước lượt mở rộng; lượt mở rộng chạy kiểm rộng một lần.
	t.Run("lõi xanh, có lượt mở rộng", func(t *testing.T) {
		h, res, rec, _ := gateRun(t, true, []func(*harness){gateEditA("2", false), gateEditA("3", false)})
		if len(rec.turns) != 2 {
			t.Fatalf("muốn 2 lượt b-feat (lõi, mở rộng), có %d; notes %v", len(rec.turns), res.Notes)
		}
		ext := rec.turns[1]
		if ext.verifs != 0 {
			t.Errorf("lượt mở rộng bắt đầu khi đã có %d lần kiểm chứng rộng; muốn 0 (lõi chỉ qua cổng hẹp)", ext.verifs)
		}
		if ext.gates != 1 || ext.lastGate == nil || !ext.lastGate.Passed || ext.lastGate.By != "bint" || ext.lastGate.Ran == "" {
			t.Errorf("muốn đúng một lần kiểm hẹp cổng lõi xanh do bint chạy trước lượt mở rộng: %d lần, %+v", ext.gates, ext.lastGate)
		}
		if ext.lastGate != nil && !strings.Contains(ext.lastGate.Ran, "./a") {
			t.Errorf("kiểm hẹp cổng lõi phải chạy test của gói đổi (./a): %q", ext.lastGate.Ran)
		}
		tk := h.load(res.TaskID)
		if len(tk.State.Verifications) == 0 {
			t.Errorf("không có kiểm chứng rộng nào sau lượt mở rộng")
		}
	})

	// Lõi đỏ ở cổng hẹp: lượt sửa lõi được mở bởi lần kiểm hẹp đỏ (không cần kiểm rộng), đề bài nêu lần kiểm hẹp đó; sửa xong thì cổng lại xanh.
	t.Run("lõi đỏ ở cổng hẹp, một lượt sửa lõi", func(t *testing.T) {
		_, res, rec, _ := gateRun(t, true, []func(*harness){gateEditA("2", true), gateEditA("3", false), gateEditA("4", false)})
		if len(rec.turns) != 3 {
			t.Fatalf("muốn 3 lượt b-feat (lõi, sửa lõi, mở rộng), có %d; notes %v", len(rec.turns), res.Notes)
		}
		fix := rec.turns[1]
		if fix.verifs != 0 || fix.lastGate == nil || fix.lastGate.Passed {
			t.Errorf("lượt sửa lõi phải mở vì kiểm hẹp cổng lõi đỏ, chưa kiểm rộng: verifs %d, gate %+v", fix.verifs, fix.lastGate)
		}
		ext := rec.turns[2]
		if ext.verifs != 0 || ext.gates != 2 || ext.lastGate == nil || !ext.lastGate.Passed {
			t.Errorf("trước lượt mở rộng muốn 2 lần cổng (đỏ, xanh) và 0 kiểm rộng: gates %d, verifs %d, gate %+v", ext.gates, ext.verifs, ext.lastGate)
		}
		if !strings.Contains(strings.Join(res.Notes, "\n"), "lõi chưa xanh") {
			t.Errorf("thiếu ghi chú mở lượt sửa lõi: %v", res.Notes)
		}
	})

	// Thăm dò không để lại thước đo: không có lượt mở rộng, nên Bint kiểm rộng một lần trước review (không để review chỉ dựa trên cổng hẹp).
	t.Run("không có lượt mở rộng", func(t *testing.T) {
		h, res, rec, _ := gateRun(t, false, []func(*harness){gateEditA("2", false)})
		if len(rec.turns) != 1 {
			t.Fatalf("muốn 1 lượt b-feat, có %d; notes %v", len(rec.turns), res.Notes)
		}
		tk := h.load(res.TaskID)
		if len(tk.State.Verifications) == 0 {
			t.Fatalf("không có kiểm chứng rộng trước review; checks %+v", tk.State.Checks)
		}
		if g := lastGateCheck(tk); g != nil && tk.State.Verifications[0].At < g.At {
			t.Errorf("kiểm rộng phải chạy SAU cổng lõi: verify %s, gate %s", tk.State.Verifications[0].At, g.At)
		}
		if !strings.Contains(roStepNames(res), "b-review") {
			t.Errorf("không tới review: %s", roStepNames(res))
		}
	})
}
