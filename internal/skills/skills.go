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
}

// LockEntry ghi phiên bản một skill gốc.
type LockEntry struct {
	SHA256    string   `yaml:"sha256"`
	Published []string `yaml:"published,omitempty"`
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
	b.WriteString("# tuỳ biến theo dự án đặt trong .bint/skills/overlays/ (P4).\n")
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
}

// ClaudeTarget là nơi Claude Code đọc skill cấp dự án.
var ClaudeTarget = Target{Harness: "claude", Dir: ".claude/skills"}

// Outcome của một lần xuất bản.
type Outcome struct {
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
}

// Install xuất bản mọi skill gốc cho các harness và cập nhật core.lock. Tệp đã bị
// sửa ngoài Bint (băm khác bản Bint ghi lần trước) không bị ghi đè trừ khi Force.
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
	for _, s := range Core() {
		entry := LockEntry{SHA256: s.Hash()}
		for _, t := range opts.Targets {
			rel := filepath.ToSlash(filepath.Join(t.Dir, s.Name, "SKILL.md"))
			o, err := publish(opts, s, rel, lock.Skills[s.Name].SHA256)
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
	if _, err := writeLock(opts.Root, lock); err != nil {
		return outs, err
	}
	return outs, nil
}

// publish ghi một skill. Tệp hiện có được coi là "chưa bị sửa" nếu băm của nó trùng
// băm Bint ghi lần trước trên máy này (manifest) hoặc trùng phiên bản trong
// core.lock đã commit (máy khác trong nhóm đã xuất bản).
func publish(opts InstallOptions, s Skill, rel, lockSHA string) (Outcome, error) {
	o := Outcome{Skill: s.Name, Path: rel}
	full := filepath.Join(opts.Root, filepath.FromSlash(rel))
	key := rel + "#skill"
	want := fsafe.Hash(string(s.Content))
	have, err := os.ReadFile(full)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := fsafe.WriteAtomic(full, s.Content, 0o644); err != nil {
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
		return o, nil
	}
	prev, known := opts.Manifest.Get(key)
	pristine := (known && prev == cur) || (lockSHA != "" && lockSHA == Skill{Content: have}.Hash())
	if !opts.Force && !pristine {
		o.Result = fsafe.Conflict
		o.Detail = "skill gốc đã bị sửa ngoài Bint (tuỳ biến thuộc về lớp phủ, P4); --force để ghi đè (bản cũ được sao lưu)"
		return o, nil
	}
	bp, err := fsafe.Backup(full, opts.BackupDir, rel, opts.Now)
	if err != nil {
		return o, err
	}
	if err := fsafe.WriteAtomic(full, s.Content, 0o644); err != nil {
		return o, err
	}
	opts.Manifest.Set(key, want)
	o.Result, o.Backup = fsafe.Updated, bp
	return o, nil
}

// Status cho biết trạng thái một skill đã xuất bản (dùng cho doctor).
func Status(root string, t Target, s Skill) (string, error) {
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
