// Package config định nghĩa schema cấu hình v1 của Bint (.bint/config.yml),
// giá trị mặc định, nạp theo lớp và kiểm tra hợp lệ.
//
// Thứ tự ghép lớp (lớp sau ghi đè lớp trước):
//
//	mặc định → cấu hình toàn cục (<UserConfigDir>/bint/config.yml)
//	         → cấu hình dự án (.bint/config.yml)
//	         → cấu hình cục bộ máy cá nhân (.bint/local.yml, không commit)
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion là phiên bản schema cấu hình hiện hành.
const SchemaVersion = 1

const (
	DirName       = ".bint"
	FileName      = "config.yml"
	LocalFileName = "local.yml"
)

// KnownHarnesses là các harness mà schema v1 chấp nhận.
var KnownHarnesses = []string{"claude", "codex", "antigravity"}

// WorkflowNames là các workflow gốc (mục 6).
var WorkflowNames = []string{"b-do", "b-req", "b-feat", "b-review"}

var knowledgeKinds = []string{"rule", "fact", "pattern", "decision", "lesson", "warning"}

type Config struct {
	Version   int       `yaml:"version"`
	Project   Project   `yaml:"project"`
	State     State     `yaml:"state"`
	Harnesses Harnesses `yaml:"harnesses"`
	Roles     Roles     `yaml:"roles"`
	Context   Context   `yaml:"context"`
	Knowledge Knowledge `yaml:"knowledge"`
	Memory    Memory    `yaml:"memory"`
	Eval      Eval      `yaml:"eval"`
	Evolution Evolution `yaml:"evolution"`
	Workflow  Workflow  `yaml:"workflow"`
	Limits    Limits    `yaml:"limits"`
	Security  Security  `yaml:"security"`
}

type Project struct {
	Name string `yaml:"name"`
	Root string `yaml:"root"`
}

type State struct {
	// Dir là "auto" hoặc đường dẫn tuyệt đối tới thư mục trạng thái của repository này.
	Dir string `yaml:"dir"`
}

type Harnesses struct {
	Enabled []string `yaml:"enabled"`
	Default string   `yaml:"default"`
	// Tham số gọi harness khi Bint điều phối (bint run, bint eval). Claude Code dùng
	// workflow.agent / eval.agent (có quyền theo công cụ); Codex và Antigravity dùng
	// các khoá dưới đây.
	Codex       HarnessAgent `yaml:"codex"`
	Antigravity HarnessAgent `yaml:"antigravity"`
}

// HarnessAgent là tham số gọi một harness không phải Claude Code.
type HarnessAgent struct {
	// Binary: đường dẫn chương trình (rỗng = tìm trong PATH).
	Binary string `yaml:"binary"`
	// Model: null = mặc định của harness.
	Model     *string  `yaml:"model"`
	ExtraArgs []string `yaml:"extra_args"`
}

// Agent trả về tham số của harness h (rỗng với claude).
func (h Harnesses) Agent(name string) HarnessAgent {
	switch name {
	case "codex":
		return h.Codex
	case "antigravity":
		return h.Antigravity
	}
	return HarnessAgent{}
}

type Roles struct {
	Planner     string `yaml:"planner"`
	Implementer string `yaml:"implementer"`
	Reviewer    string `yaml:"reviewer"`
	Evolver     string `yaml:"evolver"`
}

type Context struct {
	MaxTokens  int              `yaml:"max_tokens"`
	Interfaces ContextInterface `yaml:"interfaces"`
	Retrieval  Retrieval        `yaml:"retrieval"`
	Freshness  Freshness        `yaml:"freshness"`
	// PathMaxItems giới hạn số mục được chèn cho một lần truy xuất theo đường dẫn.
	PathMaxItems int `yaml:"path_max_items"`
	// PathHookTimeoutMS: hook chèn theo đường dẫn quá hạn thì bỏ qua, không chặn agent.
	PathHookTimeoutMS int `yaml:"path_hook_timeout_ms"`
}

type ContextInterface struct {
	MCP       bool `yaml:"mcp"`
	PathHooks bool `yaml:"path_hooks"`
	CLI       bool `yaml:"cli"`
}

type Retrieval struct {
	Lexical    bool `yaml:"lexical"`
	Structural bool `yaml:"structural"`
	Semantic   bool `yaml:"semantic"`
}

type Freshness struct {
	VerifyChangedSources bool   `yaml:"verify_changed_sources"`
	Fingerprint          string `yaml:"fingerprint"`
}

type Knowledge struct {
	RequireHumanApprovalFor []string `yaml:"require_human_approval_for"`
	// AgentsMDMaxItems giới hạn số mục tri thức trong khối Bint của AGENTS.md (mục 8.5).
	AgentsMDMaxItems int `yaml:"agents_md_max_items"`
	// StaleDeprecateDays: mục stale quá số ngày này mà chưa được xác thực lại thì
	// được đề xuất chuyển sang deprecated (mục 8.4); Bint không tự chuyển.
	StaleDeprecateDays int `yaml:"stale_deprecate_days"`
}

type Memory struct {
	Enabled       bool   `yaml:"enabled"`
	Scope         string `yaml:"scope"`
	RetentionDays int    `yaml:"retention_days"`
}

type Eval struct {
	TestCommand  *string `yaml:"test_command"`
	SetupCommand *string `yaml:"setup_command"`
	// Suggested* do `bint init` phát hiện; chỉ là đề xuất, Bint không bao giờ chạy
	// chúng cho tới khi người dùng chép sang test_command/setup_command.
	SuggestedTestCommand  *string  `yaml:"suggested_test_command,omitempty"`
	SuggestedSetupCommand *string  `yaml:"suggested_setup_command,omitempty"`
	Repeats               int      `yaml:"repeats"`
	Arms                  []string `yaml:"arms"`
	LeakageMode           string   `yaml:"leakage_mode"`
	BudgetUSD             *float64 `yaml:"budget_usd"`
	// Harness dùng để chạy tác vụ đánh giá (claude | codex | antigravity).
	Harness        string `yaml:"harness"`
	TimeoutMinutes int    `yaml:"timeout_minutes"`
	MinimalContext string `yaml:"minimal_context"`
	// MinimalSource: "knowledge" = AGENTS.md của nhánh minimal dựng từ tri thức
	// (P2); "file" = dùng tệp viết tay minimal_context (P1).
	MinimalSource string `yaml:"minimal_source"`
	// MinimalIncludeCandidates đưa cả mục chưa duyệt vào nhánh minimal (luôn bật với
	// ảnh chụp tri thức do `bint eval bootstrap` sinh).
	MinimalIncludeCandidates bool      `yaml:"minimal_include_candidates"`
	Agent                    EvalAgent `yaml:"agent"`
	Mine                     EvalMine  `yaml:"mine"`
}

// EvalAgent là tham số gọi harness khi chạy đánh giá.
type EvalAgent struct {
	Model          *string  `yaml:"model"`
	PermissionMode string   `yaml:"permission_mode"`
	AllowedTools   []string `yaml:"allowed_tools"`
	SettingSources string   `yaml:"setting_sources"`
	ExtraArgs      []string `yaml:"extra_args"`
}

// EvalMine là tiêu chí chọn commit làm tác vụ đánh giá (mục 12.2).
type EvalMine struct {
	MaxCommits      int      `yaml:"max_commits"`
	MinSourceLines  int      `yaml:"min_source_lines"`
	MaxSourceLines  int      `yaml:"max_source_lines"`
	MaxFiles        int      `yaml:"max_files"`
	MinMessageChars int      `yaml:"min_message_chars"`
	TestPatterns    []string `yaml:"test_patterns"`
	IgnorePatterns  []string `yaml:"ignore_patterns"`
}

// Limits là giới hạn vận hành (mục 5).
type Limits struct {
	PerRun PerRunLimits `yaml:"per_run"`
	// Workflows ghi đè per_run cho từng workflow (b-do, b-req, b-feat, b-review).
	// Với b-do, giới hạn áp cho tổng mọi bước của một lượt điều phối.
	Workflows      map[string]PerRunLimits `yaml:"workflows"`
	ProtectedPaths []string                `yaml:"protected_paths"`
}

// ForWorkflow trả về giới hạn của một workflow: giá trị riêng nếu có, không thì per_run.
func (l Limits) ForWorkflow(name string) PerRunLimits {
	out := l.PerRun
	if w, ok := l.Workflows[name]; ok {
		if w.MaxCostUSD != nil {
			out.MaxCostUSD = w.MaxCostUSD
		}
		if w.MaxTurns != nil {
			out.MaxTurns = w.MaxTurns
		}
	}
	return out
}

// Workflow cấu hình các workflow gốc do Bint điều phối (`bint run`, mục 6).
type Workflow struct {
	// MaxReviewRounds: số vòng b-review → b-feat tối đa của b-do trước khi dừng chờ người.
	MaxReviewRounds int `yaml:"max_review_rounds"`
	// TimeoutMinutes giới hạn thời gian mỗi bước agent.
	TimeoutMinutes int `yaml:"timeout_minutes"`
	// VerifyTimeoutMinutes giới hạn thời gian lệnh kiểm chứng của phiếu tác vụ.
	VerifyTimeoutMinutes int           `yaml:"verify_timeout_minutes"`
	Agent                WorkflowAgent `yaml:"agent"`
}

// WorkflowAgent là tham số gọi harness cho các bước workflow.
type WorkflowAgent struct {
	Model          *string `yaml:"model"`
	PermissionMode string  `yaml:"permission_mode"`
	// AllowedTools cho bước có quyền ghi (b-feat).
	AllowedTools []string `yaml:"allowed_tools"`
	// ReadonlyTools cho bước chỉ đọc (b-req, b-review); công cụ sửa tệp bị cấm thêm
	// bằng --disallowedTools.
	ReadonlyTools  []string `yaml:"readonly_tools"`
	SettingSources string   `yaml:"setting_sources"`
	ExtraArgs      []string `yaml:"extra_args"`
}

type PerRunLimits struct {
	MaxCostUSD *float64 `yaml:"max_cost_usd"`
	MaxTurns   *int     `yaml:"max_turns"`
}

type Evolution struct {
	Enabled    bool                `yaml:"enabled"`
	Mode       string              `yaml:"mode"`
	Trigger    EvolutionTrigger    `yaml:"trigger"`
	Evaluation EvolutionEvaluation `yaml:"evaluation"`
	Adoption   EvolutionAdoption   `yaml:"adoption"`
}

type EvolutionTrigger struct {
	MinSimilarRuns int `yaml:"min_similar_runs"`
	MinNewEvidence int `yaml:"min_new_evidence"`
}

type EvolutionEvaluation struct {
	Enabled       bool `yaml:"enabled"`
	MaxCandidates int  `yaml:"max_candidates"`
	// Metric là thước đo chính, chọn trước khi đánh giá (mục 12.7): pass_rate |
	// cost_usd | turns. Đề xuất loại bỏ tri thức luôn được đo theo "không kém" về tỷ lệ đạt.
	Metric string `yaml:"metric"`
	// MinEffect: mức cải thiện tỷ lệ đạt muốn phát hiện, dùng ước tính cỡ mẫu (mục 12.5).
	MinEffect float64 `yaml:"min_effect"`
	// NonInferiorityMargin: tỷ lệ đạt được phép giảm tối đa (cận dưới CI95) khi thước đo
	// chính không phải tỷ lệ đạt, hoặc khi loại bỏ tri thức.
	NonInferiorityMargin float64 `yaml:"non_inferiority_margin"`
	// Repeats: số lần lặp mỗi nhánh (0 = eval.repeats).
	Repeats int `yaml:"repeats"`
	// BudgetUSD: ngân sách đã duyệt cho một lần đối chiếu (Probe); null = chưa duyệt.
	BudgetUSD *float64 `yaml:"budget_usd"`
	// Delivery: cách đưa tri thức tới agent ở cả hai nhánh: bint (Context Engine) | minimal (AGENTS.md).
	Delivery string `yaml:"delivery"`
	// LeakageMode: strict | economy (mục 12.3) cho đánh giá đề xuất.
	LeakageMode string `yaml:"leakage_mode"`
}

type EvolutionAdoption struct {
	AutoLowRisk        bool `yaml:"auto_low_risk"`
	BindToModelVersion bool `yaml:"bind_to_model_version"`
}

type Security struct {
	AllowExternalAgents        bool `yaml:"allow_external_agents"`
	AllowNetworkInEvals        bool `yaml:"allow_network_in_evals"`
	RequireApprovalForCommands bool `yaml:"require_approval_for_commands"`
}

// Default trả về cấu hình mặc định của Bint.
func Default() Config {
	return Config{
		Version: SchemaVersion,
		Project: Project{Root: "auto"},
		State:   State{Dir: "auto"},
		Harnesses: Harnesses{
			Enabled:     []string{"claude"},
			Default:     "claude",
			Codex:       HarnessAgent{ExtraArgs: []string{}},
			Antigravity: HarnessAgent{ExtraArgs: []string{}},
		},
		Roles: Roles{Planner: "claude", Implementer: "claude", Reviewer: "claude", Evolver: "claude"},
		Context: Context{
			MaxTokens:         6000,
			Interfaces:        ContextInterface{MCP: true, PathHooks: true, CLI: true},
			Retrieval:         Retrieval{Lexical: true, Structural: true},
			Freshness:         Freshness{VerifyChangedSources: true, Fingerprint: "symbol"},
			PathMaxItems:      5,
			PathHookTimeoutMS: 1500,
		},
		Knowledge: Knowledge{RequireHumanApprovalFor: []string{"rule"}, AgentsMDMaxItems: 15, StaleDeprecateDays: 30},
		Memory:    Memory{Enabled: true, Scope: "project", RetentionDays: 90},
		Eval: Eval{
			Repeats:        3,
			Arms:           []string{"none", "minimal", "bint"},
			LeakageMode:    "strict",
			Harness:        "claude",
			TimeoutMinutes: 30,
			MinimalContext: ".bint/evals/minimal.md",
			MinimalSource:  "knowledge",
			Agent: EvalAgent{
				PermissionMode: "acceptEdits",
				AllowedTools:   []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash"},
				SettingSources: "project,local",
				ExtraArgs:      []string{},
			},
			Mine: EvalMine{
				MaxCommits: 2000, MinSourceLines: 3, MaxSourceLines: 400, MaxFiles: 12, MinMessageChars: 20,
				TestPatterns: []string{}, IgnorePatterns: []string{},
			},
		},
		Evolution: Evolution{
			Enabled: true,
			Mode:    "propose",
			Trigger: EvolutionTrigger{MinSimilarRuns: 3, MinNewEvidence: 2},
			Evaluation: EvolutionEvaluation{Enabled: true, MaxCandidates: 2, Metric: "pass_rate", MinEffect: 0.10,
				NonInferiorityMargin: 0.05, Delivery: "bint", LeakageMode: "economy"},
			Adoption: EvolutionAdoption{BindToModelVersion: true},
		},
		Workflow: Workflow{
			MaxReviewRounds: 2, TimeoutMinutes: 60, VerifyTimeoutMinutes: 15,
			Agent: WorkflowAgent{
				PermissionMode: "acceptEdits",
				AllowedTools:   []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash"},
				ReadonlyTools:  []string{"Read", "Glob", "Grep", "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)"},
				ExtraArgs:      []string{},
			},
		},
		Limits:   Limits{Workflows: map[string]PerRunLimits{}, ProtectedPaths: []string{"migrations/**", ".github/workflows/**"}},
		Security: Security{RequireApprovalForCommands: true},
	}
}

// Sources ghi lại các tệp đã được ghép vào cấu hình cuối cùng (rỗng nếu không có).
type Sources struct {
	Global  string
	Project string
	Local   string
}

// ProjectPath trả về đường dẫn .bint/config.yml của repository.
func ProjectPath(root string) string { return filepath.Join(root, DirName, FileName) }

// LocalPath trả về đường dẫn .bint/local.yml của repository.
func LocalPath(root string) string { return filepath.Join(root, DirName, LocalFileName) }

// GlobalPath trả về đường dẫn cấu hình toàn cục. BINT_CONFIG_HOME ghi đè thư mục.
func GlobalPath() string {
	if d := os.Getenv("BINT_CONFIG_HOME"); d != "" {
		return filepath.Join(d, FileName)
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "bint", FileName)
}

// ErrNoProjectConfig: repository chưa được khởi tạo Bint.
var ErrNoProjectConfig = errors.New("chưa có .bint/config.yml (chạy `bint init`)")

// Load nạp cấu hình đã ghép lớp cho repository có thư mục gốc root.
// Nếu requireProject là true mà thiếu .bint/config.yml thì trả ErrNoProjectConfig;
// nếu false thì lớp dự án dùng giá trị mặc định.
func Load(root string, requireProject bool) (Config, Sources, error) {
	cfg := Default()
	var src Sources
	if g := GlobalPath(); g != "" {
		ok, err := mergeFile(&cfg, g)
		if err != nil {
			return cfg, src, err
		}
		if ok {
			src.Global = g
		}
	}
	p := ProjectPath(root)
	ok, err := mergeFile(&cfg, p)
	if err != nil {
		return cfg, src, err
	}
	if ok {
		src.Project = p
	} else if requireProject {
		return cfg, src, ErrNoProjectConfig
	}
	l := LocalPath(root)
	ok, err = mergeFile(&cfg, l)
	if err != nil {
		return cfg, src, err
	}
	if ok {
		src.Local = l
	}
	if err := cfg.Validate(); err != nil {
		return cfg, src, err
	}
	return cfg, src, nil
}

// LoadFirst giống Load nhưng dùng thư mục đầu tiên trong roots có
// .bint/config.yml. Nếu không thư mục nào có, dùng roots[0].
func LoadFirst(roots []string, requireProject bool) (Config, Sources, error) {
	for _, r := range roots {
		if _, err := os.Stat(ProjectPath(r)); err == nil {
			return Load(r, requireProject)
		}
	}
	return Load(roots[0], requireProject)
}

func mergeFile(cfg *Config, path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := Decode(bytes.NewReader(data), cfg); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

// Decode giải mã YAML vào cfg (chỉ ghi đè các trường có mặt), từ chối trường lạ
// để bắt lỗi gõ sai tên khoá.
func Decode(r io.Reader, cfg *Config) error {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Validate kiểm tra các ràng buộc của schema v1 và trả về mọi vấn đề cùng lúc.
func (c Config) Validate() error {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if c.Version != SchemaVersion {
		add("version: cần %d, nhận %d", SchemaVersion, c.Version)
	}
	if c.State.Dir == "" {
		add(`state.dir: không được rỗng (dùng "auto" hoặc đường dẫn tuyệt đối)`)
	} else if c.State.Dir != "auto" && !filepath.IsAbs(c.State.Dir) {
		add(`state.dir: phải là "auto" hoặc đường dẫn tuyệt đối, nhận %q`, c.State.Dir)
	}
	if len(c.Harnesses.Enabled) == 0 {
		add("harnesses.enabled: cần ít nhất một harness")
	}
	for _, h := range c.Harnesses.Enabled {
		if !slices.Contains(KnownHarnesses, h) {
			add("harnesses.enabled: harness không hỗ trợ %q (hỗ trợ: %s)", h, strings.Join(KnownHarnesses, ", "))
		}
	}
	if c.Harnesses.Default != "" && !slices.Contains(c.Harnesses.Enabled, c.Harnesses.Default) {
		add("harnesses.default: %q không nằm trong harnesses.enabled", c.Harnesses.Default)
	}
	roles := []struct{ name, v string }{
		{"planner", c.Roles.Planner}, {"implementer", c.Roles.Implementer},
		{"reviewer", c.Roles.Reviewer}, {"evolver", c.Roles.Evolver},
	}
	for _, r := range roles {
		if r.v != "" && !slices.Contains(KnownHarnesses, r.v) {
			add("roles.%s: harness không hỗ trợ %q", r.name, r.v)
		}
	}
	if c.Context.MaxTokens <= 0 {
		add("context.max_tokens: phải > 0")
	}
	if c.Context.PathMaxItems < 1 || c.Context.PathMaxItems > 30 {
		add("context.path_max_items: phải trong khoảng 1..30")
	}
	if c.Context.PathHookTimeoutMS < 100 || c.Context.PathHookTimeoutMS > 10000 {
		add("context.path_hook_timeout_ms: phải trong khoảng 100..10000")
	}
	if c.Knowledge.StaleDeprecateDays < 1 {
		add("knowledge.stale_deprecate_days: phải >= 1")
	}
	if !slices.Contains([]string{"symbol", "hunk", "file"}, c.Context.Freshness.Fingerprint) {
		add("context.freshness.fingerprint: phải là symbol | hunk | file")
	}
	for _, k := range c.Knowledge.RequireHumanApprovalFor {
		if !slices.Contains(knowledgeKinds, k) {
			add("knowledge.require_human_approval_for: loại tri thức không hợp lệ %q", k)
		}
	}
	if !slices.Contains(c.Knowledge.RequireHumanApprovalFor, "rule") {
		// Mục 15.5: chỉ người mới nâng được quy tắc bắt buộc; không cho tắt bằng cấu hình.
		add(`knowledge.require_human_approval_for: bắt buộc chứa "rule"`)
	}
	if c.Knowledge.AgentsMDMaxItems < 1 || c.Knowledge.AgentsMDMaxItems > 50 {
		add("knowledge.agents_md_max_items: phải trong khoảng 1..50 (AGENTS.md cần ngắn)")
	}
	if c.Memory.Scope != "project" {
		add(`memory.scope: v1 chỉ hỗ trợ "project"`)
	}
	if c.Memory.RetentionDays < 0 {
		add("memory.retention_days: không được âm")
	}
	if c.Eval.Repeats < 1 {
		add("eval.repeats: phải >= 1")
	}
	for _, a := range c.Eval.Arms {
		if !slices.Contains([]string{"none", "minimal", "bint", "ideal"}, a) {
			add("eval.arms: nhánh không hợp lệ %q", a)
		}
	}
	if !slices.Contains([]string{"strict", "economy"}, c.Eval.LeakageMode) {
		add("eval.leakage_mode: phải là strict | economy")
	}
	if c.Eval.BudgetUSD != nil && *c.Eval.BudgetUSD < 0 {
		add("eval.budget_usd: không được âm")
	}
	if !slices.Contains(KnownHarnesses, c.Eval.Harness) {
		add("eval.harness: harness không hỗ trợ %q (hỗ trợ: %s; --agent-command cho tác nhân tuỳ chỉnh)", c.Eval.Harness, strings.Join(KnownHarnesses, ", "))
	}
	if c.Eval.TimeoutMinutes < 1 {
		add("eval.timeout_minutes: phải >= 1")
	}
	if c.Eval.MinimalContext == "" || filepath.IsAbs(c.Eval.MinimalContext) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(c.Eval.MinimalContext)), "../") {
		add("eval.minimal_context: phải là đường dẫn tương đối trong repository")
	}
	if !slices.Contains([]string{"knowledge", "file"}, c.Eval.MinimalSource) {
		add("eval.minimal_source: phải là knowledge | file")
	}
	if !slices.Contains([]string{"acceptEdits", "auto", "dontAsk", "bypassPermissions"}, c.Eval.Agent.PermissionMode) {
		add("eval.agent.permission_mode: phải là acceptEdits | auto | dontAsk | bypassPermissions")
	}
	m := c.Eval.Mine
	if m.MaxCommits < 1 || m.MaxFiles < 1 || m.MinSourceLines < 1 || m.MaxSourceLines < m.MinSourceLines || m.MinMessageChars < 0 {
		add("eval.mine: cần max_commits, max_files, min_source_lines >= 1 và max_source_lines >= min_source_lines")
	}
	if c.Limits.PerRun.MaxCostUSD != nil && *c.Limits.PerRun.MaxCostUSD <= 0 {
		add("limits.per_run.max_cost_usd: phải > 0")
	}
	if c.Limits.PerRun.MaxTurns != nil && *c.Limits.PerRun.MaxTurns < 1 {
		add("limits.per_run.max_turns: phải >= 1")
	}
	for name, w := range c.Limits.Workflows {
		if !slices.Contains(WorkflowNames, name) {
			add("limits.workflows: workflow không hợp lệ %q (%s)", name, strings.Join(WorkflowNames, ", "))
		}
		if w.MaxCostUSD != nil && *w.MaxCostUSD <= 0 {
			add("limits.workflows.%s.max_cost_usd: phải > 0", name)
		}
		if w.MaxTurns != nil && *w.MaxTurns < 1 {
			add("limits.workflows.%s.max_turns: phải >= 1", name)
		}
	}
	if c.Workflow.MaxReviewRounds < 0 || c.Workflow.MaxReviewRounds > 10 {
		add("workflow.max_review_rounds: phải trong khoảng 0..10")
	}
	if c.Workflow.TimeoutMinutes < 1 || c.Workflow.VerifyTimeoutMinutes < 1 {
		add("workflow.timeout_minutes, workflow.verify_timeout_minutes: phải >= 1")
	}
	if !slices.Contains([]string{"acceptEdits", "auto", "dontAsk", "bypassPermissions"}, c.Workflow.Agent.PermissionMode) {
		add("workflow.agent.permission_mode: phải là acceptEdits | auto | dontAsk | bypassPermissions")
	}
	if !slices.Contains([]string{"propose", "auto"}, c.Evolution.Mode) {
		add("evolution.mode: phải là propose | auto")
	}
	{
		ev := c.Evolution.Evaluation
		if ev.MaxCandidates < 1 {
			add("evolution.evaluation.max_candidates: phải >= 1")
		}
		if !slices.Contains([]string{"pass_rate", "cost_usd", "turns"}, ev.Metric) {
			add("evolution.evaluation.metric: phải là pass_rate | cost_usd | turns")
		}
		if ev.MinEffect <= 0 || ev.MinEffect >= 1 {
			add("evolution.evaluation.min_effect: phải trong khoảng (0, 1)")
		}
		if ev.NonInferiorityMargin < 0 || ev.NonInferiorityMargin >= 1 {
			add("evolution.evaluation.non_inferiority_margin: phải trong khoảng [0, 1)")
		}
		if ev.Repeats < 0 {
			add("evolution.evaluation.repeats: phải >= 0")
		}
		if ev.BudgetUSD != nil && *ev.BudgetUSD <= 0 {
			add("evolution.evaluation.budget_usd: phải > 0 hoặc null")
		}
		if !slices.Contains([]string{"bint", "minimal"}, ev.Delivery) {
			add("evolution.evaluation.delivery: phải là bint | minimal")
		}
		if !slices.Contains([]string{"strict", "economy"}, ev.LeakageMode) {
			add("evolution.evaluation.leakage_mode: phải là strict | economy")
		}
	}
	if len(errs) > 0 {
		slices.Sort(errs)
		return &ValidationError{Problems: errs}
	}
	return nil
}

// ValidationError liệt kê mọi vấn đề của cấu hình.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "cấu hình không hợp lệ:\n  - " + strings.Join(e.Problems, "\n  - ")
}
