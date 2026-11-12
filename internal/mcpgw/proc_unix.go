//go:build !windows

package mcpgw

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// procGroup là nhóm tiến trình của một upstream: kill giết cả cây (kể cả tiến trình cháu), trừ tiến trình tự
// tách khỏi nhóm (daemon của codegraph, mục 6.5 của kế hoạch).
type procGroup struct{ pgid int }

// setupCmd đặt upstream vào nhóm tiến trình riêng (pgid = pid của nó); gọi trước cmd.Start.
func setupCmd(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// attachProc gắn cmd đã Start vào nhóm.
func attachProc(cmd *exec.Cmd) (*procGroup, error) { return &procGroup{pgid: cmd.Process.Pid}, nil }

// kill giết cả nhóm (SIGKILL); an toàn khi gọi nhiều lần.
func (g *procGroup) kill() {
	if g == nil || g.pgid <= 0 {
		return
	}
	_ = syscall.Kill(-g.pgid, syscall.SIGKILL)
}

// detachCmd đặt tiến trình vào phiên riêng để sống sau khi lệnh khởi động thoát.
func detachCmd(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// processAlive cho biết pid còn sống không (kể cả tiến trình không phải của mình).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// killPID kết thúc tiến trình pid (SIGKILL).
func killPID(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
