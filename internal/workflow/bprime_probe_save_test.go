package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

// Thăm dò xong thì tệp được lưu vào thư mục tác vụ; resume dùng lại bản lưu khớp mốc base thay vì chạy lại thăm dò (đợt 7 mất ~6,6M token).
func TestSavedProbeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"probe/a_test.go": []byte("// Loại đầu vào: x\n"), "probe/helper.go": []byte("package probe\n")}
	saveProbe(dir, "base1", "child1", files)
	got, child := loadSavedProbe(dir, "base1")
	if child != "child1" || len(got) != 2 || string(got["probe/a_test.go"]) != "// Loại đầu vào: x\n" {
		t.Fatalf("đọc lại bản lưu: child=%q files=%v", child, got)
	}
	if got, _ := loadSavedProbe(dir, "base2"); got != nil {
		t.Fatalf("mốc base khác thì không được dùng lại: %v", got)
	}
	// Thiếu manifest (ghi dở) thì không dùng.
	os.Remove(filepath.Join(dir, probeSaveDir, "manifest.json"))
	if got, _ := loadSavedProbe(dir, "base1"); got != nil {
		t.Fatalf("không có manifest mà vẫn dùng lại: %v", got)
	}
}

// workflow.probe_catalog: danh mục ngoài được chép vào đề bài thăm dò kèm luật phủ hoặc ghi lý do loại trừ.
func TestProbeCatalogBlock(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	if s := r.probeCatalogBlock(); s != "" {
		t.Fatalf("không cấu hình thì rỗng: %q", s)
	}
	h.write("docs/secret-kinds.txt", "aws-access-key\nprivate-key (PEM)\n")
	r.Cfg.Workflow.ProbeCatalog = "docs/secret-kinds.txt"
	s := r.probeCatalogBlock()
	for _, want := range []string{"private-key (PEM)", "Loại trừ:", probeMarker} {
		if !strings.Contains(s, want) {
			t.Errorf("khối danh mục thiếu %q:\n%s", want, s)
		}
	}
	r.Cfg.Workflow.ProbeCatalog = "docs/khong-co.txt"
	if s := r.probeCatalogBlock(); !strings.Contains(s, "không đọc được") {
		t.Errorf("tệp thiếu phải nói rõ: %q", s)
	}
}

// workflow.budget_minutes: lượt b-feat trước review bị rút trần để còn review_reserve_minutes cho review và một vòng sửa; vòng sửa sau review không bị rút.
func TestBudgetLeftReservesReview(t *testing.T) {
	h := newHarness(t)
	h.behaveFeat(func() { h.write("done.txt", "ok\n") })
	id := h.runFeat("Thêm tính năng", "").TaskID
	r := h.runner()
	clock := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	r.Env.Now = func() time.Time { return clock }
	r.Cfg.Workflow.BudgetMinutes, r.Cfg.Workflow.ReviewReserveMinutes = 120, 30
	tk, err := r.Tasks.Update(id, clock, func(t *task.Task) error {
		if t.State.Classification == nil {
			t.State.Classification = &task.Classification{}
		}
		t.State.Classification.Variant = task.VariantBPrime
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := &runState{t: tk, res: &Result{}}
	if d, ok := r.budgetLeft(rs); !ok || d != 90*time.Minute {
		t.Fatalf("lần đầu: còn %v %v, muốn 90m", d, ok)
	}
	clock = clock.Add(80 * time.Minute)
	if d := r.sliceTimeoutFor(rs, true); d != 10*time.Minute {
		t.Errorf("sau 80 phút: trần lượt = %v, muốn 10m", d)
	}
	clock = clock.Add(30 * time.Minute)
	if d, _ := r.budgetLeft(rs); d != budgetMinTurn {
		t.Errorf("quá ngân sách: trần = %v, muốn sàn %v", d, budgetMinTurn)
	}
	rs.t.State.FastFixed = true
	if _, ok := r.budgetLeft(rs); ok {
		t.Errorf("vòng sửa sau review dùng phần giữ sẵn, không bị rút")
	}
}
