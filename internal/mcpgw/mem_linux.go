//go:build linux

package mcpgw

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// procMemory trả RSS và PSS (KiB) của nhóm tiến trình có pgid = pid (tiến trình upstream và con cháu cùng nhóm), đọc
// từ /proc. PSS lấy từ smaps_rollup; thiếu thì PSS = 0 và RSS lấy từ status.
func procMemory(pid int) (rssKB, pssKB uint64) {
	for _, p := range groupPIDs(pid) {
		r, ps := oneProcMemory(p)
		rssKB += r
		pssKB += ps
	}
	return rssKB, pssKB
}

// groupPIDs liệt kê pid thuộc nhóm tiến trình pgid (luôn gồm pgid nếu còn sống).
func groupPIDs(pgid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pid == pgid || statPgrp(pid) == pgid {
			out = append(out, pid)
		}
	}
	return out
}

// statPgrp đọc trường pgrp (thứ 5) của /proc/<pid>/stat; -1 nếu lỗi. Tên tiến trình trong ngoặc có thể chứa
// khoảng trắng và ')' nên cắt sau dấu ')' cuối.
func statPgrp(pid int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return -1
	}
	f := strings.Fields(s[i+1:])
	// f[0]=state f[1]=ppid f[2]=pgrp
	if len(f) < 3 {
		return -1
	}
	n, err := strconv.Atoi(f[2])
	if err != nil {
		return -1
	}
	return n
}

func oneProcMemory(pid int) (rssKB, pssKB uint64) {
	base := "/proc/" + strconv.Itoa(pid)
	if f, err := os.Open(base + "/smaps_rollup"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "Rss:"):
				rssKB = kbField(line)
			case strings.HasPrefix(line, "Pss:"):
				pssKB = kbField(line)
			}
		}
		if rssKB > 0 {
			return rssKB, pssKB
		}
	}
	if f, err := os.Open(base + "/status"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "VmRSS:") {
				return kbField(line), 0
			}
		}
	}
	return 0, 0
}

func kbField(line string) uint64 {
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.ParseUint(f[1], 10, 64)
	return n
}
