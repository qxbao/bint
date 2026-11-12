---
schema: 1
id: 01M779C1VVVJMQH2Q2T9XBCTPQ
type: warning
status: candidate
title: 'Helper thu thông điệp job phải giữ cả DataMsg sau khi done đóng: select có thể chọn done khi channel còn kết quả. Nhánh…'
scope:
  paths:
    - internal/tui/actions/*_test.go
evidence:
  - path: internal/tui/actions/eval_test.go
    lines: "209"
    fingerprint: lines:0b3127fe692af6a4
origin:
  kind: agent
  commit: e9ce282ba1671a476b195eb9b5d2a490fb77acac
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-12T07:08:21Z"
---

Helper thu thông điệp job phải giữ cả DataMsg sau khi done đóng: select có thể chọn done khi channel còn kết quả. Nhánh dọn cuối chỉ giữ LogMsg làm mất EvalAttempt, khiến TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch thất bại ngẫu nhiên dù production đã gửi đủ hai kết quả. Dùng cùng hàm xử lý thông điệp cho vòng chính và phần còn lại.
