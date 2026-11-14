package eval

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// git chạy lệnh git trong dir, trả về stdout dạng byte (giữ nguyên nội dung nhị phân).
func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}
	full := append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = cleanEnv("GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func gitString(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := git(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

// identity là danh tính dùng cho các commit Bint tạo trong thư mục làm việc tạm.
var identity = []string{"-c", "user.name=bint-eval", "-c", "user.email=bint-eval@localhost", "-c", "commit.gpgsign=false"}

func gitCommitAll(ctx context.Context, dir, msg string) (string, error) {
	if _, err := git(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	args := append(append([]string{}, identity...), "commit", "-q", "--allow-empty", "--no-verify", "-m", msg)
	if _, err := git(ctx, dir, args...); err != nil {
		return "", err
	}
	return gitString(ctx, dir, "rev-parse", "HEAD")
}

// FileChange là một tệp thay đổi giữa hai commit.
type FileChange struct {
	Path    string
	Status  byte // A, M, D (không theo dõi đổi tên: đổi tên = D + A)
	Added   int
	Deleted int
	Binary  bool
}

// copyFilesFromCommit ghi phiên bản của các tệp tại commit (đọc từ repository
// repoDir) vào thư mục dest. Tệp không tồn tại tại commit thì bị xoá khỏi dest.
func copyFilesFromCommit(ctx context.Context, repoDir, commit string, files []string, dest string) error {
	for _, f := range files {
		if err := checkRelPath(f); err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(f))
		// ls-tree in rỗng khi tệp không có ở commit; lỗi git khác (khoá, hết giờ) phải dừng, không được coi là "không có
		// tệp" rồi chấm điểm thiếu test ẩn.
		entry, err := gitString(ctx, repoDir, "ls-tree", "--end-of-options", commit, "--", f)
		if err != nil {
			return fmt.Errorf("đọc %s tại %s: %w", f, commit, err)
		}
		typ := ""
		if fields := strings.Fields(entry); len(fields) >= 2 {
			typ = fields[1]
		}
		if typ != "blob" {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		data, err := git(ctx, repoDir, "cat-file", "blob", commit+":"+f)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if m, err := gitString(ctx, repoDir, "ls-tree", commit, "--", f); err == nil && strings.HasPrefix(m, "100755 ") {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
	}
	return nil
}

// checkRelPath chặn đường dẫn thoát khỏi thư mục làm việc (tệp tác vụ có thể bị sửa tay).
func checkRelPath(p string) error {
	c := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if p == "" || filepath.IsAbs(p) || filepath.VolumeName(p) != "" || c == ".." || strings.HasPrefix(c, "../") || strings.HasPrefix(p, "/") {
		return fmt.Errorf("đường dẫn không hợp lệ trong tác vụ: %q", p)
	}
	return nil
}

// showFile đọc nội dung tệp tại commit; ok=false nếu không tồn tại.
func showFile(ctx context.Context, repoDir, commit, path string) ([]byte, bool) {
	data, err := git(ctx, repoDir, "cat-file", "blob", commit+":"+path)
	if err != nil {
		return nil, false
	}
	return data, true
}
