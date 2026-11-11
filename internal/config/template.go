package config

import (
	"fmt"
	"strconv"
	"strings"
)

// InitParams là các giá trị `bint init` điền vào tệp cấu hình mới.
type InitParams struct {
	ProjectName           string
	Harnesses             []string
	DefaultHarness        string
	SuggestedTestCommand  string
	SuggestedSetupCommand string
	// KeepGenericModel: harness → khoá model chung đã đặt tay; khối models của harness đó
	// được để trống (chỉ dùng khi nâng cấu hình cũ, Upgrade).
	KeepGenericModel map[string]string
}

// Render sinh nội dung .bint/config.yml có chú thích. Kết quả luôn giải mã được
// bằng Decode và qua Validate (được kiểm thử).
func Render(p InitParams) string {
	if len(p.Harnesses) == 0 {
		p.Harnesses = []string{"claude"}
	}
	if p.DefaultHarness == "" {
		p.DefaultHarness = p.Harnesses[0]
	}
	h := p.DefaultHarness
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Cấu hình Bint — schema v%d. Tệp này được commit; giá trị riêng của máy\n", SchemaVersion)
	w("# đặt trong .bint/local.yml (không commit). Xem `bint doctor` để kiểm tra.\n")
	w("version: %d\n\n", SchemaVersion)
	w("project:\n  name: %s\n  root: auto\n\n", quote(p.ProjectName))
	w("state:\n  dir: auto                 # mặc định: ${XDG_STATE_HOME}/bint/<repo-id>, nằm ngoài repository\n\n")
	w("harnesses:\n  enabled:\n")
	for _, e := range p.Harnesses {
		w("    - %s\n", e)
	}
	w("  default: %s\n", h)
	w("  # models: model của từng role khi role chạy trên harness đó (bint run, /b-do, bint evolve).\n")
	w("  # Ưu tiên: --model > harnesses.<h>.models.<role> > model chung > mặc định của harness; \"\" = bỏ qua.\n")
	w("  claude:                   # Claude Code: quyền/công cụ đặt ở workflow.agent và eval.agent\n")
	writeModels(&b, p, "claude")
	w(`  codex:                    # bint run / bint eval với Codex (codex exec --json, sandbox workspace-write)
    binary: ""              # rỗng = tìm trong PATH
    model: null             # model chung khi role không đặt model riêng
    extra_args: []
`)
	writeModels(&b, p, "codex")
	w(`  antigravity:              # bint run / bint eval với Antigravity CLI (agy -p --output-format json)
    binary: ""
    model: null
    extra_args: []          # ví dụ ["--sandbox"]; Bint không tự thêm --dangerously-skip-permissions
`)
	writeModels(&b, p, "antigravity")
	w("\n")
	w("roles:                      # harness chạy mỗi role; model lấy từ harnesses.<harness>.models.<role>\n")
	w("  planner: %s     # b-req\n  implementer: %s # b-feat\n  reviewer: %s    # b-review\n  evolver: %s     # bint evolve propose\n\n",
		pad(h, 11), pad(h, 11), pad(h, 11), pad(h, 11))
	w(`context:
  max_tokens: 6000
  interfaces:
    mcp: true
    path_hooks: true
    cli: true
  retrieval:
    lexical: true
    structural: true
    semantic: false
  freshness:
    verify_changed_sources: true
    fingerprint: symbol     # symbol | hunk | file
  path_max_items: 5         # số mục tối đa chèn cho một tệp (hook theo đường dẫn)
  path_hook_timeout_ms: 1500   # hook quá hạn thì bỏ qua, không chặn agent

knowledge:
  require_human_approval_for: [rule]   # luôn chứa rule: chỉ người mới duyệt được quy tắc bắt buộc
  agents_md_max_items: 15              # số mục tối đa trong khối Bint của AGENTS.md
  stale_deprecate_days: 30             # stale quá hạn → đề xuất deprecated (không tự chuyển)

memory:
  enabled: true
  scope: project
  retention_days: 90

eval:
  # Bint không chạy lệnh nào cho tới khi bạn xác nhận bằng cách điền hai khoá dưới.
  test_command: null
  setup_command: null
`)
	if p.SuggestedTestCommand != "" {
		w("  suggested_test_command: %s   # phát hiện khi init, chưa xác nhận\n", quote(p.SuggestedTestCommand))
	}
	if p.SuggestedSetupCommand != "" {
		w("  suggested_setup_command: %s  # phát hiện khi init, chưa xác nhận\n", quote(p.SuggestedSetupCommand))
	}
	w(`  repeats: 3
  arms: [none, minimal, bint]
  leakage_mode: strict      # strict | economy
  budget_usd: null          # bắt buộc đặt trước khi chạy tự động
  keep_bases: false         # true = giữ kho đệm commit gốc giữa các đợt (nhanh hơn, tốn đĩa)
  base_cache_max_mb: 0      # khi keep_bases: trần dung lượng kho đệm, xoá kho dùng lâu nhất trước (0 = không giới hạn)
  base_depth: 0             # 0 = nạp đầy đủ lịch sử tới commit gốc; N > 0 = nạp nông (giữ 0 để so sánh với số liệu cũ)
  min_free_mb: 1024         # dừng nếu sau khi tạo kho đệm đĩa còn ít hơn mức này (MB)
  prior_run_usd: 0.5        # chi phí tiên nghiệm mỗi lượt khi chưa có lịch sử; lượt không bắt đầu nếu ngân sách còn lại nhỏ hơn mức này (0 = tắt)
  harness: claude           # claude | codex | antigravity
  timeout_minutes: 30       # mỗi lượt chạy tác nhân
  minimal_source: knowledge   # nhánh "minimal": knowledge = AGENTS.md dựng từ tri thức; file = minimal_context
  minimal_context: .bint/evals/minimal.md   # AGENTS.md viết tay khi minimal_source: file
  minimal_include_candidates: false         # đưa cả mục chưa duyệt vào nhánh minimal
  agent:
    model: null             # null = mặc định của harness
    permission_mode: acceptEdits
    allowed_tools: [Read, Edit, Write, Glob, Grep, Bash]
    setting_sources: "project,local"   # không nạp hook/cấu hình cấp người dùng
    extra_args: []
  mine:                     # tiêu chí chọn commit làm tác vụ (bint eval mine)
    max_commits: 2000
    min_source_lines: 3
    max_source_lines: 400
    max_files: 12
    min_message_chars: 20
    test_patterns: []       # rỗng = mẫu dựng sẵn (_test.go, test_*.py, *.spec.ts, ...)
    ignore_patterns: []

evolution:
  enabled: true
  mode: propose
  execution: manual         # manual | deferred (mục 13.4): deferred = Bint đánh dấu yêu cầu tiến hoá khi đủ
                            # bằng chứng mới và xử lý ở lần gọi CLI phù hợp tiếp theo (bint status, bint evolve process)
  deferred:
    max_per_run: 2          # số yêu cầu xử lý tối đa mỗi lần gọi CLI
    budget_usd: null        # ngân sách agent evolver khi xử lý tự động; null = chỉ yêu cầu không cần mô hình
  trigger:
    min_similar_runs: 3
    min_new_evidence: 2
  evaluation:               # bint evolve evaluate (đối chiếu base/candidate, mục 11.2)
    enabled: true
    max_candidates: 2       # số đề xuất tối đa mỗi lần bint evolve propose
    metric: pass_rate       # thước đo chính chọn trước: pass_rate | cost_usd | turns
    min_effect: 0.15        # mức cải thiện tỷ lệ đạt muốn phát hiện (ước tính cỡ mẫu); +10 điểm %% không phát hiện được với tác vụ của một repository
    non_inferiority_margin: 0.10   # tỷ lệ đạt được phép giảm tối đa khi loại bỏ tri thức
    repeats: 0              # số lần lặp mỗi nhánh; 0 = bộ lập kế hoạch Probe chọn R (1..max_repeats), > 0 = cố định R
    max_repeats: 8          # R tối đa mà bộ lập kế hoạch được chọn
    effect_sd: 0.15         # σ_b: độ lệch chuẩn giữa các tác vụ của hiệu ứng thật (khi lịch sử không đủ để đo)
    budget_usd: null        # ngân sách đã duyệt cho một lần đối chiếu; null = chưa duyệt
    delivery: bint          # cách đưa tri thức ở cả hai nhánh: bint | minimal
    leakage_mode: economy   # strict | economy (mục 12.3)
  adoption:
    auto_low_risk: false    # bint evolve cycle tự áp dụng đề xuất rủi ro thấp chỉ tạo mục mới (candidate) đã supported độc lập trên test
    bind_to_model_version: true

workflow:                   # workflow gốc do Bint điều phối (bint run)
  max_review_rounds: 2      # số vòng review → sửa tối đa của b-do
  timeout_minutes: 60       # mỗi bước agent
  verify_timeout_minutes: 15   # lệnh kiểm chứng của phiếu tác vụ
  agent:
    model: null
    permission_mode: acceptEdits
    allowed_tools: [Read, Edit, Write, Glob, Grep, Bash]          # bước có quyền ghi (b-feat)
    readonly_tools: [Read, Glob, Grep, "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)"]   # b-req, b-review
    setting_sources: ""     # rỗng = mặc định của harness (gồm hook ghi nhận)
    extra_args: []

limits:
  per_run:
    max_cost_usd: null
    max_turns: null
  workflows: {}             # ví dụ: {b-do: {max_cost_usd: 5}, b-feat: {max_turns: 60}}
  protected_paths:
    - "migrations/**"
    - ".github/workflows/**"

security:
  allow_external_agents: false
  allow_network_in_evals: false
  require_approval_for_commands: true
`)
	return b.String()
}

// writeModels ghi khối "models:" (thụt 4) cho một harness.
func writeModels(b *strings.Builder, p InitParams, harness string) {
	b.WriteString("    models:\n")
	m, comment := DefaultRoleModels(harness), ""
	if g := p.KeepGenericModel[harness]; g != "" {
		m, comment = RoleModels{}, "   # trống = "+g
	}
	for _, role := range RoleNames {
		fmt.Fprintf(b, "      %s %s%s\n", pad(role+":", 13), quote(m.For(role)), comment)
	}
}

// pad thêm khoảng trắng bên phải cho thẳng cột chú thích.
func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// quote trả về chuỗi YAML an toàn (dạng JSON string, hợp lệ trong YAML).
func quote(s string) string { return strconv.Quote(s) }
