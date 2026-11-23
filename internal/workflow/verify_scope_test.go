package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
	"bint/internal/testutil"
)

// goHarness dựng harness có module Go (a, c độc lập) đã commit. verify.command ghi FULL vào tệp dấu (ngoài repo, đường dẫn trong GO_MARK); mỗi
// test Go giả ghi thư mục gói của nó vào tệp dấu khi `go test` (do bgraph dựng) chạy tới, nên ran() trả "AFFECTED ./a" khi chỉ gói a được chọn.
// done.txt bị gitignore: tệp mới không ai tham chiếu trong module Go làm bgraph nới ra cả module.
func goHarness(t *testing.T) *harness {
	h := newHarness(t)
	mark := filepath.ToSlash(filepath.Join(t.TempDir(), "ran.txt"))
	t.Setenv("GO_MARK", mark)
	t.Setenv("GOFLAGS", "-count=1") // không dùng kết quả test đã cache: test giả phải chạy để ghi dấu
	full := "echo FULL > " + mark
	h.cfg.Eval.TestCommand = &full
	h.write("go.mod", "module example.com/m\n\ngo 1.21\n")
	h.write("a/a.go", "package a\n\nfunc A() int { return 1 }\n")
	h.write("a/a_test.go", goMarkTest("a", "TestA"))
	h.write("c/c.go", "package c\n\nfunc C() {}\n")
	h.write("c/c_test.go", goMarkTest("c", "TestC"))
	h.write("docs/readme.md", "hi\n")
	h.write(".gitignore", "done.txt\n")
	testutil.Git(t, h.root, "add", "-A")
	testutil.Git(t, h.root, "commit", "-m", "go module")
	return h
}

// goMarkTest là tệp test Go của gói pkg: test name ghi "./<pkg>" vào tệp dấu (GO_MARK) khi chạy.
func goMarkTest(pkg, name string) string {
	return "package " + pkg + `

import (
	"fmt"
	"os"
	"testing"
)

func ` + name + `(t *testing.T) {
	f, err := os.OpenFile(os.Getenv("GO_MARK"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "./` + pkg + `")
}
`
}

// ran: "FULL" khi verify.command chạy, "AFFECTED <thư mục gói>…" (sắp xếp) khi test Go chạy, "" khi không chạy gì.
func (h *harness) ran() string {
	b, err := os.ReadFile(os.Getenv("GO_MARK"))
	if err != nil {
		return ""
	}
	var dirs []string
	for _, l := range strings.Fields(string(b)) {
		if l == "FULL" {
			return "FULL"
		}
		if !slices.Contains(dirs, l) {
			dirs = append(dirs, l)
		}
	}
	if len(dirs) == 0 {
		return ""
	}
	slices.Sort(dirs)
	return "AFFECTED " + strings.Join(dirs, " ")
}

// clearRan xoá tệp dấu.
func (h *harness) clearRan() { os.Remove(os.Getenv("GO_MARK")) }

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

// Kiểm chứng sau b-feat chạy phần chọn của bgraph: gói đổi (graph), không gì (none), cả module khi go.mod đổi (module), và lệnh của phiếu chỉ khi
// bgraph đòi cả bộ (full, ví dụ đổi tệp CI).
func TestVerifyRunsBgraphSelection(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(h *harness)
		scope string
		ran   string // ran.txt ("" = không chạy gì)
		cmd   string // Verification.Ran
	}{
		{"graph", func(h *harness) { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n") }, "graph", "AFFECTED ./a", "go test ./a"},
		{"none", func(h *harness) { h.write("docs/readme.md", "đổi\n") }, "none", "", ""},
		{"module", func(h *harness) { h.write("go.mod", "module example.com/m\n\ngo 1.22\n") }, "module", "AFFECTED ./a ./c", "go test ./a ./c"},
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
			if v.Scope != tc.scope || !v.Passed || v.Ran != tc.cmd || !strings.HasPrefix(v.Command, "echo FULL > ") || !v.Complete() {
				t.Fatalf("verification: %+v", v)
			}
			if h.ran() != tc.ran {
				t.Errorf("ran.txt = %q, muốn %q", h.ran(), tc.ran)
			}
			sel := v.Selection
			if sel == nil || string(sel.Scope) != tc.scope || sel.Source != "bgraph" || sel.Base == "" {
				t.Fatalf("selection: %+v", sel)
			}
			if tc.scope == "full" && len(sel.Widening) == 0 {
				t.Errorf("full phải kèm lý do của bgraph: %+v", sel)
			}
			if !strings.Contains(h.spoolText(), `"scope":"`+tc.scope+`"`) {
				t.Errorf("payload task.verified thiếu scope %s", tc.scope)
			}
		})
	}
}

// Cổng done nhận lần kiểm chứng theo bgraph trên đúng cây cuối: finalize không chạy thêm lần nào.
func TestDoneGateAcceptsBgraphVerification(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() {
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	res := h.resumeDo(id)
	if res.Status != task.StatusDone {
		t.Fatalf("muốn done: %+v", res)
	}
	got := h.load(id)
	if len(got.State.Verifications) != 1 || h.ran() != "AFFECTED ./a" {
		t.Fatalf("finalize không được chạy thêm: %d lần kiểm chứng, ran=%q", len(got.State.Verifications), h.ran())
	}
}

// bint task verify (Verify) cũng chạy phần chọn của bgraph; lệnh chưa xác nhận thì không chạy.
func TestVerifyTaskUsesSelection(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() {
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	h.clearRan()
	_, vr, err := h.runner().Env.Verify(context.Background(), id)
	if err != nil || vr.Verification.Scope != "graph" || vr.Verification.Selection == nil || h.ran() != "AFFECTED ./a" {
		t.Fatalf("Verify phải chạy phần chọn: %+v %v ran=%q", vr.Verification, err, h.ran())
	}
	e := h.runner().Env
	e.Cfg.Eval.TestCommand = nil
	_, vr, err = e.Verify(context.Background(), id)
	if err != nil || !strings.Contains(vr.Skipped, "chưa được người xác nhận") {
		t.Fatalf("lệnh chưa xác nhận: %+v %v", vr, err)
	}
}

func TestSelectAffectedWidensWithoutBase(t *testing.T) {
	h := goHarness(t)
	sel := SelectAffected(context.Background(), h.root, "", SelectOpts{})
	if sel.Scope != "full" || sel.Widening[0].Reason != ReasonNoBase {
		t.Fatalf("%+v", sel)
	}
	h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n")
	sel = SelectAffected(context.Background(), h.root, "HEAD", SelectOpts{})
	if sel.Scope != "graph" || len(sel.TestDirs) != 1 || sel.TestDirs[0] != "./c" {
		t.Fatalf("thay đổi chưa commit: %+v", sel)
	}
	h.write("newpkg/n.go", "package newpkg\n")
	sel = SelectAffected(context.Background(), h.root, "HEAD", SelectOpts{})
	if len(sel.Changed) != 2 {
		t.Fatalf("tệp chưa theo dõi phải được tính: %+v", sel.Changed)
	}
	// Sổ phiếu không bao giờ là thay đổi.
	h.write(".bint/tasks/01X/state.json", "{}\n")
	if sel2 := SelectAffected(context.Background(), h.root, "HEAD", SelectOpts{}); len(sel2.Changed) != 2 {
		t.Fatalf("sổ phiếu bị tính là thay đổi: %+v", sel2.Changed)
	}
}
