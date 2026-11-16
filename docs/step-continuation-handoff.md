# Bàn giao: nối tiếp phiên của bước (2026-11-16)

Kế hoạch gốc: `docs/step-continuation-plan.md`. Phiếu cha `01M7GD9ANV8E0R3CV8MZPRKSP2`.

## Trạng thái

| Giai đoạn | Phiếu | Trạng thái |
|---|---|---|
| GĐ1 — phân loại lý do dừng, lưu handle | `01M7GD9M0QKYZK1S5J94MRRA7F` | done, commit `810bf2b` (+ fix `fe99cab`, `6426ebe`) |
| GĐ2 — nối tiếp cho driver session, vòng câu hỏi b-req | `01M7GD9M9BARPG07T1N4QE6A2R` | **mã đã merge tay vào main, chưa qua b-review**; phiếu còn `interrupted` |
| GĐ3 — nối tiếp cho driver process (Claude, Codex) | `01M7H1PMC59NKZFXZH445VAQZ0` (giai đoạn B) | mã xong (2026-11-16), chờ b-review; còn đo thật `NT-RESUME-CLAUDE`/`NT-RESUME-CODEX` (docs/acceptance.md V1 bước 6) |

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

Đã xử lý ở phiếu `01M7H1P2Z7F782Y1ZNHN5VR71F` (giai đoạn A, 2026-11-16):

0. ~~main đỏ ở `internal/workflow`~~: sửa gốc ở `continuationDecision` (`internal/workflow/continuation.go`): không có `LastStep`
   hoặc bước sắp mở khác bước dừng thì `Why` rỗng (kể cả `--fresh`), nên `next` không phát note "không nối tiếp" ở nơi không có gì
   để nối tiếp. Golden `sessions/protected_then_resume` giữ nguyên; thêm ca vào `TestContinuationDecision`. `docs/workflows.md`
   đã sửa theo.
1. ~~Review diff GĐ2~~: đã rà (đúng đắn, tương thích ngược state, i18n, Windows). Phát hiện đã sửa: R-1 (thứ tự điều kiện, mục 0),
   R-2 (regex hạ tầng khớp nhầm văn bản tự do: `quotaRe`, `codexServerRe`, `codexRateRe` chỉ nhận cụm hạn mức/mã lỗi thật,
   có ca âm "test đỏ ở internal/quota", "chưa xong rate limiter", "error: 500 dòng log bị cắt"), R-3 (`fillSubagentUsage` chỉ ghi
   0 USD cho bước nối tiếp có mốc usage cùng transcript; bước thường usage rỗng giữ unknown).
2. ~~`go test -count=1 ./...` đầy đủ~~: xem kết quả ở mục "Kiểm chứng giai đoạn A" bên dưới; `go vet` và `staticcheck` sạch
   linux + `GOOS=windows`, kể cả S1011 ở `graph.go`.
3. ~~TSK-12~~: `printLastStepLine` đã in `via` và `đã nối tiếp N lần` (`internal/cli/task.go`, dùng chung cho `task show` và
   `run status`); thêm test ở `internal/cli/last_step_e2e_test.go`.

Kiểm chứng giai đoạn A (linux, 2026-11-16): `go test -count=1 -timeout 30m ./...` xanh toàn bộ gói; `go vet ./...` và
`staticcheck ./...` sạch với `GOOS=linux` và `GOOS=windows`. Chưa chạy test trên Windows thật (chỉ vet/staticcheck chéo).

Phát hiện low đã xử lý: `01M7GD9M0Q….2` (chú thích `heldReason` nay ngay trên `func heldReason`), `….3` (test
`TestProcessClaudeStepGetsSessionIDAndRecordsHandle`), `….4` (`LastStep` chỉ bị xoá khi bước thật sự bắt đầu: trong cùng lần
ghi `PendingStep` của `grantPending` ở driver session, ngay trước `agent.Run` ở `runStep`; test
`TestGrantPendingKeepsLastStepOnFailure`), `01M7GGHJF7….2` (regex), `01M7GHYSEG….1` (`TestCodexRunClassifiesInfraFromTurnFailed`
là test thuần, không `sh`, tên ca ASCII), S1011 ở `internal/tui/screens/tasks/graph.go`. `….5` đã xử lý ở GĐ3.

Còn phải làm:

4. Bản cài skill (`.claude/skills/**`, `.agents/**`, `.bint/skills/core.lock`) chưa sinh lại: chạy `bint init` (người).
5. Đóng phiếu GĐ2: phiếu vẫn trỏ tới worktree cũ có cùng thay đổi. Cách gọn: `bint task cancel 01M7GD9M9B… --discard`
   (chỉ người; mã đã ở main), rồi bỏ `depends_on` GĐ2 khỏi GĐ3: `bint task set 01M7GD9MKJ… --clear-depends-on
   --depends-on 01M7GD9M0Q…`. Hoặc review xong thì `bint task status 01M7GD9M9B… done --force` (người).

## GĐ3 — đã làm gì, còn gì

Đã làm (2026-11-16): xem "Giai đoạn 3 — Đã làm" ở `docs/step-continuation-plan.md` và mục "Nối tiếp phiên ở driver process" ở
`docs/workflows.md`. Tóm tắt điểm tựa cho người đọc sau:

- Eval: `AgentRequest.ResumeSessionID`, `AgentResult.ResumeMissing`; `ClaudeAgent.ArgsFor` (`--resume`), `CodexAgent.ArgsFor`
  (`… resume <id> -`, cờ của bước gốc đứng trước `resume`). Test: `internal/eval/resume_test.go` (so đối số gốc/nối tiếp, đối chiếu cờ với
  `testdata/codex-0.167/exec*-help.txt`, `ResumeMissing` bằng tiến trình giả).
- Workflow: `runStep` → `armProcessContinuation`; `armPlanContinuation` + `rs.planCont`; `process_cost.go` (chi phí/mốc từ tệp phiên);
  `LastStep.Via` = harness. Test: `internal/workflow/process_continuation_test.go` (external, timeout một lần, trần 3, cây đổi, khác harness,
  không Via, `--fresh`, phiên chưa ghi, phiên bị xoá, `ResumeMissing`, chi phí Claude từ fixture, token Codex từ rollout, vòng câu hỏi b-req
  qua resume / `b-do --task` / `b-req --task`).
- Tệp phiên: `claude.SessionTranscriptPath` (`CLAUDE_CONFIG_DIR`), `codex.RolloutPath`/`RolloutUsage` (`CODEX_HOME`); fixture
  `internal/harness/claude/testdata/claude-2.1.322/transcript-*-resume.jsonl`, `internal/harness/codex/testdata/rollout-*.jsonl` (do tay dựng
  theo cấu trúc thật, không phải bản chụp).
- Cờ `--fresh`: `bint run resume --fresh`, `bint run <wf> --task <id> --fresh` (cliRefs/inventory, i18n vi/en).

Còn phải làm:

1. Đo thật `NT-RESUME-CLAUDE`, `NT-RESUME-CODEX` (người, V1 bước 6): A1–A3, ngữ nghĩa `total_cost_usd`/usage sau resume, `--max-budget-usd` sau
   resume (có thể làm bước nối tiếp dừng sớm), cờ trước `resume` của Codex còn hiệu lực không.
2. `bint init` để sinh lại bản cài skill (skill b-do đổi); đóng phiếu GĐ2 như mục trên.
3. Codex 0.169: kế hoạch ghi 0.169 nhưng máy dựng chỉ có 0.167; chạy lại `codex exec --help`/`exec resume --help` bằng bản mới và cập nhật
   fixture nếu khác.

## Phát hiện low còn gom

Đã sửa ở phiếu `01M7H1P2Z7F782Y1ZNHN5VR71F` (xem mục trên): `01M7GD9M0Q….2`, `….3`, `….4`, `01M7GGHJF7….2`,
`01M7GHYSEG….1`, và S1011 ở `internal/tui/screens/tasks/graph.go`. Còn lại:

- ~~`01M7GD9M0Q….5`~~: đã xử lý ở GĐ3 (kiểm tệp phiên lúc dừng: `continuable=false` khi Claude chưa ghi phiên).

## Môi trường máy nhà (cần dọn)

- `.bint/local.yml` (gitignore) tạm đặt `workflow.timeout_minutes: 180`; xoá khi không cần.
- `bint` trên PATH đã `go install` lại từ `6426ebe`; cài lại sau khi pull để có GĐ2.
- Worktree GĐ2: `%LOCALAPPDATA%\bint\state\bint-8d262468937e\worktrees\01M7GD9M9BARPG07T1N4QE6A2R` (nhánh
  `bint/01M7GD9M9B…`), dọn khi huỷ/đóng phiếu.
- Tác vụ cũ `01M7G4D1TJAT48YTWM4HK4K22T` còn `in_review`, khiến mọi phiếu mới tự vào worktree.
