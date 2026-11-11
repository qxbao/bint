package app

import (
	"context"
	"slices"
	"strings"

	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
)

// Dịch vụ harness dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.3): `harness list/show` có kiểu
// Options, Validate() và Defaults(cfg); trạng thái hook/MCP tách từ Doctor thành HarnessRuntime để
// doctor và màn Hệ thống › Harness không lệch nhau.

// HarnessListOptions: `bint harness list`.
type HarnessListOptions struct {
	NoVersion bool `flag:"no-version"`
}

// HarnessListDefaults: có lấy phiên bản (gọi chương trình harness).
func HarnessListDefaults(config.Config) HarnessListOptions { return HarnessListOptions{} }

func (o HarnessListOptions) Validate() error { return nil }

// HarnessShowOptions: `bint harness show <tên>`.
type HarnessShowOptions struct {
	Name string `arg:"name" form:"required,suggest=harnesses"`
}

// HarnessShowDefaults: không có giá trị mặc định.
func HarnessShowDefaults(config.Config) HarnessShowOptions { return HarnessShowOptions{} }

// Validate kiểm tên harness có adapter.
func (o HarnessShowOptions) Validate() error {
	if _, ok := harness.Get(o.Name); !ok {
		return Usagef("harness không hỗ trợ %q (%s)", o.Name, strings.Join(harness.Names(), ", "))
	}
	return nil
}

// HarnessConfig nạp cấu hình của repository chứa dir; ngoài repository hoặc chưa init thì trả mặc định
// (bảng khả năng vẫn xem được).
func HarnessConfig(ctx context.Context, dir string) config.Config {
	if ws, err := OpenWorkspace(ctx, dir, false); err == nil {
		return ws.Config
	}
	return config.Default()
}

// HarnessVersionOf lấy phiên bản của adapter bằng cách gọi chương trình harness (chậm: gọi tiến trình ngoài).
func HarnessVersionOf(ctx context.Context, cfg config.Config) func(harness.Adapter) string {
	return func(a harness.Adapter) string {
		if ex, ok := a.(harness.Executor); ok {
			return ex.NewAgent(harness.AgentOptions{Config: cfg}).Version(ctx)
		}
		return ""
	}
}

// ListHarnesses mô tả mọi harness có adapter. NoVersion = true không gọi chương trình harness.
func ListHarnesses(ctx context.Context, dir string, o HarnessListOptions) ([]harness.Info, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	cfg := HarnessConfig(ctx, dir)
	var vf func(harness.Adapter) string
	if !o.NoVersion {
		vf = HarnessVersionOf(ctx, cfg)
	}
	var infos []harness.Info
	for _, a := range harness.All() {
		infos = append(infos, harness.Describe(a, cfg, vf))
	}
	return infos, nil
}

// ShowHarness mô tả một harness (có lấy phiên bản).
func ShowHarness(ctx context.Context, dir string, o HarnessShowOptions) (harness.Info, error) {
	if err := o.Validate(); err != nil {
		return harness.Info{}, err
	}
	a, _ := harness.Get(o.Name)
	cfg := HarnessConfig(ctx, dir)
	return harness.Describe(a, cfg, HarnessVersionOf(ctx, cfg)), nil
}

// HarnessVersion lấy phiên bản của một harness (rỗng nếu không lấy được). Gọi tiến trình ngoài nên chạy
// trong tea.Cmd.
func HarnessVersion(ctx context.Context, dir, name string) string {
	a, ok := harness.Get(name)
	if !ok {
		return ""
	}
	return HarnessVersionOf(ctx, HarnessConfig(ctx, dir))(a)
}

// HookStatus là trạng thái hook của một harness; Level/Detail đúng như dòng "hook <tên>" của doctor.
type HookStatus struct {
	Level  Level
	Detail string
	// Scopes: phạm vi đã cài (local/project/user); rỗng = chưa cài.
	Scopes []string
}

// MCPStatus là trạng thái đăng ký MCP server của Bint trong một harness.
type MCPStatus struct {
	Registered bool
	// CitationUnverified (Codex): đã đăng ký nhưng chưa xác minh cite_knowledge chạy được với approval policy never.
	CitationUnverified bool
}

// HarnessStatus là trạng thái hook/MCP của một harness bật trong cấu hình. Hook/MCP nil = harness không có
// cơ chế đó (hoặc MCP tắt trong cấu hình).
type HarnessStatus struct {
	Name string
	Hook *HookStatus
	MCP  *MCPStatus
	// CodexUntrusted: harness là Codex và repository chưa được Codex tin cậy.
	CodexUntrusted bool
}

// runtimeOf dựng trạng thái hook/MCP của adapter ad (dùng chung cho Doctor và HarnessRuntime).
func runtimeOf(ctx context.Context, ad harness.Adapter, cfg config.Config, root string) HarnessStatus {
	rt := HarnessStatus{Name: ad.Name()}
	if hi, ok := ad.(harness.HookInstaller); ok {
		h := hookStatus(ctx, ad, hi, root, cfg.Harnesses.Codex.Binary)
		rt.Hook = &h
	}
	if mi, ok := ad.(harness.MCPInstaller); ok && cfg.Context.Interfaces.MCP {
		st := MCPStatus{Registered: mi.MCPInstalled(root)}
		if st.Registered && ad.Name() == codex.HarnessName && !codex.CitationApprovalConfigured(root) {
			st.CitationUnverified = true
		}
		rt.MCP = &st
	}
	if ad.Name() == codex.HarnessName && !codex.ProjectTrusted(root) {
		rt.CodexUntrusted = true
	}
	return rt
}

// HarnessRuntime trả trạng thái hook/MCP của từng harness đang bật trong cấu hình (theo thứ tự
// cấu hình), cùng nguồn với `bint doctor`. Có thể gọi chương trình codex để kiểm tra trust nên chậm.
func HarnessRuntime(ctx context.Context, dir string) ([]HarnessStatus, error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	var out []HarnessStatus
	for _, h := range ws.Config.Harnesses.Enabled {
		if ad, ok := harness.Get(h); ok {
			out = append(out, runtimeOf(ctx, ad, ws.Config, ws.Repo.Toplevel))
		}
	}
	return out, nil
}

// hookStatus kiểm tra hook của harness ở mọi phạm vi (local, project, user).
func hookStatus(ctx context.Context, ad harness.Adapter, hi harness.HookInstaller, root, codexBinary string) HookStatus {
	var found, paths []string
	for _, sc := range []claude.Scope{claude.ScopeLocal, claude.ScopeProject, claude.ScopeUser} {
		p, err := hi.HooksPath(sc, root)
		if err != nil || slices.Contains(paths, p) {
			continue
		}
		paths = append(paths, p)
		if ok, _ := hi.HooksInstalled(p); ok {
			found = append(found, string(sc))
			if ad.Name() == codex.HarnessName {
				trust, err := codex.InspectHookTrust(ctx, codexBinary, root, p)
				if err != nil {
					return HookStatus{LevelWarn, "đã cài nhưng chưa xác minh trust: " + err.Error(), found}
				}
				if !trust.AllTrusted() {
					return HookStatus{LevelWarn, "đã cài ở " + p + ": " + trust.String() + "; mở codex, /hooks để duyệt định nghĩa hiện hành", found}
				}
			}
		}
	}
	switch len(found) {
	case 0:
		return HookStatus{LevelWarn, "chưa cài; lượt chạy trực tiếp trong " + ad.Title() + " sẽ không được ghi nhận (bint init --hooks)", nil}
	case 1:
		return HookStatus{LevelOK, "phạm vi " + found[0], found}
	default:
		return HookStatus{LevelInfo, "cài ở nhiều phạm vi (" + strings.Join(found, ", ") + "); sự kiện trùng được loại theo event_id", found}
	}
}
