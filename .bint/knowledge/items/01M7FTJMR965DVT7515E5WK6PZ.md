---
schema: 1
id: 01M7FTJMR965DVT7515E5WK6PZ
type: warning
status: candidate
title: Bỏ qua WAL rỗng khi tính chữ ký polling SQLite
scope:
  paths:
    - internal/app/data_watch*
evidence:
  - path: internal/app/data_watch.go
    lines: 80-88
    fingerprint: lines:cd89c5cc6287bb09
origin:
  kind: agent
  commit: 474c38739bccf48a647ce7be4e039e66b4dbfdb2
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
version: 1
created_at: "2026-11-15T14:42:58Z"
---

SQLite có thể tạo WAL rỗng khi chỉ mở DB để đọc. DataWatch phải coi WAL rỗng như chưa tồn tại để tránh vòng polling tự kích hoạt reload; vẫn theo dõi WAL có dữ liệu và thay đổi của DB. Hồi quy: TestDataWatchSignatureStableWhileReadingStore.
