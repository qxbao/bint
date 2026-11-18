# Tiến độ: cải tiến core Bint sau benchmark (2026-11-18)

Tài liệu bàn giao để chạy tiếp ở máy khác. Phiếu Bint (ticket, state, log) nằm trong thư mục trạng thái cục bộ
(`%LOCALAPPDATA%\bint\state\…`), **không đi theo git**: máy mới phải lập lại phiếu (mục 5). Artifact của các phiếu
đã chép vào thư mục này.

## 1. Bối cảnh

Phân tích 5 đợt benchmark (`docs/evals/bench-bint-vs-plain-tong-hop.md`, commit 3743e8d): Bint (B) không tốt hơn
không-Bint (A) vì cùng model nên cùng điểm mù, b-req phình kế hoạch, b-feat một khối 20–47 phút, review không hội tụ,
hồi quy hiệu năng không bị bắt; phép đo nhiễu (1 lượt/ô, người chấm lệch ±2 critical).

Bảy thay đổi core, chia thành phiếu cha + 4 giai đoạn (workflow `b-do`, `--plan --depth deep --shared`):

| Giai đoạn | ID cũ (máy này) | Nội dung | Trạng thái |
|---|---|---|---|
| Cha | `01M7NA1PJHQJR6SE3BVCH05BQC` | Giữ ngữ cảnh; phụ thuộc GĐ4 | created |
| GĐ1 | `01M7NA28NAXVSE14HNHACRZG97` | Kiểm tra mục tiêu độc lập + đóng băng (bước b-goal) | **blocked** (review không hội tụ, lỗi môi trường) — mã đã có trong commit này |
| GĐ2 | `01M7NA28Y3S58QJMXMP74S6BCD` | Lát mỏng + chốt một chiều (restore-green tự động) | created, chờ GĐ1 |
| GĐ3 | `01M7NA295W2XZE709EG75XSNTV` | Cổng hiệu năng, ngân sách diff, review chặn cần bằng chứng | created, chờ GĐ2 |
| GĐ4 | `01M7NA29D40S13ZN55F6F9HPQV` | b-req đo hiện trạng + chuyển luật skill vào mã | created, chờ GĐ1–3 |
| Env-fix | `01M7NWDTRN29QFP1AFAKGB87XS` | Sửa thư mục tạm của kiểm chứng trên Windows | **blocked**, worktree; mã **chưa** vào commit này — xem `envfix-01M7NWDT/envfix.patch` |

Chi phí đã tiêu: GĐ1 ≈ $14.

## 2. GĐ1 — đã làm gì (có trong commit này)

Artifact: `gd1-01M7NA28/` (requirement, plan, tasks, review).

- Bước mới **b-goal** giữa b-req và b-feat: phiên riêng (role planner; ưu tiên khác model với implementer) chỉ thấy
  `requirement.md`, viết test hộp đen cho mục `## Kiểm tra mục tiêu`.
- Bint ghi lại tệp test (`State.GoalTest`), thêm vào `scope.deny` của b-feat, từ chối diff chạm vào nó, chạy nó như
  **cổng riêng** có log `goal-*.log` (`BlockGoalFailed`).
- Bỏ việc b-feat tự viết test mục tiêu (`goalCheckBlock` trong `fastpath.go`, skill b-feat). `--fast` và phiếu không có
  mục Kiểm tra mục tiêu giữ hành vi cũ.
- Tệp mới: `internal/workflow/goalfreeze.go`, `goalfreeze_test.go`, skill `b-goal` (core + `.claude` + `.agents`).
  Sửa: `flow.go`, `builtin.go`, `validate.go`, `task.go`, `status.go`, `run.go`, `gate.go`, `fastpath.go`, `ops.go`
  (`withoutIntactGoal` trong `UnapprovedChanges`), `session.go`, `cli/task.go`, `harness/subagents.go`, skill b-do/b-feat,
  golden TUI, `docs/workflows.md`.

### Phát hiện review còn mở (GĐ1)

| # | Mức | Nội dung | Vị trí |
|---|---|---|---|
| 6 (=1) | high | Kiểm chứng cuối đỏ do 8 test hạ tầng Windows (TMPDIR dài/MSYS) — **ngoài diff**, phiếu env-fix sửa | `ops.go` verifyTempDir |
| 7 | medium (bug) | Phạm vi b-goal cần duyệt → resume chạy lại b-goal từ đầu thay vì đóng băng tệp đã duyệt | `goalfreeze.go:91-94` |
| 4 | medium | Không có lối thoát khi test đóng băng sai: `BlockGoalFailed` không forceable, không lệnh chạy lại b-goal | `task/status.go:153-158` |
| 3 | medium | Cổng mục tiêu dùng `{test_dirs}` → chạy cả gói workflow (~10–28 phút trên Windows) | `goalfreeze.go:188-194` |
| 8 | medium | Rào "b-goal không đọc plan" chỉ là lời dặn trong đề bài | `goalfreeze.go:273-287` |
| 9 | medium | `TestMilestoneTaskToDone` đỏ một lần vì khoá `state.json` trên Windows (race) | `tui/app/tasks_milestone_test.go:76-77` |
| 5 | low | `TestGoalFreezeBlackBox` thiếu vài khẳng định (lỗi Approve, ApprovedPaths) | `goalfreeze_test.go:214-304` |
| 2 | — | Đổi `.bint/config.yml` (bỏ `-count=1`) — chủ ý của người dùng, đã xử lý | |

Việc kế tiếp cho GĐ1: một vòng sửa (`--fix-round`) cho #7, #4, #3 (và nếu kịp #8, #9, #5), sau khi env-fix đã land
để kiểm chứng cuối có thể xanh.

## 3. Env-fix — sửa thư mục tạm kiểm chứng trên Windows

Artifact: `envfix-01M7NWDT/` (requirement, plan, tasks, review, **`envfix.patch`**).

- Vấn đề: `bint task verify` đặt `TMPDIR` dưới `<state>/tmp/verify-*` (đường rất sâu) → repo git lồng trong test eval/evolve
  vượt MAX_PATH ("Filename too long"); `sh.exe` đổi TMPDIR sang dạng `/c/...` làm `TestVerifyUsesPrivateTempDir` đỏ.
- Sửa: `verifyTempBase` — Windows dùng `%TEMP%\bv\<số>`, Unix giữ `<state>/tmp/verify-*`; test mới
  `TestVerifyChildProcessTempDir`, `TestHelperPrintTempDir`, helper `nativePath`; test cũ dùng `cygpath -w` trên Windows;
  ghi chú trong `docs/workflows.md`.
- Bằng chứng (chạy `bint task verify` bằng binary build từ worktree): mọi gói từng đỏ (`cli`, `eval`, `evolve`,
  `tui/actions`, `tui/app`) đều **qua**; chỉ `internal/workflow` bị cắt vì trần `verify_timeout_minutes: 30` (gói này mất
  ~28 phút). Chạy riêng 3 test TMPDIR của workflow: PASS. `go vet` Linux + `GOOS=windows`: OK.
- Review: phát hiện high duy nhất ("verify chạy bằng binary cũ") đã được giải quyết bằng lần chạy trên; còn 1 low (không GC
  `%TEMP%\bv\*` mồ côi).
- `git apply --check envfix.patch` trên cây có GĐ1: **áp được sạch**.

## 4. Còn thiếu / việc người dùng phải làm

1. **Đưa env-fix vào mã**:
   - Máy này: `bint run resume 01M7NWDTRN29QFP1AFAKGB87XS --accept` (land worktree), hoặc
   - Máy khác: `git apply docs/progress/core-improvements-20261118/envfix-01M7NWDT/envfix.patch`, chạy
     `go test ./internal/workflow -run 'TestVerify.*TempDir' && GOOS=windows go vet ./internal/workflow`, rồi commit.
2. **Cài lại bint** từ mã mới (`go build -o "$(command -v bint)" ./cmd/bint`), nếu không kiểm chứng vẫn dùng thư mục tạm cũ.
3. **Tăng `workflow.verify_timeout_minutes`** trong `.bint/config.yml` lên ~60 (gói workflow ~28 phút trên Windows).
4. **GĐ1**: vòng sửa cho các phát hiện medium ở mục 2 (`bint run resume <id> --fix-round`), rồi done.
5. **GĐ2 → GĐ3 → GĐ4** lần lượt (`bint task ready --parent <cha>`, `bint run b-do --task <id> --json`). Mỗi lần kế hoạch
   lớn bị cổng cỡ kế hoạch chặn: duyệt kế hoạch rồi resume.
6. Máy này: `bint task cancel 01M7GD9M9BARPG07T1N4QE6A2R --discard` (lần thử cũ của GĐ2 step-continuation, đã làm lại trên main;
   không cần merge).

Lưu ý: commit này đưa thay đổi GĐ1 vào HEAD. Trên **máy này**, phiếu GĐ1 (`shared`) vẫn tham chiếu commit gốc cũ;
nếu review/diff của Bint thấy lạ sau commit, coi mã GĐ1 là đã có và lập phiếu sửa riêng cho các phát hiện còn mở.

## 5. Lập lại phiếu trên máy mới

Cấu hình liên quan đã có trong `.bint/config.yml` (`eval.test_command: go test -timeout 30m ./...`,
`workflow.verify.affected_command: go test -timeout 30m {test_dirs}`, `max_review_rounds: 2`, `timeout_minutes: 90`).

```sh
set -f   # tránh shell mở rộng glob 'internal/**'
S="--clear-scope --allow internal/** --allow docs/** --allow .claude/skills/** --allow .agents/skills/** --allow .bint/config.yml"
id() { python -c "import sys,json;print(json.load(sys.stdin)['ticket']['ID'])"; }

P=$(bint task new "Cải tiến core Bint theo benchmark (docs/evals/bench-bint-vs-plain-tong-hop.md); xem docs/progress/core-improvements-20261118/README.md" --workflow b-do --no-plan --json | id)

# GĐ1 đã có mã: lập phiếu sửa các phát hiện còn mở thay vì làm lại
C1=$(bint task new "Giai đoạn 1 (sửa tiếp): bước b-goal đã có (internal/workflow/goalfreeze.go). Sửa các phát hiện review còn mở ở docs/progress/core-improvements-20261118/README.md mục 2: #7 resume sau duyệt phạm vi b-goal không chạy lại b-goal; #4 lối thoát của người khi test đóng băng sai; #3 cổng mục tiêu chỉ chạy test mục tiêu (không cả gói); #8 rào thực thi b-goal không đọc plan/tasks; #9 race khoá state.json trong TestMilestoneTaskToDone; #5 bổ sung khẳng định TestGoalFreezeBlackBox." --parent $P --workflow b-do --plan --shared --json | id)
bint task set $C1 $S
```

GĐ2–4: dùng nguyên văn mô tả dưới đây (giữ `--parent $P --depends-on …`, `--workflow b-do --plan --depth deep --shared`,
rồi `bint task set <id> $S`), cuối cùng `bint task set $P --depends-on $C4`.

- **GĐ2**: "Giai đoạn 2: b-do chạy theo lát mỏng và chốt một chiều. Kế hoạch lớn (internal/workflow/plansize.go) không chỉ dừng báo
  mà Runner tự chia: lát đầu là bản nhỏ nhất đạt mục tiêu, verify xanh thì ghim điểm xanh (internal/workflow/green.go); mỗi nhóm bề
  mặt sau là một lượt b-feat riêng có trần thời gian. Sau mỗi lát Runner so kết quả cổng kiểm tra mục tiêu của giai đoạn 1 (số ô/test
  đỏ phải giảm, test cũ vẫn xanh); không đạt thì tự quay về điểm xanh (restore-green) và bỏ lát đó, ghi rõ trong kết luận, thay vì chỉ
  dừng khi reviewStalled. Hết giờ/ngân sách thì giao điểm xanh cuối kèm danh sách lát chưa làm." — `--depends-on $C1`
- **GĐ3**: "Giai đoạn 3: Cổng hồi quy hiệu năng, ngân sách diff mỗi lát, và review chặn cần bằng chứng tái hiện. (a) Khoá cấu hình mới:
  lệnh đo hiệu năng tuỳ chọn so với gốc (tỷ lệ chậm tối đa) và trần kích thước diff mỗi lát/bước b-feat; vượt thì coi như kiểm chứng
  không qua. Theo quy ước thêm khoá cấu hình: struct, Default(), Validate(), mẫu Render (internal/config/template.go). (b) Phát hiện
  review chỉ được ở mức chặn (high/critical) khi kèm lệnh hoặc bằng chứng tái hiện (tệp:dòng dữ liệu còn sai, lệnh chạy ra lỗi); 'bỏ sót
  bề mặt' chỉ dựa trên lập luận hạ xuống mức không chặn. Sửa internal/workflow/gate.go (đề bài review, cách tính phát hiện nghiêm
  trọng) và skill b-review." — `--depends-on $C2`
- **GĐ4**: "Giai đoạn 4: b-req đo hiện trạng và chuyển luật từ skill vào mã. (a) b-req đo hiện trạng trước khi lập kế hoạch (ví dụ du
  thư mục trạng thái, đếm nơi lưu) và tiêu chí chấp nhận bắt buộc có ít nhất một số đo mục tiêu người dùng; kiểm trong hợp đồng
  artifact (bint task lint). (b) Rà skill b-do (31,6 KB) và b-req (17,9 KB): luật nào kiểm được bằng mã thì chuyển thành cổng trong
  Runner/lint, rồi cắt khỏi skill (giữ đồng bộ internal/skills/core với .claude/skills và .agents/skills, sinh lại golden nếu có)." —
  `--depends-on $C1,$C2,$C3`

Sau khi lập: `bint task show <id>` kiểm lệnh kiểm chứng là `go test -timeout 30m ./...` và đã xác nhận (khớp `eval.test_command`).

## 6. Ràng buộc khi chạy bằng agent

Agent (phiên Claude/Codex) không chạy `bint task approve`, `--force`, `--accept`, `--fix-round`, `--discard`, `restore-green --apply`,
`bint knowledge approve` — Bint từ chối trong phiên agent; người dùng chạy (trong Claude Code: tiền tố `! `).
