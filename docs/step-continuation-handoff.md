# Bàn giao: nối tiếp phiên của bước (2026-11-16)

Kế hoạch gốc: `docs/step-continuation-plan.md`. Phiếu cha `01M7GD9ANV8E0R3CV8MZPRKSP2`.

## Trạng thái

| Giai đoạn | Phiếu | Trạng thái |
|---|---|---|
| GĐ1 — phân loại lý do dừng, lưu handle | `01M7GD9M0QKYZK1S5J94MRRA7F` | done, commit `810bf2b` (+ fix `fe99cab`, `6426ebe`) |
| GĐ2 — nối tiếp cho driver session, vòng câu hỏi b-req | `01M7GD9M9BARPG07T1N4QE6A2R` | **mã đã merge tay vào main, chưa qua b-review**; phiếu còn `interrupted` |
| GĐ3 — nối tiếp cho driver process (Claude, Codex) | `01M7GD9MKJY1GWSAGF3FF6BW0V` | chưa bắt đầu (`created`, phụ thuộc GĐ1+GĐ2) |

## GĐ2 — đã làm gì, còn gì

Yêu cầu/kế hoạch đầy đủ của b-req nằm ở thư mục phiếu (`bint task show 01M7GD9M9B…` → `requirement.md`, `plan.md`,
`tasks.md`). b-feat (sonnet) chạy hết 90 phút thì bị dừng vì quá `workflow.timeout_minutes`; lần resume sau bị Claude
Code tắt vì máy thiếu RAM. Mã trong worktree được đưa về main bằng `git apply` (không qua `bint task land`, vì land đòi
bằng chứng done).

Có trong mã (đối chiếu theo `tasks.md`):

- `internal/workflow/continuation.go`: `continuationDecision` (hàm thuần), `continuationPrompt`, `answeredFor`,
  `decideContinuation`/`planContinuation`/`armContinuation`/`freshenPending` (TSK-1, 3, 4, 6).
- Trường mới omitempty ở `task.PendingStep`/`task.StepStop` (`ContinueAgentID`, `FullPrompt`, `OriginRunID`,
  `UsageBase`…) (TSK-2).
- `complete --failed` phân loại lỗi hạ tầng qua wrapper ở `internal/eval/agent.go` (TSK-5).
- Cờ `bint run next --fresh` (`internal/cli/run_session.go`, `internal/app/run_svc.go`, inventory test) (TSK-6).
- `task.AnsweredQuestions` (`internal/task/artifactlint.go`) và chèn câu đã trả lời vào đề bài b-feat (TSK-8).
- Chi phí chỉ tính phần mới (`internal/workflow/session_cost.go`, `UsageBase`) (TSK-9).
- Alias `session-steps` cho run gốc, gỡ khi complete/abort/cancel (TSK-10).
- Skill b-do + mẫu flow skill + `docs/workflows.md`: SendMessage khi có `continue_agent_id`, fallback `--fresh`, vòng
  hỏi–đáp b-req, định tuyến yêu cầu mơ hồ (TSK-11); test `internal/cli/skill_continuation_test.go`.
- Test: `internal/workflow/continuation_test.go` (bảng quyết định, giới hạn 3 lần, fingerprint đổi, `--fresh`, vòng câu
  hỏi e2e, chi phí delta, phiếu cũ, alias).

Chưa xác nhận / còn phải làm:

0. **main đang đỏ ở `internal/workflow`**: `flow_golden_test.go:647` golden không khớp — `next` của bước b-review có
   thêm note "không nối tiếp phiên cũ: chỉ bước plan chờ người mới nối tiếp phiên; bước b-feat chạy mới". Note này có vẻ
   sai chỗ (bước đang mở là b-review nhưng nói b-feat; phát ra cả khi không có gì để nối tiếp) — sửa điều kiện phát note
   trong `internal/workflow/continuation.go` trước, đừng chỉ cập nhật golden. Các gói task, skills, eval, app, cli: qua.
1. **Chưa có b-review** cho GĐ2. Việc đầu tiên: review diff của commit GĐ2 (xem `git log`).
2. **Chưa chạy `go test -count=1 ./...` đầy đủ** trên mã GĐ2 (mới build, `go vet` linux+windows, staticcheck sạch trừ
   S1011 có sẵn, và test các gói bị ảnh hưởng — xem mục "Kiểm chứng" ở commit).
3. TSK-12: `printLastStepLine` in `via` và số lần nối tiếp — kiểm lại đã có chưa.
4. Bản cài skill (`.claude/skills/**`, `.agents/**`, `.bint/skills/core.lock`) chưa sinh lại: chạy `bint init` (người).
5. Đóng phiếu GĐ2: phiếu vẫn trỏ tới worktree cũ có cùng thay đổi. Cách gọn: `bint task cancel 01M7GD9M9B… --discard`
   (chỉ người; mã đã ở main), rồi bỏ `depends_on` GĐ2 khỏi GĐ3: `bint task set 01M7GD9MKJ… --clear-depends-on
   --depends-on 01M7GD9M0Q…`. Hoặc review xong thì `bint task status 01M7GD9M9B… done --force` (người).

## GĐ3 — việc cần làm

Phạm vi trong `docs/step-continuation-plan.md` mục "Giai đoạn 3". Điểm tựa đã có sau GĐ1/GĐ2:

- Bước Claude workflow chạy với `--session-id <uuid>` do Bint cấp và **không** còn `--no-session-persistence`
  (`internal/eval/agent.go`, `ArgsFor`); handle lưu ở `task.State.LastStep.SessionID`, lớp dừng ở `StopClass`.
- Codex: thread id lấy từ `codex exec --json` (GĐ1, `internal/eval/agent_harness.go`). `codex exec resume
  [SESSION_ID] [PROMPT]` có ở codex-cli 0.169.0 (đã kiểm `--help`); giữ nguyên các `-c` của bước gốc (xem tri thức về
  `-c mcp_servers`).
- Quyết định nối tiếp (`continuationDecision`) và lời nhắn (`continuationPrompt`) của GĐ2 dùng lại được; GĐ3 cần nhánh
  driver process: `Via` = harness thay vì `subagent`, chạy harness ở chế độ resume trong `runStep`.

Việc chính:

1. `eval.AgentRequest` thêm trường resume (session id + prompt nối tiếp); Claude: `claude -p --resume <id>`; Codex:
   `codex exec resume <id> <prompt>`. Test bằng agent giả/fixture, không gọi harness thật.
2. `runStep`/`bint run resume`: dùng `continuationDecision` cho driver process; không nối tiếp được thì chạy mới như cũ.
3. Câu hỏi mở b-req ở driver process: resume sau câu trả lời nối tiếp planner (giống GĐ2) thay vì vào thẳng b-feat.
4. **Chi phí**: đo xem `total_cost_usd`/usage sau `--resume` của Claude và Codex là cộng dồn hay chỉ phần mới; không
   tính trùng (fixture theo phiên bản như testdata hiện có).
5. Kiểm trường hợp Claude chết trước khi ghi phiên: handle do Bint cấp chưa chắc tồn tại trong `~/.claude/projects`
   (phát hiện low `.5` của GĐ1) — kiểm tồn tại hoặc thử resume rồi lùi về chạy mới khi lỗi "session not found".

## Phát hiện low còn gom (sửa một lần)

- `01M7GD9M0Q….2`: chú thích `heldReason` bị gắn nhầm vào `stopFingerprint` (`internal/workflow/run.go`).
- `01M7GD9M0Q….3`: thiếu test workflow kiểm bước Claude được cấp `--session-id`.
- `01M7GD9M0Q….4`: `beginStep` xoá `LastStep` trước khi chắc bước thật sự chạy (GĐ2 có thể đã xử lý — kiểm lại).
- `01M7GD9M0Q….5`: handle Claude do Bint cấp bị coi là continuable dù tiến trình chết trước khi ghi phiên (thuộc GĐ3 mục 5).
- `01M7GGHJF7….2`: nhánh `error|code` trước mã 5xx/429 và `quotaRe` vẫn có thể khớp nhầm (`internal/eval/agent_harness.go`).
- `01M7GHYSEG….1`: test mới của eval bị bỏ qua trên Windows; tên binary giả có ký tự không ASCII (`codex-lỗi thường`).
- Có sẵn trên main, ngoài các phiếu này: staticcheck S1011 ở `internal/tui/screens/tasks/graph.go:104` (CI đỏ cả hai nền tảng).

## Môi trường máy nhà (cần dọn)

- `.bint/local.yml` (gitignore) tạm đặt `workflow.timeout_minutes: 180`; xoá khi không cần.
- `bint` trên PATH đã `go install` lại từ `6426ebe`; cài lại sau khi pull để có GĐ2.
- Worktree GĐ2: `%LOCALAPPDATA%\bint\state\bint-8d262468937e\worktrees\01M7GD9M9BARPG07T1N4QE6A2R` (nhánh
  `bint/01M7GD9M9B…`), dọn khi huỷ/đóng phiếu.
- Tác vụ cũ `01M7G4D1TJAT48YTWM4HK4K22T` còn `in_review`, khiến mọi phiếu mới tự vào worktree.
