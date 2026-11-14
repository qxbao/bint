---
schema: 1
id: 01M7DGMGC810QA0C2G90312RCS
type: warning
status: candidate
title: gitrepo.MainWorktree suy từ tên CommonDir kết thúc bằng .git nên trả rỗng với separate-git-dir và submodule. Khi xác đị…
scope:
  paths:
    - internal/codexconfig/**
evidence:
  - path: internal/codexconfig/config.go
    lines: "84"
    fingerprint: lines:50e61d965d64c584
origin:
  kind: agent
  commit: 25c26000b9b1fc48b2715f64fcb613d780f17a1f
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
version: 1
created_at: "2026-11-14T17:10:44Z"
---

gitrepo.MainWorktree suy từ tên CommonDir kết thúc bằng .git nên trả rỗng với separate-git-dir và submodule. Khi xác định trust của repo không phải linked worktree, dùng Toplevel làm gốc; nếu chỉ kế thừa từ MainWorktree, ExclusiveMCP bỏ sót server trong config thư mục con. Hồi quy TestCodexExclusiveMCPGitFileProjectLayers và CLI config opt-in xác nhận trên Codex 0.169.0.
