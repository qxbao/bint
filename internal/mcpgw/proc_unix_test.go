//go:build !windows

package mcpgw

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A live process must never satisfy the shutdown assertions below.
func TestProcessGone(t *testing.T) {
	if processGone(t, os.Getpid()) {
		t.Fatal("tiến trình kiểm thử đang sống bị coi là đã dừng")
	}
	cmd := exec.Command("sh", "-c", "read line")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if processGone(t, cmd.Process.Pid) {
		t.Fatal("child đang sống bị coi là đã dừng")
	}
	_ = stdin.Close()
	// Do not reap yet: a zombie must count as stopped even while Kill(pid, 0)
	// can still see it. This also checks the platform-specific state query.
	eventually(t, "child đã ngừng thực thi trước khi reap", time.Second, func() bool {
		return processGone(t, cmd.Process.Pid)
	})
	if err := cmd.Wait(); err == nil {
		t.Fatal("read không có đầu vào phải thoát khác 0")
	}
	if !processGone(t, cmd.Process.Pid) {
		t.Fatal("child đã reap vẫn bị coi là đang chạy")
	}
}

// processGone: tiến trình không còn (hoặc chỉ còn zombie chờ init thu dọn).
func processGone(t *testing.T, pid int) bool {
	t.Helper()
	gone, err := probeProcessGone(pid, processZombie)
	if err != nil {
		t.Fatal(err)
	}
	return gone
}

func probeProcessGone(pid int, zombie func(int) (bool, error)) (bool, error) {
	if !processAlive(pid) {
		return true, nil
	}
	z, err := zombie(pid)
	if err != nil && !processAlive(pid) {
		return true, nil // exited between Kill(0) and the state query
	}
	return z && err == nil, err
}

func TestProcessGoneProbeError(t *testing.T) {
	want := fmt.Errorf("state query denied")
	gone, err := probeProcessGone(os.Getpid(), func(int) (bool, error) { return false, want })
	if gone || !errors.Is(err, want) {
		t.Fatalf("probe thất bại không được xác nhận tiến trình chết: gone=%v err=%v", gone, err)
	}
}

// Exercise the real process-group kill without requiring an HTTP listener.
func TestProcGroupKillsDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command(os.Args[0], "__fake_upstream", "child="+pidFile)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	setupCmd(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	g, err := attachProc(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.kill)
	var childPID int
	eventually(t, "child PID", 5*time.Second, func() bool {
		b, _ := os.ReadFile(pidFile)
		childPID, _ = strconv.Atoi(string(b))
		return childPID > 0
	})
	if processGone(t, cmd.Process.Pid) || processGone(t, childPID) {
		t.Fatal("cây tiến trình phải sống trước kill")
	}
	g.kill()
	eventually(t, "kill nhóm dừng cả parent và child", 5*time.Second, func() bool {
		return processGone(t, cmd.Process.Pid) && processGone(t, childPID)
	})
}

// Dừng upstream thì kill cả cây tiến trình (process group): tiến trình con cũng chết.
func TestStopKillsProcessTree(t *testing.T) {
	h := newHarness(t, nil)
	childFile := h.logDir + "/child.pid"
	h.register("codegraph", "child="+childFile)
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	_, out := c.call(1, "codegraph_explore", nil)
	var childPID int
	eventually(t, "upstream sinh tiến trình con", 5*time.Second, func() bool {
		b, err := os.ReadFile(childFile)
		if err != nil {
			return false
		}
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return childPID > 0
	})
	if !processAlive(childPID) || processGone(t, childPID) {
		t.Fatalf("tiến trình con %d phải đang chạy", childPID)
	}
	h.d.Restart("codegraph")
	eventually(t, "tiến trình upstream chết", 5*time.Second, func() bool { return processGone(t, out.PID) })
	eventually(t, "tiến trình con của upstream chết theo", 5*time.Second, func() bool { return processGone(t, childPID) })

	// Tắt daemon cũng giết cả cây.
	os.Remove(childFile)
	_, out = c.call(2, "codegraph_explore", nil)
	eventually(t, "tiến trình con thế hệ mới", 5*time.Second, func() bool {
		b, err := os.ReadFile(childFile)
		if err != nil {
			return false
		}
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return childPID > 0
	})
	if processGone(t, childPID) {
		t.Fatalf("tiến trình con thế hệ mới %d phải đang chạy", childPID)
	}
	h.d.Close()
	eventually(t, "cây tiến trình chết khi daemon tắt", 5*time.Second, func() bool { return processGone(t, out.PID) && processGone(t, childPID) })
}
