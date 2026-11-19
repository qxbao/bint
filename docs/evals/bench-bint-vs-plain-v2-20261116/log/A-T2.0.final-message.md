Đã sửa mã nguồn để che mật khẩu/token trước khi ghi log tiến trình, trace hook và codegraph, kể cả token bị chia giữa nhiều lần ghi. Dữ liệu giao tiếp với agent được giữ nguyên.

Kiểm thử hồi quy, race và `go vet` đã qua. Toàn bộ suite chưa xác minh được do giới hạn socket, lỗi Git/VCS và timeout.

Log cũ chưa được làm sạch. Nếu từng chứa credential thật, cần đổi/thu hồi credential và xử lý các bản log đã chia sẻ.