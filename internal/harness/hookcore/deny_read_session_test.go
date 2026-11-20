package hookcore

import (
	"path/filepath"
	"testing"
	"time"
)

// Rào đọc của subagent (driver session, luồng B′): không có BINT_DENY_READ, hook tra DenyRead trong bản ghi session-step theo khối <bint-task … run=…>.
func TestDenyReadFromSessionStep(t *testing.T) {
	t.Setenv(EnvDenyRead, "")
	rt := t.TempDir()
	plan := filepath.ToSlash(filepath.Join(rt, "tasks", "T1", "plan.md"))
	rec := SessionStepRecord{RunID: "run1", TaskID: "T1", Step: "b-review", Subagent: SubagentReviewer, Worktree: "/x/wt", Isolated: true,
		Until: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), DenyRead: []string{plan}}
	if err := WriteSessionStep(rt, rec); err != nil {
		t.Fatal(err)
	}
	d := Dialect{SubagentPrompt: func(Input) (string, bool) {
		return "<bint-task id=\"T1\" step=\"b-review\" run=\"run1\">\n", true
	}}
	hi := Input{AgentType: SubagentReviewer, ToolName: "Read", ToolInput: map[string]any{"file_path": plan}}

	env := denyReadEnv(d, hi, rt)
	if env == "" {
		t.Fatal("(DenyRead × session-step) hook phải lấy rào đọc từ bản ghi bước")
	}
	if hit, deny := denyReadDecision(env, hi.ToolInput); !deny || hit == "" {
		t.Errorf("(DenyRead × Read plan.md) phải deny: hit=%q deny=%v", hit, deny)
	}
	if _, deny := denyReadDecision(env, map[string]any{"file_path": "/repo/app.txt"}); deny {
		t.Errorf("(DenyRead × tệp khác) không được deny")
	}

	// Bản ghi của tác vụ khác (task_id lệch) hoặc không có DenyRead: không chặn.
	d2 := Dialect{SubagentPrompt: func(Input) (string, bool) { return "<bint-task id=\"T2\" step=\"b-review\" run=\"run1\">\n", true }}
	if got := denyReadEnv(d2, hi, rt); got != "" {
		t.Errorf("(DenyRead × task lệch) muốn rỗng, có %q", got)
	}
	rec.RunID, rec.DenyRead = "run2", nil
	if err := WriteSessionStep(rt, rec); err != nil {
		t.Fatal(err)
	}
	d3 := Dialect{SubagentPrompt: func(Input) (string, bool) { return "<bint-task id=\"T1\" step=\"b-feat\" run=\"run2\">\n", true }}
	if got := denyReadEnv(d3, hi, rt); got != "" {
		t.Errorf("(DenyRead × bước không có rào) muốn rỗng, có %q", got)
	}
}
