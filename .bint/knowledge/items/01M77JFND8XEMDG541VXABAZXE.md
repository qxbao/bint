---
schema: 1
id: 01M77JFND8XEMDG541VXABAZXE
type: warning
status: deprecated
title: 'Golden system_update_form_* chứa đường dẫn thư mục làm việc: go test ./internal/tui/app lỗi trong worktree của tác vụ (…'
scope:
  paths:
    - internal/tui/app/**
evidence:
  - path: internal/tui/app/system_golden_test.go
    lines: "105"
    fingerprint: lines:f15973e62917230f
origin:
  kind: agent
  session: 72ad875c-8bdd-43a0-96a3-244e62243829
  commit: d81ab271f15631839e5ac6aba967563b0878d38a
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-13T03:46:30Z"
version: 1
created_at: "2026-11-12T09:47:36Z"
updated_at: "2026-11-13T03:46:30Z"
status_reason: Lỗi thời
---

Golden system_update_form_* chứa đường dẫn thư mục làm việc: go test ./internal/tui/app lỗi trong worktree của tác vụ (đường dẫn khác cây gốc) dù mã không đổi; chạy lại ở cây gốc, đừng chạy -update trong worktree.
