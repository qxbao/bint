---
schema: 1
id: 01M7FGC944KVT6NY1TZE7MJ0P8
type: warning
status: verified
title: 'Khoá liên tiến trình: dùng fslock.Try (flock/LockFileEx), không tự thu hồi khoá theo PID chết'
scope: {}
evidence:
  - path: internal/fslock/fslock.go
    lines: 11-13
    fingerprint: lines:faa97740c5da74a9
  - path: internal/evolve/apply.go
    lines: 44-57
    fingerprint: lines:b134773e6d8edd42
  - path: internal/workflow/lock_flock.go
    fingerprint: file:4ae91eb34eb7f446
origin:
  kind: agent
  session: 72156351-c1ec-4ec5-bf72-03733b0694e6
  commit: 4bb1cd0bea26f509dfa22a6fed3f919689e7c2be
  inputs:
    - internal/fslock/fslock.go
    - internal/evolve/apply.go
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-15T11:45:54Z"
confidence: high
version: 1
created_at: "2026-11-15T11:44:44Z"
updated_at: "2026-11-15T11:45:54Z"
verified_at: "2026-11-15T11:45:54Z"
status_reason: người duyệt chấp nhận
---

Khoá giữa các tiến trình dùng `fslock.Try(f, exclusive)` (gói `internal/fslock`, cặp `internal/fslock/fslock_unix.go`/`internal/fslock/fslock_windows.go`): hệ điều hành tự nhả khoá khi tiến trình chết, nên không có bước thu hồi.
Đừng dựng lại mẫu O_EXCL + `task.ProcessAlive` + `os.Remove`: đọc khoá thấy PID chết rồi xoá có thể gỡ nhầm khoá của chủ mới đang sống (race R1, commit 755b544). Nội dung PID/thời điểm trong tệp khoá chỉ để báo lỗi, không dùng để quyết định khoá.
Mẫu: `evolve.AcquireLock` (sửa ở V0, NT-P6-01), `workflow` (`internal/workflow/lock_flock.go`), `task`. Thay cho mục 01M746PTBYPSXDT0WJZS38KPFD đã lỗi thời.
