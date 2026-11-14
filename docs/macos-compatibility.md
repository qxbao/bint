# Khảo sát tương thích macOS — 2026-11-14

Tác vụ: `01M7CR900TFQK93Q58TT92A4TT`. Đã xác nhận lỗi production ở xử lý
đường dẫn qua symlink và lỗi fixture mang giả định Linux/GNU. Các lỗi đã sửa
có kiểm thử hồi quy. Core đã chạy toàn suite thành công sau sửa vòng 1
(`verify-2.log`, 2026-11-14T10:55:12Z). Vòng 2 sửa thêm hồi quy protected path
qua symlink không được bảo vệ; kết quả trước đó không thay thế kiểm chứng cho
thay đổi mới. Sandbox của agent cấm bind cổng loopback, kể cả cổng động dùng
bởi `httptest`; kết quả từng lượt được phân biệt bên dưới.

## Môi trường và cách kiểm chứng

- Native: Darwin arm64, Go 1.25.1, Apple Git 2.50.1 (Apple Git-155),
  staticcheck 2026.1 (v0.7.0); đĩa chứa TMPDIR còn khoảng 161 GiB khi bắt đầu.
- Giữ nguyên `TMPDIR=/var/folders/r3/6ymjmkyn3b510j4fb_2y_p9c0000gn/T/`;
  đây là đường dẫn qua symlink tới `/private/var/...`.
- Sandbox chặn ghi cache mặc định ngoài workspace. Các lệnh Go được chạy với
  `GOCACHE=/private/tmp/bint-macos-go-cache`; staticcheck thêm
  `STATICCHECK_CACHE=/private/tmp/bint-macos-staticcheck-cache`. Không đổi HOME,
  cấu hình cá nhân hoặc biến nhận diện agent toàn phiên để làm xanh kiểm thử.
- Bint Core quản lý lượt chạy và verify.command. Agent không gọi task
  approve/status/verify, không tự commit/land và không sửa state của tác vụ.

## Rào cản đã xác nhận và sửa

| Khu vực | Tái hiện trước sửa | Nguyên nhân và thay đổi |
| --- | --- | --- |
| Gateway: nhận diện tiến trình | `TestProcessGone` coi chính PID test đang sống là đã chết | Helper `internal/mcpgw/proc_unix_test.go` đọc `/proc` dưới tag `!windows`, trả true khi đọc lỗi. Darwin dùng `SysctlKinfoProc` trong `proc_darwin_test.go`; Linux giữ đọc stat. Lỗi probe được báo, không xác nhận chết. |
| Git worktree | `TestWorktreeAddListRemoveDeleteBranch`: `mục worktree: <nil>`; `TestWorktreeRemoveLockedSymlink` báo thành công dù worktree bị khóa vẫn đăng ký, ở cả thư mục còn và đã mất | Git trả đường dẫn vật lý, caller dùng alias. Sửa fixture so với đường dẫn vật lý; fallback của `WorktreeRemove` so bằng canonical path, kể cả tổ tiên còn tồn tại của đường dẫn đã mất. Không đổi `gitrepo.Clean`. |
| Hook và protected path | `TestRelPathThroughSymlink` không nhận tệp migration chưa tạo là trong repo; README bị đổi thành `readme.md`. Hai test protected-path ở app không trả ask/deny | `NormalizePath` chỉ giải được toàn đường dẫn tồn tại, nên root là `/private/var` nhưng tệp mới còn `/var`. `hookcore.RelPath` kiểm containment bằng đích canonical, nhưng chỉ giải prefix repository khi lấy tên tương đối cho policy; giữ cả symlink bên trong repo. Ca âm kiểm symlink ra ngoài và đường dẫn khác. |
| Vị trí state | `TestResolve` và `TestResolveRejectsMissingStateThroughSymlink` cho phép state chưa tạo bên trong repo | Cùng lỗi canonical hóa đường dẫn chưa tồn tại. `state.isInside` giải tổ tiên trước khi kiểm; lỗi giải đường dẫn được trả về. Giữ nguyên `NormalizePath`, `NormalizeWritePath`, RepoID và dữ liệu state hiện có. |
| Worktree mồ côi | `TestOrphanWorktrees` bỏ sót `01GONETASK` prunable | `workflow.isUnder` so cú pháp `/var` với `/private/var`. Giải đường dẫn cả thư mục gốc và đường dẫn Git, giữ kiểm ngoài thư mục; đánh dấu prunable bằng so sánh cùng đường dẫn thay vì chuỗi thô. |
| Cô lập user skill Codex trong eval | `TestCodexProfileKeepsRuntimeWithoutUserContext` bỏ sót tên symlink SKILL.md và đích `instructions.md` qua `/var` | Walker duyệt đường dẫn thật nhưng mất cách viết theo HOME khi đi qua symlink con. Truyền mọi alias trong cùng một lượt duyệt và ghi deny cả tên phát hiện qua HOME, tên vật lý và đích literal của Readlink. Thêm HOME symlink tường minh với symlink cả tệp lẫn thư mục để hồi quy trên cả Linux. |
| Fixture H8 và CLI worktree | `TestH8WriteGapIsViolation` không thực sự chèn ghi; hai worktree E2E so Origin sai | Canonical hóa định danh trong hook chèn ghi; fixture CLI dùng root vật lý do Git trả về. Giữ assertion về vi phạm và cô lập worktree. |
| Fixture custom flow | `TestFlowRunTDDEndToEnd` đọc thiếu prompt red | BSD `wc -l` thêm khoảng trắng, tạo tên `red.   2.txt`. Lọc whitespace ở số dòng trước khi ghép tên; giữ kiểm tra thân skill. |

Helper dùng chung `internal/pathutil/path.go:14` chỉ chuẩn hóa để so đường dẫn,
không phải cơ chế mở tệp an toàn chống TOCTOU. Các vùng đọc bảo mật eval vẫn giữ
kiểm tra handle riêng. Không thêm dependency hoặc thay schema/config.

`TestProcessGone` kiểm PID hiện tại, child sống, child đã ngừng trước khi reap
(zombie), và sau khi reap. `TestProcessGoneProbeError` kiểm lỗi truy vấn không cho
kết quả dương giả. `TestProcGroupKillsDescendants` chạy upstream/child thật và
kiểm `setupCmd`/`attachProc`/`kill` dừng cả cây mà không cần HTTP listener; đạt với
race detector. `TestStopKillsProcessTree` vẫn kiểm cả restart và shutdown,
đồng thời kiểm child thế hệ mới đang sống trước shutdown. Test cwd daemon nay đọc
cwd do chính child fixture báo, thay vì đọc `/proc` và skip khi thiếu trên macOS.

`TestWorktreeRemoveLockedSymlink` dùng tên có khoảng trắng, kiểm thư mục còn/mất,
đường dẫn khác có cùng basename, lỗi khi bị khóa và thành công sau unlock.
`TestCanonicalExistingAndRemovedThroughSymlink` kiểm cùng đối tượng qua alias,
tổ tiên đã xóa, tên khác và lỗi vòng symlink.

## Các vùng đã rà và giới hạn

- Process groups (`mcpgw`, `harness/codex`, `eval`) dùng `Setpgid` và kill nhóm,
  có cặp Unix/Windows. `TestMetadataCancellationKillsDescendants` đạt trước sửa;
  chưa tái hiện lỗi production/zombie Darwin nên giữ nguyên mã Codex này.
- File locks: `internal/fslock`, khóa gateway và workflow dùng flock trên Unix,
  LockFileEx trên Windows. Các test fslock đạt trên máy hiện tại.
- Dung lượng đĩa: `internal/eval/diskfree_unix.go` dùng Statfs, có Windows riêng.
  Không tái hiện thiếu dung lượng hoặc lỗi syscall Darwin.
- Bộ nhớ gateway: Darwin đã dùng `ps -A -o pgid=,rss=`; không dùng `/proc` ở
  production Darwin. Sandbox cấm `ps` (`operation not permitted`), nên chưa xác
  nhận số RSS thực tế trong phiên này; Darwin không cung cấp PSS tại đây.
- State mặc định macOS có nhánh `Library/Application Support/bint/state`.
  Fixture đường dẫn có khoảng trắng và HOME symlink được dùng cho khảo sát.
- Đã rà build tags/syscall và shell invocation; không thấy blocker build Darwin
  khác. CI hiện chạy runtime trên Ubuntu, chưa có native macOS test job. Không
  sửa `.github/workflows/**` đang được bảo vệ.
- Hạ chữ thường toàn Darwin trong các hàm Normalize hiện có vẫn là giới hạn trên
  volume phân biệt hoa thường; chưa kiểm volume đó, không thay định danh state
  dựa trên suy đoán. Không xác nhận mọi phiên bản macOS, macOS Intel runtime,
  Linux/Windows runtime, TUI tương tác dài hạn hoặc mọi harness thật.

## Ma trận kiểm chứng

Các lệnh dưới đây có tiền tố cache tạm như phần môi trường. Cross-build/vet
không được xem là bằng chứng runtime. Exit code 0 là đạt; 1 là thất bại.

| Lệnh | Target | Kết quả |
| --- | --- | --- |
| `go test -count=1 ./internal/mcpgw -run '^TestProcessGone'` | Darwin arm64 | 0; PID sống, zombie/reap, lỗi probe đạt |
| `go test -race -count=1 ./internal/mcpgw -run '^TestProcessGone'` | Darwin arm64 | 0 |
| `go test -race -count=1 ./internal/mcpgw -run '^TestProcessGone\|^TestProcGroupKillsDescendants$'` | Darwin arm64 | 0; kill cả cây thật, không cần cổng mạng |
| `go test -count=1 ./internal/gitrepo -run TestWorktree` | Darwin arm64 | 0; gồm locked alias |
| `go test -count=1 ./internal/harness/codex -run TestMetadataCancellationKillsDescendants` | Darwin arm64 | 0; baseline |
| `go test -count=1 ./internal/pathutil ./internal/state ./internal/harness/hookcore ./internal/harness/claude ./internal/gitrepo` | Darwin arm64 | 0; không dùng cache kết quả test |
| Các ca app: parallel worktree, PathHookMCPAndStats, protected path, H8WriteGap | Darwin arm64 | 0 |
| Các ca eval: CodexProfile, CodexEvalIsolatesHome, CodexEvalProfileRemovedAfterTimeout, HookGuardrailLogAndReport, HookPathOff | Darwin arm64 | 0; gồm HOME và directory symlink |
| `go test -race -count=1 ./internal/eval -run 'TestCodexProfileKeepsRuntimeWithoutUserContext\|TestCodexProfileThroughSymlinkHome'` | Darwin arm64 | 0 |
| `go test -race -count=1 ./internal/harness/codex -run TestMetadataCancellationKillsDescendants` | Darwin arm64 | 0 |
| Các ca CLI: EndToEnd, FlowRunTDDEndToEnd, WorktreeParallel, WorktreeStepwise | Darwin arm64 | 0 |
| `go test -count=1 ./internal/workflow -run 'TestOrphanWorktrees\|TestRootFor\|TestNormalizeWritePath'` | Darwin arm64 | 0 |
| `go test ./...` (agent trước review) | Darwin arm64, TMPDIR mặc định | 1; sandbox chặn loopback làm dừng sớm app/gateway; kết quả này chưa loại trừ lỗi ở các test phía sau |
| `go test ./...` (Core, verify-1.log) | Darwin arm64, TMPDIR mặc định | 1; loopback chạy được, còn ba assertion ở app/gateway so `/var` với `/private/var`, đã xử lý trong vòng review bên dưới |
| `go test ./...` (agent sau sửa review) | Darwin arm64, TMPDIR mặc định | 1; app/cli/eval/mcpcat-probe/mcpgw dừng ở các ca cần loopback; workflow đạt 186.921s; các test bị panic che khuất vẫn cần Core kiểm lại |
| `go vet ./...` | Darwin arm64 | 0 |
| `GOOS=windows go vet ./...` | Windows | 0 |
| `GOOS=linux go vet ./...` | Linux | 0 |
| `staticcheck ./...` | Darwin arm64 | 0; cache được phép ghi |
| `GOOS=windows staticcheck ./...` | Windows | 0; binary host, cache được phép ghi |
| `CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...` | Darwin amd64 | 0 |
| `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...` | Darwin arm64 | 0 |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` | Linux amd64 | 0 |

Các kiểm thử cần loopback còn bị chặn trong phiên này:

- `internal/app`: `TestMCPGatewayLogWithDaemon`,
  `TestMCPGatewayLogOldDaemon404` (httptest panic khi bind).
- `internal/cli`: `TestMCPServeGatewayOptOuts` không có gateway.json vì daemon
  không khởi động; `TestMCPGatewayEndToEnd` ghi rõ bind bị từ chối.
- `internal/eval`: `TestRestrictedRPCProfileCleanupAndRejectsUnknownTools`
  không mở được probe listener; các nhánh tiếp theo báo fixture chưa khởi động
  hoặc không tới timeout dự kiến.
- `internal/mcpcat/probe`: `TestHTTPJSONAndSSE` không tạo được httptest listener.
- `internal/mcpgw`: các test daemon/env/grants/groups không bind được cổng,
  dừng ở panic của `TestHTTPUpstreamSharedHandshake`. Chạy riêng
  `TestStopKillsProcessTree` và `TestStartDetachedNeutralCwd` cũng bị chặn ở bind,
  trước phần assertion tiến trình/cwd.

Không có thay đổi bỏ qua các test này. Log suite cuối nằm ở
`/private/tmp/bint-macos-final-test-after-review.log` (tệp tạm cục bộ).

## Quyết định triển khai và đối chiếu

Giữ chế độ shared mà Bint đã chọn trong phiếu; không tạo worktree hoặc commit
ngoài sự điều phối của Core. Dùng helper canonical dùng chung sau khi đã chứng
minh lỗi ở nhiều caller; không đổi hàm Normalize dùng để băm RepoID. Không sửa
production Codex process cancellation khi test chưa tái hiện lỗi. Không chuẩn hóa
TMPDIR toàn phiên, skip Darwin, bỏ assertion, tăng timeout hoặc nới sandbox.

Lượt kiểm tra trong bước triển khai ban đầu có một nhận xét về directory symlink
của Codex. Bổ sung fixture đã xác nhận thực sự thiếu
hai deny rule, nên xử lý như lỗi cần sửa của hợp đồng cô lập: truyền alias cùng
lượt duyệt, giữ khử trùng traversal và kiểm lại đỏ/xanh. Chưa kiểm runtime khám
phá skill của Codex thật cho trường hợp này. Chi phí nếu đánh giá sai là thêm
deny rule cho cùng một user skill; không mang thêm user context vào eval.
Các phần reviewer không kết luận (toàn suite, runtime nền tảng khác và volume
phân biệt hoa thường) được giữ là giới hạn kiểm chứng ở trên, không suy thành đạt.

## Xử lý review vòng 1

Review chính thức xác nhận hai phát hiện; các kết luận trước review không đủ
để khẳng định lỗi toàn suite chỉ do sandbox.

- **01M7CR900TFQK93Q58TT92A4TT.1 (high):** canonical hóa toàn bộ đường dẫn
  trước khi trả tên tương đối làm `migrations/001.sql -> ../src/a.go` trở thành
  `src/a.go`, bỏ sót protected path. Đã tái hiện bằng test thật, không overlay:
  `TestRelPathThroughSymlink` trả sai `README.md`/`src/new.go`, và
  `TestProtectedPathThroughSymlink` trả output rỗng cho cả Claude lẫn Codex.
  Sửa `internal/harness/hookcore/hook.go:RelPath`: kiểm đích thật có trong repo,
  sau đó tìm prefix repository trong đường dẫn gốc, giữ nguyên phần bên dưới
  cho glob/guardrail. Không đổi hợp đồng đường dẫn thoát repo. Test bao phủ
  symlink tệp, symlink thư mục chứa tệp mới, alias root, đường dẫn tương đối,
  symlink quay về root, tên hoa/thường và ca ngoài repo. Hook tích hợp xác nhận
  Claude trả `ask`, Codex trả `deny` cho tên protected qua cả tệp và thư mục.
- **01M7CR900TFQK93Q58TT92A4TT.2 (medium):** Core chạy loopback thành công và
  ghi ba lỗi thật trong `runtime/tasks/01M7CR900TFQK93Q58TT92A4TT/logs/verify-1.log`.
  `TestSetUILanguageService` so `res.Path` với alias của local.yml;
  `TestLandAndCancelHumanOnlyOperations` so `Origin` với alias repo;
  `TestStartDetachedRelativeStateDir` kiểm prefix của alias cwd trong đường dẫn
  vật lý. Hai fixture app nay lấy expected root bằng `EvalSymlinks` nhưng vẫn
  gọi dịch vụ qua đường dẫn gốc; fixture gateway so danh tính thư mục chính xác
  `<caller>/rel-state/gateway` bằng `os.SameFile`, thay vì kiểm prefix chuỗi.
  Giữ nguyên assertion Changed/Status/Worktree, gateway.json/PID và các kiểm
  human-only/land/cancel. Không sửa production chỉ để thỏa các assertion này.

Lệnh hẹp sau sửa (TMPDIR mặc định, GOCACHE như trên):

```sh
go test -count=1 ./internal/harness/hookcore ./internal/harness/claude ./internal/app -run 'TestRelPath|TestModelNameAndRelPath|TestProtectedPathThroughSymlink|TestGuardrailPreEditMergesWithProtectedPath|TestCodexProtectedPath|TestSetUILanguageService|TestLandAndCancelHumanOnlyOperations'
```

Kết quả exit 0 cho cả ba package. Hai ca app đã tái hiện đỏ trước sửa trên máy
này. Ca gateway cần Core chạy lại vì sandbox dừng trước assertion khi daemon
không mở được listener; log Core đã xác nhận nguyên nhân assertion cũ.

Kiểm lại sau sửa review: `go vet ./...`, `GOOS=windows go vet ./...`,
`staticcheck ./...`, `GOOS=windows staticcheck ./...` và hai build Darwin
amd64/arm64 trong bảng ma trận đều exit 0.
`go test -race -count=1 ./internal/harness/hookcore ./internal/app -run
'TestRelPathThroughSymlink|TestProtectedPathThroughSymlink'` cũng exit 0.
Chạy riêng `TestStartDetachedRelativeStateDir` exit 1 ở StartDetached vì
`listen tcp 127.0.0.1:0: bind: operation not permitted`, trước assertion đã sửa.
Lượt toàn suite sau sửa review có log
`/private/tmp/bint-macos-review-fixes-test.log`, exit 1 với năm package cần
loopback như bảng trên. Không dùng kết quả này để kết luận các test bị dừng sớm
đã đạt; Core cần chạy lại lệnh kiểm chứng trong môi trường cho phép bind.

Đã thử ghi quan sát mới bằng MCP `report_observation`, nhưng công cụ yêu cầu
approval trong phiên có policy `never`. Không tạo candidate qua kênh khác để
vượt chặn; quan sát và bằng chứng được giữ trong báo cáo này. Candidate tri thức
có sẵn `01M7CQYXC4PWC22186MG5FCTN8` không bị sửa.

## Xử lý review vòng 2

**01M7CR900TFQK93Q58TT92A4TT.3 (high):** giữ tên symlink như vòng 1 là cần
thiết nhưng chưa đủ: `src/alias.sql -> ../migrations/003.sql` có tên không được
bảo vệ, đích được bảo vệ. `RelPath` trả `src/alias.sql`, nên hook bỏ lọt đích.
Đây là hồi quy production đã xác nhận cho tệp tồn tại, không phải lỗi fixture.

Mở rộng `TestProtectedPathThroughSymlink` với migration thật, symlink từ `src`
vào migration, symlink thư mục chứa tệp chưa tạo, và ca âm `src/a.go`. Trước sửa,
cả Claude lẫn Codex trả output rỗng ở hai ca symlink mới (bốn subtest đỏ).
Sau sửa, hook pre-edit kiểm cả tên gọi và đích canonical; Claude trả `ask`,
Codex trả `deny`. Các ca vòng 1 vẫn đạt và tệp thông thường vẫn được phép.
Guardrail pre-edit và context theo đường dẫn cũng nhận cả hai tên; sự kiện đọc
vẫn giữ tên gọi ban đầu. Hàm `RelPath` công khai giữ hợp đồng trả tên gọi;
helper nội bộ `relPathNames` bổ sung đích canonical, kiểm containment trước
khi trả cả hai. Không thêm dialect branch, không đổi xử lý lỗi hook hoặc
đường dẫn thoát repo. Đây vẫn là policy preflight, không bảo đảm chống thay
symlink đồng thời giữa lúc kiểm và công cụ ghi.

Các bằng chứng trước vòng 2:

- Core `logs/verify-2.log`: `go test ./...` exit 0 trên macOS arm64 với TMPDIR
  mặc định; phiếu ghi confirmed=true, passed=true lúc 2026-11-14T10:55:12Z.
  Bao gồm các test loopback bị sandbox agent chặn và ba assertion đã sửa.
- Review vòng 2 tái hiện lỗi bằng Go overlay. Bản sửa lần này đưa fixture vào
  test thật của repository, không phụ thuộc tệp tạm để giữ hồi quy.

Kết quả kiểm chứng sau sửa vòng 2 được ghi dưới đây; Core sở hữu lượt chạy
`verify.command` cuối và quyết định hoàn thành tác vụ.

| Lệnh | Kết quả vòng 2 |
| --- | --- |
| `go test -count=1 ./internal/app -run '^TestProtectedPathThroughSymlink$'` trước sửa | 1; bốn subtest mới đỏ vì output rỗng |
| `go test -count=1 ./internal/harness/hookcore ./internal/harness/claude ./internal/app -run 'TestRelPath\|TestModelNameAndRelPath\|TestProtectedPathThroughSymlink\|TestGuardrailPreEditMergesWithProtectedPath\|TestCodexProtectedPath'` | 0 |
| `go test -race -count=1 ./internal/harness/hookcore ./internal/harness/claude ./internal/harness/codex ./internal/app -run 'TestRelPath\|TestModelNameAndRelPath\|TestProtectedPath\|TestGuardrail\|TestCodexProtectedPath'` | 0; package codex không có test khớp, hook Codex được kiểm tích hợp ở app |
| `go test -overlay=/private/tmp/bint-review-01M7CV4/current.json -count=1 ./internal/app -run '^TestReviewProtectedTargetViaUnprotectedSymlink$'` | 0; chạy lại nguyên ca reviewer cho cả hai harness |
| `go test -count=1 ./internal/pathutil ./internal/state ./internal/gitrepo ./internal/harness/hookcore ./internal/harness/claude` | 0 |
| `go vet ./...`; `GOOS=windows go vet ./...`; `GOOS=linux go vet ./...` | 0 từng lệnh |
| `staticcheck ./...`; `GOOS=windows staticcheck ./...` | 0 từng lệnh; binary 2026.1 (v0.7.0) |
| `CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...`; `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...`; `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` | 0 từng lệnh |
| `go test ./...` (agent vòng 2) | 1; năm package cần loopback thất bại như danh sách dưới; workflow đạt 201.725s |

Lượt suite vòng 2 trong sandbox ghi log
`/private/tmp/bint-macos-round2-test.log`. Các lỗi loopback được quan sát:

- app: `TestMCPGatewayLogWithDaemon`, `TestMCPGatewayLogOldDaemon404`.
- cli: `TestMCPServeGatewayOptOuts` (không có gateway.json),
  `TestMCPGatewayEndToEnd` (daemon báo lỗi bind).
- eval: `TestRestrictedRPCProfileCleanupAndRejectsUnknownTools` với các nhánh
  success, unknown-tool, unsafe-surface, timeout; probe không bind được nên
  fixture không khởi động, không tới bước RPC/timeout cần kiểm.
- mcpcat/probe: `TestHTTPJSONAndSSE`.
- mcpgw: `TestSecondDaemonRejected`, `TestInfoFile`, `TestAuthTable`,
  `TestAdminStopShutsDown`, `TestUpstreamEnv`, `TestUpstreamEnvDefaultsAndMissing`,
  `TestSecretsScrubbedFromStatus`, `TestReviewerDoesNotSeeWriteTools`,
  `TestDeniedCallsNeverReachUpstream`, `TestRoleResolutionFailsClosed`,
  `TestInteractiveIsProjectScoped`, `TestToolsCacheSurvivesDaemonRestart`,
  `TestExclusiveGroup`, `TestGroupMaxConcurrent`, `TestHTTPUpstreamSharedHandshake`.

Không bỏ qua test hoặc thay TMPDIR để che các lỗi này. AC-1 và AC-2 có bằng
chứng báo cáo và hồi quy; AC-3 cần Core chạy lại toàn suite sau thay đổi vòng 2,
dù lượt Core trước đó và ma trận vet/lint/build đã đạt.
