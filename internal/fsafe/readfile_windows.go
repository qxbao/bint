//go:build windows

package fsafe

import (
	"os"
	"time"
)

// ReadFile như os.ReadFile nhưng thử lại (tới renameRetryFor) khi tệp đang bị thay ở nơi khác: trên Windows, mở tệp đúng lúc
// WriteAtomic đổi tên đè lên nó trả ERROR_ACCESS_DENIED/ERROR_SHARING_VIOLATION dù chỉ vướng vài mili giây (người đọc là
// phía còn lại của cùng cuộc đua mà rename đã xử lý). Dùng cho tệp có thể đang được tiến trình/goroutine khác ghi nguyên tử.
func ReadFile(name string) ([]byte, error) {
	delay := time.Millisecond
	deadline := time.Now().Add(renameRetryFor)
	for {
		data, err := os.ReadFile(name)
		if err == nil || !transientRenameErr(err) || time.Now().After(deadline) {
			return data, err
		}
		time.Sleep(delay)
		delay = min(delay*2, 100*time.Millisecond)
	}
}
