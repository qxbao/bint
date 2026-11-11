---
schema: 1
id: 01M74D57EZ2HZSQMTB9YP56F3D
type: warning
status: verified
title: CheckLeak chỉ cảnh báo định danh mới; bài học dùng kiểm rò rỉ riêng (định danh mã, định danh mới ở vị trí mã, literal)
scope:
  paths:
    - internal/evolve/**
evidence:
  - path: internal/eval/leak.go
    lines: 202-322
    fingerprint: lines:557eeecd14d6b0b8
  - path: internal/evolve/lesson_check.go
    lines: 116-140
    fingerprint: lines:0dee7f8d0f5a3801
origin:
  kind: agent
  session: f6df3521-2b70-4d5c-8b4b-923b360d080d
  commit: 7dd832f666fa77c584f5354e5d1ec02638134543
author: Duong Quan Bao <dquanbao@gmail.com>
reviewed_by: Duong Quan Bao <dquanbao@gmail.com>
reviewed_at: "2026-11-11T08:33:21Z"
version: 1
created_at: "2026-11-11T04:16:48Z"
updated_at: "2026-11-11T08:33:21Z"
verified_at: "2026-11-11T08:33:21Z"
status_reason: người duyệt chấp nhận
---

CheckLeak xếp định danh mới của lời giải (NewIdents) vào mức warn, chỉ mục solution_code mới là fail. Bài học do reflector soạn không được dùng CheckLeak một mình; evolve.CheckLesson (lesson_check.go) chặn (LessonLeak) khi nội dung bài học chứa:
- eval.SolutionIdentifiers: định danh dạng mã (snake/camelCase, có số) của phần thêm mới, trừ tên test;
- eval.SolutionNewIdentifiers: MỌI định danh mới ở VỊ TRÍ MÃ (kể cả tên một từ như Reset/Flush), trừ tên test. Từ thường chỉ nằm trong chú thích hoặc nội dung chuỗi (server, cache, timeout...) không bị chặn, vì bài học hay nhắc chúng;
- eval.SolutionLiterals: chuỗi literal ≥ 12 ký tự ("...", '...', `...` raw) của phần thêm mới, trừ chuỗi có trong kiểm thử.
Thêm ca chặn mới thì sửa TestCheckLessonTable (failure_test.go), kể cả ca chặn nhầm.
