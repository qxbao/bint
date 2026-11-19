//go:build windows

package fsafe

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// renameRetryFor là tổng thời gian thử lại khi đổi tên vướng tệp đang mở.
const renameRetryFor = 2 * time.Second

// rename như os.Rename nhưng thử lại khi tệp đích đang được mở ở nơi khác. Trên Windows, os.Open của Go không
// cho FILE_SHARE_DELETE nên một lần đọc trùng lúc (TUI quét trạng thái, tiến trình bint khác) làm MoveFileEx trả
// ERROR_ACCESS_DENIED hoặc ERROR_SHARING_VIOLATION dù người đọc chỉ giữ tệp vài mili giây. Cùng cách với
// cmd/go/internal/robustio.
func rename(oldpath, newpath string) error {
	var err error
	delay := time.Millisecond
	deadline := time.Now().Add(renameRetryFor)
	for {
		if err = os.Rename(oldpath, newpath); err == nil || !transientRenameErr(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, 100*time.Millisecond)
	}
}

const (
	errAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	errSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
)

func transientRenameErr(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errAccessDenied || errno == errSharingViolation)
}
