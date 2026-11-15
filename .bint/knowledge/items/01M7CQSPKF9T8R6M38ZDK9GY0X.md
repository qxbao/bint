---
schema: 1
id: 01M7CQSPKF9T8R6M38ZDK9GY0X
type: warning
status: verified
title: 'Lỗi nạp cấu hình: guardrail chỉ đọc của subagent vẫn phải trả deny'
scope:
  paths:
    - internal/harness/**
    - internal/app/*hook*
evidence:
  - path: internal/harness/hookcore/hook.go
    lines: 1144-1157
    fingerprint: lines:f28483b58bd6871b
  - path: internal/app/subagent_hook_test.go
    symbol: TestReadonlyGuardrailFailsClosedOnBadConfig
    fingerprint: symbol:c9252c30290f385c
origin:
  kind: agent
  commit: 66bfe223b81eec049cb3cba6afeeeea79e51ba0b
  inputs:
    - internal/harness/hookcore/hook.go
    - internal/app/subagent_hook_test.go
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_by: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_at: "2026-11-15T16:07:15Z"
confidence: high
version: 1
created_at: "2026-11-14T09:56:40Z"
updated_at: "2026-11-15T16:07:15Z"
verified_at: "2026-11-15T16:07:15Z"
status_reason: 'Duyệt theo yêu cầu trực tiếp của người dùng trong phiên PR #8; đã đối chiếu nội dung với mã hiện tại, cập nhật bằng chứng resolver và vị trí bằng chứng dịch dòng.'
---

Khi hook lỗi Git hoặc giải mã cấu hình trước khi nạp được guardrail, vẫn trả quyết định từ readonlyFailClosed cho PreToolUse của dialect hỗ trợ SubagentReadonly.
Harness coi đầu ra rỗng là cho phép: reviewer/planner phải bị deny với công cụ ghi hoặc lệnh ngoài readonly_tools mặc định dù cấu hình hỏng.
Giữ các lệnh chỉ đọc hợp lệ và agent thường theo hành vi hiện tại; kiểm thử hồi quy: TestReadonlyGuardrailFailsClosedOnBadConfig.
Khi duyệt, đối chiếu lại tiêu đề tuyệt đối “Hook không bao giờ chặn agent” của mục 01M746NPTW1B304MRQQPBCAFAM vì guardrail có chủ đích trả deny.
