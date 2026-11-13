package skills

// Skill chuyên biệt do tiến hoá tạo ra (P7, L2; mục 10.5, 6.6). Nguồn chuẩn là tệp
// trong .bint/skills/evolved/<tên>/ (commit được, chia sẻ trong nhóm):
//
//	SKILL.md       nội dung đúng định dạng harness hiểu (frontmatter chỉ có name, description)
//	skill.yaml     siêu dữ liệu của Bint: trạng thái, điều kiện kích hoạt, nguồn gốc, lịch sử phiên bản
//	versions/N.md  ảnh chụp nội dung từng phiên bản (để xem bản thay đổi giữa hai phiên bản)
//
// Siêu dữ liệu của Bint không nằm trong SKILL.md: không mô phỏng khả năng mà harness
// không hiểu bằng khoá lạ trong frontmatter (mục 7).

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
	"bint/internal/pathglob"
)

// EvolvedRel là thư mục skill tiến hoá, tương đối với gốc repository.
const EvolvedRel = ".bint/skills/evolved"

// EvolvedSchema là phiên bản định dạng skill.yaml.
const EvolvedSchema = 1

// Trạng thái skill tiến hoá.
const (
	EvActive     = "active"
	EvDeprecated = "deprecated"
)

// Triggers là điều kiện kích hoạt skill: lớp đối chiếu xác định được mà Bint kiểm
// chứng bằng lịch sử (harness tự quyết định kích hoạt theo description).
type Triggers struct {
	Paths    []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Keywords []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
}

// EvolvedOrigin ghi nguồn gốc skill (mục 15.5).
type EvolvedOrigin struct {
	Proposal  string   `yaml:"proposal,omitempty" json:"proposal,omitempty"`
	Runs      []string `yaml:"runs,omitempty" json:"runs,omitempty"`
	Generator string   `yaml:"generator,omitempty" json:"generator,omitempty"`
}

// VersionEntry là một phiên bản trong lịch sử, gắn với harness/mô hình đã kiểm chứng
// nó (mục 12.6).
type VersionEntry struct {
	N          int    `yaml:"n" json:"n"`
	SHA256     string `yaml:"sha256" json:"sha256"`
	Proposal   string `yaml:"proposal,omitempty" json:"proposal,omitempty"`
	Harness    string `yaml:"harness,omitempty" json:"harness,omitempty"`
	Model      string `yaml:"model,omitempty" json:"model,omitempty"`
	Verified   bool   `yaml:"verified" json:"verified"`
	Evaluation string `yaml:"evaluation,omitempty" json:"evaluation,omitempty"`
	At         string `yaml:"at" json:"at"`
	By         string `yaml:"by,omitempty" json:"by,omitempty"`
	Note       string `yaml:"note,omitempty" json:"note,omitempty"`
}

// EvidenceRef là bằng chứng mã làm căn cứ cho skill (cùng cấu trúc với bằng chứng tri thức,
// kèm fingerprint để phát hiện mã đổi sau khi skill được viết).
type EvidenceRef struct {
	Path        string `yaml:"path" json:"path"`
	Lines       string `yaml:"lines,omitempty" json:"lines,omitempty"`
	Symbol      string `yaml:"symbol,omitempty" json:"symbol,omitempty"`
	Commit      string `yaml:"commit,omitempty" json:"commit,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty" json:"fingerprint,omitempty"`
}

// EvolvedMeta là nội dung skill.yaml.
type EvolvedMeta struct {
	Schema       int            `yaml:"schema" json:"schema"`
	ID           string         `yaml:"id" json:"id"`
	Name         string         `yaml:"name" json:"name"`
	Status       string         `yaml:"status" json:"status"`
	StatusReason string         `yaml:"status_reason,omitempty" json:"status_reason,omitempty"`
	Version      int            `yaml:"version" json:"version"`
	Triggers     Triggers       `yaml:"triggers" json:"triggers"`
	Evidence     []EvidenceRef  `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Origin       EvolvedOrigin  `yaml:"origin" json:"origin"`
	MergedFrom   []string       `yaml:"merged_from,omitempty" json:"merged_from,omitempty"`
	Versions     []VersionEntry `yaml:"versions" json:"versions"`
	CreatedAt    string         `yaml:"created_at" json:"created_at"`
	UpdatedAt    string         `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
}

// Evolved là một skill tiến hoá đã nạp.
type Evolved struct {
	Meta    EvolvedMeta
	Content []byte // SKILL.md
}

// Active cho biết skill đang được dùng.
func (e *Evolved) Active() bool { return e.Meta.Status == EvActive }

// Doc phân tích SKILL.md.
func (e *Evolved) Doc() Doc { d, _ := ParseDoc(e.Content); return d }

// AsSkill trả về skill dưới dạng Skill (để xuất bản).
func (e *Evolved) AsSkill() Skill { return Skill{Name: e.Meta.Name, Content: e.Content} }

// Last là mục phiên bản hiện tại trong lịch sử (nil nếu chưa có).
func (e *Evolved) Last() *VersionEntry {
	for i := range e.Meta.Versions {
		if e.Meta.Versions[i].N == e.Meta.Version {
			return &e.Meta.Versions[i]
		}
	}
	return nil
}

// Doc là SKILL.md đã phân tích.
type Doc struct {
	Name        string
	Description string
	// Extra: khoá frontmatter ngoài name, description (harness có thể diễn giải thành quyền hay hook).
	Extra []string
	Body  string
}

// ParseDoc tách frontmatter và thân của SKILL.md.
func ParseDoc(content []byte) (Doc, error) {
	var d Doc
	c := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(c, "---\n") {
		return d, errors.New("thiếu frontmatter (---)")
	}
	end := strings.Index(c[4:], "\n---\n")
	if end < 0 {
		return d, errors.New("frontmatter không đóng")
	}
	var fm yaml.Node
	if err := yaml.Unmarshal([]byte(c[4:4+end]), &fm); err != nil || len(fm.Content) == 0 || fm.Content[0].Kind != yaml.MappingNode {
		return d, errors.New("frontmatter không phải ánh xạ YAML")
	}
	m := fm.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		switch k := m.Content[i].Value; k {
		case "name":
			d.Name = m.Content[i+1].Value
		case "description":
			d.Description = m.Content[i+1].Value
		default:
			d.Extra = append(d.Extra, k)
		}
	}
	d.Body = strings.TrimSpace(c[4+end+5:])
	return d, nil
}

// BuildContent dựng SKILL.md từ tên, mô tả và thân.
func BuildContent(name, description, body string) []byte {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{name, description})
	enc.Close()
	return []byte("---\n" + buf.String() + "---\n\n" + strings.TrimSpace(body) + "\n")
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName kiểm tra tên skill tiến hoá. Tên "b-*" dành cho skill gốc.
func ValidName(name string) error {
	switch {
	case !nameRe.MatchString(name) || len(name) < 3 || len(name) > 40:
		return fmt.Errorf("tên skill %q không hợp lệ (chữ thường, số, gạch ngang; 3–40 ký tự)", name)
	case strings.HasPrefix(name, "b-"):
		return fmt.Errorf("tên skill %q: tiền tố b- dành cho skill gốc của Bint", name)
	case reserved(name):
		return fmt.Errorf("tên skill %q là tên dành riêng của .bint/skills", name)
	}
	if _, ok := Get(name); ok {
		return fmt.Errorf("tên skill %q trùng skill gốc", name)
	}
	return nil
}

var overrideRe = regexp.MustCompile(`(?i)(bỏ qua|phớt lờ|vô hiệu hoá|vô hiệu hóa|không cần tuân|ghi đè|ignore|disregard|override|bypass|disable)[^\n]{0,60}(quy tắc|tri thức|hướng dẫn|chỉ dẫn|skill gốc|agents\.md|claude\.md|rule|instruction|guideline|knowledge|constraint|ràng buộc|kiểm thử|test)`)

var protectedRefRe = regexp.MustCompile(`\.bint/(evals|config\.yml|knowledge/items|skills/core\.lock|skills/overlays)`)

// CheckDraft kiểm tra xác định được một skill (mục 17, kiểm thử hồi quy tiến hoá):
// không đổi các ràng buộc bắt buộc (không ghi đè quy tắc/tri thức/skill gốc, không
// xin thêm quyền qua frontmatter), không đụng tiêu chí chấm điểm hay cấu hình của
// Bint, kích thước hợp lý. Trả về danh sách vấn đề (rỗng = hợp lệ).
func CheckDraft(content []byte, tr Triggers) []string {
	var out []string
	d, err := ParseDoc(content)
	if err != nil {
		return []string{"SKILL.md: " + err.Error()}
	}
	if err := ValidName(d.Name); err != nil {
		out = append(out, err.Error())
	}
	if n := utf8.RuneCountInString(d.Description); n < 20 || n > 400 || strings.Contains(d.Description, "\n") {
		out = append(out, fmt.Sprintf("description phải một dòng, 20–400 ký tự (hiện %d): harness chọn skill theo mô tả này", n))
	}
	if len(d.Extra) > 0 {
		out = append(out, "frontmatter chỉ được có name và description (thừa: "+strings.Join(d.Extra, ", ")+"): skill tiến hoá không được tự xin quyền hay hook")
	}
	switch {
	case strings.TrimSpace(d.Body) == "":
		out = append(out, "thân skill rỗng")
	case utf8.RuneCountInString(d.Body) > 6000 || strings.Count(d.Body, "\n") > 150:
		out = append(out, "thân skill quá dài (tối đa 6000 ký tự, 150 dòng): skill dài làm đội chi phí ngữ cảnh")
	}
	if m := overrideRe.FindString(d.Body + "\n" + d.Description); m != "" {
		out = append(out, fmt.Sprintf("nội dung có vẻ ghi đè ràng buộc bắt buộc (%q): skill tiến hoá không được thay đổi quy tắc, tri thức hay skill gốc", m))
	}
	if m := protectedRefRe.FindString(d.Body); m != "" {
		out = append(out, "nội dung nhắc tới "+m+": skill tiến hoá không được đụng tiêu chí chấm điểm, cấu hình hay dữ liệu quản lý bởi Bint")
	}
	out = append(out, CheckTriggers(tr)...)
	return out
}

// CheckTriggers kiểm tra dạng của điều kiện kích hoạt.
func CheckTriggers(tr Triggers) []string {
	var out []string
	if len(tr.Paths) == 0 && len(tr.Keywords) == 0 {
		out = append(out, "thiếu điều kiện kích hoạt (triggers.paths hoặc triggers.keywords)")
	}
	if len(tr.Paths) > 10 || len(tr.Keywords) > 10 {
		out = append(out, "tối đa 10 đường dẫn và 10 từ khoá kích hoạt")
	}
	for _, p := range tr.Paths {
		switch {
		case p == "" || path.IsAbs(p) || strings.Contains(p, ".."):
			out = append(out, fmt.Sprintf("đường dẫn kích hoạt %q không hợp lệ", p))
		case strings.Trim(p, "*/") == "":
			out = append(out, fmt.Sprintf("đường dẫn kích hoạt %q khớp mọi tệp: skill sẽ kích hoạt ở mọi nơi", p))
		}
	}
	for _, k := range tr.Keywords {
		if utf8.RuneCountInString(strings.TrimSpace(k)) < 3 {
			out = append(out, fmt.Sprintf("từ khoá kích hoạt %q quá ngắn", k))
		}
	}
	return out
}

var shellRe = regexp.MustCompile("(?m)^\\s*(```\\s*(sh|bash|shell|zsh|powershell)\\b|\\$\\s|sudo\\s|curl\\s|wget\\s|rm\\s+-|chmod\\s|git\\s+push|.*\\|\\s*(sh|bash)\\b)")

// CommandRisks trả về lý do thân skill chứa lệnh thực thi (rủi ro cao: agent sẽ chạy
// theo chỉ dẫn do agent khác soạn từ dữ liệu không đáng tin, mục 15.1).
func CommandRisks(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range shellRe.FindAllString(body, -1) {
		m = strings.TrimSpace(m)
		if !seen[m] {
			seen[m] = true
			out = append(out, "chứa lệnh thực thi: "+truncate(m, 60))
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---- Lưu trữ ----

// EvolvedDir là thư mục của một skill tiến hoá.
func EvolvedDir(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(EvolvedRel), name)
}

// EvolvedRelPath là đường dẫn tương đối (dấu '/') của một tệp trong thư mục skill.
func EvolvedRelPath(name string, parts ...string) string {
	return path.Join(append([]string{EvolvedRel, name}, parts...)...)
}

// LoadEvolved nạp mọi skill tiến hoá (sắp theo tên). Thư mục hỏng được báo trong problems
// thay vì làm hỏng cả danh sách.
func LoadEvolved(root string) (list []*Evolved, problems []string, err error) {
	base := filepath.Join(root, filepath.FromSlash(EvolvedRel))
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ev, err := LoadEvolvedOne(root, e.Name())
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		list = append(list, ev)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Meta.Name < list[j].Meta.Name })
	return list, problems, nil
}

// ErrNoEvolved: không có skill tiến hoá với tên đó.
var ErrNoEvolved = errors.New("không có skill tiến hoá")

// LoadEvolvedOne nạp một skill tiến hoá.
func LoadEvolvedOne(root, name string) (*Evolved, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("tên skill %q không hợp lệ", name)
	}
	dir := EvolvedDir(root, name)
	mb, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w %q", ErrNoEvolved, name)
	}
	if err != nil {
		return nil, err
	}
	var m EvolvedMeta
	if err := yaml.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("skill.yaml: %w", err)
	}
	if m.Schema != EvolvedSchema {
		return nil, fmt.Errorf("skill.yaml: schema %d không hỗ trợ", m.Schema)
	}
	if m.Name != name {
		return nil, fmt.Errorf("skill.yaml: name %q không khớp thư mục", m.Name)
	}
	cb, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	return &Evolved{Meta: m, Content: cb}, nil
}

func marshalMeta(m EvolvedMeta) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// Files trả về nội dung các tệp của skill để ghi: SKILL.md, skill.yaml, versions/<N>.md.
func (e *Evolved) Files() (map[string][]byte, error) {
	mb, err := marshalMeta(e.Meta)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		EvolvedRelPath(e.Meta.Name, "SKILL.md"):                                       e.Content,
		EvolvedRelPath(e.Meta.Name, "skill.yaml"):                                     mb,
		EvolvedRelPath(e.Meta.Name, "versions", fmt.Sprintf("%d.md", e.Meta.Version)): e.Content,
	}, nil
}

// Save ghi skill (nguyên tử từng tệp).
func (e *Evolved) Save(root string) error {
	files, err := e.Files()
	if err != nil {
		return err
	}
	for rel, b := range files {
		if err := fsafe.WriteAtomic(filepath.Join(root, filepath.FromSlash(rel)), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// RemoveEvolved xoá thư mục skill.
func RemoveEvolved(root, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("tên skill %q không hợp lệ", name)
	}
	return os.RemoveAll(EvolvedDir(root, name))
}

// EvolvedSHA băm nội dung hiện tại của skill (SKILL.md + skill.yaml): phiên bản gốc
// mà đề xuất ghi lại (mục 8.7). Rỗng nếu skill không tồn tại.
func EvolvedSHA(root, name string) (string, error) {
	var buf bytes.Buffer
	for _, f := range []string{"SKILL.md", "skill.yaml"} {
		b, err := os.ReadFile(filepath.Join(EvolvedDir(root, name), f))
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		buf.Write(b)
		buf.WriteByte(0)
	}
	return fsafe.Hash(buf.String()), nil
}

// ReadVersion đọc ảnh chụp một phiên bản.
func ReadVersion(root, name string, n int) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(EvolvedDir(root, name), "versions", fmt.Sprintf("%d.md", n)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("skill %s không có phiên bản %d", name, n)
	}
	return b, err
}

// ---- Điều kiện kích hoạt: đối chiếu với tệp ----

// MatchPaths trả về các tệp khớp một trong các mẫu đường dẫn kích hoạt.
func (t Triggers) MatchPaths(files []string) []string {
	var out []string
	for _, f := range files {
		if pathglob.MatchAny(t.Paths, f) {
			out = append(out, f)
		}
	}
	return out
}

// Hits cho biết một tác vụ (danh sách tệp và/hoặc văn bản mô tả) có thoả điều kiện
// kích hoạt không: khớp đường dẫn, hoặc mô tả chứa từ khoá.
func (t Triggers) Hits(files []string, text string) bool {
	for _, f := range files {
		if pathglob.MatchAny(t.Paths, f) {
			return true
		}
	}
	lt := strings.ToLower(text)
	for _, k := range t.Keywords {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" && strings.Contains(lt, k) {
			return true
		}
	}
	return false
}

// Stale cho biết skill đã lỗi thời so với cây mã hiện tại: mọi mẫu đường dẫn kích hoạt
// không còn khớp tệp nào (skill chỉ có từ khoá không bị coi là lỗi thời ở đây). Skill
// lỗi thời không được xuất bản — "skill lỗi thời không được ưu tiên" (mục 17).
func (e *Evolved) Stale(files []string) (bool, string) {
	if len(e.Meta.Triggers.Paths) == 0 || files == nil {
		return false, ""
	}
	if len(e.Meta.Triggers.MatchPaths(files)) == 0 {
		return true, "không tệp nào còn khớp triggers.paths (" + strings.Join(e.Meta.Triggers.Paths, ", ") + ")"
	}
	return false, ""
}

// ---- Bản thay đổi ----

// Diff trả về bản thay đổi dạng unified (rút gọn, 2 dòng ngữ cảnh) giữa hai văn bản.
// Rỗng nếu giống nhau.
func Diff(aName, bName, a, b string) string {
	al, bl := diffLines(a), diffLines(b)
	// LCS theo quy hoạch động; skill ≤ vài trăm dòng nên O(n·m) là đủ.
	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		tag  byte
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			ops = append(ops, op{' ', al[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', al[i]})
			i++
		default:
			ops = append(ops, op{'+', bl[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', al[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', bl[j]})
	}
	changed := false
	keep := make([]bool, len(ops))
	for k, o := range ops {
		if o.tag == ' ' {
			continue
		}
		changed = true
		for d := -2; d <= 2; d++ {
			if k+d >= 0 && k+d < len(ops) {
				keep[k+d] = true
			}
		}
	}
	if !changed {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aName, bName)
	skipped := false
	for k, o := range ops {
		if !keep[k] {
			skipped = true
			continue
		}
		if skipped {
			sb.WriteString("@@\n")
			skipped = false
		}
		sb.WriteByte(o.tag)
		sb.WriteString(o.text)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func diffLines(s string) []string {
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// EvolvedProblem là một vấn đề toàn vẹn của skill tiến hoá.
type EvolvedProblem struct {
	Skill  string
	Detail string
	// Conflict: dấu xung đột Git hoặc hai nhánh cùng sửa một skill — người dùng phải xử lý
	// thủ công, Bint không tự hợp nhất (mục 8.7).
	Conflict bool
}

// CheckEvolved kiểm tra toàn vẹn các skill tiến hoá trong repository (cho `bint doctor`):
// dấu xung đột merge, phiên bản không khớp nội dung (sửa tay hoặc hai thay đổi merge vào
// nhau), thiếu ảnh chụp phiên bản, và skill đang hoạt động không qua kiểm tra ràng buộc.
func CheckEvolved(root string) []EvolvedProblem {
	var out []EvolvedProblem
	base := filepath.Join(root, filepath.FromSlash(EvolvedRel))
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		add := func(conflict bool, f string, a ...any) {
			out = append(out, EvolvedProblem{Skill: name, Detail: fmt.Sprintf(f, a...), Conflict: conflict})
		}
		for _, f := range []string{"SKILL.md", "skill.yaml"} {
			b, err := os.ReadFile(filepath.Join(base, name, f))
			if err == nil && (bytes.Contains(b, []byte("\n<<<<<<< ")) || bytes.HasPrefix(b, []byte("<<<<<<< "))) {
				add(true, "%s có dấu xung đột merge: xử lý thủ công (Bint không tự hợp nhất)", f)
			}
		}
		ev, err := LoadEvolvedOne(root, name)
		if err != nil {
			add(false, "%v", err)
			continue
		}
		last := ev.Last()
		switch {
		case last == nil:
			add(true, "skill.yaml không có mục cho phiên bản %d: hai thay đổi có thể đã merge vào nhau, xử lý thủ công", ev.Meta.Version)
		case last.SHA256 != ev.AsSkill().Hash():
			add(true, "SKILL.md không khớp phiên bản %d trong skill.yaml (sửa tay, hoặc hai thay đổi cùng sửa skill đã merge): xử lý thủ công", ev.Meta.Version)
		}
		if snap, err := ReadVersion(root, name, ev.Meta.Version); err != nil {
			add(false, "%v", err)
		} else if !bytes.Equal(snap, ev.Content) {
			add(true, "versions/%d.md khác SKILL.md hiện tại", ev.Meta.Version)
		}
		if ev.Active() {
			for _, p := range CheckDraft(ev.Content, ev.Meta.Triggers) {
				add(false, "%s", p)
			}
		}
	}
	return out
}
