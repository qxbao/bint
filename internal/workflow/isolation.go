package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/knowledge"
	"bint/internal/state"
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
type WriteLock struct {
	path, content string
	f             *os.File // tệp đang giữ khoá hệ điều hành; nil nếu không giữ
}

// lockDir là thư mục chứa khoá ghi trong thư mục trạng thái.
func lockDir(tasksDir string) string { return filepath.Join(filepath.Dir(tasksDir), "locks") }

// AcquireWriteLock lấy khoá ghi cho worktree root, không chờ.
//
// Giao thức: tính độc quyền do khoá của hệ điều hành (flock trên Unix, LockFileEx trên
// Windows) trên tệp khoá bảo đảm; nội dung tệp (PID, chủ) chỉ để thông báo. Khoá của
// tiến trình đã chết tự được hệ điều hành nhả nên không còn bước "thu hồi dựa trên
// quan sát stale" — thứ từng có thể tạm gỡ khoá của chủ đang sống (R1). Vì Release xoá
// tệp, sau khi khoá được phải kiểm tra tệp vẫn là tệp ở đường dẫn, nếu không thì mở lại.
func AcquireWriteLock(tasksDir, root, owner string) (*WriteLock, error) {
	dir := lockDir(tasksDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(state.NormalizePath(root)))
	p := filepath.Join(dir, "write-"+hex.EncodeToString(h[:])[:16]+".lock")
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("%d\n%s\n%s\n%s\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), root, owner, hex.EncodeToString(nonce[:]))

	for attempt := 0; attempt < 8; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		busy, err := tryFlock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if busy {
			holder := describeHolder(p)
			f.Close()
			return nil, fmt.Errorf("%w%s; chờ bước đó xong hoặc dùng worktree khác", ErrWriteLocked, holder)
		}
		if !sameFile(f, p) {
			f.Close() // chủ trước vừa xoá tệp khi trả khoá; mở lại
			continue
		}
		if err := f.Truncate(0); err == nil {
			_, err = f.WriteAt([]byte(content), 0)
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		return &WriteLock{path: p, content: content, f: f}, nil
	}
	return nil, ErrWriteLocked
}

// describeHolder mô tả chủ đang giữ khoá từ nội dung tệp (chỉ để báo lỗi).
func describeHolder(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 5 {
		return ""
	}
	return fmt.Sprintf(" (tiến trình %s, %s)", strings.TrimSpace(lines[0]), strings.TrimSpace(lines[3]))
}

// Release trả khoá, chỉ khi chính chủ này đang giữ khoá hệ điều hành.
func (l *WriteLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	// Còn giữ khoá nên không ai khác sửa/xoá tệp: xoá rồi mới đóng (nhả khoá).
	removeLockFile(l.path)
	l.f.Close()
	l.f = nil
}

// WorktreeSnapshot trả về mã cây (git tree) đại diện cho toàn bộ cây làm việc: tệp
// đã theo dõi và tệp mới chưa bị .gitignore loại, kể cả .bint/knowledge/: việc tạo
// ứng viên tri thức mới được phân loại riêng bởi ReadonlyViolations, còn sửa/xoá mục
// đã có vẫn bị tính là vi phạm. Dùng chỉ mục tạm nên không đụng tới chỉ mục của người dùng.
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
	if _, err := run("add", "-A", "--", "."); err != nil {
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

// ReadonlyViolations trả về các đường dẫn đổi giữa hai ảnh chụp mà bước chỉ đọc
// không được phép đổi. Ngoại lệ duy nhất: tệp tri thức MỚI (chưa có ở ảnh trước) trong
// thư mục items, hợp lệ và ở trạng thái candidate. Sửa/xoá mục có sẵn, hoặc tệp mới tự
// gán status khác candidate đều là vi phạm.
func ReadonlyViolations(ctx context.Context, root, before, after string) []string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-status", "--no-renames", "-z", before, after)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ChangedPaths(ctx, root, before, after)
	}
	var bad []string
	f := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		st, p := f[i], f[i+1]
		if st == "A" && strings.HasPrefix(p, knowledge.ItemsRel+"/") && newCandidateFile(root, p) {
			continue
		}
		bad = append(bad, p)
	}
	return bad
}

func newCandidateFile(root, rel string) bool {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	it, err := knowledge.Parse(b)
	return err == nil && it.Status == knowledge.StatusCandidate
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
