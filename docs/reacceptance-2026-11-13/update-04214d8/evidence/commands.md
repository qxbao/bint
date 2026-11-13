# Lệnh và kết quả

Múi giờ: `Asia/Ho_Chi_Minh` (`+07:00`). Mỗi lượt có JSON metadata gồm argv, cwd, thời gian, thời lượng, exit và biến môi trường liên quan; stdout/stderr thô nằm cạnh metadata. Không ghi secret. Các test dùng `-count=1`; không chạy Claude, service MCP ngoài hoặc benchmark.

| ID | Command | Exit | Kết quả |
|---|---|---:|---|
| C0a | `go test -count=1 -json -timeout 5m ./internal/workflow -run '^(TestRunStep.*MCP.*|TestRunStepWritesBindingAndTools|TestWriteStepMCPConfig|TestStepMCPTools.*|TestPruneStaleJSON)$'` | 0 | workflow fixture |
| C0b | `go test -count=1 -json -timeout 5m ./internal/eval -run '^(TestCodexExclusiveMCP.*|TestCodexEvalArgsUnchangedByUserMCP|TestClaudeArgsForMCPTools|TestCodexArgsForMCPTools|TestCodexEvalCitationApproval)$'` | 0 | harness args/Codex MCP fixture |
| C0c | `go test -count=1 -json -timeout 5m ./internal/harness/... -run '.*MCP.*'` | 0 | adapter fixture |
| C0d | `go test -count=1 -json -timeout 5m ./internal/cli -run '^TestKnowledgeCiteEndToEnd$'` | 0 | citation fixture |
| C1a | `go test -count=1 -json -timeout 5m ./internal/mcpcat/...` | 1 | catalog/stdio qua; HTTP probe không bind được loopback IPv6 |
| C1b | `go test -count=1 -json -timeout 5m ./internal/config -run '^Test(.*MCP.*|RenderMatchesDefaults|RenderRoundTrip)$'` | 0 | config fixture |
| C1c | `go test -count=1 -json -timeout 10m ./internal/app -run '^Test(.*MCP.*|EnsureGitExcludeVariants)$'` | 1 | app gateway listener bị sandbox chặn |
| C1d | `go test -count=1 -json -timeout 10m ./internal/cli -run '^TestMCP'` | 1 | case quản lý qua; gateway không bind được listener |
| C23 | `go test -count=1 -json -timeout 15m ./internal/mcpgw` | 1 | 15 test cần listener bị chặn; binding thuần qua |
| C4a | `go test -count=1 -json -timeout 10m ./internal/tui/app -run '^Test(MCP|Golden|SystemScreenHasNoMCPTab|ReadOnlyAndAgentNeverReachDo)'` | 0 | golden/read-only Do |
| C4b | `go test -count=1 -json -timeout 5m ./internal/tui/actions -run '^(TestMCP.*|TestNoReferenceToSystemMCPTab)$'` | 0 | actions/Job |
| C4c | `go test -count=1 -json -timeout 5m ./internal/cli -run '^Test(CommandAndFlagCoverage|CoverageCheckerCatchesGaps|ServiceMessageCLIInventory.*)$'` | 0 | command/flag inventory |
| C4d | `go test -count=1 -json -timeout 5m ./internal/tui/screens/mcp ./internal/tui/components/tabs` | 0 | MCP screen/tab component |
| C-RPC | `go test -count=1 -json -timeout 5m ./internal/mcpserver` | 0 | Context Engine RPC regression |
| C-README (baseline) | `go test -count=1 -json -timeout 5m ./internal/cli -run '^TestREADMEDocumentsTUIGlobalKeys$'` | 1 | raw `C-README.*` giữ nguyên lỗi tìm nhầm README ở baseline; không còn là lệnh chạy lại sau khi test rename |
| C-README-CURRENT | `go test -count=1 -json -timeout 5m ./internal/cli -run '^TestUsageDocumentsTUIGlobalKeys$'` | 0 | hợp đồng TUI hiện tại ở `docs/usage.md`; targeted green ở run mới |
| C-VET-LINUX | `GOOS=linux go vet ./...` | 0 | static analysis |
| C-VET-WINDOWS | `GOOS=windows go vet ./...` | 0 | không phải Windows runtime |
| C-SC-DISCOVERY | `command -v staticcheck && staticcheck -version` | 127 | binary vắng mặt; không cài |
| C-DOC (trước ủy quyền sửa test) | `python3 docs/reacceptance-2026-11-13/update-04214d8/evidence/check-docs.py` | 0 | raw khi đó ghi 10/10; lần audit sau sửa có 7/10 vì ba giả định snapshot/allowlist/finding cũ, xem run `20261113T073052Z-doc-audit` |
| C-DIFF-CHECK | `git diff --check` | 0 | không có whitespace error |
| C-BINT-LINT | `bint task lint 01M79NVK4EMZVFAY00X8B044X8 --strict` | 0 | artifact đạt hợp đồng v1; dùng binary hiện tại từ non-login PATH |

Lệnh Go chạy tại `/home/DUCLH/orca/bint`. Bint CLI dùng để đọc phiếu là `/tmp/bint-acceptance-current-yob_9zp_/bint` qua non-login PATH. Login shell ban đầu tìm binary cũ `~/.local/bin/bint` và báo không nhận khóa `mcp`; lỗi đó không được dùng làm kết quả task. `go test ./...` thuộc Bint Core và không lặp lại ở đây.

Không có test nào bị `skip`: C0a/b/c/d lần lượt có 6/16/4/1 test pass; C1a có 11 pass, 2 fail do listener; C1b 14 pass; C1c 9 pass, 2 fail do listener; C1d 7 pass, 2 fail do gateway listener; C23 3 pass, 15 fail do listener; C4a/b/c/d lần lượt 30/6/21/26 test pass; C-RPC 9 pass. C-README có một test fail mang finding, không phải PASS. Các số đếm được trích từ raw `go test -json` files.

`docs/status.md` đã có thay đổi trước tác vụ theo kế hoạch và được bảo toàn; nó xuất hiện trong diff hiện tại nhưng không thuộc đầu ra của lượt này. Các tệp báo cáo chưa commit được chụp hash trước khi sửa; kiểm tra sau lượt xác nhận chỉ `docs/reacceptance-2026-11-13/README.md` đổi trong số 67 tệp đó.

## Bổ sung sau review .1–.4

Các kết quả dưới đây là lượt mới, không thay các JSON/stdout/stderr C* cũ. Runner hiện ghi mỗi lần gọi vào `evidence/runs/<run-id>/`; `--check` chọn một hay nhiều ID và baseline băm source/test của package được chọn. Sáu test độc lập được chạy từ cwd `/home/DUCLH/orca/bint/docs/reacceptance-2026-11-13/update-04214d8` qua `python3 evidence/run-checks.py --check C1c-INDEX --check C1c-LINKED-WORKTREE --check C1c-DEGRADED --check C1c-SECRET-NAMES --check C1c-DOCTOR --check C1c-INIT-PERMISSION`; cả sáu exit 0, metadata và test JSON ở `evidence/runs/20261113T140852+0700-2/`.

Runner được thử trên bản sao trong `/tmp` bằng fake `go`, chạy `--check C1c-INDEX` hai lần. Lệnh thoát 0; raw sentinel trước/sau có cùng SHA-256, tạo hai run ID và metadata trỏ tới stdout/stderr riêng. Chi tiết: [`script-regression.json`](script-regression.json). Đây chỉ là kiểm tra cách ghi artifact của runner, không phải kết quả Go.

Trong lượt b-feat bổ sung này, lần đầu `bint task show 01M79NVK4EMZVFAY00X8B044X8` dùng `/home/DUCLH/.local/bin/bint` từ login PATH và thoát 1 vì config schema không nhận khóa `mcp` tại `.bint/config.yml:180`. Sau đó non-login PATH tìm đúng `/tmp/bint-acceptance-current-yob_9zp_/bint`; `bint task error` đã ghi lần thử fake-go không đầy đủ và `bint task show` đọc được task hiện tại. Không gọi status/approve/verify; Bint Core vẫn sở hữu trạng thái/verify.

Core verify-1 của chính phiếu này chạy `go test ./...` tại baseline trước khi người dùng ủy quyền sửa test, và thoát 1 vì `TestREADMEDocumentsTUIGlobalKeys` báo thiếu `## Giao diện terminal (TUI)` trong README; log gốc ở runtime task `logs/verify-1.log`. Lịch sử này được giữ nguyên và tách khỏi các fixture M1 mới. Agent không lặp full suite.

## Tái kiểm M4-06 sau khi tài liệu chuyển vị trí

`docs/usage.md` hiện là nguồn mục TUI chi tiết, còn README chỉ giới thiệu ngắn và dẫn sang tài liệu đó. Red ban đầu dùng GOCACHE mặc định không chạy được vì cache chỉ đọc (`20261113T072506Z-README-red/`, exit 1 môi trường); lần red hợp lệ dùng cache tạm và xác nhận lỗi assertion của test cũ (`20261113T072531Z-README-red-cache/`, exit 1). Sau đó test đổi tên thành `TestUsageDocumentsTUIGlobalKeys`, đọc `../../docs/usage.md`, kiểm link tương đối `tui-plan.md` và giữ các assertion còn lại. Green targeted `go test -count=1 ./internal/cli -run '^TestUsageDocumentsTUIGlobalKeys$'` exit 0 ở `20261113T072701Z-README-green/`. Cả ba run có metadata argv/cwd/env/HEAD/hash/exit/thời gian và stdout/stderr. Kết quả này không sửa raw `C-README` hay verify-1, và không thay thế full verify-2 của Core.

Doc audit sau sửa: `evidence/runs/20261113T073052Z-doc-audit/`. Lệnh `check-docs.py --help` thực ra bỏ qua `--help` và chạy validator: 7/10 kiểm tra qua, còn 3 finding cũ/đã biết — hash `.bint/config.yml` khác snapshot (nội dung người dùng đã xác nhận, không sửa), scope validator không tính test vừa được task cho phép, và validator vẫn đòi README finding cũ. Local links, nguồn ma trận, cột task và output bắt buộc qua; `git diff --check` qua. Không coi kết quả tổng này là PASS. Lệnh `bint task error` thử ghi lỗi cache không chạy được vì binary trên PATH không đọc được khoá `mcp` mới trong `.bint/config.yml`; không dùng binary cài cũ để sửa task state.

Lỗi lần chạy `bint task error` trước đó được ghi sau khi xác định binary Bint mới trong PATH. `bint task lint 01M79NVK4EMZVFAY00X8B044X8 --strict` với binary `/tmp/bint-acceptance-current-yob_9zp_/bint` exit 0; metadata và output ở `evidence/runs/20261113T073343Z-task-lint/`.

Sau lượt review, `bint task lint 01M79NVK4EMZVFAY00X8B044X8 --strict` chạy bằng binary `/tmp/bint-acceptance-current-yob_9zp_/bint`, cwd `/home/DUCLH/orca/bint`, exit 0 (`evidence/task-lint.json`). Đây là lint artifact; Bint Core vẫn chưa chạy lại lệnh verify đã xác nhận.
