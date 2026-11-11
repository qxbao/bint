---
schema: 1
id: 01M74D57FYH0PS3DKV7BHRTCF8
type: warning
status: verified
title: Mục không có bằng chứng bị EconomyFilter loại; bài học từ thất bại được nới
scope:
  paths:
    - internal/eval/**
evidence:
  - path: internal/eval/knowledge.go
    lines: 196-201
    fingerprint: lines:3fa535a0c6c03af9
origin:
  kind: agent
  session: f6df3521-2b70-4d5c-8b4b-923b360d080d
  commit: 7dd832f666fa77c584f5354e5d1ec02638134543
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T06:36:51Z"
version: 1
created_at: "2026-11-11T04:16:48Z"
updated_at: "2026-11-11T06:36:51Z"
verified_at: "2026-11-11T06:36:51Z"
status_reason: người duyệt chấp nhận
---

EconomyFilter và EvidenceHoldsAt loại mục không có tệp bằng chứng. Bài học failure_lesson (Origin.Provenance=failure_lesson, Origin.Runs khác rỗng) không có tệp nên được nới qua Item.LessonFromRuns(); mọi mục khác vẫn bị loại.
