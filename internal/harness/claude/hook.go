// Package claude là adapter cho Claude Code (cấp 2 — Skill + Hook — cùng MCP và
// thực thi theo chương trình qua `claude -p`): cài hook ghi nhận và chèn ngữ cảnh
// theo đường dẫn trước khi agent đọc/sửa tệp, đăng ký MCP server trong .mcp.json.
// Phần xử lý hook dùng chung nằm ở hookcore.
package claude

import (
	"context"
	"io"
	"strings"

	"bint/internal/harness/hookcore"
)

// HarnessName là tên harness trong cấu hình và cơ sở dữ liệu.
const HarnessName = "claude"

// Các hook_event_name của Claude Code mà Bint đăng ký.
const (
	evSessionStart       = "SessionStart"
	evSessionEnd         = "SessionEnd"
	evPostToolUse        = "PostToolUse"
	evPostToolUseFailure = "PostToolUseFailure"
	evPreToolUse         = "PreToolUse"
)

// MCPPrefix là tiền tố tên công cụ MCP của Bint trong Claude Code (server "bint").
const MCPPrefix = "mcp__bint__"

var readTools = map[string]string{"Read": "file_path"}

var editTools = map[string]string{
	"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path",
}

// Classify phân loại công cụ của Claude Code.
func Classify(tool string, input map[string]any) hookcore.ToolInfo {
	if f, ok := editTools[tool]; ok {
		p, _ := input[f].(string)
		return hookcore.ToolInfo{Kind: hookcore.ToolEdit, Paths: nonEmpty(p)}
	}
	if f, ok := readTools[tool]; ok {
		p, _ := input[f].(string)
		return hookcore.ToolInfo{Kind: hookcore.ToolRead, Paths: nonEmpty(p)}
	}
	if tool == "Bash" {
		c, _ := input["command"].(string)
		return hookcore.ToolInfo{Kind: hookcore.ToolShell, Command: c}
	}
	if strings.HasPrefix(tool, MCPPrefix) {
		return hookcore.ToolInfo{Kind: hookcore.ToolMCP, MCPTool: strings.TrimPrefix(tool, MCPPrefix)}
	}
	return hookcore.ToolInfo{}
}

func nonEmpty(p string) []string {
	if p == "" {
		return nil
	}
	return []string{p}
}

// Dialect mô tả Claude Code cho hookcore.
var Dialect = hookcore.Dialect{
	Harness: HarnessName, Classify: Classify, InjectRead: true, InjectEdit: true,
	PreToolOutput: hookcore.ClaudeStylePreToolOutput("ask"),
}

// HookEnv / HookResult giữ tên cũ cho mã gọi.
type (
	HookEnv    = hookcore.Env
	HookResult = hookcore.Result
)

// EnvRunID / EnvApprovedPaths: xem hookcore.
const (
	EnvRunID         = hookcore.EnvRunID
	EnvApprovedPaths = hookcore.EnvApprovedPaths
)

// HandleHook đọc dữ liệu hook Claude Code từ in và xử lý (ghi spool, chèn ngữ cảnh).
func HandleHook(ctx context.Context, in io.Reader, env HookEnv) (HookResult, error) {
	raw, err := io.ReadAll(io.LimitReader(in, 8<<20))
	if err != nil {
		return HookResult{}, err
	}
	hi, err := hookcore.NormalizeClaudeStyle(raw)
	if err != nil {
		return HookResult{}, err
	}
	return hookcore.Handle(ctx, hi, Dialect, env)
}

// RelPath: xem hookcore.RelPath.
func RelPath(root, cwd, p string) (string, bool) { return hookcore.RelPath(root, cwd, p) }
