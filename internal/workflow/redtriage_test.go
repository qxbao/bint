package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/testreport"
	"bint/internal/testutil"
)

// Phân loại lần kiểm chứng đỏ (redtriage.go) trên module Go thật: test hay đỏ, đỏ sẵn ở mốc, đỏ do mã.
func triageHarness(t *testing.T, aTest string) *harness {
	t.Helper()
	h := newHarness(t)
	cmd := "go test -count=1 ./..."
	h.cfg.Eval.TestCommand = &cmd
	h.cfg.Workflow.Verify = config.WorkflowVerify{BintConfigFull: true}
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", aTest)
	h.write(".gitignore", "*.mark\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	return h
}

func lastRed(tk *task.Task) *task.Verification {
	for i := len(tk.State.Verifications) - 1; i >= 0; i-- {
		if v := tk.State.Verifications[i]; !v.Passed {
			return &v
		}
	}
	return nil
}

func TestTriageRed(t *testing.T) {
	t.Run("test hay đỏ: chạy lại xanh, không mở vòng sửa", func(t *testing.T) {
		mark := filepath.ToSlash(filepath.Join(t.TempDir(), "flaky.mark"))
		h := triageHarness(t, "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestFlaky(t *testing.T) {\n"+
			"\tif _, err := os.Stat(\""+mark+"\"); err != nil {\n\t\tos.WriteFile(\""+mark+"\", nil, 0o644)\n\t\tt.Fatal(\"lần đầu đỏ\")\n\t}\n}\n")
		h.behaveFeat(func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n"); h.write("done.txt", "ok\n") })
		res := h.runFeat("Đổi A thành 2", "")
		tk := h.load(res.TaskID)
		red := lastRed(tk)
		if red == nil || red.RedClass != redFlaky || len(red.RedTests) != 1 || !strings.Contains(red.RedTests[0], "TestFlaky") {
			t.Fatalf("muốn lần đỏ được phân loại flaky với TestFlaky: %+v", red)
		}
		if last := tk.State.LastVerification(); last == nil || !last.Passed {
			t.Errorf("sau khi chạy lại xanh phải có lần kiểm chứng xanh cuối: %+v", last)
		}
		if redVerification(tk) != nil {
			t.Errorf("test hay đỏ vẫn mở vòng sửa")
		}
	})

	t.Run("đỏ sẵn ở mốc tác vụ: không mở vòng sửa", func(t *testing.T) {
		h := triageHarness(t, "package a\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) { t.Fatal(\"đỏ từ trước\") }\n")
		h.behaveFeat(func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n"); h.write("done.txt", "ok\n") })
		res := h.runFeat("Đổi A thành 2", "")
		tk := h.load(res.TaskID)
		red := lastRed(tk)
		if red == nil || red.RedClass != redPreexisting {
			t.Fatalf("muốn đỏ sẵn (preexisting): %+v", red)
		}
		if redVerification(tk) != nil {
			t.Errorf("đỏ sẵn vẫn mở vòng sửa")
		}
		if last := tk.State.LastVerification(); last == nil || last.Passed {
			t.Errorf("đỏ sẵn không được biến thành xanh (cổng done phải chặn): %+v", last)
		}
	})

	t.Run("đỏ do mã: test mới đỏ, mở vòng sửa", func(t *testing.T) {
		h := triageHarness(t, "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
		h.behaveFeat(func() {
			h.write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc TestNew(t *testing.T) {\n\tif A() != 3 {\n\t\tt.Fatal(\"A sai\")\n\t}\n}\n")
			h.write("done.txt", "ok\n")
		})
		res := h.runFeat("Thêm TestNew", "")
		tk := h.load(res.TaskID)
		red := lastRed(tk)
		if red == nil || red.RedClass != redCode {
			t.Fatalf("muốn đỏ do mã (code): %+v", red)
		}
		if redVerification(tk) == nil {
			t.Errorf("đỏ do mã phải mở vòng sửa")
		}
	})
}

// Cổng done (donererun.go): test chỉ đỏ lúc máy tải bị gắn nhầm "đỏ sẵn" được chạy lại tuần tự trước khi chặn; test thật sự đỏ sẵn
// ở gói tác vụ không đụng tới được chấp nhận khi workflow.preexisting_red: accept.
func TestDoneGatePreexisting(t *testing.T) {
	t.Run("đỏ lúc phân loại nhưng xanh khi chạy lại ở cổng done: done", func(t *testing.T) {
		mark := filepath.ToSlash(filepath.Join(t.TempDir(), "count.mark"))
		// Đỏ ba lần đầu (kiểm chứng, chạy lại trên cây, chạy lại ở mốc) rồi xanh: như test nhạy tải.
		h := triageHarness(t, "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestLoad(t *testing.T) {\n"+
			"\tb, _ := os.ReadFile(\""+mark+"\")\n\tos.WriteFile(\""+mark+"\", append(b, 'x'), 0o644)\n"+
			"\tif len(b) < 3 {\n\t\tt.Fatal(\"máy bận\")\n\t}\n}\n")
		h.behaveFeat(func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n"); h.write("done.txt", "ok\n") })
		tk, res := h.runDoneGate()
		if tk.State.Status != task.StatusDone {
			t.Fatalf("muốn done sau khi chạy lại ở cổng done xanh: %s (%s)", tk.State.Status, res.Reason)
		}
		if red := lastRed(tk); red == nil || red.RedClass != redFlaky {
			t.Fatalf("lần đỏ phải được đổi thành flaky: %+v", red)
		}
		if !strings.Contains(strings.Join(res.Notes, "|"), "cổng done: chạy lại tuần tự") {
			t.Fatalf("phải đi qua lần chạy lại ở cổng done: %v", res.Notes)
		}
	})

	oldIn := func(t *testing.T, pkg string, accept bool) *task.Task {
		h := triageHarness(t, "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
		h.write(pkg+"/old_test.go", "package "+pkg+"\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) { t.Fatal(\"đỏ từ trước\") }\n")
		testutil.Git(t, h.root, "add", "-A")
		testutil.Git(t, h.root, "commit", "-m", "test đỏ sẵn")
		if accept {
			h.cfg.Workflow.PreexistingRed = config.PreexistingAccept
		}
		cmd := "go test -count=1 ./..."
		h.cfg.Eval.TestCommand = &cmd
		h.behaveFeat(func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n"); h.write("done.txt", "ok\n") })
		tk, _ := h.runDoneGate()
		return tk
	}
	t.Run("đỏ sẵn ở gói khác, accept: done kèm đánh dấu chấp nhận", func(t *testing.T) {
		tk := oldIn(t, "b", true)
		if tk.State.Status != task.StatusDone {
			t.Fatalf("accept phải cho done: %s", tk.State.Status)
		}
		if v := tk.State.LastVerification(); v == nil || v.Passed || !v.AcceptedPreexisting {
			t.Fatalf("lần đỏ sẵn phải được đánh dấu chấp nhận: %+v", v)
		}
	})
	t.Run("đỏ sẵn, ask (mặc định): vẫn chờ người", func(t *testing.T) {
		if tk := oldIn(t, "b", false); tk.State.Status == task.StatusDone {
			t.Fatal("mặc định không được tự done khi còn test đỏ sẵn")
		}
	})
	t.Run("đỏ sẵn ở gói tác vụ đổi, accept: không áp", func(t *testing.T) {
		if tk := oldIn(t, "a", true); tk.State.Status == task.StatusDone {
			t.Fatal("tác vụ đổi gói của test đỏ thì accept không được áp")
		}
	})
}

func TestTouchedPackages(t *testing.T) {
	ts := []testreport.Test{{Package: "example.com/m/a", Name: "TestX"}, {Package: "example.com/m/b/c", Name: "TestY"}}
	if got := touchedPackages([]string{"a/a.go", "README.md"}, ts); len(got) != 1 || got[0] != "example.com/m/a" {
		t.Fatalf("got %v", got)
	}
	if got := touchedPackages([]string{"b/a.go"}, ts); len(got) != 0 {
		t.Fatalf("thư mục cha không phải gói con: %v", got)
	}
}

// runDoneGate chạy trọn b-do (classic, không b-req) tới cổng done và trả tác vụ.
func (h *harness) runDoneGate() (*task.Task, *Result) {
	h.t.Helper()
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{Request: "Đổi A thành 2", Variant: config.VariantClassic})
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
	if err != nil {
		h.t.Fatal(err)
	}
	return h.load(tk.State.ID), res
}
