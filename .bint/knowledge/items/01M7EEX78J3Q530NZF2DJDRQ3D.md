---
schema: 1
id: 01M7EEX78J3Q530NZF2DJDRQ3D
type: warning
status: verified
title: 'Resolver Codex cần tách project_root_markers khỏi Git-main trust: từng lớp ưu tiên entry chính nó, gốc marker rồi main …'
scope:
  paths:
    - internal/codexconfig/**
    - internal/eval/codex_mcp*
evidence:
  - path: internal/codexconfig/config.go
    symbol: Config.trusts
    fingerprint: symbol:767c467f3d862bbb
  - path: internal/codexconfig/config.go
    symbol: Config.projectRoot
    fingerprint: symbol:8d57d53e28375991
  - path: internal/codexconfig/config.go
    symbol: repoRoots
    fingerprint: symbol:6da287f4fe5052a0
  - path: internal/codexconfig/config.go
    symbol: Config.TrustedDirs
    fingerprint: symbol:068024fccf4745a1
origin:
  kind: agent
  commit: c46f4e1bc3ef4dc394c436abaa4bba5bb0230a49
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_by: honggduc <148782719+Leekiri1234@users.noreply.github.com>
reviewed_at: "2026-11-15T16:07:15Z"
version: 1
created_at: "2026-11-15T01:59:47Z"
updated_at: "2026-11-15T16:07:15Z"
verified_at: "2026-11-15T16:07:15Z"
status_reason: 'Duyệt theo yêu cầu trực tiếp của người dùng trong phiên PR #8; đã đối chiếu nội dung với mã hiện tại, cập nhật bằng chứng resolver và vị trí bằng chứng dịch dòng.'
---

Resolver Codex cần tách project_root_markers khỏi Git-main trust: từng lớp ưu tiên entry chính nó, gốc marker rồi main Git. Worktree root trusted/untrusted áp cho lớp con; markers=[] chỉ quét cwd. Git discovery phải dùng môi trường đã bỏ GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE/GIT_COMMON_DIR như tiến trình Codex. TestCodexMCPRootTrustLive đối chiếu CLI thật không inference.
