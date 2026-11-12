//go:build !windows

package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMetadataCancellationKillsDescendants(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	bin := filepath.Join(root, "codex")
	script := fmt.Sprintf("#!/bin/sh\nsleep 60 &\nprintf '%%s' \"$!\" > %q\nwhile IFS= read -r line; do\ncase \"$line\" in *'\"method\":\"initialize\"'*) printf '%%s\\n' '{\"id\":1,\"result\":{}}';; esac\ndone\n", pidFile)
	os.WriteFile(bin, []byte(script), 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := InspectHookTrust(ctx, bin, root, filepath.Join(root, "hooks.json")); done <- err }()
	// Chờ tiến trình con thực sự tồn tại trước khi huỷ; không dùng ngân sách
	// 400ms cho cả khởi động shell khi bộ test đang chạy đồng thời/race.
	var pid int
	for deadline := time.Now().Add(8 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if pid > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("metadata stub không khởi tạo được child")
		}
	}
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("metadata chưa trả mà không báo lỗi")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	// Kill(0) còn thấy zombie cho tới khi init reap; zombie không còn thực thi.
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		if b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); e == nil {
			if i := strings.LastIndex(string(b), ") "); i >= 0 && i+2 < len(b) && b[i+2] == 'Z' {
				return
			}
		}
	}
	t.Fatalf("tiến trình con %d còn chạy sau khi huỷ app-server", pid)
}
