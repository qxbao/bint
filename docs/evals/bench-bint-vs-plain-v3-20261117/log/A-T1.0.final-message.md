Đã thêm tự dọn dữ liệu phụ quá **30 ngày**, tối đa mỗi ngày khi bint nạp sự kiện. Giữ nguyên tri thức, phiếu tác vụ, worktree và SQLite.

- Xem trước: `bint gc --dry-run`
- Dọn ngay: `bint gc`
- Đổi thời hạn: `state.retention_days` trong `.bint/local.yml`; `0` để tắt.

Kiểm thử tính năng và `go vet` đã qua. Bộ kiểm thử toàn dự án còn lỗi do sandbox chặn mạng và hết thời gian.

Thay đổi nằm trong mã nguồn; cần build/cài lại bint để sử dụng.