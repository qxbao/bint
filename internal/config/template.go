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
	w("  default: %s\n\n", h)
	w("roles:\n  planner: %s\n  implementer: %s\n  reviewer: %s\n  evolver: %s\n\n", h, h, h, h)
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

knowledge:
  require_human_approval_for: [rule]   # luôn chứa rule: chỉ người mới duyệt được quy tắc bắt buộc
  agents_md_max_items: 15              # số mục tối đa trong khối Bint của AGENTS.md

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
  harness: claude
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
  trigger:
    min_similar_runs: 3
    min_new_evidence: 2
  evaluation:
    enabled: true
    max_candidates: 2
  adoption:
    auto_low_risk: false
    bind_to_model_version: true

limits:
  per_run:
    max_cost_usd: null
    max_turns: null
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

// quote trả về chuỗi YAML an toàn (dạng JSON string, hợp lệ trong YAML).
func quote(s string) string { return strconv.Quote(s) }
