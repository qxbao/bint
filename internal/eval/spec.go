package eval

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Đặc tả của đề bài (mục 12.2). Đề bài lấy từ thông điệp commit thường chỉ là một
// dòng tiêu đề, trong khi kiểm thử ẩn gọi hàm/lớp/hằng mới mà lời giải đưa vào.
// Agent không thể đoán đúng tên đó, nên tác vụ trở thành không giải được bất kể
// ngữ cảnh tốt đến đâu: mọi nhánh cùng 0 %, phép so sánh không phân biệt được gì.
//
// Bint bổ sung vào cuối đề bài khối "giao diện mà kiểm thử sẽ dùng": chỉ đường
// dẫn và CHỮ KÝ (tên, tham số, kiểu trả về) của những định danh vừa mới trong lời
// giải vừa được kiểm thử dùng tới — không bao giờ có thân hàm hay giá trị hằng.
// Đây là cách các bộ đánh giá như SWE-bench Verified xử lý đề bài thiếu đặc tả.
// Những gì không trích được chữ ký, cùng các chuỗi mà kiểm thử so khớp nguyên văn,
// được ghi thành cảnh báo để người duyệt viết lại đề bài hoặc loại tác vụ.

// InterfaceHeader mở khối giao diện trong đề bài. CheckLeak bỏ qua khối này: chữ
// ký là thông tin agent buộc phải biết (kiểm thử gọi đúng tên đó), không phải lời giải.
const InterfaceHeader = "Interface the hidden tests will use (signatures only; implement the behaviour yourself):"

// MinPromptChars: đề bài (không kể khối giao diện) ngắn hơn mức này bị cảnh báo
// thiếu đặc tả. Ngưỡng định hướng, chưa hiệu chỉnh từ dữ liệu.
const MinPromptChars = 100

// minExactText: chuỗi kiểm thử so khớp từ độ dài này mới xét là "câu chữ phải đoán".
const minExactText = 12

// SpecReport là kết quả phân tích đặc tả của đề bài.
type SpecReport struct {
	Status    string          `yaml:"status"` // pass | warn
	Interface []InterfaceHint `yaml:"interface,omitempty"`
	Findings  []LeakFinding   `yaml:"findings,omitempty"`
}

// InterfaceHint là chữ ký một định danh mà kiểm thử sẽ dùng.
type InterfaceHint struct {
	Path      string `yaml:"path"`
	Name      string `yaml:"name"`
	Owner     string `yaml:"owner,omitempty"` // lớp chứa (nếu là phương thức)
	Signature string `yaml:"signature"`
}

// DeriveSpec tìm chữ ký của các định danh mới mà kiểm thử dùng tới và các dấu
// hiệu đề bài thiếu đặc tả. prompt là đề bài KHÔNG kèm khối giao diện.
func DeriveSpec(ctx context.Context, repoDir string, c *Case, prompt string, in LeakInput) SpecReport {
	var rep SpecReport
	add := func(kind, detail string) {
		rep.Findings = append(rep.Findings, LeakFinding{Severity: "warn", Kind: kind, Detail: detail})
	}

	var required []string
	for id := range in.NewIdents {
		if in.TestIdents[id] {
			required = append(required, id)
		}
	}
	sort.Strings(required)

	type srcFile struct {
		path  string
		lines []string
		added map[string]bool
	}
	var files []srcFile
	for _, f := range c.SourceFiles {
		data, ok := showFile(ctx, repoDir, c.Source.Commit, f)
		if !ok {
			continue
		}
		added, _, err := diffLines(ctx, repoDir, c.Source.Base, c.Source.Commit, []string{f})
		if err != nil {
			continue
		}
		am := map[string]bool{}
		for _, a := range added {
			am[strings.TrimSpace(a)] = true
		}
		files = append(files, srcFile{path: f, lines: strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), added: am})
	}

	var gaps []string
	for _, name := range required {
		found := false
		for _, sf := range files {
			for i, ln := range sf.lines {
				if !sf.added[strings.TrimSpace(ln)] || !declares(sf.path, ln, name) {
					continue
				}
				sig := signature(sf.path, sf.lines, i)
				if sig == "" {
					continue
				}
				rep.Interface = append(rep.Interface, InterfaceHint{Path: sf.path, Name: name, Owner: owner(sf.path, sf.lines, i), Signature: sig})
				found = true
				break
			}
			if found {
				break
			}
		}
		// Chỉ là khoảng trống đặc tả khi tên thật sự mới với cả repository: tên có sẵn
		// (builtin, biến môi trường, fixture, hằng ở module khác) agent tìm được.
		if !found && codeLike(name) && !hexLikeRe.MatchString(name) && !existsAt(ctx, repoDir, c.Source.Base, name) {
			gaps = append(gaps, name)
		}
	}
	if len(gaps) > 0 {
		add("interface_gap", fmt.Sprintf("kiểm thử dùng %d định danh mới không trích được chữ ký (khoá, chuỗi, thuộc tính…): %s",
			len(gaps), strings.Join(firstN(gaps, 6), ", ")))
	}

	if n := len([]rune(strings.TrimSpace(prompt))); n < MinPromptChars {
		add("short_prompt", fmt.Sprintf("đề bài chỉ %d ký tự (< %d): thường chỉ là tiêu đề commit, thiếu hành vi mong đợi", n, MinPromptChars))
	}

	if texts := exactTexts(in, prompt); len(texts) > 0 {
		add("exact_text", fmt.Sprintf("kiểm thử so khớp nguyên văn %d chuỗi mới của lời giải mà đề bài không nêu, ví dụ %q",
			len(texts), truncate(texts[0], 60)))
	}

	rep.Status = "pass"
	if len(rep.Findings) > 0 {
		rep.Status = "warn"
	}
	return rep
}

// WithInterface nối khối giao diện vào cuối đề bài (thay khối cũ nếu có).
func WithInterface(prompt string, hints []InterfaceHint) string {
	body := StripInterface(prompt)
	if len(hints) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n")
	b.WriteString(InterfaceHeader)
	for _, h := range hints {
		loc := h.Path
		if h.Owner != "" {
			loc += " (in " + h.Owner + ")"
		}
		fmt.Fprintf(&b, "\n- %s: `%s`", loc, h.Signature)
	}
	return b.String()
}

// StripInterface trả về đề bài không kèm khối giao diện.
func StripInterface(prompt string) string {
	if i := strings.Index(prompt, InterfaceHeader); i >= 0 {
		return strings.TrimRight(prompt[:i], " \t\n")
	}
	return prompt
}

// exactTexts: chuỗi trong phần kiểm thử được thêm, cũng nằm trong mã lời giải
// được thêm, nhưng đề bài không nêu — agent phải đoán đúng từng chữ.
func exactTexts(in LeakInput, prompt string) []string {
	src := strings.Join(in.AddedLines, "\n")
	np := normWS(prompt)
	seen := map[string]bool{}
	var out []string
	for _, m := range stringLitRe.FindAllStringSubmatch(strings.Join(in.TestAdded, "\n"), -1) {
		s := m[1] + m[2]
		if len([]rune(s)) < minExactText || seen[s] || strings.Contains(np, normWS(s)) || !strings.Contains(src, s) {
			continue
		}
		if !strings.ContainsAny(s, " \t") { // một từ/định danh: đã xét ở interface_gap
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

var hexLikeRe = regexp.MustCompile(`^[0-9a-fA-F]{6,}$`)

// existsAt cho biết định danh đã xuất hiện ở đâu đó trong repository tại commit rev.
func existsAt(ctx context.Context, repoDir, rev, name string) bool {
	_, err := git(ctx, repoDir, "grep", "-q", "-w", "-F", "-e", name, rev, "--")
	return err == nil
}

var stringLitRe = regexp.MustCompile(`"((?:[^"\\\n]|\\.)*)"|'((?:[^'\\\n]|\\.)*)'`)

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(append([]string{}, s[:n]...), "…")
	}
	return s
}

func langOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".py", ".pyi":
		return "py"
	case ".go":
		return "go"
	case ".php":
		return "php"
	case ".rb":
		return "rb"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return "js"
	}
	return "brace" // Java, Kotlin, C#, Rust, Swift, C/C++…
}

// declares cho biết dòng ln khai báo name (hàm, phương thức, lớp, kiểu, hằng).
func declares(p, ln, name string) bool {
	q := regexp.QuoteMeta(name)
	var pats []string
	switch langOf(p) {
	case "py":
		pats = []string{`^\s*(?:async\s+)?def\s+` + q + `\s*\(`, `^\s*class\s+` + q + `\b`, `^` + q + `\s*(?::[^=]*)?=[^=]`}
	case "go":
		pats = []string{`^func\s+(?:\([^)]*\)\s*)?` + q + `\s*[\[(]`, `^\s*type\s+` + q + `\b`, `^(?:const|var)\s+` + q + `\b`}
	case "php":
		pats = []string{`\bfunction\s+` + q + `\s*\(`, `^\s*(?:(?:abstract|final|readonly)\s+)*(?:class|interface|trait|enum)\s+` + q + `\b`, `\bconst\s+` + q + `\b`}
	case "rb":
		pats = []string{`^\s*def\s+(?:self\.)?` + q + `\b`, `^\s*(?:class|module)\s+` + q + `\b`}
	case "js":
		pats = []string{
			`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*` + q + `\s*[(<]`,
			`^\s*(?:export\s+)?(?:const|let|var)\s+` + q + `\b`,
			`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?(?:class|interface|enum)\s+` + q + `\b`,
			`^\s*(?:export\s+)?type\s+` + q + `\b`,
			`^\s*(?:(?:public|private|protected|static|async|readonly|override)\s+)*` + q + `\s*[(<][^;]*\{\s*$`,
		}
	default:
		pats = []string{`^\s*(?:[\w<>\[\],?]+\s+)+` + q + `\s*\([^;]*$`, `^\s*(?:\w+\s+)*(?:class|interface|struct|enum|trait|record)\s+` + q + `\b`}
	}
	for _, s := range pats {
		if regexp.MustCompile(s).MatchString(ln) {
			return true
		}
	}
	return false
}

// signature trích chữ ký bắt đầu ở dòng i: gom tối đa 12 dòng tới khi ngoặc tròn
// cân bằng, rồi cắt trước phần thân (":" của Python, "{" hay "=>" của ngôn ngữ
// ngoặc nhọn) hoặc trước giá trị của hằng/biến.
func signature(p string, lines []string, i int) string {
	first := strings.TrimSpace(lines[i])
	if eq := assignIndex(first); eq >= 0 {
		rest := strings.TrimSpace(first[eq+1:])
		if !(langOf(p) == "js" && arrowRe.MatchString(rest)) {
			// Hằng/biến: chỉ tên và kiểu, không bao giờ có giá trị.
			return strings.TrimSpace(strings.TrimRight(first[:eq], " :"))
		}
	}
	var b strings.Builder
	depth, opened := 0, false
	for j := i; j < len(lines) && j < i+12; j++ {
		ln := lines[j]
		if j > i {
			b.WriteByte(' ')
		}
		b.WriteString(strings.TrimSpace(ln))
		for _, r := range ln {
			switch r {
			case '(':
				depth++
				opened = true
			case ')':
				depth--
			}
		}
		if depth <= 0 {
			break
		}
	}
	s := normWS(b.String())
	if !opened {
		s = cutValue(s)
	} else {
		s = cutBody(langOf(p), s)
	}
	s = strings.TrimSpace(strings.TrimRight(s, " {:;,"))
	if len([]rune(s)) > 300 {
		s = truncate(s, 300)
	}
	return s
}

// cutBody cắt phần thân sau danh sách tham số (và kiểu trả về).
func cutBody(lang, s string) string {
	depth, closed := 0, -1
	for k, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && closed < 0 {
				closed = k
			}
		}
	}
	if closed < 0 {
		return s
	}
	head, tail := s[:closed+1], s[closed+1:]
	if lang == "py" {
		// "-> Kiểu:" rồi thân; ":" đầu tiên ở mức ngoặc 0 kết thúc chữ ký.
		if k := topLevelIndex(tail, ":"); k >= 0 {
			tail = tail[:k]
		}
		return head + tail
	}
	if lang == "js" && strings.Contains(head, "=") && !strings.Contains(strings.SplitN(head, "(", 2)[0], "function") {
		// const f = (a, b) => …  hoặc  const f = async (a) => …
		if k := strings.Index(tail, "=>"); k >= 0 {
			return head + tail[:k]
		}
	}
	for _, stop := range []string{"{", "=>", "=", ";"} {
		if k := topLevelIndex(tail, stop); k >= 0 {
			tail = tail[:k]
		}
	}
	return head + tail
}

var arrowRe = regexp.MustCompile(`^(?:async\s*)?(?:\(|function\b|[A-Za-z_$][\w$]*\s*=>)`)

// assignIndex trả về vị trí dấu "=" của phép gán ở dòng khai báo (trước mọi
// ngoặc tròn), bỏ qua ==, =>, <=, >=, !=, :=; -1 nếu không phải phép gán.
func assignIndex(s string) int {
	for k := 0; k < len(s); k++ {
		switch s[k] {
		case '(':
			return -1
		case '=':
			if k+1 < len(s) && (s[k+1] == '=' || s[k+1] == '>') {
				k++
				continue
			}
			if k > 0 && strings.ContainsRune("<>!=:", rune(s[k-1])) {
				continue
			}
			return k
		}
	}
	return -1
}

// cutValue bỏ giá trị của hằng/biến/kiểu: "X: int = 3" → "X: int", "type T struct {" → "type T struct".
func cutValue(s string) string {
	for _, stop := range []string{" = ", "=", "{"} {
		if k := strings.Index(s, stop); k >= 0 {
			return s[:k]
		}
	}
	return s
}

func topLevelIndex(s, sub string) int {
	depth := 0
	for k := 0; k < len(s); k++ {
		switch s[k] {
		case '(', '[', '<':
			depth++
		case ')', ']', '>':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(s[k:], sub) {
			return k
		}
	}
	return -1
}

var (
	pyClassRe    = regexp.MustCompile(`^(\s*)class\s+([A-Za-z_]\w*)`)
	braceClassRe = regexp.MustCompile(`^(\s*)(?:export\s+)?(?:(?:public|private|protected|abstract|final|static|default|sealed|partial|data|open)\s+)*(?:class|interface|struct|trait|enum|object|impl)\s+([A-Za-z_]\w*)`)
)

// owner tìm lớp bao quanh dòng i (theo thụt lề), nếu dòng i được thụt vào.
func owner(p string, lines []string, i int) string {
	ind := indentOf(lines[i])
	if ind == 0 || langOf(p) == "go" {
		return ""
	}
	re := braceClassRe
	if langOf(p) == "py" || langOf(p) == "rb" {
		re = pyClassRe
	}
	for j := i - 1; j >= 0; j-- {
		ln := lines[j]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if indentOf(ln) < ind {
			if m := re.FindStringSubmatch(ln); m != nil {
				return m[2]
			}
			if langOf(p) == "py" {
				return "" // thụt trong hàm, không phải lớp
			}
		}
	}
	return ""
}

func indentOf(s string) int {
	n := 0
	for _, r := range s {
		switch r {
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

// ApplySpec phân tích đặc tả của tác vụ và nối khối giao diện vào đề bài.
func ApplySpec(ctx context.Context, repoDir string, c *Case, in LeakInput) {
	body := StripInterface(c.Prompt)
	rep := DeriveSpec(ctx, repoDir, c, body, in)
	c.Spec = &rep
	c.Prompt = WithInterface(body, rep.Interface)
}
