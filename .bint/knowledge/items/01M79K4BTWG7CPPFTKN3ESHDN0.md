---
schema: 1
id: 01M79K4BTWG7CPPFTKN3ESHDN0
type: warning
status: candidate
title: skip-worktree che thay đổi config khỏi git status; dùng diff theo snapshot và xác nhận attribution
scope:
  paths:
    - .bint/config.yml
evidence:
  - path: docs/reacceptance-2026-11-13/deferred.md
    lines: "23"
    fingerprint: lines:539573ca0f419434
origin:
  kind: agent
  commit: d788669b8f9754c21a94fb8324a65731b2edcc38
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-13T04:37:23Z"
---

Trong đợt nghiệm thu 2026-11-13, .bint/config.yml có cờ skip-worktree (git ls-files -v trả S); git status và git diff HEAD không hiển thị thay đổi nội dung trong phiên, trong khi bint task diff so với base_tree vẫn hiển thị hunk reviewer/evolver. Không dùng git status sạch để kết luận config giữ nguyên. Đối chiếu nội dung với snapshot đầu tác vụ, ghi riêng thay đổi đồng thời của người dùng và giữ nguyên khi chưa rõ ownership. Người dùng đã xác nhận hunk hai model trong DEF-017 là của họ; không sửa config hoặc metadata để làm biến mất chênh lệch.
