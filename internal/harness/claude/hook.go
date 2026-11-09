// Package claude là adapter cấp 2 (Skill + Hook) cho Claude Code: ghi nhận sự kiện
// vòng đời phiên, tệp được sửa, lệnh kiểm thử, lần dùng công cụ MCP của Bint (P0,
// P3), và chèn ngữ cảnh theo đường dẫn trước khi agent đọc/sửa tệp (P3).
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
	"regexp"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/detect"
	"bint/internal/gitrepo"
	"bint/internal/knowledge"
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
	evPreToolUse         = "PreToolUse"
)

// pathTools là các công cụ đọc/sửa tệp được chèn ngữ cảnh theo đường dẫn.
var pathTools = map[string]string{
	"Read": "file_path", "Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path",
}

// MCPPrefix là tiền tố tên công cụ MCP của Bint trong Claude Code (server "bint").
const MCPPrefix = "mcp__bint__"

var knowledgeRefRe = regexp.MustCompile(`\[k:([0-9A-HJKMNP-TV-Z]{26})\]`)
var itemIDRe = regexp.MustCompile(`item_id: ([0-9A-HJKMNP-TV-Z]{26})`)

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
	ToolResponse  json.RawMessage `json:"tool_response"`
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
	// Output là JSON in ra stdout cho Claude Code (chèn ngữ cảnh), rỗng nếu không có.
	Output string
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
	if p := os.Getenv(ctxengine.EvalEnv); p != "" {
		return handleEvalHook(hi, cwd, p)
	}
	repo, err := gitrepo.Discover(ctx, cwd)
	if errors.Is(err, gitrepo.ErrNotRepo) {
		return HookResult{Skipped: "không phải repository Git"}, nil
	}
	if err != nil {
		return HookResult{}, err
	}
	cfg, cfgSrc, err := config.LoadFirst(repo.ConfigRoots(), false)
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
	case evPreToolUse:
		if !cfg.Context.Interfaces.PathHooks || cfgSrc.Project == "" {
			return HookResult{Skipped: "context.interfaces.path_hooks tắt"}, nil
		}
		field, ok := pathTools[hi.ToolName]
		if !ok {
			return HookResult{Skipped: "công cụ không đọc/sửa tệp"}, nil
		}
		p, _ := hi.ToolInput[field].(string)
		rel, inside := RelPath(repo.Toplevel, cwd, p)
		if !inside {
			return HookResult{Skipped: "tệp nằm ngoài repository"}, nil
		}
		kroot := filepath.Dir(filepath.Dir(cfgSrc.Project))
		mem := ctxengine.NewSessionMemory(paths.Runtime, HarnessName, hi.SessionID)
		pack, ok := injectPaths(time.Duration(cfg.Context.PathHookTimeoutMS)*time.Millisecond, func() (ctxengine.Pack, error) {
			items, _, err := knowledge.NewStore(kroot).List()
			if err != nil {
				return ctxengine.Pack{}, err
			}
			e := &ctxengine.Engine{Items: items, Src: &knowledge.WorktreeSource{Root: kroot},
				MaxTokens: cfg.Context.MaxTokens, PathMaxItems: cfg.Context.PathMaxItems}
			return e.ForPaths([]string{rel}, mem.Seen()), nil
		})
		if !ok {
			return HookResult{Skipped: "quá hạn hoặc lỗi khi truy xuất theo đường dẫn"}, nil
		}
		if len(pack.Entries) == 0 {
			return HookResult{Skipped: "không có tri thức khớp " + rel}, nil
		}
		mem.Add(pack.IDs())
		key := hi.ToolUseID
		if key == "" {
			key = now.Format(time.RFC3339Nano)
		}
		cev := ctxengine.ContextEvent(HarnessName, hi.SessionID, repo.Toplevel, ctxengine.ChannelHookPath, key, pack, now)
		cev.Source = "hook:" + HarnessName
		f, err := spool.Write(paths.Spool, cev)
		if err != nil {
			return HookResult{}, err
		}
		return HookResult{Written: true, SpoolFile: f, Output: preToolOutput(pack.Render())}, nil
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
		if strings.HasPrefix(hi.ToolName, MCPPrefix) {
			if !ok {
				return HookResult{Skipped: "công cụ MCP của Bint lỗi"}, nil
			}
			if skip := mcpEvent(&ev, hi); skip != "" {
				return HookResult{Skipped: skip}, nil
			}
		} else if field, isEdit := editTools[hi.ToolName]; isEdit {
			ev.Type = spool.FileModified
			p, _ := hi.ToolInput[field].(string)
			rel, inside := RelPath(repo.Toplevel, cwd, p)
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
			// Lệnh đã được người xác nhận (eval.test_command) cho tín hiệu kết quả mạnh (mục 9.6).
			if tc := cfg.Eval.TestCommand; tc != nil && strings.TrimSpace(*tc) != "" &&
				strings.Contains(strings.TrimSpace(cmd), strings.TrimSpace(*tc)) {
				ev.Payload["confirmed"] = true
			}
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

// mcpEvent điền sự kiện cho lần gọi công cụ MCP của Bint (mục 9.7): truy xuất,
// trích dẫn, quan sát. ID được lấy từ đầu ra của server (đã kiểm tra tồn tại).
func mcpEvent(ev *spool.Event, hi HookInput) string {
	tool := strings.TrimPrefix(hi.ToolName, MCPPrefix)
	resp := string(hi.ToolResponse)
	refs := func() []any {
		seen := map[string]bool{}
		var out []any
		for _, m := range knowledgeRefRe.FindAllStringSubmatch(resp, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
		return out
	}
	switch tool {
	case "context_for_task", "context_for_paths":
		ids := refs()
		if len(ids) == 0 {
			return "truy xuất MCP không trả mục nào"
		}
		ev.Type = spool.ContextRetrieved
		ev.Payload["channel"] = map[string]string{"context_for_task": ctxengine.ChannelMCPTask, "context_for_paths": ctxengine.ChannelMCPPaths}[tool]
		ev.Payload["ids"] = ids
		if t, _ := hi.ToolInput["task"].(string); t != "" {
			ev.Payload["query"] = detect.Redact(t, 300)
		}
	case "cite_knowledge":
		ids := refs()
		if len(ids) == 0 {
			return "trích dẫn không hợp lệ"
		}
		ev.Type = spool.KnowledgeCited
		ev.Payload["ids"] = ids
		if n, _ := hi.ToolInput["note"].(string); n != "" {
			ev.Payload["note"] = detect.Redact(n, 300)
		}
	case "report_observation":
		ev.Type = spool.ObservationReported
		for _, k := range []string{"kind", "title", "body"} {
			if v, _ := hi.ToolInput[k].(string); v != "" {
				ev.Payload[k] = detect.Redact(v, 2000)
			}
		}
		if ps, ok := hi.ToolInput["paths"].([]any); ok {
			ev.Payload["paths"] = ps
		}
		if m := itemIDRe.FindStringSubmatch(resp); m != nil {
			ev.Payload["item_id"] = m[1]
		}
	default:
		return "công cụ MCP chỉ đọc, không ghi nhận"
	}
	return ""
}

// preToolOutput dựng JSON để Claude Code chèn ngữ cảnh vào lần gọi công cụ.
func preToolOutput(text string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": evPreToolUse, "additionalContext": text,
	}})
	return string(b)
}

// injectPaths chạy truy xuất theo đường dẫn trong giới hạn thời gian; quá hạn thì
// bỏ qua để không chặn agent (mục 13.1).
func injectPaths(d time.Duration, f func() (ctxengine.Pack, error)) (ctxengine.Pack, bool) {
	if d <= 0 {
		d = 1500 * time.Millisecond
	}
	type res struct {
		p   ctxengine.Pack
		err error
	}
	ch := make(chan res, 1)
	go func() {
		p, err := f()
		ch <- res{p, err}
	}()
	select {
	case r := <-ch:
		return r.p, r.err == nil
	case <-time.After(d):
		return ctxengine.Pack{}, false
	}
}

// handleEvalHook phục vụ lượt đánh giá nhánh bint: chỉ chèn theo đường dẫn, ghi
// nhật ký vào tệp của lượt (không có thư mục trạng thái).
func handleEvalHook(hi HookInput, cwd, cfgPath string) (HookResult, error) {
	if hi.HookEventName != evPreToolUse {
		return HookResult{Skipped: "chế độ đánh giá: chỉ chèn theo đường dẫn"}, nil
	}
	field, ok := pathTools[hi.ToolName]
	if !ok {
		return HookResult{Skipped: "công cụ không đọc/sửa tệp"}, nil
	}
	ec, err := ctxengine.LoadEvalConfig(cfgPath)
	if err != nil {
		return HookResult{}, err
	}
	p, _ := hi.ToolInput[field].(string)
	rel, inside := RelPath(ec.Root, cwd, p)
	if !inside {
		return HookResult{Skipped: "tệp nằm ngoài thư mục làm việc"}, nil
	}
	mem := ctxengine.NewSessionMemory(filepath.Dir(ec.Log), HarnessName, hi.SessionID)
	pack, ok := injectPaths(3*time.Second, func() (ctxengine.Pack, error) {
		items, _, err := knowledge.Store{Dir: ec.ItemsDir}.List()
		if err != nil {
			return ctxengine.Pack{}, err
		}
		e := &ctxengine.Engine{Items: items, Src: &knowledge.WorktreeSource{Root: ec.Root}, MaxTokens: ec.MaxTokens,
			PathMaxItems: ec.PathMaxItems, ExcludeEvalCase: ec.ExcludeEvalCase, PathIncludeCandidates: ec.IncludeCandidates}
		return e.ForPaths([]string{rel}, mem.Seen()), nil
	})
	if !ok || len(pack.Entries) == 0 {
		return HookResult{Skipped: "không có tri thức khớp"}, nil
	}
	mem.Add(pack.IDs())
	ctxengine.AppendLog(ec.Log, ctxengine.PackLog(ctxengine.ChannelHookPath, pack))
	return HookResult{Output: preToolOutput(pack.Render())}, nil
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

// RelPath trả về đường dẫn tương đối (dấu '/') của p so với root; p tương đối
// được hiểu theo cwd. inside=false nếu p nằm ngoài root.
func RelPath(root, cwd, p string) (string, bool) {
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
