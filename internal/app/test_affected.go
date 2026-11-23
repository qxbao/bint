package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/affected"
	"bint/internal/eval"
	"bint/internal/gitrepo"
	"bint/internal/workflow"
)

// TestAffectedOptions: `bint test affected`.
type TestAffectedOptions struct {
	// Base là ref/commit gốc để so thay đổi (mặc định HEAD: chỉ thay đổi chưa commit và tệp chưa theo dõi).
	Base string
	// Run chạy luôn lệnh kiểm thử đã chọn.
	Run bool
	// Out nhận đầu ra của lệnh khi Run.
	Out io.Writer
}

// TestAffectedResult là kết quả chọn (và chạy) kiểm thử bị ảnh hưởng.
type TestAffectedResult struct {
	Selection affected.Selection
	// Command là lệnh sẽ chạy/đã chạy ("" khi không chạy gì: scope none).
	Command string
	// Ran cho biết Command đã được chạy (Run); ExitCode là mã thoát của nó.
	Ran      bool
	ExitCode int
}

// TestAffected chọn kiểm thử bị ảnh hưởng bởi thay đổi của cây làm việc so với o.Base. Thay đổi gồm cả chưa commit và
// tệp chưa theo dõi. Phần chọn và lệnh chạy do bgraph dựng; Run chạy lệnh đó, hoặc eval.test_command khi bgraph đòi chạy cả bộ.
func TestAffected(ctx context.Context, dir string, o TestAffectedOptions) (TestAffectedResult, error) {
	var res TestAffectedResult
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return res, err
	}
	root := ws.Repo.Toplevel
	base := strings.TrimSpace(o.Base)
	if base == "" {
		base = "HEAD"
	}
	commit, err := gitrepo.Run(ctx, root, "rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil || strings.TrimSpace(commit) == "" {
		return res, Usagef("--base %q không phải commit hoặc ref hợp lệ", base)
	}
	commit = strings.TrimSpace(commit)
	// Gốc so sánh là điểm tách khỏi HEAD: thay đổi của nhánh, không lẫn thay đổi mới của nhánh gốc.
	if mb, err := gitrepo.Run(ctx, root, "merge-base", commit, "HEAD"); err == nil && strings.TrimSpace(mb) != "" {
		commit = strings.TrimSpace(mb)
	}
	cfg := ws.Config
	tasksDir := ""
	if ws.Paths.Initialized() {
		tasksDir = filepath.Join(ws.Paths.Runtime, "tasks")
	}
	res.Selection = workflow.SelectAffected(ctx, root, commit, workflow.SelectOpts{BintConfigFull: cfg.Workflow.Verify.BintConfigFull, TasksDir: tasksDir})
	sel := res.Selection
	switch sel.Scope {
	case affected.TierNone:
	case affected.TierFull:
		// bgraph đòi chạy cả bộ (lý do trong Selection.Widening): lệnh test của dự án.
		if tc := cfg.Eval.TestCommand; tc != nil {
			res.Command = strings.TrimSpace(*tc)
		}
	default:
		res.Command = sel.Script()
	}
	if !o.Run || sel.Scope == affected.TierNone {
		return res, nil
	}
	if res.Command == "" {
		return res, errors.New("bgraph đòi chạy cả bộ test nhưng chưa có eval.test_command")
	}
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	timeout := time.Duration(cfg.Workflow.VerifyTimeoutMinutes) * time.Minute
	r := eval.RunShellLive(ctx, root, res.Command, timeout, out, os.Environ())
	if r.Err != nil && ctx.Err() != nil {
		return res, ctx.Err()
	}
	res.Ran, res.ExitCode = true, r.ExitCode
	if r.TimedOut {
		return res, fmt.Errorf("lệnh kiểm thử quá giờ sau %d phút", cfg.Workflow.VerifyTimeoutMinutes)
	}
	if r.Err != nil {
		return res, r.Err
	}
	return res, nil
}
