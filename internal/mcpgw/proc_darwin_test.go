//go:build darwin

package mcpgw

import "golang.org/x/sys/unix"

func processZombie(pid int) (bool, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false, err
	}
	const szomb = 5 // Darwin sys/proc.h: exited, waiting for parent to reap
	return info.Proc.P_stat == szomb, nil
}
