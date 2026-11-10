//go:build !windows

package codex

import (
	"os/exec"
	"syscall"
)

func prepareMetadataProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killMetadataProcess(cmd) }
}

func killMetadataProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
