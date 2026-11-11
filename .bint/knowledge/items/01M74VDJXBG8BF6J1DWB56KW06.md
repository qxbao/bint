---
schema: 1
id: 01M74VDJXBG8BF6J1DWB56KW06
type: warning
status: verified
title: Không có dấu vết để xác nhận ngoài thực tế rằng việc quên bộ nhớ đã xảy ra
scope:
  paths:
    - internal/harness/hookcore/hook.go
  tags:
    - review
evidence:
  - path: internal/harness/hookcore/hook.go
    lines: 419-424
    fingerprint: lines:5bd75daa80a7086a
origin:
  kind: human
  commit: 0d0ba92ff956509301949a0b1939431833781032
  inputs:
    - internal/harness/hookcore/hook.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T08:26:02Z"
version: 1
created_at: "2026-11-11T08:26:02Z"
verified_at: "2026-11-11T08:26:02Z"
---

Ghi thêm ev.Payload[memory_reset]=true/false (hoặc lỗi cắt ngắn) để kiểm tra bằng spool sau một phiên dài thật.

Căn cứ: Lỗi của Reset bị bỏ qua (_ =) và sự kiện session.started chỉ ghi payload source; không có trường nào cho biết bộ nhớ đã được xoá hay xoá lỗi. Chưa có bằng chứng tích hợp rằng Claude Code gửi source=compact sau khi nén (chỉ kiểm bằng JSON giả lập), nên nếu sự kiện không đến thì bản sửa lặng lẽ vô tác dụng và không thể phát hiện.

(Rút ra từ phát hiện review 01M74V1M7F41TFP03TWJQKKYF1.3, tác vụ 01M74V1M7F41TFP03TWJQKKYF1.)
