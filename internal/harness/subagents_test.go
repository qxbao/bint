package harness

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bint/internal/config"
	"bint/internal/harness/hookcore"
)

func frontmatter(t *testing.T, content []byte) map[string]any {
	t.Helper()
	s := string(content)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("thiếu frontmatter:\n%s", s)
	}
	end := strings.Index(s[4:], "\n---\n")
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s[4:4+end]), &m); err != nil {
		t.Fatalf("frontmatter không phải YAML: %v\n%s", err, s)
	}
	return m
}

func agentByName(t *testing.T, cfg config.Config, name string) map[string]any {
	t.Helper()
	for _, f := range SubagentFiles(cfg) {
		if f.Name == name {
			if f.Rel != ".claude/agents/"+name+".md" {
				t.Fatalf("Rel %q", f.Rel)
			}
			return frontmatter(t, f.Content)
		}
	}
	t.Fatalf("không có %s", name)
	return nil
}

func TestSubagentFilesDefaultConfig(t *testing.T) {
	cfg := config.Default()
	if got := len(SubagentFiles(cfg)); got != 3 {
		t.Fatalf("muốn 3 tệp, được %d", got)
	}
	pl := agentByName(t, cfg, SubagentPlanner)
	im := agentByName(t, cfg, SubagentImplementer)
	rv := agentByName(t, cfg, SubagentReviewer)
	for name, m := range map[string]map[string]any{SubagentPlanner: pl, SubagentImplementer: im, SubagentReviewer: rv} {
		if m["name"] != name || m["description"] == "" {
			t.Errorf("%s: %+v", name, m)
		}
	}
	// model theo harnesses.claude.models của role.
	if pl["model"] != "opus" || im["model"] != "sonnet" || rv["model"] != "opus" {
		t.Fatalf("model: %v %v %v", pl["model"], im["model"], rv["model"])
	}
	// disallowedTools chỉ ở planner/reviewer; implementer không có.
	if pl["disallowedTools"] != "Edit, Write, MultiEdit, NotebookEdit" || rv["disallowedTools"] != pl["disallowedTools"] {
		t.Fatalf("disallowedTools: %v %v", pl["disallowedTools"], rv["disallowedTools"])
	}
	if _, ok := im["disallowedTools"]; ok {
		t.Fatal("implementer không được có disallowedTools")
	}
	// tools theo readonly_tools / allowed_tools.
	if pl["tools"] != strings.Join(cfg.Workflow.Agent.ReadonlyTools, ", ") || im["tools"] != strings.Join(cfg.Workflow.Agent.AllowedTools, ", ") {
		t.Fatalf("tools: %v | %v", pl["tools"], im["tools"])
	}
	if strings.Contains(pl["tools"].(string), "Edit") || !strings.Contains(im["tools"].(string), "Edit") {
		t.Fatalf("planner không có Edit, implementer có: %v | %v", pl["tools"], im["tools"])
	}
	// max_turns không đặt ⇒ không có maxTurns.
	for _, m := range []map[string]any{pl, im, rv} {
		if _, ok := m["maxTurns"]; ok {
			t.Fatalf("không đặt max_turns thì không có maxTurns: %+v", m)
		}
	}
}

func TestSubagentFilesFollowConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Claude.Models.Planner = ""
	cfg.Harnesses.Claude.Models.Implementer = "haiku"
	three, five := 30, 50
	cfg.Limits.Workflows = map[string]config.PerRunLimits{"b-feat": {MaxTurns: &three}, "b-review": {MaxTurns: &five}}
	pl := agentByName(t, cfg, SubagentPlanner)
	im := agentByName(t, cfg, SubagentImplementer)
	rv := agentByName(t, cfg, SubagentReviewer)
	if pl["model"] != "inherit" || im["model"] != "haiku" {
		t.Fatalf("model rỗng ⇒ inherit: %v %v", pl["model"], im["model"])
	}
	if im["maxTurns"] != 30 || rv["maxTurns"] != 50 {
		t.Fatalf("maxTurns: %v %v", im["maxTurns"], rv["maxTurns"])
	}
	if _, ok := pl["maxTurns"]; ok {
		t.Fatal("b-req không đặt max_turns")
	}
	// Không bật Claude ⇒ không có tệp.
	cfg.Harnesses.Enabled = []string{"codex"}
	if SubagentFiles(cfg) != nil {
		t.Fatal("không có Claude ⇒ nil")
	}
}

// Tên subagent của hookcore (guardrail chỉ đọc) phải khớp tên của harness.
func TestSubagentNamesMatchHookcore(t *testing.T) {
	if hookcore.SubagentPlanner != SubagentPlanner || hookcore.SubagentReviewer != SubagentReviewer {
		t.Fatalf("hookcore %q/%q lệch harness %q/%q", hookcore.SubagentPlanner, hookcore.SubagentReviewer, SubagentPlanner, SubagentReviewer)
	}
	if SubagentName("planner") != SubagentPlanner || SubagentName("implementer") != SubagentImplementer || SubagentName("reviewer") != SubagentReviewer || SubagentName("evolver") != "" {
		t.Fatal("SubagentName")
	}
}
