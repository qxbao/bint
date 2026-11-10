package eval

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/testutil"
)

func TestPlanByRepeatOrder(t *testing.T) {
	cases := []*Case{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	arms := []string{"base", "candidate"}
	p := PlanByRepeat(cases, arms, 4, 11)
	if len(p) != 3*2*4 {
		t.Fatalf("số lượt %d", len(p))
	}
	per := 3 * 2
	for i, a := range p {
		if want := i/per + 1; a.Repeat != want {
			t.Fatalf("lượt %d thuộc lần lặp %d, mong đợi %d (lần lặp phải chạy tuần tự)", i, a.Repeat, want)
		}
	}
	// Trong một lần lặp, mọi cặp (tác vụ, nhánh) có đủ; và xáo trộn xác định theo hạt giống.
	seen := map[string]bool{}
	for _, a := range p[:per] {
		seen[a.Case.ID+a.Arm] = true
	}
	if len(seen) != per {
		t.Fatalf("lần lặp 1 không đủ ô: %v", seen)
	}
	q := PlanByRepeat(cases, arms, 4, 11)
	for i := range p {
		if p[i].key() != q[i].key() {
			t.Fatal("cùng hạt giống phải cho cùng thứ tự (cần cho --resume)")
		}
	}
}

// slowAgent đếm số lượt đang chạy đồng thời và độ trễ, để lượt của các lần lặp trộn nhau
// khi chạy song song.
type slowAgent struct {
	started atomic.Int64
}

func (a *slowAgent) Name() string                   { return "slow" }
func (a *slowAgent) Version(context.Context) string { return "" }
func (a *slowAgent) ReportsCost() bool              { return false }
func (a *slowAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	n := a.started.Add(1)
	time.Sleep(time.Duration(5+n%4*4) * time.Millisecond)
	return AgentResult{}
}

// Look phải được gọi đúng một lần cho mỗi lần lặp, theo thứ tự, và chỉ sau khi mọi lượt
// của lần lặp đó đã ghi vào cơ sở dữ liệu — kể cả khi chạy song song.
func TestRunLookBoundariesParallel(t *testing.T) {
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
	var mu sync.Mutex
	var calls []int
	var batch string
	opts := RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone, ArmMinimal}, Repeats: 10, LeakageMode: LeakEconomy,
		Agent: &slowAgent{}, Parallel: 2, Seed: 3, Now: testNow,
		OnBatch: func(id string) error { batch = id; return nil },
	}
	opts.Look = func(k int) (bool, string) {
		mu.Lock()
		calls = append(calls, k)
		mu.Unlock()
		rows, err := Attempts(ctx, db, "batch_id = ? AND repeat_index = ?", batch, k)
		if err != nil {
			t.Error(err)
		}
		finished := 0
		for _, r := range rows {
			if r.Status != "running" {
				finished++
			}
		}
		if finished != 2 {
			t.Errorf("Look(%d) gọi khi mới xong %d/2 lượt", k, finished)
		}
		return k == 2, "đủ bằng chứng"
	}
	out, err := Run(ctx, env, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != 1 || calls[1] != 2 {
		t.Fatalf("Look được gọi: %v", calls)
	}
	if out.Status != StatusStoppedEarly || out.StopReason != "đủ bằng chứng" {
		t.Fatalf("trạng thái %q (%q)", out.Status, out.StopReason)
	}
	// Dừng ở ranh giới 2: lần lặp 3-4 không bắt đầu (chỉ lần lặp ≤ 2 hoặc vài lượt đã lỡ chạy song song).
	rows, _ := Attempts(ctx, db, "batch_id = ? AND repeat_index > 2", out.BatchID)
	if len(rows) > opts.Parallel {
		t.Fatalf("còn %d lượt ở lần lặp > 2", len(rows))
	}
	if b, _ := GetBatch(ctx, db, out.BatchID); StoppedEarlyReason(b) == "" {
		t.Fatalf("thiếu ghi chú dừng sớm: %v", b.Notes)
	}

	// Tiếp tục: các lần lặp đã xong được nhìn lại theo thứ tự; Look(2) vẫn dừng.
	calls = nil
	opts.ResumeBatch = out.BatchID
	opts.OnBatch = nil
	out2, err := Run(ctx, env, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) < 2 || calls[0] != 1 || calls[1] != 2 || out2.Status != StatusStoppedEarly {
		t.Fatalf("resume: calls=%v status=%s", calls, out2.Status)
	}
}
