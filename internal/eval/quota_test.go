package eval

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestClassifyInfra(t *testing.T) {
	for _, tc := range []struct {
		status       int
		term, result string
		infra, quota bool
	}{
		{429, "api_error", "You've hit your 5-hour limit · resets 1am", true, true},
		{0, "", "You've hit your weekly limit", true, true},
		{529, "api_error", "Overloaded", true, false},
		{0, "", "Lỗi cú pháp trong tệp", false, false},
	} {
		reason, q := classifyInfra(tc.status, tc.term, tc.result)
		if (reason != "") != tc.infra || q != tc.quota {
			t.Errorf("%+v → %q, %v", tc, reason, q)
		}
	}
}

// Agent hết hạn mức: lượt không bị chấm thành trượt, đợt dừng (interrupted) và --resume
// chạy lại đúng các lượt hỏng vì hạ tầng.
func TestRunQuotaStopsAndResumes(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, _ := st.Candidates()
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	agentDir := t.TempDir()
	script := filepath.Join(agentDir, "agent.sh")
	writeFiles(t, agentDir, map[string]string{"agent.sh": `echo '{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"terminal_reason":"api_error","result":"You'"'"'ve hit your 5-hour limit","total_cost_usd":0}'` + "\n"})
	agent := &CommandAgent{Command: "sh " + filepath.ToSlash(script)}

	out, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 3, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: agent, Seed: 1, Now: testNow,
	})
	if !errors.Is(err, ErrQuota) || out.Status != "interrupted" {
		t.Fatalf("mong đợi dừng vì hết hạn mức, được %+v err=%v", out, err)
	}
	rows, _ := Attempts(ctx, db, "batch_id = ?", out.BatchID)
	if len(rows) != 1 || rows[0].Status != "error" || !strings.HasPrefix(rows[0].Error, InfraErrorPrefix) || rows[0].TestPassed != nil {
		t.Fatalf("mong đợi 1 lượt lỗi hạ tầng, không chấm: %+v", rows)
	}

	writeFiles(t, agentDir, map[string]string{"agent.sh": "echo '{\"type\":\"result\",\"total_cost_usd\":0.1}'\n"})
	out, err = Run(ctx, env, db, RunOptions{Cases: []*Case{good}, ResumeBatch: out.BatchID, Parallel: 1, Agent: agent, Now: testNow})
	if err != nil || out.Status != "completed" {
		t.Fatalf("tiếp tục: %+v err=%v", out, err)
	}
	rows, _ = Attempts(ctx, db, "batch_id = ?", out.BatchID)
	if len(rows) != 3 {
		t.Fatalf("mong đợi 3 lượt sau khi tiếp tục, có %d", len(rows))
	}
	for _, r := range rows {
		if r.Status == "error" {
			t.Fatalf("lượt lỗi hạ tầng phải được chạy lại: %+v", r)
		}
	}
}

// Huỷ ctx trong lúc chuẩn bị (kiểm tra commit gốc, BuildLeakInput) ghi đợt là
// cancelled chứ không phải failed.
func TestRunCancelDuringPrepareMarksCancelled(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(context.Background(), root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, _ := st.Candidates()
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batch string
	res, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(id string) error { batch = id; cancel(); return nil },
	})
	// Lệnh git bị giết vì huỷ chỉ báo "exit status 1" trên Windows: lỗi vẫn phải nhận được là huỷ, và res.Status phải
	// khớp trạng thái đã lưu, để TUI hiện "đã huỷ" như khi huỷ giữa các lượt.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lỗi phải nhận được là huỷ: %v", err)
	}
	if res.BatchID != batch || res.Status != "cancelled" {
		t.Fatalf("cancelled preparation lost batch result: %+v; want batch %s", res, batch)
	}
	var status string
	if qerr := db.QueryRow("SELECT status FROM eval_batches WHERE batch_id = ?", batch).Scan(&status); qerr != nil {
		t.Fatal(qerr)
	}
	if status != "cancelled" {
		t.Fatalf("trạng thái đợt = %q, muốn cancelled", status)
	}
}

func goodCase(t *testing.T) (*Env, *sql.DB, *Case) {
	t.Helper()
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(context.Background(), root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, _ := st.Candidates()
	for _, c := range cands {
		if c.Source.Commit == solution {
			return &env, db, c
		}
	}
	t.Fatal("không có ứng viên")
	return nil, nil, nil
}

func batchStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var status string
	if err := db.QueryRow("SELECT status FROM eval_batches WHERE batch_id = ?", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// Huỷ sau khi đợt được tạo: Status cancelled, lỗi là context.Canceled (kể cả khi lỗi gốc là lỗi khác), đợt cancelled.
func TestRunCancelAfterBatchReturnsCancelled(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	env, db, good := goodCase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batch string
	res, err := Run(ctx, *env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(id string) error { batch = id; cancel(); return errors.New("lỗi khác do huỷ") },
	})
	if !errors.Is(err, context.Canceled) || res.Status != "cancelled" {
		t.Fatalf("status=%q err=%v", res.Status, err)
	}
	if batchStatus(t, db, batch) != "cancelled" {
		t.Fatal("đợt phải cancelled")
	}

	// Huỷ ngay sau OnBatch (nhánh chuẩn bị tiếp theo): kết quả mang BatchID.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	res, err = Run(ctx2, *env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(id string) error { cancel2(); return nil },
	})
	if !errors.Is(err, context.Canceled) || res.Status != "cancelled" || res.BatchID == "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if batchStatus(t, db, res.BatchID) != "cancelled" {
		t.Fatal("đợt phải cancelled")
	}
}

// Lỗi thật khi ctx còn sống: failed.
func TestRunRealPrepareErrorIsFailed(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	env, db, good := goodCase(t)
	var batch string
	res, err := Run(context.Background(), *env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(id string) error { batch = id; return errors.New("boom") },
	})
	if err == nil || errors.Is(err, context.Canceled) || res.Status != "failed" {
		t.Fatalf("status=%q err=%v", res.Status, err)
	}
	if batchStatus(t, db, batch) != "failed" {
		t.Fatal("đợt phải failed")
	}
}

// Huỷ trước khi tạo đợt: không tạo đợt, BatchID rỗng.
func TestRunCancelledBeforeBatchCreatesNone(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	env, db, good := goodCase(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, *env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
	})
	if !errors.Is(err, context.Canceled) || res.BatchID != "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM eval_batches").Scan(&n)
	if n != 0 {
		t.Fatalf("không được tạo đợt, có %d", n)
	}
}

func TestRunCancelledPreparationReturnsContextError(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(context.Background(), root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, err := st.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batch string
	result, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(id string) error {
			batch = id
			cancel()
			return errors.New("preparation interrupted")
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation returned %v, want context.Canceled", err)
	}
	if result.BatchID != batch || result.Status != "cancelled" {
		t.Fatalf("cancelled preparation lost batch result: %+v; want batch %s, status cancelled", result, batch)
	}
	var status string
	if err := db.QueryRow("SELECT status FROM eval_batches WHERE batch_id = ?", batch).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("batch status = %q, want cancelled", status)
	}
}

func TestRunPreparationReportsPersistenceFailure(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(context.Background(), root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, err := st.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	preparationErr := errors.New("preparation failed")
	_, err = Run(context.Background(), env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 1, Parallel: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "true"}, Seed: 1, Now: testNow,
		OnBatch: func(string) error {
			_, err := db.Exec(`CREATE TRIGGER fail_batch_update BEFORE UPDATE ON eval_batches
				BEGIN SELECT RAISE(FAIL, 'fixture persistence failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
			return preparationErr
		},
	})
	if !errors.Is(err, preparationErr) || !strings.Contains(err.Error(), "fixture persistence failure") {
		t.Fatalf("must report preparation and batch persistence failures: %v", err)
	}
}
