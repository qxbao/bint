package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
)

// Env gom những gì khung đánh giá cần về repository hiện tại.
type Env struct {
	RepoDir  string // thư mục gốc worktree của người dùng (chỉ đọc)
	WorkRoot string // <state>/eval-work
	Cfg      config.Config
}

// ErrNoTestCommand: người dùng chưa xác nhận lệnh kiểm thử.
var ErrNoTestCommand = errors.New("chưa xác nhận lệnh kiểm thử: điền eval.test_command trong .bint/config.yml (Bint không chạy lệnh đề xuất khi chưa được xác nhận)")

// TestCommand trả về lệnh chấm của tác vụ (đã thay {test_files}/{test_dirs}).
func (e Env) TestCommand(c *Case) (string, error) {
	cmd := strings.TrimSpace(c.TestCommand)
	if cmd == "" && e.Cfg.Eval.TestCommand != nil {
		cmd = strings.TrimSpace(*e.Cfg.Eval.TestCommand)
	}
	if cmd == "" {
		return "", ErrNoTestCommand
	}
	return ExpandTestCommand(cmd, c.TestFiles), nil
}

func (e Env) setupCommand() string {
	if e.Cfg.Eval.SetupCommand == nil {
		return ""
	}
	return strings.TrimSpace(*e.Cfg.Eval.SetupCommand)
}

func (e Env) timeout() time.Duration { return time.Duration(e.Cfg.Eval.TimeoutMinutes) * time.Minute }

// VerifyCase kiểm chứng tác vụ theo mục 12.2: tại commit gốc, áp tệp kiểm thử của
// commit thì kiểm thử phải THẤT BẠI; áp thêm toàn bộ thay đổi của commit thì phải
// QUA (afterRuns lần liên tiếp, để bắt kiểm thử chập chờn).
func VerifyCase(ctx context.Context, env Env, c *Case, afterRuns int, now time.Time) *Verification {
	if afterRuns < 1 {
		afterRuns = 1
	}
	v := &Verification{AfterRuns: afterRuns, VerifiedAt: nowString(now)}
	cmd, err := env.TestCommand(c)
	if err != nil {
		v.Status, v.Detail = "error", err.Error()
		return v
	}
	v.TestCommand = cmd
	dir := filepath.Join(env.WorkRoot, "verify", c.ID)
	logPath := filepath.Join(env.WorkRoot, "verify", c.ID+".log")
	os.Remove(logPath)
	defer os.RemoveAll(dir)

	fail := func(status, format string, a ...any) *Verification {
		v.Status, v.Detail = status, fmt.Sprintf(format, a...)
		if v.Status != "verified" {
			v.Detail += " (log: " + logPath + ")"
		}
		return v
	}
	if err := prepareWorkdir(ctx, env.RepoDir, env.WorkRoot, c.Source.Base, dir); err != nil {
		return fail("error", "dựng thư mục làm việc: %v", err)
	}
	if s := env.setupCommand(); s != "" {
		if r := RunShell(ctx, dir, s, env.timeout(), logPath, cleanEnv()); !r.OK() {
			return fail("error", "lệnh dựng môi trường thất bại (mã %d)", r.ExitCode)
		}
	}
	if err := copyFilesFromCommit(ctx, env.RepoDir, c.Source.Commit, c.TestFiles, dir); err != nil {
		return fail("error", "áp tệp kiểm thử: %v", err)
	}
	before := RunShell(ctx, dir, cmd, env.timeout(), logPath, cleanEnv())
	if ctx.Err() != nil {
		return fail("error", "bị huỷ")
	}
	if before.Err != nil {
		return fail("error", "không chạy được lệnh kiểm thử: %v", before.Err)
	}
	v.FailBefore = !before.OK()
	if !v.FailBefore {
		return fail("failed", "kiểm thử của commit đã QUA tại commit cha: tác vụ không phân biệt được lời giải đúng")
	}
	all := slices.Concat(c.SourceFiles, c.AuxFiles)
	if err := copyFilesFromCommit(ctx, env.RepoDir, c.Source.Commit, all, dir); err != nil {
		return fail("error", "áp lời giải: %v", err)
	}
	for i := 0; i < afterRuns; i++ {
		r := RunShell(ctx, dir, cmd, env.timeout(), logPath, cleanEnv())
		if ctx.Err() != nil {
			return fail("error", "bị huỷ")
		}
		if !r.OK() {
			if i == 0 {
				return fail("failed", "kiểm thử KHÔNG qua với lời giải thật (mã %d, timeout=%v)", r.ExitCode, r.TimedOut)
			}
			return fail("failed", "kiểm thử chập chờn: qua %d/%d lần với lời giải thật", i, i+1)
		}
	}
	v.PassAfter = true
	v.Status = "verified"
	os.Remove(logPath)
	return v
}
