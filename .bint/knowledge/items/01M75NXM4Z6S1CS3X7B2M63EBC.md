---
schema: 1
id: 01M75NXM4Z6S1CS3X7B2M63EBC
type: warning
status: verified
title: Khoá i18n gọi bằng m.t("…") (chữ thường) không được test quét khoá bắt
scope:
  paths:
    - internal/tui/screens/**
evidence:
  - path: internal/tui/i18n_scan_test.go
    lines: "117"
    fingerprint: lines:a32a3bb7121485eb
origin:
  kind: agent
  session: 835a46ec-0b91-4c46-ab95-bc1fbd584392
  commit: 1f352491d5f0a392b7cc027298e110d8eba153f2
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-12T02:15:43Z"
version: 1
created_at: "2026-11-11T16:09:11Z"
updated_at: "2026-11-12T02:15:43Z"
verified_at: "2026-11-12T02:15:43Z"
status_reason: người duyệt chấp nhận
---

TestEveryKeyUsedInCodeExists chỉ bắt T("…") và N("…") viết hoa; các màn hình gọi m.t("…") bằng chữ thường nên khoá thiếu ở vi.yaml/en.yaml KHÔNG làm test đỏ và chỉ lộ ra khi vẽ (chuỗi hiện thành khoá). Khi thêm khoá cho màn hình mới, quét thủ công các khoá gọi bằng m.t(...) rồi đối chiếu locales, hoặc mở rộng keyCallRE.
