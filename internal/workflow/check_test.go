package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/testutil"
)

// AC-1: `bint task check` (Env.Check) chỉ chạy test của gói bị chạm, không bao giờ toàn repo, ghi state.checks và kèm kiểm nền gần nhất.
func TestTaskCheckNarrow(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	e := h.runner().Env
	ctx := context.Background()
	// Phiếu đã có kiểm chứng (nền) gần nhất để Check in kèm.
	if _, err := e.Tasks.Update(id, e.now(), func(tk *task.Task) error {
		tk.State.BackgroundVerifications = append(tk.State.BackgroundVerifications, task.Verification{At: "2026-11-20T08:00:00Z", Passed: true, Scope: "full"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		edit    func()
		skipped string // chuỗi con của Skipped ("" = có chạy/ghi)
		scope   string
		ran     string // nội dung ran.txt ("" = không chạy gì)
	}{
		{"none", func() { h.write("docs/readme.md", "đổi\n") }, "", "none", ""},
		{"graph", func() { h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n") }, "", "graph", "AFFECTED ./a"},
		{"full", func() { h.write("go.mod", "module example.com/m\n\ngo 1.22\n") }, "phạm vi full", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.write("ran.txt", "")
			tc.edit()
			before := len(h.load(id).State.Checks)
			tk, res, err := e.Check(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if tc.skipped != "" {
				// Lần bị bỏ qua vẫn được ghi (Skipped kèm lý do) để phân biệt "không gọi" với "gọi nhưng không chạy".
				if !strings.Contains(res.Skipped, tc.skipped) || len(tk.State.Checks) != before+1 || tk.State.Checks[before].Skipped != res.Skipped ||
					tk.State.Checks[before].Passed {
					t.Fatalf("muốn bỏ qua (%q) và ghi lần bỏ qua: %+v", tc.skipped, res)
				}
			} else {
				if res.Skipped != "" || len(tk.State.Checks) != before+1 || res.Check.Scope != tc.scope || !res.Check.Passed {
					t.Fatalf("kết quả: %+v", res)
				}
			}
			if got := h.ran(); got != tc.ran {
				t.Errorf("ran.txt = %q, muốn %q (kiểm hẹp không được chạy verify.command đầy đủ)", got, tc.ran)
			}
			if res.Background == nil || res.Background.At != "2026-11-20T08:00:00Z" {
				t.Errorf("thiếu kiểm nền gần nhất: %+v", res.Background)
			}
			if n := len(tk.State.Verifications); n != len(h.load(id).State.Verifications) {
				t.Errorf("Check không được đụng state.verifications")
			}
		})
	}

	// Lệnh chưa được người xác nhận thì không chạy.
	t.Run("chưa xác nhận", func(t *testing.T) {
		e2 := *e
		e2.Cfg.Eval.TestCommand = nil
		h.write("a/a.go", "package a\n\nfunc A() int { return 3 }\n")
		_, res, err := e2.Check(ctx, id)
		if err != nil || !strings.Contains(res.Skipped, "chưa được người xác nhận") {
			t.Fatalf("muốn bỏ qua vì chưa xác nhận: %+v %v", res, err)
		}
	})
}

// Tầng hẹp thật (đợt 7: "gói bị chạm" so với mốc tác vụ là 39 gói mỗi lần): sau một kiểm hẹp xanh, lần sau chỉ chạy gói có tệp đổi kể từ
// lần đó, không lan theo đồ thị; cây chưa đổi thì trả lại kết quả cũ, không chạy lại.
func TestTaskCheckSinceLastGreen(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng cho gói a và c", "").TaskID
	e := h.runner().Env
	ctx := context.Background()

	h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
	if _, res, err := e.Check(ctx, id); err != nil || res.Skipped != "" || !res.Check.Passed || res.Check.Tree == "" {
		t.Fatalf("lần 1: %+v %v", res, err)
	}
	if got := h.ran(); got != "AFFECTED ./a" {
		t.Fatalf("lần 1 ran.txt = %q", got)
	}

	// Cây không đổi (ran.txt do chính lần kiểm sinh ra đã nằm trong ảnh chụp): dùng lại kết quả, không chạy.
	h.write("marker.txt", "")
	os.Remove(filepath.Join(h.root, "marker.txt"))
	before := len(h.load(id).State.Checks)
	tk, res, err := e.Check(ctx, id)
	if err != nil || !res.Cached || len(tk.State.Checks) != before {
		t.Fatalf("cây không đổi phải dùng lại kết quả và không ghi thêm: %+v %v", res, err)
	}

	// Chỉ c đổi kể từ lần xanh: chạy ./c, không chạy lại ./a.
	h.write("c/c.go", "package c\n\nfunc C() int { return 1 }\n")
	_, res, err = e.Check(ctx, id)
	if err != nil || res.Cached || !res.Check.Passed {
		t.Fatalf("lần 3: %+v %v", res, err)
	}
	if got := h.ran(); got != "AFFECTED ./c" {
		t.Errorf("lần 3 ran.txt = %q, muốn chỉ gói đổi kể từ lần kiểm xanh trước", got)
	}
	if !strings.Contains(res.Check.Reason, "kể từ lần kiểm trước") {
		t.Errorf("Reason phải nói rõ tầng hẹp so với lần kiểm trước: %q", res.Check.Reason)
	}
}

// Không chụp được cây (sandbox chặn ghi .git/objects) thì tệp đổi tính bằng lệnh git chỉ đọc phải ra cùng tập với đường ảnh chụp.
func TestWorktreeChangedReadOnlyMatchesSnapshot(t *testing.T) {
	h := goHarness(t)
	base := strings.TrimSpace(testutil.Git(t, h.root, "rev-parse", "HEAD"))
	h.write("a/a.go", "package a\n\nfunc A() int { return 9 }\n") // sửa tệp đã theo dõi
	h.write("b/b.go", "package b\n")                              // tệp mới chưa theo dõi
	os.Remove(filepath.Join(h.root, "docs", "readme.md"))         // xoá
	h.write(".gitignore", "ignored.txt\n")
	h.write("ignored.txt", "x\n")
	testutil.Git(t, h.root, "add", "a/a.go")
	ctx := context.Background()
	head, err := WorktreeSnapshot(ctx, h.root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DiffPaths(ctx, h.root, base, head)
	if err != nil {
		t.Fatal(err)
	}
	got, err := worktreeChangedReadOnly(ctx, h.root, base)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("chỉ đọc = %v, ảnh chụp = %v", got, want)
	}
}

// Ghi chú kết quả sớm đầu tiên phân biệt "không có lần kiểm nào" với "gọi nhưng mọi lần bị bỏ qua" (đợt 7 đọc nhầm thành agent bỏ chỉ dẫn).
func TestFirstResultNoteSkipped(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	since := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	tk := &task.Task{}
	if n := r.firstResultNote(tk, since); !strings.Contains(n, "không có lần kiểm hẹp nào") {
		t.Errorf("không gọi: %q", n)
	}
	tk.State.Checks = []task.Verification{{At: "2026-11-20T09:05:00Z", Skipped: "không chụp được cây"}, {At: "2026-11-20T09:06:00Z", Skipped: "không chụp được cây"}}
	if n := r.firstResultNote(tk, since); !strings.Contains(n, "gọi 2 lần") || !strings.Contains(n, "bị bỏ qua") {
		t.Errorf("gọi nhưng bỏ qua: %q", n)
	}
	tk.State.Checks = append(tk.State.Checks, task.Verification{At: "2026-11-20T09:10:00Z", Passed: true})
	if n := r.firstResultNote(tk, since); !strings.Contains(n, "kết quả kiểm chứng đầu tiên") {
		t.Errorf("có kết quả: %q", n)
	}
}

// Bint tự chạy kiểm hẹp khi cây đứng yên đủ lâu (không trông agent gọi `bint task check`): một lần cho mỗi cây.
func TestAutoCheckWhenTreeQuiet(t *testing.T) {
	h := goHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	r := h.runner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &bgVerifier{r: r, id: id, root: h.root, tried: map[string]bool{}, runCtx: ctx, runCancel: cancel}
	h.write("a/a.go", "package a\n\nfunc A() int { return 5 }\n")
	before := len(h.load(id).State.Checks)
	now := time.Now()
	b.autoCheck(now) // lần thăm đầu: ghi nhận cây
	b.autoCheck(now.Add(autoCheckQuiet / 2))
	b.wg.Wait()
	if n := len(h.load(id).State.Checks); n != before {
		t.Fatalf("chưa đứng yên đủ lâu mà đã tự kiểm: %d → %d", before, n)
	}
	b.autoCheck(now.Add(autoCheckQuiet + time.Second))
	b.wg.Wait()
	cs := h.load(id).State.Checks
	if len(cs) != before+1 || cs[len(cs)-1].By != "bint" || !strings.HasPrefix(cs[len(cs)-1].Reason, checkAutoReason) || !cs[len(cs)-1].Passed {
		t.Fatalf("muốn đúng một lần tự kiểm hẹp do bint ghi: %+v", cs[before:])
	}
	// Lần kiểm sinh ran.txt nên cây đổi; fingerprint sau lần kiểm đã được ghi nên không tự kiểm lại cây đó.
	b.autoCheck(now.Add(2 * autoCheckQuiet))
	b.autoCheck(now.Add(4 * autoCheckQuiet))
	b.wg.Wait()
	if n := len(h.load(id).State.Checks); n != before+1 {
		t.Errorf("cây không đổi kể từ lần tự kiểm mà vẫn kiểm lại: %d lần", n-before)
	}
}
