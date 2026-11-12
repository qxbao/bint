---
schema: 1
id: 01M768KJEYSVTCQEJ3JAX3E7S2
type: warning
status: verified
title: 'go test ./... cần ≥ ~1 GB trống ở đĩa chứa TMPDIR: test eval/cli dựng kho đệm commit gốc và dừng với ''không đủ dung lượ…'
scope:
  paths:
    - internal/eval/**
    - internal/cli/**
evidence:
  - path: internal/eval/basecache.go
    lines: "187"
    fingerprint: lines:b2187cad025f66c0
origin:
  kind: agent
  session: f8f803a0-c2b2-494a-b7fd-4b87dab90987
  commit: 38c13c96e378836a570ab91677974d35accc1f41
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-12T02:17:18Z"
version: 1
created_at: "2026-11-11T21:35:44Z"
updated_at: "2026-11-12T02:17:18Z"
verified_at: "2026-11-12T02:17:18Z"
status_reason: người duyệt chấp nhận
---

go test ./... cần ≥ ~1 GB trống ở đĩa chứa TMPDIR: test eval/cli dựng kho đệm commit gốc và dừng với 'không đủ dung lượng đĩa' khi còn dưới ngưỡng eval.min_free_mb (mặc định 1024). Đĩa đầy thì dọn bộ đệm build (go clean -cache) trước khi chạy; lỗi này không liên quan mã đang sửa.
