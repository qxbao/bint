---
schema: 1
id: 01M746PTBYPSXDT0WJZS38KPFD
type: warning
status: verified
title: 'Khoá liên tiến trình: dùng flock/LockFileEx, đừng chép kiểu thu hồi theo PID chết của evolve.AcquireLock'
scope: {}
evidence:
  - path: internal/workflow/isolation.go
    symbol: AcquireWriteLock
    commit: 755b544a90e8
    fingerprint: symbol:54fa95cb1762baa4
  - path: internal/workflow/flock_unix.go
    lines: 11-24
    fingerprint: lines:d6c787e8b926561a
  - path: internal/evolve/apply.go
    lines: 30-58
    fingerprint: lines:567603d4225876de
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - internal/workflow/flock_unix.go
    - internal/evolve/apply.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:28:00Z"
confidence: high
version: 1
created_at: "2026-11-11T02:24:04Z"
updated_at: "2026-11-11T02:28:00Z"
verified_at: "2026-11-11T02:28:00Z"
status_reason: người duyệt chấp nhận
---

Commit 755b544 (R1) sửa race của khoá ghi workflow: đọc khoá thấy PID chết rồi xoá/rename có thể gỡ nhầm khoá của chủ mới đang sống. Bản sửa dùng `tryFlock` (build tag unix/windows) để hệ điều hành tự nhả khoá khi tiến trình chết.
`evolve.AcquireLock` (khoá `evolve.lock`) vẫn dùng mẫu cũ O_EXCL + `task.ProcessAlive` + `os.Remove`, nên cùng loại race. Khoá mới hãy theo `workflow.AcquireWriteLock`, không chép `evolve.AcquireLock`.
