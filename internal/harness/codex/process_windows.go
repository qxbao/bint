//go:build windows

package codex

import (
	"bint/internal/gitrepo"
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
	if err := exec.Command(taskkillPath(), "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// taskkillPath dò taskkill một lần cho mỗi (PATH, PATHEXT); lỗi dò thì giữ tên trần.
func taskkillPath() string {
	if p, err := gitrepo.LookPathCached("taskkill"); err == nil {
		return p
	}
	return "taskkill"
}
