package eval

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"bint/internal/ctxengine"
)

func evalContextEnv(s ArmSetup) []string {
	if s.EvalConfig == "" {
		return nil
	}
	return []string{ctxengine.EvalEnv + "=" + s.EvalConfig}
}

// recordContextUsage ghi các mục được truy xuất / trích dẫn trong lượt (đọc nhật
// ký của hook và MCP server) cùng tập "đúng" của tác vụ, phục vụ chẩn đoán 12.9.
func recordContextUsage(ctx context.Context, db *sql.DB, attemptID string, s ArmSetup) error {
	type agg struct {
		retrieved, cited, ideal bool
		channels                map[string]bool
	}
	m := map[string]*agg{}
	get := func(id string) *agg {
		if m[id] == nil {
			m[id] = &agg{channels: map[string]bool{}}
		}
		return m[id]
	}
	for _, id := range s.Ideal {
		get(id).ideal = true
	}
	if s.ContextLog != "" {
		lines, err := ctxengine.ReadLog(s.ContextLog)
		if err != nil {
			return err
		}
		for _, l := range lines {
			for _, id := range l.IDs {
				a := get(id)
				if l.Channel == "cite" {
					a.cited = true
				} else {
					a.retrieved = true
					a.channels[l.Channel] = true
				}
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	for id, a := range m {
		var chs []string
		for c := range a.channels {
			chs = append(chs, c)
		}
		sort.Strings(chs)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO eval_context_usage
			(attempt_id, item_id, retrieved, cited, ideal, channels) VALUES (?, ?, ?, ?, ?, ?)`,
			attemptID, id, b(a.retrieved), b(a.cited), b(a.ideal), strings.Join(chs, ",")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AttemptContext là tóm tắt ngữ cảnh của một lượt.
type AttemptContext struct {
	Retrieved, Cited, Ideal    int
	IdealRetrieved, IdealCited int
	RetrievedNotIdeal          int
}

// batchContext đọc tóm tắt ngữ cảnh theo lượt của một đợt.
func batchContext(ctx context.Context, db *sql.DB, batchID string) (map[string]AttemptContext, error) {
	rows, err := db.QueryContext(ctx, `SELECT u.attempt_id,
			SUM(u.retrieved), SUM(u.cited), SUM(u.ideal),
			SUM(u.ideal * u.retrieved), SUM(u.ideal * u.cited), SUM(u.retrieved * (1 - u.ideal))
		FROM eval_context_usage u JOIN evaluation_runs r ON r.attempt_id = u.attempt_id
		WHERE r.batch_id = ? GROUP BY u.attempt_id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AttemptContext{}
	for rows.Next() {
		var id string
		var a AttemptContext
		if err := rows.Scan(&id, &a.Retrieved, &a.Cited, &a.Ideal, &a.IdealRetrieved, &a.IdealCited, &a.RetrievedNotIdeal); err != nil {
			return nil, err
		}
		out[id] = a
	}
	return out, rows.Err()
}
