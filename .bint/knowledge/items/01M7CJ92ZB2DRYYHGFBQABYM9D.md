---
schema: 1
id: 01M7CJ92ZB2DRYYHGFBQABYM9D
type: warning
status: verified
title: 'go test ./... trên Windows chậm chủ yếu do tiến trình git và Defender: đo trước khi tối ưu, đừng cache gitrepo.Discover'
scope:
  paths:
    - internal/**/*_test.go
    - internal/testutil/**
evidence:
  - path: internal/testutil/git.go
    lines: "34"
    fingerprint: lines:33a589ca175680d8
origin:
  kind: agent
  session: ea73f6ef-baf8-457c-a530-7b7efac53c94
  commit: 66bfe223b81eec049cb3cba6afeeeea79e51ba0b
author: orca <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-16T07:19:10Z"
version: 1
created_at: "2026-11-14T08:20:13Z"
updated_at: "2026-11-14T12:14:52Z"
verified_at: "2026-11-16T07:19:10Z"
status_reason: người duyệt chấp nhận
---

go test ./... trên Windows chậm chủ yếu do tiến trình git: mỗi lần spawn git tốn hàng chục ms, test không dùng t.Parallel (Isolate dùng t.Setenv; song song hoá bằng chia tiến trình con, xem 01M7TEJGBPS4W8FSHG6B1B8Y34) và repo tạm nằm trong %TEMP% bị Defender quét thời gian thực. Gói workflow và tui/app là đường găng. Đo trước khi tối ưu: go test -json rồi tính thời gian từng test; profile bằng -test.cpuprofile, thời gian chờ git hiện thành runtime.cgocall/ReadFile. Đừng cache gitrepo.Discover chỉ để test nhanh (đổi hành vi sau khi xoá worktree).
