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

	"bint/internal/symbols"
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

// Mức fingerprint (mục 8.4), theo thứ tự ưu tiên.
const (
	FPSymbol = "symbol" // vùng của ký hiệu (hàm, kiểu, khối cấu hình)
	FPLines  = "lines"  // vùng dòng
	FPFile   = "file"   // cả tệp
)

func hashLines(lines []string) string {
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:])[:16]
}

// Fingerprint tính fingerprint của bằng chứng với mức ưu tiên mặc định (ký hiệu).
func Fingerprint(e Evidence, content []byte) (string, error) {
	return FingerprintWith(e, content, FPSymbol)
}

// FingerprintWith tính fingerprint theo mức ưu tiên pref (context.freshness.fingerprint:
// symbol | hunk | file):
//   - symbol: "symbol:<băm>" của vùng ký hiệu nếu bằng chứng có symbol và định vị
//     được; nếu không thì lùi về vùng dòng, rồi cả tệp;
//   - hunk: "lines:<băm>" nếu có vùng dòng, nếu không thì cả tệp;
//   - file: "file:<băm>".
func FingerprintWith(e Evidence, content []byte, pref string) (string, error) {
	lines := normalizeLines(content)
	if pref == FPFile {
		return FPFile + ":" + hashLines(lines), nil
	}
	if pref == FPSymbol && e.Symbol != "" {
		if r, ok := symbols.Locate(e.Path, content, e.Symbol); ok && r.To <= len(lines) && !containerWithLines(e, r) {
			return FPSymbol + ":" + hashLines(lines[r.From-1:r.To]), nil
		}
	}
	from, to, ok, err := e.LineRange()
	if err != nil {
		return "", err
	}
	if !ok {
		return FPFile + ":" + hashLines(lines), nil
	}
	if to > len(lines) {
		return "", fmt.Errorf("%s có %d dòng, bằng chứng tham chiếu dòng %s", e.Path, len(lines), e.Lines)
	}
	return FPLines + ":" + hashLines(lines[from-1:to]), nil
}

// containerWithLines: ký hiệu là vật chứa (lớp, kiểu) và bằng chứng có vùng dòng nằm
// trong đó. Băm cả lớp sẽ mất hiệu lực mỗi khi lớp có thêm thành viên, nên dùng phần
// thân liên quan (vùng dòng) thay vì toàn bộ lớp.
func containerWithLines(e Evidence, r symbols.Region) bool {
	from, to, ok, err := e.LineRange()
	return r.Container && err == nil && ok && from >= r.From && to <= r.To
}

// NarrowEvidence thu hẹp ký hiệu của bằng chứng khi nó chỉ là vật chứa (lớp): nếu vùng
// dòng nằm trọn trong một hàm/phương thức thì dùng ký hiệu của hàm đó ("Lớp.phương_thức").
// Không đổi gì nếu không chắc chắn; chỉ làm ký hiệu hẹp hơn, không bao giờ rộng hơn.
func NarrowEvidence(e Evidence, content []byte) Evidence {
	if e.Symbol == "" {
		return e
	}
	from, to, ok, err := e.LineRange()
	if err != nil || !ok {
		return e
	}
	r, found := symbols.Locate(e.Path, content, e.Symbol)
	if !found || !containerWithLines(e, r) {
		return e
	}
	name, er, found := symbols.Enclosing(e.Path, content, from, to)
	if !found || er.From < r.From || er.To > r.To {
		return e
	}
	if lr, ok := symbols.Locate(e.Path, content, name); !ok || lr.From != er.From || lr.To != er.To {
		return e
	}
	e.Symbol = name
	return e
}

// Kết quả đối chiếu bằng chứng.
const (
	EvMatch     = "match"     // fingerprint khớp
	EvRelocated = "relocated" // vùng dòng chỉ bị dịch chuyển, nội dung không đổi
	EvChanged   = "changed"   // nội dung thay đổi (hoặc ký hiệu không còn)
	EvNone      = "none"      // bằng chứng chưa có fingerprint
)

// EvidenceCheck là kết quả đối chiếu một bằng chứng với nội dung hiện tại.
type EvidenceCheck struct {
	Status string
	// Current là fingerprint hiện tại cùng mức với fingerprint đã lưu.
	Current string
	// Lines là vùng dòng mới khi Status = relocated.
	Lines  string
	Detail string
}

// maxRelocateWork giới hạn chi phí tìm lại vùng dòng (số dòng × độ dài vùng).
const maxRelocateWork = 4_000_000

// CheckEvidence đối chiếu bằng chứng với nội dung tệp theo đúng mức fingerprint
// đã lưu, để mục được ghi trước khi có mức ký hiệu vẫn được kiểm tra đúng.
//
// Với mức vùng dòng: nếu nội dung tại vùng cũ khác, tìm lại khối dòng có cùng băm
// (gần vùng cũ nhất). Tìm thấy nghĩa là các thay đổi trong tệp không chồng lên vùng
// được tham chiếu, chỉ đẩy nó lên/xuống — tương đương quy tắc "chỉ lỗi thời khi
// khối thay đổi của git diff chồng lên vùng dòng" (mục 8.4) mà không cần commit gốc.
func CheckEvidence(e Evidence, content []byte) EvidenceCheck {
	kind, want, _ := strings.Cut(e.Fingerprint, ":")
	if e.Fingerprint == "" {
		return EvidenceCheck{Status: EvNone}
	}
	lines := normalizeLines(content)
	switch kind {
	case FPFile:
		cur := FPFile + ":" + hashLines(lines)
		return verdict(cur, e.Fingerprint, "")
	case FPSymbol:
		r, ok := symbols.Locate(e.Path, content, e.Symbol)
		if !ok || r.To > len(lines) {
			return EvidenceCheck{Status: EvChanged, Detail: fmt.Sprintf("không còn tìm thấy ký hiệu %q", e.Symbol)}
		}
		return verdict(FPSymbol+":"+hashLines(lines[r.From-1:r.To]), e.Fingerprint, "")
	case FPLines:
		from, to, ok, err := e.LineRange()
		if err != nil || !ok {
			return EvidenceCheck{Status: EvChanged, Detail: "fingerprint vùng dòng nhưng bằng chứng không có vùng dòng hợp lệ"}
		}
		if to <= len(lines) {
			if c := verdict(FPLines+":"+hashLines(lines[from-1:to]), e.Fingerprint, ""); c.Status == EvMatch {
				return c
			}
		}
		n := to - from + 1
		if n <= len(lines) && len(lines)*n <= maxRelocateWork {
			best := -1
			for s := 0; s+n <= len(lines); s++ {
				if hashLines(lines[s:s+n]) == want && (best < 0 || absInt(s+1-from) < absInt(best+1-from)) {
					best = s
				}
			}
			if best >= 0 {
				nl := fmt.Sprintf("%d-%d", best+1, best+n)
				if n == 1 {
					nl = fmt.Sprint(best + 1)
				}
				return EvidenceCheck{Status: EvRelocated, Current: e.Fingerprint, Lines: nl,
					Detail: fmt.Sprintf("nội dung không đổi, dịch từ dòng %s sang %s", e.Lines, nl)}
			}
		}
		cur := ""
		if to <= len(lines) {
			cur = FPLines + ":" + hashLines(lines[from-1:to])
		}
		d := "nội dung vùng dòng đã thay đổi"
		if to > len(lines) {
			d = fmt.Sprintf("tệp còn %d dòng, không tìm lại được vùng %s", len(lines), e.Lines)
		}
		return EvidenceCheck{Status: EvChanged, Current: cur, Detail: d}
	}
	return EvidenceCheck{Status: EvChanged, Detail: fmt.Sprintf("mức fingerprint lạ %q", kind)}
}

func verdict(cur, stored, detail string) EvidenceCheck {
	if cur == stored {
		return EvidenceCheck{Status: EvMatch, Current: cur}
	}
	if detail == "" {
		detail = "nội dung đã thay đổi"
	}
	return EvidenceCheck{Status: EvChanged, Current: cur, Detail: detail}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// FillFingerprints tính fingerprint cho mọi bằng chứng của mục từ src, theo mức
// ưu tiên pref (rỗng = symbol).
func FillFingerprints(it *Item, src Source, pref ...string) error {
	p := FPSymbol
	if len(pref) > 0 && pref[0] != "" {
		p = pref[0]
	}
	for i := range it.Evidence {
		e := &it.Evidence[i]
		b, ok := src.Read(e.Path)
		if !ok {
			return fmt.Errorf("bằng chứng %s: không có tệp trong %s", e.Path, src.Name())
		}
		fp, err := FingerprintWith(*e, b, p)
		if err != nil {
			return fmt.Errorf("bằng chứng %s: %w", e, err)
		}
		e.Fingerprint = fp
	}
	return nil
}
