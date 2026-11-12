package ui

import (
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// ErrBlock ngắt dòng một thông điệp lỗi rộng tối đa w ô (không cắt bằng …): dòng đầu có "✗ " màu lỗi, các dòng sau thụt treo theo; indent là
// phần thụt chung bên trái.
func ErrBlock(th theme.Theme, msg string, w int, indent string) []string {
	return text.WrapHang(msg, max(w, 12), indent+th.S(theme.Err, "✗ "), indent+"  ")
}
