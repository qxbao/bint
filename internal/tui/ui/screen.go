package ui

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/theme"
)

// Screen là giao diện chung của các màn hình chuyên biệt (Tri thức, Tác vụ, ...): mô hình gốc giữ chúng trong
// một bảng theo màn hình và không rẽ nhánh theo từng màn (docs/tui-plan.md mục 12.3 hẹn tách khi có màn thứ hai).
type Screen interface {
	// SetEnv đặt bảng màu và danh mục chuỗi (gọi lại khi đổi ngôn ngữ).
	SetEnv(th theme.Theme, cat *i18n.Catalog)
	// Ensure nạp dữ liệu lần đầu khi người dùng tới màn hình (nil nếu đã nạp hoặc đang nạp).
	Ensure() tea.Cmd
	// Update xử lý thông điệp không phải phím (kết quả nạp, kết quả hành động, từ chối); trả lệnh kế tiếp.
	Update(msg tea.Msg) tea.Cmd
	// Key xử lý một phím; handled = false nghĩa là màn hình không dùng phím này (mô hình gốc xử lý như phím toàn cục).
	Key(msg tea.KeyPressMsg) (tea.Cmd, bool)
	// Capturing: màn hình đang nhận mọi phím chữ (ô nhập, form, hộp thoại).
	Capturing() bool
	// WantsInterrupt: có việc của màn hình đang chạy; Esc/Ctrl+C huỷ việc đó thay vì thoát/lùi.
	WantsInterrupt() bool
	// Busy: có việc nền của màn hình đang chạy (nạp dữ liệu...).
	Busy() bool
	// NotList: đang ở thẻ phụ có phím riêng ở viền dưới (chân phím chung chỉ giữ phím ghim).
	NotList() bool
	// Command thực hiện hành động theo ID (từ bảng lệnh).
	Command(id string) tea.Cmd
	// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
	View(env Env) string
}
