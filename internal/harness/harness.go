// Package harness là lớp HarnessAdapter (mục 7): mỗi harness (Claude Code, Codex,
// Antigravity) được mô tả bằng một Adapter cùng bảng khả năng (capability matrix)
// đã đối chiếu với phiên bản cụ thể. Bint Core chỉ làm việc qua lớp này nên workflow
// và nguồn tri thức tách khỏi harness thực thi; khả năng không có thì được báo, không
// được mô phỏng (mục 20, quyết định 6).
//
// Hợp đồng được chia thành các giao diện nhỏ để adapter không bị ép hỗ trợ chức năng
// không tồn tại: Adapter (bắt buộc) và HookInstaller, MCPInstaller, Executor (tuỳ chọn).
package harness

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/eval"
	"bint/internal/fsafe"
	"bint/internal/harness/antigravity"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/skills"
)

// Support là mức hỗ trợ một khả năng.
type Support string

const (
	Yes     Support = "yes"
	Partial Support = "partial"
	No      Support = "no"
)

// Khoá khả năng trong bảng (thứ tự hiển thị theo AllCapabilities).
const (
	CapSkills         = "skills"
	CapHiddenSkills   = "hidden_skills"
	CapMCP            = "mcp"
	CapHooks          = "hooks"
	CapPathContext    = "path_context"
	CapProtectedPaths = "protected_paths"
	CapExecute        = "execute"
	CapReadOnly       = "readonly"
	CapCost           = "cost"
	CapTurnLimit      = "turn_limit"
	CapSkillEvents    = "skill_events"
)

// AllCapabilities liệt kê khoá khả năng cùng nhãn tiếng Việt.
var AllCapabilities = []struct{ Key, Title string }{
	{CapSkills, "Đồng bộ skill"},
	{CapHiddenSkills, "Ẩn skill / chỉ gọi tường minh"},
	{CapMCP, "MCP server của Bint"},
	{CapHooks, "Hook ghi nhận sự kiện"},
	{CapPathContext, "Chèn ngữ cảnh theo đường dẫn"},
	{CapProtectedPaths, "Hỏi người khi sửa đường dẫn bảo vệ"},
	{CapExecute, "Thực thi theo chương trình (bint run)"},
	{CapReadOnly, "Giữ bước chỉ đọc"},
	{CapCost, "Báo chi phí (áp max_cost_usd)"},
	{CapTurnLimit, "Giới hạn số lượt (max_turns)"},
	{CapSkillEvents, "Ghi nhận skill được kích hoạt (skill.activated)"},
}

// Capability là một ô trong bảng khả năng.
type Capability struct {
	Key     string  `json:"key"`
	Support Support `json:"support"`
	Detail  string  `json:"detail"`
}

// HookRequest điều khiển việc cài/gỡ hook.
type HookRequest struct {
	Root      string
	Scope     claude.Scope
	BintPath  string
	BackupDir string
	Now       time.Time
}

// HookInstaller là adapter cài được hook ghi nhận (cấp 2).
type HookInstaller interface {
	HooksPath(scope claude.Scope, root string) (string, error)
	InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error)
	UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error)
	HooksInstalled(path string) (bool, error)
	// HookEvents mô tả các sự kiện được đăng ký (cho báo cáo).
	HookEvents() string
}

// MCPRequest điều khiển việc đăng ký/gỡ MCP server.
type MCPRequest struct {
	Root      string
	BintPath  string
	BackupDir string
	Manifest  *fsafe.Manifest
	Install   bool
	Force     bool
	Now       time.Time
}

// MCPResult là kết quả đăng ký MCP.
type MCPResult struct {
	Path     string
	Changed  bool
	Conflict string
	Note     string
}

// MCPInstaller là adapter đăng ký được MCP server của Bint ở cấp dự án.
type MCPInstaller interface {
	InstallMCP(req MCPRequest) (MCPResult, error)
	MCPInstalled(root string) bool
}

// AgentOptions điều khiển việc tạo agent thực thi.
type AgentOptions struct {
	Config   config.Config
	Step     string // b-req | b-feat | b-review ("" = đánh giá)
	ReadOnly bool
	Model    string // ghi đè từ dòng lệnh
	MaxCost  *float64
	MaxTurns *int
	// Eval: tạo agent cho khung đánh giá (dùng eval.agent với Claude Code).
	Eval bool
	Bare bool
}

// Executor là adapter khởi chạy được agent theo chương trình (cấp 3).
type Executor interface {
	NewAgent(o AgentOptions) eval.Agent
}

// Adapter là phần bắt buộc của mọi harness.
type Adapter interface {
	Name() string
	// Title là tên hiển thị.
	Title() string
	// VerifiedWith là phiên bản harness đã đối chiếu bảng khả năng.
	VerifiedWith() string
	Capabilities() []Capability
	// SkillTarget là nơi xuất bản skill và định dạng của nó.
	SkillTarget() skills.Target
}

// All trả về mọi adapter theo thứ tự cố định.
func All() []Adapter { return []Adapter{Claude{}, Codex{}, Antigravity{}} }

// Get trả về adapter theo tên.
func Get(name string) (Adapter, bool) {
	for _, a := range All() {
		if a.Name() == name {
			return a, true
		}
	}
	return nil, false
}

// Names trả về tên mọi adapter.
func Names() []string {
	var out []string
	for _, a := range All() {
		out = append(out, a.Name())
	}
	return out
}

// Info là kết quả phát hiện một harness trên máy cùng bảng khả năng.
type Info struct {
	Name         string       `json:"name"`
	Title        string       `json:"title"`
	Enabled      bool         `json:"enabled"`
	Installed    bool         `json:"installed"`
	Binary       string       `json:"binary,omitempty"`
	Home         string       `json:"home,omitempty"`
	Version      string       `json:"version,omitempty"`
	VerifiedWith string       `json:"verified_with"`
	Level        int          `json:"level"`
	LevelTitle   string       `json:"level_title"`
	Capabilities []Capability `json:"capabilities"`
	SkillDir     string       `json:"skill_dir"`
}

// Level tính cấp hỗ trợ (mục 7) từ bảng khả năng: 1 = Skill (+ MCP hoặc CLI), 2 = +
// Hook, 3 = + thực thi theo chương trình. Cấp 4 (điều phối đầy đủ: nhiều agent,
// cô lập, theo dõi chi phí, thử nghiệm cải tiến) chưa adapter nào đạt ở bản này.
func Level(caps []Capability) int {
	get := func(k string) Support {
		for _, c := range caps {
			if c.Key == k {
				return c.Support
			}
		}
		return No
	}
	if get(CapSkills) == No {
		return 0
	}
	lvl := 1
	if get(CapHooks) != No {
		lvl = 2
	}
	if get(CapExecute) != No {
		lvl = 3
	}
	return lvl
}

// LevelTitles là tên các cấp hỗ trợ.
var LevelTitles = map[int]string{0: "không hỗ trợ", 1: "Skill + MCP/CLI", 2: "Skill + Hook", 3: "Programmatic Execution", 4: "Full Orchestration"}

// Describe phát hiện harness trên máy và dựng Info. versionOf (có thể nil) lấy
// phiên bản chương trình — gọi tiến trình con nên chỉ dùng khi người dùng yêu cầu.
func Describe(a Adapter, cfg config.Config, versionOf func(Adapter) string) Info {
	d := detect.Find(a.Name())
	if bin := cfg.Harnesses.Agent(a.Name()).Binary; bin != "" {
		d.Binary, d.Present = bin, usableBinary(bin)
	}
	caps := a.Capabilities()
	in := Info{Name: a.Name(), Title: a.Title(), Enabled: slices.Contains(cfg.Harnesses.Enabled, a.Name()),
		Installed: d.Present, Binary: d.Binary, Home: d.Home, VerifiedWith: a.VerifiedWith(), Capabilities: caps,
		SkillDir: a.SkillTarget().Dir}
	in.Level = Level(caps)
	in.LevelTitle = LevelTitles[in.Level]
	if versionOf != nil && d.Binary != "" {
		in.Version = versionOf(a)
	}
	return in
}

// Capability trả về một ô của bảng (No nếu adapter không khai).
func CapabilityOf(a Adapter, key string) Capability {
	for _, c := range a.Capabilities() {
		if c.Key == key {
			return c
		}
	}
	return Capability{Key: key, Support: No}
}

// SkillTargets trả về nơi xuất bản skill cho các harness được bật (gộp các harness
// dùng chung một thư mục, ví dụ Codex và Antigravity cùng đọc .agents/skills).
func SkillTargets(enabled []string) []skills.Target {
	var out []skills.Target
	seen := map[string]bool{}
	for _, name := range enabled {
		a, ok := Get(name)
		if !ok {
			continue
		}
		t := a.SkillTarget()
		if seen[t.Dir] {
			continue
		}
		seen[t.Dir] = true
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// HarnessesForTarget trả về tên các harness được bật đọc skill từ dir.
func HarnessesForTarget(enabled []string, dir string) []string {
	var out []string
	for _, name := range enabled {
		if a, ok := Get(name); ok && a.SkillTarget().Dir == dir {
			out = append(out, name)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Chọn harness theo role (mục 5: roles; mục 6.5)

// Role của một bước workflow.
func StepRole(step string) string {
	switch step {
	case "b-req":
		return "planner"
	case "b-feat":
		return "implementer"
	case "b-review":
		return "reviewer"
	}
	return ""
}

// Roles là danh sách role ghi đè được.
var Roles = []string{"planner", "implementer", "reviewer"}

// ParseRoleOverrides đọc các ghi đè dạng "role=harness" (role: planner | implementer |
// reviewer, hoặc tên bước b-req | b-feat | b-review).
func ParseRoleOverrides(specs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range specs {
		k, v, ok := strings.Cut(s, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("--role %q: cần dạng role=harness (ví dụ implementer=codex)", s)
		}
		if r := StepRole(k); r != "" {
			k = r
		}
		if !slices.Contains(Roles, k) {
			return nil, fmt.Errorf("--role %q: role không hợp lệ (%s)", s, strings.Join(Roles, ", "))
		}
		if _, ok := Get(v); !ok {
			return nil, fmt.Errorf("--role %q: harness không hỗ trợ %q (%s)", s, v, strings.Join(Names(), ", "))
		}
		out[k] = v
	}
	return out, nil
}

// Selection là các lựa chọn harness của người dùng cho một lượt (ưu tiên hơn cấu hình).
type Selection struct {
	// All: harness cho mọi bước (--harness).
	All string
	// Roles: harness theo role (--role).
	Roles map[string]string
}

// ForStep chọn harness cho bước theo thứ tự ưu tiên (mục 5): yêu cầu của người dùng
// (--role, rồi --harness) → roles trong cấu hình → harnesses.default → claude.
// Harness được chọn phải nằm trong harnesses.enabled — yêu cầu của người dùng không
// được vượt qua cấu hình dự án.
func ForStep(cfg config.Config, sel Selection, step string) (string, string, error) {
	role := StepRole(step)
	h, src := "", ""
	switch {
	case sel.Roles[role] != "":
		h, src = sel.Roles[role], "--role "+role
	case sel.All != "":
		h, src = sel.All, "--harness"
	default:
		switch role {
		case "planner":
			h = cfg.Roles.Planner
		case "implementer":
			h = cfg.Roles.Implementer
		case "reviewer":
			h = cfg.Roles.Reviewer
		}
		src = "roles." + role
		if h == "" {
			h, src = cfg.Harnesses.Default, "harnesses.default"
		}
		if h == "" {
			h, src = "claude", "mặc định"
		}
	}
	if !slices.Contains(cfg.Harnesses.Enabled, h) {
		return "", src, fmt.Errorf("harness %q (%s) chưa được bật trong harnesses.enabled của .bint/config.yml", h, src)
	}
	return h, src, nil
}

// NewAgent tạo agent thực thi cho harness name. Lỗi rõ ràng khi adapter không thực
// thi được hoặc chương trình không có trên máy (mục 17: hành vi khi adapter không khả dụng).
func NewAgent(name string, o AgentOptions) (eval.Agent, error) {
	a, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("harness không hỗ trợ %q", name)
	}
	ex, ok := a.(Executor)
	if !ok || CapabilityOf(a, CapExecute).Support == No {
		return nil, fmt.Errorf("%s không hỗ trợ thực thi theo chương trình", a.Title())
	}
	if !Available(name, o.Config) {
		return nil, &UnavailableError{Harness: name, Title: a.Title()}
	}
	return ex.NewAgent(o), nil
}

// UnavailableError: chương trình của harness không có trên máy.
type UnavailableError struct{ Harness, Title string }

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("adapter %s không khả dụng: không tìm thấy chương trình trên máy này (cài đặt, hoặc đặt harnesses.%s.binary, hoặc chọn harness khác bằng --role/--harness)", e.Title, e.Harness)
}

// Available cho biết chương trình của harness có trên máy (PATH hoặc harnesses.<h>.binary).
func Available(name string, cfg config.Config) bool {
	if bin := cfg.Harnesses.Agent(name).Binary; bin != "" {
		return usableBinary(bin)
	}
	return detect.Find(name).Binary != ""
}

func model(cli string, cfgModel *string) string {
	if cli != "" {
		return cli
	}
	if cfgModel != nil {
		return *cfgModel
	}
	return ""
}

// ---------------------------------------------------------------------------
// Claude Code

// Claude là adapter Claude Code.
type Claude struct{}

func (Claude) Name() string         { return claude.HarnessName }
func (Claude) Title() string        { return "Claude Code" }
func (Claude) VerifiedWith() string { return "Claude Code 2.x" }
func (Claude) SkillTarget() skills.Target {
	return skills.ClaudeTarget
}

func (Claude) Capabilities() []Capability {
	return []Capability{
		{CapSkills, Yes, ".claude/skills/<tên>/SKILL.md"},
		{CapHiddenSkills, Yes, "disable-model-invocation: chỉ chạy khi người gọi /b-…"},
		{CapMCP, Yes, ".mcp.json cấp dự án (bint init --mcp)"},
		{CapHooks, Yes, "SessionStart, SessionEnd, PostToolUse, PostToolUseFailure"},
		{CapPathContext, Yes, "PreToolUse trước Read/Edit/Write (additionalContext)"},
		{CapProtectedPaths, Yes, "PreToolUse permissionDecision=ask"},
		{CapExecute, Yes, "claude -p --output-format json"},
		{CapReadOnly, Yes, "--disallowedTools Edit,Write,…; Bint kiểm tra cây làm việc sau bước"},
		{CapCost, Yes, "total_cost_usd; --max-budget-usd"},
		{CapTurnLimit, Yes, "--max-turns"},
		{CapSkillEvents, Yes, "PostToolUse công cụ Skill → skill.activated"},
	}
}

func (Claude) HooksPath(scope claude.Scope, root string) (string, error) {
	return claude.SettingsPath(scope, root)
}
func (Claude) InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error) {
	return claude.InstallHooks(path, bintPath, backupDir, now)
}
func (Claude) UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error) {
	return claude.UninstallHooks(path, backupDir, now)
}
func (Claude) HooksInstalled(path string) (bool, error) { return claude.HooksInstalled(path) }
func (Claude) HookEvents() string {
	return "SessionStart, SessionEnd, PreToolUse, PostToolUse, PostToolUseFailure"
}

func (Claude) InstallMCP(req MCPRequest) (MCPResult, error) {
	p := claude.MCPConfigPath(req.Root)
	r, err := claude.InstallMCP(p, req.BintPath, req.BackupDir, req.Install, req.Now)
	return MCPResult{Path: ".mcp.json", Changed: r.Changed}, err
}
func (Claude) MCPInstalled(root string) bool { return claude.MCPInstalled(claude.MCPConfigPath(root)) }

func (Claude) NewAgent(o AgentOptions) eval.Agent {
	if o.Eval {
		return &eval.ClaudeAgent{Cfg: o.Config.Eval.Agent, Model: o.Model, MaxCostUSD: o.MaxCost, Bare: o.Bare}
	}
	return &eval.ClaudeAgent{Cfg: ClaudeWorkflowArgs(o.Config, o.ReadOnly, o.MaxTurns), Model: o.Model, MaxCostUSD: o.MaxCost}
}

// ClaudeWorkflowArgs dựng cấu hình gọi Claude Code cho một bước workflow.
func ClaudeWorkflowArgs(cfg config.Config, readOnly bool, maxTurns *int) config.EvalAgent {
	w := cfg.Workflow.Agent
	a := config.EvalAgent{Model: w.Model, PermissionMode: w.PermissionMode, SettingSources: w.SettingSources,
		AllowedTools: w.AllowedTools}
	if readOnly {
		a.AllowedTools = w.ReadonlyTools
		a.ExtraArgs = append(a.ExtraArgs, "--disallowedTools", "Edit,Write,MultiEdit,NotebookEdit")
	}
	if maxTurns != nil {
		a.ExtraArgs = append(a.ExtraArgs, "--max-turns", fmt.Sprint(*maxTurns))
	}
	a.ExtraArgs = append(a.ExtraArgs, w.ExtraArgs...)
	return a
}

// ---------------------------------------------------------------------------
// Codex

// Codex là adapter Codex CLI.
type Codex struct{}

func (Codex) Name() string               { return codex.HarnessName }
func (Codex) Title() string              { return "Codex CLI" }
func (Codex) VerifiedWith() string       { return "codex-cli 0.167" }
func (Codex) SkillTarget() skills.Target { return skills.AgentsTarget }

func (Codex) Capabilities() []Capability {
	return []Capability{
		{CapSkills, Yes, ".agents/skills/<tên>/SKILL.md (dùng chung với Antigravity)"},
		{CapHiddenSkills, Partial, "không ẩn khỏi người dùng; agents/openai.yaml policy.allow_implicit_invocation: false loại khỏi danh sách tự kích hoạt (Codex 0.167); Bint chưa sinh policy này, bỏ disable-model-invocation khi xuất bản"},
		{CapMCP, Partial, ".codex/config.toml [mcp_servers.bint] — chỉ hiệu lực khi dự án được Codex tin cậy"},
		{CapHooks, Partial, ".codex/hooks.json: SessionStart/End, Pre/PostToolUse; cần trust từng định nghĩa; kết quả shell lấy từ exit_code của transcript cùng tool_use_id, thiếu thì unknown; script phức tạp không nâng thành tín hiệu test"},
		{CapPathContext, Partial, "chỉ trước khi sửa (apply_patch); Codex đọc tệp qua shell nên không chèn khi đọc"},
		{CapProtectedPaths, Partial, "PreToolUse deny kèm lý do (không hỏi được trong codex exec); duyệt trước tệp chưa có diff bằng bint task approve <id> --path <tệp>, rồi resume"},
		{CapExecute, Yes, "codex exec --json, đề bài qua stdin"},
		{CapReadOnly, Partial, "sandbox workspace-write (bint CLI cần ghi thư mục trạng thái); Bint kiểm tra cây làm việc sau bước"},
		{CapCost, No, "chỉ báo token; max_cost_usd chỉ là chỉ dẫn"},
		{CapTurnLimit, No, "không có cờ giới hạn lượt; max_turns chỉ là chỉ dẫn"},
		{CapSkillEvents, No, "skill được chọn ngầm, không có công cụ gọi skill đi qua hook: không ghi nhận (không tổng hợp giả)"},
	}
}

func (Codex) HooksPath(scope claude.Scope, root string) (string, error) {
	return codex.HooksPath(scope, root)
}
func (Codex) InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error) {
	return codex.InstallHooks(path, bintPath, backupDir, now)
}
func (Codex) UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error) {
	return codex.UninstallHooks(path, backupDir, now)
}
func (Codex) HooksInstalled(path string) (bool, error) { return codex.HooksInstalled(path) }
func (Codex) HookEvents() string {
	return "SessionStart, SessionEnd, PreToolUse(apply_patch), PostToolUse"
}

func (Codex) InstallMCP(req MCPRequest) (MCPResult, error) {
	r, err := codex.InstallMCP(req.Root, req.BintPath, req.BackupDir, req.Manifest, req.Install, req.Force, req.Now)
	res := MCPResult{Path: ".codex/config.toml", Changed: r.Outcome == fsafe.Created || r.Outcome == fsafe.Appended || r.Outcome == fsafe.Updated}
	if r.Outcome == fsafe.Conflict {
		res.Conflict = r.Detail
	}
	if req.Install && !codex.ProjectTrusted(req.Root) {
		res.Note = "dự án chưa được Codex tin cậy: .codex/config.toml chỉ có hiệu lực sau khi mở codex trong repository và chọn tin cậy"
	}
	return res, err
}
func (Codex) MCPInstalled(root string) bool { return codex.MCPInstalled(root) }

func (Codex) NewAgent(o AgentOptions) eval.Agent {
	h := o.Config.Harnesses.Codex
	return &eval.CodexAgent{Binary: h.Binary, Model: model(o.Model, h.Model), ExtraArgs: h.ExtraArgs}
}

// ---------------------------------------------------------------------------
// Antigravity

// Antigravity là adapter Antigravity CLI.
type Antigravity struct{}

func (Antigravity) Name() string               { return antigravity.HarnessName }
func (Antigravity) Title() string              { return "Antigravity CLI" }
func (Antigravity) VerifiedWith() string       { return "agy 1.5.3" }
func (Antigravity) SkillTarget() skills.Target { return skills.AgentsTarget }

func (Antigravity) Capabilities() []Capability {
	return []Capability{
		{CapSkills, Yes, ".agents/skills/<tên>/SKILL.md (dùng chung với Codex)"},
		{CapHiddenSkills, No, "frontmatter chỉ có name, description; không ẩn được"},
		{CapMCP, No, "chỉ cấu hình ở cấp người dùng/plugin; agent dùng CLI bint theo hướng dẫn trong skill"},
		{CapHooks, Partial, ".agents/hooks.json: SessionStart, PreToolUse, PostToolUse; không có sự kiện kết thúc phiên; tên công cụ theo tài liệu, chưa đo trực tiếp"},
		{CapPathContext, No, "PreToolUse chỉ trả quyết định (allow/deny/ask), không chèn được ngữ cảnh"},
		{CapProtectedPaths, Yes, "PreToolUse decision=ask"},
		{CapExecute, Yes, "agy --print --output-format json"},
		{CapReadOnly, Partial, "--mode plan; Bint kiểm tra cây làm việc sau bước"},
		{CapCost, No, "chỉ báo token; max_cost_usd chỉ là chỉ dẫn"},
		{CapTurnLimit, No, "không có cờ giới hạn lượt; max_turns chỉ là chỉ dẫn"},
		{CapSkillEvents, No, "skill được chọn ngầm, không có công cụ gọi skill đi qua hook: không ghi nhận (không tổng hợp giả)"},
	}
}

func (Antigravity) HooksPath(scope claude.Scope, root string) (string, error) {
	return antigravity.HooksPath(scope, root)
}
func (Antigravity) InstallHooks(path, bintPath, backupDir string, now time.Time) (claude.InstallResult, error) {
	return antigravity.InstallHooks(path, bintPath, backupDir, now)
}
func (Antigravity) UninstallHooks(path, backupDir string, now time.Time) (claude.InstallResult, error) {
	return antigravity.UninstallHooks(path, backupDir, now)
}
func (Antigravity) HooksInstalled(path string) (bool, error) { return antigravity.HooksInstalled(path) }
func (Antigravity) HookEvents() string                       { return "SessionStart, PreToolUse, PostToolUse" }

func (Antigravity) NewAgent(o AgentOptions) eval.Agent {
	h := o.Config.Harnesses.Antigravity
	return &eval.AgyAgent{Binary: h.Binary, Model: model(o.Model, h.Model), ExtraArgs: h.ExtraArgs}
}

// usableBinary: đường dẫn là tệp thường và (trên Unix) có quyền thực thi.
func usableBinary(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode().Perm()&0o111 != 0
}
