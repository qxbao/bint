//go:build windows

package eval

import (
	"bint/internal/gitrepo"
	"os/exec"
	"strconv"
)

// prepareProcess: khi huỷ, dùng taskkill /T để giết cả cây tiến trình con
// (ví dụ go test sinh ra các tệp thực thi kiểm thử).
func prepareProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := exec.Command(taskkillPath(), "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

// taskkillPath dò taskkill một lần cho mỗi (PATH, PATHEXT); lỗi dò thì giữ tên trần.
func taskkillPath() string {
	if p, err := gitrepo.LookPathCached("taskkill"); err == nil {
		return p
	}
	return "taskkill"
}
