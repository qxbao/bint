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

	"bint/internal/harness/claude"
)

// RunClaudeHook xử lý một lần gọi hook của Claude Code. Không bao giờ trả lỗi
// cho harness: lỗi được ghi vào hook-errors.log trong thư mục trạng thái (hoặc
// stderr nếu chưa xác định được thư mục đó) để hook không chặn agent.
func RunClaudeHook(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, version string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := io.ReadAll(io.LimitReader(stdin, 8<<20))
	if err == nil {
		var res claude.HookResult
		res, err = claude.HandleHook(ctx, bytes.NewReader(data), claude.HookEnv{BintVersion: version})
		if err == nil && res.Output != "" {
			fmt.Fprintln(stdout, res.Output)
		}
	}
	if err == nil {
		return
	}
	var in struct {
		Cwd           string `json:"cwd"`
		HookEventName string `json:"hook_event_name"`
	}
	json.Unmarshal(data, &in)
	line := fmt.Sprintf("%s %s: %v\n", time.Now().UTC().Format(time.RFC3339), in.HookEventName, err)
	dir := in.Cwd
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
