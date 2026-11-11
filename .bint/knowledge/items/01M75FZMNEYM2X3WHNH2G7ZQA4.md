---
schema: 1
id: 01M75FZMNEYM2X3WHNH2G7ZQA4
type: warning
status: verified
title: 'Test TUI có việc nền: không dùng drive()/press() — jobs.Wait chặn đồng bộ'
scope:
  paths:
    - internal/tui/app/*_test.go
evidence:
  - path: internal/tui/app/evolve_screen_test.go
    lines: "27"
    fingerprint: lines:174c44b211acaeb7
origin:
  kind: agent
  session: 9fa1f16f-c9a0-4a50-93f2-af6ed375dab2
  commit: 46e924d8fcbea7350875973ea8d8b1aa71494e87
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T16:25:21Z"
version: 1
created_at: "2026-11-11T14:25:25Z"
updated_at: "2026-11-11T16:25:21Z"
verified_at: "2026-11-11T16:25:21Z"
status_reason: người duyệt chấp nhận
---

drive() trong testutil_test.go chạy tea.Cmd đồng bộ; khi có việc nền (Action.Job) lệnh jobs.Wait chặn tới khi việc gửi thông điệp nên test treo. Dùng fixture.press/pump (tasks_fixture_test.go) hoặc drv (evolve_screen_test.go) chạy lệnh trong goroutine. Test TestReadOnlyAndAgentNeverReachDo chỉ duyệt Action.Do: hành động Job (evolve.propose/evaluate/process/cycle) cần test chỉ-đọc/HumanOnly riêng.
