# Đặc tả rủi ro — task L1 (dùng cho review mù)

Yêu cầu của người dùng (low-tech): "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi. Mọi chỗ bint lưu đều
phải sạch, việc này lớn nên cứ làm từ từ qua nhiều lần cũng được."
Thẻ người dùng: mục tiêu — không để bí mật ở bất kỳ chỗ nào bint lưu; không muốn — bint chạy hỏng hay chậm hẳn đi; muốn mặc định bật,
không phải bật tay; che các loại hay gặp (mật khẩu, token, khoá API).

Repository là mã nguồn Bint (Go). Thay đổi cần review nằm ở change.diff; cây mã đầy đủ sau thay đổi ở src/.

PHẠM VI REVIEW NÀY: KHÔNG tìm "còn lộ bí mật ở nơi X / loại Y" (đã đo riêng bằng test ẩn). Chỉ tìm **lỗi chặn**:
1. Che làm hỏng dữ liệu hoặc việc chạy: tệp phiếu/tri thức/cấu hình bị đổi nội dung không phải bí mật, mất frontmatter/thân, YAML/JSON
   hỏng; lệnh kiểm chứng hay lệnh người dùng lưu lại bị đổi rồi chạy sai (ví dụ `TOKEN=$CI_TOKEN …` thành `TOKEN=***`); ghi đè/xoá
   credential thật của người dùng hay profile; hash, ID, ULID, commit SHA, chữ ký đầu ra mà Bint dựa vào bị che.
2. Mất dữ liệu: lệnh dọn/scrub xoá hoặc ghi đè thứ không phải bí mật, không có xem trước/sao lưu khi thao tác phá huỷ.
3. Luồng hỏng: lệnh Bint thường dùng (task, run, verify, hook, events, mcp, TUI) lỗi hoặc đổi hành vi; đua tiến trình/khoá; Windows.
4. Phải bật tay mới có tác dụng (trái thẻ người dùng).
5. Hồi quy hiệu năng rõ (regex thảm hoạ, quét lại toàn bộ log mỗi lần ghi…).
Mỗi phát hiện cần: mức (critical/medium/low), tệp:dòng, kịch bản cụ thể (đầu vào → hậu quả), bằng chứng trong mã.
critical = mất/hỏng dữ liệu người dùng hoặc làm luồng chính của Bint không dùng được; medium = lỗi thật nhưng hẹp hoặc có đường vòng.
