Bạn đang chạy **binary cũ**, chưa có lệnh `clean`. Tôi đã build và xác nhận bản mới có lệnh này.

Để cập nhật đúng bản đang nằm trong `PATH`, chạy:

```sh
cd /home/baodq/bench-bint-v2-20261116/A-T1
go build -o "$(command -v bint)" ./cmd/bint
```

Sau đó quay lại dự án đã khởi tạo Bint và chạy `bint clean --dry-run`.

Tôi chưa thay binary giúp bạn được vì thư mục cài đặt nằm ngoài vùng phiên này được phép ghi.