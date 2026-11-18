# Kế hoạch

## Hướng triển khai
Ba phần độc lập, mỗi phần là một lát (GĐ2 chia lát theo thứ tự nhóm của `## Bề mặt bị ảnh hưởng`; nhóm đầu nhỏ nhất). Không thêm bước flow,
không thêm trạng thái tác vụ: hai cổng mới là phần mở rộng của cổng sau bước implement (`afterStep` nhánh `KindImplement`, dùng chung cho
driver process và session), kết quả đỏ đi qua đường đã có (`redVerification` → vòng sửa; `sliceGate` → bỏ lát).

1. **Review cần bằng chứng** (`internal/task/review.go`): `Finding.Repro string` (json/yaml `repro`, omitempty) và
   `Finding.DowngradedFrom string` (`downgraded_from`, omitempty; không nhận từ đầu vào: `ParseSubmission` xoá như `ID`). Hàm thuần
   `ValidRepro(repro, root string) bool` theo yêu cầu (1): có lệnh trong cặp backtick, hoặc tham chiếu `đường-dẫn:dòng` (a hoặc a-b) tới tệp
   có thật (tương đối: dưới root, không `..`; tuyệt đối: tồn tại) với dòng trong phạm vi (dùng lại phép đếm dòng đang có). Trong
   `ParseSubmission`, sau kiểm hợp lệ: kind bug/risk + mức critical/high + không ValidRepro → `DowngradedFrom = Severity`, `Severity = medium`.
   `Blocking()` KHÔNG đổi (mức đã lưu là nguồn duy nhất: reviewStalled, reviewRoundsUsed, fastpath, CLI đếm đều nhất quán; vòng cũ giữ
   nguyên). `RenderReview` thêm dòng "Tái hiện" và "Hạ mức: high → medium (thiếu bằng chứng tái hiện)". `SubmitReview` (ops.go) và
   `bint task review` (cli/task.go) nêu số phát hiện bị hạ (JSON thêm `downgraded`); lời lỗi giải mã liệt kê thêm `repro`.
   `internal/workflow/gate.go`: `blockingFindingsBlock` và vòng lặp `previousBlocking` trong `reviewPrompt` in kèm repro; đoạn "Nhiệm vụ…"
   của `reviewPrompt` thay câu mức độ bằng luật mới (high/critical chỉ khi có repro; bỏ sót chỉ dựa lập luận → medium; kiểm chứng/cổng đỏ →
   repro là `<log>:<dòng>`); dòng "Kiểm chứng cuối KHÔNG QUA … ghi phát hiện high" nhắc kèm repro.
   Skill `internal/skills/core/b-review/SKILL.md`: Bước 3 mục 0, đoạn "Mức độ", ví dụ JSON/YAML ở Bước 4 thêm `repro`; sửa
   `TestCoreSkillBReviewFindsOmissions` (`internal/skills/skills_test.go`). Xuất bản bằng binary mới: `go build -o /tmp/bint-new ./cmd/bint`
   rồi `/tmp/bint-new sync` (cập nhật `.claude/skills/b-review`, `.agents/skills/b-review`, `.bint/skills/core.lock`); golden TUI
   `system_skill_detail_*` sinh lại nếu đổi.
2. **Ngân sách diff** (`internal/workflow/budget.go` mới): `budgetGate(ctx, rs)` gọi trong `afterStep` implement ngay sau `goalGate` (trước
   `refreshLastImplement`/`sliceGate`). `diffLines`: `git --literal-pathspecs diff --numstat <from> <cur> --` theo lô (`PathBatches`), cộng thêm +
   xoá, dòng nhị phân (`-`) tính 0. `from` = `State.SliceGreen.Tree` khi có lát đang chạy, ngược lại `State.BaseTree`; tệp = `ScopeFiles` trừ
   `GoalTest.Paths()`; tác vụ không `TracksTouched` hoặc thiếu `from` → bỏ qua kèm ghi chú. Kết quả vào `State.BudgetChecks []task.Verification`
   (thêm `Detail string json:"detail,omitempty"` vào `Verification`; Command "diff ≤ N dòng", RunID, Reason rỗng, Log `budget-N.log` liệt kê tệp
   và số dòng); `LastBudgetCheck()` trong task. `redVerification` (gate.go) xét thêm lần ngân sách cuối đỏ; `verifyRedBlock` và đoạn "Kiểm chứng
   gần nhất" của `reviewPrompt` có thông điệp riêng ("thu gọn về bản nhỏ nhất đạt mục tiêu, không xoá test"). `sliceGate`: lần ngân sách của
   lát đỏ → `discardSlice` với Detail làm lý do (`sliceVerdict` thêm tham số lý do ngân sách).
3. **Cổng hiệu năng** (cùng `budget.go`): `perfBaseline` gọi ở `beginStep` nhánh `KindImplement` khi bật, `VerifyConfirmed`,
   `State.PerfBaseline == nil` và chưa bước implement nào kết thúc (`LastImplement == nil`, không có `Verifications` mang RunID); khác thì ghi chú
   "không có mốc". `perfRun` chạy lệnh 3 lần qua `eval.RunShell` dưới `r.lockStep(ctx, rs, "perf", LockExclusive)` (env và thư mục tạm như
   `goalGate`), timeout `verify_timeout_minutes`, log `perf-N.log`; thời gian = nhỏ nhất. Mốc lưu `State.PerfBaseline *task.Verification` (thêm
   `Seconds float64 json:"seconds,omitempty"`). Lần đo sau mỗi b-feat là một mục `State.BudgetChecks` (Detail kiểu "perf 0,62 s so với mốc
   0,01 s = 62× > 1,5×"). Hằng nội bộ `perfRuns = 3`, `perfNoiseFloor = 100ms`.
4. **Cấu hình** (`internal/config`): `Workflow.MaxDiffLines int yaml:"max_diff_lines"`; `Workflow.Perf WorkflowPerf yaml:"perf"` gồm
   `Command string yaml:"command"`, `MaxSlowdown float64 yaml:"max_slowdown"`; `Default()` 0 / "" / 1.5; `Validate()`; dòng mẫu trong
   `template.go` không có `%` trần [k:01M746NPB0Y4D5S2D47EFTJ8T4] [k:01M7487MQV2FFZPHJDWEFVZC7Z]. `docs/workflows.md`: hai cổng, luật repro, cách bật.

## Cỡ kế hoạch
Kết luận: lớn
- `internal/task/review.go` — Repro, DowngradedFrom, ValidRepro, hạ mức trong ParseSubmission, RenderReview
- `internal/task/task.go` — Verification.Detail/Seconds, State.BudgetChecks, State.PerfBaseline, LastBudgetCheck
- `internal/task/task_test.go` — test ValidRepro và hạ mức
- `internal/workflow/ops.go` — SubmitReview đếm phát hiện bị hạ
- `internal/cli/task.go` — đầu ra bint task review nêu phát hiện bị hạ
- `internal/workflow/gate.go` — reviewPrompt, blockingFindingsBlock, redVerification, verifyRedBlock
- `internal/workflow/budget.go` — budgetGate, diffLines, perfBaseline, perfRun
- `internal/workflow/budget_test.go` — test đơn vị diffLines, quyết định perf
- `internal/workflow/budget_goal_test.go` — kiểm tra mục tiêu TestGateEvidenceAndBudget (b-goal viết)
- `internal/workflow/run.go` — móc beginStep/afterStep
- `internal/workflow/slice.go` — sliceVerdict nhận lý do ngân sách
- `internal/config/config.go` — MaxDiffLines, WorkflowPerf, Default, Validate
- `internal/config/template.go` — dòng mẫu
- `internal/config/config_test.go` — mặc định/Validate
- `internal/skills/core/b-review/SKILL.md` — luật repro
- `internal/skills/skills_test.go` — cụm từ skill
- `.claude/skills/b-review/SKILL.md` — bản xuất bản
- `.agents/skills/b-review/SKILL.md` — bản xuất bản
- `.bint/skills/core.lock` — băm mới
- `docs/workflows.md` — tài liệu

## Bề mặt bị ảnh hưởng
- Review chặn cần bằng chứng tái hiện (Finding.repro, hạ mức khi ghi vòng, review.json/review.md, đầu ra bint task review, đề bài review và vòng sửa trong gate.go, skill b-review ba bản + core.lock) — AC-1, AC-2, AC-6
- Ngân sách diff mỗi lát/bước b-feat (workflow.max_diff_lines, đo numstat trừ tệp đóng băng, State.BudgetChecks, redVerification, bỏ lát, docs) — AC-3, AC-5
- Cổng hồi quy hiệu năng (workflow.perf.*, mốc trước b-feat đầu, đo sau mỗi b-feat, perf-N.log, redVerification, bỏ lát, docs) — AC-4, AC-5
- Cổng done/finalize và `bint task verify` của người: Ngoài phạm vi — không đòi hai cổng mới; `--accept` vẫn hoàn tất được
- TUI, `bint task show`: Ngoài phạm vi — kết quả chỉ ở state.json, log, kết luận lượt
- Chạy lệnh repro để xác minh: Ngoài phạm vi — lệnh agent đề xuất không được chạy khi chưa có người xác nhận

## Giả định thay người dùng
- **Hai cổng mặc định tắt** (`max_diff_lines: 0`, `perf.command: ""`): bật làm đổi hành vi của mọi tác vụ (kế hoạch lớn hợp lệ bị đỏ), nên để
  người bật trong `.bint/config.yml` (dự án, benchmark). `max_slowdown` mặc định 1.5 (thời gian thực dao động ~20–30 %; hồi quy benchmark là 16×).
- **Đơn vị diff là dòng thêm + xoá**, tệp nhị phân không tính; không chia lát thì đo **cộng dồn** so với mốc tác vụ (đo riêng từng bước làm vòng sửa
  luôn "qua" dù tổng diff vẫn lớn).
- **Thời gian = nhỏ nhất của 3 lần**, sàn nhiễu 0,1 s (hằng nội bộ, không thêm khoá): bớt báo nhầm với lệnh rất nhanh.
- **Mốc đo trên cây ngay trước b-feat đầu** (sau b-goal chỉ thêm test), không dựng worktree của commit gốc: rẻ, không cần cài đặt lại; thiếu mốc
  (tác vụ cũ) thì bỏ qua cổng, không đoán.
- **Hạ mức thay vì từ chối** lần ghi review: từ chối có thể làm bước review lỗi và tốn thêm lượt; hạ mức đảo ngược được (mức gốc vẫn lưu, người đọc
  `review.md` thấy). Hạ về `medium` (mức không chặn cao nhất).
- repro chỉ kiểm hình thức (lệnh trong backtick hoặc tệp:dòng có thật); Bint không chạy lệnh đó.

## Giả định và mâu thuẫn
- Yêu cầu nói sửa "cách tính phát hiện nghiêm trọng" trong gate.go, nhưng `Finding.Blocking()` nằm ở `internal/task/review.go` và được 15+ nơi
  dùng. Tính lại lúc quyết định sẽ làm `review.md` ghi high mà Runner không tính, và đổi số đếm của vòng cũ khi resume → chọn hạ mức lúc ghi vòng
  (một nguồn sự thật); gate.go đổi đề bài, khối phát hiện và `redVerification`.
- "Vượt thì coi như kiểm chứng không qua" ↔ cổng done: đưa hai cổng vào cổng done thì `--accept` mất tác dụng (không lối ra); chọn đi qua
  `redVerification` (vòng sửa; hết vòng thì người chọn).
- Reviewer chỉ đọc (readonly_tools) không chạy được `go test`: repro hợp lệ khi dẫn log kiểm chứng của Bint (`<log>:<dòng>`) hoặc tệp:dòng mã, nên
  luật làm được trong sandbox chỉ đọc.
- Skill core đổi thì `.bint/skills/core.lock` phải đổi theo (GĐ1 làm vậy): đã thêm vào `scope.allow`.

## Lựa chọn và lý do
- Review: *A (chọn)* hạ mức khi ghi; *B* đổi `Blocking()` lúc đọc (vòng cũ bị tính lại, `review.md` lệch); *C* từ chối lần ghi (bước review lỗi, tốn lượt).
- Hiệu năng: *A (chọn)* đo thời gian thực của lệnh người cấu hình, mốc trước b-feat đầu; *B* phân tích số trong đầu ra (`ns/op`) — gắn với công cụ
  cụ thể; *C* mốc trên worktree commit gốc — đắt, cần setup.
- Lưu kết quả: `State.BudgetChecks []Verification` dùng lại kiểu sẵn có (log, RunID, Reason) để `redVerification`/`verifyRedBlock` xử lý cùng khuôn
  với `GoalChecks`, thay vì kiểu mới.

## Vùng ảnh hưởng
- `task`: `Finding`, `ParseSubmission`, `RenderReview`, `Verification`, `State` (trường mới omitempty; state.json/review.json cũ đọc được).
- `workflow`: `afterStep`/`beginStep` (`session.go` gọi chung), `redVerification` (nơi gọi: `reviewDecision`, `continuationExtra`, đề bài),
  `sliceGate`, `SubmitReview`; `cli`: `bint task review`.
- `config`: khoá mới; cấu hình cũ không có khoá vẫn nạp (mặc định tắt).
- Người dùng: reviewer phải ghi repro để chặn; dự án bật cổng thấy kết quả đỏ mới và thêm 3 lần chạy lệnh đo cho mốc, 3 lần mỗi b-feat.

## Rủi ro và rollback
- *Reviewer bịa repro* để giữ high: chỉ kiểm hình thức (Ngoài phạm vi); tệp:dòng phải có thật.
- *Hạ nhầm lỗi thật* (reviewer quên repro): mức gốc lưu và hiện trong `review.md`; skill và đề bài nhắc rõ.
- *Cổng hiệu năng báo nhầm do máy bận*: min của 3 lần + sàn 0,1 s; tắt bằng `perf.command: ""`.
- *Ngân sách diff đỏ mãi ở kế hoạch lớn không chia lát*: hết vòng → `--accept`; tăng/tắt `max_diff_lines`.
- *Đếm hội tụ đổi*: vòng mới ít high hơn vòng cũ (do hạ mức) → coi là hội tụ, tối đa thêm vòng sửa trong trần sẵn có.
- *Windows*: không syscall mới; `GOOS=windows go vet ./...`, staticcheck [k:01M746NASVKV9XMRHA4QNHJ3R8]; script test dùng `/` [k:01M7CX9V792957T6MGK6QWEJ7F];
  khoá đo dùng `lockStep` (fslock) [k:01M7CQSPARNSRJG3NCAV4Y0V3K].
- Rollback: revert commit; trường JSON mới bị bản cũ bỏ qua; khoá cấu hình mới làm bản cũ lỗi `KnownFields` nếu đã ghi vào config → xoá khoá.

## Chiến lược kiểm thử
- Kiểm tra mục tiêu `TestGateEvidenceAndBudget` (`internal/workflow/budget_goal_test.go`, b-goal viết từ requirement.md) phủ AC-1, AC-3, AC-4, AC-5.
- Đơn vị: `ValidRepro` (backtick, tệp:dòng có/không thật, dòng ngoài phạm vi, tuyệt đối, chỉ lập luận); hạ mức (suggestion high không hạ, medium không
  đổi, `downgraded_from` từ đầu vào bị bỏ); `diffLines` (nhị phân, trừ tệp đóng băng); quyết định perf (sàn nhiễu, tỷ lệ, thoát ≠ 0); `sliceVerdict`
  với lý do ngân sách; `TestCoreSkillBReviewFindsOmissions` cập nhật (AC-2); config Default/Validate/`TestRenderMatchesDefaults`.
- Hồi quy: test hiện có của review (`task_test.go`, `findings_test.go`, test vòng sửa/hội tụ trong `run_test.go`) — reviewer giả gửi high không repro
  thì thêm repro vào dữ liệu giả (giữ ý nghĩa test), không nới luật. Test b-do dùng plan nhỏ hợp lệ [k:01M7MKY65CS6P4Q62W7R4Y7JD2].
- Cuối: `go test -timeout 30m ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, staticcheck.
- Chưa kiểm được: hiệu quả thật trên benchmark (reviewer thật có ghi repro không; số vòng review giảm) — để nghiệm thu sau GĐ4.

## Checklist nhất quán
- AC-1 ← TSK-1; AC-2 ← TSK-2; AC-3 ← TSK-3; AC-4 ← TSK-4; AC-5 ← TSK-3, TSK-4, TSK-5; AC-6 ← TSK-6. Mỗi TSK có "xong khi" là test hoặc lệnh cụ thể.
- `--allow internal/**, docs/**, .claude/skills/**, .agents/skills/**, .bint/skills/core.lock` phủ mọi tệp ở `## Cỡ kế hoạch`; không tệp nào chạm
  `migrations/**`, `.github/workflows/**`.
