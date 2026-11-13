package harness

// Golden của GĐ3 (docs/custom-flow-plan.md): StepRole, ParseRoleOverrides và nội dung từng byte của SubagentFiles được
// sinh từ mã cũ (BINT_UPDATE_GOLDEN=1, trước refactor sang flow nhúng) và không được sửa tay.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/config"
)

const goldenPath = "testdata/flow_golden.json"

type goldenAgentFile struct {
	Name    string `json:"name"`
	Rel     string `json:"rel"`
	Content string `json:"content"`
}

type harnessGolden struct {
	StepRole  map[string]string            `json:"step_role"`
	Overrides map[string]map[string]string `json:"role_overrides"`
	Subagents map[string][]goldenAgentFile `json:"subagent_files"`
}

func goldenSubagents(cfg config.Config) []goldenAgentFile {
	out := []goldenAgentFile{}
	for _, f := range SubagentFiles(cfg) {
		out = append(out, goldenAgentFile{Name: f.Name, Rel: f.Rel, Content: string(f.Content)})
	}
	return out
}

func buildHarnessGolden(t *testing.T) harnessGolden {
	t.Helper()
	g := harnessGolden{StepRole: map[string]string{}, Overrides: map[string]map[string]string{}, Subagents: map[string][]goldenAgentFile{}}
	for _, id := range []string{"b-req", "b-feat", "b-review", "b-do", "post-feat", "verify", "", "bogus", "planner"} {
		g.StepRole[id] = StepRole(id)
	}
	for _, specs := range [][]string{{"b-req=claude"}, {"b-feat=claude", "reviewer=claude"}, {"planner=claude", "b-review=claude"}} {
		m, err := ParseRoleOverrides(specs)
		if err != nil {
			t.Fatal(err)
		}
		key := ""
		for _, s := range specs {
			key += s + ";"
		}
		g.Overrides[key] = m
	}
	g.Subagents["default"] = goldenSubagents(config.Default())

	cfg := config.Default()
	cfg.Harnesses.Claude.Models.Planner = ""
	cfg.Harnesses.Claude.Models.Implementer = "haiku"
	cfg.Harnesses.Claude.Models.Reviewer = "opus"
	three, five, seven := 30, 50, 70
	cfg.Limits.Workflows = map[string]config.PerRunLimits{"b-req": {MaxTurns: &seven}, "b-feat": {MaxTurns: &three}, "b-review": {MaxTurns: &five}}
	cfg.Workflow.Agent.AllowedTools = []string{"Read", "Edit"}
	cfg.Workflow.Agent.ReadonlyTools = []string{"Read", "Grep"}
	g.Subagents["custom"] = goldenSubagents(cfg)

	cfg.Harnesses.Enabled = []string{"codex"}
	g.Subagents["claude_disabled"] = goldenSubagents(cfg)
	return g
}

func TestFlowGoldenHarness(t *testing.T) {
	got, err := json.MarshalIndent(buildHarnessGolden(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("BINT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("đã ghi %s do BINT_UPDATE_GOLDEN=1: xem git diff rồi chạy lại không có biến để kiểm", goldenPath)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("thiếu golden (sinh bằng BINT_UPDATE_GOLDEN=1 trên mã cũ): %v", err)
	}
	if string(want) != string(got) {
		actual := filepath.Join(os.TempDir(), "bint-harness-golden-actual.json")
		_ = os.WriteFile(actual, got, 0o644)
		t.Fatalf("golden harness không khớp (%d byte, muốn %d); bản thực tế ghi ở %s để so", len(got), len(want), actual)
	}
}
