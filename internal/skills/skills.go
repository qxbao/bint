// Package skills chứa skill gốc của Bint (nhúng trong tệp thực thi) và việc xuất
// bản chúng cho harness. Skill gốc là bất biến trong repository (mục 6.6): tệp đã
// xuất bản bị sửa tay sẽ được báo xung đột thay vì ghi đè; phiên bản đang dùng
// được ghi trong .bint/skills/core.lock.
package skills

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
)

//go:embed core
var coreFS embed.FS

// Skill là một skill gốc.
type Skill struct {
	Name    string
	Content []byte
}

// Hash trả về băm nội dung (dùng làm phiên bản trong core.lock).
func (s Skill) Hash() string {
	h := sha256.Sum256(s.Content)
	return hex.EncodeToString(h[:])[:16]
}

// Body trả về nội dung skill không gồm phần đầu YAML (dùng làm đề bài khi gọi
// agent không qua cơ chế skill của harness).
func (s Skill) Body() string {
	c := strings.ReplaceAll(string(s.Content), "\r\n", "\n")
	if strings.HasPrefix(c, "---\n") {
		if i := strings.Index(c[4:], "\n---\n"); i >= 0 {
			return strings.TrimSpace(c[4+i+5:])
		}
	}
	return strings.TrimSpace(c)
}

// Core trả về các skill gốc có trong bản này.
func Core() []Skill {
	entries, err := fs.ReadDir(coreFS, "core")
	if err != nil {
		panic(err)
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := coreFS.ReadFile("core/" + e.Name() + "/SKILL.md")
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: e.Name(), Content: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get trả về skill gốc theo tên.
func Get(name string) (Skill, bool) {
	for _, s := range Core() {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}

// LockRel là đường dẫn core.lock tương đối với thư mục gốc repository.
const LockRel = ".bint/skills/core.lock"

// Lock là nội dung core.lock.
type Lock struct {
	BintVersion string               `yaml:"bint_version"`
	Skills      map[string]LockEntry `yaml:"skills"`
	// Evolved: skill chuyên biệt do tiến hoá tạo ra đã được xuất bản (P7).
	Evolved map[string]LockEntry `yaml:"evolved,omitempty"`
	// Agents: định nghĩa subagent của driver session đã được xuất bản (.claude/agents, G3b mục 6.4); khoá phiên bản như
	// skill gốc: tệp bị sửa ngoài Bint thì sync báo xung đột thay vì ghi đè.
	Agents map[string]LockEntry `yaml:"agents,omitempty"`
	// Custom: skill tuỳ biến của dự án (.bint/skills/<tên>/) đã được xuất bản (GĐ1 custom-flow-plan).
	Custom map[string]LockEntry `yaml:"custom,omitempty"`
	// Flows: skill điều phối của flow tuỳ biến (.bint/flows/<tên>.yml) đã được xuất bản (GĐ5 custom-flow-plan); SHA256 là
	// băm bản định dạng Claude, Variants["agents"] là băm bản .agents/skills. Rỗng ở core.lock cũ.
	Flows map[string]LockEntry `yaml:"flows,omitempty"`
}

// AgentFile là một định nghĩa subagent do Bint sinh (nội dung phụ thuộc cấu hình: model, maxTurns, công cụ).
type AgentFile struct {
	Name    string
	Rel     string // đường dẫn tương đối với gốc repository, ví dụ ".claude/agents/bint-planner.md"
	Content []byte
}

// LockEntry ghi phiên bản một skill gốc.
type LockEntry struct {
	// SHA256 là băm skill gốc (phiên bản đang dùng).
	SHA256 string `yaml:"sha256"`
	// Overlay là băm lớp phủ đã áp (rỗng = không có).
	Overlay string `yaml:"overlay,omitempty"`
	// Composed là băm nội dung đã xuất bản (gốc + lớp phủ); rỗng ở core.lock cũ
	// (trước P4) nghĩa là bằng SHA256.
	Composed  string   `yaml:"composed,omitempty"`
	Published []string `yaml:"published,omitempty"`
	// Variants: băm nội dung đã xuất bản theo từng định dạng khác định dạng gốc
	// (ví dụ "agents" cho .agents/skills) — để máy khác trong nhóm nhận ra tệp chưa bị sửa.
	Variants map[string]string `yaml:"variants,omitempty"`
	// Files: băm (SKILL.md theo định dạng của target, và từng tệp phụ) theo từng đường dẫn đã xuất bản — skill tuỳ biến
	// có nhiều tệp nên cần băm riêng từng tệp để nhận ra tệp nào bị sửa tay. Rỗng ở skill gốc/tiến hoá và core.lock cũ.
	Files map[string]string `yaml:"files,omitempty"`
}

// PublishedHash trả về băm nội dung đã xuất bản.
func (e LockEntry) PublishedHash() string {
	if e.Composed != "" {
		return e.Composed
	}
	return e.SHA256
}

// ReadLock đọc core.lock (rỗng nếu chưa có).
func ReadLock(root string) (Lock, error) {
	var l Lock
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(LockRel)))
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	err = yaml.Unmarshal(data, &l)
	return l, err
}

func writeLock(root string, l Lock) (bool, error) {
	var b strings.Builder
	b.WriteString("# Do Bint quản lý: phiên bản skill gốc đang dùng (mục 6.6). Không sửa tay;\n")
	b.WriteString("# tuỳ biến theo dự án đặt trong .bint/skills/overlays/ (lớp phủ) hoặc .bint/skills/<tên>/ (skill tuỳ biến)\n")
	b.WriteString("# rồi chạy `bint sync`.\n")
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(l); err != nil {
		return false, err
	}
	enc.Close()
	b.Write(buf.Bytes())
	p := filepath.Join(root, filepath.FromSlash(LockRel))
	if old, err := os.ReadFile(p); err == nil && string(old) == b.String() {
		return false, nil
	}
	return true, fsafe.WriteAtomic(p, []byte(b.String()), 0o644)
}

// Target là nơi xuất bản skill cho một harness.
type Target struct {
	Harness string
	// Dir là thư mục skill tương đối với gốc repository, ví dụ ".claude/skills".
	Dir string
	// Format: "" = định dạng gốc (Claude Code); FormatAgents = chỉ giữ name và
	// description trong frontmatter.
	Format string
}

// FormatAgents là định dạng SKILL.md tối giản của .agents/skills (Codex, Antigravity):
// frontmatter chỉ có name và description. Khoá harness không hiểu (ví dụ
// disable-model-invocation) bị bỏ thay vì giả lập (mục 7: chính sách skill nội bộ).
const FormatAgents = "agents"

// ClaudeTarget là nơi Claude Code đọc skill cấp dự án.
var ClaudeTarget = Target{Harness: "claude", Dir: ".claude/skills"}

// AgentsTarget là nơi Codex và Antigravity đọc skill cấp dự án.
var AgentsTarget = Target{Harness: "codex,antigravity", Dir: ".agents/skills", Format: FormatAgents}

// ForTarget trả về skill ở định dạng của target.
func ForTarget(s Skill, t Target) Skill {
	if t.Format != FormatAgents {
		return s
	}
	c := strings.ReplaceAll(string(s.Content), "\r\n", "\n")
	if !strings.HasPrefix(c, "---\n") {
		return s
	}
	end := strings.Index(c[4:], "\n---\n")
	if end < 0 {
		return s
	}
	var fm yaml.Node
	if err := yaml.Unmarshal([]byte(c[4:4+end]), &fm); err != nil || len(fm.Content) == 0 || fm.Content[0].Kind != yaml.MappingNode {
		return s
	}
	m := fm.Content[0]
	var kept []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		switch m.Content[i].Value {
		case "name", "description":
			kept = append(kept, m.Content[i], m.Content[i+1])
		}
	}
	m.Content = kept
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return s
	}
	enc.Close()
	return Skill{Name: s.Name, Content: []byte("---\n" + buf.String() + "---\n" + c[4+end+5:])}
}

// Outcome của một lần xuất bản.
type Outcome struct {
	// Agent: tệp là định nghĩa subagent (Skill là tên subagent), không phải skill.
	Agent  bool
	Skill  string
	Path   string
	Result fsafe.Outcome
	Backup string
	Detail string
}

// InstallOptions điều khiển Install.
type InstallOptions struct {
	Root        string
	Targets     []Target
	BintVersion string
	Force       bool
	BackupDir   string
	Manifest    *fsafe.Manifest
	Now         time.Time
	// Files: các tệp theo dõi của repository, để phát hiện skill tiến hoá lỗi thời
	// (nil = bỏ qua kiểm tra này).
	Files []string
	// Agents: định nghĩa subagent cần xuất bản (cùng cơ chế khoá phiên bản; rỗng = không xuất bản).
	Agents []AgentFile
}

// Install ghép skill gốc với lớp phủ, xuất bản cho các harness và cập nhật
// core.lock (mục 6.6). Tệp đã bị sửa ngoài Bint (băm khác bản Bint ghi lần trước)
// không bị ghi đè trừ khi Force. Lớp phủ không áp được (điểm neo không còn sau
// nâng cấp, cú pháp sai) → xung đột: bản đã xuất bản và mục core.lock cũ được giữ
// nguyên cho tới khi người dùng sửa lớp phủ; Force không vượt qua được xung đột này.
func Install(opts InstallOptions) ([]Outcome, error) {
	lock, err := ReadLock(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", LockRel, err)
	}
	if lock.Skills == nil {
		lock.Skills = map[string]LockEntry{}
	}
	lock.BintVersion = opts.BintVersion
	var outs []Outcome
	for _, core := range Core() {
		prev := lock.Skills[core.Name]
		s, ov, err := Composed(opts.Root, core)
		if err != nil {
			var ce *ConflictError
			if !errors.As(err, &ce) {
				return outs, err
			}
			for _, t := range opts.Targets {
				outs = append(outs, Outcome{Skill: core.Name, Path: filepath.ToSlash(filepath.Join(t.Dir, core.Name, "SKILL.md")),
					Result: fsafe.Conflict, Detail: ce.Error() + "; giữ bản đã xuất bản trước đó"})
			}
			continue
		}
		entry := LockEntry{SHA256: core.Hash(), Overlay: ov.Hash()}
		if ov != nil {
			entry.Composed = s.Hash()
		}
		for _, t := range opts.Targets {
			rel := filepath.ToSlash(filepath.Join(t.Dir, s.Name, "SKILL.md"))
			ts := ForTarget(s, t)
			prevHash := prev.PublishedHash()
			if t.Format != "" {
				prevHash = prev.Variants[t.Format]
				if entry.Variants == nil {
					entry.Variants = map[string]string{}
				}
				entry.Variants[t.Format] = ts.Hash()
			}
			o, err := publish(opts, ts, rel, prevHash)
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result != fsafe.Conflict {
				entry.Published = append(entry.Published, rel)
			}
		}
		lock.Skills[s.Name] = entry
	}
	for _, orphan := range OrphanOverlays(opts.Root) {
		outs = append(outs, Outcome{Skill: strings.TrimSuffix(filepath.Base(orphan), ".md"), Path: orphan, Result: fsafe.Conflict,
			Detail: "lớp phủ cho skill gốc không tồn tại (đổi tên hoặc bị bỏ ở phiên bản này?)"})
	}
	eo, err := installEvolved(opts, &lock)
	outs = append(outs, eo...)
	if err != nil {
		return outs, err
	}
	co, err := installCustom(opts, &lock)
	outs = append(outs, co...)
	if err != nil {
		return outs, err
	}
	fo, err := installFlows(opts, &lock)
	outs = append(outs, fo...)
	if err != nil {
		return outs, err
	}
	ao, err := installAgents(opts, &lock)
	outs = append(outs, ao...)
	if err != nil {
		return outs, err
	}
	if _, err := writeLock(opts.Root, lock); err != nil {
		return outs, err
	}
	return outs, nil
}

// publish ghi một skill. Tệp hiện có được coi là "chưa bị sửa" nếu băm của nó trùng
// băm Bint ghi lần trước trên máy này (manifest) hoặc trùng phiên bản trong
// core.lock đã commit (máy khác trong nhóm đã xuất bản).
func publish(opts InstallOptions, s Skill, rel, lockSHA string) (Outcome, error) {
	return publishFile(opts, s.Name, rel, s.Content, 0o644, lockSHA,
		"skill đã bị sửa ngoài Bint (tuỳ biến đặt trong "+OverlayPath(s.Name)+", xem `bint skill overlay`); --force để ghi đè (bản cũ được sao lưu)")
}

// publishFile là lõi của publish cho một tệp bất kỳ: lockSHA là băm (Skill.Hash, 16 ký tự) bản đã xuất bản ghi
// trong core.lock, conflict là Detail khi tệp bị sửa ngoài Bint.
func publishFile(opts InstallOptions, name, rel string, content []byte, perm os.FileMode, lockSHA, conflict string) (Outcome, error) {
	o := Outcome{Skill: name, Path: rel}
	full := filepath.Join(opts.Root, filepath.FromSlash(rel))
	key := rel + "#skill"
	want := fsafe.Hash(string(content))
	have, err := os.ReadFile(full)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := fsafe.WriteAtomic(full, content, perm); err != nil {
			return o, err
		}
		opts.Manifest.Set(key, want)
		o.Result = fsafe.Created
		return o, nil
	case err != nil:
		return o, err
	}
	cur := fsafe.Hash(string(have))
	if cur == want {
		opts.Manifest.Set(key, want)
		o.Result = fsafe.Unchanged
		// Nội dung không đổi nhưng nguồn đổi bit thực thi (chmod +x script phụ): đồng bộ bit đó (Windows không có).
		if runtime.GOOS != "windows" {
			if fi, err := os.Stat(full); err == nil && (fi.Mode().Perm()^perm)&0o111 != 0 {
				if err := os.Chmod(full, fi.Mode().Perm()&^0o111|perm&0o111); err != nil {
					return o, err
				}
				o.Result, o.Detail = fsafe.Updated, "đổi quyền thực thi theo nguồn"
			}
		}
		return o, nil
	}
	prev, known := opts.Manifest.Get(key)
	pristine := (known && prev == cur) || (lockSHA != "" && lockSHA == Skill{Content: have}.Hash())
	if !opts.Force && !pristine {
		o.Result = fsafe.Conflict
		o.Detail = conflict
		return o, nil
	}
	bp, err := fsafe.Backup(full, opts.BackupDir, rel, opts.Now)
	if err != nil {
		return o, err
	}
	if err := fsafe.WriteAtomic(full, content, perm); err != nil {
		return o, err
	}
	opts.Manifest.Set(key, want)
	o.Result, o.Backup = fsafe.Updated, bp
	return o, nil
}

// Trạng thái tệp skill đã xuất bản (Status, PublishedState).
const (
	StateCurrent = "current"
	StateMissing = "missing"
	StateDiffers = "differs"
	// StateOtherVersion: tệp khác bản nhúng của binary đang chạy nhưng khớp băm core.lock ghi cho định dạng của thư mục
	// đích, và băm skill gốc trong core.lock khác bản nhúng: tệp do một bản Bint khác xuất bản, chưa ai sửa tay. Hướng
	// lệch (bint đang chạy cũ hơn hay mới hơn bản đã ghi) không do trạng thái này quyết định mà do nơi gọi so phiên bản.
	StateOtherVersion = "other_version"
)

// PublishedState như Status, thêm phân biệt StateOtherVersion khỏi StateDiffers dựa vào core.lock: core là skill gốc
// nhúng của binary, e là mục core.lock của nó. Tệp khớp băm core.lock mà SHA256 gốc trong lock khác bản nhúng thì
// không phải "sửa tay" (sửa tay làm băm lệch core.lock): tệp do bản Bint khác ghi.
func PublishedState(root string, t Target, s, core Skill, e LockEntry) (string, error) {
	st, err := Status(root, t, s)
	if err != nil || st != StateDiffers {
		return st, err
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(t.Dir), s.Name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	locked := e.PublishedHash()
	if t.Format != "" {
		locked = e.Variants[t.Format]
	}
	if locked != "" && e.SHA256 != "" && e.SHA256 != core.Hash() && (Skill{Content: b}).Hash() == locked {
		return StateOtherVersion, nil
	}
	return st, nil
}

// Status cho biết trạng thái một skill đã xuất bản so với nội dung mong muốn s
// (đã ghép lớp phủ) — dùng cho doctor.
func Status(root string, t Target, s Skill) (string, error) {
	s = ForTarget(s, t)
	full := filepath.Join(root, filepath.FromSlash(t.Dir), s.Name, "SKILL.md")
	b, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if fsafe.Hash(string(b)) == fsafe.Hash(string(s.Content)) {
		return "current", nil
	}
	return "differs", nil
}
