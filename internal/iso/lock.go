package iso

import (
	"os"
	"sync"

	"bint/internal/fslock"
)

// Lock xin khoá phiên trên session.lock của profile bằng fslock.Try (không chờ): khoá chia sẻ (S) do mỗi
// phiên `bint iso run` giữ, khoá độc quyền (X) do gc/remove lấy (docs/iso-plan.md mục 5.3). busy=true nếu
// đang có chủ khác giữ khoá không tương thích (release nil). release đóng tệp để nhả khoá, gọi nhiều lần
// cũng được; tệp session.lock được giữ lại (xoá nó có thể làm hai chủ giữ hai inode khác nhau). Hệ điều
// hành tự nhả khi tiến trình chết, nên không có bước thu hồi theo PID.
func (p Profile) Lock(exclusive bool) (release func() error, busy bool, err error) {
	f, err := os.OpenFile(p.LockPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	busy, err = fslock.Try(f, exclusive)
	if err != nil || busy {
		f.Close()
		return nil, busy, err
	}
	var once sync.Once
	var cerr error
	return func() error {
		once.Do(func() { cerr = f.Close() })
		return cerr
	}, false, nil
}
