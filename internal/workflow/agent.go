package workflow

import (
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/flow"
	adapters "bint/internal/harness"
)

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
// the selected harness against the current enabled-harness policy. Chế độ chỉ đọc, role và role gốc lấy từ spec của bước
// (flow đang chạy), không tra theo id bước.
func HarnessAgentsWithChoices(cfg config.Config, command string, sel adapters.Selection, models map[string]string) AgentFactory {
	return func(spec StepSpec, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		if command != "" {
			return &eval.CommandAgent{Command: command}, nil
		}
		h, _, err := adapters.ForRole(cfg, sel, spec.Role, spec.Base)
		if err != nil {
			return nil, err
		}
		model, snapshotted := models[spec.Role]
		modelSet := snapshotted
		if !snapshotted {
			model = adapters.RoleModelFor(cfg, h, spec.Role, spec.Base, "")
			modelSet = model != ""
		}
		return adapters.NewAgent(h, adapters.AgentOptions{Config: cfg, Step: spec.ID, ReadOnly: spec.ReadOnly,
			Model: model, ModelSet: modelSet, MaxCost: maxCost, MaxTurns: maxTurns})
	}
}

// ReadOnlyRole cho biết role là của một bước chỉ đọc (kind plan hoặc review của flow nhúng: planner, reviewer): tool MCP ghi
// vào cây làm việc không được cấp cho các role này dù mcp.grants có ghi. Chỉ để `bint mcp grants` hiển thị: role tuỳ biến
// của flow không có kind cố định nên luôn false ở đây; khi chạy thật, quyền ghi theo StepSpec.ReadOnly của bước (kind của
// bước trong flow đang chạy), nên bước plan/review của flow tuỳ biến vẫn bị bỏ tool ghi.
func ReadOnlyRole(role string) bool {
	if role == "" {
		return false
	}
	for _, d := range flow.Builtin() {
		for _, s := range d.Steps {
			if readOnlyKind(s.Kind) && s.EffectiveRole() == role {
				return true
			}
		}
	}
	return false
}
