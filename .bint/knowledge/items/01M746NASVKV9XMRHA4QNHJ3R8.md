---
schema: 1
id: 01M746NASVKV9XMRHA4QNHJ3R8
type: rule
status: verified
title: Mã phải qua go vet và staticcheck với GOOS=windows; mã theo nền tảng tách cặp *_unix.go / *_windows.go
scope: {}
evidence:
  - path: .github/workflows/ci.yml
    lines: 55-64
    fingerprint: lines:db524b76df5652e1
  - path: internal/workflow/flock_unix.go
    lines: "1"
    fingerprint: lines:e7e0f97e7ab28e36
  - path: internal/workflow/flock_windows.go
    lines: "1"
    fingerprint: lines:c8cbc89b0cec1123
  - path: docs/ci-cd.md
    lines: 96-98
    fingerprint: lines:221bf96ec24295c7
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - .github/workflows/ci.yml
    - docs/ci-cd.md
    - internal/workflow/flock_unix.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:27:25Z"
confidence: high
version: 1
created_at: "2026-11-11T02:23:16Z"
updated_at: "2026-11-11T02:27:25Z"
verified_at: "2026-11-11T02:27:25Z"
status_reason: người duyệt chấp nhận
---

CI chạy `go vet` và `staticcheck` cho cả linux lẫn `GOOS=windows`; `go test ./...` trên Linux không bắt được lỗi biên dịch Windows.
Lệnh gọi `syscall` riêng của Unix (Flock, Kill, Statfs...) phải nằm trong tệp `*_unix.go` có `//go:build !windows`, kèm bản `*_windows.go` có `//go:build windows` cùng chữ ký.
Kiểm tra cục bộ: `GOOS=windows go vet ./...`; staticcheck cho windows phải cài binary rồi đặt GOOS (không dùng `go run ...@v0.7.0` với GOOS khác).
