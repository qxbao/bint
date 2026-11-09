package runlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Run là bản tóm tắt một lượt chạy để hiển thị.
type Run struct {
	RunID         string
	Workflow      sql.NullString
	Status        string
	Harness       sql.NullString
	SessionID     sql.NullString
	Model         sql.NullString
	BintVersion   sql.NullString
	GitBaseCommit sql.NullString
	GitBranch     sql.NullString
	Worktree      sql.NullString
	StartedAt     sql.NullString
	FinishedAt    sql.NullString
	LastEventAt   sql.NullString
	DurationMS    sql.NullInt64
	EndReason     sql.NullString
	FilesModified int
	TestsOK       int
	TestsFailed   int
	Events        int
}

const runSelect = `SELECT r.run_id, r.workflow, r.status, r.harness, r.harness_session_id, r.model,
	r.bint_version, r.git_base_commit, r.git_branch, r.worktree, r.started_at, r.finished_at,
	r.last_event_at, r.duration_ms, r.end_reason,
	(SELECT COUNT(*) FROM run_artifacts a WHERE a.run_id = r.run_id AND a.kind = 'file.modified'),
	(SELECT COUNT(*) FROM events e WHERE e.run_id = r.run_id AND e.event_type = 'test.completed'
	     AND json_extract(e.payload, '$.ok') = 1),
	(SELECT COUNT(*) FROM events e WHERE e.run_id = r.run_id AND e.event_type = 'test.completed'
	     AND json_extract(e.payload, '$.ok') = 0),
	(SELECT COUNT(*) FROM events e WHERE e.run_id = r.run_id)
	FROM runs r`

func scanRun(sc interface{ Scan(...any) error }) (Run, error) {
	var r Run
	err := sc.Scan(&r.RunID, &r.Workflow, &r.Status, &r.Harness, &r.SessionID, &r.Model,
		&r.BintVersion, &r.GitBaseCommit, &r.GitBranch, &r.Worktree, &r.StartedAt, &r.FinishedAt,
		&r.LastEventAt, &r.DurationMS, &r.EndReason, &r.FilesModified, &r.TestsOK, &r.TestsFailed, &r.Events)
	return r, err
}

// ListRuns trả về các lượt chạy mới nhất trước.
func ListRuns(ctx context.Context, db *sql.DB, limit int) ([]Run, error) {
	rows, err := db.QueryContext(ctx, runSelect+` ORDER BY r.started_at DESC, r.run_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrRunNotFound / ErrAmbiguous cho GetRun.
var (
	ErrRunNotFound = errors.New("không tìm thấy lượt chạy")
	ErrAmbiguous   = errors.New("tiền tố khớp nhiều lượt chạy")
)

// GetRun tìm lượt chạy theo ID đầy đủ hoặc tiền tố (không phân biệt hoa thường).
func GetRun(ctx context.Context, db *sql.DB, idOrPrefix string) (Run, error) {
	rows, err := db.QueryContext(ctx, runSelect+` WHERE r.run_id LIKE ? ESCAPE '\' LIMIT 2`,
		escapeLike(idOrPrefix)+"%")
	if err != nil {
		return Run{}, err
	}
	defer rows.Close()
	var found []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return Run{}, err
		}
		found = append(found, r)
	}
	switch len(found) {
	case 0:
		return Run{}, fmt.Errorf("%w: %s", ErrRunNotFound, idOrPrefix)
	case 1:
		return found[0], rows.Err()
	default:
		return Run{}, fmt.Errorf("%w: %s", ErrAmbiguous, idOrPrefix)
	}
}

func escapeLike(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' || s[i] == '_' || s[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// EventRow là một sự kiện đã nạp.
type EventRow struct {
	EventID   string
	Type      string
	Source    string
	Timestamp string
	Payload   string
}

// RunEvents trả về sự kiện của một lượt chạy theo thời gian.
func RunEvents(ctx context.Context, db *sql.DB, runID string) ([]EventRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT event_id, event_type, source, timestamp, payload
		FROM events WHERE run_id = ? ORDER BY timestamp, event_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.EventID, &e.Type, &e.Source, &e.Timestamp, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Artifact là tệp hoặc lệnh liên kết với lượt chạy.
type Artifact struct {
	Kind  string
	Path  string
	Count int
}

// RunArtifacts trả về các tham chiếu đầu ra của một lượt chạy.
func RunArtifacts(ctx context.Context, db *sql.DB, runID string) ([]Artifact, error) {
	rows, err := db.QueryContext(ctx, `SELECT kind, path, count FROM run_artifacts
		WHERE run_id = ? ORDER BY kind, path`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.Kind, &a.Path, &a.Count); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// StatusCounts đếm lượt chạy theo trạng thái.
func StatusCounts(ctx context.Context, db *sql.DB) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT status, COUNT(*) FROM runs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
