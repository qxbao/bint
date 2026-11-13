package skills

// Skill tuỳ biến (GĐ1 của docs/custom-flow-plan.md): người dùng tự viết trong
// .bint/skills/<tên>/ (commit được, chia sẻ trong nhóm):
//
//	SKILL.md       frontmatter name, description (+ disable-model-invocation, argument-hint) và thân
//	<tệp phụ>      tài liệu tham chiếu, script... (kể cả thư mục con), xuất bản nguyên văn
//	tests/         dành cho kiểm thử skill (GĐ6), không xuất bản
//
// Khác skill gốc (bất biến) và skill tiến hoá (do Bint sinh), đây là tệp người viết:
// ranh giới tin cậy là review Git như lớp phủ, nên Bint chỉ kiểm hình thức (CheckCustom)
// và không kiểm nội dung kiểu CheckDraft.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// CustomRel là thư mục chứa skill tuỳ biến, tương đối với gốc repository.
const CustomRel = ".bint/skills"

// Trần tệp phụ của một skill tuỳ biến (chống chép nhầm cả cây mã vào skill).
const (
	MaxCustomFiles     = 50
	MaxCustomFileBytes = 1 << 20
	MaxCustomTotal     = 5 << 20
	// MaxDescriptionRunes là trần mô tả (theo đặc tả skill: harness nạp mô tả vào ngữ cảnh).
	MaxDescriptionRunes = 1024
	// MaxBodyLines là trần số dòng thân SKILL.md; dài hơn nên tách sang tệp phụ.
	MaxBodyLines = 500
)

// ReservedNames là tên thư mục con của .bint/skills không phải skill tuỳ biến.
var ReservedNames = []string{"overlays", "evolved"}

// customKeys là các khoá frontmatter được phép (harness hiểu; khoá khác có thể bị diễn giải thành quyền/hook).
var customKeys = map[string]bool{"disable-model-invocation": true, "argument-hint": true}

// CustomFile là một tệp phụ của skill tuỳ biến.
type CustomFile struct {
	Rel     string // đường dẫn tương đối với thư mục skill, dạng '/'
	Content []byte
	Mode    os.FileMode // 0o755 nếu nguồn có bit thực thi, ngược lại 0o644
}

// CustomSkill là một skill tuỳ biến đã nạp.
type CustomSkill struct {
	Name    string // tên thư mục
	Dir     string // đường dẫn tuyệt đối
	Content []byte // SKILL.md (nil nếu thiếu/không đọc được)
	Files   []CustomFile
	// Problems: vấn đề lúc nạp (thiếu SKILL.md, symlink, vượt trần...). Skill có vấn đề không được xuất bản.
	Problems []string
}

// AsSkill trả về SKILL.md dưới dạng Skill (để xuất bản).
func (c *CustomSkill) AsSkill() Skill { return Skill{Name: c.Name, Content: c.Content} }

// Doc phân tích SKILL.md.
func (c *CustomSkill) Doc() (Doc, error) { return ParseDoc(c.Content) }

// CustomPath là đường dẫn tương đối (dấu '/') của skill hoặc một tệp trong nó.
func CustomPath(name string, parts ...string) string {
	return path.Join(append([]string{CustomRel, name}, parts...)...)
}

func reserved(name string) bool {
	return slices.Contains(ReservedNames, name)
}

// LoadCustom nạp mọi skill tuỳ biến (sắp theo tên): mọi thư mục con trực tiếp của .bint/skills trừ tên dành
// riêng và thư mục ẩn. Vấn đề của từng skill nằm trong Problems, không làm hỏng việc nạp skill khác.
func LoadCustom(root string) ([]*CustomSkill, error) {
	base := filepath.Join(root, filepath.FromSlash(CustomRel))
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*CustomSkill
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || reserved(name) {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 {
			out = append(out, &CustomSkill{Name: name, Dir: filepath.Join(base, name),
				Problems: []string{"thư mục skill là symlink: không được xuất bản (chép thư mục thật vào đây)"}})
			continue
		}
		if !e.IsDir() {
			continue
		}
		out = append(out, loadCustomDir(base, name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ErrNoCustom: không có thư mục skill tuỳ biến với tên đó.
var ErrNoCustom = errors.New("không có skill tuỳ biến")

// LoadCustomOne nạp một skill tuỳ biến theo tên thư mục.
func LoadCustomOne(root, name string) (*CustomSkill, error) {
	if name == "" || strings.HasPrefix(name, ".") || reserved(name) || strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("%w %q", ErrNoCustom, name)
	}
	base := filepath.Join(root, filepath.FromSlash(CustomRel))
	fi, err := os.Lstat(filepath.Join(base, name))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w %q", ErrNoCustom, name)
	case fi.Mode()&fs.ModeSymlink != 0:
		return &CustomSkill{Name: name, Dir: filepath.Join(base, name),
			Problems: []string{"thư mục skill là symlink: không được xuất bản (chép thư mục thật vào đây)"}}, nil
	case !fi.IsDir():
		return nil, fmt.Errorf("%w %q", ErrNoCustom, name)
	}
	return loadCustomDir(base, name), nil
}

func loadCustomDir(base, name string) *CustomSkill {
	cs := &CustomSkill{Name: name, Dir: filepath.Join(base, name)}
	add := func(f string, a ...any) { cs.Problems = append(cs.Problems, fmt.Sprintf(f, a...)) }
	skillMd := filepath.Join(cs.Dir, "SKILL.md")
	switch fi, err := os.Lstat(skillMd); {
	case errors.Is(err, fs.ErrNotExist):
		add("thiếu SKILL.md")
	case err != nil:
		add("SKILL.md: %v", err)
	case !fi.Mode().IsRegular():
		add("SKILL.md không phải tệp thường (symlink hoặc loại khác): không được xuất bản")
	case fi.Size() > MaxCustomFileBytes:
		add("SKILL.md quá lớn (%d byte, tối đa %d)", fi.Size(), MaxCustomFileBytes)
	default:
		b, err := os.ReadFile(skillMd)
		if err != nil {
			add("SKILL.md: %v", err)
		} else {
			cs.Content = b
		}
	}
	var total int64
	overflow := false
	werr := filepath.WalkDir(cs.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == cs.Dir {
			return nil
		}
		rel, _ := filepath.Rel(cs.Dir, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel == "tests" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "SKILL.md" {
			return nil
		}
		if !d.Type().IsRegular() {
			add("%s không phải tệp thường (symlink hoặc loại khác): không được xuất bản", rel)
			return nil
		}
		if len(cs.Files) >= MaxCustomFiles {
			if !overflow {
				overflow = true
				add("quá %d tệp phụ", MaxCustomFiles)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			add("%s: %v", rel, err)
			return nil
		}
		if info.Size() > MaxCustomFileBytes {
			add("%s quá lớn (%d byte, tối đa %d)", rel, info.Size(), MaxCustomFileBytes)
			return nil
		}
		total += info.Size()
		if total > MaxCustomTotal {
			if total-info.Size() <= MaxCustomTotal {
				add("tổng kích thước tệp phụ vượt %d byte", MaxCustomTotal)
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			add("%s: %v", rel, err)
			return nil
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		cs.Files = append(cs.Files, CustomFile{Rel: rel, Content: b, Mode: mode})
		return nil
	})
	if werr != nil {
		add("đọc thư mục skill: %v", werr)
	}
	sort.Slice(cs.Files, func(i, j int) bool { return cs.Files[i].Rel < cs.Files[j].Rel })
	return cs
}

var linkRe = regexp.MustCompile(`\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+(?:"[^"]*"|'[^']*'))?\s*\)`)
var inlineCodeRe = regexp.MustCompile("`[^`\n]*`")
var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// CheckCustom kiểm hình thức một skill tuỳ biến: errs làm skill không được xuất bản (và `bint skill lint`
// thoát 1), warns chỉ nhắc. root dùng để phát hiện trùng skill tiến hoá.
func CheckCustom(root string, cs *CustomSkill) (errs, warns []string) {
	errs = append(errs, cs.Problems...)
	if err := CheckCustomName(root, cs.Name); err != nil {
		errs = append(errs, err.Error())
	}
	if cs.Content == nil {
		return errs, warns // thiếu SKILL.md đã được báo ở Problems
	}
	d, err := ParseDoc(cs.Content)
	if err != nil {
		return append(errs, "SKILL.md: "+err.Error()), warns
	}
	switch {
	case d.Name == "":
		errs = append(errs, fmt.Sprintf("frontmatter thiếu name (phải là %q)", cs.Name))
	case d.Name != cs.Name:
		errs = append(errs, fmt.Sprintf("name %q khác tên thư mục %q", d.Name, cs.Name))
	}
	desc := strings.TrimSpace(d.Description)
	switch n := utf8.RuneCountInString(desc); {
	case desc == "":
		errs = append(errs, "description rỗng: harness chọn skill theo mô tả này")
	case n > MaxDescriptionRunes:
		errs = append(errs, fmt.Sprintf("description quá dài (%d ký tự, tối đa %d)", n, MaxDescriptionRunes))
	default:
		l := strings.ToLower(desc)
		if !strings.HasPrefix(l, "dùng khi") && !strings.HasPrefix(l, "use when") {
			warns = append(warns, `description nên mở đầu bằng "Dùng khi …" và chỉ nêu điều kiện kích hoạt (không tóm tắt cách làm)`)
		}
	}
	var bad []string
	for _, k := range d.Extra {
		if !customKeys[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		errs = append(errs, "frontmatter chỉ được có name, description, disable-model-invocation, argument-hint (thừa: "+strings.Join(bad, ", ")+")")
	}
	switch {
	case strings.TrimSpace(d.Body) == "":
		errs = append(errs, "thân SKILL.md rỗng")
	case strings.Count(d.Body, "\n")+1 > MaxBodyLines:
		errs = append(errs, fmt.Sprintf("thân SKILL.md quá dài (%d dòng, tối đa %d): tách phần tham chiếu sang tệp phụ", strings.Count(d.Body, "\n")+1, MaxBodyLines))
	}
	errs = append(errs, checkLinks(cs, d.Body)...)
	return errs, warns
}

// CustomExists cho biết .bint/skills/<name>/ đã là thư mục skill tuỳ biến (tên không dành riêng). Skill tiến hoá
// không được mang tên này: hai nguồn cùng xuất bản vào .claude/skills/<name>/ sẽ đè nhau.
func CustomExists(root, name string) bool {
	if root == "" || reserved(name) || strings.HasPrefix(name, ".") {
		return false
	}
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(CustomRel), name))
	return err == nil
}

// CheckCustomName kiểm tên skill tuỳ biến: quy tắc đặt tên (ValidName), và không trùng skill tiến hoá của repository.
func CheckCustomName(root, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if root != "" {
		if fi, err := os.Stat(EvolvedDir(root, name)); err == nil && fi.IsDir() {
			return fmt.Errorf("tên skill %q trùng skill tiến hoá", name)
		}
	}
	return nil
}

// openFence trả về rào mở khối mã (3 ký tự ` hoặc ~ trở lên, lặp cùng một ký tự) ở đầu dòng t đã cắt khoảng trắng;
// rỗng nếu không phải rào mở.
func openFence(t string) string {
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := len(t) - len(strings.TrimLeft(t, t[:1]))
	if n < 3 {
		return ""
	}
	if t[0] == '`' && strings.Contains(t[n:], "`") {
		return "" // ```code``` là mã dòng, không phải rào
	}
	return t[:n]
}

// closesFence: dòng t đóng khối mở bằng open khi chỉ gồm cùng ký tự rào, dài ít nhất bằng rào mở.
func closesFence(t, open string) bool {
	return len(t) >= len(open) && strings.Trim(t, open[:1]) == ""
}

// checkLinks kiểm liên kết Markdown tương đối trong thân (bỏ khối mã và mã dòng): phải trỏ tới tệp phụ đã nạp.
func checkLinks(cs *CustomSkill, body string) []string {
	have := map[string]bool{"SKILL.md": true}
	for _, f := range cs.Files {
		have[f.Rel] = true
	}
	var prose []string
	fence := ""
	for ln := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(ln)
		if fence != "" {
			if closesFence(t, fence) {
				fence = ""
			}
			continue
		}
		if f := openFence(t); f != "" {
			fence = f
			continue
		}
		prose = append(prose, inlineCodeRe.ReplaceAllString(ln, ""))
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(strings.Join(prose, "\n"), -1) {
		target := m[1]
		if target == "" || strings.HasPrefix(target, "#") || schemeRe.MatchString(target) || strings.HasPrefix(target, "//") {
			continue
		}
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
		if u, err := url.PathUnescape(target); err == nil {
			target = u
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		clean := path.Clean(strings.ReplaceAll(target, `\`, "/"))
		switch {
		case path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../"):
			out = append(out, fmt.Sprintf("liên kết %q thoát khỏi thư mục skill", m[1]))
		case clean == "tests" || strings.HasPrefix(clean, "tests/"):
			out = append(out, fmt.Sprintf("liên kết %q trỏ vào tests/ (không được xuất bản)", m[1]))
		case have[clean] || hasUnder(have, clean):
		default:
			out = append(out, fmt.Sprintf("liên kết %q không trỏ tới tệp phụ nào của skill", m[1]))
		}
	}
	return out
}

func hasUnder(have map[string]bool, dir string) bool {
	for f := range have {
		if strings.HasPrefix(f, dir+"/") {
			return true
		}
	}
	return false
}

// CustomTemplate dựng SKILL.md mẫu cho `bint skill new`: qua CheckCustom không lỗi và không cảnh báo.
func CustomTemplate(name string) []byte {
	return []byte(`---
name: ` + name + `
description: Dùng khi <điều kiện kích hoạt cụ thể, như loại tác vụ, triệu chứng hay từ khoá agent sẽ gặp> (chỉ nêu khi nào dùng, không tóm tắt cách làm)
---

# ` + name + `

## Tổng quan

Một hai câu: skill này giúp agent làm gì và nguyên tắc cốt lõi là gì.

## Khi nào dùng

- Tình huống hay triệu chứng cụ thể 1.
- Tình huống hay triệu chứng cụ thể 2.

Không dùng khi: nêu rõ trường hợp tương tự nhưng skill này không áp dụng.

## Cách làm

1. Bước thứ nhất.
2. Bước thứ hai.

Tài liệu tham chiếu dài (hơn khoảng 100 dòng) hay script dùng lại được thì tách thành tệp phụ trong thư mục này
và liên kết từ đây; tệp phụ được ` + "`bint sync`" + ` xuất bản cùng SKILL.md.

## Lỗi thường gặp

- Lỗi agent hay mắc và cách tránh.
`)
}

// CustomFilesStatus so các tệp phụ đã xuất bản của skill tuỳ biến với nguồn (missing | current | differs), cho doctor;
// SKILL.md do Status kiểm riêng.
func CustomFilesStatus(root string, t Target, cs *CustomSkill) (string, error) {
	for _, f := range cs.Files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(t.Dir), cs.Name, filepath.FromSlash(f.Rel)))
		if errors.Is(err, os.ErrNotExist) {
			return "missing", nil
		}
		if err != nil {
			return "", err
		}
		if !bytes.Equal(b, f.Content) {
			return "differs", nil
		}
	}
	return "current", nil
}
