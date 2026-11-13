package workflow

import (
	"bint/internal/config"
	"bint/internal/eval"
	adapters "bint/internal/harness"
)

// readonlySteps không được sửa tệp (b-req, b-review).
var readonlySteps = map[string]bool{StepReq: true, StepReview: true}

// IsReadOnly cho biết bước có phải bước chỉ đọc.
func IsReadOnly(step string) bool { return readonlySteps[step] }

// AgentArgs dựng cấu hình gọi Claude Code cho một bước.
func AgentArgs(cfg config.Config, step string, maxTurns *int) config.EvalAgent {
	return adapters.ClaudeWorkflowArgs(cfg, readonlySteps[step], maxTurns)
}

// DefaultAgents trả về factory agent theo cấu hình (roles → harness).
func DefaultAgents(cfg config.Config, command, model string) AgentFactory {
	return HarnessAgents(cfg, command, model, adapters.Selection{})
}

// HarnessAgents trả về factory agent: command khác rỗng thì dùng tác nhân tuỳ chỉnh
// (đề bài qua stdin và $BINT_EVAL_PROMPT_FILE) cho mọi bước; còn lại chọn harness
// theo role của bước (ghi đè của người dùng trong sel → roles → harnesses.default),
// model theo harnesses.<h>.models.<role> (--model ghi đè mọi bước), và tạo agent qua
// adapter tương ứng. Adapter không khả dụng → lỗi rõ ràng, bước
// thất bại (mục 17).
func HarnessAgents(cfg config.Config, command, model string, sel adapters.Selection) AgentFactory {
	models := map[string]string{}
	if model != "" {
		for _, role := range []string{"planner", "implementer", "reviewer"} {
			models[role] = model
		}
	}
	return HarnessAgentsWithChoices(cfg, command, sel, models)
}

// HarnessAgentsWithChoices uses the saved model for each role while still checking
// the selected harness against the current enabled-harness policy.
func HarnessAgentsWithChoices(cfg config.Config, command string, sel adapters.Selection, models map[string]string) AgentFactory {
	return func(step string, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		if command != "" {
			return &eval.CommandAgent{Command: command}, nil
		}
		h, _, err := adapters.ForStep(cfg, sel, step)
		if err != nil {
			return nil, err
		}
		role := adapters.StepRole(step)
		model, snapshotted := models[role]
		modelSet := snapshotted
		if !snapshotted {
			model = adapters.RoleModel(cfg, h, role, "")
			modelSet = model != ""
		}
		return adapters.NewAgent(h, adapters.AgentOptions{Config: cfg, Step: step, ReadOnly: readonlySteps[step],
			Model: model, ModelSet: modelSet, MaxCost: maxCost, MaxTurns: maxTurns})
	}
}

// ReadOnlyRole cho biết role (harness.StepRole) là của một bước chỉ đọc (planner = b-req, reviewer = b-review): tool
// MCP ghi vào cây làm việc không được cấp cho các role này dù mcp.grants có ghi.
func ReadOnlyRole(role string) bool {
	if role == "" {
		return false
	}
	for step := range readonlySteps {
		if adapters.StepRole(step) == role {
			return true
		}
	}
	return false
}
