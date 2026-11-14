---
schema: 1
id: 01M7CQSPARNSRJG3NCAV4Y0V3K
type: warning
status: candidate
title: Khoá workflow và evolve dùng internal/fslock; giữ tệp khoá khi nhả
scope:
  paths:
    - internal/workflow/**
    - internal/evolve/**
    - internal/task/**
    - internal/fslock/**
evidence:
  - path: internal/evolve/apply.go
    lines: 32-39
    fingerprint: lines:5cc9dcd48c69c5ee
  - path: internal/workflow/isolation.go
    lines: 86-94
    fingerprint: lines:1849c2f07f2afda0
  - path: internal/fslock/fslock.go
    lines: 1-7
    fingerprint: lines:b27a8263d898df67
origin:
  kind: agent
  commit: 66bfe223b81eec049cb3cba6afeeeea79e51ba0b
  inputs:
    - internal/evolve/apply.go
    - internal/workflow/isolation.go
    - internal/fslock/fslock.go
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
confidence: high
version: 1
created_at: "2026-11-14T09:56:40Z"
---

Dùng internal/fslock cho khoá liên tiến trình; không thu hồi khoá bằng cách kiểm PID rồi xoá hoặc rename tệp.
Workflow và evolve đều đã dùng khoá hệ điều hành (flock/LockFileEx); đóng handle để nhả, giữ nguyên tệp khoá để tránh hai chủ giữ hai inode khác nhau.
PID/owner chỉ phục vụ thông báo, không quyết định quyền giữ khoá.
Mục này thay thế nhận định cũ trong 01M746PTBYPSXDT0WJZS38KPFD rằng evolve.AcquireLock còn dùng O_EXCL; nhận định đó đã hết đúng sau commit f1467a0a1c91.
