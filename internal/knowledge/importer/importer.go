// Package importer nhập bộ nhớ sẵn có của harness (Claude auto memory, Codex memories, .cursor/rules, một tệp Markdown)
// thành ý ứng viên cho mục tri thức imported (docs/next-directions.md mục B3). Toàn bộ xác định: không gọi mô hình,
// không mạng. Gói chỉ đọc; việc dựng mục, kiểm tra và ghi thuộc internal/app (RequireHuman, repoWriter).
package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"bint/internal/knowledge"
)

// Lý do bỏ qua một ý (ImportIdea.Reason ở internal/app).
const (
	SkipNoEvidence      = "no_evidence"      // không tìm được tệp/ký hiệu trong repository để làm bằng chứng
	SkipDuplicate       = "duplicate"        // trùng mục đang có hiệu lực
	SkipAlreadyImported = "already_imported" // đã nhập trước đó (kể cả mục đã bị từ chối)
	SkipWorkState       = "work_state"       // ghi chú trạng thái công việc, không phải tri thức về mã
	SkipPrivate         = "private"          // bộ nhớ Claude loại user (riêng tư)
	SkipBintDerived     = "bint_derived"     // nội dung do chính Bint sinh ra
	SkipIndexFile       = "index_file"       // tệp chỉ mục MEMORY.md
	SkipCheckFailed     = "check_failed"     // không qua Verifier
)

// Giới hạn đọc: bộ nhớ ngoài là dữ liệu không tin cậy, nên chặn kích thước và số tệp.
const (
	MaxFileBytes = 256 << 10
	MaxFiles     = 500
	maxTitle     = 120
	minIdeaRunes = 20
)

// ErrNoSource: không tìm thấy nguồn nhập ở các vị trí đã thử.
var ErrNoSource = errors.New("không tìm thấy nguồn nhập")

// Idea là một ý tách từ nguồn, chưa có bằng chứng.
type Idea struct {
	Source string // knowledge.ImportClaudeMemory...
	// Ref là tham chiếu nguồn không chứa đường dẫn home ("claude-memory:feedback_x.md", "cursor-rules:.cursor/rules/a.mdc#2").
	Ref   string
	Title string
	Body  string
	Paths []string // phạm vi do nguồn khai báo (globs của rule Cursor)
	Type  string   // loại gợi ý; rỗng = để nơi gọi chọn
	// Skip != "" là ý bị bỏ qua ngay từ bước tách (một trong Skip*), SkipDetail giải thích.
	Skip       string
	SkipDetail string
}

// File là một tệp nguồn đã định vị. Name là tên tương đối dùng trong Ref (không chứa home).
type File struct {
	Name string
	Abs  string
}

// LocateOptions là đầu vào của Locate.
type LocateOptions struct {
	Root     string // gốc repository (cây làm việc hiện tại)
	MainRoot string // gốc cây chính khi đang ở worktree; rỗng hoặc trùng Root thì bỏ qua
	Path     string // ghi đè thư mục/tệp nguồn
	// Getenv và Home thay cho môi trường thật khi kiểm thử; rỗng = dùng của tiến trình.
	Getenv func(string) string
	Home   string
}

func (o LocateOptions) env(k string) string {
	if o.Getenv != nil {
		return o.Getenv(k)
	}
	return os.Getenv(k)
}

func (o LocateOptions) home() string {
	if o.Home != "" {
		return o.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// ParseFrom tách giá trị --from: tên nguồn đã biết, hoặc đường dẫn một tệp (source = file).
func ParseFrom(from, path string) (source, p string, err error) {
	from = strings.TrimSpace(from)
	switch {
	case from == "":
		return "", "", errors.New("thiếu nguồn nhập")
	case strings.Contains(from, "://"):
		return "", "", fmt.Errorf("%q: chỉ nhận tệp cục bộ, không nhận URL", from)
	}
	for _, s := range knowledge.ImportSources {
		if from == s {
			if s == knowledge.ImportFile && strings.TrimSpace(path) == "" {
				return "", "", errors.New("nguồn file cần đường dẫn tệp (--path)")
			}
			return s, strings.TrimSpace(path), nil
		}
	}
	// Không phải tên nguồn: coi là đường dẫn tệp.
	if strings.TrimSpace(path) != "" {
		return "", "", fmt.Errorf("nguồn %q không hợp lệ (%s, hoặc đường dẫn tệp)", from, strings.Join(knowledge.ImportSources, ", "))
	}
	return knowledge.ImportFile, from, nil
}

// SlugOf là tên thư mục dự án của Claude Code: đường dẫn tuyệt đối với mọi ký tự không phải chữ/số ASCII thành '-'.
func SlugOf(abs string) string {
	b := []byte(abs)
	for i, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			b[i] = '-'
		}
	}
	return string(b)
}

// Locate liệt kê tệp của nguồn. Chưa đọc nội dung.
func Locate(source string, o LocateOptions) (files []File, err error) {
	defer func() { err = sourceError(err) }()
	switch source {
	case knowledge.ImportClaudeMemory:
		return locateClaude(o)
	case knowledge.ImportCodexMemory:
		dir := o.Path
		if dir == "" {
			base := o.env("CODEX_HOME")
			if base == "" {
				base = filepath.Join(o.home(), ".codex")
			}
			dir = filepath.Join(base, "memories")
		}
		return walkDir(dir, "", func(rel string) bool { return isMarkdown(rel) })
	case knowledge.ImportCursorRules:
		return locateCursor(o)
	case knowledge.ImportFile:
		if o.Path == "" {
			return nil, errors.New("thiếu đường dẫn tệp")
		}
		st, err := os.Stat(o.Path)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("%s không phải tệp thường", filepath.Base(o.Path))
		}
		return []File{{Name: filepath.Base(o.Path), Abs: o.Path}}, nil
	}
	return nil, fmt.Errorf("nguồn %q không hợp lệ (%s)", source, strings.Join(knowledge.ImportSources, ", "))
}

func locateClaude(o LocateOptions) ([]File, error) {
	if o.Path != "" {
		st, err := os.Stat(o.Path)
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			return walkDir(o.Path, "", func(rel string) bool { return !strings.Contains(rel, "/") && isMarkdown(rel) })
		}
		return []File{{Name: filepath.Base(o.Path), Abs: o.Path}}, nil
	}
	base := o.env("CLAUDE_CONFIG_DIR")
	if base == "" {
		base = filepath.Join(o.home(), ".claude")
	}
	var tried []string
	for _, root := range []string{o.Root, o.MainRoot} {
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		dir := filepath.Join(base, "projects", SlugOf(root), "memory")
		if slicesContains(tried, dir) {
			continue
		}
		tried = append(tried, dir)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return walkDir(dir, "", func(rel string) bool { return !strings.Contains(rel, "/") && isMarkdown(rel) })
		}
	}
	return nil, fmt.Errorf("%w: bộ nhớ Claude của dự án (thử %d thư mục theo tên dự án); dùng --path để chỉ thẳng thư mục", ErrNoSource, len(tried))
}

func locateCursor(o LocateOptions) ([]File, error) {
	if o.Path != "" {
		st, err := os.Stat(o.Path)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			return []File{{Name: filepath.Base(o.Path), Abs: o.Path}}, nil
		}
		return walkDir(o.Path, "", func(rel string) bool { return isMarkdown(rel) })
	}
	var out []File
	rules := filepath.Join(o.Root, ".cursor", "rules")
	if fs, err := walkDir(rules, ".cursor/rules/", func(rel string) bool { return isMarkdown(rel) }); err == nil {
		out = append(out, fs...)
	}
	if st, err := os.Stat(filepath.Join(o.Root, ".cursorrules")); err == nil && st.Mode().IsRegular() {
		out = append(out, File{Name: ".cursorrules", Abs: filepath.Join(o.Root, ".cursorrules")})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: không có .cursor/rules/*.mdc hay .cursorrules trong repository", ErrNoSource)
	}
	return out, nil
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func isMarkdown(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".md", ".mdc", ".markdown", ".txt":
		return true
	}
	return false
}

// walkDir liệt kê tệp thường dưới dir thoả keep (đường dẫn tương đối dạng '/'), không theo symlink, tối đa MaxFiles.
func walkDir(dir, prefix string, keep func(rel string) bool) ([]File, error) {
	st, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: không có thư mục nguồn", ErrNoSource)
		}
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s không phải thư mục", filepath.Base(dir))
	}
	var out []File
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if keep(rel) && len(out) < MaxFiles {
			out = append(out, File{Name: prefix + rel, Abs: p})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: thư mục nguồn không có tệp Markdown", ErrNoSource)
	}
	return out, nil
}

// Read đọc tệp nguồn, chặn tệp quá lớn.
func Read(f File) (data []byte, err error) {
	defer func() { err = sourceError(err) }()
	st, err := os.Stat(f.Abs)
	if err != nil {
		return nil, err
	}
	if st.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%s: tệp quá lớn (%d KB, tối đa %d KB)", f.Name, st.Size()>>10, MaxFileBytes>>10)
	}
	return os.ReadFile(f.Abs)
}

// sourceError giữ nguyên loại lỗi, nhưng không đưa đường dẫn nguồn riêng tư vào báo cáo.
func sourceError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: pe.Op, Path: filepath.Base(pe.Path), Err: pe.Err}
	}
	return err
}

var (
	bintBlockRe = regexp.MustCompile(`(?s)<!--\s*bint:begin.*?<!--\s*bint:end[^>]*-->`)
	bintOpenRe  = regexp.MustCompile(`<!--\s*bint:(begin|end)`)
	kRefRe      = regexp.MustCompile(`\[k:[0-9A-Za-z]{10,}\]`)
)

// Split tách tệp nguồn thành ý. name là Name của File; data là nội dung.
func Split(source, name string, data []byte) []Idea {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimPrefix(text, string(rune(0xFEFF)))
	base := filepath.Base(name)
	if strings.EqualFold(base, "MEMORY.md") {
		return []Idea{{Source: source, Ref: source + ":" + name, Title: name, Skip: SkipIndexFile,
			SkipDetail: "tệp chỉ mục MEMORY.md chỉ liệt kê các ghi chú khác"}}
	}
	fm, body, err := splitFrontmatter(text)
	if err != nil {
		return []Idea{{Source: source, Ref: source + ":" + name, Title: name, Skip: SkipCheckFailed,
			SkipDetail: "frontmatter không hợp lệ"}}
	}
	var out []Idea
	if bintBlockRe.MatchString(body) {
		body = bintBlockRe.ReplaceAllString(body, "")
		out = append(out, Idea{Source: source, Ref: source + ":" + name + "#bint", Title: name + " (khối bint)", Skip: SkipBintDerived,
			SkipDetail: "khối bint:begin…bint:end do Bint sinh ra từ tri thức đã có"})
	}
	if bintOpenRe.MatchString(body) { // khối hở: bỏ cả tệp, không đoán ranh giới
		return append(out, Idea{Source: source, Ref: source + ":" + name, Title: name, Skip: SkipBintDerived,
			SkipDetail: "khối bint:begin/bint:end không đóng"})
	}
	if source == knowledge.ImportClaudeMemory { // mỗi tệp bộ nhớ Claude là một ý (kể cả khi thiếu frontmatter)
		return append(out, claudeIdea(source, name, fm, body))
	}
	if source == knowledge.ImportCursorRules {
		paths := globList(fm["globs"])
		for i, b := range splitMarkdown(body) {
			out = append(out, mk(source, name, i, b, fm["description"], paths, knowledge.TypeRule))
		}
		return numbered(out, name)
	}
	for i, b := range splitMarkdown(body) {
		out = append(out, mk(source, name, i, b, "", nil, ""))
	}
	return numbered(out, name)
}

// numbered gắn "#n" vào Ref khi một tệp cho nhiều ý (ý duy nhất giữ Ref gọn).
func numbered(ideas []Idea, name string) []Idea {
	real := 0
	for _, id := range ideas {
		if id.Skip != SkipBintDerived || !strings.HasSuffix(id.Ref, "#bint") {
			real++
		}
	}
	if real <= 1 {
		for i := range ideas {
			if !strings.HasSuffix(ideas[i].Ref, "#bint") {
				ideas[i].Ref = ideas[i].Source + ":" + name
			}
		}
	}
	return ideas
}

func claudeIdea(source, name string, fm map[string]string, body string) Idea {
	title := strings.TrimSpace(fm["description"])
	if title == "" {
		title = strings.TrimSpace(fm["name"])
	}
	body = strings.TrimSpace(body)
	if title == "" {
		title = firstLine(body)
	}
	if body == "" {
		body = title
	}
	id := Idea{Source: source, Ref: source + ":" + name, Title: clip(title), Body: body}
	switch fm["type"] {
	case "feedback":
		id.Type = knowledge.TypeWarning
	case "project", "reference":
		id.Type = knowledge.TypeFact
	}
	if fm["type"] == "user" {
		id.Skip, id.SkipDetail = SkipPrivate, "bộ nhớ Claude loại user là thông tin riêng về người dùng, không nhập"
		id.Body = "" // không giữ nội dung riêng tư trong kết quả
		return id
	}
	return filter(id)
}

func mk(source, name string, n int, b block, desc string, paths []string, typ string) Idea {
	title := b.title
	if desc != "" && n == 0 {
		title = desc
	}
	return filter(Idea{Source: source, Ref: fmt.Sprintf("%s:%s#%d", source, name, n+1), Title: clip(title), Body: b.text, Paths: paths, Type: typ})
}

// filter đặt Skip khi ý do Bint sinh ra hoặc là ghi chú trạng thái công việc.
func filter(id Idea) Idea {
	if id.Skip != "" {
		return id
	}
	all := id.Title + "\n" + id.Body
	if kRefRe.MatchString(all) {
		id.Skip, id.SkipDetail = SkipBintDerived, "có tham chiếu [k:…] tới mục tri thức của Bint"
		return id
	}
	if why := WorkState(all); why != "" {
		id.Skip, id.SkipDetail = SkipWorkState, why
	}
	return id
}

var (
	commitRe      = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	dateRe        = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	ticketRe      = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d{1,6}\b`)
	progressRe    = regexp.MustCompile(`(?im)^[ \t#*+-]*((wip|todo|in progress|đang làm|tiến độ|đã hoàn thành|next steps?|bước tiếp theo)([ \t:]|$)|(status|trạng thái)[ \t]*:)`)
	workContextRe = regexp.MustCompile(`(?im)(\bhôm qua\b|\byesterday\b|^[ \t#*+-]*((hạn|deadline|due)[ \t]*:?[ \t]+[0-9]|theo ticket\b))`)
	digitRe       = regexp.MustCompile(`\d`)
	letterRe      = regexp.MustCompile(`[a-f]`)
)

// WorkState nhận diện tiến độ rõ ràng; ngày/ticket/hash chỉ là dấu hiệu khi có ngữ cảnh công việc.
// Ví dụ trong quy tắc bền vững có thể chứa mọi loại giá trị này.
func WorkState(s string) string {
	if m := progressRe.FindString(s); m != "" {
		return "ghi chú tiến độ (“" + strings.TrimSpace(m) + "”)"
	}
	if m := dateRe.FindString(s); m != "" && workContextRe.MatchString(s) {
		return "chứa ngày " + m
	}
	if m := ticketRe.FindString(s); m != "" && workContextRe.MatchString(s) {
		return "chứa mã ticket " + m
	}
	for _, m := range commitRe.FindAllString(s, -1) {
		if digitRe.MatchString(m) && letterRe.MatchString(m) && workContextRe.MatchString(s) { // chỉ chữ số hay chỉ chữ thì không phải băm commit
			return "chứa mã băm commit " + m
		}
	}
	return ""
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#-*> ")); l != "" {
			return l
		}
	}
	return ""
}

func clip(s string) string {
	s = strings.Join(strings.Fields(strings.NewReplacer("**", "", "__", "").Replace(s)), " ")
	if r := []rune(s); len(r) > maxTitle {
		return string(r[:maxTitle-1]) + "…"
	}
	return s
}

// splitFrontmatter tách phần đầu YAML đơn giản (khoá: giá trị chuỗi hoặc danh sách) khỏi nội dung.
// Dòng "---" đầu tệp chỉ mở frontmatter khi dòng khác rỗng kế tiếp có dạng "khoá:"; nếu không đó là đường kẻ Markdown.
// Frontmatter không đóng hoặc globs hỏng trả lỗi (không đoán ranh giới, tránh lộ siêu dữ liệu riêng tư vào nội dung);
// YAML không giải mã được thì đọc lỏng từng dòng "khoá: giá trị" và vẫn giữ nội dung.
func splitFrontmatter(text string) (map[string]string, string, error) {
	fm := map[string]string{}
	first, rest, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(first) != "---" {
		return fm, text, nil
	}
	parts := strings.Split(rest, "\n")
	opens := false
	for _, line := range parts {
		if strings.TrimSpace(line) != "" {
			opens = fmKeyRe.MatchString(line)
			break
		}
	}
	if !opens {
		return fm, text, nil
	}
	end := -1
	for i, line := range parts {
		if strings.TrimSpace(line) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, "", errors.New("frontmatter chưa đóng")
	}
	head, tail := strings.Join(parts[:end], "\n"), strings.Join(parts[end+1:], "\n")
	// Cursor dùng cả globs: **/*.go, {src,lib}/**/*.ts và [**/*.go, internal/**], không phải YAML hợp lệ.
	// Chỉ chuẩn hoá trường globs nếu YAML chuẩn không giải mã được giá trị đó.
	lines := strings.Split(head, "\n")
	for i, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "globs" {
			continue
		}
		value = strings.TrimSpace(value)
		var parsed any
		if yaml.Unmarshal([]byte(value), &parsed) == nil {
			continue
		}
		// Chỉ hỗ trợ cú pháp glob không trích dẫn, không chữa YAML hỏng.
		if strings.HasPrefix(value, "[") != strings.HasSuffix(value, "]") || strings.ContainsAny(value, `"'`) {
			return fm, "", errors.New("globs không hợp lệ")
		}
		globs, ok := splitGlobs(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		if !ok {
			return fm, "", errors.New("globs không hợp lệ")
		}
		var quoted []string
		for _, glob := range globs {
			if strings.ContainsAny(glob, "[]") {
				return fm, "", errors.New("globs không hợp lệ")
			}
			quoted = append(quoted, fmt.Sprintf("%q", glob))
		}
		lines[i] = key + ": [" + strings.Join(quoted, ", ") + "]"
	}
	head = strings.Join(lines, "\n")
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(head), &raw); err != nil {
		// Ví dụ "description: Run tests: use go test" không phải YAML hợp lệ nhưng ý nghĩa rõ ràng.
		for _, line := range lines {
			if !fmKeyRe.MatchString(line) {
				continue
			}
			k, v, _ := strings.Cut(line, ":")
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			if k = strings.TrimSpace(k); k == "globs" {
				v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
			}
			if v != "" {
				fm[k] = v
			}
		}
		return fm, tail, nil
	}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			fm[k] = x
		case []any:
			var parts []string
			for _, e := range x {
				parts = append(parts, fmt.Sprint(e))
			}
			fm[k] = strings.Join(parts, ",")
		case nil:
		default:
			fm[k] = fmt.Sprint(x)
		}
	}
	return fm, tail, nil
}

// globList đổi giá trị globs của rule Cursor thành danh sách mẫu đường dẫn hợp lệ trong repository.
func globList(v string) []string {
	parts, ok := splitGlobs(v)
	if !ok {
		return nil
	}
	var out []string
	for _, g := range parts {
		for _, p := range expandBraces(strings.Trim(g, `"'`)) {
			p = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"), "./")
			if p == "" || knowledge.ValidRelPath(p) != nil || slicesContains(out, p) {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

var fmKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*[ \t]*:`)

// splitGlobs tách danh sách glob theo dấu phẩy nằm ngoài cặp {}; ok=false khi ngoặc nhọn không cân.
func splitGlobs(v string) (out []string, ok bool) {
	depth, start := 0, 0
	push := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	for i, c := range v {
		switch c {
		case '{':
			depth++
		case '}':
			if depth--; depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				push(v[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	push(v[start:])
	return out, true
}

// expandBraces mở rộng {a,b} trong glob (lồng nhau được) thành từng mẫu riêng.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	depth := 0
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				alts, ok := splitGlobs(p[open+1 : i])
				if !ok || len(alts) == 0 {
					return []string{p}
				}
				var out []string
				for _, a := range alts {
					out = append(out, expandBraces(p[:open]+a+p[i+1:])...)
				}
				return out
			}
		}
	}
	return []string{p}
}

type block struct{ title, text string }

var bulletRe = regexp.MustCompile(`^([-*+]|\d{1,3}[.)])\s+(.*)$`)

// splitMarkdown tách văn bản thành khối: mỗi gạch đầu dòng cấp đầu (kèm dòng thụt vào) và mỗi đoạn văn
// ngoài danh sách. Tiêu đề (#) là ranh giới, không thành ý. Khối rào ``` không bị tách. Khối ngắn hơn minIdeaRunes bị bỏ.
func splitMarkdown(text string) []block {
	var out []block
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		t := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = nil
		if len([]rune(t)) >= minIdeaRunes {
			out = append(out, block{title: firstLine(t), text: t})
		}
	}
	inFence, inBullet := false, false
	for _, l := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			cur = append(cur, l)
			continue
		}
		if inFence {
			cur = append(cur, l)
			continue
		}
		switch {
		case trim == "":
			if !inBullet {
				flush()
			} else {
				cur = append(cur, "")
			}
		case strings.HasPrefix(l, "#"):
			flush()
			inBullet = false
		case bulletRe.MatchString(l):
			flush()
			inBullet = true
			cur = append(cur, bulletRe.FindStringSubmatch(l)[2])
		case inBullet && (l[0] == ' ' || l[0] == '\t'):
			cur = append(cur, trim)
		default:
			if inBullet {
				flush()
				inBullet = false
			}
			cur = append(cur, trim)
		}
	}
	flush()
	return out
}
