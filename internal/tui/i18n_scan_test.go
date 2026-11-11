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
	"bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/form"
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
	dynamicPrefix := []string{"tab.", "action.nav.", "init.does.", "overview.inbox.", "overview.runstatus.",
		// Màn Tri thức: khoá ghép từ tên hành động, thẻ phụ, mã finding, chỉ số điều kiện chuyển (có test phủ riêng).
		"action.knowledge.", "kn.sub.", "kn.survey.", "kn.panel.hint.", "kn.reason.title.", "kn.blocked.", "kn.meta.", "kn.insp.",
		"finding.", "transition.", "form."}
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

// Mọi trường của mọi Options gắn với hành động có nhãn form theo quy ước khoá form.<ID hành động>.<tên cờ
// hoặc đối số> ở vi và en (xx sinh từ en); nhãn cũng không thừa so với các trường còn tồn tại.
func TestFormLabelsCoverEveryOptionsField(t *testing.T) {
	vi, en := i18n.MustLoad("vi"), i18n.MustLoad("en")
	want := map[string]bool{}
	for _, a := range actions.Default().All() {
		if a.Options == nil {
			continue
		}
		for _, f := range form.Fields(a.Options) {
			k := form.LabelKey(a.FormNS(), f)
			want[k] = true
			if !vi.Has(k) || !en.Has(k) {
				t.Errorf("hành động %s: thiếu nhãn %q (vi=%v en=%v)", a.ID, k, vi.Has(k), en.Has(k))
			}
		}
	}
	if len(want) < 40 {
		t.Fatalf("quét được quá ít nhãn form (%d)", len(want))
	}
	for _, k := range vi.Keys() {
		if strings.HasPrefix(k, "form.knowledge.") || strings.HasPrefix(k, "form.bootstrap.") || strings.HasPrefix(k, "form.inspect.") || strings.HasPrefix(k, "form.context.") {
			if !want[strings.TrimSuffix(k, ".hint")] {
				t.Errorf("nhãn form %q không ứng với trường nào của Options", k)
			}
		}
	}
}

// Mã finding kiểm tra tri thức và điều kiện chuyển trạng thái được dịch đủ, khớp số dòng của bảng chuyển.
func TestFindingCodesAndTransitionsTranslated(t *testing.T) {
	vi, en := i18n.MustLoad("vi"), i18n.MustLoad("en")
	for _, c := range []string{knowledge.CodeSchema, knowledge.CodeEvidence, knowledge.CodeFingerprint, knowledge.CodeSymbol, knowledge.CodeScope,
		knowledge.CodeExample, knowledge.CodePathRef, knowledge.CodeDuplicate, knowledge.CodeThirdParty, knowledge.CodeOverview,
		knowledge.CodeLength, knowledge.CodeUnreviewed, knowledge.CodeRelocated} {
		if k := "finding." + c; !vi.Has(k) || !en.Has(k) {
			t.Errorf("thiếu khoá %q", k)
		}
	}
	n := 0
	for _, k := range vi.Keys() {
		if strings.HasPrefix(k, "transition.") {
			n++
		}
	}
	if n != len(knowledge.TransitionTable) {
		t.Fatalf("có %d khoá transition.*, bảng chuyển có %d dòng", n, len(knowledge.TransitionTable))
	}
	for i := range knowledge.TransitionTable {
		if k := "transition." + strconv.Itoa(i); !vi.Has(k) || !en.Has(k) {
			t.Errorf("thiếu khoá %q", k)
		}
	}
}
