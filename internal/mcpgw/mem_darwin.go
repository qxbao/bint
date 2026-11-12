//go:build darwin

package mcpgw

import (
	"os/exec"
	"strconv"
	"strings"
)

// procMemory trả RSS (KiB) của nhóm tiến trình pgid = pid bằng `ps`; macOS không cho PSS nên PSS = 0.
func procMemory(pid int) (rssKB, pssKB uint64) {
	out, err := exec.Command("ps", "-A", "-o", "pgid=,rss=").Output()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if g, err := strconv.Atoi(f[0]); err != nil || g != pid {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		rssKB += n
	}
	return rssKB, 0
}
