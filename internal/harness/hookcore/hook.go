// Package hookcore là phần dùng chung của hook ghi nhận cho mọi harness (mục 13):
// ghi sự kiện vòng đời phiên, tệp được sửa, lệnh kiểm thử, lần dùng công cụ MCP
// của Bint vào spool; chèn ngữ cảnh theo đường dẫn trước khi agent đọc/sửa tệp; hỏi
// người khi sửa đường dẫn được bảo vệ. Mỗi harness mô tả khác biệt của nó (tên
// công cụ, cách lấy đường dẫn, định dạng đầu ra) bằng một Dialect; dữ liệu đầu vào
// được chuẩn hoá về Input trước khi xử lý.
//
// Hook không gọi mô hình, không mở SQLite, và không bao giờ chặn agent vì lỗi của
// chính nó. Sự kiện harness không cung cấp thì không được tự tổng hợp giả (mục 13.2).
package hookcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"bint/internal/pathglob"
	"bint/internal/spool"
	"bint/internal/state"
)

// Loại sự kiện đã chuẩn hoá.
const (
	EvSessionStart    = "session_start"
	EvSessionEnd      = "session_end"
	EvPreTool         = "pre_tool"
	EvPostTool        = "post_tool"
	EvPostToolFailure = "post_tool_failure"
	// EvPrompt: người dùng gửi yêu cầu (UserPromptSubmit); hook chèn ngữ cảnh theo đề bài.
	EvPrompt = "prompt"
)

// Input là dữ liệu hook đã chuẩn hoá.
type Input struct {
	SessionID string
	Cwd       string
	// Event là loại đã chuẩn hoá (Ev*); RawEvent là tên gốc của harness (dùng làm
	// một phần event_id để giữ ổn định với dữ liệu cũ).
	Event        string
	RawEvent     string
	Source       string
	Reason       string
	Model        string
	ToolName     string
	ToolInput    map[string]any
	ToolUseID    string
	ToolResponse string
	AgentType    string
	// Prompt: nội dung yêu cầu (EvPrompt).
	Prompt string
	// TranscriptPath: tệp transcript của phiên (nếu harness cung cấp ở SessionEnd).
	TranscriptPath string
}

// ToolKind phân loại một lần gọi công cụ.
type ToolKind int

const (
	ToolOther ToolKind = iota
	ToolRead
	ToolEdit
	ToolShell
	ToolMCP
	ToolSkill
)

// ToolInfo mô tả một lần gọi công cụ theo cách hiểu của Bint.
type ToolInfo struct {
	Skill string // ToolSkill: tên skill được kích hoạt
	Kind  ToolKind
	// Paths: tệp được đọc/sửa (tương đối theo cwd hoặc tuyệt đối).
	Paths []string
	// Command: lệnh shell (ToolShell).
	Command string
	// MCPTool: tên công cụ MCP của Bint, không có tiền tố (ToolMCP).
	MCPTool string
}

// Dialect mô tả khác biệt của một harness.
type Dialect struct {
	Harness string
	// Classify phân loại lần gọi công cụ.
	Classify func(tool string, input map[string]any) ToolInfo
	// InjectRead / InjectEdit: harness nhận ngữ cảnh chèn trước công cụ đọc / sửa tệp.
	InjectRead, InjectEdit bool
	// PreToolOutput dựng đầu ra cho sự kiện trước công cụ: text là ngữ cảnh chèn
	// (rỗng nếu không có), ask là lý do cần người duyệt (rỗng nếu không cần).
	PreToolOutput func(text, ask string) string
	// ProtectedPathHelp bổ sung cách tiếp tục riêng của harness khi cần duyệt.
	ProtectedPathHelp func(paths []string) string
	// PostToolOutcome xác minh kết quả riêng của harness. nil giữ nguyên hợp
	// đồng Claude; known=false ghi test command nhưng không tạo tín hiệu pass/fail.
	PostToolOutcome func(Input, ToolInfo) (success, known bool)
	IsTestCommand   func(string, ...string) bool
	// PromptOutput dựng đầu ra chèn ngữ cảnh cho sự kiện yêu cầu (EvPrompt); nil = harness
	// không hỗ trợ chèn lúc nhận yêu cầu.
	PromptOutput func(text string) string
	// Usage đọc token của phiên từ transcript (nil nếu harness không hỗ trợ). Giá trị nil
	// nghĩa là không biết, không phải 0 (mục 9.2).
	Usage func(transcriptPath string) (in, out *int64)
}

// Env cho phép kiểm thử thay thế đồng hồ và thư mục hiện tại.
type Env struct {
	BintVersion string
	Now         func() time.Time
	Getwd       func() (string, error)
}

// Result mô tả việc hook đã làm (để kiểm thử và chẩn đoán).
type Result struct {
	Written   bool
	SpoolFile string
	Skipped   string // lý do bỏ qua, nếu có
	// Output là dữ liệu in ra stdout cho harness (chèn ngữ cảnh / hỏi người), rỗng nếu không có.
	Output string
}

// EnvRunID: lượt chạy do `bint run` cấp cho tiến trình agent; hook gắn sự kiện vào đó.
const EnvRunID = "BINT_RUN_ID"

// EnvApprovedPaths: tệp được bảo vệ mà người đã duyệt cho tác vụ đang điều phối
// (mỗi dòng một đường dẫn tương đối); hook không hỏi lại các tệp này.
const EnvApprovedPaths = "BINT_APPROVED_PATHS"

var knowledgeRefRe = regexp.MustCompile(`\[k:([0-9A-HJKMNP-TV-Z]{26})\]`)
var itemIDRe = regexp.MustCompile(`item_id: ([0-9A-HJKMNP-TV-Z]{26})`)

// Handle xử lý một sự kiện hook đã chuẩn hoá và ghi tối đa một sự kiện vào spool.
// Bỏ qua yên lặng khi repository chưa được khởi tạo Bint hoặc sự kiện không thuộc
// phạm vi ghi nhận.
func Handle(ctx context.Context, hi Input, d Dialect, env Env) (Result, error) {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Getwd == nil {
		env.Getwd = os.Getwd
	}
	if hi.SessionID == "" || hi.Event == "" {
		return Result{Skipped: "thiếu mã phiên hoặc loại sự kiện"}, nil
	}
	cwd := hi.Cwd
	if cwd == "" {
		var err error
		if cwd, err = env.Getwd(); err != nil {
			return Result{}, err
		}
	}
	if p := os.Getenv(ctxengine.EvalEnv); p != "" {
		return handleEval(hi, d, cwd, p)
	}
	repo, err := gitrepo.Discover(ctx, cwd)
	if errors.Is(err, gitrepo.ErrNotRepo) {
		return Result{Skipped: "không phải repository Git"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	cfg, cfgSrc, err := config.LoadFirst(repo.ConfigRoots(), false)
	if err != nil {
		return Result{}, err
	}
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return Result{}, err
	}
	if !paths.Initialized() {
		return Result{Skipped: "repository chưa chạy bint init"}, nil
	}

	now := env.Now().UTC()
	h := d.Harness
	ev := spool.Event{
		SchemaVersion: spool.SchemaVersion,
		Source:        "hook:" + h,
		Timestamp:     now,
		Harness:       h,
		SessionID:     hi.SessionID,
		Worktree:      repo.Toplevel,
		Payload:       map[string]any{},
	}
	if hi.AgentType != "" {
		ev.Payload["agent_type"] = hi.AgentType
	}
	// Lượt chạy do `bint run` điều phối: sự kiện thuộc về lượt đó (mục 9.2).
	if id := strings.TrimSpace(os.Getenv(EnvRunID)); id != "" {
		ev.RunID = id
	}
	toolKey := func() string {
		if hi.ToolUseID != "" {
			return hi.ToolUseID
		}
		return now.Format(time.RFC3339Nano)
	}

	switch hi.Event {
	case EvPrompt:
		if d.PromptOutput == nil || !cfg.Context.Interfaces.PathHooks || cfgSrc.Project == "" || strings.TrimSpace(hi.Prompt) == "" {
			return Result{Skipped: "không chèn ngữ cảnh theo đề bài"}, nil
		}
		kroot := filepath.Dir(filepath.Dir(cfgSrc.Project))
		mem := ctxengine.NewSessionMemory(paths.Runtime, h, hi.SessionID)
		pack, ok := injectPaths(time.Duration(cfg.Context.PathHookTimeoutMS)*time.Millisecond, func() (ctxengine.Pack, error) {
			items, _, err := knowledge.NewStore(kroot).List()
			if err != nil {
				return ctxengine.Pack{}, err
			}
			e := &ctxengine.Engine{Items: items, Src: &knowledge.WorktreeSource{Root: kroot},
				MaxTokens: cfg.Context.MaxTokens, PathMaxItems: cfg.Context.PathMaxItems,
				LessonGlobalK: cfg.Context.Lessons.GlobalK, LessonMaxTokens: cfg.Context.Lessons.MaxTokens}
			return e.ForTask(ctx, hi.Prompt, nil)
		})
		if !ok || len(pack.Entries) == 0 && len(pack.Lessons) == 0 {
			return Result{Skipped: "không có tri thức khớp đề bài"}, nil
		}
		mem.Add(pack.IDs())
		cev := ctxengine.ContextEvent(h, hi.SessionID, repo.Toplevel, ctxengine.ChannelHookTask, toolKey(), pack, now)
		cev.Source = "hook:" + h
		cev.RunID = ev.RunID
		f, err := spool.Write(paths.Spool, cev)
		if err != nil {
			return Result{}, err
		}
		return Result{Written: true, SpoolFile: f, Output: d.PromptOutput(pack.Render())}, nil
	case EvPreTool:
		info := d.Classify(hi.ToolName, hi.ToolInput)
		if info.Kind == ToolShell {
			// Ảnh chụp working tree trước lệnh shell, để phát hiện tệp lệnh đó sửa (PostTool).
			sctx, cancel := context.WithTimeout(ctx, snapTimeout(cfg.Context.PathHookTimeoutMS))
			defer cancel()
			if snap, ok := takeSnapshot(sctx, repo.Toplevel, nil); ok {
				saveSnapshot(paths.Runtime, h, hi.SessionID, hi.ToolUseID, snap)
			}
			return Result{Skipped: "ảnh chụp trước lệnh shell"}, nil
		}
		if info.Kind != ToolRead && info.Kind != ToolEdit {
			return Result{Skipped: "công cụ không đọc/sửa tệp"}, nil
		}
		var rels []string
		for _, p := range info.Paths {
			if rel, inside := RelPath(repo.Toplevel, cwd, p); inside {
				rels = append(rels, rel)
			}
		}
		if len(rels) == 0 {
			return Result{Skipped: "tệp nằm ngoài repository"}, nil
		}
		// Tệp được đọc (P6, mục 10.2): dữ liệu cho phát hiện khám phá lặp lại. Ghi trước
		// mọi nhánh bên dưới; lỗi ghi không chặn agent.
		if info.Kind == ToolRead {
			rev := ev
			rev.Type = spool.FileRead
			rev.Payload = map[string]any{"paths": rels, "tool": hi.ToolName}
			rev.EventID = EventID(h, hi.SessionID, hi.RawEvent, toolKey(), "read")
			spool.Write(paths.Spool, rev)
		}
		// Giới hạn vận hành (mục 5): sửa đường dẫn được bảo vệ cần người duyệt. Hook chỉ
		// yêu cầu harness hỏi người dùng, không tự quyết.
		ask := ""
		if info.Kind == ToolEdit && cfgSrc.Project != "" {
			var hit []string
			for _, rel := range rels {
				if pathglob.MatchAny(cfg.Limits.ProtectedPaths, rel) && !approvedPath(rel) {
					hit = append(hit, rel)
				}
			}
			if len(hit) > 0 {
				ask = fmt.Sprintf("Bint: %s thuộc đường dẫn được bảo vệ (limits.protected_paths trong .bint/config.yml); thay đổi cần người dùng duyệt.", strings.Join(hit, ", "))
				if d.ProtectedPathHelp != nil {
					ask += d.ProtectedPathHelp(hit)
				}
			}
		}
		inject := (info.Kind == ToolRead && d.InjectRead) || (info.Kind == ToolEdit && d.InjectEdit)
		if !inject || !cfg.Context.Interfaces.PathHooks || cfgSrc.Project == "" {
			if ask != "" {
				return Result{Output: d.PreToolOutput("", ask)}, nil
			}
			if !inject {
				return Result{Skipped: "harness không nhận ngữ cảnh chèn cho công cụ này"}, nil
			}
			return Result{Skipped: "context.interfaces.path_hooks tắt"}, nil
		}
		kroot := filepath.Dir(filepath.Dir(cfgSrc.Project))
		mem := ctxengine.NewSessionMemory(paths.Runtime, h, hi.SessionID)
		pack, ok := injectPaths(time.Duration(cfg.Context.PathHookTimeoutMS)*time.Millisecond, func() (ctxengine.Pack, error) {
			items, _, err := knowledge.NewStore(kroot).List()
			if err != nil {
				return ctxengine.Pack{}, err
			}
			e := &ctxengine.Engine{Items: items, Src: &knowledge.WorktreeSource{Root: kroot},
				MaxTokens: cfg.Context.MaxTokens, PathMaxItems: cfg.Context.PathMaxItems,
				LessonGlobalK: cfg.Context.Lessons.GlobalK, LessonMaxTokens: cfg.Context.Lessons.MaxTokens}
			return e.ForPaths(rels, mem.Seen()), nil
		})
		if !ok || len(pack.Entries) == 0 && len(pack.Lessons) == 0 {
			if ask != "" {
				return Result{Output: d.PreToolOutput("", ask)}, nil
			}
			if !ok {
				return Result{Skipped: "quá hạn hoặc lỗi khi truy xuất theo đường dẫn"}, nil
			}
			return Result{Skipped: "không có tri thức khớp " + strings.Join(rels, ", ")}, nil
		}
		mem.Add(pack.IDs())
		cev := ctxengine.ContextEvent(h, hi.SessionID, repo.Toplevel, ctxengine.ChannelHookPath, toolKey(), pack, now)
		cev.Source = "hook:" + h
		cev.RunID = ev.RunID
		f, err := spool.Write(paths.Spool, cev)
		if err != nil {
			return Result{}, err
		}
		return Result{Written: true, SpoolFile: f, Output: d.PreToolOutput(pack.Render(), ask)}, nil
	case EvSessionStart:
		ev.Type = spool.SessionStarted
		ev.Payload["source"] = hi.Source
		ev.Payload["bint_version"] = env.BintVersion
		if hi.Model != "" {
			ev.Payload["model"] = hi.Model
		}
		if g := gitrepo.HeadCommit(ctx, repo.Toplevel); g != "" {
			ev.Payload["git_head"] = g
		}
		if b := gitrepo.Branch(ctx, repo.Toplevel); b != "" {
			ev.Payload["git_branch"] = b
		}
		ev.Payload["linked_worktree"] = repo.IsLinkedWorktree()
		// Hai lần gọi trùng (hook cài ở hai phạm vi) rơi vào cùng một giây.
		ev.EventID = EventID(h, hi.SessionID, hi.RawEvent, hi.Source, now.Truncate(time.Second).Format(time.RFC3339))
	case EvSessionEnd:
		ev.Type = spool.SessionEnded
		ev.Payload["reason"] = hi.Reason
		if d.Usage != nil && hi.TranscriptPath != "" {
			in, out := d.Usage(hi.TranscriptPath)
			if in != nil {
				ev.Payload["input_tokens"] = *in
			}
			if out != nil {
				ev.Payload["output_tokens"] = *out
			}
			// Không có bảng giá trong mã: cost_usd để trống (NULL), không suy từ token.
		}
		ev.EventID = EventID(h, hi.SessionID, hi.RawEvent, hi.Reason, now.Truncate(time.Second).Format(time.RFC3339))
	case EvPostTool, EvPostToolFailure:
		ok := hi.Event == EvPostTool
		known := true
		info := d.Classify(hi.ToolName, hi.ToolInput)
		if d.PostToolOutcome != nil {
			ok, known = d.PostToolOutcome(hi, info)
		}
		if known {
			ev.Payload["ok"] = ok
		}
		ev.Payload["tool"] = hi.ToolName
		ev.EventID = EventID(h, hi.SessionID, hi.RawEvent, toolKey())
		switch info.Kind {
		case ToolMCP:
			if !ok {
				return Result{Skipped: "công cụ MCP của Bint lỗi"}, nil
			}
			if skip := mcpEvent(&ev, info.MCPTool, hi); skip != "" {
				return Result{Skipped: skip}, nil
			}
		case ToolSkill:
			if !ok || info.Skill == "" {
				return Result{Skipped: "lần gọi skill không thành công"}, nil
			}
			ev.Type = spool.SkillActivated
			ev.Payload["skill"] = info.Skill
			ev.EventID = EventID(h, hi.SessionID, hi.RawEvent, toolKey(), info.Skill)
		case ToolEdit:
			var rels []string
			for _, p := range info.Paths {
				if rel, inside := RelPath(repo.Toplevel, cwd, p); inside {
					rels = append(rels, rel)
				}
			}
			if len(rels) == 0 {
				return Result{Skipped: "tệp nằm ngoài repository"}, nil
			}
			ev.Type = spool.FileModified
			if len(rels) == 1 {
				ev.Payload["path"] = rels[0]
				break
			}
			// Một lần gọi sửa nhiều tệp (apply_patch): mỗi tệp một sự kiện.
			res := Result{Written: true}
			for _, rel := range rels {
				e := ev
				e.Payload = map[string]any{"path": rel}
				for k, v := range ev.Payload {
					e.Payload[k] = v
				}
				e.Payload["path"] = rel
				e.EventID = EventID(h, hi.SessionID, hi.RawEvent, toolKey(), rel)
				f, err := spool.Write(paths.Spool, e)
				if err != nil {
					return Result{}, err
				}
				res.SpoolFile = f
			}
			return res, nil
		case ToolShell:
			shellModified(ctx, ev, paths.Spool, paths.Runtime, repo.Toplevel, hi.ToolUseID, snapTimeout(cfg.Context.PathHookTimeoutMS))
			var known []string
			for _, c := range []*string{cfg.Eval.TestCommand, cfg.Eval.SuggestedTestCommand} {
				if c != nil {
					known = append(known, *c)
				}
			}
			if detect.IsBintInvocation(info.Command) {
				return Result{Skipped: "lệnh gọi CLI của Bint, không phải kiểm thử"}, nil
			}
			isTest := detect.IsTestCommand
			if d.IsTestCommand != nil {
				isTest = d.IsTestCommand
			}
			if !isTest(info.Command, known...) {
				return Result{Skipped: "lệnh shell không phải kiểm thử"}, nil
			}
			ev.Type = spool.TestCompleted
			ev.Payload["command"] = detect.Redact(info.Command, 300)
			// Lệnh đã được người xác nhận (eval.test_command) cho tín hiệu kết quả mạnh (mục 9.6).
			if isConfirmedTest(cfg.Eval.TestCommand, info.Command) {
				ev.Payload["confirmed"] = true
			}
		default:
			return Result{Skipped: "công cụ không thuộc phạm vi ghi nhận"}, nil
		}
	default:
		return Result{Skipped: "sự kiện không thuộc phạm vi ghi nhận: " + hi.RawEvent}, nil
	}

	f, err := spool.Write(paths.Spool, ev)
	if err != nil {
		return Result{}, err
	}
	return Result{Written: true, SpoolFile: f}, nil
}

// mcpEvent điền sự kiện cho lần gọi công cụ MCP của Bint (mục 9.7): truy xuất,
// trích dẫn, quan sát. ID được lấy từ đầu ra của server (đã kiểm tra tồn tại).
func mcpEvent(ev *spool.Event, tool string, hi Input) string {
	resp := hi.ToolResponse
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

func approvedPath(rel string) bool {
	for _, l := range strings.Split(os.Getenv(EnvApprovedPaths), "\n") {
		if strings.TrimSpace(l) == rel {
			return true
		}
	}
	return false
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

// handleEval phục vụ lượt đánh giá nhánh bint: chỉ chèn theo đường dẫn, ghi nhật
// ký vào tệp của lượt (không có thư mục trạng thái).
func handleEval(hi Input, d Dialect, cwd, cfgPath string) (Result, error) {
	if hi.Event == EvPrompt {
		return handleEvalPrompt(hi, d, cfgPath)
	}
	if hi.Event != EvPreTool {
		return Result{Skipped: "chế độ đánh giá: chỉ chèn theo đề bài và đường dẫn"}, nil
	}
	info := d.Classify(hi.ToolName, hi.ToolInput)
	inject := (info.Kind == ToolRead && d.InjectRead) || (info.Kind == ToolEdit && d.InjectEdit)
	if !inject {
		return Result{Skipped: "công cụ không đọc/sửa tệp"}, nil
	}
	ec, err := ctxengine.LoadEvalConfig(cfgPath)
	if err != nil {
		return Result{}, err
	}
	var rels []string
	for _, p := range info.Paths {
		if rel, inside := RelPath(ec.Root, cwd, p); inside {
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		return Result{Skipped: "tệp nằm ngoài thư mục làm việc"}, nil
	}
	mem := ctxengine.NewSessionMemory(filepath.Dir(ec.Log), d.Harness, hi.SessionID)
	pack, ok := injectPaths(3*time.Second, func() (ctxengine.Pack, error) {
		items, _, err := knowledge.Store{Dir: ec.ItemsDir}.List()
		if err != nil {
			return ctxengine.Pack{}, err
		}
		e := &ctxengine.Engine{Items: items, Src: &knowledge.WorktreeSource{Root: ec.Root}, MaxTokens: ec.MaxTokens,
			PathMaxItems: ec.PathMaxItems, ExcludeEvalCase: ec.ExcludeEvalCase, PathIncludeCandidates: ec.IncludeCandidates,
			LessonGlobalK: ec.LessonGlobalK, LessonMaxTokens: ec.LessonMaxTokens}
		return e.ForPaths(rels, mem.Seen()), nil
	})
	if !ok || len(pack.Entries) == 0 && len(pack.Lessons) == 0 {
		return Result{Skipped: "không có tri thức khớp"}, nil
	}
	mem.Add(pack.IDs())
	ctxengine.AppendLog(ec.Log, ctxengine.PackLog(ctxengine.ChannelHookPath, pack))
	return Result{Output: d.PreToolOutput(pack.Render(), "")}, nil
}

// handleEvalPrompt chèn gói ngữ cảnh theo đề bài khi agent nhận yêu cầu (nhánh bint):
// agent không phải tự gọi context_for_task mới nhận được tri thức.
func handleEvalPrompt(hi Input, d Dialect, cfgPath string) (Result, error) {
	if d.PromptOutput == nil || strings.TrimSpace(hi.Prompt) == "" {
		return Result{Skipped: "không chèn ngữ cảnh theo đề bài"}, nil
	}
	ec, err := ctxengine.LoadEvalConfig(cfgPath)
	if err != nil {
		return Result{}, err
	}
	mem := ctxengine.NewSessionMemory(filepath.Dir(ec.Log), d.Harness, hi.SessionID)
	pack, ok := injectPaths(5*time.Second, func() (ctxengine.Pack, error) {
		// Cùng engine với MCP server của lượt (context_for_task), để hai kênh trả cùng kết quả.
		e, closeFn, err := ec.Engine(context.Background())
		if err != nil {
			return ctxengine.Pack{}, err
		}
		defer closeFn()
		return e.ForTask(context.Background(), hi.Prompt, nil)
	})
	if !ok || len(pack.Entries) == 0 && len(pack.Lessons) == 0 {
		return Result{Skipped: "không có tri thức khớp đề bài"}, nil
	}
	mem.Add(pack.IDs())
	ctxengine.AppendLog(ec.Log, ctxengine.PackLog(ctxengine.ChannelHookTask, pack))
	return Result{Output: d.PromptOutput(pack.Render())}, nil
}

// ClaudeStylePromptOutput dựng JSON hookSpecificOutput cho UserPromptSubmit.
func ClaudeStylePromptOutput(text string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "UserPromptSubmit", "additionalContext": text}})
	return string(b)
}

// EventID dựng event_id ổn định từ tên harness và các phần định danh.
func EventID(harness string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(harness))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return "ev_" + hex.EncodeToString(h.Sum(nil))[:32]
}

// ModelName chấp nhận model dạng chuỗi hoặc đối tượng {id, display_name}.
func ModelName(raw json.RawMessage) string {
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

// RawText trả về chuỗi JSON thô; nếu raw là một chuỗi JSON thì trả nội dung chuỗi.
func RawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
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

// ClaudeStyleInput là payload hook kiểu Claude Code (Codex dùng cùng khuôn).
type ClaudeStyleInput struct {
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	Prompt         string          `json:"prompt"`
	Source         string          `json:"source"`
	Reason         string          `json:"reason"`
	Model          json.RawMessage `json:"model"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	AgentID        string          `json:"agent_id"`
	AgentType      string          `json:"agent_type"`
	TranscriptPath string          `json:"transcript_path"`
}

// claudeEvents ánh xạ hook_event_name kiểu Claude Code sang loại chuẩn hoá.
var claudeEvents = map[string]string{
	"SessionStart": EvSessionStart, "SessionEnd": EvSessionEnd, "PreToolUse": EvPreTool,
	"PostToolUse": EvPostTool, "PostToolUseFailure": EvPostToolFailure,
	"UserPromptSubmit": EvPrompt,
}

// NormalizeClaudeStyle chuyển payload kiểu Claude Code về Input.
func NormalizeClaudeStyle(raw []byte) (Input, error) {
	var ci ClaudeStyleInput
	if err := json.Unmarshal(raw, &ci); err != nil {
		return Input{}, fmt.Errorf("đọc dữ liệu hook: %w", err)
	}
	in := Input{SessionID: ci.SessionID, Cwd: ci.Cwd, RawEvent: ci.HookEventName, Event: claudeEvents[ci.HookEventName],
		Source: ci.Source, Reason: ci.Reason, Model: ModelName(ci.Model), ToolName: ci.ToolName, ToolUseID: ci.ToolUseID,
		ToolResponse: RawText(ci.ToolResponse), AgentType: ci.AgentType, TranscriptPath: ci.TranscriptPath, Prompt: ci.Prompt}
	if ci.HookEventName != "" && in.Event == "" {
		in.Event = "other"
	}
	if len(ci.ToolInput) > 0 {
		// tool_input thường là object; một số bản ghi nó thành chuỗi JSON.
		if json.Unmarshal(ci.ToolInput, &in.ToolInput) != nil {
			var s string
			if json.Unmarshal(ci.ToolInput, &s) == nil {
				json.Unmarshal([]byte(s), &in.ToolInput)
			}
		}
	}
	return in, nil
}

// ClaudeStylePreToolOutput dựng JSON hookSpecificOutput (Claude Code, Codex):
// additionalContext để chèn ngữ cảnh, permissionDecision để hỏi/chặn.
func ClaudeStylePreToolOutput(decision string) func(text, ask string) string {
	return func(text, ask string) string {
		out := map[string]any{"hookEventName": "PreToolUse"}
		if text != "" {
			out["additionalContext"] = text
		}
		if ask != "" {
			out["permissionDecision"] = decision
			out["permissionDecisionReason"] = ask
		}
		b, _ := json.Marshal(map[string]any{"hookSpecificOutput": out})
		return string(b)
	}
}

// isConfirmedTest cho biết lệnh shell chạy đúng eval.test_command (coi là mẫu:
// {test_files} khớp các đối số đường dẫn, phần cố định khớp theo token).
func isConfirmedTest(tc *string, command string) bool {
	if tc == nil || strings.TrimSpace(*tc) == "" {
		return false
	}
	return detect.MatchesTemplate(command, *tc)
}
