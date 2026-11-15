---
schema: 1
id: 01M7EEX78J3Q530NZF2DJDRQ3D
type: warning
status: candidate
title: 'Resolver Codex cần tách project_root_markers khỏi Git-main trust: từng lớp ưu tiên entry chính nó, gốc marker rồi main …'
scope:
  paths:
    - internal/codexconfig/**
    - internal/eval/codex_mcp*
evidence:
  - path: internal/codexconfig/config.go
    lines: "73"
    fingerprint: lines:99d7a1e31f421cd9
origin:
  kind: agent
  commit: c46f4e1bc3ef4dc394c436abaa4bba5bb0230a49
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
version: 1
created_at: "2026-11-15T01:59:47Z"
---

Resolver Codex cần tách project_root_markers khỏi Git-main trust: từng lớp ưu tiên entry chính nó, gốc marker rồi main Git. Worktree root trusted/untrusted áp cho lớp con; markers=[] chỉ quét cwd. Git discovery phải dùng môi trường đã bỏ GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE/GIT_COMMON_DIR như tiến trình Codex. TestCodexMCPRootTrustLive đối chiếu CLI thật không inference.
