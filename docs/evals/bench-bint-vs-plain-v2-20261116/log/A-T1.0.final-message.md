Đã thêm tự dọn log, bản sao lưu và dữ liệu đánh giá cũ sau **30 ngày**, tối đa mỗi ngày khi Bint nạp sự kiện. Giữ nguyên lịch sử SQLite, tài liệu và tác vụ còn tiếp tục được.

- Xem trước: `bint clean --dry-run`
- Dọn ngay: `bint clean`
- Đổi thời hạn: `state.retention_days`; đặt `0` để tắt.

Kiểm thử mới, race và `go vet` đã qua. Bộ kiểm thử tổng thể còn lỗi môi trường và timeout; [chi tiết](/home/baodq/bench-bint-v2-20261116/tmp/A-T1-run/cleanup-verification.md).

Thay đổi nằm trong mã nguồn, chưa cài lại binary trên máy.