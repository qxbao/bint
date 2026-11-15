package runlog

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// Kinds của run_artifacts dùng cho truy vấn lịch sử.
const (
	ArtifactModified = "file.modified"
	ArtifactRead     = "file.read"
)

// RunTouch là một lượt chạy đã chạm vào các đường dẫn được hỏi.
type RunTouch struct {
	RunID     string
	TaskID    string
	Workflow  string
	Harness   string
	Model     string
	StartedAt string
	Status    string
	// Files là các tệp khớp (đã sắp xếp), thuộc kinds được hỏi.
	Files []string
}

func cleanHistoryPath(p string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
}

// RunsTouching trả về các lượt chạy có artifact thuộc kinds (file.modified, file.read) khớp một trong paths,
// mới nhất trước, tối đa limit. Path khớp khi bằng đúng tệp hoặc nằm dưới thư mục (tiền tố "path/").
// Đường dẫn rỗng hoặc "." khớp mọi tệp. Truy vấn chỉ đọc.
func RunsTouching(ctx context.Context, db *sql.DB, paths, kinds []string, limit int) ([]RunTouch, error) {
	if len(paths) == 0 || len(kinds) == 0 || limit <= 0 {
		return nil, nil
	}
	var conds []string
	var args []any
	for _, p := range paths {
		p = cleanHistoryPath(p)
		if p == "" || p == "." {
			conds = append(conds, "1 = 1")
			continue
		}
		conds = append(conds, `(a.path = ? OR a.path LIKE ? ESCAPE '\')`)
		args = append(args, p, escapeLike(p)+"/%")
	}
	kindMarks := strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")
	args = append(args, anyStrings(kinds)...)
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `SELECT r.run_id, COALESCE(r.task_id, ''), COALESCE(r.workflow, ''), COALESCE(r.harness, ''),
		COALESCE(r.model, ''), COALESCE(r.started_at, ''), r.status
		FROM runs r WHERE r.run_id IN (
			SELECT a.run_id FROM run_artifacts a WHERE (`+strings.Join(conds, " OR ")+`) AND a.kind IN (`+kindMarks+`))
		ORDER BY r.started_at DESC, r.run_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	var out []RunTouch
	for rows.Next() {
		var t RunTouch
		if err := rows.Scan(&t.RunID, &t.TaskID, &t.Workflow, &t.Harness, &t.Model, &t.StartedAt, &t.Status); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		files, err := touchedFiles(ctx, db, out[i].RunID, paths, kinds)
		if err != nil {
			return nil, err
		}
		out[i].Files = files
	}
	return out, nil
}

func touchedFiles(ctx context.Context, db *sql.DB, runID string, paths, kinds []string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT path FROM run_artifacts WHERE run_id = ? AND kind IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",")+`) ORDER BY path`, append([]any{runID}, anyStrings(kinds)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if pathMatches(p, paths) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, rows.Err()
}

func anyStrings(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// pathMatches khớp cùng quy tắc với truy vấn SQL của RunsTouching.
func pathMatches(file string, paths []string) bool {
	for _, p := range paths {
		p = cleanHistoryPath(p)
		if p == "" || p == "." || file == p || strings.HasPrefix(file, p+"/") {
			return true
		}
	}
	return false
}

// FailedTestCommands trả về lệnh kiểm thử không qua (test.completed có ok = 0) của một lượt chạy, theo thời gian,
// bỏ trùng. Lệnh rỗng (hook không ghi) bị bỏ.
func FailedTestCommands(ctx context.Context, db *sql.DB, runID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(json_extract(payload, '$.command'), '') FROM events
		WHERE run_id = ? AND event_type = 'test.completed' AND json_extract(payload, '$.ok') = 0
		ORDER BY timestamp, event_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	seen := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, rows.Err()
}
