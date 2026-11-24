---
schema: 1
id: 01M7VX8CD0CDJT1TTFD9QRFETB
type: warning
status: verified
title: 'go test (không -count=1) trên Windows có thể đứng hơn 20 phút với go.exe ăn một nhân CPU, không tiến trình con: go test…'
scope:
  paths:
    - internal/testutil/**
evidence:
  - path: internal/testutil/shard.go
    lines: "286"
    fingerprint: lines:69fcfb1efb8c6da1
origin:
  kind: agent
  session: d7a27d4c-ad07-47d5-8ea8-3a652ba4e66f
  commit: 39a9b702c4f995ee2551b64b968acbcccb8e0e80
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-24T17:04:46Z"
version: 1
created_at: "2026-11-20T07:20:41Z"
updated_at: "2026-11-20T10:58:53Z"
verified_at: "2026-11-24T17:04:46Z"
status_reason: người duyệt chấp nhận
---

go test (không -count=1) trên Windows có thể đứng hơn 20 phút với go.exe ăn một nhân CPU, không tiến trình con: go test duyệt nhật ký đầu vào test (testlog) khi lưu và tra cache, mỗi dòng open/stat ngoài module tốn hai lần EvalSymlinks. testutil.runShards gộp nhật ký của các mẻ qua mergeTestlogs, phải giữ rút gọn (bỏ đường dẫn trong thư mục tạm, bỏ dòng trùng): bản cũ ghi ~1,1 triệu dòng (106 MB) cho gói workflow, bản rút gọn ~500 dòng. Test: TestCompactTestlogDropsTempAndDuplicates.
