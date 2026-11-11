package evolvesvc

import (
	"context"
	"fmt"
	"io"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
)

// ProcessDeferred đánh dấu yêu cầu cho cơ hội mới đủ điều kiện rồi xử lý hàng đợi (mục 13.3).
// budget > 0: dùng agent evolver cho yêu cầu cần mô hình; ngược lại chỉ yêu cầu xác định. warn
// nhận cảnh báo khi một agent không dùng được (nil = bỏ). Không kiểm tra chỉ-dành-cho-người:
// người gọi (Process, hoặc tác vụ nền của `bint status` đã tự bỏ qua trong phiên agent) lo việc đó.
func ProcessDeferred(ctx context.Context, e *Workspace, warn io.Writer, budget float64, max int) (evolve.ProcessReport, []evolve.Request, error) {
	scan, h, err := e.scanData(ctx, 0, true)
	if err != nil {
		return evolve.ProcessReport{}, nil, err
	}
	now := time.Now()
	added, err := evolve.Enqueue(e.K.Paths.Runtime, scan, now)
	if err != nil {
		return evolve.ProcessReport{}, nil, err
	}
	warnf := func(f string, a ...any) {
		if warn != nil {
			fmt.Fprintf(warn, f, a...)
		}
	}
	var agent, reflector, hproposer eval.Agent
	if budget > 0 {
		if agent, err = EvolverAgent(e.K, "", ""); err != nil {
			warnf("! không dùng được agent evolver: %v\n", err)
		}
		if reflector, err = ReflectorAgent(e.K, "", ""); err != nil {
			warnf("! không dùng được agent reflector: %v\n", err)
		}
		if hproposer, err = HarnessProposerAgent(e.K, "", ""); err != nil {
			warnf("! không dùng được agent đề xuất quy trình: %v\n", err)
		}
	}
	// Tác vụ đánh giá và repository để kiểm rò rỉ cho bài học (như `bint evolve propose`).
	cases, err := e.Cases.Cases()
	if err != nil {
		return evolve.ProcessReport{}, nil, err
	}
	rep, err := evolve.ProcessRequests(ctx, evolve.ProcessInput{K: e.K, DB: e.Store.DB, Scan: scan, History: h, Agent: agent,
		Reflector: reflector, HarnessProposer: hproposer, Cases: cases, RepoDir: e.WS.Repo.Toplevel, BudgetUSD: budget, Max: max,
		Timeout: time.Duration(e.K.Config.Eval.TimeoutMinutes) * time.Minute, Now: now})
	return rep, added, err
}

// ---- queue ----

// QueueOptions: `bint evolve queue`.
type QueueOptions struct {
	All bool `flag:"all"`
}

func QueueDefaults(config.Config) QueueOptions { return QueueOptions{} }
func (o QueueOptions) Validate() error         { return nil }

// QueueResult: yêu cầu tiến hoá đã đánh dấu và chế độ thực thi hiện hành.
type QueueResult struct {
	Requests  []evolve.Request
	Execution string
}

// Queue liệt kê yêu cầu tiến hoá đã đánh dấu (mặc định: đang chờ).
func Queue(e *Workspace, opts QueueOptions) (*QueueResult, error) {
	var st []string
	if !opts.All {
		st = []string{evolve.ReqPending}
	}
	rs, err := evolve.ListRequests(e.K.Paths.Runtime, st...)
	if err != nil {
		return nil, err
	}
	return &QueueResult{Requests: rs, Execution: e.K.Config.Evolution.Execution}, nil
}

// ---- process ----

// ProcessOptions: `bint evolve process`.
type ProcessOptions struct {
	BudgetUSD float64 `flag:"budget"`
	Max       int     `flag:"max" form:"advanced"`
	// Warn nhận cảnh báo khi một agent không dùng được; nil = bỏ.
	Warn io.Writer
}

func ProcessDefaults(config.Config) ProcessOptions { return ProcessOptions{} }
func (o ProcessOptions) Validate() error           { return nil }

// ProcessResult: yêu cầu mới được đánh dấu và báo cáo xử lý hàng đợi.
type ProcessResult struct {
	Added  []evolve.Request
	Report evolve.ProcessReport
}

// Process đánh dấu và xử lý yêu cầu tiến hoá ngay (chế độ deferred; dùng được cả khi
// execution = manual). Chỉ dành cho người.
func Process(ctx context.Context, e *Workspace, opts ProcessOptions) (*ProcessResult, error) {
	if err := app.RequireHuman("bint evolve process"); err != nil {
		return nil, err
	}
	if !e.K.Config.Evolution.Enabled {
		return nil, fmt.Errorf("evolution.enabled = false trong cấu hình")
	}
	rep, added, err := ProcessDeferred(ctx, e, opts.Warn, opts.BudgetUSD, opts.Max)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Added: added, Report: rep}, nil
}
