package eval

import (
	"context"
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
