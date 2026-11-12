---
schema: 1
id: 01M77BS1E11BFC8C6JSS20Z5VF
type: warning
status: candidate
title: 'Codex 0.167.2: features.multi_agent=false và features.code_mode=false không đủ tắt công cụ vì metadata model tool_mode/…'
scope:
  paths:
    - internal/eval/codex*
evidence:
  - path: internal/eval/codex_restricted.go
    lines: "22"
    fingerprint: lines:eb5ca9795395685d
origin:
  kind: agent
  commit: e9ce282ba1671a476b195eb9b5d2a490fb77acac
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-12T07:50:23Z"
---

Codex 0.167.2: features.multi_agent=false và features.code_mode=false không đủ tắt công cụ vì metadata model tool_mode/multi_agent_version vẫn bật Code Mode/collaboration; apply_patch_tool_type còn mở apply_patch. Adapter evolution cần catalog tạm bỏ các metadata này, environments=[], profile cô lập và probe request thật kiểm cả tools lẫn input.additional_tools trước khi đưa dữ liệu eval. Không suy no-tools từ thread/start hoặc riêng tools cấp cao nhất rỗng.
