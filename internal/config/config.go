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
	UI        UI        `yaml:"ui"`

	// contextBase: Context khi KHÔNG có strategy.yml (nil nếu không có strategy được áp).
	// Đo lường (eval) cần cấu hình gốc để nhánh không-strategy không bị strategy chi phối.
	contextBase *Context
}

// BaseContext trả về cấu hình ngữ cảnh chưa trộn strategy.yml (H1): nhánh đo `harness=off`
// và nhánh base của đề xuất phải chạy với giá trị gốc, chỉ tập strategy của nhánh mới được áp.
func (c Config) BaseContext() Context {
	if c.contextBase != nil {
		return *c.contextBase
	}
	return c.Context
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
	// workflow.agent / eval.agent (có quyền theo công cụ) và chỉ khai model theo role ở
	// đây; Codex và Antigravity dùng các khoá dưới đây.
	Claude      ClaudeHarness `yaml:"claude"`
	Codex       HarnessAgent  `yaml:"codex"`
	Antigravity HarnessAgent  `yaml:"antigravity"`
}

// RoleNames là các role của workflow (thứ tự hiển thị).
var RoleNames = []string{"planner", "implementer", "reviewer", "evolver", "reflector"}

// RoleModels là model của từng role khi role đó chạy trên một harness. Rỗng = dùng
// model chung của harness (workflow.agent.model với Claude, harnesses.<h>.model với
// Codex/Antigravity), rồi mặc định của harness.
type RoleModels struct {
	Planner     string `yaml:"planner"`
	Implementer string `yaml:"implementer"`
	Reviewer    string `yaml:"reviewer"`
	Evolver     string `yaml:"evolver"`
	// Reflector rút bài học từ thất bại (bint evolve propose, mục E2); tách khỏi evolver
	// vì nó chỉ đọc bản tóm tắt và nên dùng model mạnh nhất.
	Reflector string `yaml:"reflector"`
}

// For trả về model của role ("" nếu không đặt hoặc role lạ).
func (m RoleModels) For(role string) string {
	switch role {
	case "planner":
		return m.Planner
	case "implementer":
		return m.Implementer
	case "reviewer":
		return m.Reviewer
	case "evolver":
		return m.Evolver
	case "reflector":
		return m.Reflector
	}
	return ""
}

// ClaudeHarness là phần cấu hình riêng của Claude Code dưới harnesses.claude.
type ClaudeHarness struct {
	Models RoleModels `yaml:"models"`
}

// Models trả về bảng model theo role của harness name.
func (h Harnesses) Models(name string) RoleModels {
	switch name {
	case "claude":
		return h.Claude.Models
	case "codex":
		return h.Codex.Models
	case "antigravity":
		return h.Antigravity.Models
	}
	return RoleModels{}
}

// HarnessAgent là tham số gọi một harness không phải Claude Code.
type HarnessAgent struct {
	// Binary: đường dẫn chương trình (rỗng = tìm trong PATH).
	Binary string `yaml:"binary"`
	// Model: null = mặc định của harness.
	Model     *string  `yaml:"model"`
	ExtraArgs []string `yaml:"extra_args"`
	// Models: model theo role (ưu tiên hơn Model).
	Models RoleModels `yaml:"models"`
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
	Reflector   string `yaml:"reflector"`
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
	// Lessons: thư viện bài học từ thất bại (mục E5), có ngân sách token riêng.
	Lessons Lessons `yaml:"lessons"`
}

// Lessons cấu hình cách chèn bài học từ thất bại vào ngữ cảnh.
type Lessons struct {
	// GlobalK: số bài học chung (scope global) luôn được chèn mỗi lượt; 0 = không chèn.
	GlobalK int `yaml:"global_k"`
	// MaxTokens: ngân sách token của toàn bộ phần bài học, tách khỏi ngân sách tri thức;
	// 0 = tắt phần bài học.
	MaxTokens int `yaml:"max_tokens"`
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
	// PriorRunUSD: chi phí tiên nghiệm mỗi lượt khi chưa có lịch sử đo được (lượt đầu của
	// đợt đầu tiên). Chọn thận trọng ($0,50) để lượt đầu không chạy lọt ngân sách nhỏ; từ
	// khi có lượt thật thì dùng chi phí đo được. 0 = tắt (không chặn lượt đầu).
	PriorRunUSD float64 `yaml:"prior_run_usd"`
	// Kho đệm commit gốc (<eval-work>/bases). Mặc định dọn khi mọi lượt dùng base đã xong.
	// KeepBases giữ kho giữa các đợt (nhanh hơn, tốn đĩa: ~60 MB/base trên monorepo).
	KeepBases bool `yaml:"keep_bases"`
	// BaseCacheMaxMB: khi KeepBases, trần tổng dung lượng kho đệm (xoá kho dùng lâu nhất
	// trước); 0 = không giới hạn.
	BaseCacheMaxMB int `yaml:"base_cache_max_mb"`
	// BaseDepth: số commit nạp cho kho đệm; 0 = đầy đủ lịch sử (mặc định, để số liệu so
	// sánh được với các đợt cũ). > 0 nạp nông, nhỏ hơn nhiều nhưng agent mất `git log` sâu.
	BaseDepth int `yaml:"base_depth"`
	// MinFreeMB: dung lượng trống tối thiểu còn lại sau khi tạo một kho đệm; thiếu thì dừng
	// có thông báo thay vì làm đầy đĩa giữa đợt chạy.
	MinFreeMB int `yaml:"min_free_mb"`
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
	Enabled bool   `yaml:"enabled"`
	Mode    string `yaml:"mode"`
	// Execution: manual (người gọi bint evolve) | deferred (Bint đánh dấu yêu cầu tiến hoá và
	// xử lý ở lần gọi CLI phù hợp tiếp theo, mục 13.4). Managed (tiến trình nền) chưa có.
	Execution  string              `yaml:"execution"`
	Deferred   EvolutionDeferred   `yaml:"deferred"`
	Trigger    EvolutionTrigger    `yaml:"trigger"`
	Evaluation EvolutionEvaluation `yaml:"evaluation"`
	Adoption   EvolutionAdoption   `yaml:"adoption"`
}

type EvolutionDeferred struct {
	// MaxPerRun: số yêu cầu xử lý tối đa mỗi lần gọi CLI.
	MaxPerRun int `yaml:"max_per_run"`
	// BudgetUSD: ngân sách agent evolver cho yêu cầu cần mô hình khi xử lý tự động; null =
	// chỉ xử lý yêu cầu không cần mô hình (loại bỏ, gộp), phần còn lại chờ `bint evolve process --budget`.
	BudgetUSD *float64 `yaml:"budget_usd"`
}

type EvolutionTrigger struct {
	MinSimilarRuns int `yaml:"min_similar_runs"`
	MinNewEvidence int `yaml:"min_new_evidence"`
	// GroupDepth: số cấp thư mục đầu của khu vực mã khi nhóm tác vụ (mục 10.3); 0 = 2. Nhóm
	// chiếm quá nửa số lượt tự được hạ sâu thêm để tách monorepo.
	GroupDepth int `yaml:"group_depth"`
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
	// Repeats: số lần lặp mỗi nhánh. 0 = để bộ lập kế hoạch Probe chọn R (1..MaxRepeats)
	// sao cho tổng số lượt thấp nhất; > 0 = cố định R.
	Repeats int `yaml:"repeats"`
	// MaxRepeats: R tối đa mà bộ lập kế hoạch được chọn.
	MaxRepeats int `yaml:"max_repeats"`
	// EffectSD (σ_b): độ lệch chuẩn giữa các tác vụ của hiệu ứng thật, dùng khi lịch sử
	// không đủ để ước lượng (dữ liệu một nhánh không xác định được đại lượng này).
	EffectSD float64 `yaml:"effect_sd"`
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

// UILanguages là các giá trị hợp lệ của ui.language. "auto": theo BINT_LANG, rồi LC_ALL/LANG
// của hệ thống, cuối cùng tiếng Việt.
var UILanguages = []string{"auto", "vi", "en"}

// UI là cấu hình giao diện (TUI).
type UI struct {
	Language string `yaml:"language"`
}

// DefaultRoleModels là model mặc định theo role của từng harness: bước suy luận nặng
// (lập kế hoạch, review, tiến hoá, rút bài học) dùng model mạnh nhất, bước thực thi dùng
// model cân bằng (sonnet; haiku rẻ hơn nhưng yếu hơn ở các bước sửa mã). Tên model theo bậc chính thức của nhà cung cấp; đổi trong .bint/config.yml.
func DefaultRoleModels(harness string) RoleModels {
	switch harness {
	case "claude":
		return RoleModels{Planner: "opus", Implementer: "sonnet", Reviewer: "opus", Evolver: "opus", Reflector: "opus"}
	case "codex":
		return RoleModels{Planner: "gpt-6-astra", Implementer: "gpt-6-luna", Reviewer: "gpt-6-astra", Evolver: "gpt-6-astra",
			Reflector: "gpt-6-astra"}
	case "antigravity":
		return RoleModels{Planner: "gemini-4-argon-high", Implementer: "gemini-3.8-flash-medium",
			Reviewer: "gemini-4-argon-high", Evolver: "gemini-4-argon-high", Reflector: "gemini-4-argon-high"}
	}
	return RoleModels{}
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
			Claude:      ClaudeHarness{Models: DefaultRoleModels("claude")},
			Codex:       HarnessAgent{ExtraArgs: []string{}, Models: DefaultRoleModels("codex")},
			Antigravity: HarnessAgent{ExtraArgs: []string{}, Models: DefaultRoleModels("antigravity")},
		},
		Roles: Roles{Planner: "claude", Implementer: "claude", Reviewer: "claude", Evolver: "claude", Reflector: "claude"},
		Context: Context{
			MaxTokens:         6000,
			Interfaces:        ContextInterface{MCP: true, PathHooks: true, CLI: true},
			Retrieval:         Retrieval{Lexical: true, Structural: true},
			Freshness:         Freshness{VerifyChangedSources: true, Fingerprint: "symbol"},
			PathMaxItems:      5,
			PathHookTimeoutMS: 1500,
			Lessons:           Lessons{GlobalK: 3, MaxTokens: 600},
		},
		Knowledge: Knowledge{RequireHumanApprovalFor: []string{"rule"}, AgentsMDMaxItems: 15, StaleDeprecateDays: 30},
		Memory:    Memory{Enabled: true, Scope: "project", RetentionDays: 90},
		Eval: Eval{
			Repeats:        3,
			Arms:           []string{"none", "minimal", "bint"},
			LeakageMode:    "strict",
			PriorRunUSD:    0.5,
			MinFreeMB:      1024,
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
			Enabled:   true,
			Mode:      "propose",
			Execution: "manual", Deferred: EvolutionDeferred{MaxPerRun: 2},
			Trigger: EvolutionTrigger{MinSimilarRuns: 3, MinNewEvidence: 2},
			Evaluation: EvolutionEvaluation{Enabled: true, MaxCandidates: 2, Metric: "pass_rate", MinEffect: 0.15,
				NonInferiorityMargin: 0.10, MaxRepeats: 8, EffectSD: 0.15, Delivery: "bint", LeakageMode: "economy"},
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
		UI:       UI{Language: "auto"},
	}
}

// Khoảng giá trị của context.path_max_items và context.lessons.global_k. Dùng chung cho
// Validate và hcomp.checkStrategy để strategy.yml hợp lệ ở hcomp thì cũng hợp lệ ở config.
const (
	MinPathMaxItems   = 1
	MaxPathMaxItems   = 30
	MaxLessonsGlobalK = 10
)

// Sources ghi lại các tệp đã được ghép vào cấu hình cuối cùng (rỗng nếu không có).
type Sources struct {
	Global  string
	Project string
	Local   string
	// Strategy: .bint/harness/strategy.yml đã áp (chỉ khi verified); xem mergeStrategy.
	Strategy string
	// StrategyProblem: lý do strategy.yml verified bị bỏ qua (hỏng); rỗng nếu không có vấn đề.
	StrategyProblem string
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
	cfg, src, err := load(root, requireProject, true, true)
	if err != nil || src.Strategy == "" {
		return cfg, src, err
	}
	// Cấu hình nền chỉ để lấy Context: không Validate, vì strategy có thể đang che một giá
	// trị sai của config.yml (lần nạp đầu đã Validate bản cuối).
	base, _, err := load(root, requireProject, false, false)
	if err != nil {
		return cfg, src, err
	}
	b := base.Context
	cfg.contextBase = &b
	return cfg, src, nil
}

func load(root string, requireProject, withStrategy, validate bool) (Config, Sources, error) {
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
	// Chiến lược ngữ cảnh đã duyệt (H1) nằm giữa lớp dự án và lớp cục bộ: người vẫn ghi đè
	// được bằng .bint/local.yml.
	if withStrategy {
		if ok, src.StrategyProblem, err = mergeStrategy(&cfg, root); err != nil {
			return cfg, src, err
		} else if ok {
			src.Strategy = StrategyPath(root)
		}
	}
	l := LocalPath(root)
	ok, err = mergeFile(&cfg, l)
	if err != nil {
		return cfg, src, err
	}
	if ok {
		src.Local = l
	}
	if validate {
		if err := cfg.Validate(); err != nil {
			return cfg, src, err
		}
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
		{"reviewer", c.Roles.Reviewer}, {"evolver", c.Roles.Evolver}, {"reflector", c.Roles.Reflector},
	}
	for _, r := range roles {
		if r.v != "" && !slices.Contains(KnownHarnesses, r.v) {
			add("roles.%s: harness không hỗ trợ %q", r.name, r.v)
		}
	}
	for _, h := range KnownHarnesses {
		m := c.Harnesses.Models(h)
		for _, role := range RoleNames {
			if v := m.For(role); v != strings.TrimSpace(v) || strings.ContainsAny(v, " \t\n") {
				add("harnesses.%s.models.%s: tên model không được chứa khoảng trắng (%q)", h, role, v)
			}
		}
	}
	if c.Context.MaxTokens <= 0 {
		add("context.max_tokens: phải > 0")
	}
	if c.Context.PathMaxItems < MinPathMaxItems || c.Context.PathMaxItems > MaxPathMaxItems {
		add("context.path_max_items: phải trong khoảng %d..%d", MinPathMaxItems, MaxPathMaxItems)
	}
	if c.Context.PathHookTimeoutMS < 100 || c.Context.PathHookTimeoutMS > 10000 {
		add("context.path_hook_timeout_ms: phải trong khoảng 100..10000")
	}
	if c.Context.Lessons.GlobalK < 0 || c.Context.Lessons.GlobalK > MaxLessonsGlobalK {
		add("context.lessons.global_k: phải trong khoảng 0..%d", MaxLessonsGlobalK)
	}
	if c.Context.Lessons.MaxTokens < 0 {
		add("context.lessons.max_tokens: không được âm")
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
	if c.Eval.BaseCacheMaxMB < 0 || c.Eval.BaseDepth < 0 || c.Eval.MinFreeMB < 0 {
		add("eval.base_cache_max_mb, eval.base_depth, eval.min_free_mb: không được âm")
	}
	if c.Eval.PriorRunUSD < 0 {
		add("eval.prior_run_usd: không được âm")
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
	if !slices.Contains([]string{"manual", "deferred"}, c.Evolution.Execution) {
		add("evolution.execution: phải là manual | deferred (managed chưa hỗ trợ)")
	}
	if c.Evolution.Deferred.MaxPerRun < 1 {
		add("evolution.deferred.max_per_run: phải >= 1")
	}
	if b := c.Evolution.Deferred.BudgetUSD; b != nil && *b <= 0 {
		add("evolution.deferred.budget_usd: phải > 0 hoặc null")
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
		if ev.MaxRepeats < 1 {
			add("evolution.evaluation.max_repeats: phải >= 1")
		}
		if ev.EffectSD <= 0 || ev.EffectSD >= 1 {
			add("evolution.evaluation.effect_sd: phải trong khoảng (0, 1)")
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
	if !slices.Contains(UILanguages, c.UI.Language) {
		add("ui.language: phải là %s, nhận %q", strings.Join(UILanguages, " | "), c.UI.Language)
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
