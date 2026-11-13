---
schema: 1
id: 01M79KJX8AQHHAFRA7ZX2XVAAB
type: warning
status: candidate
title: README main 04214d8 làm test tài liệu TUI thất bại
scope:
  paths:
    - README.md
    - internal/cli/readme_tui_test.go
evidence:
  - path: internal/cli/readme_tui_test.go
    lines: "16"
    fingerprint: lines:a32a3bb7121485eb
  - path: README.md
    lines: "63"
    fingerprint: lines:e48c2caef70f5b44
origin:
  kind: agent
  commit: 04214d8babb97baa3994dd0cb571418df21706ab
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-13T04:45:20Z"
---

Fast-forward tới origin/main 04214d8 rồi go test ./... thất bại ở TestREADMEDocumentsTUIGlobalKeys: README thiếu tiêu đề ## Giao diện terminal (TUI). README và test khớp origin/main; đây là hồi quy có sẵn trên main, không phải lỗi gộp cục bộ. Các package khác cùng lượt đã qua; go vet Linux/Windows qua. Kiểm lại toàn bộ hợp đồng readme_tui_test.go khi sửa cấu trúc README. Binary bint cũ trên PATH cũng chưa hiểu khóa mcp mới; khi kiểm chứng commit này dùng binary dựng từ source hiện tại.
