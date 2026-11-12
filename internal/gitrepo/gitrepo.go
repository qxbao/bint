// Package gitrepo xác định thông tin repository Git cần cho Bint Core mà không
// gọi mô hình: thư mục gốc của worktree, git-common-dir và các lệnh Git cơ bản.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotRepo được trả về khi thư mục không nằm trong repository Git.
var ErrNotRepo = errors.New("không nằm trong repository Git")

// Repo mô tả một worktree của một repository.
type Repo struct {
	// Toplevel là thư mục gốc của worktree hiện tại (đường dẫn tuyệt đối).
	Toplevel string
	// CommonDir là git-common-dir tuyệt đối; giống nhau giữa mọi worktree.
	CommonDir string
	// GitDir là git-dir riêng của worktree hiện tại.
	GitDir string
}

// IsLinkedWorktree cho biết đây có phải worktree phụ (git worktree add).
func (r Repo) IsLinkedWorktree() bool {
	return Clean(r.GitDir) != Clean(r.CommonDir)
}

// MainWorktree trả về thư mục gốc của worktree chính, hoặc "" nếu không xác định
// được (ví dụ git-common-dir không phải "<gốc>/.git").
func (r Repo) MainWorktree() string {
	if filepath.Base(r.CommonDir) == ".git" {
		return filepath.Dir(r.CommonDir)
	}
	return ""
}

// ConfigRoots là các thư mục được dùng để tìm .bint/config.yml theo thứ tự ưu
// tiên: worktree hiện tại, rồi worktree chính (khi .bint chưa được commit nên
// chưa có trong worktree phụ).
func (r Repo) ConfigRoots() []string {
	roots := []string{r.Toplevel}
	if m := r.MainWorktree(); m != "" && Clean(m) != Clean(r.Toplevel) {
		roots = append(roots, m)
	}
	return roots
}

// Discover tìm repository chứa dir.
func Discover(ctx context.Context, dir string) (Repo, error) {
	out, err := Run(ctx, dir, "rev-parse", "--path-format=absolute",
		"--show-toplevel", "--git-common-dir", "--git-dir")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return Repo{}, ErrNotRepo
		}
		return Repo{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] == "" {
		// Thư mục bare repository không có toplevel.
		return Repo{}, fmt.Errorf("%w (repository bare hoặc đầu ra rev-parse không hợp lệ)", ErrNotRepo)
	}
	return Repo{
		Toplevel:  Clean(lines[0]),
		CommonDir: Clean(lines[1]),
		GitDir:    Clean(lines[2]),
	}, nil
}

// Clean chuẩn hoá đường dẫn do Git trả về (Git trên Windows dùng dấu '/').
func Clean(p string) string {
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(p)))
}

// Run chạy một lệnh git trong dir và trả về stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// HeadCommit trả về commit HEAD, hoặc "" nếu repository chưa có commit.
func HeadCommit(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Branch trả về tên nhánh hiện tại, hoặc "" nếu đang detached HEAD.
func Branch(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Version trả về phiên bản git, ví dụ "2.50.1".
func Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(out))
	if len(f) < 3 {
		return strings.TrimSpace(string(out)), nil
	}
	return f[2], nil
}

// Worktree là một mục của `git worktree list`.
type Worktree struct {
	Path   string
	Head   string
	Branch string // tên nhánh đầy đủ (refs/heads/...) hoặc ""
	// Prunable: thư mục đã mất, `git worktree prune` sẽ dọn mục này.
	Prunable bool
	Locked   bool
	Bare     bool
}

// WorktreeList liệt kê worktree của repository chứa dir (đầu ra porcelain).
func WorktreeList(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			list = append(list, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			flush()
			cur = &Worktree{Path: Clean(val)}
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = val
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	flush()
	return list, nil
}

// WorktreeAdd tạo worktree mới ở path trên nhánh mới branch, bắt đầu từ commit.
func WorktreeAdd(ctx context.Context, dir, path, branch, commit string) error {
	_, err := Run(ctx, dir, "worktree", "add", "-q", "-b", branch, path, commit)
	return err
}

// WorktreeRemove gỡ worktree path (kể cả khi còn thay đổi chưa commit), rồi dọn mục đã mất.
func WorktreeRemove(ctx context.Context, dir, path string) error {
	_, err := Run(ctx, dir, "worktree", "remove", "--force", path)
	if err != nil {
		// Thư mục đã mất khỏi đĩa: chỉ còn mục quản trị trong .git/worktrees.
		if _, perr := Run(ctx, dir, "worktree", "prune"); perr == nil {
			if list, lerr := WorktreeList(ctx, dir); lerr == nil {
				found := false
				for _, w := range list {
					found = found || Clean(w.Path) == Clean(path)
				}
				if !found {
					return nil
				}
			}
		}
		return err
	}
	return nil
}

// DeleteBranch xoá nhánh (kể cả chưa gộp). Nhánh không tồn tại không phải lỗi.
func DeleteBranch(ctx context.Context, dir, branch string) error {
	if _, err := Run(ctx, dir, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err != nil {
		return nil
	}
	_, err := Run(ctx, dir, "branch", "-D", "--", branch)
	return err
}
