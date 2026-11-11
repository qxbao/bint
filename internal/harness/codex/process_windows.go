//go:build windows

package codex

import (
	"os/exec"
	"strconv"
)

func prepareMetadataProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return killMetadataProcess(cmd) }
}

func killMetadataProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
