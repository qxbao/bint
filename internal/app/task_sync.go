package app

import (
	"context"
	"errors"

	"bint/internal/config"
	"bint/internal/task"
)

// TaskSyncOptions: `bint task sync [--json]` — đồng bộ ba bên phiếu tác vụ giữa thư mục cục bộ và `.bint/tasks/`.
type TaskSyncOptions struct{}

func TaskSyncDefaults(config.Config) TaskSyncOptions { return TaskSyncOptions{} }
func (TaskSyncOptions) Validate() error              { return nil }

// ErrLedgerConflict: có phiếu đổi cả hai phía; bản cục bộ được giữ.
var ErrLedgerConflict = errors.New("có phiếu xung đột giữa thư mục cục bộ và .bint/tasks")

// TaskSync đồng bộ phiếu: ghi ra cây phiếu cục bộ chưa có (ghi bù), nhập phiếu mới hơn từ cây, hợp findings,
// báo xung đột. Phiên agent chỉ xuất và liệt kê thay đổi chờ người nhập (Store.Sync human=false).
func TaskSync(_ context.Context, ws *TaskWS, _ TaskSyncOptions) (task.SyncReport, error) {
	if !ws.Tasks.Ledger {
		return task.SyncReport{}, Usagef("tasks.ledger đang là local: không đọc hay ghi %s", task.LedgerDir)
	}
	return ws.Tasks.Sync(ws.Human)
}
