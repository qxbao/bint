package iso

import (
	"fmt"
	"regexp"
	"strings"
)

// Tool là harness có profile. Agy (antigravity) để đợt sau: thêm một dòng vào tools.
type Tool string

const (
	Claude Tool = "claude"
	Codex  Tool = "codex"
)

// DefaultName là tên profile khi người dùng không chỉ định.
const DefaultName = "default"

// toolSpec mô tả cách harness tìm thư mục cấu hình của nó.
type toolSpec struct {
	Name Tool
	// ConfigEnv là biến môi trường trỏ thư mục cấu hình; ConfigDir là thư mục đó trong HOME giả.
	ConfigEnv string
	ConfigDir string
}

var tools = []toolSpec{
	{Claude, "CLAUDE_CONFIG_DIR", ".claude"},
	{Codex, "CODEX_HOME", ".codex"},
}

// Tools trả về tên các tool được hỗ trợ.
func Tools() []Tool {
	out := make([]Tool, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}

// ParseTool nhận "claude" hoặc "codex" (đúng chữ thường).
func ParseTool(s string) (Tool, error) {
	for _, t := range tools {
		if string(t.Name) == s {
			return t.Name, nil
		}
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, string(t.Name))
	}
	return "", fmt.Errorf("tool không hỗ trợ %q (hỗ trợ: %s)", s, strings.Join(names, ", "))
}

func (t Tool) spec() toolSpec {
	for _, s := range tools {
		if s.Name == t {
			return s
		}
	}
	return toolSpec{Name: t}
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidName kiểm tên profile: ^[A-Za-z0-9._-]{1,64}$, không phải "." hay "..".
func ValidName(s string) error {
	if s == "." || s == ".." || !nameRE.MatchString(s) {
		return fmt.Errorf("tên profile không hợp lệ %q (chỉ chữ, số, '.', '_', '-', 1–64 ký tự, không phải . hay ..)", s)
	}
	return nil
}

// dirName là tên thư mục của profile: <tool>--<name>. Tên tool không chứa "-" nên cắt ở "--" đầu tiên
// luôn đúng, kể cả khi name tự chứa "--".
func dirName(t Tool, name string) string { return string(t) + "--" + name }

func parseDirName(dir string) (Tool, string, error) {
	ts, name, ok := strings.Cut(dir, "--")
	if !ok {
		return "", "", fmt.Errorf("tên thư mục %q không có dạng <tool>--<name>", dir)
	}
	t, err := ParseTool(ts)
	if err != nil {
		return "", "", err
	}
	if err := ValidName(name); err != nil {
		return "", "", err
	}
	return t, name, nil
}

// ParseID tách "<tool>/<name>" (dạng BINT_ISO_PROFILE, Profile.ID) và kiểm cả hai phần.
func ParseID(id string) (Tool, string, error) {
	ts, name, ok := strings.Cut(id, "/")
	if !ok {
		return "", "", fmt.Errorf("mã profile %q không có dạng <tool>/<name>", id)
	}
	t, err := ParseTool(ts)
	if err != nil {
		return "", "", err
	}
	if err := ValidName(name); err != nil {
		return "", "", err
	}
	return t, name, nil
}
