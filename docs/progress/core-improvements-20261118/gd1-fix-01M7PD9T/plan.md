# Kế hoạch

## Hướng triển khai
Sáu sửa nhỏ, độc lập, theo đúng gợi ý của review (gd1-01M7NA28/review.md); không đổi cấu hình, không thêm trạng thái mới.

- **#7 — tiếp tục sau duyệt phạm vi b-goal.** Thêm `State.GoalAwaitingCheck bool` (json `goal_awaiting_check,omitempty`,
  `internal/task/task.go`). `afterGoal` ghi cờ = true TRƯỚC `checkScope`; xoá cờ khi đã ghi `GoalTest` hoặc khi chặn "không tạo tệp
  nào" (để resume chạy lại b-goal như cũ). `pickStep` (run.go ~830): `planned` + cờ → bước giả mới `stepGoalCheck` (vd "post-goal");
  vòng `Run` (run.go ~545) và vòng driver session (session.go ~576) xử lý nó bằng `afterGoal` như `stepCheck` → `afterStep` của implement.
  `goalPending` trả false khi cờ bật. Vì sao cờ riêng thay vì dùng lại `AwaitingCheck`: `AwaitingCheck` gắn với `in_progress` và được
  reverify/gate/ops đọc; dùng lại dễ lây sang đường implement.
- **#4 — lối thoát của người.** Thêm `BlockGoalFailed` vào `Forceable()` (`internal/task/status.go`), sửa chú thích; `BlockGoalMissing`
  giữ cứng. `land --force` đã kiểm Human + Reason và ghi Bypassed nên không cần đổi land.go. Cập nhật docs/workflows.md (dòng ~397:
  thay "Chưa có đường bỏ đóng băng…" bằng: shared → `bint task status <id> done --force`, worktree → `bint task land <id> --force --reason`).
  Chọn hướng này thay `--regoal`: một dòng, nhất quán với kiểm chứng đỏ, đảo ngược được (chỉ là quyết định của người, có ghi lý do).
- **#3 — cổng chỉ chạy test mục tiêu.** Trong `goalCommand` (goalfreeze.go): sau khi chọn lệnh như cũ, nếu lệnh (sau TrimSpace) bắt đầu
  bằng `go test`, chưa có `-run`, và các tệp đóng băng `*_test.go` khai báo ≥ 1 hàm `Test*` (đọc bằng `go/parser`, chỉ hàm cấp gói tên
  `Test…` có một tham số `*testing.T`) thì chèn ` -run '^(A|B)$'` ngay sau `go test`. Tên sắp xếp, bỏ trùng. Không có tên → lệnh cũ.
- **#8 — rào đọc plan/tasks.** `run.go` (~1035, dựng env bước): khi `sr.kind == flow.KindGoal` thêm
  `BINT_DENY_READ=<abs plan.md>\n<abs tasks.md>`. `hookcore`: hằng `EnvDenyRead`; ở nhánh PreToolUse, trước các luật khác, nếu biến có
  giá trị: ToolRead có đường dẫn (sau Abs/Clean, so không phân biệt hoa thường trên Windows) trùng một mục → deny; ToolShell có lệnh
  chứa nguyên văn một đường dẫn (cả dạng `/`) → deny. Lý do deny nêu "b-goal chỉ được dựa vào requirement.md". Hook không trả lỗi, không
  mở SQLite [k:01M746NPTW1B304MRQQPBCAFAM]; deny có chủ đích như guardrail chỉ đọc [k:01M7CQSPKF9T8R6M38ZDK9GY0X]. Dialect không hỗ
  trợ deny PreToolUse thì bỏ qua (giữ hành vi cũ). Thêm `BINT_DENY_READ` vào danh sách biến bị lọc khi kiểm chứng (`internal/eval/shell.go`
  ~165/178) như BINT_APPROVED_PATHS.
- **#9.** `tasks_fixture_test.go`: thêm `f.tryTask(id) *task.Task` (lỗi → nil); `tasks_milestone_test.go:77` dùng
  `t := f.tryTask(id); return t != nil && t.State.VerifyApproval != nil`. Các `f.task` gọi ngoài pump (sau `waitJobsDone`) giữ nguyên.
- **#5.** goalfreeze_test.go: kiểm `err` của `r.Approve` và `ApprovedPaths(tk)` chứa `frozenRel` (ca sửa/xoá: tệp bị sửa có trong
  ApprovedPaths mà vẫn blocked); ca thiếu mục: `goal-1.log` không tồn tại và `h.prompts[StepFeat]` không có "ĐÃ ĐƯỢC VIẾT độc lập";
  ca driver session: `act.Model` của b-goal = model của `models.planner` trong cấu hình harness test.

## Cỡ kế hoạch
Kết luận: lớn
- `internal/task/task.go` — trường GoalAwaitingCheck
- `internal/task/status.go` — Forceable gồm BlockGoalFailed
- `internal/task/status_test.go` — test Forceable
- `internal/workflow/goalfreeze.go` — cờ #7, goalCommand #3
- `internal/workflow/run.go` — pickStep/vòng Run #7, env b-goal #8
- `internal/workflow/session.go` — bước giả #7 ở driver session
- `internal/workflow/goalfreeze_test.go` — khẳng định #5, test đơn vị goalCommand
- `internal/workflow/goalfix_test.go` — kiểm tra mục tiêu (b-goal viết)
- `internal/harness/hookcore/hook.go` — deny đọc theo BINT_DENY_READ
- `internal/harness/hookcore/deny_read_test.go` — test hook
- `internal/eval/shell.go` — lọc BINT_DENY_READ khỏi env kiểm chứng
- `internal/tui/app/tasks_fixture_test.go` — tryTask
- `internal/tui/app/tasks_milestone_test.go` — điều kiện chờ #9
- `docs/workflows.md` — lối thoát #4, cổng -run #3, rào #8

## Bề mặt bị ảnh hưởng
- Điều phối b-goal (Runner `Run` và driver session `Next/Complete`): resume sau duyệt — AC-1.
- Điều chặn done/land (`DoneBlockersTyped`, `Land --force`, `status done --force`) — AC-2.
- Lệnh cổng mục tiêu (`goalCommand`, log goal-N.log, `State.GoalChecks`) — AC-3.
- Môi trường tiến trình bước + hook PreToolUse (Claude/Codex) + env kiểm chứng — AC-4.
- Test TUI milestone (fixture pump) — AC-5.
- Test hộp đen GĐ1 — AC-6.
- Tài liệu người dùng (docs/workflows.md) — AC-2 (lối thoát), mô tả #3/#8 đi cùng TSK tương ứng.
- Driver session với #8: Ngoài phạm vi (env không tới được subagent; vẫn là ràng buộc mềm, docs ghi rõ).

## Giả định thay người dùng
- #4 dùng "ép bỏ qua có lý do" (Forceable) thay vì lệnh chạy lại b-goal: ít xâm lấn, đảo ngược được. Muốn lệnh `--regoal` thì lập phiếu riêng.
- #3 chỉ thu hẹp cho Go (`go test` + `*_test.go`), vì đó là lệnh của repo này; ngôn ngữ khác giữ lệnh cũ (cả gói) — không làm hỏng gì.
  Nếu `affected_command` trống, lệnh rơi về `verify.command` (`go test ./...`) kèm `-run`: vẫn biên dịch mọi gói nhưng chỉ chạy test mục tiêu.
- #8 so khớp đường dẫn tuyệt đối/chuỗi con trong lệnh shell; lệnh cố tình né (ghép chuỗi, biến) không bị bắt — chặn đọc nhầm, không chặn cố ý.
- #9 chỉ sửa test (thử lại khi đọc), không sửa cơ chế ghi `state.json`.

## Rủi ro
- #7: cờ kẹt bật (b-goal lỗi giữa chừng) làm resume không bao giờ chạy lại b-goal → chỉ đặt cờ trong `afterGoal` (sau khi agent xong),
  xoá ở nhánh "không có tệp". Test hồi quy: ca "b-goal không viết gì" cũ vẫn chạy lại b-goal.
- #8: hook deny nhầm bước khác → chỉ đặt env cho kind goal; test kiểm Env của mọi bước. Biến lọt vào lệnh kiểm chứng → thêm vào danh sách lọc.
- #3: chèn `-run` sai cú pháp sh trên Windows → dùng nháy đơn, test đơn vị so nguyên chuỗi.
- Gói workflow chậm trên Windows (~28 phút); verify_timeout_minutes đã 60.

## Chiến lược kiểm thử
- Kiểm tra mục tiêu `TestGoalFixRound` (internal/workflow/goalfix_test.go, do b-goal viết; xem requirement.md) cho #7/#4/#3/#8.
- Test đơn vị: `Forceable` (task), `goalCommand` (bảng ca: go+Test, go có -run, check.sh, tệp Go không có Test), hook deny (hookcore).
- #9: `go test -race -count=3 -run TestMilestoneTaskToDone ./internal/tui/app`. #5: chạy `TestGoalFreezeBlackBox`.
- Cuối: `go test -timeout 30m ./...`, `go vet ./...`, `GOOS=windows go vet ./...` [k:01M746NASVKV9XMRHA4QNHJ3R8].
