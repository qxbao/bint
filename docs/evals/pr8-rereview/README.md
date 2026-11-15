# PR #8 — kết quả review và kiểm chứng

Cây `duc` đã đồng bộ main `69b3554` qua merge `8b8a90a`.
Phiếu Bint: `01M7EBQH6CZ2TZ962EV6QZG8R2` và `01M7EHRRFGBS85RXA06VYRDPJA`.

## Các sửa đổi

- Codex: đọc đúng trust và project-root markers, gồm linked worktree và đường dẫn tuyệt đối; bỏ ảnh hưởng biến Git của tiến trình cha khi tìm cấu hình MCP.
- Tiến trình agent: đồng bộ bộ đệm log stdout/stderr; kiểm tính toàn vẹn dữ liệu mà không giả định hai luồng ghép nguyên dòng.
- Eval: giữ BatchID/trạng thái khi hủy chuẩn bị và trả cả lỗi ghi SQLite; đồng bộ fixture TUI theo hộp thoại và kết quả action.
- Import tri thức từ main: khử trùng theo nội dung, giữ globs hợp lệ, xử lý frontmatter lỗi/riêng tư an toàn, sửa bộ lọc trạng thái, vô hiệu preview cũ và ẩn đường dẫn nguồn trong lỗi.
- Các test hồi quy nằm trong mã nguồn; giữ CLI inventory và nội dung hướng dẫn sử dụng liên quan.

## Kiểm chứng nguồn cuối — 2026-11-15

Full suite, race rộng 14 package, live Codex CLI và phân tích tĩnh đều PASS.
Review độc lập cuối: 0 phát hiện mở ở mọi mức độ.

```sh
go test ./...
go test -race -count=1 -timeout 20m \
  ./internal/codexconfig ./internal/gitrepo ./internal/eval \
  ./internal/harness/codex ./internal/harness/hookcore \
  ./internal/knowledge ./internal/knowledge/importer ./internal/app ./internal/cli \
  ./internal/tui/actions ./internal/tui/screens/knowledge \
  ./internal/tui/screens/initwizard ./internal/tui/screens/overview ./internal/tui/app
BINT_CODEX_CONFIG_LIVE=1 go test -count=3 -timeout 3m \
  ./internal/eval -run '^TestCodexMCPConfigLive$' -v
go vet ./...
GOOS=windows go vet ./...
staticcheck ./...
GOOS=windows staticcheck ./...
```

Windows mới kiểm phân tích tĩnh/cross-compile; chưa kiểm runtime Windows,
inference hoặc tools/list của dịch vụ MCP thật.

Log thô, manifest nguồn, patch và báo cáo trung gian được lưu ngoài Git.
Nhật ký workflow gốc vẫn nằm trong thư mục trạng thái Bint theo ID phiếu.
