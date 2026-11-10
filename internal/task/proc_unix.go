//go:build !windows

package task

import (
	"errors"
	"syscall"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ProcessAlive cho biết tiến trình pid còn chạy.
func ProcessAlive(pid int) bool { return processAlive(pid) }
