package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill gốc khai báo điểm neo có tên (mục 6.6):
//
//	<!-- bint:anchor project-rules -->
//	nội dung mặc định (có thể rỗng)
//	<!-- bint:anchor-end project-rules -->
//
// Lớp phủ .bint/skills/overlays/<skill>.md chỉ được thêm hoặc thay nội dung tại các
// điểm neo đó:
//
//	<!-- bint:overlay project-rules append -->
//	nội dung của dự án
//	<!-- bint:overlay-end -->
//
// Chế độ: append (thêm sau nội dung mặc định), prepend (thêm trước), replace (thay).
// Văn bản nằm ngoài các khối overlay là ghi chú cho người đọc và bị bỏ qua. Dấu
// mốc phải bắt đầu ở cột đầu tiên; dòng thụt lề (ví dụ trong tài liệu) không có hiệu lực.

// OverlaysRel là thư mục lớp phủ, tương đối với gốc repository.
const OverlaysRel = ".bint/skills/overlays"

var (
	anchorRe     = regexp.MustCompile(`^<!--\s*bint:anchor\s+([a-z0-9][a-z0-9-]*)\s*-->\s*$`)
	anchorEndRe  = regexp.MustCompile(`^<!--\s*bint:anchor-end\s+([a-z0-9][a-z0-9-]*)\s*-->\s*$`)
	overlayRe    = regexp.MustCompile(`^<!--\s*bint:overlay\s+(\S+)(?:\s+(\S+))?\s*-->\s*$`)
	overlayEndRe = regexp.MustCompile(`^<!--\s*bint:overlay-end\s*-->\s*$`)
)

// Chế độ áp lớp phủ.
const (
	ModeAppend  = "append"
	ModePrepend = "prepend"
	ModeReplace = "replace"
)

// Anchor là một điểm neo trong skill gốc.
type Anchor struct {
	Name    string
	Default string
	start   int // dòng mở (chỉ số trong mảng dòng)
	end     int // dòng đóng
}

// Anchors liệt kê điểm neo của skill theo thứ tự xuất hiện.
func (s Skill) Anchors() ([]Anchor, error) {
	_, as, err := parseAnchors(string(s.Content))
	return as, err
}

func splitLines(c string) []string {
	return strings.Split(strings.ReplaceAll(c, "\r\n", "\n"), "\n")
}

func parseAnchors(content string) ([]string, []Anchor, error) {
	lines := splitLines(content)
	var out []Anchor
	open := -1
	name := ""
	seen := map[string]bool{}
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t\r")
		if m := anchorRe.FindStringSubmatch(t); m != nil {
			if open >= 0 {
				return nil, nil, fmt.Errorf("dòng %d: điểm neo %q mở khi %q chưa đóng", i+1, m[1], name)
			}
			if seen[m[1]] {
				return nil, nil, fmt.Errorf("dòng %d: điểm neo %q bị khai báo hai lần", i+1, m[1])
			}
			open, name = i, m[1]
			seen[name] = true
			continue
		}
		if m := anchorEndRe.FindStringSubmatch(t); m != nil {
			if open < 0 || m[1] != name {
				return nil, nil, fmt.Errorf("dòng %d: đóng điểm neo %q không khớp", i+1, m[1])
			}
			out = append(out, Anchor{Name: name, Default: strings.Join(lines[open+1:i], "\n"), start: open, end: i})
			open = -1
		}
	}
	if open >= 0 {
		return nil, nil, fmt.Errorf("điểm neo %q không được đóng", name)
	}
	return lines, out, nil
}

// Patch là nội dung lớp phủ cho một điểm neo.
type Patch struct {
	Anchor  string
	Mode    string
	Content string
	Line    int
}

// Overlay là lớp phủ của một skill.
type Overlay struct {
	Skill   string
	Path    string // tương đối với gốc repository
	Patches []Patch
	Raw     []byte
}

// Hash là băm nội dung lớp phủ (ghi vào core.lock).
func (o *Overlay) Hash() string {
	if o == nil {
		return ""
	}
	return Skill{Content: o.Raw}.Hash()
}

// ParseOverlay đọc nội dung một tệp lớp phủ.
func ParseOverlay(skill, rel string, data []byte) (*Overlay, error) {
	o := &Overlay{Skill: skill, Path: rel, Raw: data}
	lines := splitLines(string(data))
	cur := -1
	seen := map[string]bool{}
	var buf []string
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t\r")
		if m := overlayRe.FindStringSubmatch(t); m != nil {
			if cur >= 0 {
				return nil, fmt.Errorf("%s:%d: khối overlay mở khi khối trước chưa đóng", rel, i+1)
			}
			mode := m[2]
			if mode == "" {
				mode = ModeAppend
			}
			if mode != ModeAppend && mode != ModePrepend && mode != ModeReplace {
				return nil, fmt.Errorf("%s:%d: chế độ %q không hợp lệ (append | prepend | replace)", rel, i+1, mode)
			}
			if seen[m[1]] {
				return nil, fmt.Errorf("%s:%d: điểm neo %q có hơn một khối overlay", rel, i+1, m[1])
			}
			seen[m[1]] = true
			o.Patches = append(o.Patches, Patch{Anchor: m[1], Mode: mode, Line: i + 1})
			cur, buf = len(o.Patches)-1, nil
			continue
		}
		if overlayEndRe.MatchString(t) {
			if cur < 0 {
				return nil, fmt.Errorf("%s:%d: overlay-end không có khối mở", rel, i+1)
			}
			o.Patches[cur].Content = strings.Trim(strings.Join(buf, "\n"), "\n")
			cur = -1
			continue
		}
		if anchorRe.MatchString(t) || anchorEndRe.MatchString(t) {
			return nil, fmt.Errorf("%s:%d: lớp phủ không được khai báo điểm neo", rel, i+1)
		}
		if cur >= 0 {
			buf = append(buf, ln)
		}
	}
	if cur >= 0 {
		return nil, fmt.Errorf("%s: khối overlay %q không được đóng", rel, o.Patches[cur].Anchor)
	}
	return o, nil
}

// OverlayPath trả về đường dẫn tương đối của lớp phủ cho skill.
func OverlayPath(skill string) string { return OverlaysRel + "/" + skill + ".md" }

// LoadOverlay đọc lớp phủ của skill trong repository (nil nếu không có).
func LoadOverlay(root, skill string) (*Overlay, error) {
	rel := OverlayPath(skill)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseOverlay(skill, rel, data)
}

// OrphanOverlays trả về các tệp lớp phủ không ứng với skill gốc nào.
func OrphanOverlays(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(OverlaysRel)))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if _, ok := Get(strings.TrimSuffix(name, ".md")); !ok {
			out = append(out, OverlaysRel+"/"+name)
		}
	}
	sort.Strings(out)
	return out
}

// ConflictError: lớp phủ không áp được lên skill gốc (ví dụ điểm neo không còn sau
// khi nâng cấp). Bint giữ bản đã xuất bản cũ cho tới khi người dùng xử lý.
type ConflictError struct {
	Skill   string
	Overlay string
	Missing []string
	Err     error
}

func (e *ConflictError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("lớp phủ %s: %v", e.Overlay, e.Err)
	}
	return fmt.Sprintf("lớp phủ %s dùng điểm neo không còn trong skill gốc %s: %s (không tự đoán vị trí mới; sửa lớp phủ)",
		e.Overlay, e.Skill, strings.Join(e.Missing, ", "))
}

// Compose ghép skill gốc với lớp phủ (nil = không có). Mọi điểm neo mà lớp phủ dùng
// phải tồn tại; nếu không trả *ConflictError.
func Compose(s Skill, o *Overlay) ([]byte, error) {
	if o == nil || len(o.Patches) == 0 {
		return s.Content, nil
	}
	lines, anchors, err := parseAnchors(string(s.Content))
	if err != nil {
		return nil, fmt.Errorf("skill gốc %s: %w", s.Name, err)
	}
	byName := map[string]Anchor{}
	for _, a := range anchors {
		byName[a.Name] = a
	}
	patches := map[string]Patch{}
	var missing []string
	for _, p := range o.Patches {
		if _, ok := byName[p.Anchor]; !ok {
			missing = append(missing, p.Anchor)
		}
		patches[p.Anchor] = p
	}
	if len(missing) > 0 {
		return nil, &ConflictError{Skill: s.Name, Overlay: o.Path, Missing: missing}
	}
	var out []string
	next := 0
	for _, a := range anchors {
		out = append(out, lines[next:a.start+1]...)
		p, ok := patches[a.Name]
		inner := a.Default
		if ok {
			def := strings.Trim(a.Default, "\n")
			switch p.Mode {
			case ModeReplace:
				inner = p.Content
			case ModePrepend:
				inner = joinNonEmpty(p.Content, def)
			default:
				inner = joinNonEmpty(def, p.Content)
			}
		}
		if inner != "" {
			out = append(out, splitLines(inner)...)
		}
		out = append(out, lines[a.end])
		next = a.end + 1
	}
	out = append(out, lines[next:]...)
	return []byte(strings.Join(out, "\n")), nil
}

func joinNonEmpty(a, b string) string {
	switch {
	case strings.TrimSpace(a) == "":
		return b
	case strings.TrimSpace(b) == "":
		return a
	}
	return a + "\n\n" + b
}

// Composed trả về skill đã ghép lớp phủ của repository.
func Composed(root string, s Skill) (Skill, *Overlay, error) {
	o, err := LoadOverlay(root, s.Name)
	if err != nil {
		return s, nil, &ConflictError{Skill: s.Name, Overlay: OverlayPath(s.Name), Err: err}
	}
	c, err := Compose(s, o)
	if err != nil {
		return s, o, err
	}
	return Skill{Name: s.Name, Content: c}, o, nil
}

// OverlayTemplate dựng tệp lớp phủ mẫu liệt kê các điểm neo của skill. Ví dụ được
// thụt lề nên chưa có hiệu lực cho tới khi người dùng bỏ thụt lề.
func OverlayTemplate(s Skill) (string, error) {
	as, err := s.Anchors()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Lớp phủ cho skill gốc %s\n\n", s.Name)
	b.WriteString("Tuỳ biến theo dự án chỉ đặt trong các khối overlay (mục 6.6); skill gốc không được\n")
	b.WriteString("sửa trực tiếp. Chế độ: append | prepend | replace. Văn bản ngoài khối bị bỏ qua.\n")
	b.WriteString("Khối mẫu dưới đây đang thụt lề nên CHƯA có hiệu lực: bỏ thụt lề để bật, rồi `bint sync`.\n")
	if len(as) == 0 {
		b.WriteString("\nSkill này không có điểm neo nào.\n")
	}
	for _, a := range as {
		fmt.Fprintf(&b, "\n## %s\n\n", a.Name)
		if d := strings.TrimSpace(a.Default); d != "" {
			b.WriteString("Nội dung mặc định của skill gốc:\n\n")
			for _, l := range splitLines(d) {
				b.WriteString("> " + l + "\n")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "    <!-- bint:overlay %s append -->\n    (nội dung của dự án)\n    <!-- bint:overlay-end -->\n", a.Name)
	}
	return b.String(), nil
}
