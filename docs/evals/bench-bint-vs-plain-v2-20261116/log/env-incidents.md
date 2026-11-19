# Sự cố môi trường (mục 6.2)

- ~10:30–10:47Z: ổ / đầy 100% (183/193 GB; còn 4,6 MB). Kiểm chứng verify-1 của B-T2 (go test ./... sau b-feat vòng 1) đỏ vì "no space left on device"; đó là lỗi môi trường, không phải lỗi mã. Xử lý 10:50Z: xoá thư mục tạm của hai lượt A đã xong (~880 MB rác test, giữ tệp .md) và cache staticcheck (2,7 GB, sẽ tự sinh lại). Sau đó còn trống 14 GB. Không can thiệp vào lượt B-T2: Bint tự chạy vòng sửa sau review (5 phát hiện, 2 nghiêm trọng) và sẽ kiểm chứng lại.
- /tmp/.git do sandbox Codex tạo lại lúc 17:18 (giờ VN) dù TMPDIR riêng; sẽ xoá sau đợt chạy. Agent bên A nhắc lỗi Git/VCS khi chạy toàn bộ test, có thể do thư mục này.
