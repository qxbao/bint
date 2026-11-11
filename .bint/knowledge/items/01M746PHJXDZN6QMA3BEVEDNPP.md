---
schema: 1
id: 01M746PHJXDZN6QMA3BEVEDNPP
type: warning
status: stale
title: 'go test chạy trong phiên agent có CLAUDECODE=1: kiểm thử thao tác chỉ-dành-cho-người phải xoá biến agent'
scope:
  paths:
    - '**/*_test.go'
evidence:
  - path: internal/app/knowledge.go
    lines: 18-42
    fingerprint: lines:6aeab850efb7c995
  - path: internal/testutil/git.go
    lines: 52-78
    fingerprint: lines:6a2b13084e90182a
  - path: internal/cli/knowledge_e2e_test.go
    lines: 14-25
    fingerprint: lines:f23d29dce8172758
  - path: internal/evolve/evolve_test.go
    lines: 23-29
    fingerprint: lines:a331080b6e61ef56
origin:
  kind: agent
  session: edfbaec3-afde-4296-aadf-402dd3973dac
  commit: 513bc1852e6c99236813d3246054f164f6034664
  inputs:
    - internal/app/knowledge.go
    - internal/testutil/git.go
    - internal/cli/knowledge_e2e_test.go
    - internal/evolve/evolve_test.go
    - internal/app/context_test.go
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T02:27:50Z"
confidence: high
version: 1
created_at: "2026-11-11T02:23:55Z"
updated_at: "2026-11-11T17:47:13Z"
verified_at: "2026-11-11T02:27:50Z"
status_reason: 'bằng chứng thay đổi: bằng chứng internal/app/knowledge.go:18-42 đã thay đổi kể từ khi ghi: nội dung vùng dòng đã thay đổi'
---

`app.InAgentSession` coi tiến trình là agent khi có một biến trong `agentEnvVars` (BINT_AGENT, CLAUDECODE, AI_AGENT, CODEX_SANDBOX...); khi đó approve/apply/reject trả `ErrAgentForbidden`.
`testutil.Isolate` chỉ xoá CLAUDE_CODE_SESSION_ID/BINT_RUN_ID/BINT_TASK_ID/BINT_APPROVED_PATHS, KHÔNG xoá các biến trên. Kiểm thử đóng vai người phải gọi `asHuman(t)` (hoặc đặt rỗng từng biến); nếu không, test qua trên CI nhưng hỏng khi agent chạy `go test`.
Danh sách biến bị chép tay trong `internal/cli/knowledge_e2e_test.go` và `internal/evolve/evolve_test.go`: thêm biến vào `agentEnvVars` thì sửa cả hai.
