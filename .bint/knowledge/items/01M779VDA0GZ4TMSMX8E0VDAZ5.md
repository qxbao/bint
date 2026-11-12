---
schema: 1
id: 01M779VDA0GZ4TMSMX8E0VDAZ5
type: warning
status: candidate
title: 'Routing task có sẵn phải đánh giá protected scope sau khi áp edit hợp lệ'
scope:
  paths:
    - internal/workflow/run.go
evidence:
  - path: internal/workflow/run.go
    lines: "340"
    fingerprint: lines:d439d21b32589415
origin:
  kind: agent
  commit: e9ce282ba1671a476b195eb9b5d2a490fb77acac
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-12T07:16:44Z"
---

Trước sửa, Runner đánh giá --no-plan trên scope cũ trước khi áp --allow, nên agent có thể bỏ b-req khi scope mới chạm protected path. Hồi quy task 01M77MTS8RPSP3MS0QCXNYW5YE đã tái hiện b-feat chạy trước và snapshot planner rỗng. Runner nay kiểm override, áp edit hợp lệ rồi tính classification trong cùng callback; kiểm thử xác nhận scope mới buộc b-req và model planner được chọn. Scope guard và human approval vẫn độc lập; evidence trước/sau tại docs/evals/codex-flow-acceptance-2026-11-12/evidence/routing-protected-scope-2026-11-12.txt. Candidate chờ người duyệt.
