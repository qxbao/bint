package app

import (
	"errors"
	"fmt"
)

// Nguyên thuỷ dùng chung của tầng dịch vụ: CLI, TUI (và về sau giao diện khác) cùng gọi các
// hàm trong app và nhận lại cấu trúc dữ liệu, không phân tích văn bản đầu ra.

// UsageError là lỗi dùng sai (tham số loại trừ nhau, thiếu tham số bắt buộc, giá trị ngoài
// khoảng...) do Validate() hoặc dịch vụ phát hiện. CLI ánh xạ nó sang mã thoát 2 như
// usageError cũ; TUI hiện nó như lỗi nhập form.
type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

// Usagef dựng UsageError từ định dạng.
func Usagef(format string, a ...any) error {
	return UsageError{Err: fmt.Errorf(format, a...)}
}

// IsUsage cho biết lỗi (hoặc lỗi bọc bên trong) là UsageError.
func IsUsage(err error) bool {
	var u UsageError
	return errors.As(err, &u)
}

// ErrCancelled: người dùng từ chối ở một lời hỏi xác nhận (Confirm trả false).
var ErrCancelled = errors.New("đã huỷ")

// ErrNeedYes: cần --yes khi chạy không tương tác (không có Confirm và không Yes).
func errNeedYes() error { return Usagef("cần --yes khi chạy không tương tác") }

// ConfirmRequest là một lời hỏi xác nhận giữa chừng của dịch vụ. Text là câu hỏi tiếng Việt
// (CLI in nguyên văn); Key là khoá i18n để TUI dịch; Detail là nội dung kèm theo (ví dụ bản
// thay đổi sắp ghi), có thể rỗng.
type ConfirmRequest struct {
	Key    string
	Text   string
	Detail string
}

// ConfirmFunc trả true nếu người dùng đồng ý. Hàm có thể chặn (TUI chờ hộp thoại trả lời).
type ConfirmFunc func(ConfirmRequest) bool

// ConfirmOrFail áp quy tắc chung cho thao tác cần xác nhận: yes bỏ qua lời hỏi; không yes mà
// không có người trả lời được (confirm == nil) là lỗi dùng sai; người trả lời "không" thì
// ErrCancelled.
func ConfirmOrFail(yes bool, confirm ConfirmFunc, req ConfirmRequest) error {
	if yes {
		return nil
	}
	if confirm == nil {
		return errNeedYes()
	}
	if !confirm(req) {
		return ErrCancelled
	}
	return nil
}
