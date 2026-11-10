package runlog

import (
	"context"
	"database/sql"
	"fmt"

	"bint/internal/spool"
)

// numOrNil đọc số từ payload JSON; thiếu thì NULL (không thay bằng 0).
func numOrNil(p map[string]any, k string) any {
	switch v := p[k].(type) {
	case float64:
		return v
	case int:
		return v
	case int64:
		return v
	}
	return nil
}

func intOrNil(p map[string]any, k string) any {
	if v, ok := numOrNil(p, k).(float64); ok {
		return int64(v)
	}
	return numOrNil(p, k)
}

// applyWorkflowEffects áp sự kiện của workflow và tác vụ (P4).
func applyWorkflowEffects(ctx context.Context, tx *sql.Tx, runID string, e spool.Event, ts string) error {
	p := e.Payload
	if task := str(p, "task_id"); task != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET task_id = COALESCE(task_id, ?) WHERE run_id = ?`, task, runID); err != nil {
			return err
		}
	}
	switch e.Type {
	case spool.WorkflowStarted:
		_, err := tx.ExecContext(ctx, `UPDATE runs SET
			workflow        = COALESCE(NULLIF(?1, ''), workflow),
			parent_run_id   = COALESCE(parent_run_id, (SELECT run_id FROM runs WHERE run_id = NULLIF(?2, '') AND run_id <> ?9)),
			harness         = COALESCE(harness, NULLIF(?3, '')),
			model           = COALESCE(NULLIF(?4, ''), model),
			bint_version    = COALESCE(NULLIF(?5, ''), bint_version),
			git_base_commit = COALESCE(git_base_commit, NULLIF(?6, '')),
			git_branch      = COALESCE(git_branch, NULLIF(?7, '')),
			harness_version = COALESCE(NULLIF(?8, ''), harness_version),
			started_at      = CASE WHEN started_at IS NULL OR started_at > ?10 THEN ?10 ELSE started_at END,
			status          = CASE WHEN finished_at IS NULL THEN 'running' ELSE status END
			WHERE run_id = ?9`,
			str(p, "workflow"), str(p, "parent_run_id"), str(p, "harness"), str(p, "model"), str(p, "bint_version"),
			str(p, "git_head"), str(p, "git_branch"), str(p, "harness_version"), runID, ts)
		if err != nil {
			return err
		}
		return recomputeDuration(ctx, tx, runID)
	case spool.WorkflowFinished:
		status := str(p, "status")
		switch status {
		case "completed", "failed", "cancelled", "interrupted":
		default:
			return fmt.Errorf("workflow.finished: trạng thái %q không hợp lệ", status)
		}
		_, err := tx.ExecContext(ctx, `UPDATE runs SET
			status              = ?1,
			finished_at         = ?2,
			end_reason          = NULLIF(?3, ''),
			result_summary      = COALESCE(NULLIF(?4, ''), result_summary),
			verification_status = COALESCE(NULLIF(?5, ''), verification_status),
			model               = COALESCE(NULLIF(?6, ''), model),
			input_tokens        = COALESCE(?7, input_tokens),
			output_tokens       = COALESCE(?8, output_tokens),
			cost_usd            = COALESCE(?9, cost_usd),
			duration_ms         = CASE WHEN started_at IS NOT NULL
			                      THEN CAST(ROUND((julianday(?2) - julianday(started_at)) * 86400000) AS INTEGER)
			                      ELSE duration_ms END
			WHERE run_id = ?10 AND (finished_at IS NULL OR finished_at <= ?2)`,
			status, ts, str(p, "end_reason"), str(p, "result_summary"), str(p, "verification_status"), str(p, "model"),
			intOrNil(p, "input_tokens"), intOrNil(p, "output_tokens"), numOrNil(p, "cost_usd"), runID)
		return err
	case spool.StepStarted, spool.StepFinished:
		stepID := str(p, "step_id")
		if stepID == "" {
			return nil
		}
		started, finished := ts, ""
		if e.Type == spool.StepFinished {
			started, finished = "", ts
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO run_steps (step_id, run_id, name, status, started_at, finished_at)
			VALUES (?1, ?2, ?3, ?4, NULLIF(?5, ''), NULLIF(?6, ''))
			ON CONFLICT (step_id) DO UPDATE SET
				status      = CASE WHEN excluded.finished_at IS NOT NULL OR run_steps.finished_at IS NULL THEN excluded.status ELSE run_steps.status END,
				started_at  = COALESCE(run_steps.started_at, excluded.started_at),
				finished_at = COALESCE(excluded.finished_at, run_steps.finished_at)`,
			stepID, runID, str(p, "name"), str(p, "status"), started, finished)
		return err
	case spool.TaskArtifact:
		if path := str(p, "path"); path != "" {
			return upsertArtifact(ctx, tx, runID, "task."+str(p, "name"), path, ts)
		}
	case spool.TaskVerified:
		passed, has := p["passed"].(bool)
		if !has {
			return nil
		}
		strength := "medium"
		if c, _ := p["confirmed"].(bool); c {
			strength = "strong"
		}
		status := "failed"
		if passed {
			status = "passed"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET verification_status = ? WHERE run_id = ?`, status, runID); err != nil {
			return err
		}
		return UpsertSignal(ctx, tx, Signal{RunID: runID, Kind: SignalVerify, Positive: passed, Strength: strength,
			Source: e.Source, Detail: str(p, "command"), DedupKey: "verify:" + e.EventID, ObservedAt: ts})
	case spool.TaskCriteria:
		met, has := p["met"].(bool)
		if !has {
			return nil
		}
		idx := fmt.Sprint(intOrNil(p, "index"))
		return UpsertSignal(ctx, tx, Signal{RunID: runID, Kind: SignalCriteria, Positive: met, Strength: "medium",
			Source: e.Source, Detail: "tiêu chí " + idx + ": " + str(p, "evidence"),
			DedupKey: "criteria:" + runID + ":" + str(p, "task_id") + ":" + idx, ObservedAt: ts})
	case spool.ReviewCompleted:
		if path := str(p, "path"); path != "" {
			if err := upsertArtifact(ctx, tx, runID, "task.review", path, ts); err != nil {
				return err
			}
		}
		if n, _ := numOrNil(p, "blocking").(float64); n > 0 {
			return UpsertSignal(ctx, tx, Signal{RunID: runID, Kind: SignalReview, Positive: false, Strength: "medium",
				Source: e.Source, Detail: fmt.Sprintf("%d phát hiện nghiêm trọng (vòng %v)", int(n), intOrNil(p, "round")),
				DedupKey: "review:" + e.EventID, ObservedAt: ts})
		}
	}
	return nil
}

// RunsForTask trả về các lượt chạy gắn với tác vụ, cũ nhất trước.
func RunsForTask(ctx context.Context, db *sql.DB, taskID string) ([]Run, error) {
	rows, err := db.QueryContext(ctx, runSelect+` WHERE r.task_id = ? ORDER BY r.started_at, r.run_id`, taskID)
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

// RunMeta là các trường workflow của một lượt chạy.
type RunMeta struct {
	TaskID             sql.NullString
	ParentRunID        sql.NullString
	CostUSD            sql.NullFloat64
	InputTokens        sql.NullInt64
	OutputTokens       sql.NullInt64
	ResultSummary      sql.NullString
	VerificationStatus sql.NullString
}

// GetRunMeta đọc các trường workflow của lượt chạy.
func GetRunMeta(ctx context.Context, db *sql.DB, runID string) (RunMeta, error) {
	var m RunMeta
	err := db.QueryRowContext(ctx, `SELECT task_id, parent_run_id, cost_usd, input_tokens, output_tokens, result_summary, verification_status
		FROM runs WHERE run_id = ?`, runID).Scan(&m.TaskID, &m.ParentRunID, &m.CostUSD, &m.InputTokens, &m.OutputTokens, &m.ResultSummary, &m.VerificationStatus)
	return m, err
}

// RunStep là một bước của lượt chạy.
type RunStep struct {
	StepID, Name, Status string
	StartedAt            sql.NullString
	FinishedAt           sql.NullString
}

// RunSteps trả về các bước của lượt chạy.
func RunSteps(ctx context.Context, db *sql.DB, runID string) ([]RunStep, error) {
	rows, err := db.QueryContext(ctx, `SELECT step_id, name, status, started_at, finished_at FROM run_steps
		WHERE run_id = ? ORDER BY COALESCE(started_at, finished_at), step_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunStep
	for rows.Next() {
		var s RunStep
		if err := rows.Scan(&s.StepID, &s.Name, &s.Status, &s.StartedAt, &s.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
