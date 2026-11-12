package ui

import (
	"context"
	"errors"
	"strings"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/knowledge"
	"bint/internal/tui/actions"
)

// ErrText dịch một lỗi của tầng dịch vụ để hiện cho người dùng: lỗi đã biết (chỉ dành cho người, chưa
// khởi tạo, chuyển trạng thái, thiếu ngân sách, huỷ) có khoá i18n riêng; lỗi khác hiện nguyên văn kèm
// nhãn "chi tiết kỹ thuật" (docs/tui-plan.md mục 7.1).
func ErrText(rw Rewriter, err error) string {
	cat := rw.Cat
	var ee *app.EvidenceError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ee):
		return evidenceErrText(rw, ee)
	case errors.Is(err, app.ErrAgentForbidden):
		return cat.T("err.agent_forbidden")
	case errors.Is(err, app.ErrNotInitialized):
		return cat.T("err.not_initialized")
	case errors.Is(err, knowledge.ErrTransition):
		return cat.T("err.transition", i18n.A{"detail": rw.Rewrite(err.Error())})
	case errors.Is(err, app.ErrNoBudget):
		return cat.T("err.no_budget")
	case errors.Is(err, actions.ErrWorkflowBusy):
		return cat.T("err.workflow_busy")
	case errors.Is(err, actions.ErrEvolveBusy):
		return cat.T("err.evolve_busy")
	case errors.Is(err, actions.ErrEvalBusy):
		return cat.T("err.eval_busy")
	case errors.Is(err, app.ErrCancelled), errors.Is(err, context.Canceled):
		return cat.T("err.cancelled")
	}
	return cat.T("err.technical", i18n.A{"detail": rw.Rewrite(err.Error())})
}

// FindingText dịch mã của một finding kiểm tra tri thức; chi tiết đi kèm nguyên văn (kỹ thuật).
func FindingText(rw Rewriter, f knowledge.Finding) string { return findingText(rw, f, true) }

// FindingCause như FindingText nhưng finding bằng chứng chỉ nêu nguyên nhân, không có cách xử lý: dùng khi mục chưa được ghi (thêm mục),
// nên không có mục nào để duyệt, cập nhật hay sửa.
func FindingCause(rw Rewriter, f knowledge.Finding) string { return findingText(rw, f, false) }

func findingText(rw Rewriter, f knowledge.Finding, withFix bool) string {
	cat := rw.Cat
	label := f.Code
	if k := "finding." + f.Code; cat.Has(k) {
		label = cat.T(k)
	}
	if iss, ok := f.IssueOf(); ok {
		if withFix {
			return label + " — " + EvidenceText(rw, iss)
		}
		return label + " — " + strings.Join(EvidenceCause(rw, iss), "; ")
	}
	if f.Detail == "" {
		return label
	}
	return label + " — " + rw.Rewrite(f.Detail)
}
