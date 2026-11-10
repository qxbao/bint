package eval

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"bint/internal/config"
)

// MineOptions điều khiển việc quét lịch sử.
type MineOptions struct {
	Rev   string // điểm bắt đầu quét (mặc định HEAD)
	Limit int    // số ứng viên mới tối đa (0 = không giới hạn)
	Now   func() time.Time
}

// Lý do bỏ qua commit khi khai thác.
const (
	SkipNoParent = "commit gốc (không có commit cha)"
	SkipNoTests  = "không thêm/sửa kiểm thử"
	SkipNoSource = "không sửa mã nguồn"
	SkipTooSmall = "thay đổi mã nguồn quá nhỏ"
	SkipTooLarge = "thay đổi quá lớn"
	SkipBinary   = "có tệp nhị phân trong mã nguồn"
	SkipShortMsg = "mô tả quá ngắn để làm đề bài"
	SkipKnown    = "đã có (ứng viên hoặc tác vụ)"
)

// MineResult là kết quả khai thác.
type MineResult struct {
	Scanned int
	New     []*Case
	Skipped map[string]int
}

type logCommit struct {
	hash, author, date, message string
	parents                     []string
	files                       []FileChange
}

// Classifier phân loại tệp của commit thành kiểm thử / mã nguồn / phụ trợ.
type Classifier struct {
	Test   []string
	Ignore []string
}

// NewClassifier dựng Classifier từ cấu hình (mẫu rỗng = mẫu dựng sẵn).
func NewClassifier(m config.EvalMine) Classifier {
	c := Classifier{Test: DefaultTestPatterns, Ignore: slices.Concat(DefaultIgnorePatterns, m.IgnorePatterns)}
	if len(m.TestPatterns) > 0 {
		c.Test = m.TestPatterns
	}
	return c
}

// Kind trả về "test", "aux" hoặc "source".
func (c Classifier) Kind(path string) string {
	switch {
	case MatchAny(c.Test, path):
		return "test"
	case MatchAny(c.Ignore, path):
		return "aux"
	}
	return "source"
}

// Mine quét lịch sử Git và tạo ứng viên tác vụ đánh giá (mục 12.2) cho các commit:
// có một commit cha; thêm hoặc sửa kiểm thử và sửa mã nguồn; quy mô mã nguồn
// trong giới hạn; mô tả đủ dài. Ứng viên đã có (kể cả đã loại) không bị tạo lại.
func Mine(ctx context.Context, repoDir string, m config.EvalMine, st Store, opts MineOptions) (MineResult, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rev == "" {
		opts.Rev = "HEAD"
	}
	res := MineResult{Skipped: map[string]int{}}
	known := map[string]bool{}
	for _, list := range []func() ([]*Case, error){st.Candidates, st.Cases} {
		cs, err := list()
		if err != nil {
			return res, err
		}
		for _, c := range cs {
			known[c.ID] = true
		}
	}
	commits, err := readLog(ctx, repoDir, opts.Rev, m.MaxCommits)
	if err != nil {
		return res, err
	}
	cls := NewClassifier(m)
	for _, lc := range commits {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Scanned++
		if opts.Limit > 0 && len(res.New) >= opts.Limit {
			break
		}
		if len(lc.parents) != 1 {
			res.Skipped[SkipNoParent]++
			continue
		}
		id := CaseID(lc.hash)
		if known[id] {
			res.Skipped[SkipKnown]++
			continue
		}
		c, reason := buildCase(lc, cls, m)
		if reason != "" {
			res.Skipped[reason]++
			continue
		}
		c.ID = id
		c.MinedAt = nowString(opts.Now())
		in, err := BuildLeakInput(ctx, repoDir, c)
		if err != nil {
			return res, err
		}
		ApplySpec(ctx, repoDir, c, in)
		c.Leak = CheckLeak(c.Prompt, in)
		if err := st.SaveCandidate(c); err != nil {
			return res, err
		}
		known[id] = true
		res.New = append(res.New, c)
	}
	return res, nil
}

func buildCase(lc logCommit, cls Classifier, m config.EvalMine) (*Case, string) {
	c := &Case{
		Schema: CaseSchemaVersion,
		Status: StatusCandidate,
		Source: Source{Commit: lc.hash, Base: lc.parents[0], Author: lc.author, Date: lc.date},
	}
	testAdded := false
	for _, f := range lc.files {
		switch cls.Kind(f.Path) {
		case "test":
			c.TestFiles = append(c.TestFiles, f.Path)
			c.Stats.TestLines += f.Added + f.Deleted
			if f.Added > 0 || f.Binary {
				testAdded = true
			}
		case "aux":
			c.AuxFiles = append(c.AuxFiles, f.Path)
		default:
			if f.Binary {
				return nil, SkipBinary
			}
			c.SourceFiles = append(c.SourceFiles, f.Path)
			c.Stats.SourceLines += f.Added + f.Deleted
		}
	}
	c.Stats.Files = len(lc.files)
	switch {
	case !testAdded:
		return nil, SkipNoTests
	case len(c.SourceFiles) == 0:
		return nil, SkipNoSource
	case c.Stats.SourceLines < m.MinSourceLines:
		return nil, SkipTooSmall
	case c.Stats.SourceLines > m.MaxSourceLines || c.Stats.Files > m.MaxFiles:
		return nil, SkipTooLarge
	}
	subject, _, _ := strings.Cut(strings.TrimSpace(lc.message), "\n")
	c.Source.Subject = strings.TrimSpace(subject)
	c.OriginalMessage = strings.TrimSpace(lc.message)
	c.Prompt = DraftPrompt(lc.message, lc.hash)
	if len([]rune(c.Prompt)) < m.MinMessageChars {
		return nil, SkipShortMsg
	}
	return c, ""
}

const (
	recSep   = "\x1e"
	fieldSep = "\x1f"
)

// readLog đọc tối đa max commit không phải merge kể từ rev, kèm numstat.
func readLog(ctx context.Context, repoDir, rev string, max int) ([]logCommit, error) {
	out, err := git(ctx, repoDir, "log", "--no-merges", "--no-renames", "--numstat",
		"-n", strconv.Itoa(max), "--format="+recSep+"%H"+fieldSep+"%P"+fieldSep+"%an <%ae>"+fieldSep+"%aI"+fieldSep+"%B"+fieldSep, rev, "--")
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") || strings.Contains(err.Error(), "unknown revision") {
			return nil, fmt.Errorf("không đọc được lịch sử từ %s: %w", rev, err)
		}
		return nil, err
	}
	var commits []logCommit
	for _, rec := range strings.Split(string(out), recSep) {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, fieldSep, 6)
		if len(f) != 6 {
			continue
		}
		lc := logCommit{hash: f[0], parents: strings.Fields(f[1]), author: f[2], date: f[3], message: f[4]}
		for _, ln := range strings.Split(f[5], "\n") {
			p := strings.SplitN(strings.TrimRight(ln, "\r"), "\t", 3)
			if len(p) != 3 {
				continue
			}
			fc := FileChange{Path: unquotePath(p[2])}
			if p[0] == "-" {
				fc.Binary = true
			} else {
				fc.Added, _ = strconv.Atoi(p[0])
				fc.Deleted, _ = strconv.Atoi(p[1])
			}
			lc.files = append(lc.files, fc)
		}
		commits = append(commits, lc)
	}
	return commits, nil
}

// unquotePath bỏ dấu ngoặc kép kiểu C mà git dùng cho đường dẫn đặc biệt.
func unquotePath(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		if s, err := strconv.Unquote(p); err == nil {
			return s
		}
	}
	return p
}
