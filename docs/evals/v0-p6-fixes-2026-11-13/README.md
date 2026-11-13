# V0 — Sửa mã P6 và hồi quy tự động — 2026-11-13

- Commit kiểm tra: `f1467a0` (bản sửa của vòng này, trên `7925e7d`). Môi trường: Linux amd64, Go 1.25.1,
  git 2.43.0, người dùng không phải root; Claude Code driver session (planner/reviewer `opus`, implementer `sonnet`).
- Ngân sách được duyệt: 0 token đo (vòng không chạy eval/evolve). Chi phí phiên agent theo `bint run status`:
  **$10,06** (phiếu 1 $4,13; phiếu 2 $4,71; phiếu 3 $1,22).
- Tác vụ validation/test do agent duyệt: không áp dụng (vòng không dùng tập tác vụ).

## Cách chạy

Ba phiếu `b-do`, `--driver session --shared`, phạm vi `internal/evolve/**` (phiếu 1 thêm `internal/fslock/**`,
`internal/workflow/flock_*.go`, `internal/workflow/lock*.go`):

| Phiếu | Task ID | Nội dung | Bước | Chi phí |
|---|---|---|---|---|
| 1 | `01M79V8RFX3BB88E55XQBQ16C9` | `NT-P6-01..03` | b-req (deep) → b-feat → b-review | $4,13 |
| 2 | `01M79X3GFGV4AHP2MBVF2NEJM2` | `NT-P6-04..05` + phát hiện review phiếu 1 | b-req → b-feat → b-review | $4,71 |
| 3 | `01M79Z83VFMTJ6CM2AJ9PPH0HY` | phát hiện review phiếu 2 (có 1 medium) | b-feat → b-review | $1,22 |

Phiếu 2 bị Bint chặn ở Finalize vì `NT-WF-BASELINE` (dưới); kiểm chứng đã qua nên chủ dự án đóng bằng
`bint task status … done --force` ở terminal riêng. Phiếu 1 và 3 tự `done`.

Hiện trạng trước khi sửa: chạy bộ tái hiện `docs/evals/acceptance-p6-dff6570/` tại `d6327e4`, cả 5 probe FAIL
([evidence/probes-before.log](evidence/probes-before.log)); tại `1391936` không có bản sửa cũng 5/5 FAIL
([evidence/probes-head-1391936-without-fix.log](evidence/probes-head-1391936-without-fix.log)). Kiểm tra cuối chạy
trên cây làm việc chỉ có thay đổi của vòng này, tại `7925e7d` — đúng trạng thái được commit
([evidence/summary.txt](evidence/summary.txt)); một lần chạy trước trong worktree tạm tại `1391936` + tệp của vòng
này cũng qua toàn bộ.

## Kết quả

| Mã | Kết quả | Bằng chứng | Ghi chú |
|---|---|---|---|
| `NT-P6-01` | đạt | `TestAcquireLockExclusiveAcrossProcesses`, `TestAcquireLockExclusiveGoroutines`, `TestEvolveLockKilledHolderReleases`, `TestAcquireLockStaleFileAndBusy`, `TestLockReleaseClearsHolderInfo`, `TestLockBusyMessageOmitsPreviousHolder`, `fslock.TestTryExclusiveAndShared`; `-race -count=5` qua ([lock-count5.log](evidence/lock-count5.log)); probe `LockExclusive` PASS | `evolve.AcquireLock` dùng `flock`/`LockFileEx` qua gói mới `internal/fslock` (workflow dùng chung); không còn thu hồi theo PID chết; `Release` xoá thông tin chủ rồi đóng, không xoá tệp |
| `NT-P6-02` | đạt | `TestEvolverMutationOfAlreadyDirtyFileRejected`, `TestSkillEvolverMutationOfAlreadyDirtyFileRejected`, `TestEvolverDirtyTreeUnchangedNotFlagged`, `TestGuardReadOnly`, `TestGuardReadOnlyCoversRepoRootWhenBintInSubdir`; probe `DirtyFile` PASS | `guardReadOnly` (tri thức và skill) so cây Git bằng `workflow.WorktreeSnapshot` (chỉ mục tạm + `write-tree`) ở gốc repo; lỗi Git làm hỏng lượt thay vì bị bỏ qua |
| `NT-P6-03` | đạt | `TestRollbackFailureMidwayRestoresAppliedState`, `TestRollbackCompensationFailureIsReported`, `TestRollbackReadErrorDoesNotDeleteFile`; probe `RollbackFailure` PASS | Bù trừ dựng trước vòng ghi, dùng nội dung đã đọc ở vòng kiểm xung đột; lỗi tiêm qua biến hàm (không `chmod`) |
| `NT-P6-04` | đạt | `TestUpdateMergesDraftInputsAndThirdParty`, `TestDraftItemBaseInputs`; probe `UpdatePreserves` PASS | `Inputs` hợp nhất bỏ trùng, giữ thứ tự; `third_party` chỉ bật thêm, không hạ |
| `NT-P6-05` | đạt | `TestMergeBodiesTable`, `TestMergeKeepsDistinctFacts` (kèm rollback), `TestMergeIdenticalBodiesNotDuplicated`, `TestMergeContradictingFactsSurfaced`, `TestMergeCarriesThirdParty`, `TestEvaluateCandidateCarriesThirdParty`; probe `MergePreserves` PASS | Cách (a) do chủ dự án chọn: nối nguyên văn đơn vị (câu/mục/khối mã) chỉ có ở mục bị gộp sau dòng `Gộp từ <ID>:`, so theo tập đơn vị đã chuẩn hoá; mang `Inputs` và cờ `third_party` của mục bị gộp (Evaluate và Apply dùng chung `carryThirdParty`) |

Lệnh của mục 4.3, chạy tại `7925e7d` + thay đổi của vòng này:

| Lệnh | Kết quả |
|---|---|
| `go test -count=1 ./...` | qua ([go-test.log](evidence/go-test.log)) |
| `go test -race -count=1 ./internal/evolve/... ./internal/workflow/... ./internal/fslock/...` | qua |
| `go test -race -count=5 -run Lock ./internal/evolve/ ./internal/fslock/` | qua |
| `go vet ./...`, `GOOS=windows go vet ./...` | sạch |
| `staticcheck ./...`, `GOOS=windows staticcheck ./...` | sạch |
| `run-probes.py` (5 probe P6) | 5/5 PASS ([probes-after.log](evidence/probes-after.log)) |

"Test hỏng khi hoàn nguyên bản sửa": implementer của từng phiếu báo đã đảo từng bản sửa và thấy test tương ứng
đỏ (ghi trong `bint task show <id>`). Người điều phối không tự đảo lại; bằng chứng độc lập là 5 probe P6 viết
từ trước (biên bản P6) chuyển từ FAIL sang PASS.

## Lỗi tìm được

- **`NT-WF-BASELINE` (mới, trung bình) — workflow:** tác vụ `shared` lấy cây gốc lúc lập phiếu làm mốc; commit của
  người (hoặc tác vụ worktree khác land vào cây) xảy ra trong lúc tác vụ chạy bị `bint task diff` tính là thay đổi
  của tác vụ. Ở Finalize, Bint báo "thay đổi không thuộc tác vụ" và chặn `done` dù kiểm chứng qua; chạy lại
  `bint task verify` không gỡ được. Tái hiện ở phiếu 2 (11 tệp của commit `1391936`) và phiếu 3 (49 tệp do GĐ4
  custom-flow land sau khi phiếu 3 xong). Lối ra hiện tại chỉ có `--force` của người.
- Review phiếu 1 (7 low) và phiếu 2 (1 medium, 3 low) đã sửa trong phiếu 2 và 3, trừ các mục ghi ở dưới.

## Phát hiện low còn mở (gom vào `NT-P6-06`)

- `mergeBodies` không idempotent khi body mục giữ lại có khối ``` chưa đóng (`splitBlocks` nuốt khối "Gộp từ"),
  phát hiện `01M79Z83VFMTJ6CM2AJ9PPH0HY.1`.
- Cùng một câu ở dạng đơn vị khác (mục danh sách so với văn xuôi, thiếu dấu chấm) bị nối trùng (`.2`); an toàn vì
  không mất ý.
- `TestEvaluateCandidateCarriesThirdParty` chưa ràng mục giữ lại khác mục mang cờ (`.3`).
- `repoToplevel` gọi ở cả hai lần chụp; monorepo lớn dễ chạm hạn 2 phút (`.4`).
- `guardReadOnly` không thấy thay đổi bên trong submodule (phiếu 1, `.5`; đã ghi chú trong mã).
- Nhánh merge của skill (`deterministicSkillProposal`) chưa giữ chi tiết như nhánh tri thức.

## Việc của người

- Mục tri thức `k:01M746PTBYPSXDT0WJZS38KPFD` (và đoạn tương ứng trong `AGENTS.md`) nói `evolve.AcquireLock` còn dùng
  mẫu PID chết — đã lỗi thời sau vòng này; cần sửa hoặc revalidate.

## Chưa kiểm được và lý do

- `LockFileEx` và `fslock_windows.go` mới chỉ qua `GOOS=windows go vet`/staticcheck; chạy thật thuộc `NT-WIN-LOCK`,
  `NT-WIN-TEST` (V2).
- Không có lượt `evolve` thật (tốn token); hành vi trên đề xuất thật thuộc V4.

## Đề xuất kết luận (chờ chủ dự án ký)

`NT-P6-01..05` đạt. P6 vẫn "Một phần": còn `NT-EVO-TEST` (V4) và `NT-P6-06` (low). Điều kiện "V0 xong" của V4 đã thoả.
