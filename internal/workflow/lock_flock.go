package workflow

import (
	"os"

	"bint/internal/fslock"
)

// tryFlock xin khoá không chờ trên tệp (chia sẻ hoặc độc quyền); busy=true nếu đang có chủ
// khác giữ khoá không tương thích. Khoá gắn với mô tả tệp đang mở nên hệ điều hành tự nhả
// khi tiến trình chết (cài đặt theo nền tảng ở gói fslock).
func tryFlock(f *os.File, mode LockMode) (busy bool, err error) {
	return fslock.Try(f, mode == LockExclusive)
}
