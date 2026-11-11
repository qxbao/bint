---
schema: 1
id: 01M746PJW8B5A8S1N5B3BEAPC0
type: pattern
status: verified
title: Thao tác chỉ dành cho người (duyệt/áp dụng/loại bỏ) phải chặn bằng requireHuman / app.InAgentSession
scope:
  paths:
    - internal/app/**
    - internal/cli/**
    - internal/evolve/**
evidence:
  - path: internal/app/knowledge.go
    lines: 34-42
    fingerprint: lines:972acf6b6eae4a6b
  - path: internal/evolve/apply.go
    lines: 69-74
    fingerprint: lines:97715b34914ac70e
  - path: internal/app/revalidate.go
    lines: "80"
    fingerprint: lines:212c4ddabcb84e06
  - path: internal/knowledge/transition.go
    lines: 81-92
    fingerprint: lines:4cb593f2f937888e
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - internal/app/knowledge.go
    - internal/evolve/apply.go
    - internal/knowledge/transition.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:27:59Z"
confidence: high
version: 1
created_at: "2026-11-11T02:23:57Z"
updated_at: "2026-11-11T02:27:59Z"
verified_at: "2026-11-11T02:27:59Z"
status_reason: người duyệt chấp nhận
---

Lệnh làm thay đổi tri thức/đề xuất đã duyệt (approve, review, reject, deprecate, evolve apply/reject/rollback, knowledge revalidate, verify --apply) gọi `requireHuman("<lệnh>")` ở đầu, trả lỗi bọc `app.ErrAgentForbidden`.
Lệnh mới cùng loại phải làm tương tự (gói `evolve` có bản `requireHuman` riêng gọi `app.InAgentSession`). Đây là lớp phòng vệ bổ sung (docs/bint-plan.md mục 15.5), ranh giới chính vẫn là review Git; agent chỉ được tạo mục `candidate`.
