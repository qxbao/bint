package app

import (
	"fmt"
	"os"
	"sync"

	"bint/internal/config"
	"bint/internal/task"
)

var (
	ledgerWarnMu sync.Mutex
	ledgerWarnFn = func(msg string) { fmt.Fprintln(os.Stderr, "bint: "+msg) }
)

// LedgerWarnings gom cảnh báo của sổ phiếu cho giao diện không in được ra stderr (TUI): Add nhận cảnh báo (an toàn
// khi gọi từ nhiều goroutine), Drain lấy và xoá các cảnh báo chưa hiện.
type LedgerWarnings struct {
	mu   sync.Mutex
	msgs []string
}

// maxPendingLedgerWarnings giới hạn số cảnh báo chờ hiện (bỏ cảnh báo cũ nhất).
const maxPendingLedgerWarnings = 50

// Add thêm một cảnh báo.
func (q *LedgerWarnings) Add(msg string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, msg)
	if len(q.msgs) > maxPendingLedgerWarnings {
		q.msgs = q.msgs[len(q.msgs)-maxPendingLedgerWarnings:]
	}
}

// Drain trả và xoá các cảnh báo đang chờ.
func (q *LedgerWarnings) Drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.msgs
	q.msgs = nil
	return out
}

// SetLedgerWarn đặt nơi nhận cảnh báo của sổ phiếu (lỗi ghi .bint/tasks, xung đột, thay đổi chờ nhập). Mặc định
// in ra stderr; giao diện TUI chuyển cảnh báo vào LedgerWarnings để hiện thành thông báo (nil = bỏ).
func SetLedgerWarn(fn func(string)) {
	ledgerWarnMu.Lock()
	defer ledgerWarnMu.Unlock()
	if fn == nil {
		fn = func(string) {}
	}
	ledgerWarnFn = fn
}

func ledgerWarn(msg string) {
	ledgerWarnMu.Lock()
	fn := ledgerWarnFn
	ledgerWarnMu.Unlock()
	fn(msg)
}

// taskStore dựng kho phiếu của workspace: bật sổ Git (`.bint/tasks/`, tasks.ledger: git) trừ khi cấu hình đặt local.
// Tự nhập từ cây chỉ trong phiên người; phiên agent chỉ xuất và liệt kê thay đổi chờ người nhập.
func taskStore(ws *Workspace) task.Store {
	s := task.NewStore(ws.Paths.Runtime)
	if ws.Config.Tasks.Ledger == config.LedgerLocal {
		return s
	}
	inAgent, _ := InAgentSession()
	s.Ledger, s.Root, s.Import, s.Warn = true, ws.Repo.Toplevel, !inAgent, ledgerWarn
	return s
}
