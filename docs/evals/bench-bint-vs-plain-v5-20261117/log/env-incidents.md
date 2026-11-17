2026-11-17T10:42:57Z xoá bản chép tạm scratch-T1/T2 (+ thư mục state của chúng) dùng để thử classify; đường nhanh được chọn cho cả hai prompt
2026-11-17T13:31:56Z dựng lại B-T1/B-T2 từ 8757340 bằng binary 9095646 merge: b-feat nối tiếp phiên b-req khi khác model (claude, codex)
2026-11-17T14:04:21Z đĩa 5,8 GB trước v5: xoá nội dung tmp/ của v3 và v4 (cache chạy của mình, ~645 MB); khối trust v5 đã thêm vào ~/.codex/config.toml (sao lưu log/codex-config.toml.before-bench)
2026-11-17T14:04:28Z đĩa 6,5 GB vẫn thấp cho hai lượt song song (v4 B-T1 cần ~3 GB cache): chạy go clean -cache (~6,3 GB cache build chung, tái sinh được; chủ dự án đã cho phép tự dọn đĩa)
2026-11-17T15:19:49Z đĩa 4,2 GB: theo lệnh của chủ dự án, docker rmi 32 image không dùng bởi container nào, trừ tmu-* và codex-lb (không đụng container/volume)
2026-11-17T15:34:24Z đĩa 2,1 GB sau test ẩn T1: xoá tmp/hidden-B-T1 và go clean -cache (cache build chung, tái sinh được)
