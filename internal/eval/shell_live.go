package eval

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// RunShellLive chạy command trong dir như RunShell nhưng chuyển tiếp đầu ra (stdout và stderr) tới out theo thời gian
// thực thay vì ghi log; dùng cho lệnh người đang theo dõi (bint test affected --run). timeout <= 0 là không giới hạn.
func RunShellLive(ctx context.Context, dir, command string, timeout time.Duration, out io.Writer, env []string) CmdResult {
	start := time.Now()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	prog, args := shellCommand(command)
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout, cmd.Stderr = out, out
	prepareProcess(cmd)
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	res := CmdResult{Duration: time.Since(start)}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.TimedOut, res.ExitCode = true, -1
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
		if ctx.Err() != nil {
			res.Err = ctx.Err()
		}
	default:
		res.ExitCode, res.Err = -1, err
		if ctx.Err() != nil {
			res.Err = ctx.Err()
		}
	}
	return res
}
