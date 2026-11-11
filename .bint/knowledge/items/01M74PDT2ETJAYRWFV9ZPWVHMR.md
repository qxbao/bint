---
schema: 1
id: 01M74PDT2ETJAYRWFV9ZPWVHMR
type: warning
status: candidate
title: Cờ security không thay thế cô lập agent và mạng
scope:
  paths:
    - internal/config/**
    - internal/eval/**
evidence:
  - path: internal/config/config.go
    lines: 385-389
    fingerprint: lines:599f9b25a6dff930
  - path: internal/cli/eval.go
    lines: "1009"
    fingerprint: lines:169434fc1a9da302
origin:
  kind: agent
  commit: 183ac178f5a3bffae7c0b44ebfd17d9e2731ea06
author: Leekiri1234 <hongducluong0603@gmail.com>
version: 1
created_at: "2026-11-11T06:58:46Z"
---

`security.allow_external_agents` và `security.allow_network_in_evals` hiện mới là khoá khai báo trong schema/mặc định, chưa gắn với cơ chế chặn agent ngoài hay cô lập mạng ở tầng hệ điều hành; lượt đánh giá vẫn chạy với quyền tiến trình của người dùng và truy cập mạng phụ thuộc vào cấu hình sandbox của từng harness.
