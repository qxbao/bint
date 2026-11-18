package workflow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/task"
)

// Hồi quy phát hiện review 01M7PD9T2ASG8XXMEM7SJ2G6C1.6: hook Bint đã cài cho Claude chỉ đăng ký Read|Edit|Write|MultiEdit|NotebookEdit|Bash nên
// Grep/Glob không tới hookcore.Handle và rào BINT_DENY_READ không chặn được Grep vào tasks.md. Bước b-goal chạy bằng Claude phải nhận settings
// bổ sung (--settings) đăng ký `bint hook claude` cho Grep|Glob; bước khác thì không (không thêm hook, không tốn thời gian).
func TestGoalStepGetsGrepGlobHookSettings(t *testing.T) {
	h := goalBox(t, true, nil)
	h.agentName = "claude"
	settings := map[string]string{} // bước → nội dung tệp settings đọc lúc bước đang chạy (tệp bị xoá sau bước)
	given := map[string]string{}    // bước → AgentRequest.Settings
	h.mutateReq = func(req eval.AgentRequest, step string, _ *eval.AgentResult) {
		given[step] = req.Settings
		if req.Settings != "" {
			b, err := os.ReadFile(req.Settings)
			if err != nil {
				t.Errorf("bước %s: không đọc được settings %s: %v", step, req.Settings, err)
			}
			settings[step] = string(b)
		}
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review" {
		t.Fatalf("%+v %v calls=%v", res, err, h.calls)
	}
	for _, step := range []string{StepReq, StepFeat, "b-review"} {
		if given[step] != "" {
			t.Errorf("bước %s không được có settings rào đọc: %q", step, given[step])
		}
	}
	raw := settings[StepGoal]
	if raw == "" {
		t.Fatalf("b-goal (Claude) phải nhận settings rào đọc, AgentRequest.Settings=%q", given[StepGoal])
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type, Command string
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("settings không phải JSON: %v\n%s", err, raw)
	}
	pre := doc.Hooks["PreToolUse"]
	if len(pre) != 1 || len(pre[0].Hooks) != 1 {
		t.Fatalf("cần đúng một nhóm PreToolUse: %s", raw)
	}
	for _, tool := range []string{"Grep", "Glob"} {
		if !strings.Contains("|"+pre[0].Matcher+"|", "|"+tool+"|") {
			t.Errorf("matcher %q phải gồm %s", pre[0].Matcher, tool)
		}
	}
	if c := pre[0].Hooks[0]; c.Type != "command" || !strings.HasSuffix(c.Command, "hook claude") {
		t.Errorf("hook phải là lệnh `bint hook claude`: %+v", c)
	}
	if _, err := os.Stat(given[StepGoal]); err == nil {
		t.Errorf("tệp settings phải bị xoá sau bước: %s", given[StepGoal])
	}
}

// Harness khác Claude (Codex không có Grep/Glob riêng) không nhận settings rào đọc.
func TestGoalStepNoGrepGlobSettingsOnOtherHarness(t *testing.T) {
	h := goalBox(t, true, nil)
	h.agentName = "codex"
	var goalSettings = "unset"
	h.mutateReq = func(req eval.AgentRequest, step string, _ *eval.AgentResult) {
		if step == StepGoal {
			goalSettings = req.Settings
		}
	}
	if _, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if goalSettings != "" {
		t.Errorf("b-goal chạy Codex không được nhận settings Claude: %q", goalSettings)
	}
}
