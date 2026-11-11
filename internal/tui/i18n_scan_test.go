package tui_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
)

// vietnameseRune cho biết r là chữ cái tiếng Việt có dấu (hoặc đ/Đ).
func vietnameseRune(r rune) bool {
	if r == 'đ' || r == 'Đ' {
		return true
	}
	if r >= 0x1EA0 && r <= 0x1EF9 { // Latin Extended Additional: ạ ả ấ ầ ... ỹ
		return true
	}
	return strings.ContainsRune("àáâãèéêìíòóôõùúýăĩũơưÀÁÂÃÈÉÊÌÍÒÓÔÕÙÚÝĂĨŨƠƯ", r)
}

// tuiGoFiles trả các tệp .go (không phải test) dưới internal/tui.
func tuiGoFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

// Mọi chuỗi giao diện của TUI đi qua danh mục i18n: không literal có chữ tiếng Việt có dấu viết thẳng
// trong mã (chú thích thì được).
func TestNoVietnameseLiteralsInTUICode(t *testing.T) {
	fset := token.NewFileSet()
	var bad []string
	files := tuiGoFiles(t)
	if len(files) < 10 {
		t.Fatalf("quét thiếu tệp: %v", files)
	}
	for _, f := range files {
		if f == filepath.Join("actions", "coverage.go") {
			continue // dữ liệu của test phủ lệnh (lý do loại trừ cho nhà phát triển), không hiện cho người dùng
		}
		node, err := parser.ParseFile(fset, f, nil, 0) // 0: bỏ chú thích
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, r := range s {
				if vietnameseRune(r) {
					bad = append(bad, fset.Position(lit.Pos()).String()+": "+lit.Value)
					break
				}
			}
			return true
		})
	}
	if len(bad) > 0 {
		t.Fatalf("chuỗi tiếng Việt viết thẳng trong mã (phải qua internal/i18n):\n%s", strings.Join(bad, "\n"))
	}
}

// Bài kiểm tra của bài kiểm tra: bộ phát hiện bắt được chữ có dấu.
func TestVietnameseDetectorSanity(t *testing.T) {
	for _, s := range []string{"Tổng quan", "đ", "Đánh giá", "ắ", "ữ"} {
		found := false
		for _, r := range s {
			found = found || vietnameseRune(r)
		}
		if !found {
			t.Errorf("%q phải bị phát hiện", s)
		}
	}
	for _, s := range []string{"Overview", "bint init", "→ ✓ ⟦~⟧"} {
		for _, r := range s {
			if vietnameseRune(r) && unicode.IsLetter(r) {
				t.Errorf("%q không được bị coi là tiếng Việt", s)
			}
		}
	}
}

var keyCallRE = regexp.MustCompile(`\b(?:T|N)\("([a-z0-9_.]+)"`)
var keyFieldRE = regexp.MustCompile(`\b(?:LabelKey|TitleKey|Key):\s*"([a-z0-9_.]+)"`)

// Mọi khoá i18n nhắc trong mã (T/N, LabelKey/TitleKey, tab và nav theo ID màn hình) có trong danh
// mục vi và en; khoá tab.<id>/tab.<id>.short có đủ cho cả bảy tab.
func TestEveryKeyUsedInCodeExists(t *testing.T) {
	vi, en := i18n.MustLoad("vi"), i18n.MustLoad("en")
	seen := map[string]bool{}
	for _, f := range tuiGoFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, re := range []*regexp.Regexp{keyCallRE, keyFieldRE} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				k := m[1]
				if strings.HasSuffix(k, ".") {
					continue // tiền tố ghép động, kiểm bên dưới
				}
				seen[k] = true
			}
		}
	}
	for _, s := range actions.Screens {
		for _, k := range []string{"tab." + string(s), "tab." + string(s) + ".short", "action.nav." + string(s) + ".title"} {
			seen[k] = true
		}
	}
	for _, a := range actions.Default().All() {
		for _, k := range []string{a.LabelKey, a.TitleKey} {
			if k != "" {
				seen[k] = true
			}
		}
	}
	for _, k := range []string{"init.does.config", "init.does.state", "init.does.hooks", "init.does.gitignore",
		"overview.inbox.knowledge", "overview.inbox.evolve", "overview.inbox.doctor_fail", "overview.inbox.doctor_warn"} {
		seen[k] = true
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) < 40 {
		t.Fatalf("quét được quá ít khoá (%d): bộ quét hỏng?", len(keys))
	}
	for _, k := range keys {
		// "Key:" cũng khớp trường phím tắt (Key: "q"); chỉ kiểm khoá trông như khoá i18n (có dấu chấm).
		if !strings.Contains(k, ".") {
			continue
		}
		if !vi.Has(k) || !en.Has(k) {
			t.Errorf("khoá %q thiếu ở vi=%v en=%v", k, vi.Has(k), en.Has(k))
		}
	}
}

// Mọi khoá trong danh mục đều được dùng (không để chuỗi thừa).
func TestNoUnusedKeys(t *testing.T) {
	var src strings.Builder
	for _, f := range tuiGoFiles(t) {
		b, _ := os.ReadFile(f)
		src.Write(b)
	}
	code := src.String()
	vi := i18n.MustLoad("vi")
	dynamicPrefix := []string{"tab.", "action.nav.", "init.does.", "overview.inbox."}
	for _, k := range vi.Keys() {
		if k == "app.name" || strings.Contains(code, `"`+k+`"`) {
			continue
		}
		dyn := false
		for _, p := range dynamicPrefix {
			dyn = dyn || strings.HasPrefix(k, p)
		}
		if !dyn {
			t.Errorf("khoá %q không được dùng trong mã", k)
		}
	}
}
