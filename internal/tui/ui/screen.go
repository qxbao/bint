package ui

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/theme"
)

// Screen là giao diện chung của các màn hình chuyên biệt (Tri thức, Tác vụ, ...): mô hình gốc giữ chúng trong
// một bảng theo màn hình và không rẽ nhánh theo từng màn (docs/tui-plan.md mục 12.3 hẹn tách khi có màn thứ hai).
type Screen interface {
	// SetEnv đặt bảng màu và bộ viết lại thông điệp (danh mục chuỗi, sổ hành động, chế độ chỉ đọc của mô hình; gọi lại khi đổi ngôn ngữ).
	SetEnv(th theme.Theme, rw Rewriter)
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

// Pointer là giao diện tuỳ chọn của màn hình nhận chuột. id là id vùng do chính màn hình (hoặc component dùng chung) đánh dấu khi vẽ:
// "r:<i>" hàng thứ i của danh sách hiện tại, "t:<i>" thẻ phụ thứ i, "p:<pane>" khung (focus khung đó).
type Pointer interface {
	// Click xử lý bấm trái vào vùng id; trả lệnh kế tiếp (nil nếu không có việc).
	Click(id string) tea.Cmd
	// Wheel cuộn khung pane (tên khung trong "p:<pane>") dy dòng; dy dương = xuống.
	Wheel(pane string, dy int) tea.Cmd
}

// Toaster là giao diện tuỳ chọn: trả nguyên văn thông báo hiện tại của màn hình (rỗng = không có), để hộp Thông báo của
// mô hình gốc hiện đủ nội dung dù thanh phụ đã rút gọn bằng "…".
type Toaster interface {
	Toast() (text string, isErr bool)
}

// Helper là giao diện tuỳ chọn cho trợ giúp theo ngữ cảnh: mã (không dịch) của thẻ phụ đang xem ("" = màn không có thẻ phụ), dùng ghép
// khoá giới thiệu help.ctx.<màn>.<mã>. Các phím của thẻ thu từ chân phím trong khung qua Env.Collect.
type Helper interface {
	HelpTab() string
}

// SubTabber là giao diện tuỳ chọn: tên (đã dịch) của thẻ phụ đang chọn, đúng chữ hiện trên thanh thẻ phụ ("" = màn không có thẻ phụ).
type SubTabber interface {
	SubTab() string
}
