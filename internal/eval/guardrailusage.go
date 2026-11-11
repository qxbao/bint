package eval

import (
	"context"
	"database/sql"
	"sort"

	"bint/internal/ctxengine"
	"bint/internal/harness/hookcore"
)

// recordGuardrailUsage ghi số liệu guardrail của lượt từ nhật ký của hook: một dòng cho mỗi
// guardrail có hiệu lực ở nhánh (kể cả guardrail không bao giờ đủ điều kiện).
func recordGuardrailUsage(ctx context.Context, db *sql.DB, attemptID string, s ArmSetup) error {
	if len(s.Guardrails) == 0 {
		return nil
	}
	type agg struct{ eligible, fired, followed, blocked, bypass int }
	m := map[string]*agg{}
	for _, id := range s.Guardrails {
		m[id] = &agg{}
	}
	if s.ContextLog != "" {
		lines, err := ctxengine.ReadLog(s.ContextLog)
		if err != nil {
			return err
		}
		for _, l := range lines {
			a := m[l.Guardrail]
			if l.Channel != ctxengine.ChannelGuardrail || a == nil {
				continue
			}
			switch l.Outcome {
			case hookcore.NoteEligible:
				a.eligible = 1
			case hookcore.NoteFired:
				a.fired++
				if l.Blocked {
					a.blocked++
				}
			case hookcore.NoteFollowed:
				a.followed = 1
			case hookcore.NoteBypass:
				a.bypass++
			}
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, a := range m {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO eval_guardrail_usage
			(attempt_id, guardrail, eligible, fired, followed, blocked, bypass) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			attemptID, id, a.eligible, a.fired, a.followed, a.blocked, a.bypass); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GuardrailStat là số liệu một guardrail ở một nhánh, tính theo SỐ LƯỢT (mỗi lượt đếm một
// lần cho mỗi cột, trừ FiredCount).
type GuardrailStat struct {
	Arm       string `json:"arm"`
	Guardrail string `json:"guardrail"`
	// Attempts: số lượt mà guardrail có hiệu lực (mẫu số).
	Attempts int `json:"attempts"`
	// Eligible: lượt có điều kiện when đúng; Fired: lượt guardrail kích hoạt; Followed: lượt
	// agent làm theo sau khi bị nhắc (unless được thoả); Blocked: lượt bị ask/deny hoặc chặn
	// kết thúc; Bypass: lượt deny ở stop vượt trần.
	Eligible int `json:"eligible"`
	Fired    int `json:"fired"`
	Followed int `json:"followed"`
	Blocked  int `json:"blocked"`
	Bypass   int `json:"bypass"`
	// FiredCount: tổng số lần kích hoạt (một lượt có thể kích hoạt nhiều lần).
	FiredCount int `json:"fired_count"`
}

// batchGuardrails đọc số liệu guardrail của một đợt theo nhánh. maxRepeat > 0 chỉ tính các
// lượt có lần lặp <= maxRepeat (điểm nhìn tuần tự).
func batchGuardrails(ctx context.Context, db *sql.DB, batchID string, maxRepeat int) ([]GuardrailStat, error) {
	q := `SELECT r.arm, u.guardrail, COUNT(*),
			SUM(u.eligible > 0), SUM(u.fired > 0), SUM(u.followed > 0), SUM(u.blocked > 0), SUM(u.bypass > 0), SUM(u.fired)
		FROM eval_guardrail_usage u JOIN evaluation_runs r ON r.attempt_id = u.attempt_id
		WHERE r.batch_id = ?`
	args := []any{batchID}
	if maxRepeat > 0 {
		q += ` AND r.repeat_index <= ?`
		args = append(args, maxRepeat)
	}
	rows, err := db.QueryContext(ctx, q+` GROUP BY r.arm, u.guardrail ORDER BY r.arm, u.guardrail`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GuardrailStat
	for rows.Next() {
		var g GuardrailStat
		if err := rows.Scan(&g.Arm, &g.Guardrail, &g.Attempts, &g.Eligible, &g.Fired, &g.Followed, &g.Blocked, &g.Bypass, &g.FiredCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Arm < out[j].Arm })
	return out, rows.Err()
}
