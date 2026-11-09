//go:build !windows

package eval

import (
	"os/exec"
	"syscall"
)

// prepareProcess đặt tiến trình vào nhóm riêng để khi huỷ thì giết cả cây.
func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
