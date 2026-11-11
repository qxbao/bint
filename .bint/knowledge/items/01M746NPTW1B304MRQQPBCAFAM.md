---
schema: 1
id: 01M746NPTW1B304MRQQPBCAFAM
type: rule
status: verified
title: 'Hook không bao giờ chặn agent: không trả lỗi, không mở SQLite, không gọi mô hình, không tự tổng hợp sự kiện'
scope:
  paths:
    - internal/harness/**
    - internal/app/hook.go
evidence:
  - path: internal/harness/hookcore/hook.go
    lines: 1-9
    fingerprint: lines:c70a128d762c5cd2
  - path: internal/app/hook.go
    lines: 19-31
    fingerprint: lines:a89fd436e83e0814
  - path: internal/cli/commands.go
    lines: 267-270
    fingerprint: lines:bca66967c0d90458
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - internal/harness/hookcore/hook.go
    - internal/app/hook.go
    - internal/cli/commands.go
    - internal/harness/claude/hook.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:27:38Z"
confidence: high
version: 1
created_at: "2026-11-11T02:23:28Z"
updated_at: "2026-11-11T02:27:38Z"
verified_at: "2026-11-11T02:27:38Z"
status_reason: người duyệt chấp nhận
---

Hook chạy ở mỗi lần agent dùng công cụ (giới hạn 5 giây). Lỗi của hook được ghi vào `hook-errors.log`, lệnh `bint hook ...` luôn trả `nil`; đừng thêm đường trả lỗi/mã thoát khác 0.
Hook chỉ ghi vào spool, không mở SQLite và không gọi mô hình (việc nạp spool vào CSDL làm ở nơi khác).
Sự kiện mà harness không cung cấp thì không được giả lập (docs/bint-plan.md mục 13.2). Khác biệt giữa harness đặt trong `hookcore.Dialect`, không rẽ nhánh theo tên harness trong hookcore.
