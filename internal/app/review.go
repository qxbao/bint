package app

import (
	"context"
	"slices"
	"strings"

	"bint/internal/knowledge"
)

// ReviewQueueOptions chọn các mục chờ duyệt cho `bint knowledge review` (và hàng đợi duyệt
// trong TUI). Cờ CLI: --status.
type ReviewQueueOptions struct {
	// Statuses là các trạng thái cần duyệt (mặc định candidate, stale).
	Statuses []string `flag:"status"`
}

// ReviewQueueDefaults: mục chờ duyệt mặc định là candidate và stale.
func ReviewQueueDefaults() ReviewQueueOptions {
	return ReviewQueueOptions{Statuses: []string{knowledge.StatusCandidate, knowledge.StatusStale}}
}

// Validate từ chối trạng thái không tồn tại.
func (o ReviewQueueOptions) Validate() error {
	for _, s := range o.Statuses {
		if !slices.Contains(knowledge.Statuses, s) {
			return Usagef("--status: %q không hợp lệ (%s)", s, strings.Join(knowledge.Statuses, ", "))
		}
	}
	return nil
}

// ReviewSession là không gian tri thức đã mở kèm danh sách mục chờ duyệt.
type ReviewSession struct {
	K       *KnowledgeWS
	Pending []string
}

// ReviewQueue mở kho tri thức ở dir và trả ID các mục đang chờ duyệt theo thứ tự trong kho. Là
// thao tác chỉ dành cho người: trong phiên agent trả lỗi bọc ErrAgentForbidden (kiểm tra trước
// khi mở kho, như lệnh CLI cũ).
func ReviewQueue(ctx context.Context, dir string, opts ReviewQueueOptions) (*ReviewSession, error) {
	if err := RequireHuman("bint knowledge review"); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	items, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	rs := &ReviewSession{K: k}
	for _, it := range items {
		if slices.Contains(opts.Statuses, it.Status) {
			rs.Pending = append(rs.Pending, it.ID)
		}
	}
	return rs, nil
}
