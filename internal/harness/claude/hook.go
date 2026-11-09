// Package claude là adapter cấp 2 (Skill + Hook) cho Claude Code ở P0: chỉ ghi
// nhận sự kiện vòng đời phiên, tệp được sửa và lệnh kiểm thử.
package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/gitrepo"
	"bint/internal/spool"
	"bint/internal/state"
)

// HarnessName là tên harness trong cấu hình và cơ sở dữ liệu.
const HarnessName = "claude"

// Các hook_event_name của Claude Code mà Bint đăng ký.
const (
	evSessionStart       = "SessionStart"
	evSessionEnd         = "SessionEnd"
	evPostToolUse        = "PostToolUse"
	evPostToolUseFailure = "PostToolUseFailure"
)

var editTools = map[string]string{
	"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path",
}

// HookInput là phần dữ liệu Claude Code gửi qua stdin mà Bint dùng.
type HookInput struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	Source        string          `json:"source"`
	Reason        string          `json:"reason"`
	Model         json.RawMessage `json:"model"`
	ToolName      string          `json:"tool_name"`
	ToolInput     map[string]any  `json:"tool_input"`
	ToolUseID     string          `json:"tool_use_id"`
	AgentID       string          `json:"agent_id"`
	AgentType     string          `json:"agent_type"`
}

// HookEnv cho phép kiểm thử thay thế đồng hồ và thư mục hiện tại.
type HookEnv struct {
	BintVersion string
	Now         func() time.Time
	Getwd       func() (string, error)
}

// HookResult mô tả việc hook đã làm (để kiểm thử và chẩn đoán).
type HookResult struct {
	Written   bool
	SpoolFile string
	Skipped   string // lý do bỏ qua, nếu có
}

// HandleHook đọc dữ liệu hook từ in và ghi tối đa một sự kiện vào spool.
// Không gọi mô hình, không mở SQLite. Bỏ qua yên lặng khi repository chưa
// được khởi tạo Bint hoặc sự kiện không thuộc phạm vi ghi nhận.
func HandleHook(ctx context.Context, in io.Reader, env HookEnv) (HookResult, error) {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Getwd == nil {
		env.Getwd = os.Getwd
	}
	var hi HookInput
	if err := json.NewDecoder(io.LimitReader(in, 8<<20)).Decode(&hi); err != nil {
		return HookResult{}, fmt.Errorf("đọc dữ liệu hook: %w", err)
	}
	if hi.SessionID == "" || hi.HookEventName == "" {
		return HookResult{Skipped: "thiếu session_id hoặc hook_event_name"}, nil
	}
	cwd := hi.Cwd
	if cwd == "" {
		var err error
		if cwd, err = env.Getwd(); err != nil {
			return HookResult{}, err
		}
	}
	repo, err := gitrepo.Discover(ctx, cwd)
	if errors.Is(err, gitrepo.ErrNotRepo) {
		return HookResult{Skipped: "không phải repository Git"}, nil
	}
	if err != nil {
		return HookResult{}, err
	}
	cfg, _, err := config.LoadFirst(repo.ConfigRoots(), false)
	if err != nil {
		return HookResult{}, err
	}
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return HookResult{}, err
	}
	if !paths.Initialized() {
		return HookResult{Skipped: "repository chưa chạy bint init"}, nil
	}

	now := env.Now().UTC()
	ev := spool.Event{
		SchemaVersion: spool.SchemaVersion,
		Source:        "hook:" + HarnessName,
		Timestamp:     now,
		Harness:       HarnessName,
		SessionID:     hi.SessionID,
		Worktree:      repo.Toplevel,
		Payload:       map[string]any{},
	}
	if hi.AgentType != "" {
		ev.Payload["agent_type"] = hi.AgentType
	}

	switch hi.HookEventName {
	case evSessionStart:
		ev.Type = spool.SessionStarted
		ev.Payload["source"] = hi.Source
		ev.Payload["bint_version"] = env.BintVersion
		if m := modelName(hi.Model); m != "" {
			ev.Payload["model"] = m
		}
		if h := gitrepo.HeadCommit(ctx, repo.Toplevel); h != "" {
			ev.Payload["git_head"] = h
		}
		if b := gitrepo.Branch(ctx, repo.Toplevel); b != "" {
			ev.Payload["git_branch"] = b
		}
		ev.Payload["linked_worktree"] = repo.IsLinkedWorktree()
		// Hai lần gọi trùng (hook cài ở hai phạm vi) rơi vào cùng một giây.
		ev.EventID = eventID(hi.SessionID, hi.HookEventName, hi.Source, now.Truncate(time.Second).Format(time.RFC3339))
	case evSessionEnd:
		ev.Type = spool.SessionEnded
		ev.Payload["reason"] = hi.Reason
		ev.EventID = eventID(hi.SessionID, hi.HookEventName, hi.Reason, now.Truncate(time.Second).Format(time.RFC3339))
	case evPostToolUse, evPostToolUseFailure:
		ok := hi.HookEventName == evPostToolUse
		ev.Payload["ok"] = ok
		ev.Payload["tool"] = hi.ToolName
		key := hi.ToolUseID
		if key == "" {
			key = now.Format(time.RFC3339Nano)
		}
		ev.EventID = eventID(hi.SessionID, hi.HookEventName, key)
		if field, isEdit := editTools[hi.ToolName]; isEdit {
			ev.Type = spool.FileModified
			p, _ := hi.ToolInput[field].(string)
			rel, inside := relPath(repo.Toplevel, cwd, p)
			if !inside {
				return HookResult{Skipped: "tệp nằm ngoài repository"}, nil
			}
			ev.Payload["path"] = rel
		} else if hi.ToolName == "Bash" {
			cmd, _ := hi.ToolInput["command"].(string)
			var known []string
			for _, c := range []*string{cfg.Eval.TestCommand, cfg.Eval.SuggestedTestCommand} {
				if c != nil {
					known = append(known, *c)
				}
			}
			if !detect.IsTestCommand(cmd, known...) {
				return HookResult{Skipped: "lệnh Bash không phải kiểm thử"}, nil
			}
			ev.Type = spool.TestCompleted
			ev.Payload["command"] = detect.Redact(cmd, 300)
		} else {
			return HookResult{Skipped: "công cụ không thuộc phạm vi ghi nhận"}, nil
		}
	default:
		return HookResult{Skipped: "sự kiện không thuộc phạm vi ghi nhận: " + hi.HookEventName}, nil
	}

	f, err := spool.Write(paths.Spool, ev)
	if err != nil {
		return HookResult{}, err
	}
	return HookResult{Written: true, SpoolFile: f}, nil
}

func eventID(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(HarnessName))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return "ev_" + hex.EncodeToString(h.Sum(nil))[:32]
}

// modelName chấp nhận model dạng chuỗi hoặc đối tượng {id, display_name}.
func modelName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.ID != "" {
			return o.ID
		}
		return o.DisplayName
	}
	return ""
}

// relPath trả về đường dẫn tương đối (dấu '/') của p so với root; p tương đối
// được hiểu theo cwd. inside=false nếu p nằm ngoài root.
func relPath(root, cwd, p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	rel, err := filepath.Rel(state.NormalizePath(root), state.NormalizePath(p))
	if err != nil || outside(rel) {
		return "", false
	}
	// NormalizePath hạ chữ thường trên Windows/macOS; lấy lại cách viết gốc nếu được.
	if r2, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p)); err == nil && !outside(r2) {
		rel = r2
	}
	return filepath.ToSlash(rel), true
}

func outside(rel string) bool {
	r := filepath.ToSlash(rel)
	return r == ".." || strings.HasPrefix(r, "../") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != ""
}
