---
schema: 1
id: 01M84GM05P2DYC6WRBMM7PNAC8
type: warning
status: verified
title: 'Thêm skill gốc: tên mới thành tên cấm cho flow/skill tuỳ biến; tệp phụ tự xuất bản (trừ tests/)'
scope:
  paths:
    - internal/skills/core/**
evidence:
  - path: internal/skills/flow_install_test.go
    lines: "14"
    fingerprint: lines:55add69571472569
  - path: internal/skills/core_files.go
    lines: "13"
    fingerprint: lines:2aca24f8511571c1
origin:
  kind: agent
  session: 6e81169f-9de2-4084-b52f-e220f93603ac
  commit: 610acef3e0e43d7aa4e119bada93e8523d537ba7
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-23T16:25:37Z"
version: 1
created_at: "2026-11-23T15:33:00Z"
updated_at: "2026-11-23T16:25:37Z"
verified_at: "2026-11-23T16:25:37Z"
status_reason: người duyệt chấp nhận
---

Mỗi thư mục trong internal/skills/core/ là một skill gốc: tên của nó thành tên cấm cho flow và skill tuỳ biến (flow.Validate báo trùng tên), nên ví dụ flow/skill trong test và tài liệu trùng tên mới sẽ đỏ hàng loạt (flow ví dụ 'tdd' đã phải đổi thành 'testfirst' khi nhập skill tdd). Mọi tệp trong thư mục skill gốc trừ SKILL.md và tests/ được nhúng và xuất bản cạnh SKILL.md (băm từng tệp trong core.lock); kịch bản áp lực đặt ở tests/*.yml. Sau khi thêm/sửa skill gốc chạy go test ./internal/skills/ ./internal/flow/ ./internal/workflow/ và bint sync.
