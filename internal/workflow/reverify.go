package workflow

import (
	"context"
	"strings"

	"bint/internal/task"
)

// staleVerifyOnly cho biết cổng done của t chỉ bị chặn vì lần kiểm chứng cuối đã cũ do tệp NGOÀI tác vụ đổi sau đó
// (người hay tác vụ khác commit/land vào cùng cây), và trả về các tệp đó. Khi đúng, Finalize tự chạy lại lệnh kiểm
// chứng đã được người xác nhận (kiểm chứng phải chạy trên mã hiện tại, không bỏ qua cổng) thay vì dừng chờ người.
//
// Điều kiện: phiếu có mã (không phải chỉ lập kế hoạch); lệnh kiểm chứng đã xác nhận, lần chạy cuối qua, đúng lệnh
// hiện tại và có ảnh chụp cây; vân tay mã khác lần đó; mọi tệp đổi kể từ đó đều ngoài Touched (có ít nhất một);
// và nếu coi lần kiểm chứng là còn mới thì mọi điều chặn khác của cổng done (đường dẫn bảo vệ chưa duyệt, tiêu chí
// chưa đạt, đưa về xung đột...) đều rỗng: lý do chặn khác nào cũng loại việc chạy lại, không phải liệt kê tay.
func (e *Env) staleVerifyOnly(ctx context.Context, t *task.Task) ([]string, bool) {
	if e.planOnlyTask(t) || t.State.Landed != nil {
		return nil, false
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" || !e.VerifyConfirmed(t) {
		return nil, false
	}
	v := t.State.LastVerification()
	if v == nil || v.Command != cmd || !v.Passed || v.Tree == "" || v.Worktree == "" {
		return nil, false
	}
	pg := e.planGateFor(ctx, t)
	in, err := e.DoneInputDetailed(ctx, t)
	if err != nil || in.Worktree == "" || in.Worktree == v.Worktree ||
		len(in.OutsideSinceVerify) == 0 || len(in.TouchedSinceVerify) > 0 {
		return nil, false
	}
	fresh := in
	fresh.Worktree = v.Worktree // coi như lần kiểm chứng còn mới: chỉ xét các lý do chặn còn lại
	fresh.OutsideSinceVerify = nil
	fresh.IgnoreLanding = t.State.NeedsLand()
	if len(e.doneBlockersFor(ctx, t, fresh, nil, pg)) > 0 {
		return nil, false
	}
	return in.OutsideSinceVerify, true
}

// reverifyReason là lý do ghi vào Verification.Reason khi Finalize tự kiểm chứng lại.
func reverifyReason(files []string) string {
	return "mã khác đổi sau lần kiểm chứng: " + strings.Join(files, ", ")
}
