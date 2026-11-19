---
schema: 1
id: 01M7TEJG4NZ81HYY0QR6C6A5GF
type: warning
status: verified
title: Trên Windows, os.Rename đè lên tệp đang được tiến trình/goroutine khác mở (os.Open của Go không có FILE_SHARE_DELETE) t…
scope:
  paths:
    - internal/**
evidence:
  - path: internal/fsafe/rename_windows.go
    lines: 15-30
    fingerprint: lines:efbd29d01f620c47
origin:
  kind: agent
  session: 930cb81b-bb95-400b-bb60-265403cc2bf5
  commit: c6972a0c7e799ea7aca6e2a6e3691730ab5e076d
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-19T17:47:08Z"
version: 1
created_at: "2026-11-19T17:44:49Z"
updated_at: "2026-11-19T17:47:08Z"
verified_at: "2026-11-19T17:47:08Z"
status_reason: người duyệt chấp nhận
---

Trên Windows, os.Rename đè lên tệp đang được tiến trình/goroutine khác mở (os.Open của Go không có FILE_SHARE_DELETE) trả Access is denied. Ghi nguyên tử tệp mà TUI hay tiến trình bint khác có thể đang đọc phải đi qua fsafe.WriteAtomic (có thử lại tới 2 s), đừng tự viết CreateTemp + os.Rename.
