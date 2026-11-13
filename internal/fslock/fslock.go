// Package fslock là khoá tệp không chờ của hệ điều hành (flock trên Unix, LockFileEx trên
// Windows), dùng chung cho các khoá liên tiến trình của Bint.
//
// Khoá gắn với mô tả/handle tệp đang mở nên hệ điều hành tự nhả khi tiến trình chết hoặc khi
// đóng tệp: không có bước "thu hồi khoá của PID đã chết" (thứ có thể gỡ nhầm khoá của chủ
// mới đang sống). Người dùng gói không được xoá tệp khoá khi trả.
package fslock

import "os"

// Try xin khoá không chờ trên f (độc quyền hoặc chia sẻ); busy=true nếu đang có chủ khác giữ
// khoá không tương thích. Lỗi khác (không phải bận) trả về trong err. Khoá được nhả khi đóng f.
func Try(f *os.File, exclusive bool) (busy bool, err error) { return tryLock(f, exclusive) }
