---
schema: 1
id: 01M7FVYABYHEYW273WG8XAH0G6
type: warning
status: verified
title: Chốt callback trước khi tạo goroutine có thể sống qua timeout
scope:
  paths:
    - internal/workflow/codegraph*
evidence:
  - path: internal/workflow/codegraph.go
    lines: 138-145
    fingerprint: lines:97442a7c977cf474
origin:
  kind: agent
  commit: 741abf134742cc5bb0523d9607e9dae5e69d1ffc
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_by: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_at: "2026-11-15T16:07:15Z"
version: 1
created_at: "2026-11-15T15:06:49Z"
updated_at: "2026-11-15T16:07:15Z"
verified_at: "2026-11-15T16:07:15Z"
status_reason: 'Duyệt theo yêu cầu trực tiếp của người dùng trong phiên PR #8; đã đối chiếu nội dung với mã hiện tại, cập nhật bằng chứng resolver và vị trí bằng chứng dịch dòng.'
---

Nhả gateway có timeout có thể trả về trước khi goroutine kết thúc. Nếu goroutine đọc biến callback toàn cục thì test khôi phục callback ở Cleanup gây data race. Chốt giá trị callback trong goroutine gọi trước khi tạo goroutine nhả; giữ kiểm thử TestCleanupWorktreeReleasesGatewayBounded dưới race detector.
