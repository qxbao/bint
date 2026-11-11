---
schema: 1
id: 01M746PJF8DCXQRH53DYRVN08X
type: fact
status: verified
title: '"mục N.M" trong chú thích và thông báo là mục của docs/bint-plan.md'
scope: {}
evidence:
  - path: internal/harness/hookcore/hook.go
    lines: "9"
    fingerprint: lines:fa182ee1b85bd6e7
  - path: internal/app/knowledge.go
    lines: 27-30
    fingerprint: lines:28670d388976ccc4
  - path: docs/bint-plan.md
    lines: "1295"
    fingerprint: lines:f7fdd4b4c2b07a9d
  - path: docs/bint-plan.md
    lines: "1448"
    fingerprint: lines:65f3236baa43a167
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - docs/bint-plan.md
    - internal/harness/hookcore/hook.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:27:57Z"
confidence: high
version: 1
created_at: "2026-11-11T02:23:56Z"
updated_at: "2026-11-11T02:27:57Z"
verified_at: "2026-11-11T02:27:57Z"
status_reason: người duyệt chấp nhận
---

Mã và thông báo lỗi tham chiếu đặc tả bằng "mục 13.2", "mục 15.5", "mục 12.3"... — đó là số đề mục trong `docs/bint-plan.md` (bản kế hoạch thiết kế), không phải tài liệu ngoài.
Trước khi đổi một hành vi có chú thích "(mục N.M)", đọc mục đó: nó nêu ràng buộc (chống rò rỉ đáp án, ranh giới tin cậy, nhãn kết quả...) mà kiểm thử không phải lúc nào cũng bắt.
