package mcpgw

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// lockHeld cho biết có daemon đang giữ khoá trong dir không (thử lấy khoá rồi nhả ngay).
func lockHeld(dir string) bool {
	f, err := os.OpenFile(filepath.Join(dir, LockFileName), os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	busy, err := tryLock(f)
	return err == nil && busy
}

// QueryStatus lấy trạng thái daemon đang chạy trong dir; ErrNotRunning nếu chưa có hoặc gateway.json là của daemon đã chết.
func QueryStatus(ctx context.Context, dir string) (Status, Info, error) {
	c, err := Connect(dir)
	if err != nil {
		return Status{}, Info{}, err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return Status{}, c.Info, err
	}
	return st, c.Info, nil
}

// StartDetached khởi động daemon tách rời bằng `exe args...` (tiến trình con chạy Run), ghi nhật ký vào gateway.log và chờ
// gateway.json xuất hiện. started=false nếu đã có daemon chạy (trả Info của nó).
func StartDetached(ctx context.Context, dir, exe string, args, env []string) (info Info, started bool, err error) {
	if _, i, qerr := QueryStatus(ctx, dir); qerr == nil {
		return i, false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, false, err
	}
	logPath := filepath.Join(dir, LogFileName)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Info{}, false, err
	}
	defer lf.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = lf, lf
	if env != nil {
		cmd.Env = env
	}
	detachCmd(cmd)
	if err := cmd.Start(); err != nil {
		return Info{}, false, fmt.Errorf("khởi động %s: %w", exe, err)
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }() // thu dọn nếu con thoát sớm; daemon thật sống tiếp sau khi ta thoát
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Info{}, false, ctx.Err()
		case <-exited:
			// Con thoát: có thể daemon khác đã giữ khoá.
			if _, i, qerr := QueryStatus(ctx, dir); qerr == nil {
				return i, false, nil
			}
			return Info{}, false, fmt.Errorf("daemon thoát ngay sau khi khởi động; xem %s%s", logPath, logTail(logPath))
		case <-time.After(50 * time.Millisecond):
		}
		if i, err := ReadInfo(dir); err == nil && i.PID == pid {
			if _, _, qerr := QueryStatus(ctx, dir); qerr == nil {
				return i, true, nil
			}
		}
	}
	return Info{}, false, fmt.Errorf("daemon (PID %d) chưa sẵn sàng sau 15 giây; xem %s%s", pid, logPath, logTail(logPath))
}

func logTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > 400 {
		data = data[len(data)-400:]
	}
	return ": " + strings.TrimSpace(string(data))
}

// Stop tắt daemon trong dir: gọi /admin/stop rồi chờ khoá được nhả; không gọi được mà khoá còn bị giữ thì kill PID trong
// gateway.json. stopped=false nếu không có daemon.
func Stop(ctx context.Context, dir string) (stopped bool, err error) {
	c, err := Connect(dir)
	if errors.Is(err, ErrNotRunning) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if serr := c.Stop(ctx); serr != nil {
		if !lockHeld(dir) { // daemon đã chết: gateway.json cũ
			_ = os.Remove(filepath.Join(dir, InfoFileName))
			return false, nil
		}
		if kerr := killPID(c.Info.PID); kerr != nil {
			return false, fmt.Errorf("daemon không trả lời /admin/stop (%v) và không kill được PID %d: %w", serr, c.Info.PID, kerr)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !lockHeld(dir) {
			// Daemon thoát êm đã xoá gateway.json; bị kill thì còn lại tệp cũ.
			if i, err := ReadInfo(dir); err == nil && i.PID == c.Info.PID {
				_ = os.Remove(filepath.Join(dir, InfoFileName))
			}
			return true, nil
		}
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return true, fmt.Errorf("daemon (PID %d) chưa thoát sau 10 giây", c.Info.PID)
}
