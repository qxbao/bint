package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Source là một trạng thái mã nguồn để kiểm tra bằng chứng: cây làm việc hiện tại
// hoặc một commit (dùng cho chống rò rỉ ở chế độ economy, mục 12.3).
type Source interface {
	// Read trả về nội dung tệp (đường dẫn tương đối, dấu '/').
	Read(rel string) ([]byte, bool)
	// Files trả về mọi tệp (đã sắp xếp).
	Files() []string
	// Name mô tả nguồn trong thông báo.
	Name() string
}

// WorktreeSource đọc cây làm việc; danh sách tệp gồm tệp được theo dõi và tệp
// mới chưa bị .gitignore loại (mục tri thức thường được viết trước khi commit).
type WorktreeSource struct {
	Root  string
	once  sync.Once
	files []string
	err   error
}

func (w *WorktreeSource) Name() string { return "cây làm việc" }

func (w *WorktreeSource) Read(rel string) ([]byte, bool) {
	if ValidRelPath(rel) != nil {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(w.Root, filepath.FromSlash(rel)))
	return b, err == nil
}

func (w *WorktreeSource) Files() []string {
	w.once.Do(func() {
		out, err := gitOut(context.Background(), w.Root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if err != nil {
			w.err = err
			return
		}
		seen := map[string]bool{}
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" && !seen[f] {
				if _, err := os.Lstat(filepath.Join(w.Root, filepath.FromSlash(f))); err == nil {
					seen[f] = true
					w.files = append(w.files, f)
				}
			}
		}
		sort.Strings(w.files)
	})
	return w.files
}

// CommitSource đọc trạng thái tại một commit.
type CommitSource struct {
	RepoDir string
	Commit  string
	once    sync.Once
	files   []string
	mu      sync.Mutex
	cache   map[string][]byte
}

func (c *CommitSource) Name() string { return "commit " + shortSHA(c.Commit) }

func (c *CommitSource) Files() []string {
	c.once.Do(func() {
		out, err := gitOut(context.Background(), c.RepoDir, "ls-tree", "-r", "-z", "--name-only", c.Commit)
		if err != nil {
			return
		}
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" {
				c.files = append(c.files, f)
			}
		}
		sort.Strings(c.files)
	})
	return c.files
}

func (c *CommitSource) Read(rel string) ([]byte, bool) {
	if ValidRelPath(rel) != nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if b, ok := c.cache[rel]; ok {
		return b, b != nil
	}
	if c.cache == nil {
		c.cache = map[string][]byte{}
	}
	b, err := gitOut(context.Background(), c.RepoDir, "show", c.Commit+":"+rel)
	if err != nil {
		c.cache[rel] = nil
		return nil, false
	}
	c.cache[rel] = b
	return b, true
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// HasFileOrDir cho biết rel là một tệp, hoặc tiền tố thư mục của một tệp, trong src.
func HasFileOrDir(src Source, rel string) bool {
	rel = strings.TrimSuffix(rel, "/")
	files := src.Files()
	i := sort.SearchStrings(files, rel)
	if i < len(files) && files[i] == rel {
		return true
	}
	prefix := rel + "/"
	j := sort.SearchStrings(files, prefix)
	return j < len(files) && strings.HasPrefix(files[j], prefix)
}

// normalizeLines chuẩn hoá xuống dòng và bỏ khoảng trắng cuối dòng, để fingerprint
// không đổi khi chỉ đổi CRLF/LF hay khoảng trắng thừa.
func normalizeLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return lines
}

// Fingerprint tính fingerprint của bằng chứng trên nội dung tệp: "lines:<băm>" cho
// vùng dòng, "file:<băm>" cho cả tệp. Fingerprint theo ký hiệu (tree-sitter) thuộc P3.
func Fingerprint(e Evidence, content []byte) (string, error) {
	lines := normalizeLines(content)
	from, to, ok, err := e.LineRange()
	if err != nil {
		return "", err
	}
	kind := "file"
	if ok {
		if to > len(lines) {
			return "", fmt.Errorf("%s có %d dòng, bằng chứng tham chiếu dòng %s", e.Path, len(lines), e.Lines)
		}
		lines = lines[from-1 : to]
		kind = "lines"
	}
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return kind + ":" + hex.EncodeToString(h[:])[:16], nil
}

// FillFingerprints tính fingerprint cho mọi bằng chứng của mục từ src.
func FillFingerprints(it *Item, src Source) error {
	for i := range it.Evidence {
		e := &it.Evidence[i]
		b, ok := src.Read(e.Path)
		if !ok {
			return fmt.Errorf("bằng chứng %s: không có tệp trong %s", e.Path, src.Name())
		}
		fp, err := Fingerprint(*e, b)
		if err != nil {
			return fmt.Errorf("bằng chứng %s: %w", e, err)
		}
		e.Fingerprint = fp
	}
	return nil
}
