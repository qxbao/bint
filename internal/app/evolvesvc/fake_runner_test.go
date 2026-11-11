package evolvesvc

import (
	"context"
	"sync"
)

// fakeRunner là CycleRunner giả cho kiểm thử: ghi lại các lệnh gọi và trả kết quả dựng sẵn.
type fakeRunner struct {
	mu       sync.Mutex
	calls    []string
	evaluate func(opts EvaluateOptions) (*EvaluateResult, error)
	propose  func(opts ProposeOptions) (*ProposeResult, error)
}

func (f *fakeRunner) Evaluate(_ context.Context, opts EvaluateOptions) (*EvaluateResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "evaluate "+opts.ID+" "+opts.Split)
	f.mu.Unlock()
	if f.evaluate != nil {
		return f.evaluate(opts)
	}
	return &EvaluateResult{}, nil
}

func (f *fakeRunner) Propose(_ context.Context, opts ProposeOptions) (*ProposeResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "propose")
	f.mu.Unlock()
	if f.propose != nil {
		return f.propose(opts)
	}
	return &ProposeResult{}, nil
}
