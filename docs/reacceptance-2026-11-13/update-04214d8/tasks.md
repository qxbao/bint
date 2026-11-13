# Danh sách chạy bổ sung

Mỗi dòng là một phần của cùng đợt kiểm toán. Các lệnh fixture không gọi Claude hoặc service MCP ngoài; `C23` có thể cần quyền bind loopback và hiện bị sandbox chặn. Exit 0 chỉ đạt fixture tương ứng. Log và môi trường liên quan nằm trong `evidence/<ID>.json` cùng `.stdout`/`.stderr`.

| ID | Stage | Tiêu chí | Command / protocol | Dependency | Trạng thái | Evidence / hoãn |
|---|---|---|---|---|---|---|
| TSK-01 | baseline | AC-1, AC-3 | Chụp HEAD, `git diff --name-status d788669..HEAD`, hash trực tiếp báo cáo/config/index/binary | — | xong | `evidence/baseline.json`; 67 tệp baseline cũ |
| TSK-02 | inventory | AC-1 | Đối chiếu MCP §4–8/§11, TUI §5.8/6/7.4/8, workflows MCP và README với source/test | TSK-01 | xong | `matrix.md`; source chỉ là implemented, chưa thay evidence chạy |
| TSK-03 | M0 | AC-1, AC-2 | C0a workflow; C0b eval/Codex; C0c harness; C0d citation CLI; protocol live Claude/Codex ở deferred | TSK-02 | fixture một phần qua | C0a–d exit 0; live Claude/Codex hoãn |
| TSK-04 | M1 | AC-1, AC-2 | C1a catalog/probe; C1b config; C1c app; C1d CLI; C23 sharing/gateway | TSK-02 | fixture một phần qua | C1b qua; C1a/C1c/C1d/C23 có lỗi socket sandbox; các test index/degraded/secret/doctor được chạy riêng ở TSK-12 |
| TSK-05 | M2 | AC-1, AC-2 | C23: `go test -count=1 -json -timeout 15m ./internal/mcpgw` | TSK-04 | hoãn một phần | Test khởi động daemon không bind được `127.0.0.1`; raw `C23.stdout`; không tính là đạt |
| TSK-06 | M3 | AC-1, AC-2 | C0a–d, C1c, C23, C-RPC; không chạy harness thật | TSK-03, TSK-04, TSK-05 | fixture một phần qua | C0*, C-RPC qua; các case cần daemon hoãn; live ở DEF-04–06 |
| TSK-07 | M4 + hồi quy | AC-1–3 | C4a app/golden/Do; C4b actions/Job; C4c CLI coverage; C4d màn/tab; C-README baseline và targeted recheck | TSK-02 | fixture TUI qua; test tài liệu trỏ sang `docs/usage.md` và targeted green | C4a–d exit 0; red/green mới có log riêng. C-README/verify-1 baseline vẫn FAIL lịch sử; Core full verify chờ |
| TSK-08 | M5 | AC-1–3 | Đọc §11 và kiểm provenance số PSS lịch sử; không benchmark | TSK-02 | đối chiếu tài liệu xong | `matrix.md`; không có phép đo mới |
| TSK-09 | deferred/supersession | AC-1–3 | Đối chiếu DEF-007, DEF-018 với source và baseline mới | TSK-03–08 | xong | [deferred.md](deferred.md); raw evidence cũ giữ nguyên |
| TSK-10 | phân tích tĩnh | AC-2 | `GOOS=linux go vet ./...`; `GOOS=windows go vet ./...`; dò `staticcheck` trên PATH | TSK-01 | vet qua; staticcheck hoãn | `C-VET-LINUX`, `C-VET-WINDOWS` exit 0; `C-SC-DISCOVERY` exit 127 |
| TSK-11 | tài liệu/phạm vi | AC-3 | Kiểm link, tiêu chí, log, hash và diff chỉ trong allow scope; `git diff --check` | TSK-01–10 | qua lượt bổ sung | `evidence/reference-audit.json`: 10 link, 25 evidence path, 27 DEF refs, 10 source-line refs hợp lệ; `git diff --check` exit 0. `evidence/raw-preservation.json`: 67 hash, chỉ README cảnh báo baseline khác hash. `evidence/task-lint.json`: lint strict exit 0 |
| TSK-12 | M1 independent rerun | AC-1, AC-2 | Chạy riêng `C1c-INDEX`, `C1c-LINKED-WORKTREE`, `C1c-DEGRADED`, `C1c-SECRET-NAMES`, `C1c-DOCTOR`, `C1c-INIT-PERMISSION` qua runner | TSK-04 | qua fixture | `evidence/runs/20261113T140852+0700-2/checks-summary.json`; sáu test exit 0, không suy ra live service |
| TSK-13 | an toàn runner | AC-2, AC-3 | Bản sao `/tmp`, `--check C1c-INDEX` hai lượt, fake `go`; xác nhận raw sentinel không đổi, run dir riêng, metadata trỏ đúng output | TSK-01, TSK-12 | qua | `evidence/script-regression.json`; exit 0, hai run ID riêng, SHA-256 sentinel trước/sau trùng |
| TSK-14 | inventory bổ sung M0/M1/M3 | AC-1, AC-2 | Đối chiếu MCP §4.3 ba skill; §5.5 seed/exclusion và code source; §7 G3 role interactive; ghi protocol/hoãn | TSK-02 | đã ánh xạ; §5.5 chưa tìm thấy triển khai | `matrix.md` M0-05/M1-08/M3-07; G3 live/attribution vẫn hoãn tại DEF-06, seed/exclusion tại DEF-15 |
| TSK-15 | xử lý review | AC-1–3 | Đối chiếu từng finding .1–.4 với log mới, đường dẫn và trạng thái; giữ raw evidence | TSK-11–14 | đã ghi; kiểm tài liệu cuối ở TSK-16 | `review-resolution.md` ghi sửa đổi và bằng chứng truy xuất |
| TSK-16 | audit tham chiếu | AC-1, AC-3 | Quét ID DEF và đường dẫn evidence dạng Markdown/plain text/backtick trong phụ lục; `git diff --check` | TSK-11–15 | qua | `evidence/reference-audit.json`: 0 lỗi; xử lý đủ finding .1–.4 tại `review-resolution.md` |
| TSK-17 | M4-06 targeted fix | AC-2, AC-3 | Red `go test -count=1 ./internal/cli -run '^TestREADMEDocumentsTUIGlobalKeys$'`; sửa test đọc `../../docs/usage.md`, đổi link `docs/tui-plan.md` thành `tui-plan.md`; green `go test -count=1 ./internal/cli -run '^TestUsageDocumentsTUIGlobalKeys$'` | TSK-07 | targeted green; doc audit có ba finding validator cũ | `evidence/runs/20261113T072531Z-README-red-cache/` exit 1 đúng assertion; `evidence/runs/20261113T072701Z-README-green/` exit 0. Lần đỏ cache chỉ đọc ở `20261113T072506Z-README-red/`; audit `20261113T073052Z-doc-audit/` ghi local links và diff check qua, ba giả định validator cũ không được coi PASS. Bint task lint strict exit 0 ở `evidence/runs/20261113T073343Z-task-lint/`; final `git diff --check` và lint exit 0 ở `20261113T073450Z-final-check/`. Không thay kết quả baseline hoặc full verify Core |

## Lệnh fixture

Các ID dưới đây ánh xạ đúng argv trong JSON metadata. Chạy `python3 evidence/run-checks.py --check C1c-INDEX` để chọn một mục; bỏ `--check` để chạy toàn bộ nhóm. Mỗi invocation ghi vào `evidence/runs/<run-id>/` mới, không đụng raw log hay summary cũ. Các mục cùng gateway dùng một fixture cục bộ, không tạo sáu phiếu feature.

| ID | Bao phủ |
|---|---|
| C0a–d | MCP config/binding ở workflow, đối số Codex/Claude, harness MCP và citation CLI |
| C1a–d, C1c-* | catalog/probe, schema/default/render, app config/index/doctor/secret, CLI; các C1c-* chạy độc lập để tránh panic ở test gateway cắt ngắn package |
| C23 | daemon, sáu client cùng ID, worktree/generation, HTTP, grants, retry, process cleanup, spool |
| C4a–d | tám tab/golden/read-only Do, Action.Job, màn/tabs và command/flag inventory |
| C-RPC | hồi quy Context Engine stdio/RPC |
| C-README | raw baseline lịch sử; không dùng cho lượt mới. Chạy lại hợp đồng hiện hành bằng `python3 evidence/run-checks.py --check C-README-CURRENT` hoặc lệnh riêng; ID này chạy `TestUsageDocumentsTUIGlobalKeys` và không ghi đè raw C-README cũ |
| C-VET-* | phân tích tĩnh Linux và Windows; không phải kiểm runtime Windows |
| C-SC-DISCOVERY | kiểm có binary staticcheck; binary không có nên hai lượt staticcheck chưa chạy |
