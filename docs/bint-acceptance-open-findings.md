# Nghiệm thu các lỗi còn mở sau tái nghiệm thu P0–P5 và đợt đo P6–P7

Ngày: 2026-11-10. Merge vào `main`: `8b5cc9f` (nhánh `fix/open-findings`), cộng commit sửa bổ sung ghi ở cuối.

Nguồn lỗi: [bint-reacceptance-p0-p5-137dbf9.md](bint-reacceptance-p0-p5-137dbf9.md) (R1–R3) và
[evals/p6-p7-claude-2026-11-10/README.md](evals/p6-p7-claude-2026-11-10/README.md) (F1, F3, F7, F8, F11, F12;
F2/F4/F5/F6/F10/F13 đã sửa trước đó).

Mỗi lỗi được sửa trên một worktree riêng. Người nghiệm thu đọc diff, đối chiếu với điều kiện đóng, chạy lại
kiểm tra và trả lại những bản sửa chưa đạt. Không chạy harness thật hay benchmark trả phí.

## Kết quả

| ID | Kết luận | Commit | Bằng chứng / ghi chú |
|---|---|---|---|
| R1 | **Đóng** | `755b544` | Khoá ghi dùng khoá của hệ điều hành (`flock` trên Unix, `LockFileEx` trên Windows), bỏ bước thu hồi dựa trên quan sát stale. Probe cũ (24 đợt × 24 goroutine × 30 lần, khoá PID chết) thành `TestReacceptStaleReclamationExclusive`: 0 lượt chồng chủ, qua với `-count=5`; `TestAcquireWriteLockExclusiveAcrossProcesses` qua với `-count=5`. |
| R2 | **Đóng** | `d2945f1` | Ảnh chụp bước chỉ đọc gồm cả `.bint/knowledge`; `ReadonlyViolations` chỉ cho qua tệp **mới** trong `items/`, hợp lệ, `status: candidate`. Test cho 4 ca: tạo candidate (được), sửa rule verified, xoá mục có sẵn, tạo tệp tự gán verified (đều bị đánh lỗi). |
| R3 | **Đóng** | `831db65` | `Approve` tính lại diff chưa duyệt khi task chưa qua Runner. E2E CLI: done bị chặn → approve → done thành công, không `--force`, không tự gán `ProtectedApproval`. Đã xác nhận test FAIL khi hoàn nguyên bản sửa. |
| F1 | **Đóng** (trả lại 1 lần) | `1aad24f`, `d615276` | Gom tệp anh em theo mẫu tên chung (`*_suffix`, `test_*`) khi từng tệp chưa đạt ngưỡng. Lần đầu bị trả lại: prompt evolver không có tệp thành viên, quan sát không khớp, và mẫu theo đuôi tệp quá rộng. |
| F3 | **Đóng** (trả lại 1 lần) | `6d62b55`, `45ac160` | Cửa sổ 24 giờ chỉ được bỏ cho commit sau đề xuất khi lượt căn cứ đã kết thúc trước đề xuất **và** đã được gắn commit. Lần đầu bị trả lại vì mở lại rò rỉ: lượt chưa commit, công việc được commit ngay sau đề xuất. |
| F7 | **Đóng** (trả lại 1 lần, sửa bổ sung khi nghiệm thu) | `2d8fb48`, `f04d3ee`, bổ sung | Hook chụp `git status` + dấu nội dung trước/sau lệnh shell. Lần đầu bị trả lại: ảnh chụp khoá theo phiên làm mất sự kiện khi Bash chạy song song, và không giới hạn thời gian trong hook. Sửa bổ sung khi nghiệm thu: ảnh sau lấy dấu theo đúng cách của ảnh trước, tránh báo nhầm khi hết ngân sách băm (`TestSnapshotStampModeFollowsBefore`, đã xác nhận FAIL khi không có bản sửa). |
| F8 | **Đóng một phần theo phạm vi** | `2d8fb48` | Token của phiên Claude Code đọc từ transcript ở SessionEnd. `cost_usd` vẫn NULL vì chưa có bảng giá; Codex/Antigravity chưa có token. |
| F11 | **Đóng** | `c1da19f`, `45ac160` | Kiểm chứng lại ứng viên bị loại do kiểm chứng thì cập nhật trạng thái; lý do loại thủ công có tiền tố `kiểm chứng: ` được đổi thành `người duyệt: …`. |
| F12 | **Đóng** | `1aad24f` | Nhóm chiếm quá nửa số lượt tự hạ sâu thêm cấp thư mục; cấu hình `evolution.trigger.group_depth`. |

## Kiểm tra trên `main` sau merge

- `go build ./...`, `go vet ./...`: qua.
- `go test -count=1 ./...` và `go test -race -count=1 ./...`: 196 test qua, 31 package.
- `CGO_ENABLED=0 GOOS=windows go build ./...`, `GOOS=darwin`: qua.

## Giới hạn còn lại

- R1: bản Windows mới biên dịch và vet, chưa chạy test trên Windows thật. Hệ tệp không hỗ trợ `flock` (một số NFS) có thể hành xử khác.
- R3: `approve` duyệt toàn bộ diff được bảo vệ hiện tại; thay đổi phát sinh giữa lúc `done` bị chặn và lúc `approve` cũng được duyệt (vẫn được in ra).
- F3: vòng tiến cứu trong ngày chỉ mở được khi lượt căn cứ đã được quét Git gắn commit; chưa có trường phân biệt lượt cô lập của eval.
- F7: tệp trong `.gitignore` không thấy; chưa có `file.read` cho lệnh đọc qua shell; giới hạn 5000 tệp bẩn mỗi ảnh.
- F12: nhóm bị hạ sâu đổi khoá cơ hội, nên đề xuất đang chờ theo khoá cũ không còn khớp.

## Chưa thuộc đợt này

Các việc thực nghiệm cần ngân sách mô hình vẫn mở và quyết định nghiệm thu P1–P3, P6–P7 vẫn phụ thuộc vào chúng:
đo lại tỷ lệ đạt nền với đặc tả giao diện trong đề bài, đo chi phí Probe ở cỡ mẫu dùng được, repository thứ hai
và corpus cho cổng quyết định 1 và 2, bằng chứng tích hợp provider (citation của Codex, hook công cụ của Antigravity).
