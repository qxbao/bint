package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/testutil"
)

// goHarness dựng harness có module Go (a, c độc lập) đã commit, verify.command ghi FULL vào ran.txt và
// affected_command ghi AFFECTED kèm thư mục test được chọn.
func goHarness(t *testing.T) *harness {
	h := newHarness(t)
	full := "echo FULL > ran.txt"
	h.cfg.Eval.TestCommand = &full
	h.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeAffected, AffectedCommand: "echo AFFECTED {test_dirs} > ran.txt"}
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	h.write("c/c.go", "package c\n\nfunc C() {}\n")
	h.write("c/c_test.go", "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n")
	h.write("docs/readme.md", "hi\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	return h
}

func (h *harness) ran() string {
	b, err := os.ReadFile(filepath.Join(h.root, "ran.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (h *harness) spoolText() string {
	var sb strings.Builder
	filepath.Walk(h.spool, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			sb.Write(b)
		}
		return nil
	})
	return sb.String()
}

func TestVerifyScopeAffectedTiers(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(h *harness)
		scope string
		ran   string // nội dung ran.txt ("" = không chạy gì)
		cmd   string // Verification.Ran
	}{
		{"graph", func(h *harness) { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n") }, "graph", "AFFECTED ./a", "echo AFFECTED ./a > ran.txt"},
		{"none", func(h *harness) { h.write("docs/readme.md", "đổi\n") }, "none", "", ""},
		{"full", func(h *harness) { h.write("go.mod", "module example.com/m\n\ngo 1.22\n") }, "full", "FULL", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := goHarness(t)
			h.behaveFeat(func() {
				tc.edit(h)
				h.write("done.txt", "ok\n")
			})
			res := h.runFeat("Thêm tính năng cho gói a", "")
			if res.Status != task.StatusInReview {
				t.Fatalf("b-feat: %+v", res)
			}
			got := h.load(res.TaskID)
			if len(got.State.Verifications) != 1 {
				t.Fatalf("verifications = %d", len(got.State.Verifications))
			}
			v := got.State.Verifications[0]
			if v.Scope != tc.scope || !v.Passed || v.Ran != tc.cmd || v.Command != "echo FULL > ran.txt" {
				t.Fatalf("verification: %+v", v)
			}
			if h.ran() != tc.ran {
				t.Errorf("ran.txt = %q, muốn %q", h.ran(), tc.ran)
			}
			sel := v.Selection
			if sel == nil || string(sel.Scope) != tc.scope || sel.Base == "" || sel.Head == "" || len(sel.Changed) == 0 {
				t.Fatalf("selection: %+v", sel)
			}
			if tc.scope == "graph" && (len(sel.TestDirs) != 1 || sel.TestDirs[0] != "./a") {
				t.Errorf("test_dirs: %v (không được có c)", sel.TestDirs)
			}
			if tc.scope == "full" && (len(sel.Widening) == 0 || sel.Widening[0].Reason != "root_config") {
				t.Errorf("widening: %+v", sel.Widening)
			}
			if !strings.Contains(h.spoolText(), `"scope":"`+tc.scope+`"`) {
				t.Errorf("payload task.verified thiếu scope %s", tc.scope)
			}
		})
	}
}

// Cổng done không nhận lần kiểm chứng một phần; finalize tự chạy đầy đủ rồi done.
func TestDoneGateNeedsFullAndFinalizeRunsFull(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() {
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	tk := h.load(id)
	if v := tk.State.LastVerification(); v.Scope != "graph" {
		t.Fatalf("lần đầu: %+v", v)
	}
	e := h.runner().Env
	in, err := e.DoneInputDetailed(context.Background(), tk)
	if err != nil {
		t.Fatal(err)
	}
	in.VerifyConfirmed = true
	var partial bool
	for _, b := range task.DoneBlockersTyped(tk, in) {
		if b.Kind == task.BlockVerifyPartial {
			partial = b.Kind.Forceable()
		}
	}
	if !partial {
		t.Fatalf("cổng done phải chặn lần kiểm chứng một phần (forceable): %v", task.DoneBlockers(tk, in))
	}
	res := h.resumeDo(id)
	if res.Status != task.StatusDone {
		t.Fatalf("finalize phải chạy đầy đủ rồi done: %+v", res)
	}
	got := h.load(id)
	if len(got.State.Verifications) != 2 {
		t.Fatalf("verifications = %d, muốn 2", len(got.State.Verifications))
	}
	last := got.State.LastVerification()
	if !last.Full() || last.Ran != "" || !strings.Contains(last.Reason, "chỉ chạy test bị ảnh hưởng") || h.ran() != "FULL" {
		t.Fatalf("lần cuối: %+v ran=%q", last, h.ran())
	}
}

// bint task verify (Verify) luôn chạy đầy đủ; scope full giữ hành vi cũ; lệnh chưa xác nhận thì không chạy.
func TestVerifyAlwaysFullOutsideRunner(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() {
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	_, vr, err := h.runner().Env.Verify(context.Background(), id)
	if err != nil || vr.Verification.Scope != "" || vr.Verification.Selection != nil || h.ran() != "FULL" {
		t.Fatalf("Verify phải chạy đầy đủ: %+v %v ran=%q", vr.Verification, err, h.ran())
	}

	h2 := goHarness(t)
	h2.cfg.Workflow.Verify = config.WorkflowVerify{Scope: config.VerifyScopeFull}
	h2.behaveFeat(func() {
		h2.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h2.write("done.txt", "ok\n")
	})
	got := h2.load(h2.runFeat("Thêm tính năng cho gói a", "").TaskID)
	if v := got.State.LastVerification(); v.Scope != "" || v.Selection != nil || h2.ran() != "FULL" {
		t.Fatalf("scope full: %+v", v)
	}
}

func TestSelectAffectedWidensWithoutBase(t *testing.T) {
	h := goHarness(t)
	sel := SelectAffected(context.Background(), h.root, "")
	if sel.Scope != "full" || sel.Widening[0].Reason != ReasonNoBase {
		t.Fatalf("%+v", sel)
	}
	h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n")
	sel = SelectAffected(context.Background(), h.root, "HEAD")
	if sel.Scope != "graph" || len(sel.TestDirs) != 1 || sel.TestDirs[0] != "./c" {
		t.Fatalf("thay đổi chưa commit: %+v", sel)
	}
	h.write("newpkg/n.go", "package newpkg\n")
	sel = SelectAffected(context.Background(), h.root, "HEAD")
	if len(sel.Changed) != 2 {
		t.Fatalf("tệp chưa theo dõi phải được tính: %+v", sel.Changed)
	}
}

// Hồi quy ngoài tập test được chọn lộ ở lần chạy đầy đủ của finalize: tác vụ về bước sửa (b-feat) khi còn vòng
// workflow.max_review_rounds, sửa xong thì finalize chạy đầy đủ lại và done.
func TestFinalizeFullVerifyFailureReturnsToFix(t *testing.T) {
	h := goHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	full := "test ! -f broken.txt && echo FULL > ran.txt"
	h.cfg.Eval.TestCommand = &full
	feats := 0
	h.behaveFeat(func() {
		feats++
		if feats == 1 {
			h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
			h.write("broken.txt", "x\n")
		} else {
			os.Remove(filepath.Join(h.root, "broken.txt")) // lần sửa
		}
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	res := h.resumeDo(id)
	if res.Status != task.StatusDone || feats != 2 {
		t.Fatalf("muốn done sau đúng 1 vòng sửa: status=%s feats=%d notes=%v", res.Status, feats, res.Notes)
	}
	got := h.load(id)
	fails := 0
	for _, v := range got.State.Verifications {
		if !v.Passed && v.Full() && strings.Contains(v.Reason, "chỉ chạy test bị ảnh hưởng") {
			fails++
		}
	}
	if fails != 1 {
		t.Errorf("số lần kiểm chứng đầy đủ hỏng ở finalize = %d, muốn 1", fails)
	}
}

// Hết vòng thì chặn chờ người như cũ.
func TestFinalizeFullVerifyFailureBlocksWhenRoundsExhausted(t *testing.T) {
	h := goHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	full := "test ! -f broken.txt && echo FULL > ran.txt"
	h.cfg.Eval.TestCommand = &full
	feats := 0
	h.behaveFeat(func() {
		feats++
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("broken.txt", "x\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || feats != 2 {
		t.Fatalf("muốn blocked sau 1 vòng sửa: status=%s feats=%d", res.Status, feats)
	}
	if n := fullVerifyFailRounds(h.load(id)); n != 2 {
		t.Errorf("số lần hỏng = %d, muốn 2", n)
	}
}

// Driver session: cùng luật; kiểm chứng đầy đủ ở finalize hỏng → next trả lại bước b-feat (không blocked).
func TestSessionFinalizeFullVerifyFailureReturnsToFix(t *testing.T) {
	h := goHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	full := "test ! -f broken.txt && echo FULL > ran.txt"
	h.cfg.Eval.TestCommand = &full
	feats := 0
	h.behaveFeat(func() {
		feats++
		if feats == 1 {
			h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
			h.write("broken.txt", "x\n")
		} else {
			os.Remove(filepath.Join(h.root, "broken.txt"))
		}
		h.write("done.txt", "ok\n")
	})
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false), Request: "Thêm tính năng cho gói a"})
	steps, last := h.drive(r, tk.State.ID, false)
	if last.Action != ActionDone || feats != 2 {
		t.Fatalf("bước %v, dừng: %+v, feats=%d", steps, last, feats)
	}
}
