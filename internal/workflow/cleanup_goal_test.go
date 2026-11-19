package workflow

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bint/internal/task"
)

// countCalls đếm số lần agent của bước step đã được gọi trong harness.
func (h *harness) countCalls(step string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls {
		if c == step {
			n++
		}
	}
	return n
}

// TestCleanupAfterSliceTaskGoal đo mục tiêu của phiếu dọn sau phiếu về lát giữ chờ người: tài liệu không còn tả hành vi cũ,
// resume sau approve chạy kiểm chứng mà không review lại, và cấu hình có khoá lạ chặn trước b-review với lý do rõ.
func TestCleanupAfterSliceTaskGoal(t *testing.T) {
	t.Run("tai_lieu_khong_con_hanh_vi_cu", func(t *testing.T) {
		stale := regexp.MustCompile(`lát bị bỏ|bỏ lát dở|(đưa )?cây về điểm xanh|chốt (hoặc|hay|/) bỏ|hay bỏ lát|hoặc bỏ lát`)
		var files []string
		ents, err := os.ReadDir(".")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			if n := e.Name(); !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
				files = append(files, n)
			}
		}
		files = append(files, filepath.Join("..", "..", "docs", "workflows.md"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(b), "\n") {
				if !stale.MatchString(line) {
					continue
				}
				// Ngữ cảnh hợp lệ: nói về lựa chọn `--slice discard` của người (người chọn bỏ lát thì tệp mới về điểm xanh).
				if strings.Contains(line, "discard") {
					continue
				}
				t.Errorf("%s:%d còn tả hành vi cũ của lát: %s", filepath.ToSlash(f), i+1, strings.TrimSpace(line))
			}
		}
	})

	t.Run("approve_roi_resume_chay_kiem_chung_khong_review_lai", func(t *testing.T) {
		h := newHarness(t)
		cmd := "test -f done.txt"
		h.cfg.Eval.TestCommand = nil // lệnh kiểm chứng chưa được người xác nhận
		res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
		if res.Status != task.StatusInReview {
			t.Fatalf("b-feat: %+v", res)
		}
		id := res.TaskID
		if _, err := h.tasks.Update(id, h.runner().now(), func(x *task.Task) error { x.Ticket.Verify.Command = cmd; return nil }); err != nil {
			t.Fatal(err)
		}
		res = h.resumeDo(id)
		if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chưa được người xác nhận") {
			t.Fatalf("phải blocked vì lệnh chưa xác nhận: %+v", res)
		}
		reviews, verifs := h.countCalls(StepReview), len(h.load(id).State.Verifications)
		if reviews != 1 || verifs != 0 {
			t.Fatalf("tiền điều kiện: b-review %d lần (muốn 1), kiểm chứng %d lần (muốn 0)", reviews, verifs)
		}
		h.cfg.Eval.TestCommand = &cmd // người xác nhận lệnh (bint task approve)
		res = h.resumeDo(id)
		if res.Status != task.StatusDone {
			t.Fatalf("sau approve phải chạy kiểm chứng rồi done: %+v", res)
		}
		if got := h.countCalls(StepReview); got != reviews {
			t.Errorf("b-review chạy thêm %d lần sau approve, muốn 0", got-reviews)
		}
		if got := len(h.load(id).State.Verifications); got != verifs+1 {
			t.Errorf("kiểm chứng %d lần, muốn %d (đúng một lần chạy sau approve)", got, verifs+1)
		}
	})

	t.Run("khoa_cau_hinh_la_chan_truoc_review", func(t *testing.T) {
		h := newHarness(t)
		h.behaveFeat(func() {
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			h.write(".bint/config.yml", "workflow:\n  khoa_moi_xyz: 1\n")
		})
		res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
		id := res.TaskID
		res = h.resumeDo(id)
		if res.Status != task.StatusBlocked {
			t.Fatalf("khoá cấu hình lạ phải blocked (không %s): %+v", res.Status, res)
		}
		if n := h.countCalls(StepReview); n != 0 {
			t.Errorf("b-review chạy %d lần, muốn 0", n)
		}
		for _, want := range []string{"khoa_moi_xyz", "bint run resume"} {
			if !strings.Contains(res.Reason, want) {
				t.Errorf("lý do thiếu %q: %s", want, res.Reason)
			}
		}
	})
}

// noCLISkew là CLISkewCheck của harness: kiểm thử không chạy `bint` thật trên PATH của máy.
func noCLISkew(context.Context, string, string) (string, string) { return "", "" }

// `bint` trên PATH (khác binary đang chạy) không đọc được cấu hình: blocked trước b-review, lý do nêu đường dẫn binary và lỗi.
func TestConfigSkewFromPathBinaryBlocksBeforeReview(t *testing.T) {
	h := newHarness(t)
	res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	r := h.runner()
	r.CLISkewCheck = func(context.Context, string, string) (string, string) {
		return "/old/bint", "line 3: field khoa_moi_xyz not found in type config.Workflow"
	}
	out, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != task.StatusBlocked || h.countCalls(StepReview) != 0 {
		t.Fatalf("phải blocked trước b-review: %+v (review %d)", out, h.countCalls(StepReview))
	}
	for _, want := range []string{"/old/bint", "khoa_moi_xyz", "bint run resume"} {
		if !strings.Contains(out.Reason, want) {
			t.Errorf("lý do thiếu %q: %s", want, out.Reason)
		}
	}
}
