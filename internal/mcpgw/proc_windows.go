//go:build windows

package mcpgw

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procGroup là Job Object của một upstream (KILL_ON_JOB_CLOSE): kill giết cả cây tiến trình. Tiến trình cháu sinh ra
// trước khi cmd được gán vào job (khoảng giữa Start và attachProc) có thể lọt ra ngoài; chỉ kiểm được trên Windows thật.
type procGroup struct {
	job  windows.Handle // bất biến sau attachProc
	once killOnce
}

// setupCmd: nhóm tiến trình riêng để Ctrl+Break của daemon không lan sang upstream.
func setupCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// attachProc tạo Job Object và gán cmd đã Start vào đó.
func attachProc(cmd *exec.Cmd) (*procGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return &procGroup{job: job}, nil
}

// kill giết cả cây bằng cách kết thúc job; an toàn khi gọi nhiều lần và đồng thời (handle chỉ đóng một lần).
func (g *procGroup) kill() {
	if g == nil || g.job == 0 {
		return
	}
	g.once.do(func() {
		_ = windows.TerminateJobObject(g.job, 1)
		_ = windows.CloseHandle(g.job)
	})
}

// detachCmd khởi động tiến trình tách rời, không gắn với console của lệnh gọi.
func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

// processAlive cho biết pid còn chạy không.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// killPID kết thúc tiến trình pid.
func killPID(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
