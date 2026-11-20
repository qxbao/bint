package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Phân loại lần kiểm chứng đỏ (redtriage.go) trên module Go thật: test hay đỏ, đỏ sẵn ở mốc, đỏ do mã.
func triageHarness(t *testing.T, aTest string) *harness {
	t.Helper()
	h := newHarness(t)
	cmd := "go test -count=1 ./..."
	h.cfg.Eval.TestCommand = &cmd
	h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeFull}
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
