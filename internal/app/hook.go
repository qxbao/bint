package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"bint/internal/harness/antigravity"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/harness/hookcore"
)

// RunClaudeHook xử lý một lần gọi hook của Claude Code. Không bao giờ trả lỗi
// cho harness: lỗi được ghi vào hook-errors.log trong thư mục trạng thái (hoặc
// stderr nếu chưa xác định được thư mục đó) để hook không chặn agent.
func RunClaudeHook(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, version string) {
	RunHook(ctx, claude.HarnessName, "", stdin, stdout, stderr, version)
}

// RunHook xử lý một lần gọi hook của harness (claude | codex | antigravity). event
// chỉ dùng cho Antigravity (payload không chứa tên sự kiện). Không bao giờ làm gián
// đoạn agent: lỗi được ghi vào hook-errors.log; Antigravity luôn nhận một JSON object.
func RunHook(ctx context.Context, harness, event string, stdin io.Reader, stdout, stderr io.Writer, version string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := io.ReadAll(io.LimitReader(stdin, 8<<20))
	env := hookcore.Env{BintVersion: version}
	var res hookcore.Result
	if err == nil {
		switch harness {
		case codex.HarnessName:
			res, err = codex.HandleHook(ctx, bytes.NewReader(data), env)
		case antigravity.HarnessName:
			res, err = antigravity.HandleHook(ctx, event, bytes.NewReader(data), env)
		default:
			res, err = claude.HandleHook(ctx, bytes.NewReader(data), env)
		}
	}
	if harness == antigravity.HarnessName && res.Output == "" {
		res.Output = antigravity.DefaultOutput(event)
	}
	if res.Output != "" {
		fmt.Fprintln(stdout, res.Output)
	}
	if err == nil {
		return
	}
	var in struct {
		Cwd            string   `json:"cwd"`
		HookEventName  string   `json:"hook_event_name"`
		WorkspacePaths []string `json:"workspacePaths"`
	}
	json.Unmarshal(data, &in)
	name := in.HookEventName
	if name == "" {
		name = event
	}
	line := fmt.Sprintf("%s %s %s: %v\n", time.Now().UTC().Format(time.RFC3339), harness, name, err)
	dir := in.Cwd
	if dir == "" && len(in.WorkspacePaths) > 0 {
		dir = in.WorkspacePaths[0]
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if ws, werr := OpenWorkspace(context.Background(), dir, false); werr == nil && ws.Paths.Initialized() {
		f, ferr := os.OpenFile(filepath.Join(ws.Paths.Root, HookErrorLog), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr == nil {
			defer f.Close()
			if _, ferr = f.WriteString(line); ferr == nil {
				return
			}
		}
	}
	fmt.Fprint(stderr, "bint hook: "+line)
}
