---
schema: 1
id: 01M7JG5ZA2ZMVS3VSHCYP2VZBW
type: warning
status: verified
title: Danh sách readonly cấu hình dự án ghi đè Default
scope:
  paths:
    - internal/harness/hookcore/**
    - internal/config/**
evidence:
  - path: internal/config/upgrade.go
    symbol: upgradeReadonlySequence
    fingerprint: file:52c3563b19246cf8
origin:
  kind: agent
  commit: 339f13400f3d3150607c79924ccc8ab952eb71aa
author: Leekiri1234 <hongducluong0603@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-21T03:52:36Z"
version: 1
created_at: "2026-11-16T15:39:00Z"
updated_at: "2026-11-17T01:51:45Z"
verified_at: "2026-11-21T03:52:36Z"
status_reason: người duyệt chấp nhận
---

workflow.agent.readonly_tools là một danh sách ghi đè, không tự hợp nhất với Default khi Load. Thêm rule mặc định không nâng được cấu hình dự án đã có. Upgrade chỉ nhận đúng mẫu lịch sử đã ghim, có preview/backup; danh sách tuỳ biến và lớp local được giữ nguyên. Hồi quy cần kiểm cả Default, Render, Upgrade rồi config.Load và hook thực, đồng thời giữ các ca chặn toán tử shell thật.
