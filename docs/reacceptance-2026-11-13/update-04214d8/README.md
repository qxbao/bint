# Phụ lục tái nghiệm thu — delta 04214d8

Phụ lục này kiểm tra phần mới trong delta `d788669..04214d8babb97baa3994dd0cb571418df21706ab`, không chạy lại P0–P8 không đổi. Phiếu gốc `01M79DW2JXXF41V10HZ5MFMACM` (`bint task show 01M79DW2JXXF41V10HZ5MFMACM`) và phiếu đính chính `01M79J8MVX0PFTFTJ9J2FTM5JX` (`bint task show 01M79J8MVX0PFTFTJ9J2FTM5JX`). Baseline tại đầu lượt: HEAD `04214d8babb97baa3994dd0cb571418df21706ab`; delta và hash chụp trực tiếp được lưu trong [`evidence/baseline.json`](evidence/baseline.json). Báo cáo cũ có 67 tệp được băm; không sửa log, hash hoặc nội dung báo cáo cũ ngoài liên kết cảnh báo được thêm vào mục lục của nó.

## Kết quả theo loại chứng cứ

| Nhóm | Implemented (đọc source) | Tested fixture (lượt mới) | Live / measurement |
|---|---|---|---|
| M0 | Có wiring cấu hình theo run, citation và tắt MCP Codex trong source | Workflow, eval, harness, citation fixture qua; xem C0a–d | Chưa chạy Claude/Codex thật; chưa xác nhận citation spool trong phiên harness thật |
| M1 | Có catalog, config hai tầng, CLI, check/index/doctor | Config và nhiều CLI/app case qua; các probe HTTP bị sandbox chặn bind, xem C1a–d | Không cài hoặc gọi service MCP thật; index/check máy thật hoãn |
| M2 | Có daemon, multiplex, partition, auth, retry, cleanup và spool | C23 bị chặn ở test cần loopback; một số test thuần trong package qua | Chưa xác nhận runtime Windows hoặc service thật |
| M3 | Có shim, grants, read-only/deny và binding | C23/C0/C1 và RPC có fixture; nhánh cần daemon bị sandbox chặn | Claude, S3, agy, attribution interactive, resources/prompts và Windows hoãn |
| M4 | Có tab thứ tám, màn MCP và hành động | Golden/tab, actions và CLI coverage qua ở C4a–d. C-README lịch sử đỏ ở baseline 04214d8; test được trỏ đúng sang `docs/usage.md` và targeted test mới xanh (M4-06). | Golden không chứng minh trải nghiệm terminal; user/VHS/Windows hoãn. Full verify sau sửa do Core xác nhận. |
| M5 | Kế hoạch giữ số đo PSS lịch sử | Chỉ đối chiếu tài liệu, không chạy đo mới | Số liệu lịch sử tại 38a1a9d; chất lượng, grants, nhiều worktree và Windows chưa được chứng minh |

`PASS` ở đây chỉ có nghĩa lệnh fixture tương ứng exit 0. Lỗi bind là **hoãn do môi trường**, không phải PASS. C-README và verify-1 giữ nguyên lỗi baseline; test hồi quy hiện hành đã được trỏ sang `docs/usage.md`, có red/green riêng. Chạy lại bằng `python3 evidence/run-checks.py --check C-README-CURRENT`; targeted green không thay thế full verify của Core. Ma trận từng tiêu chí và trạng thái có trong [matrix.md](matrix.md); danh sách chạy lại có trong [tasks.md](tasks.md); các giới hạn và điều kiện tái kiểm ở [deferred.md](deferred.md). Raw output cũ được giữ nguyên; lượt chạy mới nằm trong `evidence/runs/<run-id>/`.

## Diễn giải DEF-007

Kết luận cũ “M0–M4 chưa triển khai” vẫn là lịch sử chính xác ở baseline `d788669`. Tại baseline mới, source đã có các phần triển khai M0–M4 được ánh xạ trong ma trận; điều này chỉ supersede có điều kiện nhận định *không có source triển khai* ở những tiêu chí tìm thấy mã. Nó không chứng minh mọi tiêu chí đã test, được nghiệm thu live, hoặc hoàn tất M0–M5. M5 không có phép đo mới trong lượt này.

## Cách chạy lại

Từ thư mục `docs/reacceptance-2026-11-13/update-04214d8`, chạy `python3 evidence/run-checks.py --check C1c-INDEX` để chọn một nhóm; bỏ `--check` để chạy toàn bộ fixture. Mỗi lần gọi tạo `evidence/runs/<run-id>/` riêng, ghi baseline HEAD/env/hash, argv, cwd, thời gian, exit và stdout/stderr, không ghi đè log cũ. Script không chạy Claude hoặc service MCP bên ngoài. `go vet` Linux/Windows đã có log riêng; `staticcheck` chưa có binary và không được cài. Không chạy lại `go test ./...`: lệnh đó thuộc Bint Core.

Các finding review vòng bổ sung và cách xử lý có trong [review-resolution.md](review-resolution.md). Sáu test M1 độc lập được chạy sau khi C1c panic, và kiểm thử không ghi đè runner, được lưu trong [run riêng](evidence/runs/20261113T140852+0700-2/checks-summary.json) và [script-regression.json](evidence/script-regression.json).
