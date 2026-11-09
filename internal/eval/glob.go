package eval

import (
	"path"
	"regexp"
	"strings"
	"sync"
)

// Pattern là mẫu glob kiểu gitignore đơn giản trên đường dẫn dùng '/':
//   - "*" khớp mọi ký tự trừ '/', "?" khớp một ký tự trừ '/';
//   - "**" khớp qua nhiều cấp thư mục ("**/" có thể rỗng);
//   - mẫu không chứa '/' được so với tên tệp (basename).
type Pattern struct {
	raw      string
	re       *regexp.Regexp
	basename bool
}

var patternCache sync.Map

// CompilePattern dịch mẫu glob sang biểu thức chính quy.
func CompilePattern(p string) Pattern {
	if v, ok := patternCache.Load(p); ok {
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
	patternCache.Store(p, pt)
	return pt
}

// Match cho biết đường dẫn tương đối (dấu '/') có khớp mẫu không.
func (p Pattern) Match(rel string) bool {
	if p.basename {
		return p.re.MatchString(path.Base(rel))
	}
	return p.re.MatchString(rel)
}

// MatchAny cho biết rel có khớp một trong các mẫu không.
func MatchAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		if CompilePattern(p).Match(rel) {
			return true
		}
	}
	return false
}

// DefaultTestPatterns nhận diện tệp kiểm thử và dữ liệu kiểm thử ở các hệ sinh thái phổ biến.
var DefaultTestPatterns = []string{
	"*_test.go", "**/testdata/**",
	"test_*.py", "*_test.py", "conftest.py",
	"*.test.js", "*.test.jsx", "*.test.ts", "*.test.tsx", "*.test.mjs", "*.test.cjs",
	"*.spec.js", "*.spec.jsx", "*.spec.ts", "*.spec.tsx", "*.spec.mjs", "*.spec.cjs",
	"**/__tests__/**", "**/__snapshots__/**", "**/fixtures/**",
	"**/test/**", "**/tests/**",
	"*Test.java", "*Tests.java", "*Test.kt", "*Tests.kt", "*Test.cs", "*Tests.cs",
	"*_spec.rb", "**/spec/**", "*Test.php",
}

// DefaultIgnorePatterns là các tệp không tính vào quy mô lời giải (tài liệu, lockfile,
// tệp ngữ cảnh của agent). Chúng vẫn là một phần của commit khi kiểm chứng.
var DefaultIgnorePatterns = []string{
	"*.md", "*.mdx", "*.rst", "*.txt", "LICENSE*", "CHANGELOG*", "docs/**",
	"go.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb",
	"Cargo.lock", "poetry.lock", "uv.lock", "Gemfile.lock", "composer.lock",
	".bint/**", ".claude/**", ".agents/**",
}
