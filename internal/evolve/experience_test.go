package evolve

import (
	"context"
	"testing"
	"time"

	"bint/internal/knowledge"
	"bint/internal/store"
)

// TestExperienceShowOrderAndCounts: thư viện bài học — chung trước, riêng nhóm theo vùng; số
// lần được chèn (lượt thật) và đạt/trượt khi có bài học (lượt đánh giá) phải đúng.
func TestExperienceShowOrderAndCounts(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	apiLesson := f.item(t, knowledge.TypeLesson, knowledge.StatusVerified, "Khi sửa api nhớ đăng ký route", []string{"internal/api/**"})
	coreLesson := f.item(t, knowledge.TypeLesson, knowledge.StatusVerified, "Khi sửa core nhớ gọi Reset", []string{"internal/core/**"})
	global := f.item(t, knowledge.TypeLesson, knowledge.StatusCandidate, "Chạy test trước khi kết thúc", nil)
	f.item(t, knowledge.TypeWarning, knowledge.StatusVerified, "Không phải bài học", nil) // không được hiện

	// Lượt thật: bài học core được chèn hai lần, bài học chung một lần.
	f.run(t, "r1", time.Hour, "", nil, nil, []string{coreLesson.ID, global.ID})
	f.run(t, "r2", 2*time.Hour, "", nil, nil, []string{coreLesson.ID})

	// Lượt đánh giá: bài học core có mặt ở hai lượt (đạt một, trượt một); bài học api ở một lượt đạt.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-x', 'completed', 'claude', 'economy', '["bint"]', 1, '[]', 1, 1, ?)`,
		store.FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	attempt := func(id string, passed int, item string) {
		t.Helper()
		if _, err := f.db.ExecContext(ctx, `INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, case_split, arm,
			repeat_index, status, harness, started_at, test_passed) VALUES (?, 'eb-x', ?, 'fp', 'validation', 'bint', 0, 'passed', 'claude', ?, ?)`,
			id, "c-"+id, store.FormatTime(now), passed); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(ctx, `INSERT INTO eval_context_usage (attempt_id, item_id, retrieved, cited) VALUES (?, ?, 1, 1)`,
			id, item); err != nil {
			t.Fatal(err)
		}
	}
	attempt("ea-a", 1, coreLesson.ID)
	attempt("ea-b", 0, coreLesson.ID)
	attempt("ea-c", 1, apiLesson.ID)

	items, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ExperienceShow(ctx, f.db, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("thư viện có %d dòng, muốn 3 (không kể cảnh báo): %+v", len(rows), rows)
	}
	// Chung trước; riêng nhóm theo vùng theo thứ tự tên vùng (api trước core).
	wantOrder := []string{global.ID, apiLesson.ID, coreLesson.ID}
	for i, id := range wantOrder {
		if rows[i].ID != id {
			t.Fatalf("thứ tự dòng %d = %s, muốn %s:\n%+v", i, rows[i].ID, id, rows)
		}
	}
	if rows[0].Area != "" || rows[1].Area != "internal/api" || rows[2].Area != "internal/core" {
		t.Fatalf("vùng sai: %q %q %q", rows[0].Area, rows[1].Area, rows[2].Area)
	}
	core := rows[2]
	if core.Inserted != 2 || core.EvalPass != 1 || core.EvalFail != 1 {
		t.Fatalf("số liệu bài học core = chèn %d, đạt %d, trượt %d (muốn 2, 1, 1)", core.Inserted, core.EvalPass, core.EvalFail)
	}
	if rows[0].Inserted != 1 || rows[0].Status != knowledge.StatusCandidate {
		t.Fatalf("bài học chung: chèn %d, trạng thái %s (muốn 1, candidate)", rows[0].Inserted, rows[0].Status)
	}
	if rows[1].EvalPass != 1 || rows[1].EvalFail != 0 {
		t.Fatalf("bài học api: đạt %d, trượt %d", rows[1].EvalPass, rows[1].EvalFail)
	}
}
