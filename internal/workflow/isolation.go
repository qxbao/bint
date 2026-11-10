package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bint/internal/state"
	"bint/internal/task"
)

// Cô lập các bước có quyền ghi (mục 15.2, P5):
//
//   - Khoá ghi theo worktree: tại một thời điểm chỉ một bước có quyền ghi (b-feat) — dù
//     chạy bằng harness nào — được làm việc trên một worktree. Bước chỉ đọc không cần khoá.
//     Muốn chạy song song thì dùng worktree khác (`git worktree add`), trạng thái Bint
//     dùng chung giữa các worktree.
//   - Bước chỉ đọc (b-req, b-review) được kiểm tra lại ở tầng chương trình: ảnh chụp cây
//     làm việc trước và sau bước phải trùng nhau, bất kể harness áp chỉ đọc mạnh tới đâu.

// ErrWriteLocked: worktree đang có bước ghi khác chạy.
var ErrWriteLocked = errors.New("worktree đang có một bước có quyền ghi khác chạy")

// WriteLock là khoá ghi của một worktree.
type WriteLock struct{ path string }

// lockDir là thư mục chứa khoá ghi trong thư mục trạng thái.
func lockDir(tasksDir string) string { return filepath.Join(filepath.Dir(tasksDir), "locks") }

// AcquireWriteLock lấy khoá ghi cho worktree root. Khoá của tiến trình đã chết được
// coi là bỏ lại và bị thay.
func AcquireWriteLock(tasksDir, root, owner string) (*WriteLock, error) {
	dir := lockDir(tasksDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(state.NormalizePath(root)))
	p := filepath.Join(dir, "write-"+hex.EncodeToString(h[:])[:16]+".lock")
	content := fmt.Sprintf("%d\n%s\n%s\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), root, owner)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.WriteString(content)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(p)
				return nil, errors.Join(werr, cerr)
			}
			return &WriteLock{path: p}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		b, _ := os.ReadFile(p)
		lines := strings.Split(string(b), "\n")
		pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
		if pid > 0 && task.ProcessAlive(pid) {
			who := ""
			if len(lines) > 3 {
				who = strings.TrimSpace(lines[3])
			}
			return nil, fmt.Errorf("%w (tiến trình %d, %s); chờ bước đó xong hoặc dùng worktree khác", ErrWriteLocked, pid, who)
		}
		os.Remove(p) // khoá bỏ lại của tiến trình đã chết
	}
	return nil, ErrWriteLocked
}

// Release trả khoá.
func (l *WriteLock) Release() {
	if l != nil {
		os.Remove(l.path)
	}
}

// WorktreeSnapshot trả về mã cây (git tree) đại diện cho toàn bộ cây làm việc: tệp
// đã theo dõi và tệp mới chưa bị .gitignore loại, trừ .bint/ (tri thức ứng viên do
// CLI bint ghi). Dùng chỉ mục tạm nên không đụng tới chỉ mục của người dùng.
func WorktreeSnapshot(ctx context.Context, root string) (string, error) {
	tmp, err := os.CreateTemp("", "bint-index-*")
	if err != nil {
		return "", err
	}
	idx := tmp.Name()
	tmp.Close()
	os.Remove(idx) // git tạo chỉ mục mới
	defer os.Remove(idx)
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+idx)
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := run("add", "-A", "--", ".", ":(exclude).bint"); err != nil {
		return "", err
	}
	return run("write-tree")
}

// ChangedPaths liệt kê tệp khác nhau giữa hai ảnh chụp.
func ChangedPaths(ctx context.Context, root, before, after string) []string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", before, after)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var ps []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			ps = append(ps, l)
		}
	}
	return ps
}

// stateDirs là thư mục trạng thái Bint mà agent cần ghi (lệnh bint task …, spool).
func (e *Env) stateDirs() []string {
	switch {
	case e.Spool != "":
		return []string{filepath.Dir(e.Spool)}
	case e.Tasks.Dir != "":
		return []string{filepath.Dir(e.Tasks.Dir)}
	}
	return nil
}
