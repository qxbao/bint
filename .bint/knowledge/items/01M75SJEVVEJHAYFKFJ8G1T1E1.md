---
schema: 1
id: 01M75SJEVVEJHAYFKFJ8G1T1E1
type: warning
status: verified
title: Xử lý ResultMsg của màn hình không được tự phát yêu cầu kế tiếp khi Value rỗng
scope:
  paths:
    - internal/tui/screens/**
evidence:
  - path: internal/tui/screens/system/model.go
    lines: "541"
    fingerprint: lines:74be6a639f6e2c6f
  - path: internal/tui/app/knowledge_test.go
    lines: "183"
    fingerprint: lines:af5d8bd75ddea8cb
origin:
  kind: agent
  session: 6cbf352c-523c-4831-a7ef-92e1ad1b1d41
  commit: 191ac2b850cec907b823ff67a6f35a8b6ecf7226
author: Duong Quan Bao <dquanbao@gmail.com>
version: 1
created_at: "2026-11-11T17:12:59Z"
updated_at: "2026-11-11T17:47:13Z"
verified_at: "2026-11-11T17:47:13Z"
status_reason: qua kiểm tra xác định được
---

TestReadOnlyAndAgentNeverReachDo thay Do của mọi hành động bằng hàm giả trả ResultMsg không có Value rồi đếm số lần Do được gọi theo từng ID. Nếu onResult của một màn hình, khi nhận Value rỗng, lại gửi RequestMsg sang hành động khác (ví dụ skill_overlay → open_file), bộ đếm của hành động kia cộng dồn và test báo sai 'Do được gọi 2 lần, muốn 0'. Chỉ chuỗi hoá yêu cầu khi Value có dữ liệu thật (ví dụ OverlayResult.Abs khác rỗng).
