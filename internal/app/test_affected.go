package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
// tệp chưa theo dõi. Với Run, chạy affected_command (tầng graph/package) hoặc eval.test_command (tầng full).
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
	res.Selection = workflow.SelectAffected(ctx, root, commit)
	cfg := ws.Config
	vc := cfg.Workflow.Verify
	sel := res.Selection
	switch sel.Scope {
	case affected.TierNone:
	case affected.TierFull:
		if tc := cfg.Eval.TestCommand; tc != nil {
			res.Command = strings.TrimSpace(*tc)
		}
	default:
		if strings.TrimSpace(vc.AffectedCommand) != "" {
			res.Command = eval.ExpandTestCommand(strings.TrimSpace(vc.AffectedCommand), sel.TestFiles)
		}
	}
	if !o.Run || sel.Scope == affected.TierNone {
		return res, nil
	}
	if res.Command == "" {
		if sel.Scope == affected.TierFull {
			return res, errors.New("phạm vi full nhưng chưa có eval.test_command để chạy toàn bộ kiểm thử")
		}
		return res, errors.New("chưa đặt workflow.verify.affected_command để chạy test bị ảnh hưởng")
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
