package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Batch là một đợt chạy `bint eval run`.
type Batch struct {
	BatchID        string
	Status         string
	Harness        string
	HarnessVersion string
	RequestedModel string
	BintVersion    string
	LeakageMode    string
	Arms           []string
	Repeats        int
	CaseIDs        []string
	PromptVersion  int
	Seed           uint64
	BudgetUSD      *float64
	EstimatedUSD   *float64
	GitHead        string
	StartedAt      string
	FinishedAt     string
	Notes          []string
}

// AttemptRow là một lượt chạy (tác vụ × nhánh × lần lặp).
type AttemptRow struct {
	AttemptID       string
	BatchID         string
	CaseID          string
	CaseFingerprint string
	CaseSplit       string
	Arm             string
	Repeat          int
	Status          string
	Harness         string
	HarnessVersion  string
	Model           string
	ContextSource   string
	StartedAt       string
	FinishedAt      string
	DurationMS      *int64
	AgentDurationMS *int64
	AgentExitCode   *int64
	NumTurns        *int64
	InputTokens     *int64
	OutputTokens    *int64
	CostUSD         *float64
	SetupOK         *bool
	TestExitCode    *int64
	TestPassed      *bool
	FilesChanged    *int64
	LinesChanged    *int64
	TouchedTests    *bool
	Error           string
	LogDir          string
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullI(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullB(p *bool) any {
	if p == nil {
		return nil
	}
	if *p {
		return 1
	}
	return 0
}

func createBatch(ctx context.Context, db *sql.DB, b Batch) error {
	arms, _ := json.Marshal(b.Arms)
	ids, _ := json.Marshal(b.CaseIDs)
	notes, _ := json.Marshal(nonNil(b.Notes))
	_, err := db.ExecContext(ctx, `INSERT INTO eval_batches(batch_id, status, harness, harness_version, requested_model,
		bint_version, leakage_mode, arms, repeats, case_ids, prompt_version, seed, budget_usd, estimated_cost_usd,
		git_head, started_at, notes) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.BatchID, b.Status, b.Harness, nullStr(b.HarnessVersion), nullStr(b.RequestedModel), nullStr(b.BintVersion),
		b.LeakageMode, string(arms), b.Repeats, string(ids), b.PromptVersion, int64(b.Seed), nullF(b.BudgetUSD),
		nullF(b.EstimatedUSD), nullStr(b.GitHead), b.StartedAt, string(notes))
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func updateBatch(ctx context.Context, db *sql.DB, id, status, finishedAt string, notes []string) error {
	n, _ := json.Marshal(nonNil(notes))
	_, err := db.ExecContext(ctx, `UPDATE eval_batches SET status = ?, finished_at = ?, notes = ? WHERE batch_id = ?`,
		status, nullStr(finishedAt), string(n), id)
	return err
}

const batchCols = `batch_id, status, harness, harness_version, requested_model, bint_version, leakage_mode, arms,
	repeats, case_ids, prompt_version, seed, budget_usd, estimated_cost_usd, git_head, started_at, finished_at, notes`

func scanBatch(sc interface{ Scan(...any) error }) (Batch, error) {
	var b Batch
	var hv, rm, bv, gh, fin sql.NullString
	var budget, est sql.NullFloat64
	var arms, ids, notes string
	var seed int64
	err := sc.Scan(&b.BatchID, &b.Status, &b.Harness, &hv, &rm, &bv, &b.LeakageMode, &arms, &b.Repeats, &ids,
		&b.PromptVersion, &seed, &budget, &est, &gh, &b.StartedAt, &fin, &notes)
	if err != nil {
		return b, err
	}
	b.HarnessVersion, b.RequestedModel, b.BintVersion, b.GitHead, b.FinishedAt = hv.String, rm.String, bv.String, gh.String, fin.String
	b.Seed = uint64(seed)
	if budget.Valid {
		b.BudgetUSD = &budget.Float64
	}
	if est.Valid {
		b.EstimatedUSD = &est.Float64
	}
	json.Unmarshal([]byte(arms), &b.Arms)
	json.Unmarshal([]byte(ids), &b.CaseIDs)
	json.Unmarshal([]byte(notes), &b.Notes)
	return b, nil
}

// ErrNoBatch: chưa có đợt chạy nào.
var ErrNoBatch = errors.New("chưa có đợt đánh giá nào (chạy `bint eval run`)")

// GetBatch đọc một đợt theo ID, tiền tố ID, hoặc "latest"/"" cho đợt mới nhất.
func GetBatch(ctx context.Context, db *sql.DB, id string) (Batch, error) {
	var row *sql.Row
	if id == "" || id == "latest" {
		row = db.QueryRowContext(ctx, `SELECT `+batchCols+` FROM eval_batches ORDER BY started_at DESC, batch_id DESC LIMIT 1`)
	} else {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM eval_batches WHERE batch_id LIKE ? || '%'`, id).Scan(&n); err != nil {
			return Batch{}, err
		}
		if n > 1 {
			return Batch{}, fmt.Errorf("tiền tố %q khớp %d đợt", id, n)
		}
		row = db.QueryRowContext(ctx, `SELECT `+batchCols+` FROM eval_batches WHERE batch_id LIKE ? || '%'`, id)
	}
	b, err := scanBatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		if id == "" || id == "latest" {
			return b, ErrNoBatch
		}
		return b, fmt.Errorf("không tìm thấy đợt đánh giá %q", id)
	}
	return b, err
}

// ListBatches liệt kê các đợt, mới nhất trước.
func ListBatches(ctx context.Context, db *sql.DB, limit int) ([]Batch, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+batchCols+` FROM eval_batches ORDER BY started_at DESC, batch_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func startAttempt(ctx context.Context, db *sql.DB, a AttemptRow) error {
	// Lượt "running" còn sót từ một lần chạy bị ngắt sẽ được chạy lại.
	if _, err := db.ExecContext(ctx, `DELETE FROM evaluation_runs WHERE batch_id = ? AND case_id = ? AND arm = ? AND repeat_index = ?`,
		a.BatchID, a.CaseID, a.Arm, a.Repeat); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO evaluation_runs(attempt_id, batch_id, case_id, case_fingerprint, case_split,
		arm, repeat_index, status, harness, harness_version, started_at, log_dir) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.AttemptID, a.BatchID, a.CaseID, a.CaseFingerprint, nullStr(a.CaseSplit), a.Arm, a.Repeat, "running",
		a.Harness, nullStr(a.HarnessVersion), a.StartedAt, nullStr(a.LogDir))
	return err
}

func finishAttempt(ctx context.Context, db *sql.DB, a AttemptRow) error {
	_, err := db.ExecContext(ctx, `UPDATE evaluation_runs SET status=?, model=?, context_source=?, finished_at=?,
		duration_ms=?, agent_duration_ms=?, agent_exit_code=?, num_turns=?, input_tokens=?, output_tokens=?, cost_usd=?,
		setup_ok=?, test_exit_code=?, test_passed=?, files_changed=?, lines_changed=?, touched_tests=?, error=?
		WHERE attempt_id=?`,
		a.Status, nullStr(a.Model), nullStr(a.ContextSource), nullStr(a.FinishedAt), nullI(a.DurationMS),
		nullI(a.AgentDurationMS), nullI(a.AgentExitCode), nullI(a.NumTurns), nullI(a.InputTokens), nullI(a.OutputTokens),
		nullF(a.CostUSD), nullB(a.SetupOK), nullI(a.TestExitCode), nullB(a.TestPassed), nullI(a.FilesChanged),
		nullI(a.LinesChanged), nullB(a.TouchedTests), nullStr(a.Error), a.AttemptID)
	return err
}

const attemptCols = `attempt_id, batch_id, case_id, case_fingerprint, case_split, arm, repeat_index, status, harness,
	harness_version, model, context_source, started_at, finished_at, duration_ms, agent_duration_ms, agent_exit_code,
	num_turns, input_tokens, output_tokens, cost_usd, setup_ok, test_exit_code, test_passed, files_changed,
	lines_changed, touched_tests, error, log_dir`

// Attempts đọc các lượt của một đợt (where rỗng = mọi đợt).
func Attempts(ctx context.Context, db *sql.DB, where string, args ...any) ([]AttemptRow, error) {
	q := `SELECT ` + attemptCols + ` FROM evaluation_runs`
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY case_id, arm, repeat_index"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AttemptRow
	for rows.Next() {
		var a AttemptRow
		var split, hv, model, src, fin, errS, logDir sql.NullString
		var dur, adur, aexit, turns, in, outT, texit, files, lines sql.NullInt64
		var cost sql.NullFloat64
		var setup, passed, touched sql.NullBool
		if err := rows.Scan(&a.AttemptID, &a.BatchID, &a.CaseID, &a.CaseFingerprint, &split, &a.Arm, &a.Repeat,
			&a.Status, &a.Harness, &hv, &model, &src, &a.StartedAt, &fin, &dur, &adur, &aexit, &turns, &in, &outT,
			&cost, &setup, &texit, &passed, &files, &lines, &touched, &errS, &logDir); err != nil {
			return nil, err
		}
		a.CaseSplit, a.HarnessVersion, a.Model, a.ContextSource = split.String, hv.String, model.String, src.String
		a.FinishedAt, a.Error, a.LogDir = fin.String, errS.String, logDir.String
		a.DurationMS, a.AgentDurationMS, a.AgentExitCode = ip(dur), ip(adur), ip(aexit)
		a.NumTurns, a.InputTokens, a.OutputTokens = ip(turns), ip(in), ip(outT)
		a.TestExitCode, a.FilesChanged, a.LinesChanged = ip(texit), ip(files), ip(lines)
		if cost.Valid {
			a.CostUSD = &cost.Float64
		}
		a.SetupOK, a.TestPassed, a.TouchedTests = bp(setup), bp(passed), bp(touched)
		out = append(out, a)
	}
	return out, rows.Err()
}

func ip(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func bp(n sql.NullBool) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Bool
	return &v
}

// batchSpent trả về tổng chi phí đã biết của đợt và số lượt có chi phí.
func batchSpent(ctx context.Context, db *sql.DB, batchID string) (float64, int, error) {
	var s sql.NullFloat64
	var n int
	err := db.QueryRowContext(ctx, `SELECT SUM(cost_usd), COUNT(cost_usd) FROM evaluation_runs
		WHERE batch_id = ? AND status <> 'running'`, batchID).Scan(&s, &n)
	return s.Float64, n, err
}

// HistoricalCost trả về chi phí trung bình mỗi lượt (đã có chi phí) của harness,
// lọc theo mô hình nếu khác rỗng, cùng số lượt làm căn cứ.
func HistoricalCost(ctx context.Context, db *sql.DB, harness, model string) (float64, int, error) {
	q := `SELECT AVG(cost_usd), COUNT(cost_usd) FROM evaluation_runs WHERE harness = ? AND cost_usd IS NOT NULL
		AND status IN ('passed','failed','timeout')`
	args := []any{harness}
	if model != "" {
		q += ` AND (model = ? OR model LIKE ? || ',%' OR model LIKE '%' || ? || '%')`
		args = append(args, model, model, model)
	}
	var avg sql.NullFloat64
	var n int
	err := db.QueryRowContext(ctx, q, args...).Scan(&avg, &n)
	return avg.Float64, n, err
}
