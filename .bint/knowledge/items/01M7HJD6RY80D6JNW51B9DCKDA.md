---
schema: 1
id: 01M7HJD6RY80D6JNW51B9DCKDA
type: warning
status: verified
title: 'GC iso phải tìm temp theo env của phiên, không theo os.Environ(): CollectTemp/GC nhận Env (TempOptions.Env, GCOptions.E…'
scope:
  paths:
    - internal/iso/**
evidence:
  - path: internal/iso/temp_gc.go
    lines: "45"
    fingerprint: lines:07d7ac83c23ca27b
origin:
  kind: agent
  session: 8d907998-07c5-4fae-9af2-034593f8fcd9
  commit: c686ebbb050b87e2252cb2f002b211f6f24181bf
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-16T07:15:22Z"
version: 1
created_at: "2026-11-16T06:58:40Z"
updated_at: "2026-11-16T07:15:22Z"
verified_at: "2026-11-16T07:15:22Z"
status_reason: người duyệt chấp nhận
---

GC iso phải tìm temp theo env của phiên, không theo os.Environ(): CollectTemp/GC nhận Env (TempOptions.Env, GCOptions.Env); autoGC trong iso.Run truyền env của spec. Dùng os.Environ() thì test hay phiên có spec.Env khác sẽ quét /tmp/bint-iso-<uid> thật và xoá thư mục temp mồ côi (cũ hơn tmp_ttl) của profile thật. Mọi đường xoá của GC đều phải qua removeTree/removeEmptyDir (không đi qua liên kết chia sẻ) và nằm trong thư mục profile.
