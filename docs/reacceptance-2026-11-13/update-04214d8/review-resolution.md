# Xử lý review bổ sung

Phạm vi là bốn finding của review vòng 1 cho phiếu `01M79NVK4EMZVFAY00X8B044X8`. Thay đổi chỉ ở tài liệu phụ lục và runner evidence; raw C* logs, baseline/hash lịch sử, source, README sản phẩm, test và cấu hình không bị sửa.

| Finding | Xử lý | Bằng chứng | Trạng thái |
|---|---|---|---|
| .1 — C1c panic trước index/doctor/secret | Ma trận không còn coi regex đã chọn là test đã chạy. Chạy riêng index, linked worktree, degraded, secret names, doctor và init permission. | `evidence/runs/20261113T140852+0700-2/checks-summary.json` cùng từng JSON/stdout/stderr; sáu test event `pass`, exit 0. C1c gốc vẫn là exit 1 và không được nâng thành PASS. | Đã xử lý; doctor/service thật vẫn hoãn ở DEF-05. |
| .2 — runner ghi đè raw evidence | Runner tạo `evidence/runs/<run-id>/` mới, lưu baseline theo HEAD/env/source hash, thêm `--check` và từ chối trùng run ID. Hướng dẫn chạy theo cwd thực tế đã cập nhật. | `evidence/run-checks.py`; `evidence/script-regression.json`: bản sao `/tmp`, fake `go`, hai lượt exit 0, sentinel raw cũ giữ nguyên SHA-256, mỗi metadata trỏ đúng stdout/stderr trong run riêng. | Đã xử lý. Kiểm tra runner giả lập không phải test Go. |
| .3 — thiếu tiêu chí nguồn | Thêm M0-05 cho ba skill/fallback/cite, M1-08 cho seed/exclusion §5.5 và M3-07 cho giới hạn G3. M1 index/degraded/secret/doctor được ánh xạ test mới. | `matrix.md` và `tasks.md` TSK-12/14. Tìm trong `internal/` không thấy triển khai sao chép `.codegraph/codegraph.db`/`.gitignore` và loại pid/socket/log/wal/shm; test linked-worktree chỉ xác nhận index trong worktree. | Đã ánh xạ. §5.5 chưa có chứng cứ triển khai; DEF-15 ghi protocol tái kiểm. G3 live/attribution vẫn hoãn. |
| .4 — DEF/path sai | M5-03 trỏ DEF-09; tasks trỏ artifact hiện có `evidence/C-DIFF-CHECK.json`. Cập nhật kiểm tra tài liệu cũ thành mô tả đúng phạm vi: nó chỉ kiểm local Markdown links, không xác thực ID/path dạng text/code. | `matrix.md` M5-03; `tasks.md` TSK-11; `evidence/C-DIFF-CHECK.json`; `evidence/reference-audit.json` rà 10 Markdown links, 23 path, 27 DEF IDs và 10 source-line refs, không có lỗi. | Đã xử lý. Hash 67 artifact được đối chiếu tại `evidence/raw-preservation.json`; chỉ README tổng quan có thay đổi cảnh báo baseline được phép. |

## Verify của Core

Core verify-1 chạy `go test ./...` tại baseline trước khi người dùng ủy quyền sửa test và thoát 1 ở `TestREADMEDocumentsTUIGlobalKeys`: README thiếu tiêu đề `## Giao diện terminal (TUI)`. Log thực nằm trong runtime task `logs/verify-1.log`; tóm tắt test và kết luận tương ứng ở `evidence/commands.md`/DEF-11. Kết quả lịch sử được giữ nguyên; agent không lặp full suite.

## Đính chính DEF-018

Guardrail metadata chỉ được coi là xác minh sau chuyển trạng thái do người thực hiện; source metadata tự thân không chứng minh guardrail được chọn. `stop_hook_active` chỉ bỏ chặn remind/ask; deny có thể block tối đa ba lần rồi bypass. Đây là làm rõ protocol đầu vào, không sửa hoặc ghi đè raw evidence cũ.

## Tái kiểm test tài liệu TUI

Sau lần review sạch, người dùng ủy quyền sửa riêng `internal/cli/readme_tui_test.go`: tài liệu TUI chi tiết đã ở `docs/usage.md`, link kế hoạch trong file này là `tui-plan.md`. Red hợp lệ tại `evidence/runs/20261113T072531Z-README-red-cache/` xác nhận test cũ còn tìm tiêu đề trong README. Test được đổi thành `TestUsageDocumentsTUIGlobalKeys`, đọc `../../docs/usage.md`, cập nhật tên/link/diagnostics và giữ assertions về tabs, phím, chế độ chỉ đọc, locale, NO_COLOR, terminal và chuột. Green targeted tại `evidence/runs/20261113T072701Z-README-green/` exit 0. Lần thử trước đó chỉ vướng cache chỉ đọc, được lưu riêng tại `20261113T072506Z-README-red/`.

Raw C-README và Core verify-1 không bị thay đổi: cả hai vẫn phản ánh FAIL tại baseline 04214d8. Targeted green chỉ xác nhận test được sửa; verify-2/full suite thuộc Core và phải được đọc riêng trước khi kết luận.
