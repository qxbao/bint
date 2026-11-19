# Review tác vụ 01M7NWDTRN29QFP1AFAKGB87XS

## Vòng 1 — 2026-11-17T23:40:08Z

Mã sửa đúng hướng và phủ mọi đường gọi (Runner/Env.Verify ops.go:818, goalfreeze.go:221 đều qua verifyTempDir; test workflow kể cả TestVerifyChildProcessTempDir xanh trong verify-1.log), nhưng kiểm chứng cuối đỏ vì lần verify chạy bằng binary bint cũ: AC-4 chưa được chứng minh.

### [01M7NWDTRN29QFP1AFAKGB87XS.1] risk/high — Kiểm chứng cuối đỏ do bint đang chạy chưa chứa bản sửa; AC-4 chưa được chứng minh

- Vị trí: `internal/workflow/ops.go:872-900`
- Bằng chứng: verify-1.log:13,23,45 cho thấy TMPDIR vẫn là C:/Users/admin/AppData/Local/bint/state/bint-8d262468937e/tmp/verify-3666362666 (mẫu cũ state.TmpDirFor + verify-*), không phải %TEMP%/bv/<số>; dòng 45 vẫn 'Filename too long' khi git init --bare. 8 test eval/evolve/tui/cli đã nêu còn đỏ cùng nguyên nhân. Gói workflow ok (dòng 103) nên mã trong diff đúng; tiến trình bint chạy verify được build trước thay đổi.
- Tiêu chí: 4

Build và cài bint từ worktree này rồi chạy lại bint task verify để chứng minh AC-4; không cần sửa mã thêm. Bản sửa môi trường verify chỉ có hiệu lực khi chính binary chạy verify được thay.

### [01M7NWDTRN29QFP1AFAKGB87XS.2] suggestion/low — Thư mục %TEMP%/bv/* mồ côi không có GC

- Vị trí: `internal/workflow/ops.go:893-900`
- Bằng chứng: verifyTempBase trả filepath.Join(os.TempDir(), 'bv'); cleanup chỉ chạy khi lệnh kết thúc bình thường; docs/workflows.md đã ghi nhận là ngoài phạm vi.

Có thể dọn thư mục con bv cũ hơn N giờ khi tạo thư mục mới; không chặn tác vụ.

## Vòng 2 — 2026-11-18T00:12:21Z

Mã sửa (verifyTempBase → %TEMP%/bv/<số> trên Windows, test cygpath, test mục tiêu gọi tiến trình Go con) đúng hướng và test đúng đặc tả; nhưng kiểm chứng cuối vẫn chạy bằng bint cũ nên AC-4 chưa được chứng minh (lỗi cũ chưa xử lý, không phải lỗi mới).

### [01M7NWDTRN29QFP1AFAKGB87XS.3] risk/high — Kiểm chứng cuối vẫn dùng binary bint cũ (TMPDIR dưới state/tmp/verify-*); AC-4 chưa được chứng minh

- Vị trí: `internal/workflow/ops.go:872-900`
- Bằng chứng: verify-2.log của tác vụ này dòng 13 và 23: đường dẫn tạm là C:/Users/admin/AppData/Local/bint/state/bint-8d262468937e/tmp/verify-3816364643/... tức mã TRƯỚC bản sửa (bản sửa cho ra %TEMP%/bv/<số>). Vẫn đúng 8 test đã nêu đỏ: TestEvalMineFromTasksDebugEndToEnd, TestEvalMinimalFromBootstrapSnapshot (cli), TestRunHarnessVariantRecordsGuardrailUsage (eval), TestEvaluateCreateProposal, TestEvaluateSkillProposal (evolve, lượt lỗi hạ tầng), TestEvalActionsAgainstRealService, TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch (tui/actions), TestEvalRunEscCancelsBatchAndShowsResumeCommand (tui/app). Mã thoát -1 và log dừng sau tui/ui, không có dòng nào của bint/internal/workflow: lần chạy bị cắt trước gói chứa TestVerifyChildProcessTempDir/TestVerifyUsesPrivateTempDir, nên test mục tiêu chưa được chứng minh xanh qua verify.
- Tiêu chí: 4

Cùng phát hiện vòng 1 (01M7NWDTRN29QFP1AFAKGB87XS.1), chưa xử lý. Không phải lỗi của diff: cần build lại bint từ worktree này hoặc chạy verify bằng binary mới, rồi chạy lại go test -timeout 30m ./... và đối chiếu: đường dẫn tạm phải là %TEMP%/bv/<số>, 8 test hết đỏ, gói workflow có kết quả. Đồng thời tìm vì sao lần chạy kết thúc mã -1 trước gói workflow (timeout của bint verify?).

### [01M7NWDTRN29QFP1AFAKGB87XS.4] suggestion/low — Thư mục %TEMP%/bv/* mồ côi không có GC

- Vị trí: `internal/workflow/ops.go:892-900`
- Bằng chứng: verifyTempDir chỉ dọn qua cleanup; bint bị giết giữa lần kiểm chứng để lại %TEMP%/bv/<số> (có thể chứa repo test lớn). docs/workflows.md và plan ghi rõ là ngoài phạm vi.

Có thể dọn các thư mục bv/* cũ hơn một ngưỡng khi tạo thư mục mới.
