package hcomp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
)

func writeFile(path string, data []byte) error { return fsafe.WriteAtomic(path, data, 0o644) }

// Problem là một tệp không đọc được hoặc không hợp lệ; thành phần đó bị bỏ qua (không có
// hiệu lực) nhưng không làm hỏng việc nạp các thành phần khác.
type Problem struct {
	File string
	Err  string
}

func (p Problem) String() string { return p.File + ": " + p.Err }

// Library là mọi thành phần đọc được từ một thư mục harness.
type Library struct {
	// Dir là thư mục .bint/harness (có thể chưa tồn tại).
	Dir        string
	Components []Component
	Problems   []Problem
}

// Dir trả về thư mục thành phần của repository có gốc root.
func Dir(root string) string { return filepath.Join(root, ".bint", DirName) }

// Load đọc .bint/harness/ của repository có gốc root.
func Load(root string) *Library { return LoadDir(Dir(root)) }

type guidanceFM struct {
	Meta     `yaml:",inline"`
	Title    string   `yaml:"title,omitempty"`
	Requires []string `yaml:"requires,omitempty"`
}

type guardrailFile struct {
	Meta    `yaml:",inline"`
	On      string `yaml:"on"`
	When    When   `yaml:"when,omitempty"`
	Unless  Unless `yaml:"unless,omitempty"`
	Action  string `yaml:"action"`
	Message string `yaml:"message"`
}

type scriptFile struct {
	Meta        `yaml:",inline"`
	Description string `yaml:"description"`
	Usage       string `yaml:"usage,omitempty"`
}

type strategyFile struct {
	Meta    `yaml:",inline"`
	Context *StrategyContext `yaml:"context"`
}

// maxFile giới hạn kích thước một tệp thành phần (chống tệp lớn làm chậm hook).
const maxFile = 256 << 10

func decodeStrict(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// ParseGuidance đọc một tệp guidance (frontmatter + thân Markdown).
func ParseGuidance(data []byte) (Component, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return Component{}, errors.New("thiếu frontmatter (---)")
	}
	front, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return Component{}, errors.New("frontmatter không đóng (---)")
	}
	body = strings.TrimPrefix(body, "\n")
	var fm guidanceFM
	if err := decodeStrict([]byte(front), &fm); err != nil {
		return Component{}, fmt.Errorf("frontmatter: %w", err)
	}
	return Component{Kind: KindGuidance, Meta: fm.Meta, Title: fm.Title, Requires: fm.Requires,
		Body: strings.Trim(body, "\n")}, nil
}

// ParseGuardrail đọc một tệp guardrail.
func ParseGuardrail(data []byte) (Component, error) {
	var g guardrailFile
	if err := decodeStrict(data, &g); err != nil {
		return Component{}, err
	}
	return Component{Kind: KindGuardrail, Meta: g.Meta, On: g.On, When: g.When, Unless: g.Unless,
		Action: g.Action, Message: g.Message}, nil
}

// ParseScript đọc phần mô tả (.yml) của một script; source là nội dung .sh (có thể nil).
func ParseScript(data, source []byte) (Component, error) {
	var s scriptFile
	if err := decodeStrict(data, &s); err != nil {
		return Component{}, err
	}
	return Component{Kind: KindScript, Meta: s.Meta, Description: s.Description, Usage: s.Usage, Source: source}, nil
}

// ParseStrategy đọc strategy.yml.
func ParseStrategy(data []byte) (Component, error) {
	var s strategyFile
	if err := decodeStrict(data, &s); err != nil {
		return Component{}, err
	}
	return Component{Kind: KindStrategy, Meta: s.Meta, Context: s.Context}, nil
}

// Marshal dựng nội dung tệp chính của thành phần (guidance .md, còn lại .yml). Script:
// phần mô tả; nội dung .sh nằm ở Component.Source.
func Marshal(c Component) ([]byte, error) {
	switch c.Kind {
	case KindGuidance:
		b, err := yaml.Marshal(guidanceFM{Meta: c.Meta, Title: c.Title, Requires: c.Requires})
		if err != nil {
			return nil, err
		}
		return []byte("---\n" + string(b) + "---\n\n" + strings.Trim(c.Body, "\n") + "\n"), nil
	case KindGuardrail:
		return yaml.Marshal(guardrailFile{Meta: c.Meta, On: c.On, When: c.When, Unless: c.Unless, Action: c.Action, Message: c.Message})
	case KindScript:
		return yaml.Marshal(scriptFile{Meta: c.Meta, Description: c.Description, Usage: c.Usage})
	case KindStrategy:
		return yaml.Marshal(strategyFile{Meta: c.Meta, Context: c.Context})
	}
	return nil, fmt.Errorf("loại thành phần không hợp lệ %q", c.Kind)
}

// LoadDir đọc một thư mục harness (dùng cho cả bản dựng của lượt eval). Không bao giờ trả
// lỗi: tệp hỏng được ghi vào Problems và bị bỏ qua.
func LoadDir(dir string) *Library {
	lib := &Library{Dir: dir}
	bad := func(rel string, err error) { lib.Problems = append(lib.Problems, Problem{File: rel, Err: err.Error()}) }
	read := func(rel string) ([]byte, bool) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		fi, err := os.Lstat(p)
		if err != nil {
			bad(rel, err)
			return nil, false
		}
		if !fi.Mode().IsRegular() {
			bad(rel, errors.New("không phải tệp thường (liên kết/thiết bị bị bỏ qua)"))
			return nil, false
		}
		if fi.Size() > maxFile {
			bad(rel, fmt.Errorf("tệp quá lớn (%d > %d byte)", fi.Size(), maxFile))
			return nil, false
		}
		b, err := os.ReadFile(p)
		if err != nil {
			bad(rel, err)
			return nil, false
		}
		return b, true
	}
	names := func(sub, ext string) []string {
		ents, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return nil
		}
		var out []string
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
				out = append(out, e.Name())
			}
		}
		sort.Strings(out)
		return out
	}
	add := func(rel string, c Component, err error, wantID string) {
		if err != nil {
			bad(rel, err)
			return
		}
		c.File = rel
		if wantID != "" && c.ID != wantID {
			bad(rel, fmt.Errorf("id %q không khớp tên tệp %q", c.ID, wantID))
			return
		}
		lib.Components = append(lib.Components, c)
	}

	for _, n := range names("guidance", ".md") {
		rel := "guidance/" + n
		if b, ok := read(rel); ok {
			c, err := ParseGuidance(b)
			add(rel, c, err, strings.TrimSuffix(n, ".md"))
		}
	}
	for _, n := range names("guardrails", ".yml") {
		rel := "guardrails/" + n
		if b, ok := read(rel); ok {
			c, err := ParseGuardrail(b)
			add(rel, c, err, strings.TrimSuffix(n, ".yml"))
		}
	}
	ymls := map[string]bool{}
	for _, n := range names("scripts", ".yml") {
		id := strings.TrimSuffix(n, ".yml")
		ymls[id] = true
		rel := "scripts/" + n
		b, ok := read(rel)
		if !ok {
			continue
		}
		var src []byte
		if _, err := os.Lstat(filepath.Join(dir, "scripts", id+".sh")); err == nil {
			src, _ = read("scripts/" + id + ".sh")
		}
		c, err := ParseScript(b, src)
		add(rel, c, err, id)
	}
	for _, n := range names("scripts", ".sh") {
		if id := strings.TrimSuffix(n, ".sh"); !ymls[id] {
			bad("scripts/"+n, errors.New("thiếu tệp mô tả "+id+".yml (script không có hiệu lực)"))
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "strategy.yml")); err == nil {
		if b, ok := read("strategy.yml"); ok {
			c, err := ParseStrategy(b)
			add("strategy.yml", c, err, StrategyID)
		}
	}
	// Tệp hợp lệ về cú pháp nhưng sai nội dung cũng không có hiệu lực.
	kept := lib.Components[:0]
	for _, c := range lib.Components {
		if err := Check(c); err != nil {
			bad(c.File, err)
			continue
		}
		kept = append(kept, c)
	}
	lib.Components = kept
	sort.SliceStable(lib.Components, func(i, j int) bool {
		a, b := lib.Components[i], lib.Components[j]
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		return a.ID < b.ID
	})
	return lib
}

func kindOrder(k Kind) int {
	for i, x := range Kinds {
		if x == k {
			return i
		}
	}
	return len(Kinds)
}

// Find trả về thành phần theo loại và id.
func (l *Library) Find(kind Kind, id string) (Component, bool) {
	for _, c := range l.Components {
		if c.Kind == kind && c.ID == id {
			return c, true
		}
	}
	return Component{}, false
}

// FindID tìm theo id ở mọi loại (id guidance/guardrail/script có thể trùng giữa các loại,
// khi đó trả loại đầu tiên theo thứ tự Kinds).
func (l *Library) FindID(id string) (Component, bool) {
	for _, c := range l.Components {
		if c.ID == id {
			return c, true
		}
	}
	return Component{}, false
}

// Files dựng nội dung các tệp của thành phần, theo đường dẫn tương đối trong thư mục harness
// (tệp chính; thêm .sh với script có nội dung).
func Files(c Component) (map[string][]byte, error) {
	if err := Check(c); err != nil {
		return nil, err
	}
	body, err := Marshal(c)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{RelFile(c.Kind, c.ID): body}
	if c.Kind == KindScript && len(c.Source) > 0 {
		files["scripts/"+c.ID+".sh"] = c.Source
	}
	return files, nil
}

// Save ghi thành phần vào thư mục (tạo thư mục con khi cần) bằng ghi nguyên tử. Script
// ghi cả .sh (quyền 0o644: Bint không bao giờ chạy nó). Trả về các tệp đã ghi (tương đối).
func Save(dir string, c Component) ([]string, error) {
	files, err := Files(c)
	if err != nil {
		return nil, err
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(n)), files[n]); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// Remove xoá tệp của thành phần (dùng khi quay lui một đề xuất create).
func Remove(dir string, c Component) error {
	rel := RelFile(c.Kind, c.ID)
	paths := []string{rel}
	if c.Kind == KindScript {
		paths = append(paths, "scripts/"+c.ID+".sh")
	}
	for _, r := range paths {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(r))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// FileSHA băm nội dung các tệp của thành phần trong dir (tệp chính, kèm .sh của script);
// "" nếu tệp chính không tồn tại. Dùng làm phiên bản gốc của đề xuất quy trình.
func FileSHA(dir string, c Component) (string, error) {
	rels := []string{RelFile(c.Kind, c.ID)}
	if c.Kind == KindScript {
		rels = append(rels, "scripts/"+c.ID+".sh")
	}
	h := sha256.New()
	for i, r := range rels {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r)))
		if errors.Is(err, fs.ErrNotExist) {
			if i == 0 {
				return "", nil
			}
			continue
		}
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", r, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
