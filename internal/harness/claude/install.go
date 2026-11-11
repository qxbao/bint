package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/jsondoc"
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

const toolMatcher = "Edit|Write|MultiEdit|NotebookEdit|Bash|Skill|mcp__bint__.*"

// pathMatcher: công cụ được chèn ngữ cảnh theo đường dẫn (PreToolUse).
// Bash: chụp trạng thái working tree trước lệnh để phát hiện tệp sửa qua shell.
const pathMatcher = "Read|Edit|Write|MultiEdit|NotebookEdit|Bash"

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
		evUserPromptSubmit:   group(""),
		// Stop: guardrail on: stop (H1). Hook thoát ngay nếu repository không có guardrail.
		evStop: group(""),
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
	return EditHooks(settingsPath, backupDir, "claude/"+filepath.Base(settingsPath), now, desiredGroups(HookCommand(bintPath)), isBintHook)
}

// UninstallHooks gỡ các hook của Bint khỏi tệp settings.
func UninstallHooks(settingsPath, backupDir string, now time.Time) (InstallResult, error) {
	return EditHooks(settingsPath, backupDir, "claude/"+filepath.Base(settingsPath), now, nil, isBintHook)
}

// EditHooks sửa khoá "hooks" (định dạng của Claude Code, Codex dùng chung) trong
// tệp JSON: gỡ mọi handler mà isOurs nhận là của Bint, rồi thêm các nhóm desired
// (nil = chỉ gỡ). Giữ nguyên mọi khoá và hook khác; có sao lưu trước khi ghi.
func EditHooks(path, backupDir, backupRel string, now time.Time, desired map[string]map[string]any, isOurs func(cmd string) bool) (InstallResult, error) {
	res := InstallResult{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	doc, err := jsondoc.Parse(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	hooks := map[string][]any{}
	if v, ok := doc.Get("hooks"); ok {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(v, &m); err != nil {
			return res, fmt.Errorf("%s: khoá hooks không phải object: %w", path, err)
		}
		for ev, list := range m {
			var groups []any
			if err := json.Unmarshal(list, &groups); err != nil {
				return res, fmt.Errorf("%s: hooks.%s không phải mảng: %w", path, ev, err)
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
				if c, _ := hm["command"].(string); hm != nil && isOurs(c) {
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
	for ev, g := range desired {
		hooks[ev] = append(hooks[ev], g)
	}
	if len(hooks) == 0 {
		doc.Del("hooks")
	} else if err := doc.SetValue("hooks", hooks); err != nil {
		return res, err
	}
	out, err := doc.MarshalIndent()
	if err != nil {
		return res, err
	}
	if jsondoc.Equal(raw, out) {
		return res, nil
	}
	if len(raw) == 0 && desired == nil {
		return res, nil
	}
	if res.BackupPath, err = fsafe.Backup(path, backupDir, backupRel, now); err != nil {
		return res, err
	}
	if err := fsafe.WriteAtomic(path, out, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	return res, nil
}

// HooksInstalled cho biết tệp settings có chứa đủ hook của Bint không.
func HooksInstalled(settingsPath string) (bool, error) {
	return HooksPresent(settingsPath, desiredGroups("x"), isBintHook)
}

// HooksPresent cho biết khoá "hooks" của tệp có handler của Bint cho mọi sự kiện
// trong want.
func HooksPresent(settingsPath string, want map[string]map[string]any, isOurs func(string) bool) (bool, error) {
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
	for ev := range want {
		found := false
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				if isOurs(h.Command) {
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
