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
	"strconv"
	"strings"
	"time"

	"bint/internal/knowledge"
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
type WriteLock struct{ path, content string }

// lockDir là thư mục chứa khoá ghi trong thư mục trạng thái.
func lockDir(tasksDir string) string { return filepath.Join(filepath.Dir(tasksDir), "locks") }

// AcquireWriteLock lấy khoá ghi cho worktree root. Khoá của tiến trình đã chết được
// coi là bỏ lại và bị thay.
//
// Giao thức: nội dung khoá được ghi đầy đủ vào tệp tạm rồi công bố nguyên tử bằng
// os.Link (thất bại nếu đã có khoá), nên không bao giờ có tệp khoá rỗng/dở dang. Việc
// thu hồi khoá bỏ lại dùng os.Rename sang tên duy nhất: chỉ một bên thắng; bên thắng
// kiểm tra lại nội dung đúng là khoá đã thấy là bỏ lại, nếu không thì trả lại.
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
	token := hex.EncodeToString(nonce[:])
	content := fmt.Sprintf("%d\n%s\n%s\n%s\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), root, owner, token)

	tmp := p + ".tmp-" + token
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)

	for attempt := 0; attempt < 5; attempt++ {
		err := os.Link(tmp, p)
		if err == nil {
			return &WriteLock{path: p, content: content}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				continue // vừa được trả, thử lại
			}
			return nil, rerr
		}
		lines := strings.Split(string(b), "\n")
		pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
		if pid <= 0 || len(lines) < 5 {
			// Không thể dở dang (công bố nguyên tử) nên tệp hỏng là bất thường: không tự thu hồi.
			return nil, fmt.Errorf("%w (tệp khoá %s không hợp lệ; xoá tay nếu chắc chắn không có bước nào đang chạy)", ErrWriteLocked, p)
		}
		if task.ProcessAlive(pid) {
			return nil, fmt.Errorf("%w (tiến trình %d, %s); chờ bước đó xong hoặc dùng worktree khác", ErrWriteLocked, pid, strings.TrimSpace(lines[3]))
		}
		// Khoá bỏ lại: chuyển sang tên duy nhất để chỉ một bên thu hồi được.
		stale := p + ".stale-" + token
		if err := os.Rename(p, stale); err != nil {
			continue // bên khác đã thu hồi/trả
		}
		sb, _ := os.ReadFile(stale)
		if string(sb) != string(b) {
			// Đã lỡ chuyển đi khoá của chủ khác: trả lại nếu chỗ còn trống.
			if lerr := os.Link(stale, p); lerr != nil {
				os.Remove(stale)
				return nil, ErrWriteLocked
			}
			os.Remove(stale)
			return nil, ErrWriteLocked
		}
		os.Remove(stale)
	}
	return nil, ErrWriteLocked
}

// Release trả khoá, chỉ khi tệp khoá vẫn là của chính chủ này.
func (l *WriteLock) Release() {
	if l == nil {
		return
	}
	if b, err := os.ReadFile(l.path); err == nil && string(b) == l.content {
		os.Remove(l.path)
	}
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
