package mcpgw

import "sync"

// handleKiller kết thúc rồi đóng một handle hệ điều hành (Job Object trên Windows) đúng một lần dù kill bị gọi đồng thời từ
// nhiều goroutine (die() của stop/readLoop/writeLoop và goroutine chờ cmd.Wait). Đóng một handle hai lần là data race và có
// thể đóng nhầm handle đã được hệ điều hành cấp lại. Hai hàm giải phóng được tiêm vào (Windows: TerminateJobObject,
// CloseHandle) nên chính mã kill chạy được trong test trên Linux.
type handleKiller struct {
	h         uintptr
	terminate func(h uintptr) error
	closeH    func(h uintptr) error
	once      sync.Once
}

func newHandleKiller(h uintptr, terminate, closeH func(uintptr) error) *handleKiller {
	return &handleKiller{h: h, terminate: terminate, closeH: closeH}
}

// kill chạy terminate rồi close ở lần gọi đầu tiên; các lần gọi sau (kể cả đồng thời) chờ lần đầu xong rồi trở về.
func (k *handleKiller) kill() {
	if k == nil || k.h == 0 {
		return
	}
	k.once.Do(func() {
		_ = k.terminate(k.h)
		_ = k.closeH(k.h)
	})
}
