package workflow

import (
	"context"
	"strings"
	"testing"

	"bint/internal/task"
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
				if !strings.Contains(res.Skipped, tc.skipped) || len(tk.State.Checks) != before {
					t.Fatalf("muốn bỏ qua (%q) và không ghi: %+v", tc.skipped, res)
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
