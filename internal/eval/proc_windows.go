//go:build windows

package eval

import (
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
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
