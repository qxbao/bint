---
schema: 1
id: 01M78A0654JVMQRZPNS9TJ162C
type: warning
status: candidate
title: Không gọi Model.Update trong tea.Cmd truyền cho fixture.pump
scope:
  paths:
    - internal/tui/app/*_test.go
evidence:
  - path: internal/tui/app/tasks_fixture_test.go
    lines: 166-198
    fingerprint: lines:fae293f88b72b0d4
  - path: internal/tui/app/lifecycle_test.go
    lines: 474-476
    fingerprint: lines:67dc502014ddcda3
origin:
  kind: agent
  commit: fb1cc877c9e2b2dcd643c17e24d62cdda64d501a
author: honggduc <148782719+Leekiri1234@users.noreply.github.com>
version: 1
created_at: "2026-11-12T16:38:35Z"
---

fixture.pump chạy tea.Cmd trong goroutine nền, đồng thời tự gọi Model.Update trên goroutine kiểm thử. Nếu closure tea.Cmd cũng gọi Update thì model, toast và bảng thông báo bị truy cập đồng thời; go test -race đã bắt được lỗi trong TestUpdateTabShowsLiveLogAndEscCancels. Gửi phím bằng fixture.press rồi pump với cmd=nil, hoặc trả về thông điệp từ tea.Cmd để vòng pump cập nhật model tuần tự.
