package config

import (
	"strings"
	"testing"
)

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
	if cfg.Harnesses.Claude.Models != DefaultRoleModels("claude") || cfg.Context.MaxTokens != 9000 {
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
	if cfg.Harnesses.Claude.Models != (RoleModels{}) {
		t.Fatalf("models của claude phải trống: %+v", cfg.Harnesses.Claude.Models)
	}
	if cfg.Harnesses.Codex.Models != DefaultRoleModels("codex") {
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
