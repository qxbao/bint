// Package runlog nạp sự kiện từ spool vào SQLite và duy trì bảng runs.
package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"bint/internal/spool"
	"bint/internal/store"
	"bint/internal/ulid"
)

// DefaultStaleAfter: lượt chạy "running" không có sự kiện mới trong khoảng này
// được coi là "interrupted" (phiên kết thúc bất thường, không có session.ended).
const DefaultStaleAfter = 12 * time.Hour

// IngestResult tóm tắt một lần nạp.
type IngestResult struct {
	Files       int
	Events      int // sự kiện mới được ghi
	Duplicates  int // sự kiện bị bỏ do trùng event_id
	Rejected    int // tệp hỏng chuyển sang spool/rejected
	Skipped     int // tệp tạm thời không đọc/xoá được, sẽ xử lý ở lần sau
	Interrupted int // lượt chạy chuyển sang interrupted
}

// Ingest nạp mọi tệp chờ trong spoolDir. An toàn khi nhiều tiến trình cùng chạy:
// trùng lặp bị loại bởi khoá chính event_id.
func Ingest(ctx context.Context, s *store.Store, spoolDir string, now time.Time, staleAfter time.Duration) (IngestResult, error) {
	var res IngestResult
	files, err := spool.Pending(spoolDir)
	if err != nil {
		return res, err
	}
	for _, f := range files {
		events, err := spool.ReadFile(f)
		if err != nil && !errors.Is(err, spool.ErrCorrupt) {
			// Đã bị tiến trình khác nạp và xoá, hoặc đang bị khoá (Windows): để lần sau.
			res.Skipped++
			continue
		}
		if err != nil {
			if rerr := spool.Reject(spoolDir, f); rerr == nil || errors.Is(rerr, os.ErrNotExist) {
				res.Rejected++
			}
			continue
		}
		added, dups, err := ingestFile(ctx, s.DB, events, now)
		if err != nil {
			return res, fmt.Errorf("%s: %w", f, err)
		}
		res.Files++
		res.Events += added
		res.Duplicates += dups
		// Trên Windows, xoá thất bại khi tiến trình nạp khác đang mở tệp. Dữ liệu đã
		// được ghi; lần nạp sau sẽ đọc lại, loại trùng theo event_id rồi xoá.
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			res.Skipped++
		}
	}
	if staleAfter > 0 {
		n, err := MarkInterrupted(ctx, s.DB, now.Add(-staleAfter))
		if err != nil {
			return res, err
		}
		res.Interrupted = n
	}
	return res, nil
}

func ingestFile(ctx context.Context, db *sql.DB, events []spool.Event, now time.Time) (added, dups int, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	for _, e := range events {
		ok, err := applyEvent(ctx, tx, e, now)
		if err != nil {
			return 0, 0, fmt.Errorf("sự kiện %s: %w", e.EventID, err)
		}
		if ok {
			added++
		} else {
			dups++
		}
	}
	return added, dups, tx.Commit()
}

// applyEvent ghi một sự kiện; trả false nếu event_id đã tồn tại.
func applyEvent(ctx context.Context, tx *sql.Tx, e spool.Event, now time.Time) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_id = ?`, e.EventID).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	runID, err := resolveRun(ctx, tx, e)
	if err != nil {
		return false, err
	}
	payload := []byte("{}")
	if len(e.Payload) > 0 {
		if payload, err = json.Marshal(e.Payload); err != nil {
			return false, err
		}
	}
	ts := store.FormatTime(e.Timestamp)
	if _, err := tx.ExecContext(ctx, `INSERT INTO events
		(event_id, run_id, step_id, event_type, source, timestamp, payload, schema_version, ingested_at)
		VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)`,
		e.EventID, runID, e.StepID, e.Type, e.Source, ts, string(payload), e.SchemaVersion, store.FormatTime(now)); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET last_event_at = ?
		WHERE run_id = ? AND (last_event_at IS NULL OR last_event_at < ?)`, ts, runID, ts); err != nil {
		return false, err
	}
	return true, applyRunEffects(ctx, tx, runID, e, ts)
}

// resolveRun tìm hoặc tạo lượt chạy cho sự kiện.
func resolveRun(ctx context.Context, tx *sql.Tx, e spool.Event) (string, error) {
	ts := store.FormatTime(e.Timestamp)
	if e.RunID != "" {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO runs (run_id, status, harness, started_at, worktree)
			VALUES (?, 'running', NULLIF(?, ''), ?, NULLIF(?, ''))`, e.RunID, e.Harness, ts, e.Worktree)
		return e.RunID, err
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT run_id FROM runs WHERE harness = ? AND harness_session_id = ?`,
		e.Harness, e.SessionID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = ulid.At(e.Timestamp)
	_, err = tx.ExecContext(ctx, `INSERT INTO runs (run_id, status, harness, harness_session_id, started_at, worktree, workflow)
		VALUES (?, 'running', ?, ?, ?, NULLIF(?, ''), 'session')`, id, e.Harness, e.SessionID, ts, e.Worktree)
	return id, err
}

func str(p map[string]any, k string) string {
	if v, ok := p[k].(string); ok {
		return v
	}
	return ""
}

func applyRunEffects(ctx context.Context, tx *sql.Tx, runID string, e spool.Event, ts string) error {
	p := e.Payload
	switch e.Type {
	case spool.SessionStarted:
		// started_at là thời điểm sớm nhất đã biết; chỉ chuyển lại "running" nếu sự
		// kiện này mới hơn lần kết thúc (phiên được resume), tránh sai do thứ tự nạp.
		_, err := tx.ExecContext(ctx, `UPDATE runs SET
			started_at       = CASE WHEN started_at IS NULL OR started_at > ?1 THEN ?1 ELSE started_at END,
			status           = CASE WHEN finished_at IS NULL OR finished_at < ?1 THEN 'running' ELSE status END,
			end_reason       = CASE WHEN finished_at IS NULL OR finished_at < ?1 THEN NULL ELSE end_reason END,
			duration_ms      = CASE WHEN finished_at IS NULL OR finished_at < ?1 THEN NULL ELSE duration_ms END,
			finished_at      = CASE WHEN finished_at IS NULL OR finished_at < ?1 THEN NULL ELSE finished_at END,
			model            = COALESCE(NULLIF(?2, ''), model),
			harness_version  = COALESCE(NULLIF(?3, ''), harness_version),
			bint_version     = COALESCE(NULLIF(?4, ''), bint_version),
			git_base_commit  = COALESCE(git_base_commit, NULLIF(?5, '')),
			git_branch       = COALESCE(git_branch, NULLIF(?6, '')),
			worktree         = COALESCE(worktree, NULLIF(?7, ''))
			WHERE run_id = ?8`,
			ts, str(p, "model"), str(p, "harness_version"), str(p, "bint_version"),
			str(p, "git_head"), str(p, "git_branch"), e.Worktree, runID)
		return err
	case spool.SessionEnded:
		// Chỉ áp dụng nếu đây là lần kết thúc mới nhất đã biết.
		_, err := tx.ExecContext(ctx, `UPDATE runs SET
			status      = 'completed',
			finished_at = ?1,
			end_reason  = NULLIF(?2, ''),
			duration_ms = CASE WHEN started_at IS NOT NULL
			              THEN CAST(ROUND((julianday(?1) - julianday(started_at)) * 86400000) AS INTEGER)
			              ELSE NULL END
			WHERE run_id = ?3 AND (finished_at IS NULL OR finished_at < ?1)`,
			ts, str(p, "reason"), runID)
		return err
	case spool.FileModified:
		path := str(p, "path")
		if path == "" {
			return nil
		}
		return upsertArtifact(ctx, tx, runID, "file.modified", path, ts)
	case spool.TestCompleted:
		cmd := str(p, "command")
		if cmd == "" {
			return nil
		}
		return upsertArtifact(ctx, tx, runID, "test.command", cmd, ts)
	}
	return nil
}

func upsertArtifact(ctx context.Context, tx *sql.Tx, runID, kind, path, ts string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO run_artifacts (artifact_id, run_id, kind, path, first_seen_at, last_seen_at, count)
		VALUES (?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT (run_id, kind, path) DO UPDATE SET
			count = count + 1,
			first_seen_at = MIN(first_seen_at, excluded.first_seen_at),
			last_seen_at  = MAX(last_seen_at, excluded.last_seen_at)`,
		ulid.New(), runID, kind, path, ts, ts)
	return err
}

// MarkInterrupted chuyển các lượt "running" không có sự kiện từ trước cutoff sang
// "interrupted". Một sự kiện mới hơn (ví dụ resume) sẽ đưa lượt chạy về "running".
func MarkInterrupted(ctx context.Context, db *sql.DB, cutoff time.Time) (int, error) {
	r, err := db.ExecContext(ctx, `UPDATE runs SET status = 'interrupted', end_reason = 'no-end-event'
		WHERE status = 'running' AND COALESCE(last_event_at, started_at) < ?`, store.FormatTime(cutoff))
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	return int(n), err
}
