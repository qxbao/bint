// Package symbols xác định vùng dòng của một ký hiệu (hàm, kiểu, khối cấu hình)
// trong tệp nguồn, phục vụ fingerprint theo ký hiệu (mục 8.4).
//
// Kế hoạch đề xuất tree-sitter. Các binding tree-sitter cho Go đều cần CGO, mâu
// thuẫn với quyết định biên dịch Go thuần (P0). Vì vậy:
//   - Go: phân tích chính xác bằng go/parser của thư viện chuẩn.
//   - Ngôn ngữ dùng ngoặc nhọn (JS/TS, Java, Kotlin, Rust, C/C++, C#, PHP, Swift,
//     Scala, Dart): tìm dòng khai báo rồi đếm ngoặc (bỏ qua chuỗi và chú thích
//     đơn giản) — heuristic.
//   - Python, Ruby, YAML: khối theo thụt lề — heuristic. TOML: bảng [tên].
//
// Locator là điểm cắm để thay bằng tree-sitter khi có bản Go thuần đáng tin.
// Khi không xác định được ký hiệu, nơi gọi lùi về fingerprint theo vùng dòng.
package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strings"
)

// Region là vùng dòng [From, To], đánh số từ 1.
type Region struct {
	From, To int
	// Exact = true khi phân tích cú pháp thật (Go), false khi heuristic.
	Exact bool
	// Container = true khi vùng là vật chứa các ký hiệu khác (lớp, kiểu, module): nội
	// dung của nó đổi mỗi khi có thành viên mới, nên không phải bằng chứng ổn định.
	Container bool
}

// Locator tìm vùng của ký hiệu sym trong tệp rel có nội dung src.
type Locator interface {
	Locate(rel string, src []byte, sym string) (Region, bool)
}

// Default là bộ định vị mặc định.
var Default Locator = builtin{}

// Locate dùng bộ định vị mặc định.
func Locate(rel string, src []byte, sym string) (Region, bool) { return Default.Locate(rel, src, sym) }

// Supported cho biết đuôi tệp có bộ định vị.
func Supported(rel string) bool { return lang(rel) != "" }

type builtin struct{}

func lang(rel string) string {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".java", ".kt", ".kts", ".rs", ".c", ".h", ".cc", ".cpp",
		".hpp", ".cs", ".php", ".swift", ".scala", ".dart", ".groovy":
		return "brace"
	case ".py", ".rb":
		return "indent"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "toml"
	}
	return ""
}

func (builtin) Locate(rel string, src []byte, sym string) (Region, bool) {
	sym = strings.TrimSpace(sym)
	if sym == "" {
		return Region{}, false
	}
	lines := splitLines(src)
	switch lang(rel) {
	case "go":
		return locateGo(src, sym)
	case "brace":
		return locateBrace(lines, sym)
	case "indent":
		return locateIndentQualified(lines, sym)
	case "yaml":
		return locateIndent(lines, sym, `^\s*["']?`+regexp.QuoteMeta(lastPart(sym))+`["']?\s*:`)
	case "toml":
		return locateTOML(lines, sym)
	}
	return Region{}, false
}

func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lastPart trả về phần sau dấu chấm cuối cùng ("Type.Method" → "Method").
func lastPart(sym string) string {
	if i := strings.LastIndexAny(sym, ".:#"); i >= 0 && i+1 < len(sym) {
		return sym[i+1:]
	}
	return sym
}

// locateGo nhận "Name", "Type.Method" hoặc "(*Type).Method".
func locateGo(src []byte, sym string) (Region, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil && f == nil {
		return Region{}, false
	}
	recv, name := "", sym
	if i := strings.LastIndex(sym, "."); i > 0 {
		recv, name = strings.Trim(sym[:i], "()*"), sym[i+1:]
	}
	reg := func(n ast.Node) (Region, bool) {
		return Region{From: fset.Position(n.Pos()).Line, To: fset.Position(n.End()).Line, Exact: true}, true
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name.Name != name {
				continue
			}
			r := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				r = recvName(d.Recv.List[0].Type)
			}
			if recv == r || (recv == "" && r == "") {
				return reg(d)
			}
		case *ast.GenDecl:
			if recv != "" {
				// "Type.Field": vùng của kiểu chứa trường.
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == recv {
						if st, ok := ts.Type.(*ast.StructType); ok {
							for _, fl := range st.Fields.List {
								for _, n := range fl.Names {
									if n.Name == name {
										return reg(fl)
									}
								}
							}
						}
					}
				}
				continue
			}
			for _, s := range d.Specs {
				container := false
				var names []*ast.Ident
				switch s := s.(type) {
				case *ast.TypeSpec:
					names = []*ast.Ident{s.Name}
					if s.Name.Name == name {
						container = true
					}
				case *ast.ValueSpec:
					names = s.Names
				}
				for _, n := range names {
					if n.Name == name {
						r, ok := reg(s)
						if len(d.Specs) == 1 {
							r, ok = reg(d)
						}
						r.Container = container
						return r, ok
					}
				}
			}
		}
	}
	return Region{}, false
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

var declKeywords = `(func|function|class|interface|struct|enum|trait|impl|fn|def|type|const|let|var|val|object|record|module|namespace|protocol|extension)`

var braceContainer = regexp.MustCompile(`\b(class|interface|struct|enum|trait|impl|object|module|namespace|protocol|extension)\b`)

func locateBrace(lines []string, sym string) (Region, bool) {
	name := regexp.QuoteMeta(lastPart(sym))
	pats := []*regexp.Regexp{
		// khai báo có từ khoá: "function foo(", "class Foo", "fn foo", "const foo ="
		regexp.MustCompile(`^\s*([\w@<>\[\],.?:]+\s+)*` + declKeywords + `\s*[\w<>\[\]*&:]*\s*\b` + name + `\b`),
		// phương thức/thuộc tính: "foo(...) {", "foo = (", "foo: function"
		regexp.MustCompile(`^\s*([\w@<>\[\],.?]+\s+)*` + name + `\s*(<[^>]*>)?\s*(\(|=\s*(async\s*)?(\(|function)|:\s*(async\s*)?function)`),
	}
	for _, re := range pats {
		for i, l := range lines {
			if !re.MatchString(l) || isCommentLine(l) {
				continue
			}
			if end, ok := braceEnd(lines, i); ok {
				return Region{From: i + 1, To: end + 1, Container: braceContainer.MatchString(l)}, true
			}
		}
	}
	return Region{}, false
}

func isCommentLine(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "#")
}

// braceEnd tìm dòng đóng khối bắt đầu ở dòng start. Khai báo kết thúc bằng ';'
// trước khi gặp '{' là khai báo một dòng (hoặc vài dòng).
func braceEnd(lines []string, start int) (int, bool) {
	depth, opened := 0, false
	inBlock := false
	for i := start; i < len(lines) && i < start+2000; i++ {
		l := lines[i]
		var quote byte
		for j := 0; j < len(l); j++ {
			c := l[j]
			if inBlock {
				if c == '*' && j+1 < len(l) && l[j+1] == '/' {
					inBlock = false
					j++
				}
				continue
			}
			if quote != 0 {
				if c == '\\' {
					j++
				} else if c == quote {
					quote = 0
				}
				continue
			}
			switch c {
			case '"', '\'', '`':
				quote = c
			case '/':
				if j+1 < len(l) && l[j+1] == '/' {
					j = len(l)
				} else if j+1 < len(l) && l[j+1] == '*' {
					inBlock = true
					j++
				}
			case '{':
				depth++
				opened = true
			case '}':
				depth--
				if opened && depth == 0 {
					return i, true
				}
			case ';':
				if !opened && depth == 0 {
					return i, true
				}
			}
		}
	}
	return 0, false
}

var indentContainer = regexp.MustCompile(`^\s*(class|module)\s`)

func locateIndent(lines []string, sym, pattern string) (Region, bool) {
	re := regexp.MustCompile(pattern)
	for i, l := range lines {
		if !re.MatchString(l) {
			continue
		}
		ind := indentOf(l)
		end := i
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if indentOf(lines[j]) <= ind {
				// Ruby: "end" cùng cấp thuộc về khối.
				if t == "end" && indentOf(lines[j]) == ind {
					end = j
				}
				break
			}
			end = j
		}
		return Region{From: i + 1, To: end + 1, Container: indentContainer.MatchString(l)}, true
	}
	return Region{}, false
}

func indentOf(l string) int {
	n := 0
	for _, c := range l {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func locateTOML(lines []string, sym string) (Region, bool) {
	head := "[" + sym + "]"
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t != head && t != "["+head+"]" {
			continue
		}
		end := i
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "[") {
				break
			}
			if strings.TrimSpace(lines[j]) != "" {
				end = j
			}
		}
		return Region{From: i + 1, To: end + 1}, true
	}
	return locateIndent(lines, sym, `^\s*`+regexp.QuoteMeta(lastPart(sym))+`\s*=`)
}

func indentPattern(name string) string {
	return `^\s*(async\s+def|def|class|module)\s+` + regexp.QuoteMeta(name) + `\b`
}

// locateIndentQualified định vị "Lớp.phương_thức" (Python, Ruby): tìm lớp trước rồi
// tìm phương thức BÊN TRONG lớp đó, để hai lớp có phương thức trùng tên không bị nhầm
// theo thứ tự trong tệp. Nếu không tìm thấy phần định danh trước thì lùi về cách cũ
// (khớp theo tên cuối); fingerprint vẫn phải khớp nội dung nên không nới được gì.
func locateIndentQualified(lines []string, sym string) (Region, bool) {
	parts := strings.Split(sym, ".")
	if len(parts) < 2 {
		return locateIndent(lines, sym, indentPattern(lastPart(sym)))
	}
	lo, hi := 0, len(lines)
	var r Region
	for i, p := range parts {
		found, ok := locateIndent(lines[lo:hi], p, indentPattern(p))
		if !ok {
			if i == 0 {
				return locateIndent(lines, sym, indentPattern(lastPart(sym)))
			}
			return Region{}, false
		}
		r = Region{From: found.From + lo, To: found.To + lo, Container: found.Container}
		lo, hi = r.From, r.To // r.From là chỉ số 0 của dòng kế tiếp tiêu đề
	}
	return r, true
}

var indentDef = regexp.MustCompile(`^\s*(async\s+def|def|class)\s+(\w+)`)

// Enclosing trả về ký hiệu hàm/phương thức hẹp nhất (dạng "Lớp.phương_thức") bao trọn
// vùng dòng [from, to]. Hỗ trợ Go và Python/Ruby; ngôn ngữ khác không có kết quả.
func Enclosing(rel string, src []byte, from, to int) (string, Region, bool) {
	switch lang(rel) {
	case "go":
		return enclosingGo(src, from, to)
	case "indent":
		return enclosingIndent(splitLines(src), from, to)
	}
	return "", Region{}, false
}

func enclosingGo(src []byte, from, to int) (string, Region, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil && f == nil {
		return "", Region{}, false
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		r := Region{From: fset.Position(fd.Pos()).Line, To: fset.Position(fd.End()).Line, Exact: true}
		if r.From > from || r.To < to {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			name = recvName(fd.Recv.List[0].Type) + "." + name
		}
		return name, r, true
	}
	return "", Region{}, false
}

func enclosingIndent(lines []string, from, to int) (string, Region, bool) {
	type frame struct {
		ind  int
		name string
	}
	var stack []frame
	best, bestName := Region{}, ""
	for i, l := range lines {
		m := indentDef.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		ind := indentOf(l)
		for len(stack) > 0 && stack[len(stack)-1].ind >= ind {
			stack = stack[:len(stack)-1]
		}
		name := m[2]
		if len(stack) > 0 {
			name = stack[len(stack)-1].name + "." + name
		}
		stack = append(stack, frame{ind, name})
		if m[1] == "class" {
			continue
		}
		r, ok := locateIndent(lines[i:], m[2], indentPattern(m[2]))
		if !ok || r.From != 1 {
			continue
		}
		r = Region{From: i + 1, To: i + r.To}
		if r.From <= from && r.To >= to && r.From >= best.From {
			best, bestName = r, name
		}
	}
	return bestName, best, bestName != ""
}
