package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// pathCache nhớ kết quả exec.LookPath theo (tên, PATH, PATHEXT). Mỗi
// exec.Command("git") gọi exec.LookPath, trên Windows sinh (số thư mục PATH x số
// đuôi PATHEXT) lần stat; go test ghi từng lần vào testlog rồi băm lại ở lần
// chạy sau, nên dò một lần là đủ (xem docs/workflows.md).
var pathCache struct {
	mu      sync.Mutex
	entries map[string]pathEntry
	lookups int
}

type pathEntry struct {
	path, pathext string
	exe           string
	err           error
}

// LookPathCached như exec.LookPath nhưng nhớ kết quả theo (PATH, PATHEXT) và dò
// lại khi một trong hai đổi.
func LookPathCached(name string) (string, error) {
	p, pe := os.Getenv("PATH"), os.Getenv("PATHEXT")
	c := &pathCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[name]; ok && e.path == p && e.pathext == pe {
		return e.exe, e.err
	}
	exe, err := exec.LookPath(name)
	c.lookups++
	// Chỉ nhớ khi dò được và đường dẫn tuyệt đối (đường dẫn tương đối phụ thuộc cwd).
	if err == nil && filepath.IsAbs(exe) {
		if c.entries == nil {
			c.entries = map[string]pathEntry{}
		}
		c.entries[name] = pathEntry{path: p, pathext: pe, exe: exe}
	}
	return exe, err
}

// lookGit trả về đường dẫn git, dò lại khi PATH hoặc PATHEXT đổi.
func lookGit() (string, error) { return LookPathCached("git") }

// Command tương đương exec.Command("git", args...) nhưng dùng đường dẫn đã dò.
func Command(args ...string) *exec.Cmd {
	return CommandContext(context.Background(), args...)
}

// CommandContext tương đương exec.CommandContext(ctx, "git", args...).
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	exe, err := lookGit()
	if err != nil {
		// Để exec báo lỗi y như trước (Cmd.Err do LookPath đặt).
		return exec.CommandContext(ctx, "git", args...)
	}
	return exec.CommandContext(ctx, exe, args...)
}

// lookupCount trả số lần dò thật (dùng cho kiểm thử).
func lookupCount() int {
	pathCache.mu.Lock()
	defer pathCache.mu.Unlock()
	return pathCache.lookups
}
