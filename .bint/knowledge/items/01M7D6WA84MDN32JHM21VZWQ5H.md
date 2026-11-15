---
schema: 1
id: 01M7D6WA84MDN32JHM21VZWQ5H
type: warning
status: deprecated
title: go test ./... (có test cache, không -count=1) trên Windows treo hàng chục phút, go.exe không tiến trình con, không in g…
scope:
  paths:
    - internal/**/*_test.go
evidence:
  - path: internal/gitrepo/gitrepo.go
    lines: "90"
    fingerprint: lines:adc044d81427d045
origin:
  kind: agent
  session: dc78c41f-9fdf-4a4e-aa3c-fa30827e0ff6
  commit: 0f285b752a6c5b6511ad70f55c99e91f1e62b881
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-14T16:59:21Z"
version: 1
created_at: "2026-11-14T14:20:14Z"
updated_at: "2026-11-15T14:39:19Z"
verified_at: "2026-11-14T16:59:21Z"
status_reason: Đã lỗi thời
---

go test ./... (có test cache, không -count=1) trên Windows treo hàng chục phút, go.exe không tiến trình con, không in gì: trước khi chạy mỗi gói, go.exe đọc testlog của lần trước và băm từng dòng stat/open; đường dẫn ngoài module bị filepath.EvalSymlinks hai lần (search.InDir). Mỗi exec.Command('git') gọi LookPath, sinh ~số thư mục PATH x số đuôi PATHEXT dòng stat, nên testlog của internal/tui/app ~477 nghìn dòng (~7000 lần LookPath git), internal/app ~357 nghìn; 56 nghìn dòng đã tốn ~17s mỗi lượt băm. Đo bằng -count=1 sẽ không tái hiện vì cờ này tắt test cache và testlog. Lệnh kiểm chứng nên dùng go test -count=1 ./..., hoặc giảm số lần LookPath git trong test.
