---
schema: 1
id: 01M7CQYXC4PWC22186MG5FCTN8
type: warning
status: verified
title: 'Hậu kiểm cô lập: Bash is_error vẫn có thể đã chạy và ghi ngoài worktree'
scope:
  paths:
    - internal/workflow/session_isolation*
    - internal/harness/hookcore/worktree_audit*
evidence:
  - path: internal/workflow/session_isolation.go
    symbol: ranForReal
    fingerprint: symbol:2e5c269d850f1266
  - path: internal/workflow/session_isolation_test.go
    symbol: TestIsolationAuditShellViolations
    fingerprint: symbol:2caecb2582a1f7c3
  - path: internal/workflow/session_isolation_test.go
    symbol: TestIsolationAuditNotViolations
    fingerprint: symbol:8a23c42120aae8ed
origin:
  kind: agent
  commit: f8b219308258eee347462820524188ec57eb3f4c
  inputs:
    - internal/workflow/session_isolation.go
    - internal/workflow/session_isolation_test.go
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_by: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_at: "2026-11-15T16:07:15Z"
confidence: high
version: 1
created_at: "2026-11-14T09:59:31Z"
updated_at: "2026-11-15T16:07:15Z"
verified_at: "2026-11-15T16:07:15Z"
status_reason: 'Duyệt theo yêu cầu trực tiếp của người dùng trong phiên PR #8; đã đối chiếu nội dung với mã hiện tại, cập nhật bằng chứng resolver và vị trí bằng chứng dịch dòng.'
---

Khi hậu kiểm transcript, không bỏ qua Bash chỉ vì is_error: lệnh thoát khác 0 hoặc trả lỗi lạ vẫn có thể đã ghi ngoài worktree.
Chỉ coi Bash lỗi là chưa chạy khi IsPreExecDenial nhận diện thông báo chặn trước thực thi; thiếu tool_result thì chưa đủ dữ liệu để tính.
Với công cụ Edit, chỉ dựa vào is_error: kết quả thành công có thể chứa trích đoạn mã có chữ “hook error”, không được coi đó là dấu hiệu bị chặn.
Giữ cả ca hồi quy bỏ qua lời gọi bị chặn và ca phát hiện lệnh đã chạy nhưng báo lỗi trong session_isolation_test.go.
