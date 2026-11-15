package tasks_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"bint/internal/app"
	"bint/internal/tui/screens/tasks"
)

func f64(v float64) *float64 { return &v }

// richRuns: điều phối b-do, bước b-feat chạy hai lần (có chi phí), b-review thiếu chi phí.
func richRuns(calls *atomic.Int32) tasks.Options {
	rows := []app.TaskRunRow{
		{RunID: "01RUNPARENT", Workflow: "b-do", Status: "completed", Duration: "30s", CostUSD: f64(9)},
		{RunID: "01RUNCHILD1", Workflow: "b-feat", Parent: "01RUNPARENT", Status: "completed", Duration: "20s", CostUSD: f64(0.5)},
		{RunID: "01RUNCHILD2", Workflow: "b-review", Parent: "01RUNPARENT", Status: "completed", Duration: "5s"},
		{RunID: "01RUNCHILD3", Workflow: "b-feat", Parent: "01RUNPARENT", Status: "completed", Duration: "9s", CostUSD: f64(0.25)},
		{RunID: "01RUNORPHAN", Workflow: "b-lone", Parent: "01GONE", Status: "failed", Duration: "1s"},
	}
	steps, total, partial := app.StepCosts(rows, []string{"b-feat", "b-review", "b-extra"})
	return tasks.Options{Runs: func(context.Context, string, string, bool) (app.TaskRunsResult, error) {
		calls.Add(1)
		return app.TaskRunsResult{Runs: rows, Steps: steps, TotalUSD: total, TotalPartial: partial}, nil
	}}
}

// Dòng tiêu đề mục không rộng quá độ rộng nội dung: không còn dòng gạch ngang bị gãy xuống dòng sau.
func TestSectionRuleDoesNotWrap(t *testing.T) {
	r := newRig(t, tasks.Options{})
	for _, w := range []int{60, 100, 140} {
		for _, line := range strings.Split(r.view(w, 40), "\n") {
			in := strings.Trim(strings.TrimSpace(line), "│ ")
			if in != "" && strings.Trim(in, "─") == "" && len([]rune(in)) < 3 {
				t.Fatalf("w=%d: dòng gạch ngang mồ côi %q", w, line)
			}
		}
	}
}

// Thẻ Phiếu có khối nổi bật: badge trạng thái, đường ống bước với ký hiệu và chi phí, tổng một phần có ≥; lượt chạy
// được nạp một lần ngay ở thẻ Phiếu và không nạp lại khi đã có.
func TestSpotlightShowsStatusPipelineAndPartialTotal(t *testing.T) {
	var calls atomic.Int32
	r := newRig(t, richRuns(&calls))
	if calls.Load() != 1 {
		t.Fatalf("thẻ Phiếu phải nạp lượt chạy đúng một lần, được %d", calls.Load())
	}
	out := r.view(140, 40)
	for _, want := range []string{"[bị chặn]", "✓ b-feat $0.7500", "✓ b-review ?", "○ b-extra", "Tổng ≥$0.7500", "Thêm cột email"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$9") {
		t.Errorf("lượt điều phối không được cộng vào chi phí:\n%s", out)
	}
	r.press(".", ",", ".", ",")
	if calls.Load() != 1 {
		t.Fatalf("không nạp lại khi đã có cache: %d", calls.Load())
	}
	// Hẹp: đường ống gói theo từng bước, không dòng nào tràn khung.
	for _, line := range strings.Split(r.view(60, 40), "\n") {
		if n := len([]rune(line)); n > 60 {
			t.Fatalf("dòng rộng %d > 60: %q", n, line)
		}
	}
}

// Thẻ Lượt chạy: bảng chi phí theo bước có thanh, "?" cho chi phí không biết (không bao giờ $0.0000), lượt con thụt vào dưới lượt điều phối.
func TestRunsTabStepTableAndGrouping(t *testing.T) {
	var calls atomic.Int32
	r := newRig(t, richRuns(&calls))
	r.press(",")
	out := r.view(140, 40)
	for _, want := range []string{"Chi phí theo bước", "b-feat", "×2", "$0.7500", "≥$0.7500", "█", "?"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "$0.0000") {
		t.Errorf("chi phí không biết không được hiện là 0:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	parent, child, orphan := -1, -1, -1
	for i, l := range lines {
		if strings.Contains(l, "NPARENT") {
			parent = i
		}
		if strings.Contains(l, "└ ") && strings.Contains(l, "CHILD1") {
			child = i
		}
		if strings.Contains(l, "└ ") && strings.Contains(l, "ORPHAN") {
			orphan = i
		}
	}
	if !(parent >= 0 && child == parent+1 && orphan > child) {
		t.Fatalf("lượt con thụt ngay dưới lượt cha, mồ côi cuối (%d %d %d):\n%s", parent, child, orphan, out)
	}
}

func TestCriteriaBarAndReviewBadges(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press(".", ".")
	out := r.view(140, 34)
	if !strings.Contains(out, "0/2 tiêu chí") || !strings.Contains(out, "░░░░") {
		t.Errorf("thẻ Tiêu chí thiếu thanh tiến độ:\n%s", out)
	}
	r.press(".", ".")
	out = r.view(140, 34)
	for _, want := range []string{"[nghiêm trọng 1]", "[vừa 1]", "[thấp 1]"} {
		if !strings.Contains(out, want) {
			t.Errorf("thẻ Review thiếu badge %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " 0]") {
		t.Errorf("không có badge cho mức 0:\n%s", out)
	}
}
