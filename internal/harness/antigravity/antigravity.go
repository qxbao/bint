// Package antigravity là adapter cho Antigravity CLI (`agy`). Căn cứ: tài liệu đi
// kèm bản cài (builtin/skills/agy-customizations/docs: hooks.md, skills.md,
// mcp_servers.md) và payload hook bắt được từ agy 1.5.3:
//
//   - Skill: <repo>/.agents/skills/<tên>/SKILL.md (dùng chung với Codex); frontmatter
//     chỉ có name và description — không có cơ chế ẩn skill khỏi agent.
//   - Hook: <repo>/.agents/hooks.json (hoặc ~/.gemini/config/hooks.json), mỗi khoá
//     cấp cao nhất là một hook có tên; Bint dùng khoá "bint". Payload camelCase
//     (conversationId, workspacePaths, toolCall{name,args}, stepIdx) không chứa tên
//     sự kiện, nên lệnh hook nhận tên sự kiện qua đối số.
//   - PreToolUse chỉ trả được decision (allow/deny/ask): KHÔNG chèn được ngữ cảnh
//     theo đường dẫn; vẫn hỏi người được khi sửa đường dẫn được bảo vệ.
//   - Không có sự kiện kết thúc phiên (Stop là kết thúc một lượt), nên Bint không
//     ghi session.ended cho Antigravity.
//   - MCP: chỉ cấu hình được ở cấp người dùng (~/.gemini/config/mcp_config.json, khoá
//     mcpServers) hoặc plugin. Mặc định Bint không ghi cấu hình người dùng — agent dùng CLI
//     `bint` qua skill; `bint init --mcp-user` ghi mục `bint` (UserMCPPath) sau khi người dùng chọn.
//   - Thực thi: `agy --print=<đề bài> --output-format json --mode plan|accept-edits`;
//     báo token, không báo chi phí.
//
// Tên công cụ (write_to_file, replace_file_content, view_file, run_command) và khoá
// đối số (TargetFile, AbsolutePath, CommandLine) lấy theo tài liệu hooks.md và mã
// tích hợp đã đo; chưa bắt được payload công cụ thật từ máy này (hết quota).
package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/jsondoc"
)

// HarnessName là tên harness trong cấu hình và cơ sở dữ liệu.
const HarnessName = "antigravity"

// Binaries là tên chương trình có thể có của Antigravity CLI.
var Binaries = []string{"agy", "antigravity"}

var editTools = map[string]bool{"write_to_file": true, "replace_file_content": true, "multi_replace_file_content": true}
var readTools = map[string]bool{"view_file": true, "view_file_outline": true, "view_code_item": true}
var pathKeys = []string{"TargetFile", "AbsolutePath", "FilePath", "File", "Path", "file_path", "path"}

// Classify phân loại công cụ của Antigravity.
func Classify(tool string, args map[string]any) hookcore.ToolInfo {
	paths := func() []string {
		for _, k := range pathKeys {
			if s, _ := args[k].(string); s != "" {
				return []string{s}
			}
		}
		return nil
	}
	switch {
	case editTools[tool]:
		return hookcore.ToolInfo{Kind: hookcore.ToolEdit, Paths: paths()}
	case readTools[tool]:
		return hookcore.ToolInfo{Kind: hookcore.ToolRead, Paths: paths()}
	case tool == "run_command":
		c, _ := args["CommandLine"].(string)
		if c == "" {
			c, _ = args["command"].(string)
		}
		return hookcore.ToolInfo{Kind: hookcore.ToolShell, Command: c}
	}
	return hookcore.ToolInfo{}
}

// preToolOutput: Antigravity không nhận ngữ cảnh từ PreToolUse (text bị bỏ).
func preToolOutput(_ string, ask string) string {
	if ask == "" {
		return `{"decision":""}`
	}
	b, _ := json.Marshal(map[string]string{"decision": "ask", "reason": ask})
	return string(b)
}

// Dialect mô tả Antigravity cho hookcore.
var Dialect = hookcore.Dialect{
	Harness: HarnessName, Classify: Classify, InjectRead: false, InjectEdit: false, PreToolOutput: preToolOutput,
	// Guardrail (H1): PreToolUse chỉ trả quyết định (deny/ask) — không chèn được ngữ cảnh nên
	// remind bị bỏ; không có sự kiện Stop của phiên.
	PreToolDecisions: []string{"deny", "ask"}, PreToolDecide: preToolDecide,
}

// preToolDecide dựng quyết định tường minh; text (ngữ cảnh) bị bỏ vì Antigravity không nhận.
func preToolDecide(_, decision, reason string) string {
	if decision == "" {
		return `{"decision":""}`
	}
	b, _ := json.Marshal(map[string]string{"decision": decision, "reason": reason})
	return string(b)
}

// Payload là dữ liệu hook Antigravity gửi qua stdin.
type Payload struct {
	ConversationID string          `json:"conversationId"`
	WorkspacePaths json.RawMessage `json:"workspacePaths"`
	ModelName      string          `json:"modelName"`
	ToolCall       *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	} `json:"toolCall"`
	StepIdx json.RawMessage `json:"stepIdx"`
	Error   string          `json:"error"`
}

var events = map[string]string{
	"SessionStart": hookcore.EvSessionStart, "PreToolUse": hookcore.EvPreTool, "PostToolUse": hookcore.EvPostTool,
}

// Normalize chuyển payload Antigravity của sự kiện event về hookcore.Input.
func Normalize(event string, raw []byte) (hookcore.Input, error) {
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return hookcore.Input{}, fmt.Errorf("đọc dữ liệu hook: %w", err)
	}
	in := hookcore.Input{SessionID: p.ConversationID, RawEvent: event, Event: events[event], Model: p.ModelName}
	if in.Event == "" {
		in.Event = "other"
	}
	var ws []string
	if json.Unmarshal(p.WorkspacePaths, &ws) == nil && len(ws) > 0 {
		in.Cwd = ws[0]
	}
	if event == "SessionStart" {
		in.Source = "startup"
	}
	if p.ToolCall != nil {
		in.ToolName, in.ToolInput = p.ToolCall.Name, p.ToolCall.Args
	}
	if step := strings.Trim(string(p.StepIdx), `"`); step != "" {
		if _, err := strconv.Atoi(step); err == nil {
			in.ToolUseID = "step-" + step
		}
	}
	if in.Event == hookcore.EvPostTool && p.Error != "" {
		in.Event = hookcore.EvPostToolFailure
	}
	return in, nil
}

// HandleHook đọc dữ liệu hook Antigravity của sự kiện event từ in và xử lý. Output
// luôn là một JSON object (Antigravity đòi hỏi), kể cả khi Bint bỏ qua sự kiện.
func HandleHook(ctx context.Context, event string, in io.Reader, env hookcore.Env) (hookcore.Result, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 8<<20))
	if err != nil {
		return hookcore.Result{Output: DefaultOutput(event)}, err
	}
	hi, err := Normalize(event, raw)
	if err != nil {
		return hookcore.Result{Output: DefaultOutput(event)}, err
	}
	res, err := hookcore.Handle(ctx, hi, Dialect, env)
	if res.Output == "" {
		res.Output = DefaultOutput(event)
	}
	return res, err
}

// DefaultOutput là đầu ra trung tính cho sự kiện (không thay đổi hành vi agent).
func DefaultOutput(event string) string {
	if event == "PreToolUse" {
		return `{"decision":""}`
	}
	return "{}"
}

// HooksPath trả về hooks.json theo phạm vi: user → ~/.gemini/config/hooks.json, còn
// lại → <repo>/.agents/hooks.json (local: tệp được thêm vào .gitignore).
func HooksPath(scope claude.Scope, repoRoot string) (string, error) {
	if scope == claude.ScopeUser {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini", "config", "hooks.json"), nil
	}
	return filepath.Join(repoRoot, ".agents", "hooks.json"), nil
}

// HookName là khoá của Bint trong hooks.json.
const HookName = "bint"

// HookTimeoutSeconds giới hạn thời gian mỗi lần chạy hook.
const HookTimeoutSeconds = 10

const hookSubcommand = "hook antigravity"

// HookCommand dựng lệnh hook cho sự kiện event.
func HookCommand(bintPath, event string) string {
	if bintPath == "bint" || bintPath == "" {
		return "bint " + hookSubcommand + " " + event
	}
	return `"` + filepath.ToSlash(bintPath) + `" ` + hookSubcommand + " " + event
}

// HookSpec là nội dung khoá "bint" trong hooks.json.
func HookSpec(bintPath string) map[string]any {
	h := func(ev string) map[string]any {
		return map[string]any{"type": "command", "command": HookCommand(bintPath, ev), "timeout": HookTimeoutSeconds}
	}
	return map[string]any{
		"SessionStart": []any{h("SessionStart")},
		"PreToolUse":   []any{map[string]any{"matcher": "write_to_file|replace_file_content|multi_replace_file_content|run_command", "hooks": []any{h("PreToolUse")}}},
		"PostToolUse":  []any{map[string]any{"matcher": "write_to_file|replace_file_content|multi_replace_file_content|run_command", "hooks": []any{h("PostToolUse")}}},
	}
}

// EditHooks đặt (spec khác nil) hoặc gỡ khoá "bint" trong hooks.json, giữ nguyên
// hook khác; có sao lưu trước khi ghi.
func EditHooks(path, backupDir string, spec map[string]any, now time.Time) (claude.InstallResult, error) {
	res := claude.InstallResult{Path: path}
	raw, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	doc, err := jsondoc.Parse(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	if spec == nil {
		if _, ok := doc.Get(HookName); !ok {
			return res, nil
		}
		doc.Del(HookName)
	} else if err := doc.SetValue(HookName, spec); err != nil {
		return res, err
	}
	out, err := doc.MarshalIndent()
	if err != nil {
		return res, err
	}
	if jsondoc.Equal(raw, out) {
		return res, nil
	}
	if res.BackupPath, err = fsafe.Backup(path, backupDir, "antigravity/hooks.json", now); err != nil {
		return res, err
	}
	// Băm của nội dung chính hàm này đã đọc/ghi (claude.InstallResult.Prev/SHA256); xoá tệp thì SHA256 rỗng.
	if existed {
		res.Prev = fsafe.Hash(string(raw))
	}
	if spec == nil && doc.Len() == 0 {
		return res, os.Remove(path)
	}
	if err := fsafe.WriteAtomic(path, out, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	res.SHA256 = fsafe.Hash(string(out))
	return res, nil
}

// InstallHooks / UninstallHooks: xem EditHooks.
func InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error) {
	return EditHooks(path, backupDir, HookSpec(bintPath), now)
}

func UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error) {
	r, err := EditHooks(path, backupDir, nil, now)
	if err == nil && r.BackupPath != "" {
		r.Changed = true
	}
	return r, err
}

// HooksInstalled cho biết hooks.json có khoá "bint" đủ ba sự kiện và đang bật.
func HooksInstalled(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, err
	}
	spec, ok := m[HookName]
	if !ok {
		return false, nil
	}
	if v, ok := spec["enabled"]; ok && string(v) == "false" {
		return false, nil
	}
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse"} {
		if _, ok := spec[ev]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// UserMCPPath là cấu hình MCP cấp người dùng của Antigravity (~/.gemini/config/mcp_config.json).
func UserMCPPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "config", "mcp_config.json"), nil
}
