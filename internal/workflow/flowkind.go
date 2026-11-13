package workflow

// Runner chọn bước, trạng thái tác vụ, chế độ chỉ đọc và cổng sau bước theo kind của flow (GĐ3 của
// docs/custom-flow-plan.md), không theo tên bước. Các hằng StepReq/StepFeat/StepReview/StepDo chỉ còn là tên workflow
// gốc (khoá limits.workflows.<tên>, sự kiện spool, tên log, lệnh `bint run <tên>`), không dùng để rẽ nhánh hành vi.

import (
	"bint/internal/flow"
)

// builtinFlow trả flow nhúng theo tên workflow; Def rỗng (không bước nào) nếu tên không phải workflow gốc.
func builtinFlow(name string) flow.Def {
	d, _ := flow.GetBuiltin(name)
	return d
}

// kindOf trả kind của bước theo flow đang chạy; "" với bước giả (post-feat, verify, scope) hoặc id không thuộc flow.
func (rs *runState) kindOf(step string) flow.Kind {
	s, _ := rs.flow.StepByID(step)
	return s.Kind
}

// implementStep trả id bước implement (đầu tiên) của flow đang chạy; "" nếu flow không có.
func (rs *runState) implementStep() string {
	s, _ := rs.flow.FirstOfKind(flow.KindImplement)
	return s.ID
}

// readOnlyKind: bước plan và review không được sửa tệp; chỉ bước implement có quyền ghi.
func readOnlyKind(k flow.Kind) bool { return k == flow.KindPlan || k == flow.KindReview }

// stepReadOnly cho biết bước (id của flow nhúng) có phải bước chỉ đọc; id lạ → false.
func stepReadOnly(step string) bool {
	s, ok := flow.BuiltinStep(step)
	return ok && readOnlyKind(s.Kind)
}
