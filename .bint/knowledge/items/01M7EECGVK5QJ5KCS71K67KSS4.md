---
schema: 1
id: 01M7EECGVK5QJ5KCS71K67KSS4
type: warning
status: candidate
title: runProcess thu stdout riêng làm os/exec dùng hai copy goroutine cho stdout/stderr cùng ghi diagnostic tail; tailBuffer …
scope:
  paths:
    - internal/eval/shell.go
evidence:
  - path: internal/eval/shell.go
    lines: 125-150
    fingerprint: lines:4ac1f353e3ba7595
origin:
  kind: agent
  commit: c46f4e1bc3ef4dc394c436abaa4bba5bb0230a49
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
version: 1
created_at: "2026-11-15T01:50:40Z"
---

runProcess thu stdout riêng làm os/exec dùng hai copy goroutine cho stdout/stderr cùng ghi diagnostic tail; tailBuffer cần đồng bộ cả Write/String. Hồi quy TestRunProcessConcurrentOutput tái hiện race bằng tiến trình fixture thật, không cần inference.
