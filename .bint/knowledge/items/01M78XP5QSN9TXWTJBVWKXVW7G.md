---
schema: 1
id: 01M78XP5QSN9TXWTJBVWKXVW7G
type: warning
status: verified
title: 'Thanh tab 80 cột ở locale giả (xx) không đủ chỗ cho tám tab khi tab đang chọn mang nhãn đầy đủ: tabs.Render thu hẹp dấu…'
scope:
  paths:
    - internal/tui/components/tabs/**
evidence:
  - path: internal/tui/components/tabs/tabs.go
    lines: "37"
    fingerprint: lines:1299fc914ca77ad4
origin:
  kind: agent
  session: d78237c6-bd8b-4c03-b358-5aa96047ec1c
  commit: 3debc5691742df84ebd914af0bf83b2a390be51e
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-13T03:46:35Z"
version: 1
created_at: "2026-11-12T22:22:38Z"
updated_at: "2026-11-13T03:46:35Z"
verified_at: "2026-11-13T03:46:35Z"
status_reason: người duyệt chấp nhận
---

Thanh tab 80 cột ở locale giả (xx) không đủ chỗ cho tám tab khi tab đang chọn mang nhãn đầy đủ: tabs.Render thu hẹp dấu ngăn (seps) trước khi cắt. Thêm tab thứ chín hoặc nhãn dài hơn thì TestMCPTabBarShowsAllEightLabelsAtEveryTab (tui/app) đỏ; rút nhãn tab.<id>.short trước khi sửa tabs.Render.
