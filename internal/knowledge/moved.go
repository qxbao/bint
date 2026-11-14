package knowledge

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"bint/internal/symbols"
)

// Giới hạn chi phí của tìm chuyển tệp (cho một bằng chứng). Vượt giới hạn thì dừng và
// đánh dấu Truncated: "không thấy" khi đó chưa chắc là không có.
const (
	maxMoveFiles  = 400      // số tệp ứng viên được đọc
	maxMoveBytes  = 32 << 20 // tổng số byte đọc
	maxMoveFile   = 1 << 20  // bỏ tệp lớn hơn
	maxCandidates = 5        // số ứng viên liệt kê khi khớp nhiều nơi
	minMoveChars  = 20       // khối ngắn hơn (ký tự không trắng) không đủ đặc trưng để kết luận "đã chuyển"
	maxMoveWork   = maxRelocateWork
)

// ChangeSet là tập tệp đã đổi kể từ một commit.
type ChangeSet struct {
	// Renames ánh xạ đường dẫn cũ sang mới (git diff -M).
	Renames map[string]string
	// Files là tệp được thêm/sửa/đổi tên đến, cùng tệp chưa theo dõi (nếu nguồn là cây làm việc).
	Files []string
}

// ChangeTracker là giao diện tuỳ chọn của Source: cho biết tệp nào đã đổi kể từ commit.
// Nguồn không cài thì tìm chuyển tệp lùi về mọi tệp cùng đuôi.
type ChangeTracker interface {
	ChangedSince(commit string) (ChangeSet, bool)
}

// parseNameStatus đọc đầu ra `git diff --name-status -z`.
func parseNameStatus(out []byte) ChangeSet {
	cs := ChangeSet{Renames: map[string]string{}}
	f := strings.Split(string(out), "\x00")
	for i := 0; i < len(f); {
		st := f[i]
		i++
		if st == "" {
			continue
		}
		switch st[0] {
		case 'R', 'C':
			if i+1 >= len(f) {
				return cs
			}
			from, to := f[i], f[i+1]
			i += 2
			if st[0] == 'R' {
				cs.Renames[from] = to
			}
			cs.Files = append(cs.Files, to)
		case 'D':
			i++
		default:
			if i >= len(f) {
				return cs
			}
			cs.Files = append(cs.Files, f[i])
			i++
		}
	}
	return cs
}

func (w *WorktreeSource) ChangedSince(commit string) (ChangeSet, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.changes[commit]; ok {
		return c.cs, c.ok
	}
	if w.changes == nil {
		w.changes = map[string]changeEntry{}
	}
	cs, ok := ChangeSet{}, false
	if !ValidRev(commit) {
		w.changes[commit] = changeEntry{cs, ok}
		return cs, ok
	}
	if out, err := gitOut(context.Background(), w.Root, "diff", "-M", "--name-status", "-z", "--end-of-options", commit, "--"); err == nil {
		cs, ok = parseNameStatus(out), true
		if un, err := gitOut(context.Background(), w.Root, "ls-files", "-z", "--others", "--exclude-standard"); err == nil {
			for _, f := range strings.Split(string(un), "\x00") {
				if f != "" {
					cs.Files = append(cs.Files, f)
				}
			}
		}
	}
	w.changes[commit] = changeEntry{cs, ok}
	return cs, ok
}

func (c *CommitSource) ChangedSince(commit string) (ChangeSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.changes[commit]; ok {
		return e.cs, e.ok
	}
	if c.changes == nil {
		c.changes = map[string]changeEntry{}
	}
	cs, ok := ChangeSet{}, false
	if !ValidRev(commit) || !ValidRev(c.Commit) {
		c.changes[commit] = changeEntry{cs, ok}
		return cs, ok
	}
	if out, err := gitOut(context.Background(), c.RepoDir, "diff", "-M", "--name-status", "-z", "--end-of-options", commit, c.Commit, "--"); err == nil {
		cs, ok = parseNameStatus(out), true
	}
	c.changes[commit] = changeEntry{cs, ok}
	return cs, ok
}

type changeEntry struct {
	cs ChangeSet
	ok bool
}

// moveResult là kết quả tìm chuyển tệp cho một bằng chứng.
type moveResult struct {
	Kind       string // IssueMoved, IssueAmbiguous hoặc "" (không thấy)
	NewPath    string
	NewLines   string
	Candidates []string
	Truncated  bool
	Content    []byte // nội dung tệp mới (khi moved)
}

// findMoved tìm khối có cùng fingerprint ở tệp khác. Chỉ gọi khi bằng chứng có fingerprint
// và khối không còn ở tệp cũ (tệp mất, vùng dòng vượt quá tệp, ký hiệu mất); nội dung đổi tại
// chỗ không phải ca chuyển tệp. Tìm bị cắt theo giới hạn thì một nơi khớp duy nhất chưa đủ để
// kết luận "đã chuyển" (có thể còn bản khác ở phần chưa quét): trả ambiguous kèm ứng viên đó. commits: các commit để thu hẹp ứng viên (evidence.commit,
// origin.commit).
func findMoved(src Source, e Evidence, commits ...string) moveResult {
	kind, want, ok := strings.Cut(e.Fingerprint, ":")
	if !ok || want == "" {
		return moveResult{}
	}
	n := 0
	switch kind {
	case FPLines:
		from, to, ok, err := e.LineRange()
		if err != nil || !ok {
			return moveResult{}
		}
		n = to - from + 1
	case FPSymbol:
		if e.Symbol == "" {
			return moveResult{}
		}
	case FPFile:
	default:
		return moveResult{}
	}

	cands, truncated := moveCandidates(src, e, commits)
	var res moveResult
	res.Truncated = truncated
	var bytesRead, work int
	type match struct {
		path, lines string
		content     []byte
		weak        bool
	}
	var matches []match
scan:
	for _, p := range cands {
		b, ok := src.Read(p)
		if !ok || len(b) > maxMoveFile {
			continue
		}
		if bytesRead += len(b); bytesRead > maxMoveBytes {
			res.Truncated = true
			break
		}
		lines := normalizeLines(b)
		add := func(lr string, block []string) {
			matches = append(matches, match{p, lr, b, weakBlock(block)})
		}
		switch kind {
		case FPLines:
			if work += len(lines) * n; work > maxMoveWork {
				res.Truncated = true
				break scan
			}
			for s := 0; s+n <= len(lines); s++ {
				if hashLines(lines[s:s+n]) == want {
					lr := fmt.Sprintf("%d-%d", s+1, s+n)
					if n == 1 {
						lr = fmt.Sprint(s + 1)
					}
					add(lr, lines[s:s+n])
				}
			}
		case FPSymbol:
			if work += len(lines); work > maxMoveWork {
				res.Truncated = true
				break scan
			}
			if !containsSymbol(b, e.Symbol) {
				continue
			}
			r, ok := symbols.Locate(p, b, e.Symbol)
			if !ok || r.To > len(lines) || hashLines(lines[r.From-1:r.To]) != want {
				continue
			}
			lr := ""
			if e.Lines != "" {
				lr = fmt.Sprintf("%d-%d", r.From, r.To)
			}
			add(lr, lines[r.From-1:r.To])
		case FPFile:
			if work += len(lines); work > maxMoveWork {
				res.Truncated = true
				break scan
			}
			if hashLines(lines) == want {
				add("", lines)
			}
		}
	}
	switch {
	case len(matches) == 0:
	case len(matches) == 1 && !matches[0].weak && !res.Truncated:
		m := matches[0]
		res.Kind, res.NewPath, res.NewLines, res.Content = IssueMoved, m.path, m.lines, m.content
	default:
		res.Kind = IssueAmbiguous
		for _, m := range matches {
			if len(res.Candidates) == maxCandidates {
				break
			}
			res.Candidates = append(res.Candidates, Evidence{Path: m.path, Lines: m.lines}.String())
		}
	}
	return res
}

// weakBlock: khối quá ngắn/phổ biến ("}", "return nil") để kết luận một nơi duy nhất là nó.
func weakBlock(lines []string) bool {
	n := 0
	for _, l := range lines {
		for _, r := range l {
			if !unicode.IsSpace(r) {
				n++
			}
		}
	}
	return n < minMoveChars
}

// moveCandidates trả các tệp ứng viên theo thứ tự: đích đổi tên của tệp cũ, tệp đã đổi kể từ
// commit, rồi (khi không có commit dùng được) mọi tệp cùng đuôi, gần thư mục cũ trước.
// Ứng viên không phải đích đổi tên chỉ lấy tệp cùng đuôi với tệp cũ (mục .md trích mã không
// được tính là nơi chứa mã).
func moveCandidates(src Source, e Evidence, commits []string) (out []string, truncated bool) {
	seen := map[string]bool{e.Path: true}
	push := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	ext := path.Ext(e.Path)
	tracked := false
	if ct, ok := src.(ChangeTracker); ok {
		for _, c := range commits {
			if c == "" {
				continue
			}
			cs, ok := ct.ChangedSince(c)
			if !ok {
				continue
			}
			tracked = true
			push(cs.Renames[e.Path])
			files := append([]string(nil), cs.Files...)
			sort.Strings(files)
			for _, f := range files {
				if path.Ext(f) == ext && ValidRelPath(f) == nil {
					push(f)
				}
			}
			break
		}
	}
	if !tracked {
		files := append([]string(nil), src.Files()...)
		sort.Strings(files)
		dir := path.Dir(e.Path)
		sort.SliceStable(files, func(i, j int) bool { return sharedPrefix(files[i], dir) > sharedPrefix(files[j], dir) })
		for _, f := range files {
			if path.Ext(f) == ext {
				push(f)
			}
		}
	}
	if len(out) > maxMoveFiles {
		out, truncated = out[:maxMoveFiles], true
	}
	return out, truncated
}

func sharedPrefix(p, dir string) int {
	n := 0
	for n < len(p) && n < len(dir) && p[n] == dir[n] {
		n++
	}
	return n
}
