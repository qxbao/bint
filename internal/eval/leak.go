package eval

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// LeakReport là kết quả kiểm tra rò rỉ lời giải trong đề bài (mục 12.3, 17).
type LeakReport struct {
	Status   string        `yaml:"status"` // pass | warn | fail
	Findings []LeakFinding `yaml:"findings,omitempty"`
}

// LeakFinding là một dấu hiệu rò rỉ.
type LeakFinding struct {
	Severity string `yaml:"severity"` // fail | warn
	Kind     string `yaml:"kind"`
	Detail   string `yaml:"detail"`
}

// LeakInput là phần lời giải dùng để đối chiếu với đề bài.
type LeakInput struct {
	Commit     string
	AddedLines []string        // dòng được thêm vào tệp mã nguồn (không gồm kiểm thử)
	NewIdents  map[string]bool // định danh lời giải đưa vào mà trước đó chưa có
	TestIdents map[string]bool // định danh xuất hiện trong phần kiểm thử được thêm
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{3,}`)

func idents(s string, into map[string]bool) {
	for _, m := range identRe.FindAllString(s, -1) {
		into[m] = true
	}
}

// codeLike: định danh trông như tên trong mã (snake_case, camelCase, có số),
// không phải một từ tiếng Anh thông thường. Chỉ những định danh này mới bị xem
// là dấu hiệu lộ lời giải khi xuất hiện trong đề bài.
func codeLike(id string) bool {
	if strings.Contains(id, "_") || strings.ContainsAny(id, "0123456789") {
		return true
	}
	upper := 0
	for i, r := range id {
		if r >= 'A' && r <= 'Z' {
			upper++
			if i > 0 {
				return true // camelCase hoặc PascalCase nhiều từ
			}
		}
	}
	return false
}

// BuildLeakInput đọc lời giải (diff base..commit) từ repository.
func BuildLeakInput(ctx context.Context, repoDir string, c *Case) (LeakInput, error) {
	in := LeakInput{Commit: c.Source.Commit, NewIdents: map[string]bool{}, TestIdents: map[string]bool{}}
	if len(c.SourceFiles) > 0 {
		added, removed, err := diffLines(ctx, repoDir, c.Source.Base, c.Source.Commit, c.SourceFiles)
		if err != nil {
			return in, err
		}
		in.AddedLines = added
		before := map[string]bool{}
		idents(strings.Join(removed, "\n"), before)
		for _, f := range c.SourceFiles {
			if data, ok := showFile(ctx, repoDir, c.Source.Base, f); ok {
				idents(string(data), before)
			}
		}
		after := map[string]bool{}
		idents(strings.Join(added, "\n"), after)
		for id := range after {
			if !before[id] {
				in.NewIdents[id] = true
			}
		}
	}
	if len(c.TestFiles) > 0 {
		added, _, err := diffLines(ctx, repoDir, c.Source.Base, c.Source.Commit, c.TestFiles)
		if err != nil {
			return in, err
		}
		idents(strings.Join(added, "\n"), in.TestIdents)
	}
	return in, nil
}

func diffLines(ctx context.Context, repoDir, from, to string, files []string) (added, removed []string, err error) {
	args := append([]string{"diff", "--no-renames", "--no-color", "-U0", from, to, "--"}, files...)
	out, err := git(ctx, repoDir, args...)
	if err != nil {
		return nil, nil, err
	}
	for _, ln := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"):
		case strings.HasPrefix(ln, "+"):
			added = append(added, ln[1:])
		case strings.HasPrefix(ln, "-"):
			removed = append(removed, ln[1:])
		}
	}
	return added, removed, nil
}

var (
	wsRe        = regexp.MustCompile(`\s+`)
	commentRe   = regexp.MustCompile(`^(//|#|/\*|\*|--|;|"""|''')`)
	codePunctRe = regexp.MustCompile(`[(){}\[\]=;<>]|\.\w|::|->|:=`)
	promptDiff  = regexp.MustCompile(`(?m)^[+-]\s*\S.*[(){};=]`)
)

func normWS(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

// CheckLeak đối chiếu đề bài với lời giải:
//   - fail: đề bài chứa nguyên một dòng mã của lời giải, hoặc tham chiếu commit lời giải;
//   - warn: đề bài chứa chú thích của lời giải, định danh mới mà kiểm thử không
//     dùng tới (kiểm thử dùng tới thì agent buộc phải biết tên), hoặc trông như có khối mã/diff.
func CheckLeak(prompt string, in LeakInput) LeakReport {
	np := normWS(prompt)
	var fs []LeakFinding
	add := func(sev, kind, detail string) {
		fs = append(fs, LeakFinding{Severity: sev, Kind: kind, Detail: detail})
	}

	seen := map[string]bool{}
	for _, ln := range in.AddedLines {
		n := normWS(ln)
		isComment := commentRe.MatchString(n)
		if isComment {
			// So khớp phần chữ của chú thích, bỏ ký hiệu mở chú thích.
			n = strings.TrimSpace(commentRe.ReplaceAllString(n, ""))
		}
		if len(n) < 16 || seen[n] || !strings.Contains(np, n) {
			continue
		}
		seen[n] = true
		switch {
		case isComment:
			add("warn", "solution_comment", truncate(n, 120))
		case codePunctRe.MatchString(n):
			add("fail", "solution_code", truncate(n, 120))
		}
	}

	var newIDs []string
	for id := range in.NewIdents {
		if codeLike(id) && !in.TestIdents[id] && regexp.MustCompile(`\b`+regexp.QuoteMeta(id)+`\b`).MatchString(prompt) {
			newIDs = append(newIDs, id)
		}
	}
	sort.Strings(newIDs)
	for _, id := range newIDs {
		add("warn", "new_identifier", id+" (lời giải mới đưa vào, kiểm thử không dùng)")
	}

	if in.Commit != "" {
		for _, h := range hexRe.FindAllString(prompt, -1) {
			if strings.HasPrefix(in.Commit, h) {
				add("fail", "solution_commit", h)
			}
		}
	}
	if fenceRe.MatchString(prompt) || strings.Contains(prompt, "```") {
		add("warn", "code_block", "đề bài có khối mã")
	} else if promptDiff.MatchString(prompt) {
		add("warn", "diff_like", "đề bài có dòng trông như diff")
	}

	r := LeakReport{Status: "pass", Findings: fs}
	for _, f := range fs {
		if f.Severity == "fail" {
			r.Status = "fail"
			break
		}
		r.Status = "warn"
	}
	return r
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
