package workflow

import "bint/internal/flow"

// sp dựng StepSpec của một bước flow nhúng theo id (b-req, b-feat, b-review), cho kiểm thử gọi AgentFactory trực tiếp.
func sp(step string) StepSpec {
	s, _ := flow.BuiltinStep(step)
	return specOf(s)
}

// goldenReadOnly, goldenSubagent, goldenRole: thuộc tính theo id bước của flow nhúng, chỉ để sinh/so golden GĐ3 (mã sản
// phẩm không còn tra theo id bước: nguồn là StepSpec dựng từ flow đang chạy).
func goldenReadOnly(step string) bool {
	s, ok := flow.BuiltinStep(step)
	return ok && readOnlyKind(s.Kind)
}

func goldenSubagent(step string) string {
	s, ok := flow.BuiltinStep(step)
	if !ok {
		return ""
	}
	return subagentOf(s.Kind)
}
