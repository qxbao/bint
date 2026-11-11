---
schema: 1
id: 01M74FMJH0K4HXQ6HCB941HJRY
type: warning
status: verified
title: EvalCases của bài học lấy từ mọi tác vụ trong prompt, không chỉ bằng chứng
scope:
  paths:
    - internal/evolve/**
evidence:
  - path: internal/evolve/reflector.go
    lines: 151-154
    fingerprint: lines:eae762892ec0c4ad
origin:
  kind: agent
  session: 1088a544-42a1-4549-bd88-4e3c79d6f17e
  commit: de4c70fdeccf2b0d78d08b6d7c8ac472bac71a9b
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T06:36:51Z"
version: 1
created_at: "2026-11-11T05:00:08Z"
updated_at: "2026-11-11T06:36:51Z"
verified_at: "2026-11-11T06:36:51Z"
status_reason: người duyệt chấp nhận
---

Tập tác vụ nguồn (EvalCases) của bài học phải là mọi tác vụ có bản tóm tắt trong prompt của reflector (biến shown từ caseIDsOf(fails)), không lấy từ các lượt reflector nêu làm evidence. Chỉ lấy theo evidence thì tác vụ reflector đã thấy nhưng không nêu sẽ bị chấm lại bằng chính bài học đó, vi phạm mục 12.3 và E3.5 của kế hoạch P8.
