---
schema: 1
id: 01M7FWW8FZNTXHE5HNS2TW0XHA
type: warning
status: verified
title: So đường dẫn gateway theo alias vật lý, giữ nguyên định danh lưu trữ
scope:
  paths:
    - internal/mcpgw/route*
evidence:
  - path: internal/mcpgw/route.go
    lines: 18-34
    fingerprint: lines:4a68e6007bb1baf7
origin:
  kind: agent
  commit: 741abf134742cc5bb0523d9607e9dae5e69d1ffc
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_by: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_at: "2026-11-15T16:07:15Z"
version: 1
created_at: "2026-11-15T15:23:10Z"
updated_at: "2026-11-15T16:07:15Z"
verified_at: "2026-11-15T16:07:15Z"
status_reason: 'Duyệt theo yêu cầu trực tiếp của người dùng trong phiên PR #8; đã đối chiếu nội dung với mã hiện tại, cập nhật bằng chứng resolver và vị trí bằng chứng dịch dòng.'
---

Thư mục trạng thái có thể đi qua symlink, như /var và /private/var trên macOS. Kiểm tra bằng nhau hoặc chứa worktree phải giải alias trước khi so, không chỉ filepath.Clean; chỉ chuẩn hóa cho so sánh, không đổi repo ID hay khóa phân vùng. Hồi quy: TestRouteAndReleaseWorktreeThroughStateAlias.
