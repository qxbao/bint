// Package pathglob khớp đường dẫn tương đối (dấu '/') với mẫu glob kiểu gitignore
// đơn giản. Dùng chung cho khung đánh giá (phân loại tệp) và tri thức (phạm vi).
package pathglob

import (
	"path"
	"regexp"
	"strings"
	"sync"
)

// Pattern là mẫu glob:
//   - "*" khớp mọi ký tự trừ '/', "?" khớp một ký tự trừ '/';
//   - "**" khớp qua nhiều cấp thư mục ("**/" có thể rỗng);
//   - mẫu không chứa '/' được so với tên tệp (basename).
type Pattern struct {
	raw      string
	re       *regexp.Regexp
	basename bool
}

var cache sync.Map

// Compile dịch mẫu glob sang biểu thức chính quy (có bộ đệm).
func Compile(p string) Pattern {
	if v, ok := cache.Load(p); ok {
		return v.(Pattern)
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			if i+2 < len(p) && p[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 2
			} else {
				b.WriteString(".*")
				i++
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	pt := Pattern{raw: p, re: regexp.MustCompile(b.String()), basename: !strings.Contains(p, "/")}
	cache.Store(p, pt)
	return pt
}

// String trả về mẫu gốc.
func (p Pattern) String() string { return p.raw }

// Match cho biết đường dẫn tương đối có khớp mẫu không.
func (p Pattern) Match(rel string) bool {
	if p.basename {
		return p.re.MatchString(path.Base(rel))
	}
	return p.re.MatchString(rel)
}

// MatchAny cho biết rel có khớp một trong các mẫu không.
func MatchAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		if Compile(p).Match(rel) {
			return true
		}
	}
	return false
}

// HasMeta cho biết chuỗi có ký tự glob không.
func HasMeta(p string) bool { return strings.ContainsAny(p, "*?") }
