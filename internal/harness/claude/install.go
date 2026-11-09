package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/fsafe"
)

// Scope là phạm vi tệp settings của Claude Code để cài hook.
type Scope string

const (
	// ScopeLocal: <repo>/.claude/settings.local.json — không commit, chỉ máy này.
	ScopeLocal Scope = "local"
	// ScopeProject: <repo>/.claude/settings.json — commit, cả nhóm cần cài Bint.
	ScopeProject Scope = "project"
	// ScopeUser: ~/.claude/settings.json — mọi repository và mọi worktree trên máy;
	// hook tự bỏ qua repository chưa khởi tạo Bint.
	ScopeUser Scope = "user"
)

// ParseScope kiểm tra giá trị phạm vi.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeLocal, ScopeProject, ScopeUser:
		return Scope(s), nil
	}
	return "", fmt.Errorf("phạm vi hook không hợp lệ %q (local | project | user)", s)
}

// SettingsPath trả về tệp settings tương ứng phạm vi.
func SettingsPath(scope Scope, repoRoot string) (string, error) {
	switch scope {
	case ScopeLocal:
		return filepath.Join(repoRoot, ".claude", "settings.local.json"), nil
	case ScopeProject:
		return filepath.Join(repoRoot, ".claude", "settings.json"), nil
	case ScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	return "", fmt.Errorf("phạm vi không hợp lệ %q", scope)
}

// HookTimeoutSeconds giới hạn thời gian mỗi lần chạy hook; hook chỉ ghi một tệp nhỏ.
const HookTimeoutSeconds = 10

const toolMatcher = "Edit|Write|MultiEdit|NotebookEdit|Bash|mcp__bint__.*"

// pathMatcher: công cụ được chèn ngữ cảnh theo đường dẫn (PreToolUse).
const pathMatcher = "Read|Edit|Write|MultiEdit|NotebookEdit"

// hookSubcommand nhận diện hook do Bint cài (để cập nhật/gỡ mà không động tới hook khác).
const hookSubcommand = "hook claude"

// HookCommand dựng lệnh hook từ đường dẫn chương trình bint.
func HookCommand(bintPath string) string {
	if bintPath == "bint" {
		return "bint " + hookSubcommand
	}
	return `"` + filepath.ToSlash(bintPath) + `" ` + hookSubcommand
}

func isBintHook(cmd string) bool {
	return strings.Contains(cmd, "bint") && strings.HasSuffix(strings.TrimSpace(cmd), hookSubcommand)
}

type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func desiredGroups(cmd string) map[string]map[string]any {
	h := handler{Type: "command", Command: cmd, Timeout: HookTimeoutSeconds}
	group := func(matcher string) map[string]any {
		g := map[string]any{"hooks": []any{h}}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return g
	}
	return map[string]map[string]any{
		evSessionStart:       group(""),
		evSessionEnd:         group(""),
		evPostToolUse:        group(toolMatcher),
		evPostToolUseFailure: group(toolMatcher),
		evPreToolUse:         group(pathMatcher),
	}
}

// InstallResult mô tả kết quả cài hook.
type InstallResult struct {
	Path       string
	Changed    bool
	BackupPath string
}

// InstallHooks thêm (hoặc cập nhật) các hook ghi nhận của Bint vào tệp settings,
// giữ nguyên mọi khoá và hook khác.
func InstallHooks(settingsPath, bintPath, backupDir string, now time.Time) (InstallResult, error) {
	return editHooks(settingsPath, backupDir, now, HookCommand(bintPath))
}

// UninstallHooks gỡ các hook của Bint khỏi tệp settings.
func UninstallHooks(settingsPath, backupDir string, now time.Time) (InstallResult, error) {
	return editHooks(settingsPath, backupDir, now, "")
}

func editHooks(settingsPath, backupDir string, now time.Time, cmd string) (InstallResult, error) {
	res := InstallResult{Path: settingsPath}
	raw, err := os.ReadFile(settingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	doc, err := parseOrdered(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %w", settingsPath, err)
	}
	hooks := map[string][]any{}
	if v, ok := doc.get("hooks"); ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(v, &m); err != nil {
			return res, fmt.Errorf("%s: khoá hooks không phải object: %w", settingsPath, err)
		}
		for ev, list := range m {
			var groups []any
			if err := json.Unmarshal(list, &groups); err != nil {
				return res, fmt.Errorf("%s: hooks.%s không phải mảng: %w", settingsPath, ev, err)
			}
			hooks[ev] = groups
		}
	}
	// Gỡ mọi handler của Bint hiện có, bỏ nhóm rỗng.
	for ev, groups := range hooks {
		var kept []any
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			hs, _ := gm["hooks"].([]any)
			var keepH []any
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); hm != nil && isBintHook(c) {
					continue
				}
				keepH = append(keepH, h)
			}
			if len(hs) > 0 && len(keepH) == 0 {
				continue
			}
			gm["hooks"] = keepH
			kept = append(kept, gm)
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	if cmd != "" {
		for ev, g := range desiredGroups(cmd) {
			hooks[ev] = append(hooks[ev], g)
		}
	}
	if len(hooks) == 0 {
		doc.del("hooks")
	} else {
		b, err := json.Marshal(hooks)
		if err != nil {
			return res, err
		}
		doc.set("hooks", b)
	}
	out, err := doc.marshalIndent()
	if err != nil {
		return res, err
	}
	if bytes.Equal(normalizeJSON(raw), normalizeJSON(out)) {
		return res, nil
	}
	if len(raw) == 0 && cmd == "" {
		return res, nil
	}
	if res.BackupPath, err = fsafe.Backup(settingsPath, backupDir, "claude/"+filepath.Base(settingsPath), now); err != nil {
		return res, err
	}
	if err := fsafe.WriteAtomic(settingsPath, out, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	return res, nil
}

// HooksInstalled cho biết tệp settings có chứa đủ hook của Bint không.
func HooksInstalled(settingsPath string) (bool, error) {
	raw, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []handler `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, err
	}
	for ev := range desiredGroups("x") {
		found := false
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				if isBintHook(h.Command) {
					found = true
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func normalizeJSON(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// orderedDoc giữ thứ tự các khoá cấp cao nhất của tệp settings để hạn chế xáo trộn.
type orderedDoc struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrdered(raw []byte) (*orderedDoc, error) {
	d := &orderedDoc{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(raw)) == 0 {
		return d, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("tệp settings không phải JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := d.vals[k]; !dup {
			d.keys = append(d.keys, k)
		}
		d.vals[k] = v
	}
	return d, nil
}

func (d *orderedDoc) get(k string) (json.RawMessage, bool) {
	v, ok := d.vals[k]
	return v, ok
}

func (d *orderedDoc) set(k string, v json.RawMessage) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

func (d *orderedDoc) del(k string) {
	if _, ok := d.vals[k]; !ok {
		return
	}
	delete(d.vals, k)
	for i, kk := range d.keys {
		if kk == k {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			break
		}
	}
}

func (d *orderedDoc) marshalIndent() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range d.keys {
		if i > 0 {
			buf.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		buf.WriteString("\n  ")
		buf.Write(kb)
		buf.WriteString(": ")
		var ind bytes.Buffer
		if err := json.Indent(&ind, d.vals[k], "  ", "  "); err != nil {
			return nil, err
		}
		buf.Write(ind.Bytes())
	}
	if len(d.keys) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
