package app

import (
	"strings"
	"testing"
)

// Agent ngoài tác vụ sắp chạy cả bộ test: PreToolUse chèn lời nhắc `bint test affected --run`, một lần mỗi phiên, không chặn; lệnh hẹp
// và tiến trình thuộc tác vụ (BINT_TASK_ID) không được nhắc.
func TestWideTestNudgeOncePerSessionOutsideTasks(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, nil)
	pre := func(session, cmd string) string {
		return runHook(t, hookInput(session, "PreToolUse", root, map[string]any{"tool_name": "Bash",
			"tool_input": map[string]any{"command": cmd}, "tool_use_id": session + cmd})).Output
	}
	if out := pre("s1", "go test ./internal/a"); strings.Contains(out, "bint test affected") {
		t.Fatalf("lệnh hẹp bị nhắc: %s", out)
	}
	out := pre("s1", "go test ./...")
	if !strings.Contains(out, "additionalContext") || !strings.Contains(out, "bint test affected --run") || strings.Contains(out, "deny") {
		t.Fatalf("thiếu lời nhắc hoặc bị chặn: %s", out)
	}
	if out := pre("s1", "npm test"); strings.Contains(out, "bint test affected") {
		t.Fatalf("nhắc lần hai trong cùng phiên: %s", out)
	}
	if out := pre("s2", "pytest"); !strings.Contains(out, "bint test affected") {
		t.Fatalf("phiên mới phải được nhắc: %q", out)
	}
	sub := runHook(t, hookInput("s4", "PreToolUse", root, map[string]any{"tool_name": "Bash", "agent_type": "bint-implementer",
		"tool_input": map[string]any{"command": "go test ./..."}, "tool_use_id": "sub"})).Output
	if strings.Contains(sub, "bint test affected") {
		t.Fatalf("subagent bint-* không được nhắc: %s", sub)
	}
	t.Setenv("BINT_TASK_ID", "TSK-1")
	if out := pre("s3", "go test ./..."); strings.Contains(out, "bint test affected") {
		t.Fatalf("trong tác vụ không được nhắc: %s", out)
	}
}
