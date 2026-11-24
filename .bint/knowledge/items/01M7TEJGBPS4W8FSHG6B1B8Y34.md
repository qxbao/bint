---
schema: 1
id: 01M7TEJGBPS4W8FSHG6B1B8Y34
type: warning
status: verified
title: go test chia test của gói ra nhiều tiến trình con (testutil.RunTests; BINT_TEST_PROCS, giới hạn toàn máy BINT_TEST_SLOT…
scope:
  paths:
    - internal/**/*_test.go
    - internal/testutil/**
evidence:
  - path: internal/testutil/shard.go
    lines: 80-100
    fingerprint: lines:b597accd9642fdfe
  - path: internal/tui/app/tasks_fixture_test.go
    lines: 233-266
    fingerprint: lines:9f4639a614c279f5
origin:
  kind: agent
  session: 930cb81b-bb95-400b-bb60-265403cc2bf5
  commit: c6972a0c7e799ea7aca6e2a6e3691730ab5e076d
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-24T17:04:46Z"
version: 1
created_at: "2026-11-19T17:44:49Z"
updated_at: "2026-11-19T17:47:06Z"
verified_at: "2026-11-24T17:04:46Z"
status_reason: người duyệt chấp nhận
---

go test chia test của gói ra nhiều tiến trình con (testutil.RunTests; BINT_TEST_PROCS, giới hạn toàn máy BINT_TEST_SLOTS; =1 để tắt khi cần đo tuần tự). Máy bận hơn nên test TUI không được gõ phím nối tiếp sau một action chạy nền: press/settle chỉ chờ 40 ms yên lặng; phải pump tới khi điều kiện đúng (vd. f.m.sy.Capturing()) rồi mới gõ tiếp. Hạn chờ trong test chỉ để chặn treo, đừng đặt sát thời gian thật.
