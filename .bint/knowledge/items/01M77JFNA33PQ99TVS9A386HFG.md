---
schema: 1
id: 01M77JFNA33PQ99TVS9A386HFG
type: warning
status: verified
title: 'codex -c tách khoá theo dấu chấm, không hiểu dấu trích dẫn: -c ''mcp_servers."a.b".enabled=false'' làm Codex lỗi (invalid…'
scope:
  paths:
    - internal/eval/**
evidence:
  - path: internal/eval/codex_mcp.go
    lines: "97"
    fingerprint: lines:a32a3bb7121485eb
origin:
  kind: agent
  session: 72ad875c-8bdd-43a0-96a3-244e62243829
  commit: d81ab271f15631839e5ac6aba967563b0878d38a
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-13T03:46:35Z"
version: 1
created_at: "2026-11-12T09:47:36Z"
updated_at: "2026-11-13T03:46:35Z"
verified_at: "2026-11-13T03:46:35Z"
status_reason: người duyệt chấp nhận
---

codex -c tách khoá theo dấu chấm, không hiểu dấu trích dẫn: -c 'mcp_servers."a.b".enabled=false' làm Codex lỗi (invalid transport). Tên server có dấu chấm tắt bằng bảng nội tuyến -c 'mcp_servers={"a.b"={enabled=false}}' đặt TRƯỚC mọi -c mcp_servers.* khác (bảng nội tuyến thay thế các khoá đặt trước). Tắt tên không tồn tại (vd server chỉ có trong .codex/config.toml của dự án chưa trust) cũng làm Codex lỗi.
