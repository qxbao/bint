// Package codex là adapter cho Codex CLI. Đã đối chiếu với codex-cli 0.167:
//
//   - Skill: đọc từ <repo>/.agents/skills/<tên>/SKILL.md (dùng chung với Antigravity).
//   - Hook: <repo>/.codex/hooks.json (hoặc ~/.codex/hooks.json), cùng khuôn với Claude
//     Code (hook_event_name, tool_name, tool_input, session_id, cwd). Codex không có
//     công cụ đọc tệp riêng (đọc qua shell) nên chỉ chèn ngữ cảnh trước apply_patch;
//     additionalContext của PreToolUse được agent nhận (đã thử). Mỗi hook cần được
//     người dùng duyệt (trust) trong Codex trước khi chạy.
//   - MCP: bảng [mcp_servers.bint] trong <repo>/.codex/config.toml — chỉ có hiệu lực
//     khi dự án được Codex tin cậy (trust_level = "trusted").
//   - Thực thi: `codex exec --json`, đề bài qua stdin; sandbox workspace-write.
//     Codex không báo chi phí (chỉ báo token).
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"context"

	"bint/internal/fsafe"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
)

// HarnessName là tên harness trong cấu hình và cơ sở dữ liệu.
const HarnessName = "codex"

// MCPPrefix: tiền tố tên công cụ MCP của Bint trong payload hook (giả định cùng
// quy ước với Claude Code; chưa thấy Codex gửi hook cho công cụ MCP).
const MCPPrefix = "mcp__bint__"

var patchFileRe = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+?)\s*$|^\*\*\* Move to: (.+?)\s*$`)

// PatchPaths trả về các tệp mà một bản vá apply_patch chạm tới.
func PatchPaths(patch string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range patchFileRe.FindAllStringSubmatch(patch, -1) {
		p := m[1]
		if p == "" {
			p = m[2]
		}
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

var shellTools = map[string]bool{"Bash": true, "shell": true, "exec_command": true, "local_shell": true}

// Classify phân loại công cụ của Codex.
func Classify(tool string, input map[string]any) hookcore.ToolInfo {
	switch {
	case tool == "apply_patch":
		patch, _ := input["command"].(string)
		if patch == "" {
			patch, _ = input["patch"].(string)
		}
		if patch == "" {
			patch, _ = input["input"].(string)
		}
		return hookcore.ToolInfo{Kind: hookcore.ToolEdit, Paths: PatchPaths(patch)}
	case shellTools[tool]:
		return hookcore.ToolInfo{Kind: hookcore.ToolShell, Command: shellCommand(input["command"])}
	case strings.HasPrefix(tool, MCPPrefix):
		return hookcore.ToolInfo{Kind: hookcore.ToolMCP, MCPTool: strings.TrimPrefix(tool, MCPPrefix)}
	}
	return hookcore.ToolInfo{}
}

// shellCommand nhận lệnh dạng chuỗi hoặc argv; argv [sh, -lc, kịch bản] trả về kịch bản.
func shellCommand(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var argv []string
		for _, a := range c {
			argv = append(argv, fmt.Sprint(a))
		}
		if len(argv) == 3 && strings.HasPrefix(argv[1], "-") && strings.Contains(argv[1], "c") {
			switch filepath.Base(argv[0]) {
			case "sh", "bash", "zsh", "dash", "ksh":
				return argv[2]
			}
		}
		return strings.Join(argv, " ")
	}
	return ""
}

// Dialect mô tả Codex cho hookcore. Đường dẫn được bảo vệ: Codex không có cách hỏi
// người từ hook trong `codex exec`, nên hook chặn (deny) kèm lý do; người duyệt
// bằng `bint task approve` (BINT_APPROVED_PATHS) rồi chạy lại.
var Dialect = hookcore.Dialect{
	Harness: HarnessName, Classify: Classify, InjectRead: false, InjectEdit: true,
	PreToolOutput: hookcore.ClaudeStylePreToolOutput("deny"),
}

// HandleHook đọc dữ liệu hook Codex từ in và xử lý.
func HandleHook(ctx context.Context, in io.Reader, env hookcore.Env) (hookcore.Result, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 8<<20))
	if err != nil {
		return hookcore.Result{}, err
	}
	hi, err := hookcore.NormalizeClaudeStyle(raw)
	if err != nil {
		return hookcore.Result{}, err
	}
	return hookcore.Handle(ctx, hi, Dialect, env)
}

// Home trả về thư mục cấu hình người dùng của Codex ($CODEX_HOME hoặc ~/.codex).
func Home() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// HooksPath trả về tệp hooks.json theo phạm vi: user → ~/.codex/hooks.json, còn lại
// → <repo>/.codex/hooks.json (local: tệp được thêm vào .gitignore).
func HooksPath(scope claude.Scope, repoRoot string) (string, error) {
	if scope == claude.ScopeUser {
		h, err := Home()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, "hooks.json"), nil
	}
	return filepath.Join(repoRoot, ".codex", "hooks.json"), nil
}

const hookSubcommand = "hook codex"

// HookCommand dựng lệnh hook từ đường dẫn chương trình bint.
func HookCommand(bintPath string) string {
	if bintPath == "bint" || bintPath == "" {
		return "bint " + hookSubcommand
	}
	return `"` + filepath.ToSlash(bintPath) + `" ` + hookSubcommand
}

func isBintHook(cmd string) bool {
	return strings.Contains(cmd, "bint") && strings.HasSuffix(strings.TrimSpace(cmd), hookSubcommand)
}

// HookTimeoutSeconds giới hạn thời gian mỗi lần chạy hook.
const HookTimeoutSeconds = 10

func desiredGroups(cmd string) map[string]map[string]any {
	h := map[string]any{"type": "command", "command": cmd, "timeout": HookTimeoutSeconds}
	group := func(matcher string) map[string]any {
		g := map[string]any{"hooks": []any{h}}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return g
	}
	return map[string]map[string]any{
		"SessionStart": group(""),
		"SessionEnd":   group(""),
		"PreToolUse":   group("apply_patch|Bash|shell|exec_command"),
		"PostToolUse":  group("apply_patch|Bash|shell|exec_command|mcp__bint__.*"),
	}
}

// InstallHooks thêm (hoặc cập nhật) hook của Bint trong hooks.json, giữ nguyên hook khác.
func InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error) {
	return claude.EditHooks(path, backupDir, "codex/hooks.json", now, desiredGroups(HookCommand(bintPath)), isBintHook)
}

// UninstallHooks gỡ hook của Bint khỏi hooks.json.
func UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error) {
	return claude.EditHooks(path, backupDir, "codex/hooks.json", now, nil, isBintHook)
}

// HooksInstalled cho biết hooks.json có đủ hook của Bint.
func HooksInstalled(path string) (bool, error) {
	return claude.HooksPresent(path, desiredGroups("x"), isBintHook)
}

// HooksTrusted cho biết người dùng đã duyệt (trust) ít nhất một hook trong tệp path
// trong Codex (bảng [hooks.state] của ~/.codex/config.toml). Codex lưu băm nội
// dung: hook đã đổi sau khi duyệt sẽ cần duyệt lại, Bint không kiểm tra được băm.
func HooksTrusted(path string) bool {
	h, err := Home()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(h, "config.toml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), `[hooks.state."`+filepath.ToSlash(path)+`:`) ||
		strings.Contains(string(b), `[hooks.state."`+path+`:`)
}

// ProjectTrusted cho biết repository được Codex tin cậy (cần cho .codex/config.toml
// và .codex/hooks.json cấp dự án).
func ProjectTrusted(repoRoot string) bool {
	h, err := Home()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(h, "config.toml"))
	if err != nil {
		return false
	}
	s := string(b)
	for _, root := range []string{repoRoot, filepath.ToSlash(repoRoot)} {
		i := strings.Index(s, `[projects."`+root+`"]`)
		if i < 0 {
			continue
		}
		rest := s[i+len(root)+13:]
		if j := strings.Index(rest, "\n["); j >= 0 {
			rest = rest[:j]
		}
		if regexp.MustCompile(`(?m)^\s*trust_level\s*=\s*"trusted"`).MatchString(rest) {
			return true
		}
	}
	return false
}

// ConfigPath là tệp cấu hình Codex cấp dự án.
func ConfigPath(repoRoot string) string { return filepath.Join(repoRoot, ".codex", "config.toml") }

// MCPServerName là tên server trong mcp_servers.
const MCPServerName = "bint"

var mcpTableRe = regexp.MustCompile(`(?m)^\s*\[mcp_servers\.(?:bint|"bint")\]`)

// MCPBlock là nội dung khối Bint trong .codex/config.toml.
func MCPBlock(bintPath string) string {
	cmd := bintPath
	if cmd == "" {
		cmd = "bint"
	}
	b, _ := json.Marshal(filepath.ToSlash(cmd))
	return "# Do Bint quản lý (bint init --mcp): MCP server của Context Engine.\n" +
		"[mcp_servers.bint]\ncommand = " + string(b) + "\nargs = [\"mcp\", \"serve\"]"
}

// InstallMCP thêm (install = true) hoặc gỡ khối [mcp_servers.bint] trong
// .codex/config.toml. Bảng trùng tên do người dùng tự khai báo ngoài khối → xung đột.
func InstallMCP(repoRoot, bintPath, backupDir string, m *fsafe.Manifest, install, force bool, now time.Time) (fsafe.Result, error) {
	p := ConfigPath(repoRoot)
	req := fsafe.BlockSync{Path: p, RelName: ".codex/config.toml", Markers: fsafe.HashMarkers, Inner: MCPBlock(bintPath),
		Force: force, BackupDir: backupDir, Manifest: m, Now: now}
	if !install {
		return fsafe.RemoveBlock(req)
	}
	raw, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fsafe.Result{}, err
	}
	if mcpTableRe.MatchString(outsideBlock(string(raw))) {
		return fsafe.Result{Outcome: fsafe.Conflict, Detail: p + " đã có bảng [mcp_servers.bint] ngoài khối bint; gỡ nó hoặc để nguyên (Bint không ghi đè)"}, nil
	}
	return fsafe.SyncBlock(req)
}

// MCPInstalled cho biết .codex/config.toml có bảng [mcp_servers.bint].
func MCPInstalled(repoRoot string) bool {
	b, err := os.ReadFile(ConfigPath(repoRoot))
	return err == nil && mcpTableRe.Match(b)
}

// outsideBlock trả về nội dung tệp không gồm khối bint.
func outsideBlock(s string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case t == fsafe.HashMarkers.Begin:
			in = true
		case t == fsafe.HashMarkers.End:
			in = false
		case !in:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
