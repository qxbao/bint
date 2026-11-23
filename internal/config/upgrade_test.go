package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestUpgradeReadonlyLoadProjectAndLocal(t *testing.T) {
	t.Setenv("BINT_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(ProjectPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	current := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	old := strings.Replace(current, `, "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)"`, "", 1)
	up, err := Upgrade([]byte(old))
	if err != nil || !up.Changed() {
		t.Fatalf("upgrade: %v", err)
	}
	if err := os.WriteFile(ProjectPath(root), up.Content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, true)
	if err != nil || !slices.Contains(cfg.Workflow.Agent.ReadonlyTools, "Bash(git ls-files:*)") {
		t.Fatalf("project load: %v %v", cfg.Workflow.Agent.ReadonlyTools, err)
	}
	local := "workflow:\n  agent:\n    readonly_tools: [Read]\n"
	if err := os.WriteFile(LocalPath(root), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Load(root, true)
	if err != nil || !slices.Equal(cfg.Workflow.Agent.ReadonlyTools, []string{"Read"}) {
		t.Fatalf("local override: %v %v", cfg.Workflow.Agent.ReadonlyTools, err)
	}
	b, err := os.ReadFile(LocalPath(root))
	if err != nil || string(b) != local {
		t.Fatalf("local modified: %v", err)
	}
}

func TestUpgradeReadonlyLegacyAndCustom(t *testing.T) {
	current := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	old := strings.Replace(current, `, "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)"`, "", 1)
	result, err := Upgrade([]byte(old))
	if err != nil || !result.Changed() || string(result.Content) != current {
		t.Fatalf("legacy upgrade: changed=%v err=%v", result.Changed(), err)
	}
	again, err := Upgrade(result.Content)
	if err != nil || again.Changed() {
		t.Fatalf("second upgrade: %v %v", again.Changed(), err)
	}
	custom := strings.Replace(old, `"Bash(git status:*)"`, `"Bash(git status:*)", "Bash(custom:*)"`, 1)
	result, err = Upgrade([]byte(custom))
	if err != nil || string(result.Content) != custom {
		t.Fatalf("custom config changed: %v", err)
	}
}

func TestUpgradeReadonlyBlockLegacy(t *testing.T) {
	current := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	flow := `    readonly_tools: [Read, Glob, Grep, "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)", "mcp__bint__*"]   # b-req, b-review`
	block := "    readonly_tools: # keep header\n      - Read\n      - Glob\n      - Grep\n      - 'Bash(bint:*)'\n      - 'Bash(git diff:*)'\n      - 'Bash(git log:*)'\n      - 'Bash(git show:*)'\n      - 'Bash(git status:*)' # keep comment\n      - 'mcp__bint__*'"
	old := strings.Replace(current, flow, block, 1)
	if old == current {
		t.Fatal("fixture did not replace sequence")
	}
	result, err := Upgrade([]byte(old))
	if err != nil || !result.Changed() {
		t.Fatalf("legacy block not migrated: %v", err)
	}
	if !strings.Contains(string(result.Content), "# keep header") || !strings.Contains(string(result.Content), "# keep comment") {
		t.Fatal("comments lost")
	}
	var cfg = Default()
	if err := Decode(strings.NewReader(string(result.Content)), &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Workflow.Agent.ReadonlyTools, "Bash(git ls-files:*)") {
		t.Fatal("missing ls-files")
	}
	again, err := Upgrade(result.Content)
	if err != nil || again.Changed() {
		t.Fatalf("not idempotent: %v", err)
	}
	custom := strings.Replace(old, "      - 'mcp__bint__*'", "      - 'custom'\n      - 'mcp__bint__*'", 1)
	res, err := Upgrade([]byte(custom))
	if err != nil || string(res.Content) != custom {
		t.Fatalf("custom modified: %v", err)
	}
}

// oldConfig mô phỏng .bint/config.yml do bản Bint trước khi có models theo role sinh ra.
func oldConfig(t *testing.T, edit func(string) string) string {
	t.Helper()
	s := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude", "codex"}})
	// Bỏ chú thích models, khối harnesses.claude và mọi khối models.
	var out []string
	skip := 0
	for _, l := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(l)
		switch {
		case skip > 0 && indentOf(l) > skip:
			continue
		case strings.HasPrefix(trim, "# models:") || strings.HasPrefix(trim, "# Ưu tiên: --model"):
			continue
		case l == "  claude:" || strings.HasPrefix(l, "  claude: "):
			skip = indentOf(l)
			continue
		case trim == "models:":
			skip = indentOf(l)
			continue
		}
		skip = 0
		out = append(out, l)
	}
	old := strings.Join(out, "\n")
	old = strings.Replace(old, "+10 điểm % không", "+10 điểm %!k(MISSING)hông", 1)
	if edit != nil {
		old = edit(old)
	}
	return old
}

func TestUpgradeAddsMissingKeysKeepingEveryLine(t *testing.T) {
	old := oldConfig(t, func(s string) string {
		// Giá trị và chú thích người dùng tự sửa phải giữ nguyên.
		s = strings.Replace(s, "  max_tokens: 6000", "  max_tokens: 9000   # tôi tăng lên", 1)
		return strings.Replace(s, "roles:", "# vai trò của nhóm\nroles:", 1)
	})
	if strings.Contains(old, "models:") {
		t.Fatalf("fixture vẫn còn models:\n%s", old)
	}
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed() || res.Fixed != 1 {
		t.Fatalf("changed=%v fixed=%d", res.Changed(), res.Fixed)
	}
	want := []string{"harnesses.claude", "harnesses.codex.models", "harnesses.antigravity.models"}
	if strings.Join(res.Added, ",") != strings.Join(want, ",") {
		t.Fatalf("added = %v, muốn %v", res.Added, want)
	}
	got := string(res.Content)
	// Mọi dòng cũ (trừ dòng chú thích hỏng đã sửa) còn nguyên, đúng thứ tự.
	rest := got
	for _, l := range strings.Split(old, "\n") {
		l = templateFixes.Replace(l)
		i := strings.Index(rest, l)
		if i < 0 {
			t.Fatalf("mất hoặc đổi thứ tự dòng %q\n%s", l, got)
		}
		rest = rest[i+len(l):]
	}
	if strings.Contains(got, "%!k(MISSING)") {
		t.Fatal("chú thích hỏng chưa được sửa")
	}
	cfg := Default()
	cfg.Harnesses.Claude.Models = RoleModels{}
	if err := Decode(strings.NewReader(got), &cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Harnesses.Claude.Models, DefaultRoleModels("claude")) || cfg.Context.MaxTokens != 9000 {
		t.Fatalf("models=%+v max_tokens=%d", cfg.Harnesses.Claude.Models, cfg.Context.MaxTokens)
	}
	// Khối mới nằm đúng chỗ: claude sau default, models của codex trong khối codex.
	if !strings.Contains(got, "  default: claude\n  # models:") || !strings.Contains(got, "    extra_args: []\n    models:\n      planner:      \"gpt-6-astra\"") {
		t.Fatalf("vị trí chèn sai:\n%s", got)
	}

	// Chạy lại: không đổi gì.
	again, err := Upgrade(res.Content)
	if err != nil || again.Changed() || string(again.Content) != got {
		t.Fatalf("lần hai phải không đổi: changed=%v err=%v", again.Changed(), err)
	}
}

// Mẫu hiện tại đã đủ khoá: Upgrade không đổi gì.
func TestUpgradeCurrentTemplateNoop(t *testing.T) {
	cur := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}, SuggestedTestCommand: "go test ./..."})
	res, err := Upgrade([]byte(cur))
	if err != nil || res.Changed() || string(res.Content) != cur {
		t.Fatalf("changed=%v err=%v added=%v", res.Changed(), err, res.Added)
	}
}

// Model chung đã đặt tay: khối models thêm vào để trống, hành vi giữ nguyên.
func TestUpgradeKeepsGenericModel(t *testing.T) {
	old := oldConfig(t, func(s string) string {
		s = strings.Replace(s, "workflow:", "workflow:\n  # model riêng", 1)
		i := strings.Index(s, "workflow:")
		j := strings.Index(s[i:], "    model: null") + i
		return s[:j] + "    model: sonnet" + s[j+len("    model: null"):]
	})
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := Decode(strings.NewReader(string(res.Content)), &cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Harnesses.Claude.Models, RoleModels{}) {
		t.Fatalf("models của claude phải trống: %+v", cfg.Harnesses.Claude.Models)
	}
	if !reflect.DeepEqual(cfg.Harnesses.Codex.Models, DefaultRoleModels("codex")) {
		t.Fatalf("codex không đặt model chung, phải nhận mặc định: %+v", cfg.Harnesses.Codex.Models)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "workflow.agent.model") {
		t.Fatalf("notes = %v", res.Notes)
	}
	if !strings.Contains(string(res.Content), `planner:      ""   # trống = workflow.agent.model`) {
		t.Fatalf("thiếu chú thích giải thích:\n%s", res.Content)
	}
}

// Ánh xạ viết dạng {…}: không chèn được, báo để người dùng thêm tay.
func TestUpgradeFlowMappingReported(t *testing.T) {
	old := oldConfig(t, func(s string) string {
		i := strings.Index(s, "  codex:")
		j := strings.Index(s, "  antigravity:")
		return s[:i] + "  codex: {binary: \"\", model: null, extra_args: []}\n" + s[j:]
	})
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range res.Notes {
		found = found || strings.Contains(n, "harnesses.codex.models")
	}
	if !found {
		t.Fatalf("notes = %v", res.Notes)
	}
	for _, a := range res.Added {
		if a == "harnesses.codex.models" {
			t.Fatal("không được chèn vào ánh xạ dạng {…}")
		}
	}
}

func TestSummarizeAdded(t *testing.T) {
	got := SummarizeAdded([]string{"harnesses.claude", "harnesses.claude.models", "harnesses.codex.models"})
	if got != "harnesses.claude, harnesses.codex.models" {
		t.Fatal(got)
	}
}

// Cấu hình cũ chưa có khối ui: Upgrade chèn ui.language (giữ mọi dòng cũ), nạp lại được và hợp lệ.
func TestUpgradeAddsUILanguage(t *testing.T) {
	old := Render(InitParams{ProjectName: "demo"})
	i := strings.Index(old, "\nui:\n")
	if i < 0 {
		t.Fatalf("mẫu thiếu khối ui:\n%s", old)
	}
	old = old[:i+1] // cấu hình do bản trước khi có ui sinh ra
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed() || !slices.Contains(res.Added, "ui") && !slices.Contains(res.Added, "ui.language") {
		t.Fatalf("Added = %v", res.Added)
	}
	got := string(res.Content)
	if !strings.Contains(got, "ui:\n  language: auto") || !strings.HasPrefix(got, strings.TrimRight(old, "\n")) {
		t.Fatalf("khối ui chèn sai hoặc làm mất dòng cũ:\n%s", got)
	}
	cfg := Default()
	if err := Decode(strings.NewReader(got), &cfg); err != nil || cfg.Validate() != nil || cfg.UI.Language != "auto" {
		t.Fatalf("nạp lại: %v %+v", err, cfg.UI)
	}
	// Người dùng đã đặt ui.language: giữ nguyên giá trị.
	mine, err := Upgrade([]byte(old + "\nui:\n  language: en\n"))
	if err != nil || slices.Contains(mine.Added, "ui") || slices.Contains(mine.Added, "ui.language") {
		t.Fatalf("không được chèn lại: %v %v", err, mine.Added)
	}
}

// Tệp CRLF: dòng chèn vào danh sách block phải giữ CRLF, không trộn LF.
func TestUpgradeReadonlyBlockCRLF(t *testing.T) {
	current := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	flow := `    readonly_tools: [Read, Glob, Grep, "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)", "mcp__bint__*"]   # b-req, b-review`
	block := "    readonly_tools:\n      - Read\n      - Glob\n      - Grep\n      - 'Bash(bint:*)'\n      - 'Bash(git diff:*)'\n      - 'Bash(git log:*)'\n      - 'Bash(git show:*)'\n      - 'Bash(git status:*)'\n      - 'mcp__bint__*'"
	old := strings.ReplaceAll(strings.Replace(current, flow, block, 1), "\n", "\r\n")
	result, err := Upgrade([]byte(old))
	if err != nil || len(result.Migrated) == 0 {
		t.Fatalf("not migrated: %v %v", result.Migrated, err)
	}
	out := string(result.Content)
	if n := strings.Count(out, "\n"); n != strings.Count(out, "\r\n") {
		t.Fatalf("mixed line endings: %d LF, %d CRLF", n, strings.Count(out, "\r\n"))
	}
}

// Danh sách mẫu cũ có bố cục không tự sửa được (flow trải nhiều dòng): giữ nguyên nhưng phải có ghi chú.
func TestUpgradeReadonlyUnrewritableNotes(t *testing.T) {
	current := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	flow := `    readonly_tools: [Read, Glob, Grep, "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)", "mcp__bint__*"]   # b-req, b-review`
	wrapped := "    readonly_tools: [Read, Glob, Grep,\n      \"Bash(bint:*)\", \"Bash(git diff:*)\", \"Bash(git log:*)\", \"Bash(git show:*)\", \"Bash(git status:*)\", \"mcp__bint__*\"]"
	old := strings.Replace(current, flow, wrapped, 1)
	if old == current {
		t.Fatal("fixture did not replace sequence")
	}
	result, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Migrated) != 0 || string(result.Content) != old {
		t.Fatalf("unexpected rewrite: %v", result.Migrated)
	}
	if !slices.ContainsFunc(result.Notes, func(n string) bool { return strings.Contains(n, "readonly_tools") }) {
		t.Fatalf("missing note: %v", result.Notes)
	}
}

// Cấu hình còn khoá của bộ chọn cũ: Upgrade giữ nguyên chúng nhưng ghi chú rằng chúng không còn dùng.
func TestUpgradeNotesDeprecatedVerifyKeys(t *testing.T) {
	plain := Render(InitParams{ProjectName: "demo", Harnesses: []string{"claude"}})
	old := strings.Replace(plain, "    bint_config_full: true", `    scope: affected
    done: graph
    bint_config_full: true`, 1)
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Notes, "|"), "workflow.verify.scope, workflow.verify.done không còn dùng") {
		t.Fatalf("thiếu ghi chú khoá cũ: %v", res.Notes)
	}
}
