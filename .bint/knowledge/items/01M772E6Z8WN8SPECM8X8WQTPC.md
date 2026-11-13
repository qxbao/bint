---
schema: 1
id: 01M772E6Z8WN8SPECM8X8WQTPC
type: warning
status: deprecated
title: Role chưa chạy bị lưu như snapshot model rỗng khi chuyển từ b-req sang b-do
scope:
  paths:
    - internal/app/run_svc.go
  tags:
    - review
evidence:
  - path: internal/app/run_svc.go
    lines: 210-236
    fingerprint: lines:fee01304be475b1d
origin:
  kind: agent
  run_id: 01M7727NRVCSHANVFPSN21XS3Z
  commit: e9ce282ba1671a476b195eb9b5d2a490fb77acac
  inputs:
    - internal/app/run_svc.go
author: Leekiri1234 <hongducluong0603@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-13T07:57:49Z"
version: 1
created_at: "2026-11-12T05:07:11Z"
updated_at: "2026-11-13T07:57:49Z"
status_reason: người duyệt từ chối
---

Chỉ lưu role đã được chọn thực sự, hoặc coi lựa chọn có Harness rỗng là chưa chụp; thêm hồi quy b-req sang b-do với model cấu hình khác mặc định CLI.

Căn cứ: agentChoices cho b-req luôn ghi cả implementer/reviewer với AgentChoice rỗng ở dòng 214-216. Lần b-do --task sau đó thấy saved=true cho hai role, ForStep lấy harness từ cấu hình nhưng nhánh 233-234 không lấy model cấu hình. runWith đưa key model rỗng vào HarnessAgentsWithChoices, nơi ModelSet=true giữ mặc định CLI thay vì model theo role. Runner cho phép chuyển Workflow b-req sang b-do tại internal/workflow/run.go:228-231. Ví dụ roles.implementer=codex và harnesses.codex.model=gpt-config: sau b-req, b-do chạy Codex với model rỗng. Test hiện tại chỉ xét role đang chạy hoặc snapshot có đủ ba role.

(Rút ra từ phát hiện review 01M76X7ZNGP537004YW0P5JP7N.13, tác vụ 01M76X7ZNGP537004YW0P5JP7N.)
