package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Môi trường làm việc của một lượt đánh giá là một repository Git MỚI chỉ chứa
// lịch sử tới commit gốc. Không dùng `git worktree` của repository người dùng vì
// worktree dùng chung kho đối tượng: agent có thể `git log --all` hoặc
// `git show <commit lời giải>` và đọc được đáp án (mục 12.3).
//
// Mỗi commit gốc có một kho trần đệm (<eval-work>/bases/<sha>.git) được nạp bằng
// `git fetch <repo> <sha>`, nên chỉ chứa đối tượng tới được từ commit đó. Mỗi lượt
// chạy clone cục bộ từ kho đệm (hardlink, nhanh) rồi gỡ remote.
//
// Kho đệm chỉ là bộ nhớ tạm: được dọn khi mọi lượt dùng base đó đã xong (releaseBase),
// giới hạn theo LRU (eval.base_cache_max_mb) và xoá tay bằng `bint eval clean`.

var baseLocks sync.Map

// ensureBaseLocked tạo kho đệm cho commit base nếu chưa có. Bên gọi giữ khoá của kho
// (lockBase) trong suốt thời gian dùng kho, nên releaseBase/PruneBases không xoá giữa chừng.
func ensureBaseLocked(ctx context.Context, env Env, base string) (string, error) {
	dir := filepath.Join(basesDir(env.WorkRoot), base+".git")
	if out, err := gitString(ctx, dir, "rev-parse", "--verify", "-q", "refs/heads/eval-base"); err == nil && out == base {
		now := time.Now()
		os.Chtimes(dir, now, now) // đánh dấu vừa dùng cho LRU
		return dir, nil
	}
	os.RemoveAll(dir)
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", err
	}
	// Kiểm tra dung lượng trước khi nạp (tmp đã tồn tại nên đo được ổ đĩa).
	if err := checkFreeSpace(ctx, env, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	if _, err := git(ctx, tmp, "init", "-q", "--bare"); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	src, err := filepath.Abs(env.RepoDir)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	// Mặc định nạp đầy đủ lịch sử tới commit gốc (số liệu cũ dựa trên đó). eval.base_depth
	// > 0 nạp nông; git bỏ qua --depth với đường dẫn cục bộ nên dùng URL file://.
	args := []string{"fetch", "-q", "--no-tags", "--no-write-fetch-head"}
	if d := env.Cfg.Eval.BaseDepth; d > 0 {
		args = append(args, "--depth", strconv.Itoa(d))
		src = "file://" + filepath.ToSlash(src)
	}
	args = append(args, src, base+":refs/heads/eval-base")
	if _, err := git(ctx, tmp, args...); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("nạp commit gốc %s: %w", short(base), err)
	}
	if _, err := git(ctx, tmp, "symbolic-ref", "HEAD", "refs/heads/eval-base"); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return dir, nil
}

// checkBaseCommit kiểm tra nhanh commit gốc có trong repository người dùng, không tạo
// kho đệm (kho được tạo lười ở lượt đầu dùng base, để không giữ cùng lúc mọi kho).
func checkBaseCommit(ctx context.Context, repoDir, base string) error {
	if _, err := git(ctx, repoDir, "cat-file", "-e", base+"^{commit}"); err != nil {
		return fmt.Errorf("commit gốc %s không có trong repository: %w", short(base), err)
	}
	return nil
}

// prepareWorkdir tạo thư mục làm việc tại commit base trong dest. Giữ khoá của kho đệm
// suốt lúc nạp và clone nên dọn kho song song (releaseBase) không phá clone đang chạy.
func prepareWorkdir(ctx context.Context, env Env, base, dest string) error {
	unlock := lockBase(filepath.Join(basesDir(env.WorkRoot), base+".git"))
	defer unlock()
	cache, err := ensureBaseLocked(ctx, env, base)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := git(ctx, filepath.Dir(dest), "clone", "-q", "--no-tags", "-c", "core.autocrlf=false", "--branch", "eval-base", cache, dest); err != nil {
		return err
	}
	if _, err := git(ctx, dest, "remote", "remove", "origin"); err != nil {
		return err
	}
	// Tên nhánh trung tính; agent không thấy commit nào sau base.
	if _, err := git(ctx, dest, "branch", "-q", "-m", "eval-base", "main"); err != nil {
		return err
	}
	head, err := gitString(ctx, dest, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != base {
		return fmt.Errorf("thư mục làm việc ở %s, mong đợi %s", short(head), short(base))
	}
	return nil
}

// contextFiles là các tệp/thư mục ngữ cảnh dành cho agent bị gỡ ở mọi nhánh trước
// khi nhánh thêm ngữ cảnh của riêng nó (mục 12.4: nhánh "không ngữ cảnh" chỉ có harness).
var contextNames = map[string]bool{"AGENTS.md": true, "CLAUDE.md": true, "CLAUDE.local.md": true, "AGENT.md": true, "GEMINI.md": true}

var contextRootDirs = []string{".bint", ".claude", ".agents", ".codex", ".cursor", ".gemini", ".windsurf",
	".github/copilot-instructions.md", ".cursorrules", ".windsurfrules"}

// stripContext xoá tệp ngữ cảnh agent khỏi thư mục làm việc. Trả về danh sách đã xoá.
func stripContext(dir string) ([]string, error) {
	var removed []string
	for _, p := range contextRootDirs {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if _, err := os.Lstat(full); err == nil {
			if err := os.RemoveAll(full); err != nil {
				return removed, err
			}
			removed = append(removed, p)
		}
	}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if contextNames[d.Name()] {
			if err := os.Remove(p); err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			removed = append(removed, filepath.ToSlash(rel))
		}
		return nil
	})
	return removed, err
}

// changeStats đo thay đổi của agent so với commit chuẩn bị prep: số tệp, số dòng,
// và có chạm vào tệp kiểm thử hay không.
func changeStats(ctx context.Context, dir, prep string, cls Classifier) (files, lines int, touchedTests bool, err error) {
	if _, err = git(ctx, dir, "add", "-A"); err != nil {
		return
	}
	var out []byte
	out, err = git(ctx, dir, "diff", "--cached", "--no-renames", "--numstat", "-z", prep)
	if err != nil {
		return
	}
	for _, rec := range strings.Split(string(out), "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 {
			continue
		}
		files++
		a, _ := atoiSafe(f[0])
		d, _ := atoiSafe(f[1])
		lines += a + d
		if cls.Kind(f[2]) == "test" {
			touchedTests = true
		}
	}
	return
}

func atoiSafe(s string) (int, bool) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// ExpandTestCommand thay {test_files} và {test_dirs} trong lệnh kiểm thử bằng
// danh sách tệp kiểm thử của tác vụ (thư mục ở dạng ./dir, gốc là ".").
func ExpandTestCommand(cmd string, testFiles []string) string {
	if !strings.Contains(cmd, "{test_files}") && !strings.Contains(cmd, "{test_dirs}") {
		return cmd
	}
	var filesQ, dirsQ []string
	seen := map[string]bool{}
	for _, f := range testFiles {
		filesQ = append(filesQ, shellQuote(f))
		d := filepath.ToSlash(filepath.Dir(filepath.FromSlash(f)))
		if d == "." {
			d = "."
		} else {
			d = "./" + d
		}
		if !seen[d] {
			seen[d] = true
			dirsQ = append(dirsQ, shellQuote(d))
		}
	}
	cmd = strings.ReplaceAll(cmd, "{test_files}", strings.Join(filesQ, " "))
	return strings.ReplaceAll(cmd, "{test_dirs}", strings.Join(dirsQ, " "))
}

func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t'\"$`\\&|;<>()*?[]{}") {
		return `"` + strings.NewReplacer(`"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s) + `"`
	}
	return s
}
