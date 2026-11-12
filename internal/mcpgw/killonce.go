package mcpgw

import "sync"

// killOnce bảo đảm hành động giải phóng (TerminateJobObject + CloseHandle trên Windows) chỉ chạy một lần dù kill bị
// gọi đồng thời từ nhiều goroutine (die() của stop/readLoop/writeLoop và goroutine chờ cmd.Wait). Đóng một handle hai
// lần là data race và có thể đóng nhầm handle đã được hệ điều hành cấp lại. Nằm ngoài tệp *_windows.go để test được
// trên Linux.
type killOnce struct{ once sync.Once }

// do chạy f ở lần gọi đầu tiên; các lần gọi sau (kể cả đồng thời) chờ f xong rồi trở về mà không chạy lại.
func (k *killOnce) do(f func()) { k.once.Do(f) }
