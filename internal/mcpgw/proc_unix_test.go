//go:build !windows

package mcpgw

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// processGone: tiến trình không còn (hoặc chỉ còn zombie chờ init thu dọn).
func processGone(pid int) bool {
	if !processAlive(pid) {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(data)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(f) > 0 && f[0] == "Z"
}

// Dừng upstream thì kill cả cây tiến trình (process group): tiến trình con cũng chết.
func TestStopKillsProcessTree(t *testing.T) {
	h := newHarness(t, nil)
	childFile := h.logDir + "/child.pid"
	h.register("codegraph", "child="+childFile)
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	_, out := c.call(1, "codegraph_explore", nil)
	var childPID int
	eventually(t, "upstream sinh tiến trình con", 5*time.Second, func() bool {
		b, err := os.ReadFile(childFile)
		if err != nil {
			return false
		}
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return childPID > 0
	})
	if !processAlive(childPID) || processGone(childPID) {
		t.Fatalf("tiến trình con %d phải đang chạy", childPID)
	}
	h.d.Restart("codegraph")
	eventually(t, "tiến trình upstream chết", 5*time.Second, func() bool { return processGone(out.PID) })
	eventually(t, "tiến trình con của upstream chết theo", 5*time.Second, func() bool { return processGone(childPID) })

	// Tắt daemon cũng giết cả cây.
	os.Remove(childFile)
	_, out = c.call(2, "codegraph_explore", nil)
	eventually(t, "tiến trình con thế hệ mới", 5*time.Second, func() bool {
		b, err := os.ReadFile(childFile)
		if err != nil {
			return false
		}
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return childPID > 0
	})
	h.d.Close()
	eventually(t, "cây tiến trình chết khi daemon tắt", 5*time.Second, func() bool { return processGone(out.PID) && processGone(childPID) })
}
