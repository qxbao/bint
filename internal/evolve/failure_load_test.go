package evolve

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/store"
	"bint/internal/task"
)

// LoadFailures: chỉ nạp lượt eval trong cửa sổ lịch sử, và bỏ lượt timeout nhưng test đạt.
func TestLoadFailuresWindowAndTimeoutPassed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.ExecContext(ctx, `INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-w', 'completed', 'claude', 'economy', '["bint"]', 1, '[]', 1, 1, ?)`,
		store.FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	ins := func(id, status string, passed any, at time.Time) {
		t.Helper()
		if _, err := f.db.ExecContext(ctx, `INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, case_split, arm,
			repeat_index, status, harness, started_at, test_passed) VALUES (?, 'eb-w', ?, 'fp', 'validation', 'bint', 0, ?, 'claude', ?, ?)`,
			id, "c-"+id, status, store.FormatTime(at), passed); err != nil {
			t.Fatal(err)
		}
	}
	ins("ea-old", "failed", 0, now.Add(-200*24*time.Hour))
	ins("ea-new", "failed", 0, now.Add(-time.Hour))
	ins("ea-timeout-pass", "timeout", 1, now.Add(-time.Hour))   // đã giải đúng: không phải thất bại
	ins("ea-timeout-fail", "timeout", 0, now.Add(-time.Hour))   // timeout thật
	ins("ea-timeout-null", "timeout", nil, now.Add(-time.Hour)) // chưa chạy test: vẫn là thất bại
	var cases []*eval.Case
	for _, id := range []string{"ea-old", "ea-new", "ea-timeout-pass", "ea-timeout-fail", "ea-timeout-null"} {
		cases = append(cases, &eval.Case{ID: "c-" + id, Split: eval.SplitValidation, Prompt: "p"})
	}

	ids := func(since time.Time) map[string]bool {
		t.Helper()
		fails, err := LoadFailures(ctx, f.db, cases, nil, "", since, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, x := range fails {
			out[x.ID] = true
		}
		return out
	}
	all := ids(time.Time{})
	if !all["ea-old"] || !all["ea-new"] || all["ea-timeout-pass"] || !all["ea-timeout-fail"] || !all["ea-timeout-null"] {
		t.Fatalf("không cửa sổ: %v (timeout + test đạt phải bị bỏ)", all)
	}
	win := ids(now.Add(-90 * 24 * time.Hour))
	if win["ea-old"] || !win["ea-new"] || len(win) != 3 {
		t.Fatalf("cửa sổ 90 ngày phải bỏ lượt cũ: %v", win)
	}
}

// Bài học từ thất bại được chèn ở mọi đề bài và hiếm khi được trích dẫn: chúng không được
// làm bật ignored_ctx, cả ở lượt eval (eval_context_usage) lẫn lượt thật (context_usages).
func TestIgnoredCtxExcludesLessons(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.ExecContext(ctx, `INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-l', 'completed', 'claude', 'economy', '["bint"]', 1, '[]', 1, 1, ?)`,
		store.FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	var cases []*eval.Case
	for _, id := range []string{"ea-lesson", "ea-mixed"} {
		if _, err := f.db.ExecContext(ctx, `INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, case_split, arm,
			repeat_index, status, harness, started_at) VALUES (?, 'eb-l', ?, 'fp', 'validation', 'bint', 0, 'failed', 'claude', ?)`,
			id, "c-"+id, store.FormatTime(now)); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, &eval.Case{ID: "c-" + id, Split: eval.SplitValidation, Prompt: "p"})
	}
	use := func(attempt, item string) {
		t.Helper()
		if _, err := f.db.ExecContext(ctx, `INSERT INTO eval_context_usage (attempt_id, item_id, retrieved) VALUES (?, ?, 1)`,
			attempt, item); err != nil {
			t.Fatal(err)
		}
	}
	use("ea-lesson", "k-lesson")
	use("ea-mixed", "k-lesson")
	use("ea-mixed", "k-rule")
	lessons := map[string]bool{"k-lesson": true}

	fails, err := LoadFailures(ctx, f.db, cases, nil, "", time.Time{}, lessons)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, x := range fails {
		got[x.ID] = containsStr(x.Signals, SigIgnoredCtx)
	}
	if got["ea-lesson"] || !got["ea-mixed"] {
		t.Fatalf("ignored_ctx theo lượt eval = %v; muốn chỉ ea-mixed (có mục tri thức thường không được trích)", got)
	}

	onlyLesson := runFailure(&Run{ID: "r1", Retrieved: []string{"k-lesson"}}, task.Store{}, false, lessons)
	if containsStr(onlyLesson.Signals, SigIgnoredCtx) {
		t.Fatalf("lượt thật chỉ có bài học được chèn không được báo ignored_ctx: %v", onlyLesson.Signals)
	}
	mixed := runFailure(&Run{ID: "r2", Retrieved: []string{"k-lesson", "k-rule"}}, task.Store{}, false, lessons)
	if !containsStr(mixed.Signals, SigIgnoredCtx) {
		t.Fatalf("lượt thật có mục tri thức thường không được trích phải báo ignored_ctx: %v", mixed.Signals)
	}
}

// Tiền tố thư mục làm việc: dựng từ WorkRoot/runs/<batch>/<key>, kể cả khi WorkRoot chứa
// "/logs/"; relTo chuẩn hoá dấu phân cách trước khi so (Windows).
func TestWorkDirOfAndRelTo(t *testing.T) {
	root := filepath.Join("tmp", "logs", "eval-work")
	logDir := filepath.Join(root, "logs", "eb-1", "c1-bint-0")
	want := filepath.Join(root, "runs", "eb-1", "c1-bint-0")
	if got := workDirOf(logDir); got != want {
		t.Fatalf("workDirOf = %q, muốn %q (không thay /logs/ đầu tiên)", got, want)
	}
	if got := workDirOf(filepath.Join("lạ", "c1")); got != "" {
		t.Fatalf("cấu trúc lạ phải trả rỗng: %q", got)
	}
	if got := relTo("C:/w/runs/b/k/internal/x.go", "C:/w/runs/b/k"); got != "internal/x.go" {
		t.Fatalf("relTo = %q", got)
	}
	if got := relTo("/w/runs/b/k2/x.go", "/w/runs/b/k"); got != "/w/runs/b/k2/x.go" {
		t.Fatalf("tiền tố phải dừng ở ranh giới thư mục: %q", got)
	}
}
