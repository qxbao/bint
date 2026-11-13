---
schema: 1
id: 01M78XP5NXWWRAZQHN5PSHNC13
type: warning
status: verified
title: 'Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (in…'
scope:
  paths:
    - internal/app/**
    - internal/tui/actions/**
evidence:
  - path: internal/cli/cliref_inventory_test.go
    lines: "77"
    fingerprint: lines:f3a3838944fd478e
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

Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (internal/tui/actions/cliref.go) để Rewriter đổi lời nhắc 'bint <lệnh>' thành phím TUI, (2) cờ rời trong literal lỗi cách dùng phải vào litsrUsage của cliref_inventory_test.go. Quên cả hai thì test đỏ ở cli, không ở actions.
