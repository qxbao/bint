//go:build windows

package mcpgw

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	psapi                = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemory = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters là PROCESS_MEMORY_COUNTERS của psapi.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// procMemory trả working set (KiB) của tiến trình upstream (không cộng con cháu); Windows không có PSS nên PSS = 0.
func procMemory(pid int) (rssKB, pssKB uint64) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return 0, 0
	}
	defer windows.CloseHandle(h)
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	r, _, _ := procGetProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	if r == 0 {
		return 0, 0
	}
	return uint64(c.WorkingSetSize) / 1024, 0
}
