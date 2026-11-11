package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/fsafe"
	"bint/internal/gitrepo"
	"bint/internal/harness"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/skills"
	"bint/internal/state"
)

// Dịch vụ init dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.6, 5.7): InitFlagOptions là hình dạng của
// các cờ `bint init` (một cờ một trường, có thẻ `flag`), InitState đọc trạng thái hiện tại để điền sẵn form và
// PlanInit liệt kê tệp sẽ tạo/sửa/gỡ cho hộp xem trước. Cả hai chỉ đọc: không mở SQLite, không tạo thư mục,
// không gọi Ensure.

// InitFlagOptions là Options của `bint init`: đủ 11 cờ. Cặp --hooks/--no-hooks và --mcp/--no-mcp là hai trường
// bool vì CLI có hai cờ; ToInit đổi thành *bool của InitOptions.
type InitFlagOptions struct {
	Harnesses  []string `flag:"harness" form:"suggest=harnesses"`
	Hooks      bool     `flag:"hooks"`
	NoHooks    bool     `flag:"no-hooks"`
	HooksScope string   `flag:"hooks-scope" form:"choices=local|project|user"`
	AgentsMD   bool     `flag:"agents-md"`
	MCP        bool     `flag:"mcp"`
	NoMCP      bool     `flag:"no-mcp"`
	Force      bool     `flag:"force"`
	NoSkills   bool     `flag:"no-skills"`
	GitMode    string   `flag:"git" form:"choices=track|local"`
	Update     bool     `flag:"update"`
}

// InitDefaults là giá trị khởi đầu của form khởi tạo: đúng mặc định của các cờ CLI.
func InitDefaults(config.Config) InitFlagOptions {
	return InitFlagOptions{HooksScope: string(claude.ScopeLocal)}
}

// Validate kiểm các ràng buộc giữa các cờ; lỗi là UsageError (mã thoát 2).
func (o InitFlagOptions) Validate() error {
	if o.Hooks && o.NoHooks {
		return Usagef("--hooks và --no-hooks loại trừ nhau")
	}
	if o.MCP && o.NoMCP {
		return Usagef("--mcp và --no-mcp loại trừ nhau")
	}
	if o.HooksScope != "" {
		if _, err := claude.ParseScope(o.HooksScope); err != nil {
			return Usagef("--hooks-scope: %v", err)
		}
	}
	if o.GitMode != "" && !slices.Contains(GitModes, o.GitMode) {
		return Usagef("--git: phải là %s (nhận %q)", strings.Join(GitModes, " | "), o.GitMode)
	}
	for _, h := range o.Harnesses {
		if !slices.Contains(config.KnownHarnesses, h) {
			return Usagef("--harness: harness không hỗ trợ %q (hỗ trợ: %s)", h, strings.Join(config.KnownHarnesses, ", "))
		}
	}
	return nil
}

// ToInit kiểm Validate rồi đổi sang InitOptions của app.Init. Hooks và MCP là nil khi cả hai cờ của cặp đều tắt (giữ nguyên /
// hỏi qua Confirm do người gọi gắn).
func (o InitFlagOptions) ToInit(dir, bintPath, version string) (InitOptions, error) {
	if err := o.Validate(); err != nil {
		return InitOptions{}, err
	}
	sc := claude.ScopeLocal
	if o.HooksScope != "" {
		sc, _ = claude.ParseScope(o.HooksScope) // đã qua Validate
	}
	opts := InitOptions{
		Dir: dir, Harnesses: o.Harnesses, HooksScope: sc, AgentsMD: o.AgentsMD, Force: o.Force,
		BintPath: bintPath, Version: version, NoSkills: o.NoSkills, Update: o.Update, GitMode: o.GitMode,
	}
	switch {
	case o.Hooks:
		t := true
		opts.Hooks = &t
	case o.NoHooks:
		f := false
		opts.Hooks = &f
	}
	switch {
	case o.MCP:
		t := true
		opts.MCP = &t
	case o.NoMCP:
		f := false
		opts.MCP = &f
	}
	return opts, nil
}

// SetupSteps đổi phần thay đổi ch (kết quả so form Cài đặt repo với InitState s) thành các lần chạy init tuần tự.
// app.Init chỉ nhắm MỘT phạm vi hook mỗi lần (--hooks-scope, mặc định local), nên tắt hook hoặc đổi phạm vi khi hook đang cài ở
// phạm vi khác sẽ không tác động hook thật nếu chỉ gửi một lần:
//   - tắt hook: một bước --no-hooks cho mỗi phạm vi đang có hook;
//   - bật hoặc đổi phạm vi (formScope là phạm vi trên form): các bước gỡ ở mọi phạm vi cũ TRƯỚC, bước --hooks ở phạm vi
//     mới SAU. Phạm vi cũ mà hook nằm đúng tệp mà phạm vi mới sẽ cài (Codex dùng chung .codex/hooks.json cho local và
//     project) không cần bước gỡ: gỡ rồi cài lại chỉ để mất hook rồi tạo lại, còn gỡ sau khi cài thì mất hook vừa cài.
//
// Các cờ không liên quan hook đi kèm bước đầu. Không có thay đổi hook thì trả đúng ch (một bước).
func SetupSteps(s InitState, ch InitFlagOptions, formScope string) []InitFlagOptions {
	if !ch.Hooks && !ch.NoHooks {
		return []InitFlagOptions{ch}
	}
	scope := ch.HooksScope
	if scope == "" {
		scope = formScope
	}
	if scope == "" {
		scope = s.HooksScope()
	}
	rest := ch
	rest.Hooks, rest.NoHooks, rest.HooksScope = false, false, ""

	type hookFile struct{ harness, path string }
	installed := map[string][]hookFile{}
	target := map[hookFile]bool{}
	for _, h := range s.Harnesses {
		if !h.Enabled || !h.HasHooks {
			continue
		}
		ad, ok := harness.Get(h.Name)
		if !ok {
			continue
		}
		hi, ok := ad.(harness.HookInstaller)
		if !ok {
			continue
		}
		for _, sc := range h.HookScopes {
			if p, err := hi.HooksPath(claude.Scope(sc), s.Root); err == nil {
				installed[sc] = append(installed[sc], hookFile{h.Name, p})
			}
		}
		if p, err := hi.HooksPath(claude.Scope(scope), s.Root); err == nil {
			target[hookFile{h.Name, p}] = true
		}
	}
	var steps []InitFlagOptions
	covered := map[hookFile]bool{}
	for _, sc := range []string{"local", "project", "user"} {
		if ch.Hooks && sc == scope {
			continue
		}
		need := false
		for _, f := range installed[sc] {
			need = need || (!covered[f] && !(ch.Hooks && target[f]))
		}
		if !need {
			continue
		}
		for _, f := range installed[sc] {
			covered[f] = true
		}
		steps = append(steps, InitFlagOptions{NoHooks: true, HooksScope: sc})
	}
	if ch.Hooks {
		steps = append(steps, InitFlagOptions{Hooks: true, HooksScope: scope})
	}
	if len(steps) == 0 { // không có gì để gỡ (trạng thái không khớp): giữ nguyên yêu cầu
		return []InitFlagOptions{ch}
	}
	first := rest
	first.Hooks, first.NoHooks, first.HooksScope = steps[0].Hooks, steps[0].NoHooks, steps[0].HooksScope
	steps[0] = first
	return steps
}

// HarnessInitState là trạng thái hook/MCP của một harness (probe chỉ đọc, không gọi chương trình harness).
type HarnessInitState struct {
	Name    string
	Enabled bool // có trong harnesses.enabled
	Present bool // chương trình hoặc thư mục cấu hình có trên máy
	// HasHooks/HasMCP: adapter có cơ chế đó.
	HasHooks bool
	HasMCP   bool
	// HookScopes: các phạm vi có hook (local/project/user); rỗng = chưa cài. Phạm vi cùng trỏ một tệp (Codex: local = project) đều có mặt.
	HookScopes []string
	MCP        bool
}

// InitState là trạng thái hiện tại của repository đối với `bint init` (điền sẵn form Cài đặt repo).
type InitState struct {
	// NotInit: chưa có cấu hình hoặc chưa có thư mục trạng thái trên máy này.
	NotInit        bool
	Root           string
	LinkedWorktree bool
	Main           string
	Harnesses      []HarnessInitState
	// Enabled: harnesses.enabled trong cấu hình ("" khi chưa init).
	Enabled []string
	// GitMode: lựa chọn --git đã lưu cho máy này ("" = chưa chọn, tức track).
	GitMode string
	// AgentsBlock: AGENTS.md đã có khối bint. ClaudeImport: CLAUDE.md đã nhập AGENTS.md.
	AgentsBlock  bool
	ClaudeImport bool
	// ConfigOutdated: cấu hình theo mẫu cũ (config.Upgrade có gì để đổi); ConfigAdded là các khoá còn thiếu.
	ConfigOutdated bool
	ConfigAdded    []string
	ConfigFixed    int
}

// HooksOn: mọi harness bật có hook đều đã cài hook (và có ít nhất một harness như vậy).
func (s InitState) HooksOn() bool {
	n := 0
	for _, h := range s.Harnesses {
		if h.Enabled && h.HasHooks {
			n++
			if len(h.HookScopes) == 0 {
				return false
			}
		}
	}
	return n > 0
}

// MCPOn: mọi harness bật có MCP đều đã đăng ký.
func (s InitState) MCPOn() bool {
	n := 0
	for _, h := range s.Harnesses {
		if h.Enabled && h.HasMCP {
			n++
			if !h.MCP {
				return false
			}
		}
	}
	return n > 0
}

// HooksScope: phạm vi hook chung của mọi harness đã cài ("local" nếu chưa cài hoặc không có phạm vi chung); ưu tiên local, project, user.
func (s InitState) HooksScope() string {
	var installed []HarnessInitState
	for _, h := range s.Harnesses {
		if h.Enabled && len(h.HookScopes) > 0 {
			installed = append(installed, h)
		}
	}
	for _, sc := range []string{"local", "project", "user"} {
		all := len(installed) > 0
		for _, h := range installed {
			all = all && slices.Contains(h.HookScopes, sc)
		}
		if all {
			return sc
		}
	}
	return string(claude.ScopeLocal)
}

// FormDefaults dựng Options khởi đầu của form Cài đặt repo từ trạng thái hiện tại.
func (s InitState) FormDefaults() InitFlagOptions {
	o := InitDefaults(config.Default())
	o.Harnesses = slices.Clone(s.Enabled)
	o.Hooks, o.MCP, o.AgentsMD = s.HooksOn(), s.MCPOn(), s.AgentsBlock
	o.HooksScope = s.HooksScope()
	o.GitMode = s.GitMode
	if o.GitMode == "" {
		o.GitMode = GitTrack
	}
	return o
}

// ReadInitState đọc trạng thái hiện tại; chỉ đọc. Repository chưa init trả NotInit (không phải lỗi).
func ReadInitState(ctx context.Context, dir string) (InitState, error) {
	var st InitState
	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		return st, err
	}
	st.Root, st.LinkedWorktree, st.Main = repo.Toplevel, repo.IsLinkedWorktree(), repo.MainWorktree()
	ws, err := OpenWorkspace(ctx, dir, true)
	if errors.Is(err, ErrNotInitialized) {
		st.NotInit = true
		st.Harnesses = harnessStates(detect.Harnesses(), nil, repo.Toplevel)
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Enabled = slices.Clone(ws.Config.Harnesses.Enabled)
	st.Harnesses = harnessStates(detect.Harnesses(), st.Enabled, repo.Toplevel)
	st.GitMode = LoadGitMode(ws.Paths)
	cfgRoot := configRoot(repo)
	if b, _ := fsafe.ReadBlock(filepath.Join(cfgRoot, "AGENTS.md"), fsafe.HTMLMarkers); b != nil {
		st.AgentsBlock = true
	}
	if data, err := os.ReadFile(filepath.Join(repo.Toplevel, "CLAUDE.md")); err == nil {
		st.ClaudeImport = hasImportLine(string(data), "@AGENTS.md")
		if b, _ := fsafe.ReadBlock(filepath.Join(repo.Toplevel, "CLAUDE.md"), fsafe.HTMLMarkers); b != nil {
			st.ClaudeImport = true
		}
	}
	if up, err := readUpgrade(cfgRoot); err == nil && up.Changed() {
		st.ConfigOutdated, st.ConfigAdded, st.ConfigFixed = true, up.Added, up.Fixed
	}
	return st, nil
}

// harnessStates dựng trạng thái từng harness Bint biết (probe không gọi chương trình ngoài).
func harnessStates(detected []detect.Harness, enabled []string, root string) []HarnessInitState {
	var out []HarnessInitState
	for _, d := range detected {
		h := HarnessInitState{Name: d.Name, Present: d.Present, Enabled: slices.Contains(enabled, d.Name)}
		if ad, ok := harness.Get(d.Name); ok {
			if hi, ok := ad.(harness.HookInstaller); ok {
				h.HasHooks = true
				// Mọi phạm vi có hook, không loại phạm vi trùng đường dẫn (Codex dùng chung .codex/hooks.json cho local và project):
				// nên hook ở tệp đó được tính cho cả hai phạm vi.
				for _, sc := range []claude.Scope{claude.ScopeLocal, claude.ScopeProject, claude.ScopeUser} {
					p, err := hi.HooksPath(sc, root)
					if err != nil {
						continue
					}
					if ok, _ := hi.HooksInstalled(p); ok {
						h.HookScopes = append(h.HookScopes, string(sc))
					}
				}
			}
			if mi, ok := ad.(harness.MCPInstaller); ok {
				h.HasMCP, h.MCP = true, mi.MCPInstalled(root)
			}
		}
		out = append(out, h)
	}
	return out
}

// configRoot trả thư mục gốc chứa .bint/config.yml (worktree chính khi worktree này chưa có).
func configRoot(repo gitrepo.Repo) string {
	for _, r := range repo.ConfigRoots() {
		if _, err := os.Stat(config.ProjectPath(r)); err == nil {
			return r
		}
	}
	return repo.Toplevel
}

func readUpgrade(cfgRoot string) (config.UpgradeResult, error) {
	old, err := os.ReadFile(config.ProjectPath(cfgRoot))
	if err != nil {
		return config.UpgradeResult{}, err
	}
	return config.Upgrade(old)
}

// PlanChange là loại thay đổi của một mục trong kế hoạch.
type PlanChange string

const (
	PlanCreate PlanChange = "create"
	PlanUpdate PlanChange = "update"
	PlanRemove PlanChange = "remove"
	PlanKeep   PlanChange = "keep"
)

// PlanItem là một tệp (hoặc mục) sẽ bị `bint init` đụng tới.
type PlanItem struct {
	Path   string
	Change PlanChange
	Detail string
}

// InitPlan là kế hoạch dự kiến của một lần `bint init` (xem trước, không chính xác tuyệt đối: báo cáo sau khi chạy
// mới là kết quả thật).
type InitPlan struct {
	FirstInit bool
	Items     []PlanItem
	// ConfigDiff: diff nâng cấu hình (khi Update và cấu hình theo mẫu cũ); Notes là ghi chú của config.Upgrade.
	ConfigDiff string
	Notes      []string
}

// Changes đếm các mục thật sự đổi (không tính keep).
func (p InitPlan) Changes() int {
	n := 0
	for _, it := range p.Items {
		if it.Change != PlanKeep {
			n++
		}
	}
	return n
}

// PlanInit liệt kê tệp sẽ tạo/sửa/gỡ nếu chạy `bint init` với o. Chỉ đọc.
func PlanInit(ctx context.Context, dir string, o InitFlagOptions) (InitPlan, error) {
	var p InitPlan
	if err := o.Validate(); err != nil {
		return p, err
	}
	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		return p, err
	}
	root := repo.Toplevel
	cfgRoot := configRoot(repo)
	cfgPath := config.ProjectPath(cfgRoot)
	_, statErr := os.Stat(cfgPath)
	cfgExists := statErr == nil
	p.FirstInit = !cfgExists
	add := func(path string, ch PlanChange, format string, a ...any) {
		p.Items = append(p.Items, PlanItem{Path: path, Change: ch, Detail: fmt.Sprintf(format, a...)})
	}

	cfg := config.Default()
	var enabled []string
	switch {
	case !cfgExists:
		enabled = o.Harnesses
		if len(enabled) == 0 {
			for _, h := range detect.Harnesses() {
				if h.Present {
					enabled = append(enabled, h.Name)
				}
			}
		}
		if len(enabled) == 0 {
			enabled = []string{"claude"}
		}
		cfg.Harnesses.Enabled = enabled
		add(".bint/config.yml", PlanCreate, "harness: %s", strings.Join(enabled, ", "))
	default:
		loaded, _, err := config.Load(cfgRoot, true)
		if err != nil {
			return p, fmt.Errorf("cấu hình hiện có không hợp lệ: %w", err)
		}
		cfg, enabled = loaded, loaded.Harnesses.Enabled
		up, uerr := readUpgrade(cfgRoot)
		switch {
		case uerr != nil:
			add(".bint/config.yml", PlanKeep, "%v", uerr)
		case o.Update && cfgRoot != root:
			add(".bint/config.yml", PlanKeep, "cấu hình nằm ở worktree chính %s — chạy ở đó", cfgRoot)
		case o.Update && up.Changed():
			add(".bint/config.yml", PlanUpdate, "thêm: %s (bản cũ được sao lưu)", orDash(config.SummarizeAdded(up.Added)))
			old, _ := os.ReadFile(cfgPath)
			p.ConfigDiff = UnifiedDiff(".bint/config.yml", ".bint/config.yml (mẫu mới)", string(old), string(up.Content))
			p.Notes = up.Notes
		case o.Update:
			add(".bint/config.yml", PlanKeep, "đã theo mẫu hiện tại")
		default:
			add(".bint/config.yml", PlanKeep, "đã tồn tại, giữ nguyên")
		}
	}

	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return p, err
	}
	if paths.Initialized() {
		add(paths.Root, PlanKeep, "thư mục trạng thái đã có")
	} else {
		add(paths.Root, PlanCreate, "thư mục trạng thái và SQLite (ngoài repository)")
	}

	if !o.NoSkills {
		core := skills.Core()
		for _, t := range harness.SkillTargets(enabled) {
			exists := false
			for _, sk := range core {
				if _, err := os.Stat(filepath.Join(root, t.Dir, sk.Name, "SKILL.md")); err == nil {
					exists = true
					break
				}
			}
			ch := PlanCreate
			if exists {
				ch = PlanUpdate
			}
			add(filepath.ToSlash(t.Dir), ch, "%d skill gốc cho %s", len(core), strings.Join(harness.HarnessesForTarget(enabled, t.Dir), ", "))
		}
	}

	scope := claude.ScopeLocal
	if o.HooksScope != "" {
		scope, _ = claude.ParseScope(o.HooksScope)
	}
	for _, h := range enabled {
		ad, ok := harness.Get(h)
		if !ok {
			continue
		}
		if hi, ok := ad.(harness.HookInstaller); ok {
			path, err := hi.HooksPath(scope, root)
			if err != nil {
				continue
			}
			already, _ := hi.HooksInstalled(path)
			label := planPath(root, path)
			switch {
			case o.NoHooks && already:
				add(label, PlanRemove, "gỡ hook %s", h)
			case o.NoHooks:
				add(label, PlanKeep, "hook %s chưa cài", h)
			case o.Hooks && !already:
				add(label, PlanCreate, "cài hook %s (%s)", h, hi.HookEvents())
			case already:
				add(label, PlanUpdate, "làm mới hook %s", h)
			default:
				add(label, PlanKeep, "hook %s: không đổi (chưa có sự đồng ý để cài)", h)
			}
		}
		if mi, ok := ad.(harness.MCPInstaller); ok && (o.MCP || o.NoMCP) {
			rel := mcpRel(h)
			installed := mi.MCPInstalled(root)
			switch {
			case o.NoMCP && installed:
				add(rel, PlanRemove, "gỡ mục %q (MCP %s)", claude.MCPServerName, h)
			case o.NoMCP:
				add(rel, PlanKeep, "MCP %s chưa đăng ký", h)
			case installed:
				add(rel, PlanKeep, "MCP %s đã đăng ký", h)
			default:
				add(rel, PlanCreate, "đăng ký MCP server của Bint cho %s", h)
			}
		}
	}

	prev := ""
	if paths.Initialized() {
		prev = LoadGitMode(paths)
	}
	mode := o.GitMode
	if mode == "" {
		mode = prev
	}
	if mode == "" {
		mode = GitTrack
	}
	if mode == GitLocal {
		add(".git/info/exclude", PlanUpdate, "chế độ local: tệp của Bint vào khối exclude, tệp đã theo dõi đặt skip-worktree")
	} else {
		if b, _ := fsafe.ReadBlock(filepath.Join(root, ".gitignore"), fsafe.HashMarkers); b == nil {
			add(".gitignore", PlanUpdate, "thêm khối bint")
		} else {
			add(".gitignore", PlanKeep, "đã có khối bint")
		}
		if prev == GitLocal {
			add(".git/info/exclude", PlanRemove, "chế độ track: gỡ khối bint và skip-worktree")
		}
	}

	if o.AgentsMD {
		ch, detail := PlanCreate, "ghi khối bint"
		if b, _ := fsafe.ReadBlock(filepath.Join(root, "AGENTS.md"), fsafe.HTMLMarkers); b != nil {
			ch, detail = PlanUpdate, "cập nhật khối bint"
		}
		add("AGENTS.md", ch, "%s", detail)
		if slices.Contains(enabled, claude.HarnessName) {
			add("CLAUDE.md", PlanUpdate, "nhập lại @AGENTS.md")
		}
	}
	return p, nil
}

// planPath rút đường dẫn trong repository về dạng tương đối (dùng "/"); ngoài repository giữ tuyệt đối.
func planPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return p
}

func mcpRel(h string) string {
	switch h {
	case claude.HarnessName:
		return ".mcp.json"
	case codex.HarnessName:
		return ".codex/config.toml"
	}
	return "MCP " + h
}

// ConfigUpgradePlan là bản nâng cấu hình theo mẫu hiện tại (xem trước của `bint init --update`).
type ConfigUpgradePlan struct {
	Path string
	// Changed: có gì để nâng. Diff là so sánh nội dung cũ/mới; Added/Notes/Fixed theo config.Upgrade.
	Changed bool
	Diff    string
	Added   []string
	Notes   []string
	Fixed   int
	Content []byte
}

// PlanConfigUpgrade tính bản nâng cấu hình bằng config.Upgrade, không ghi gì. Ở worktree phụ trả
// LinkedWorktreeError (cấu hình nằm ở worktree chính, mục 7.5).
func PlanConfigUpgrade(ctx context.Context, dir string) (ConfigUpgradePlan, error) {
	var pl ConfigUpgradePlan
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return pl, err
	}
	if ws.Repo.IsLinkedWorktree() {
		return pl, LinkedWorktreeError{Main: ws.Repo.MainWorktree()}
	}
	cfgRoot := configRoot(ws.Repo)
	pl.Path = config.ProjectPath(cfgRoot)
	old, err := os.ReadFile(pl.Path)
	if err != nil {
		return pl, err
	}
	up, err := config.Upgrade(old)
	if err != nil {
		return pl, fmt.Errorf("nâng %s: %w", pl.Path, err)
	}
	pl.Changed, pl.Added, pl.Notes, pl.Fixed, pl.Content = up.Changed(), up.Added, up.Notes, up.Fixed, up.Content
	if pl.Changed {
		pl.Diff = UnifiedDiff(".bint/config.yml", ".bint/config.yml (mẫu mới)", string(old), string(up.Content))
	}
	return pl, nil
}

// ConfigUpgradeOptions: `bint init --update` chạy từ TUI (hành động system.config_upgrade).
type ConfigUpgradeOptions struct {
	BintPath string
	Version  string
	Now      func() time.Time
}

// UpgradeConfig nâng cấu hình bằng app.Init với Update (đúng như CLI: cũng làm mới skill và hook đã cài).
func UpgradeConfig(ctx context.Context, dir string, o ConfigUpgradeOptions) (*InitReport, error) {
	if _, err := PlanConfigUpgrade(ctx, dir); err != nil {
		return nil, err
	}
	return Init(ctx, InitOptions{Dir: dir, Update: true, BintPath: o.BintPath, Version: o.Version, Now: o.Now})
}

// ConfigBackupPath là đường dẫn bản sao lưu cấu hình của lần nâng (InitReport.ConfigBackup); rỗng nếu không có.
func ConfigBackupPath(rep *InitReport) string {
	if rep == nil {
		return ""
	}
	return rep.ConfigBackup
}
