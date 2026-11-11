---
schema: 1
id: 01M74E2CYQEZ443CCRB0N2YS72
type: warning
status: verified
title: Mọi đường gọi evolve.Propose (kể cả ProcessRequests/deferred) phải truyền Cases và RepoDir. Thiếu chúng thì kiểm rò rỉ …
scope:
  paths:
    - internal/evolve/**
evidence:
  - path: internal/evolve/deferred.go
    lines: 234-238
    fingerprint: lines:3a48cf69718090de
  - path: internal/evolve/reflector.go
    lines: 155-160
    fingerprint: lines:a4aa5da6790f1879
origin:
  kind: agent
  session: 51ab62c7-230e-4064-a68c-7c9cbd72dcde
  commit: 7dd832f666fa77c584f5354e5d1ec02638134543
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T08:33:21Z"
version: 1
created_at: "2026-11-11T04:32:44Z"
updated_at: "2026-11-11T08:33:21Z"
verified_at: "2026-11-11T08:33:21Z"
status_reason: người duyệt chấp nhận
---

Mọi đường gọi evolve.Propose (kể cả ProcessRequests/deferred) phải truyền Cases và RepoDir. Thiếu chúng thì kiểm rò rỉ của bài học failure_lesson báo 'không tìm thấy tác vụ' sau khi reflector đã chạy: tốn tiền rồi bỏ hết. Kiểm rò rỉ được dựng trước khi gọi reflector để lỗi này không tốn tiền.
