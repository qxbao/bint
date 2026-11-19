# Kế hoạch — G1: kiểm tra mục tiêu độc lập và đóng băng

## Hướng triển khai
Kind mới `goal` trong `internal/flow` (thứ tự plan → goal → implement → review); bước `b-goal` (Optional, Role `planner`, Skill
`b-goal`) chen giữa b-req và b-feat của flow `b-do` gốc. Logic mới ở `internal/workflow/goalfreeze.go`:
- `goalNeeded`: b-do gốc, không Fast, requirement có mục Kiểm tra mục tiêu, chưa có `State.GoalTest`. `pickStep` (planned) chọn b-goal.
- `beginStep` kind goal: giữ planned, chạy `preflightFeat`, khoá độc quyền, `beginTouched`; không handoff/continuation, `endStep` không
  ghi LastStep/LastPlan (b-feat vẫn nối tiếp b-req).
- `afterStep` kind goal: tệp đổi trong bước (rỗng → block), `checkScope`, ghi `State.GoalTest` (Path, SHA256, RunID, At), nối
  `Ticket.Scope.Deny`, chạy `goalGate` mốc (không chặn; xanh ngay → ghi chú test có thể yếu).
- `freezeGate`: đầu nhánh implement của `afterStep`, trước `checkScope`; sha lệch/thiếu → block, không qua approve. `checkScope` lọc tệp
  đóng băng còn khớp sha.
- `goalGate`: lệnh = `eval.ExpandTestCommand(affected_command, files)` khi có và verify đã xác nhận, không thì `verify.command`; chạy như
  `Env.verify` (env, TMPDIR, timeout, khoá verify); log `goal-N.log`; `State.GoalChecks` (kiểu `task.Verification`). Đỏ góp vào tín hiệu
  kiểm chứng đỏ và đề bài review; `finalize` chạy lại khi cây đổi, không done khi lần mới nhất đỏ.
- Đề bài: nhánh kind goal riêng (skill, phiếu, nguyên văn requirement.md; không plan/tasks); `goalCheckBlock` khi có GoalTest trả khối
  tệp đóng băng (cấm sửa, phải xanh), không thì khối cũ; `planHandoffPrompt`, vòng sửa dùng chung; `gate.go` sửa câu đề bài review.
- Driver session: `subagentOf(goal)` = implementer (planner chỉ đọc); model theo role planner.
- Skill mới b-goal; sửa b-feat, b-do và bản cài; `bint task show` in tệp đóng băng + cổng mục tiêu; `docs/workflows.md`.

## Cỡ kế hoạch
Kết luận: lớn
- `internal/flow/flow.go`, `internal/flow/builtin.go`, `internal/flow/validate.go`
- `internal/task/task.go`
- `internal/workflow/goalfreeze.go` (mới), `internal/workflow/run.go`, `internal/workflow/flowkind.go`
- `internal/workflow/fastpath.go`, `internal/workflow/plan_handoff.go`, `internal/workflow/gate.go`, `internal/workflow/session.go`
- `internal/cli/task.go`
- `internal/skills/core/b-goal/SKILL.md` (mới), `internal/skills/core/b-feat/SKILL.md`, `internal/skills/core/b-do/SKILL.md`
- `.claude/skills/b-goal/SKILL.md`, `.agents/skills/b-goal/SKILL.md`, bản cài b-feat/b-do, `.bint/skills/core.lock`
- `internal/workflow/testdata/flow_golden.json`, `internal/harness/testdata/flow_golden.json`
- `docs/workflows.md`
- `internal/workflow/goalfreeze_test.go` (mới)

## Bề mặt bị ảnh hưởng
- Định nghĩa flow (kind, b-do, validate, hash, golden) — AC-1
- Vòng đời bước của Runner (pickStep, begin/end/afterStep, Touched, nối tiếp b-req → b-feat) — AC-1
- Đề bài và skill (b-goal, b-feat, b-review, b-do) — AC-1, AC-4
- Trạng thái phiếu, task.yml (GoalTest, GoalChecks, scope.deny) — AC-2, AC-3
- Cổng sau implement (đóng băng, kiểm phạm vi/duyệt) — AC-2
- Lệnh, log, done (cổng mục tiêu, goal-N.log, finalize) — AC-3
- Hiển thị: `bint task show` — AC-3; TUI ngoài phạm vi (tab phiếu chưa hiện cổng mục tiêu, xem bằng CLI)
- Driver session (`bint run next`) — AC-1, AC-5
- Đường cũ (--fast, thiếu mục, flow b-feat một bước, flow tuỳ biến) — AC-4

## Giả định thay người dùng
- Role `planner` cho b-goal (mặc định khác model implementer), không thêm khoá cấu hình; đổi bằng `roles.planner`/`models.planner`.
  Trùng implementer thì ghi chú, không chặn.
- Không nối tiếp phiên b-req cho b-goal (phiên đó đã thấy plan); cơ chế a0b8581 giữ cho b-feat.
- Đóng băng mọi tệp b-goal đổi. Vi phạm → blocked, không tự revert; khôi phục bằng `git restore` rồi resume. Viết lại test mục tiêu:
  người chạy lại b-goal (b-feat chốt lệnh, ghi docs).
- Cổng mốc không chặn; cổng đỏ sau implement đi như kiểm chứng đỏ. Không affected_command thì chạy cả verify.command (chậm trên
  Windows [k:01M7CJ92ZB2DRYYHGFBQABYM9D]).

## Giả định và mâu thuẫn
`checkScope` coi chạm deny là cần duyệt, yêu cầu đòi từ chối: deny chỉ để thấy, từ chối do `freezeGate` (sha). Tệp b-goal nằm trong
Touched nên phải lọc khỏi hỏi duyệt. Đề bài review cũ nói test do b-feat viết: sửa.

## Lựa chọn và lý do
1. Kind `goal` (chọn): luật implement không đổi, nhánh mới tách bạch, hiện trong flow.
2. Bước implement có cờ: phải chặn từng luật implement (in_progress, firstFeat, verify, fix, handoff), dễ sót.
3. Pha nội bộ ngoài flow: driver session cần hành động riêng, khó resume.

## Vùng ảnh hưởng
`internal/flow` (Kinds, Validate, Hash), `internal/workflow`, `internal/task` (trường omitempty), `internal/cli`, skill. Phiếu cũ còn
planned sẽ chạy b-goal khi resume (ghi docs).

## Rủi ro và rollback
- BaseTree/firstFeat: b-goal ghi khi còn planned; b-feat (EverInProgress false) có thể đặt lại BaseTree làm mất tệp b-goal khỏi Touched.
  Đặt BaseTree ở bước ghi đầu tiên, không đặt lại; test (a) kiểm.
- Mất nối tiếp b-req → b-feat: test AC-1 kiểm handle.
- Golden flow đổi: cập nhật bằng cờ golden, soát diff. Rollback: revert commit.

## Chiến lược kiểm thử
- `TestGoalFreezeBlackBox` (`internal/workflow/goalfreeze_test.go`) theo `requirement.md`.
- Unit: flow (Kinds, validate), dựng lệnh goalGate, khối đề bài đóng băng, checkScope lọc; `GOOS=windows go vet ./...`
  [k:01M746NASVKV9XMRHA4QNHJ3R8]; đường dẫn `/` trong `sh -c` [k:01M7CX9V792957T6MGK6QWEJ7F]; planner giả ghi plan nhỏ hợp lệ
  [k:01M7MKY65CS6P4Q62W7R4Y7JD2].
- Chưa kiểm: model thật viết test hộp đen tốt hơn (benchmark sau).

## Checklist nhất quán
AC-1: TSK-1, TSK-2; AC-2: TSK-3; AC-3: TSK-4; AC-4: TSK-5; AC-5: TSK-6. Mọi tệp nằm trong `--allow`.
