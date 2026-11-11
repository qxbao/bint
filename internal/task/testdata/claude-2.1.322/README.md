Phiên Claude Code 2.1.322 thật chạy b-review chỉ đọc qua `bint run` (đã che đường dẫn, task ID).

- `review-denied-command.txt`: lệnh Bash agent gửi; Claude Code trả lỗi nguyên văn
  `Contains brace with quote character (expansion obfuscation)` (kiểm tra an toàn có sẵn,
  không phải rule allowlist; tái hiện độc lập ở phiên haiku riêng với allowlist đầy đủ).
- `review-denied-heredoc.json`: nội dung JSON trong heredoc của lệnh đó.
