package knowledge

import (
	"strings"
	"testing"
)

func TestOriginFromEvalCase(t *testing.T) {
	o := Origin{EvalCase: "c1", EvalCases: []string{"c2", "c3"}}
	for id, want := range map[string]bool{"c1": true, "c2": true, "c3": true, "c4": false, "": false} {
		if got := o.FromEvalCase(id); got != want {
			t.Errorf("FromEvalCase(%q) = %v, muốn %v", id, got, want)
		}
	}
}

// lessonItem dựng bài học do agent tạo từ lượt thất bại (không có tệp bằng chứng).
func lessonItem(runs []string, provenance string) *Item {
	return &Item{Schema: SchemaVersion, ID: "01M74BAFEAP3EAS71818R7KQ0T", Type: TypeLesson, Status: StatusCandidate,
		Title: "Chạy test trước khi kết thúc", Body: "Khi sửa mã có test: chạy test vùng đã sửa.", Version: 1,
		Origin: Origin{Kind: OriginAgent, Provenance: provenance, Runs: runs}, CreatedAt: "2026-11-11T00:00:00Z"}
}

func TestValidateFailureLesson(t *testing.T) {
	if errs := lessonItem([]string{"ea_1", "ea_2"}, ProvenanceFailureLesson).Validate(); len(errs) != 0 {
		t.Fatalf("bài học từ lượt thất bại hợp lệ bị báo lỗi: %v", errs)
	}
	// Không có runs: vẫn phải có bằng chứng tệp (mục agent thường).
	if errs := lessonItem(nil, ProvenanceFailureLesson).Validate(); !containsErr(errs, "evidence") {
		t.Fatalf("thiếu runs phải báo thiếu bằng chứng: %v", errs)
	}
	// Provenance lạ hoặc gắn nhầm loại mục: bị từ chối.
	if errs := lessonItem([]string{"ea_1"}, "lạ").Validate(); !containsErr(errs, "provenance") {
		t.Fatalf("provenance không hợp lệ phải bị báo: %v", errs)
	}
	w := lessonItem([]string{"ea_1"}, ProvenanceFailureLesson)
	w.Type = TypeWarning
	if errs := w.Validate(); !containsErr(errs, "provenance") {
		t.Fatalf("provenance chỉ dành cho lesson: %v", errs)
	}
}

func containsErr(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}
