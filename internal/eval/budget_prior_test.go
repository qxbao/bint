package eval

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// F13: chưa có lịch sử chi phí thì dùng giá trị tiên nghiệm, không bắt đầu lượt nào
// khi ngân sách còn lại nhỏ hơn chi phí ước tính của lượt đó.
func TestRunBudgetPrior(t *testing.T) {
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
	writeFiles(t, agentDir, map[string]string{"agent.sh": "echo '{\"type\":\"result\",\"total_cost_usd\":1.0}'\n"})
	agent := &CommandAgent{Command: "sh " + filepath.ToSlash(filepath.Join(agentDir, "agent.sh"))}

	// Ngân sách $0,01 < tiên nghiệm $0,50: không lượt nào được chạy.
	tiny := 0.01
	out, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 2, LeakageMode: LeakEconomy,
		Agent: agent, BudgetUSD: &tiny, PriorRunUSD: 0.5, Seed: 1, Now: testNow,
	})
	if err == nil || out.Status != "budget_exhausted" || !errors.Is(err, ErrBudget) {
		t.Fatalf("mong đợi dừng vì ngân sách, được %+v err=%v", out, err)
	}
	if !strings.Contains(err.Error(), "chưa đo được") || !strings.Contains(err.Error(), "0.50") {
		t.Fatalf("thông báo thiếu lý do: %v", err)
	}
	if rows, _ := Attempts(ctx, db, "batch_id = ?", out.BatchID); len(rows) != 0 {
		t.Fatalf("không được chạy lượt nào, có %d", len(rows))
	}

	// Ngân sách $0,60 đủ cho lượt đầu theo tiên nghiệm; sau đó dùng chi phí đo được ($1,00).
	ok := 0.6
	out, err = Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 2, LeakageMode: LeakEconomy,
		Agent: agent, BudgetUSD: &ok, PriorRunUSD: 0.5, Seed: 1, Now: testNow,
	})
	if err == nil || out.Status != "budget_exhausted" {
		t.Fatalf("mong đợi dừng sau lượt đầu, được %+v err=%v", out, err)
	}
	if rows, _ := Attempts(ctx, db, "batch_id = ?", out.BatchID); len(rows) != 1 {
		t.Fatalf("mong đợi đúng 1 lượt, có %d", len(rows))
	}
}
