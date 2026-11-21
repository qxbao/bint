---
schema: 1
id: 01M7HPJYDMS59SCS3GM5N8NSEX
type: warning
status: verified
title: 'iso sync: chỉ chạy Codex (hooks/list) khi ghi; test live phải khôi phục PATH thật sau testutil.Isolate. App-server Code…'
scope:
  paths:
    - internal/app/iso_sync*
    - internal/iso/sync*
evidence:
  - path: internal/app/iso_sync.go
    lines: "225"
    fingerprint: lines:dab44bb0de73c8ca
  - path: internal/app/iso_sync_test.go
    lines: "347"
    fingerprint: lines:9d17eb6870b9919f
origin:
  kind: agent
  session: 24c008ee-61b9-4f8d-b595-289c26d1e1cf
  commit: 339f13400f3d3150607c79924ccc8ab952eb71aa
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-21T03:52:16Z"
version: 1
created_at: "2026-11-16T08:11:42Z"
updated_at: "2026-11-16T11:08:58Z"
verified_at: "2026-11-21T03:52:16Z"
status_reason: người duyệt chấp nhận
---

iso sync: chỉ chạy Codex (hooks/list) khi ghi; test live phải khôi phục PATH thật sau testutil.Isolate. App-server Codex tạo tệp sqlite trong CODEX_HOME nên --check và kiểm tĩnh của run không được spawn nó (phải không ghi gì). testutil.Isolate đặt codex giả đứng đầu PATH: test dùng Codex thật phải lưu PATH trước Isolate rồi t.Setenv lại, nếu không ListHooks gặp stub và báo EOF.
