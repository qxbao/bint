---
schema: 1
id: 01M7JG5ZA2ZMVS3VSHCYP2VZBW
type: warning
status: candidate
title: Danh sách readonly cấu hình dự án ghi đè Default
scope:
  paths:
    - internal/harness/hookcore/**
    - internal/config/**
evidence:
  - path: internal/config/upgrade.go
    symbol: upgradeReadonlySequence
    fingerprint: file:4212fc91722ce779
origin:
  kind: agent
  commit: 339f13400f3d3150607c79924ccc8ab952eb71aa
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-16T15:39:00Z"
---

workflow.agent.readonly_tools là một danh sách ghi đè, không tự hợp nhất với Default khi Load. Thêm rule mặc định không nâng được cấu hình dự án đã có. Upgrade chỉ nhận đúng mẫu lịch sử đã ghim, có preview/backup; danh sách tuỳ biến và lớp local được giữ nguyên. Hồi quy cần kiểm cả Default, Render, Upgrade rồi config.Load và hook thực, đồng thời giữ các ca chặn toán tử shell thật.
