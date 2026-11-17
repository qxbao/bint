# Yêu cầu
## Mục tiêu và phạm vi
Giảm việc thư mục dữ liệu Bint tăng mãi bằng tự dọn dữ liệu cũ. Đây là bản phác chờ người dùng xác định dữ liệu cần giữ và cách bật tự dọn; chưa đủ điều kiện triển khai.
## Yêu cầu chức năng
1. Tự dọn theo chính sách giữ dữ liệu sẽ chốt sau câu trả lời, có báo cáo dung lượng đã dọn và mục bỏ qua; không làm mất công việc đang dở.
## Tiêu chí chấp nhận
- [ ] AC-1: Kiểm tra mục tiêu `TestAutoCleanupStateGoal` xanh: dữ liệu cũ thuộc chính sách được dọn tự động, dung lượng giảm, dữ liệu mới và công việc đang dở được giữ nguyên.
## Kiểm tra mục tiêu
Chuẩn bị: trong state dir tạm, cài dữ liệu cũ/mới cho SQLite và WAL, runtime/tasks cùng log và phiên, spool pending/rejected/denials, eval-work, tmp, worktrees, generated, backups và tệp định danh. Dựng thêm profile iso tạm có cache/temp/session, tham chiếu tới tác vụ mở, khóa đang giữ và symlink ra ngoài. Ghi dung lượng và băm dữ liệu cần giữ trước hành động.
Hành động: kích hoạt đường tự dọn thực tế theo chính sách đã được người dùng chốt; nếu có xem trước thì kiểm tra xem trước trước khi kích hoạt.
Quét/đo: duyệt toàn bộ state dir, profile thử nghiệm và đích symlink bên ngoài; đối chiếu từng nhóm với chính sách, đo byte đã xóa và kiểm tra vẫn đọc/tiếp tục được tác vụ mở. SQLite phải kiểm tra qua truy vấn và kích thước tệp, không xóa WAL trực tiếp.
Điều kiện đạt: dữ liệu đủ điều kiện được dọn, dung lượng giảm đúng dữ liệu đã xóa, nhóm được giữ không đổi, lần dọn lặp không xóa thêm dữ liệu cần giữ; báo đường dẫn hoặc hàng dữ liệu sai theo từng nhóm. Mốc tuổi, tập được xóa và cách bật còn phụ thuộc Q-1/Q-2.
Tên test và tệp dự kiến: `TestAutoCleanupStateGoal` tại `internal/app/cleanup_goal_test.go`, thuộc lệnh đã có `go test -count=1 -timeout 30m ./...`; bước này không chạy test.
## Ngoài phạm vi
Không dọn thư mục cá nhân ngoài nơi Bint quản lý; dữ liệu ngoài đó vẫn chiếm dung lượng. Chưa loại nhóm dữ liệu Bint nào khỏi khảo sát; chỉ chốt tập được xóa sau trả lời.
## Câu hỏi mở
- [ ] Q-1: Bạn cần giữ lại những dữ liệu nào (chẳng hạn lịch sử tác vụ, log, bản sao lưu), và muốn giữ chúng trong bao lâu nếu đã có mốc mong muốn?
- [ ] Q-2: Lần đầu áp dụng cho dữ liệu đang có, bạn muốn xem trước rồi bật tự dọn, hay cho phép tự dọn ngay theo chính sách đã chốt; có công việc nào tuyệt đối không được chậm hoặc gián đoạn?
