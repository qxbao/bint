---
schema: 1
id: 01M77M51CTEECC6J7JZBKVTTB1
type: warning
status: candidate
title: 'Dữ liệu hook cần cho workflow (đường dẫn transcript của subagent) không được chỉ dựa vào spool: bất kỳ lệnh bint nào cũ…'
scope:
  paths:
    - internal/workflow/session_cost.go
    - internal/harness/hookcore/**
evidence:
  - path: internal/harness/hookcore/subagent.go
    lines: "46"
    fingerprint: lines:cb4434024597a74b
origin:
  kind: agent
  session: 040e79b8-f534-4b1f-b8e3-758b6766a862
  commit: 4fc68688ca7c5f84c41139b1d15b44096a271eca
author: Duong Quan Bao <dquanbao@gmail.com>
version: 1
created_at: "2026-11-12T10:16:45Z"
---

Dữ liệu hook cần cho workflow (đường dẫn transcript của subagent) không được chỉ dựa vào spool: bất kỳ lệnh bint nào cũng nạp và xoá spool trước khi complete chạy. Hook ghi thêm tệp runtime/subagents/<agent_id>.json; workflow đọc tệp đó rồi mới quét spool chờ.
