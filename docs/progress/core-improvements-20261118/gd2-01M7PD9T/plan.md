# Kế hoạch

## Hướng triển khai
Thêm một lớp "lát" mỏng quanh vòng `Run` hiện có, không đổi flow b-do (không thêm bước, không thêm trạng thái tác vụ): trong pha
`in_progress`, mỗi lát là một lần chạy bước implement (b-feat) với đề bài/timeout riêng; cổng sau bước implement có thêm `sliceGate`
quyết định chốt / bỏ / sang lát kế trước khi cho chuyển `in_review`. Toàn bộ logic mới nằm ở `internal/workflow/slice.go`; các chỗ chạm vào
mã cũ chỉ là móc gọi.

1. **Dữ liệu** (`internal/task/task.go`): `State.Slices *SliceRun` (json `slices,omitempty`) gồm `Items []Slice`, `Current int` (1-based),
   `Green SlicePoint` (`Tree`, `At`, `Red []string`, `Measured bool`). `Slice`: `N`, `Text` (nguyên văn dòng nhóm), `ACs []int`, `Status`
   (`pending|active|accepted|discarded`), `Reason`, `Red []string`, `Tree`, `Backup` (ref sao lưu), `RunID`. Hằng trạng thái trong `task`.
   `Result.Slices []task.Slice` (json `slices,omitempty`) trong `run.go`.
2. **Tách lát** (`internal/task/artifactlint.go`): `PlanSlices(plan) []PlanSlice` — bullet cấp ngoài cùng dưới `## Bề mặt bị ảnh hưởng`
   (cùng luật với `PlanSurfaces`) có ít nhất một `AC-N` (regex `\bAC-(\d+)\b`); giữ nguyên văn dòng (bỏ dấu bullet).
3. **Bật lát** (`slice.go: (r *Runner) initSlices(ctx, rs)`): gọi trong `beginStep` nhánh `KindImplement`, sau `preflightFeat`, chỉ khi
   `State.Slices == nil`, `!rs.session`, `State.GoalTest != nil`, `rs.planSizeGated()`, không `Fast`, `SliceTimeoutMinutes > 0`,
   `len(planSizeReasons(t)) > 0` và `len(PlanSlices) >= 2`. Điểm xanh đầu: `WorktreeSnapshot` → `pinTree` vào `sliceGreenRef(id)`; số đo lấy
   từ log cổng mốc (`LastGoalCheck()` có `Reason == goalReasonBaseline`). Lát 1 `active`.
4. **Đề bài + timeout**: `prompt()` nhánh `KindImplement` thêm `sliceBlock(t)` khi có lát `active` (lát k/N, nguyên văn, lát đã chốt, lát sau).
   `runStep`: `Timeout = min(TimeoutMinutes, SliceTimeoutMinutes)` khi có lát `active`. Đề bài b-req của b-do gốc (`KindPlan`, `planSizeGated`)
   thêm `sliceGuideBlock()`: thứ tự nhóm = thứ tự lát, nhóm đầu là bản nhỏ nhất đạt mục tiêu, gộp phần tài liệu/cấu hình vào lát cần nó hoặc
   đặt cuối, nhóm ngoài phạm vi không ghi AC. Lý do chặn của `planSizeGate` nối "Bint sẽ chạy N lát: L1 …; L2 …" khi điều kiện chia (trừ phần
   cần GoalTest) thoả.
5. **Đo** (`slice.go`): `failSet(log string, exit int, timedOut bool) (names []string, measured bool)` theo yêu cầu mục 4; `oldTestsGreen(v,
   goalNames)`; `goalNames` = `goTestFuncs` của tệp đóng băng (dùng lại hàm GĐ1) ∪ tên cấp đầu trong tập ô đỏ của cổng mục tiêu cùng lát.
6. **Chốt/bỏ** (`(r *Runner) sliceGate(ctx, rs) (cont bool)`): móc trong `afterStep` nhánh `KindImplement` ngay sau `goalGate`/
   `refreshLastImplement`, chỉ khi có lát `active`. Chốt → `WorktreeSnapshot`+`pinTree(sliceGreenRef)`, cập nhật `Green`, lát `accepted`.
   Bỏ → `discardSlice`: `pinTree(cur, sliceDiscardRef(id,N))` rồi `restoreTo(ctx, root, t, greenTree)` dưới `AcquireLock(..., LockExclusive)`
   [k:01M7CQSPARNSRJG3NCAV4Y0V3K], lát `discarded` + lý do. Sau đó: còn lát `pending` → lát kế `active`, `AwaitingCheck=false`, giữ
   `in_progress`, trả true (vòng `Run` mở b-feat mới); lát 1 bị bỏ → `block` với kết luận; hết lát → rơi về đường cũ (`transition in_review`).
   Vì cây đã đổi sau khi bỏ lát, lần kiểm chứng/cổng mục tiêu cuối không còn khớp cây: finalize/review dùng cơ chế `reverifyIfStale` sẵn có.
7. **Quay về** (`green.go`): tách thân ghi của `RestoreGreen` thành `restoreTo(ctx, root, t, tree) (*RestoreResult, error)` (tính tệp thuộc
   tác vụ, ghim `beforeRestoreRef`/ref truyền vào, xoá/khôi phục) — `RestoreGreen` (người) và `discardSlice` (Runner) dùng chung. Điểm xanh
   chung `lastGreenPoint(t)`: mới hơn trong `greenVerification` và `State.Slices.Green` (so `At`); `greenInfo`, `RestoreGreen` dùng nó.
   `deleteTaskRefs` (`touched.go`) xoá thêm `slice-green` và mọi `slice-*-discarded` (`for-each-ref refs/bint/tasks/<id>/`).
8. **Giao** (`slice.go: deliverGreen`): (a) `beginStep` chặn hết ngân sách khi có lát → lý do nối danh sách lát; (b) `endStep` lát kết thúc
   `TimedOut` → bỏ lát (lý do "quá trần N phút") và đi tiếp như mục 6 thay vì `failed`; lỗi agent khác / vượt chi phí → bỏ lát dở + `block`
   (không `failed`) kèm danh sách; người huỷ (`ctx.Err()`) giữ hành vi cũ (interrupted, không quay về). Mọi lượt có `State.Slices`:
   `finish` gắn `Result.Slices` và ghi chú tóm tắt (`sliceNote`).
9. **Vòng lặp**: `loopGuard` cộng `len(State.Slices.Items)` để vòng `Run` không cạn trần trước khi hết lát.
10. **Cấu hình**: `Workflow.SliceTimeoutMinutes int yaml:"slice_timeout_minutes"`, `Default()` = 30, `Validate()` từ chối âm, dòng có chú thích
    trong `template.go` (không dùng `%` trần) [k:01M746NPB0Y4D5S2D47EFTJ8T4] [k:01M7487MQV2FFZPHJDWEFVZC7Z].

## Cỡ kế hoạch
Kết luận: lớn
- `internal/task/task.go` — State.Slices, kiểu Slice/SliceRun/SlicePoint
- `internal/task/artifactlint.go` — PlanSlices
- `internal/task/artifactlint_test.go` — test PlanSlices
- `internal/workflow/slice.go` — initSlices, sliceBlock, sliceGuideBlock, failSet, sliceGate, discardSlice, deliverGreen, sliceNote
- `internal/workflow/slice_test.go` — test đơn vị failSet, điều kiện chốt
- `internal/workflow/slice_goal_test.go` — kiểm tra mục tiêu TestSliceRatchet (b-goal viết)
- `internal/workflow/run.go` — móc beginStep/runStep/endStep/afterStep/prompt/loopGuard/finish, Result.Slices
- `internal/workflow/plansize.go` — lý do chặn liệt kê lát
- `internal/workflow/green.go` — restoreTo, lastGreenPoint, ref lát
- `internal/workflow/touched.go` — deleteTaskRefs xoá ref lát
- `internal/config/config.go` — SliceTimeoutMinutes, Default, Validate
- `internal/config/template.go` — dòng mẫu slice_timeout_minutes
- `internal/config/config_test.go` — mặc định/Validate khoá mới
- `docs/workflows.md` — lát mỏng, điều kiện chốt, lấy lại lát bỏ

## Bề mặt bị ảnh hưởng
- Chia lát và chạy từng lát (plansize.go lý do chặn, PlanSlices, State.Slices, đề bài b-req/b-feat, timeout bước) — AC-1
- Đo và chốt (log goal-N.log/verify, tập ô đỏ, ref slice-green, State.Slices.Green) — AC-2
- Quay về điểm xanh tự động (restoreTo dùng chung với restore-green, ref slice-N-discarded, Touched/worktree, khoá cây) — AC-3
- Kết luận và giao (endStep quá giờ/lỗi, beginStep ngân sách, Result.Slices, LastGreen/GreenReport, restore-green của người, resume) — AC-4
- Cấu hình và tài liệu (workflow.slice_timeout_minutes, mẫu Render, docs/workflows.md) — AC-5
- Driver session `bint run next` (session.go): Ngoài phạm vi — vẫn một b-feat; ghi chú khi gặp tác vụ đã chia lát
- Vòng sửa sau review: Ngoài phạm vi — không chốt một chiều, giữ luật reviewStalled/ngân sách
- TUI, `bint task show`: Ngoài phạm vi — chỉ JSON kết quả/state.json

## Giả định thay người dùng
- **Kế hoạch lớn vẫn dừng chờ người xem** (như hiện nay, docs/progress README mục 4.5 cũng dự tính "duyệt kế hoạch rồi resume"); "không chỉ
  dừng báo" được hiểu là lời dừng nay kèm danh sách lát và resume chạy theo lát thay vì một khối. Muốn không dừng: trả lời "có" ở câu hỏi
  tương tác (`Confirm`) như hiện nay. Lý do: cổng cỡ kế hoạch còn là chỗ người thấy đường dẫn được bảo vệ và kế hoạch phình.
- **Trần thời gian lát mặc định 30 phút** (benchmark: một khối 20–47 phút); đổi bằng `workflow.slice_timeout_minutes`; 0 tắt hẳn lát mỏng.
  Trần chỉ tính thời gian agent b-feat, không tính kiểm chứng.
- **"Giảm" là tập con thực sự** của ô đỏ ở điểm xanh (không chỉ đếm số): lát sửa 2 ô mà làm đỏ 1 ô đã xanh bị bỏ — đúng nghĩa "chốt một chiều".
  Lát chỉ sửa tài liệu/cấu hình khi cổng còn đỏ không làm giảm ô đỏ nên bị bỏ (đúng lời người dùng "phải giảm"); b-req được nhắc gộp phần
  đó vào lát cần nó. Lát bị bỏ **lấy lại được**: `git restore --source=refs/bint/tasks/<id>/slice-<N>-discarded -- <tệp>`.
- **Điểm xanh không đo được** (thường gặp: test hộp đen của b-goal chưa biên dịch trước khi có mã) coi là tệ nhất: lát nào làm log đo được là tiến.
- **Test cũ đỏ sẵn từ trước** (ngoài test mục tiêu) làm lát 1 bị bỏ và tác vụ blocked với lý do nêu tên test: chọn dừng hỏi người thay vì
  đoán test nào "vốn đã đỏ" (không có lần kiểm chứng mốc trước lát 1).
- **Lát 1 bị bỏ thì dừng** (không chạy lát sau): lát sau thường dựa trên lát 1, chạy tiếp chỉ tốn tiền. Lát sau bị bỏ thì chạy lát kế.
- **Quay về chỉ đụng tệp của tác vụ** (luật của `restore-green`), luôn ghim cây trước khi ghi: không có ghi đè không đường lui.
- **Agent lỗi trong lát** (không phải quá giờ) dừng blocked thay vì `failed`, cây ở điểm xanh: người resume là chạy lại lát đó.
- Mỗi lát là phiên b-feat mới (không nối tiếp phiên lát trước): lát trước có thể đã bị bỏ, phiên cũ mang ngữ cảnh sai.

## Giả định và mâu thuẫn
- Mâu thuẫn "verify xanh thì ghim" ↔ lệnh kiểm chứng `go test ./...` chứa cả test mục tiêu đóng băng (còn đỏ cho tới lát cuối): kiểm chứng
  đầy đủ không thể xanh giữa chừng. Giải: điểm xanh của lát = "test cũ xanh + ô đỏ mục tiêu giảm", ghim ở ref riêng `slice-green`; ref
  `green` cũ giữ nghĩa "kiểm chứng xanh hẳn" (không đổi hành vi `pinGreen`).
- `go test` không `-v` không in ô xanh: chỉ dùng tập ô đỏ (đủ để biết giảm và không ô nào chuyển đỏ), không đổi lệnh cổng mục tiêu của GĐ1.
- Dựa vào: b-req ghi `AC-N` trong từng dòng nhóm bề mặt (skill b-req đã yêu cầu "mỗi nhóm gắn một AC-N"); cổng mốc sau b-goal đã chạy
  (`afterGoal`); `RestoreGreen` hiện chỉ cho tác vụ đã dừng — Runner gọi `restoreTo` trực tiếp khi đang giữ quyền điều phối.
- Không đụng skill (`internal/skills/core`, `.claude/skills`, `.agents/skills`): luật thứ tự nhóm đưa vào đề bài do Bint dựng, đúng hướng GĐ4
  (chuyển luật vào mã) và tránh phải sinh lại `.bint/skills/core.lock` (ngoài phạm vi ghi) và golden TUI.

## Lựa chọn và lý do
- *A (chọn)*: lát là các lần chạy lặp của bước implement sẵn có, quyết định trong `sliceGate`. Không đổi flow/validate/golden flow, resume
  dùng lại `AwaitingCheck`/`stepCheck`.
- *B*: thêm kind bước `slice` mới vào flow b-do — phải sửa `flow.go`, `builtin.go`, `validate.go`, golden flow, session driver; phình diff.
- *C*: lập phiếu con cho mỗi lát — mỗi lát có b-req/b-review riêng, chi phí gấp nhiều lần, ngược mục tiêu tiết kiệm của benchmark.
- Đo bằng tập ô đỏ từ log thay vì thêm `-json`: không đổi lệnh của người, chạy được với script không phải Go (test hộp đen dùng script giả).

## Vùng ảnh hưởng
- Gói `workflow`: `Runner.Run` (vòng lặp, `loopGuard`), `beginStep`, `runStep`, `endStep`, `afterStep` (implement), `prompt`, `finish`,
  `planSizeGate`, `RestoreGreen`/`GreenReport` (nơi gọi: `internal/app` restore-green, TUI đọc `LastGreen`), `deleteTaskRefs` (land/done/cancel).
- Gói `task`: `State` thêm trường tuỳ chọn (state.json cũ đọc được, `omitempty`); `artifactlint` thêm hàm thuần.
- Gói `config`: khoá mới; `Decode` dùng `KnownFields(true)` nên cấu hình cũ không có khoá vẫn nạp (lấy mặc định).
- JSON kết quả `bint run --json`: thêm `slices` (tuỳ chọn) — người đọc cũ bỏ qua.
- Người dùng: kế hoạch lớn của b-do dưới `bint run` chạy thành nhiều b-feat ngắn; có thể thấy lát bị bỏ và tệp bị đưa về.

## Rủi ro và rollback
- *Bỏ nhầm việc tốt* (cổng mục tiêu không phủ nhóm đó): phát hiện qua lý do "không giảm" trong kết luận; lấy lại bằng ref `slice-N-discarded`;
  tắt toàn bộ bằng `workflow.slice_timeout_minutes: 0`.
- *Quay về xoá thay đổi của người trong cây chung*: chỉ đụng tệp thuộc Touched, ghim cây trước khi ghi, khoá độc quyền; test có ca tệp ngoài tác vụ.
- *Vòng `Run` cạn `loopGuard` hoặc lặp vô hạn*: cộng số lát vào trần; mỗi lát chỉ chạy một lần (trạng thái lát không quay về `pending`).
- *Lát quá giờ bị coi `failed` như cũ*: test ca (d). *Resume chạy lại lát đã chốt*: test ca (e) kiểm không chạy lại L1.
- *Kiểm chứng mỗi lát làm chậm* (gói workflow ~10–28 phút trên Windows): kiểm chứng dùng `VerifyScoped` (tier ảnh hưởng) như mọi bước implement;
  chấp nhận, ghi trong docs.
- *Windows*: không có syscall mới; chạy `GOOS=windows go vet ./...` [k:01M746NASVKV9XMRHA4QNHJ3R8]; đường dẫn ref/tệp dùng `/` cho git.
- Rollback mã: revert commit; bản cũ vẫn đọc được state.json có `State.Slices` (`json.Unmarshal` bỏ qua trường lạ); ref `slice-*` dọn bằng
  `git update-ref -d` hoặc tự xoá khi tác vụ kết thúc (bản mới).

## Chiến lược kiểm thử
- Kiểm tra mục tiêu `TestSliceRatchet` (`internal/workflow/slice_goal_test.go`, do b-goal viết từ requirement.md) phủ AC-1..AC-4.
- Đơn vị: `PlanSlices` (dòng có/không AC, bullet thụt, code fence); `failSet` (lá/cha, `[build failed]`, `panic:`, quá giờ, exit 0, exit≠0
  không FAIL); điều kiện chốt dạng bảng (không đo được→đo được, 0→0, tập con thực sự, bằng nhau, thêm ô mới, test cũ đỏ).
- Hồi quy: test b-do hiện có (cổng cỡ kế hoạch, `goalfreeze_test.go`, `green_test.go`, `flow_golden_test.go`) phải xanh — chú ý plan giả trong
  test cũ dùng kế hoạch NHỎ [k:01M7MKY65CS6P4Q62W7R4Y7JD2] nên không bị chia lát.
- Cấu hình: `TestRenderMatchesDefaults`, ca Validate âm.
- Cuối: `go test -timeout 30m ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, staticcheck.
- Chưa kiểm được: chạy thật với Claude/Codex trên một kế hoạch lớn thật (đo thời gian/chi phí so với benchmark) — để nghiệm thu GĐ sau.

## Checklist nhất quán
- AC-1 ← TSK-1; AC-2 ← TSK-2; AC-3 ← TSK-3; AC-4 ← TSK-4; AC-5 ← TSK-5; AC-6 ← TSK-6. Mỗi TSK có "xong khi" là test cụ thể.
- `--allow internal/**, docs/**` phủ mọi tệp ở `## Cỡ kế hoạch`; không tệp nào chạm `limits.protected_paths` (`migrations/**`, `.github/workflows/**`).
