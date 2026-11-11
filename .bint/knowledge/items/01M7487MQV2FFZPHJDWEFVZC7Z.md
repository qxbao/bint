---
schema: 1
id: 01M7487MQV2FFZPHJDWEFVZC7Z
type: warning
status: verified
title: 'Render ghi mẫu bằng w(format) = fmt.Fprintf: mọi ký tự % trong chú thích của mẫu phải viết %% (bản cũ sinh ''%!k(MISSING…'
scope:
  paths:
    - internal/config/template.go
evidence:
  - path: internal/config/template.go
    lines: "145"
    fingerprint: lines:1cc300d3b75f00e2
origin:
  kind: agent
  session: d2190a51-0b39-4ef0-a263-efdcdb709cb3
  commit: a8ed16460d746b62255219aecc9f731d8c80fa92
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T07:55:08Z"
version: 1
created_at: "2026-11-11T02:50:44Z"
updated_at: "2026-11-11T07:55:08Z"
verified_at: "2026-11-11T07:55:08Z"
status_reason: người duyệt chấp nhận
---

Render ghi mẫu bằng w(format) = fmt.Fprintf: mọi ký tự % trong chú thích của mẫu phải viết %% (bản cũ sinh '%!k(MISSING)hông' vào config của người dùng; Upgrade có templateFixes để sửa). Thêm dòng chú thích có % thì kiểm bằng TestRenderMatchesDefaults không đủ vì chú thích không được so.
