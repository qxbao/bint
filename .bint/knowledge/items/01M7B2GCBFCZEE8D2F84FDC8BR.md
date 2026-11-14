---
schema: 1
id: 01M7B2GCBFCZEE8D2F84FDC8BR
type: warning
status: verified
title: Git for Windows đặt core.autocrlf=true ở cấu hình hệ thống. testutil.Git chạy với GIT_CONFIG_NOSYSTEM=1 nhưng lệnh git …
scope:
  paths:
    - internal/**/*_test.go
    - internal/testutil/**
evidence:
  - path: internal/testutil/git.go
    lines: "20"
    fingerprint: lines:fc271feb12d55252
  - path: internal/workflow/isolation.go
    lines: "442"
    fingerprint: lines:b34122da689ac59f
origin:
  kind: agent
  session: 90ad2213-bdd5-45d1-913b-958c3658463a
  commit: dd5a45ed7167bfd7ffb0821ef58679f565d77f70
author: orca <dquanbao@gmail.com>
reviewed_by: orca <dquanbao@gmail.com>
reviewed_at: "2026-11-14T05:31:48Z"
version: 1
created_at: "2026-11-13T18:25:20Z"
updated_at: "2026-11-14T05:31:48Z"
verified_at: "2026-11-14T05:31:48Z"
status_reason: người duyệt chấp nhận
---

Git for Windows đặt core.autocrlf=true ở cấu hình hệ thống. testutil.Git chạy với GIT_CONFIG_NOSYSTEM=1 nhưng lệnh git do Bint gọi thì không, nên repo test dựng với LF còn checkout/apply/cat-file --filters của Bint ra CRLF: test so nội dung chính xác hỏng trên Windows dù Linux xanh. Test cần môi trường git kín (NOSYSTEM cho cả tiến trình); muốn kiểm hành vi autocrlf thì đặt core.autocrlf ở cấu hình repo để chạy được trên mọi OS.
