package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// CmdResult là kết quả chạy một lệnh.
type CmdResult struct {
	ExitCode int
	Duration time.Duration
	TimedOut bool
	Tail     string // phần cuối đầu ra (stdout+stderr)
	Err      error  // lỗi khởi chạy (không phải mã thoát khác 0)
}

// OK cho biết lệnh chạy xong với mã thoát 0.
func (r CmdResult) OK() bool { return r.Err == nil && !r.TimedOut && r.ExitCode == 0 }

const tailBytes = 4096

// shellCommand trả về chương trình và đối số để chạy một dòng lệnh shell.
// Trên Windows ưu tiên sh (Git for Windows) để lệnh viết kiểu POSIX chạy giống
// nhau trên mọi máy; không có thì dùng cmd.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		if sh, err := exec.LookPath("sh"); err == nil {
			return sh, []string{"-c", command}
		}
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// RunShell chạy command trong dir với giới hạn thời gian. Toàn bộ đầu ra được ghi
// vào logPath (nếu khác rỗng). Hết giờ thì giết cả cây tiến trình.
func RunShell(ctx context.Context, dir, command string, timeout time.Duration, logPath string, env []string) CmdResult {
	prog, args := shellCommand(command)
	return runProcess(ctx, dir, prog, args, timeout, logPath, env, nil, nil)
}

// runProcess chạy prog. Nếu stdout khác nil, stdout của tiến trình được giữ riêng
// vào đó (vẫn được ghi vào log).
func runProcess(ctx context.Context, dir, prog string, args []string, timeout time.Duration, logPath string, env []string, stdin io.Reader, stdout *bytes.Buffer) CmdResult {
	start := time.Now()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = stdin
	prepareProcess(cmd)
	cmd.WaitDelay = 5 * time.Second

	tail := &tailBuffer{max: tailBytes}
	writers := []io.Writer{tail}
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
			if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				defer f.Close()
				fmt.Fprintf(f, "$ %s %s\n", filepath.Base(prog), strings.Join(quoteArgs(args), " "))
				writers = append(writers, f)
			}
		}
	}
	combined := io.MultiWriter(writers...)
	if stdout != nil {
		cmd.Stdout = io.MultiWriter(stdout, combined)
	} else {
		cmd.Stdout = combined
	}
	cmd.Stderr = combined

	err := cmd.Run()
	res := CmdResult{Duration: time.Since(start), Tail: tail.String()}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		return res
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
		if ctx.Err() != nil {
			res.Err = ctx.Err()
		}
	default:
		res.ExitCode = -1
		res.Err = err
	}
	return res
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if len(a) > 200 {
			a = a[:200] + "…"
		}
		if strings.ContainsAny(a, " \t\n\"'") {
			a = fmt.Sprintf("%q", a)
		}
		out[i] = a
	}
	return out
}

type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
	}
	return string(b)
}

// cleanEnv trả về môi trường của tiến trình hiện tại, bỏ các biến khiến tiến
// trình con nhận nhầm repository hoặc nhận là đang chạy lồng trong phiên khác.
func cleanEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT",
			"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "BINT_EVAL_CONTEXT",
			// Định danh lượt chạy/tác vụ của phiên chủ: lượt eval không thuộc tác vụ đó.
			"BINT_RUN_ID", "BINT_TASK_ID", "BINT_APPROVED_PATHS", "CLAUDE_CODE_SESSION_ID":
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// verifyStripped là biến định danh theo lượt/tác vụ và biến trỏ repository mà lệnh kiểm chứng không được thấy: tiến
// trình con (go test dựng repo tạm, gọi lại bint...) sẽ nhận nhầm là đang chạy trong lượt/tác vụ của phiên chủ, hoặc
// đọc nhầm kho git của worktree. Biến đánh dấu agent (BINT_AGENT, CLAUDECODE, AI_AGENT, CODEX_SANDBOX, GEMINI_CLI)
// được GIỮ để thao tác chỉ dành cho người vẫn bị từ chối trong kiểm thử.
var verifyStripped = map[string]bool{
	"BINT_RUN_ID": true, "BINT_TASK_ID": true, "BINT_APPROVED_PATHS": true, "BINT_RUNNER_PID": true,
	"CLAUDE_CODE_SESSION_ID": true, "CLAUDE_CODE_SSE_PORT": true,
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true, "GIT_COMMON_DIR": true,
}

// VerifyEnv trả môi trường của tiến trình hiện tại cho lệnh kiểm chứng (Env.VerifyWith): bỏ biến định danh theo lượt,
// giữ biến đánh dấu agent.
func VerifyEnv(extra ...string) []string { return VerifyEnvFrom(os.Environ(), extra...) }

// VerifyEnvFrom như VerifyEnv nhưng lọc từ môi trường environ cho trước (kiểm thử tầng workflow không phải đặt biến
// GIT_* thật vào tiến trình kiểm thử, vì chúng làm hỏng chính git của harness).
func VerifyEnvFrom(environ []string, extra ...string) []string {
	var out []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if verifyStripped[strings.ToUpper(k)] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}
