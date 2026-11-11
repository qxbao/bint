package evolve

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/task"
)

// DigestReport là kết quả `bint experience digest` cho một đợt đánh giá hoặc một lượt thật.
type DigestReport struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"` // batch | run
	// Digests: bản tóm tắt từng lượt thất bại (chỉ tập validation với đợt đánh giá).
	Digests []Digest `json:"digests"`
	// Groups: nhóm thất bại (vùng mã và dấu hiệu) với trạng thái sẵn sàng sinh bài học.
	Groups []GroupSummary `json:"groups"`
	// Skipped: số lượt không thuộc tập validation, bỏ qua (tập test không bao giờ là đầu vào).
	Skipped int `json:"skipped_not_validation"`
}

// GroupSummary là một nhóm thất bại trong báo cáo.
type GroupSummary struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Summary string   `json:"summary"`
	Runs    []string `json:"runs"`
	Ready   bool     `json:"ready"`
	Blocked string   `json:"blocked,omitempty"`
}

// errNotBatchOrRun: ref không là đợt đánh giá cũng không là lượt chạy.
var errNotBatchOrRun = errors.New("không tìm thấy đợt đánh giá hoặc lượt chạy")

// DigestRef dựng bản tóm tắt thất bại cho một đợt đánh giá (ref là ID đợt, tiền tố, hoặc
// "latest") hoặc một lượt chạy thật. minUnits/minNew là ngưỡng nhóm (evolution.trigger).
func DigestRef(ctx context.Context, db *sql.DB, cases []*eval.Case, ref, runtime string, minUnits, minNew int) (DigestReport, error) {
	rep := DigestReport{Ref: ref}
	if b, err := eval.GetBatch(ctx, db, ref); err == nil {
		rep.Ref, rep.Kind = b.BatchID, "batch"
		fails, err := LoadFailures(ctx, db, cases, nil, runtime, time.Time{})
		if err != nil {
			return rep, err
		}
		var mine []Failure
		for _, f := range fails {
			if f.Batch == b.BatchID {
				mine = append(mine, f)
			}
		}
		for _, f := range mine {
			rep.Digests = append(rep.Digests, BuildDigest(f))
		}
		for _, o := range failureLessonOpps(mine, minUnits, minNew, nil, nil) {
			rep.Groups = append(rep.Groups, GroupSummary{Key: o.Key, Group: o.Group, Summary: o.Summary,
				Runs: o.SupportingRuns, Ready: o.Ready, Blocked: o.Blocked})
		}
		rep.Skipped, err = nonValidationFailures(ctx, db, b.BatchID)
		return rep, err
	}
	run, err := runlog.GetRun(ctx, db, ref)
	if err != nil {
		return rep, errNotBatchOrRun
	}
	h, err := LoadHistory(ctx, db, time.Now(), 10*365*24*time.Hour)
	if err != nil {
		return rep, err
	}
	r := h.ByID[run.RunID]
	if r == nil {
		return rep, errNotBatchOrRun
	}
	rep.Ref, rep.Kind = r.ID, "run"
	rep.Digests = []Digest{BuildDigest(RunFailure(r, runtime))}
	return rep, nil
}

// RunFailure dựng đơn vị thất bại từ một lượt thật (phạm vi và mục tiêu đọc từ phiếu nếu có
// runtime). Dùng cho `bint experience digest <run>`.
func RunFailure(r *Run, runtime string) Failure {
	return runFailure(r, task.NewStore(runtime), runtime != "")
}

// nonValidationFailures đếm lượt thất bại của đợt không thuộc tập validation (bị bỏ qua).
func nonValidationFailures(ctx context.Context, db *sql.DB, batch string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evaluation_runs
		WHERE batch_id = ? AND status IN ('failed','timeout') AND (case_split IS NULL OR case_split <> ?)`,
		batch, eval.SplitValidation).Scan(&n)
	return n, err
}

// LessonRow là một dòng của `bint experience show`.
type LessonRow struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Area       string   `json:"area,omitempty"` // "" = bài học chung
	Scope      []string `json:"scope,omitempty"`
	Provenance string   `json:"provenance,omitempty"`
	Runs       int      `json:"runs"`
	// Inserted: số lần được chèn trong lượt thật (context_usages).
	Inserted int `json:"inserted"`
	// RealSuccess/RealFailure: số lượt thật có bài học được chèn theo nhãn lượt (runs, mục 9.6):
	// success / failure. Lượt chưa có nhãn đủ mạnh không được tính vào cột nào.
	RealSuccess int `json:"real_success"`
	RealFailure int `json:"real_failure"`
	// EvalPass/EvalFail: lượt đánh giá có bài học được chèn và đạt/trượt (eval_context_usage).
	EvalPass int `json:"eval_pass"`
	EvalFail int `json:"eval_fail"`
}

// ExperienceShow trả về thư viện bài học (mục lesson còn hiệu lực), bài học chung trước,
// bài học riêng nhóm theo vùng; kèm số liệu sử dụng. h (có thể nil) là lịch sử lượt thật để đếm
// đạt/trượt theo nhãn lượt; nil thì hai cột đó là 0.
func ExperienceShow(ctx context.Context, db *sql.DB, items []*knowledge.Item, h *History) ([]LessonRow, error) {
	inserted, err := countByItem(ctx, db, `SELECT item_id, COUNT(*) FROM context_usages GROUP BY item_id`)
	if err != nil {
		return nil, err
	}
	pass, err := countByItem(ctx, db, `SELECT u.item_id, COUNT(*) FROM eval_context_usage u
		JOIN evaluation_runs r ON r.attempt_id = u.attempt_id WHERE u.retrieved = 1 AND r.test_passed = 1 GROUP BY u.item_id`)
	if err != nil {
		return nil, err
	}
	fail, err := countByItem(ctx, db, `SELECT u.item_id, COUNT(*) FROM eval_context_usage u
		JOIN evaluation_runs r ON r.attempt_id = u.attempt_id WHERE u.retrieved = 1 AND r.test_passed = 0 GROUP BY u.item_id`)
	if err != nil {
		return nil, err
	}
	realOK, realBad := map[string]int{}, map[string]int{}
	if h != nil {
		for _, r := range h.Runs {
			seen := map[string]bool{}
			for _, id := range r.Retrieved {
				if seen[id] {
					continue
				}
				seen[id] = true
				switch r.Label {
				case runlog.LabelSuccess:
					realOK[id]++
				case runlog.LabelFailure:
					realBad[id]++
				}
			}
		}
	}
	var rows []LessonRow
	for _, it := range items {
		if !it.LessonFromRuns() || !it.Active() { // cùng định nghĩa với ctxengine.isLesson
			continue
		}
		area := ""
		if !it.Scope.Global() {
			area = scopeArea(it.Scope.Paths)
		}
		rows = append(rows, LessonRow{ID: it.ID, Title: it.Title, Status: it.Status, Area: area, Scope: it.Scope.Paths,
			Provenance: it.Origin.Provenance, Runs: len(it.Origin.Runs), Inserted: inserted[it.ID],
			RealSuccess: realOK[it.ID], RealFailure: realBad[it.ID],
			EvalPass: pass[it.ID], EvalFail: fail[it.ID]})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Area == "") != (b.Area == "") {
			return a.Area == "" // chung trước
		}
		if a.Area != b.Area {
			return a.Area < b.Area
		}
		if a.Status != b.Status {
			return a.Status == knowledge.StatusVerified // đã xác thực trước ứng viên
		}
		return a.ID < b.ID
	})
	return rows, nil
}

// scopeArea trả về vùng mã của phạm vi (tiền tố cố định của mẫu đầu tiên), "." nếu ở gốc.
func scopeArea(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	p := paths[0]
	if i := strings.IndexAny(p, "*?["); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

// countByItem chạy truy vấn (item_id, số đếm) và trả về bản đồ.
func countByItem(ctx context.Context, db *sql.DB, q string) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
