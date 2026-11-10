package workflow

import (
	"fmt"
	"strconv"

	"bint/internal/config"
	"bint/internal/eval"
)

// readonlySteps không được sửa tệp (b-req, b-review).
var readonlySteps = map[string]bool{StepReq: true, StepReview: true}

// roleHarness trả về harness của role ứng với bước (mục 5: roles).
func roleHarness(cfg config.Config, step string) string {
	h := ""
	switch step {
	case StepReq:
		h = cfg.Roles.Planner
	case StepFeat:
		h = cfg.Roles.Implementer
	case StepReview:
		h = cfg.Roles.Reviewer
	}
	if h == "" {
		h = cfg.Harnesses.Default
	}
	if h == "" {
		h = "claude"
	}
	return h
}

// AgentArgs dựng cấu hình gọi Claude Code cho một bước.
func AgentArgs(cfg config.Config, step string, maxTurns *int) config.EvalAgent {
	w := cfg.Workflow.Agent
	a := config.EvalAgent{Model: w.Model, PermissionMode: w.PermissionMode, SettingSources: w.SettingSources,
		AllowedTools: w.AllowedTools}
	if readonlySteps[step] {
		a.AllowedTools = w.ReadonlyTools
		a.ExtraArgs = append(a.ExtraArgs, "--disallowedTools", "Edit,Write,MultiEdit,NotebookEdit")
	}
	if maxTurns != nil {
		a.ExtraArgs = append(a.ExtraArgs, "--max-turns", strconv.Itoa(*maxTurns))
	}
	a.ExtraArgs = append(a.ExtraArgs, w.ExtraArgs...)
	return a
}

// DefaultAgents trả về factory agent theo cấu hình: command khác rỗng thì dùng tác
// nhân tuỳ chỉnh (đề bài qua stdin và $BINT_EVAL_PROMPT_FILE) cho mọi bước; còn lại
// theo role → harness. Bản này chỉ có adapter thực thi cho Claude Code.
func DefaultAgents(cfg config.Config, command, model string) AgentFactory {
	return func(step string, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		if command != "" {
			return &eval.CommandAgent{Command: command}, nil
		}
		h := roleHarness(cfg, step)
		if h != "claude" {
			return nil, fmt.Errorf("role của bước %s dùng harness %q: adapter thực thi chưa có (P5); dùng --agent-command hoặc đổi roles trong cấu hình", step, h)
		}
		return &eval.ClaudeAgent{Cfg: AgentArgs(cfg, step, maxTurns), Model: model, MaxCostUSD: maxCost}, nil
	}
}
