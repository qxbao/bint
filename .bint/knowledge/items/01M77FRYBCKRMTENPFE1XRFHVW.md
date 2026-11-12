---
schema: 1
id: 01M77FRYBCKRMTENPFE1XRFHVW
type: warning
status: candidate
title: 'Kiểm Lstat trước rồi mở lại toàn đường dẫn có race symlink: os.Root chỉ chặn thoát root, không chặn chuyển hướng vào .b…'
scope:
  paths:
    - internal/eval/codex_read_tools.go
evidence:
  - path: internal/eval/codex_read_tools.go
    lines: "80"
    fingerprint: lines:54253a7017434466
origin:
  kind: agent
  commit: e9ce282ba1671a476b195eb9b5d2a490fb77acac
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-12T09:00:15Z"
---

Kiểm Lstat trước rồi mở lại toàn đường dẫn có race symlink: os.Root chỉ chặn thoát root, không chặn chuyển hướng vào .bint/evals. Phải kiểm danh tính từng đối tượng đã mở và dùng handle thư mục đã kiểm.
