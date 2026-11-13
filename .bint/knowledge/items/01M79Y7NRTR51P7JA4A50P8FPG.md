---
schema: 1
id: 01M79Y7NRTR51P7JA4A50P8FPG
type: warning
status: verified
title: 'yaml.v3 đặt lại map inline mỗi lần giải mã lại khối chứa nó: lớp cấu hình sau làm mất role tuỳ biến của lớp trước'
scope:
  paths:
    - internal/config/**
evidence:
  - path: internal/config/config.go
    symbol: mergeFile
    fingerprint: symbol:95ae30d7b7d63fda
  - path: internal/config/extra_roles_test.go
    symbol: TestMergeFileKeepsExtraRoles
    fingerprint: symbol:c0692c2795ddbc9a
origin:
  kind: agent
  session: 025c6f27-a407-48ce-a2cf-004bac84f254
  commit: 5bb23cd697032f584d26160c942938b8f37fc705
  inputs:
    - internal/config/config.go#mergeFile
    - internal/config/extra_roles_test.go#TestMergeFileKeepsExtraRoles
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-13T10:14:40Z"
version: 1
created_at: "2026-11-13T07:51:26Z"
updated_at: "2026-11-13T10:14:40Z"
verified_at: "2026-11-13T10:14:40Z"
status_reason: người duyệt chấp nhận
---

Roles.Extra và RoleModels.Extra dùng thẻ yaml:",inline". yaml.v3 đặt map inline về nil mỗi khi giải mã lại khối `roles:`/`models:`, nên Decode lớp sau (local.yml) xoá role tuỳ biến đã nạp từ lớp trước. mergeFile phải chụp (snapshotExtras) rồi phục hồi (restore) quanh Decode. Thêm map inline mới vào Config thì làm tương tự; test: TestMergeFileKeepsExtraRoles.
