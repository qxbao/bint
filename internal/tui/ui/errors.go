package ui

import (
	"context"
	"errors"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/knowledge"
	"bint/internal/tui/actions"
)

// ErrText dịch một lỗi của tầng dịch vụ để hiện cho người dùng: lỗi đã biết (chỉ dành cho người, chưa
// khởi tạo, chuyển trạng thái, thiếu ngân sách, huỷ) có khoá i18n riêng; lỗi khác hiện nguyên văn kèm
// nhãn "chi tiết kỹ thuật" (docs/tui-plan.md mục 7.1).
func ErrText(cat *i18n.Catalog, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, app.ErrAgentForbidden):
		return cat.T("err.agent_forbidden")
	case errors.Is(err, app.ErrNotInitialized):
		return cat.T("err.not_initialized")
	case errors.Is(err, knowledge.ErrTransition):
		return cat.T("err.transition", i18n.A{"detail": err.Error()})
	case errors.Is(err, app.ErrNoBudget):
		return cat.T("err.no_budget")
	case errors.Is(err, actions.ErrWorkflowBusy):
		return cat.T("err.workflow_busy")
	case errors.Is(err, app.ErrCancelled), errors.Is(err, context.Canceled):
		return cat.T("err.cancelled")
	}
	return cat.T("err.technical", i18n.A{"detail": err.Error()})
}

// FindingText dịch mã của một finding kiểm tra tri thức; chi tiết đi kèm nguyên văn (kỹ thuật).
func FindingText(cat *i18n.Catalog, f knowledge.Finding) string {
	label := f.Code
	if k := "finding." + f.Code; cat.Has(k) {
		label = cat.T(k)
	}
	if f.Detail == "" {
		return label
	}
	return label + " — " + f.Detail
}
