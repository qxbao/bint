---
schema: 1
id: 01M7HX0RXVEF2AYCCZDA41669X
type: warning
status: verified
title: 'Trong profile iso HOME/XDG là giả: tiến trình bint con phải được ghim BINT_STATE_DIR/BINT_CONFIG_HOME thật, và Codex MC…'
scope:
  paths:
    - internal/iso/**
    - internal/state/**
    - internal/config/**
    - internal/app/iso_sync*
    - internal/app/iso_import*
evidence:
  - path: internal/iso/env.go
    lines: "144"
    fingerprint: lines:226f1bc1cff3629d
  - path: internal/app/iso_sync.go
    lines: "108"
    fingerprint: lines:8f88113fc838a15b
origin:
  kind: agent
  session: a06f0a5b-d4b9-447f-9a33-384b86f7537f
  commit: 339f13400f3d3150607c79924ccc8ab952eb71aa
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-21T03:52:23Z"
version: 1
created_at: "2026-11-16T10:04:07Z"
updated_at: "2026-11-16T11:08:58Z"
verified_at: "2026-11-21T03:52:23Z"
status_reason: người duyệt chấp nhận
---

Trong profile iso HOME/XDG là giả: tiến trình bint con phải được ghim BINT_STATE_DIR/BINT_CONFIG_HOME thật, và Codex MCP stdio chỉ nhận HOME/PATH/TMPDIR nên mục [mcp_servers.bint] phải có bảng env. iso.Env đặt BINT_STATE_DIR/BINT_CONFIG_HOME tuyệt đối của máy thật (không để bint trong profile suy từ XDG giả rồi mở state.sqlite rỗng). Codex (spike S1) không chuyển env của tiến trình cho MCP stdio, chỉ HOME/PATH/TMPDIR, nên iso sync chép mục MCP chạy bint kèm bảng env BINT_STATE_DIR/BINT_CONFIG_HOME (iso.BintMCPEnv + AdaptCodexMCP); chép nguyên mục từ bản thật thì bint mcp do Codex khởi chạy trong profile rơi về state giả. Đường dẫn state/config trong profile luôn lấy từ iso.RealEnv/RealHome, không từ os.Getenv(HOME).
