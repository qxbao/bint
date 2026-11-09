package eval

import "bint/internal/pathglob"

// Pattern là mẫu glob kiểu gitignore đơn giản (xem package pathglob).
type Pattern = pathglob.Pattern

// CompilePattern dịch mẫu glob sang biểu thức chính quy.
func CompilePattern(p string) Pattern { return pathglob.Compile(p) }

// MatchAny cho biết rel có khớp một trong các mẫu không.
func MatchAny(patterns []string, rel string) bool { return pathglob.MatchAny(patterns, rel) }

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
