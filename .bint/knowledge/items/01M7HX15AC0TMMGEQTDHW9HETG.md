---
schema: 1
id: 01M7HX15AC0TMMGEQTDHW9HETG
type: warning
status: verified
title: 'Nhập profile ai-isolator phải theo danh sách cho phép và chỉ tệp thường, không đi theo symlink: home/.codex/hooks.json …'
scope:
  paths:
    - internal/iso/import*
    - internal/app/iso_import*
evidence:
  - path: internal/iso/import.go
    lines: "52"
    fingerprint: lines:453c5d91c4a132db
  - path: internal/iso/import.go
    lines: "422"
    fingerprint: lines:950861a8e787270a
origin:
  kind: agent
  session: a06f0a5b-d4b9-447f-9a33-384b86f7537f
  commit: 339f13400f3d3150607c79924ccc8ab952eb71aa
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-21T03:52:15Z"
version: 1
created_at: "2026-11-16T10:04:20Z"
updated_at: "2026-11-16T11:08:58Z"
verified_at: "2026-11-21T03:52:15Z"
status_reason: người duyệt chấp nhận
---

Nhập profile ai-isolator phải theo danh sách cho phép và chỉ tệp thường, không đi theo symlink: home/.codex/hooks.json của profile ai-isolator là symlink tới ~/.codex/hooks.json thật nên chép theo danh sách loại trừ (hoặc ghi xuyên symlink ở iso sync sau đó) sẽ sửa tệp thật của người dùng. iso.Import (importAllow + planImport dùng Lstat/WalkDir không theo liên kết) chỉ chép các tệp nêu tên; thêm tệp được nhập thì thêm một dòng vào importAllow kèm test nhóm trong import_test.go, đừng đổi sang chép cả thư mục.
