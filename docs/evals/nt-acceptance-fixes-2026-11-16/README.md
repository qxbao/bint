# NT acceptance: bản vá và bằng chứng kiểm chứng

Tác vụ `01M7HXACMQCA3GQ4V49R8ASPNW`, worktree riêng, base
`08040da602467bc3180f5708a2d19c5325a5531d`. Chưa nghiệm thu; không có chữ ký.

- MCP: app chuyển cặp harness/session tường minh hoặc `CLAUDE_CODE_SESSION_ID`
  tới shim. Fixture xuyên lớp tới spool đã qua trên host có giới hạn; sandbox từ chối socket
  loopback (EPERM). Chưa xác minh nguồn phiên Codex
  hay hoạt động harness thật. Role vẫn do gateway giải từ run binding.
- Readonly: test scanner và migration hẹp đã qua. Migration nhận hai danh sách
  cũ chính xác ở dạng flow một dòng hoặc block, giữ danh sách khác; fixture
  config.Load kiểm lớp dự án và local; preview/backup/idempotence đã kiểm trên
  repository tạm. Fixture hook ứng dụng đã kiểm một số probe cho planner và
  reviewer, gồm năm probe của báo cáo.
- Codex: test hiện có xác nhận matcher cũ bị coi là không current và matcher
  mới có `local_shell`; fixture nâng qua InstallHooks kiểm backup, metadata,
  handler khác và idempotence. Fixture hookStatus kiểm current và trust. Bốn hook
  đã khảo sát trước đó enabled/trusted; không kết luận toàn bộ hook bị hỏng.
  Chưa có payload Code Mode thật để mở rộng tên công cụ.

Kiểm hẹp đã chạy trong worktree với `GOMAXPROCS=2`,
`GOFLAGS='-buildvcs=false -p=2'`, `GOCACHE=/tmp/bint-acceptance-gocache`:
`TestReadonlyShellAllowed`, `TestMCPShimSessionIdentity`,
`TestReadonlyGuardrailFailsClosedOnBadConfig`, các `TestUpgrade`,
`TestRenderMatchesDefaults`, `TestMergeFileKeepsExtraRoles`, và ba test Codex
`TestHooksCurrentDetectsStaleMatcher`, `TestInstalledHooksMatchSupportedShellTools`,
`TestSessionEndTimeoutMatchesRealCodex` đều exit 0. Test migration mới đã đỏ
trước khi sửa và xanh sau đó. Các kiểm này không thay thế verify của phiếu.

Lượt tiếp nối: `go test ./internal/config -run '^TestUpgradeReadonly' -count=1`,
`go test ./internal/harness/codex -run '^TestInstallHooksUpgradesStalePreservingOthers$' -count=1`,
`go test ./internal/app -run '^TestReadonlyUpgradePreviewAndBackup$' -count=1`,
`go test ./internal/app -run '^TestReadonlyGuardrailProjectRules$' -count=1`,
`go test ./internal/cli -run '^TestServiceMessageCLIInventory$' -count=1` qua;
`TestMCPShimIdentityToSpool` biên dịch nhưng dừng khi mở socket loopback với
`operation not permitted` trong sandbox. Các lệnh Go dùng cùng giới hạn env ở trên.

Sau khi hoàn tất mã và review, người vận hành cần xác định revision/diff và
binary thực sự trên PATH, sao lưu binary nếu cần rollback, dùng
`bint update --source <nguồn-đã-kiểm>` không có `--pull`, rồi kiểm lại đường
dẫn và phiên bản binary. Tại repository vận hành, xem trước phạm vi cập nhật,
dùng binary mới chạy `bint init --update --hooks --no-skills` theo scope đang
dùng; lệnh này có thể cập nhật cấu hình dự án và hook của các harness đang bật.
Kiểm doctor và metadata trust sau đó; nếu định nghĩa mới chưa được tin cậy,
người dùng tự duyệt trong Codex. Khôi phục từ backup đã xác minh nếu cần, rồi
kiểm lại trust. Không có bước nào trong lượt này tự cập nhật binary hoặc trust.

Verify có giới hạn lần 2 đã qua trên worktree, gồm test phạm vi và vet/staticcheck Linux cùng GOOS=windows. Review vòng 1 không có critical/high, còn hai phát hiện đang xử lý; kiểm lại diff cuối và review vòng 2 còn chờ Core. Windows runtime và harness thật chưa được đo.

## Tiếp nối fixture và đối chiếu

Trong worktree của tác vụ tại base `08040da602467bc3180f5708a2d19c5325a5531d`,
fixture MCP nay lấy `cg_explore` từ `tools/list` của shim, gửi `tools/call`,
đợi response trước EOF và giữ kiểm số sự kiện, định danh, role và dữ liệu riêng
tư. Host đã chạy fixture cũ và báo đỏ do đóng sớm/gọi sai tên; log tại
`runtime/tasks/01M7HXACMQCA3GQ4V49R8ASPNW/logs/narrow-host/20261116T104645Z-mcp-identity-host.{stdout.jsonl,stderr.log,resources.jsonl,meta.json}`.
Fixture mới chạy trong sandbox trả EPERM khi mở socket; lần chạy lại trên host có giới hạn đã qua như ghi dưới đây.

`TestReadonlyGuardrailProjectRules` nay nạp bản `.bint/config.yml` được theo dõi
vào repo tạm và kiểm cả năm probe với planner/reviewer; `TestReadonlyShellAllowed`
kiểm các probe tương ứng trên default. `TestCodexHookStatusCurrentAndTrust` kiểm
chưa cài, matcher cũ, current với metadata trusted, modified, partial, disabled,
initial và lỗi API bằng app-server giả. Matcher lỗi được báo chưa xác minh.
Lệnh `go test ./internal/app -run '^(TestCodexHookStatusCurrentAndTrust|TestReadonlyGuardrailProjectRules)$' -count=1 -timeout 2m`
và `go test ./internal/harness/hookcore -run '^TestReadonlyShellAllowed$' -count=1 -timeout 2m`
đều exit 0 trong sandbox, cwd worktree trên, với `GOMAXPROCS=2`,
`GOFLAGS='-buildvcs=false -p=2'`, `GOCACHE=/tmp/bint-acceptance-gocache`.
`git diff --check` exit 0. Đây là kiểm hẹp; chưa phải kết quả verify trên diff cuối.

Đối chiếu TSK: 1 có wiring nhưng nguồn phiên harness thật chưa xác minh; 2 có
fixture host đã xanh, gồm role/privacy; 3–5 đã có scanner,
migration, preview và probe qua kiểm hẹp; 6–7 có fixture nâng hook/trust và bàn
giao, chưa chạy binary/hook người dùng; 8 verify.command đã dùng trực tiếp runner
giới hạn theo quyết định điều phối, cổng đã được xác nhận; 9 verify lần 2 và review vòng 1 đã có bằng chứng dưới đây, còn verify lại và review vòng 2 sau sửa review. Harness thật,
Windows thật và Code Mode không có payload vẫn chưa đo; không có chữ ký nghiệm thu.

## Kiểm chứng host và sửa ba sai lệch

Lượt host hẹp `go test -count=1 -timeout 3m -json ./internal/app -run
'^(TestMCPShimIdentityToSpool|TestMCPShimSessionIdentity|TestHookStatus.*)$'`
exit 0, không có stop_reason; explicit/environment/process/reviewer/missing,
role và privacy được kiểm bằng fixture. Log stdout, stderr, tài nguyên và metadata:
`runtime/tasks/01M7HXACMQCA3GQ4V49R8ASPNW/logs/narrow-host/20261116T105436Z-mcp-identity-host-recheck.{stdout.jsonl,stderr.log,resources.jsonl,meta.json}`.
Lịch sử đỏ của fixture trước vẫn được giữ ở trên. Đây không phải chứng cứ
harness thật chuyển định danh tới MCP.

Verify chính thức lần 1 trong cùng worktree, base `08040da602467bc3180f5708a2d19c5325a5531d`,
diff hash trước/sau `602705291b8de35470d123c2ae530c4396827bf2cb0758aad6c7a39022889886`,
exit 1, không có stop_reason; giới hạn 2 GiB, swap 0, 256 tác vụ, CPU 200%.
Env chọn lọc: GOMAXPROCS=2, GOFLAGS=-buildvcs=false -p=2, cache bền vững
`<cache-dir>`. Log stdout, stderr, tài nguyên
và metadata tại `runtime/tasks/01M7HXACMQCA3GQ4V49R8ASPNW/logs/bounded-verify/20261116T105934Z-nt-acceptance-verify.{stdout.jsonl,stderr.log,resources.jsonl,meta.json}`.
App, mcpgw, claude, codex, hookcore, config, mcpserver và tui/actions qua;
harness và cli đỏ do golden thiếu `git ls-files`, test đếm default cũ, và
README mô tả lệnh dự kiến như lệnh hiện có. Chuỗi `&&` dừng trước vet và
staticcheck; chưa có kết quả cho chúng.

Đã cập nhật đúng hai mục golden default, thay kiểm số lượng bằng so sánh snapshot
để vẫn phát hiện thay đổi cấu hình đầu vào, và mô tả README là đề xuất. Trong
worktree, với GOMAXPROCS=2, GOFLAGS='-buildvcs=false -p=2' và
GOCACHE=/tmp/bint-acceptance-gocache, `go test -count=1 -timeout 2m
./internal/harness -run '^(TestFlowGoldenHarness|TestClaudeReadonlyAllowsCurrentBintExecutable)$'`
và `go test -count=1 -timeout 2m ./internal/cli -run '^TestDocCommandsExist$'`
đều exit 0.

Verify chính thức lần 2 chạy trong cùng worktree, base trên, diff SHA-256 trước và sau đều
`e1d8306cb1c5044af8fd8f71ba98905f8831d38f5658511e1685ca6ae4719027`. Chuỗi test phạm vi,
`go vet ./...`, `GOOS=windows go vet ./...`, staticcheck Linux và GOOS=windows exit 0;
`stop_reason` null, không có sự kiện OOM hoặc chạm giới hạn PID. Cùng env và giới hạn
như lần 1. Log stdout, stderr, tài nguyên và metadata:
`runtime/tasks/01M7HXACMQCA3GQ4V49R8ASPNW/logs/bounded-verify/20261116T110744Z-nt-acceptance-verify.{stdout.jsonl,stderr.log,resources.jsonl,meta.json}`.
Review vòng 1 không phát hiện critical/high; hai finding về lời hướng dẫn cập nhật và
biên bản được sửa sau lượt verify này. Core cần chạy lại verify đã duyệt trên diff mới
và review vòng 2. Cross-check Windows không phải chạy Windows thật; harness thật,
nguồn phiên Codex, Code Mode và vận hành binary/hook vẫn mở, chưa có chữ ký.

## Kết quả cuối của phiếu sửa

Phiếu `01M7HXACMQCA3GQ4V49R8ASPNW` đã được Core finalize `done`; 24 tệp được đưa về
cây gốc, worktree riêng đã được dọn. Verify lần 3 trên diff sau sửa review hoàn tất
chuỗi test phạm vi và vet/staticcheck Linux/GOOS=windows với exit 0, không OOM hay
chạm giới hạn PID; source diff trước/sau trùng
`da8ffb8418debe6ce9efc8ecff0758dde2df8c6463e32eaad55587be5fdcbd32`.
Review vòng 2 xác nhận hai finding vòng 1 đã sửa và không có finding mới.
Đây là kết quả kiểm trên worktree; không thay thế full suite hoặc nghiệm thu harness thật.

Bằng chứng đã lược đường dẫn máy: [stdout](evidence/final-checks.stdout.txt),
[stderr và giới hạn](evidence/final-checks.stderr.txt),
[metadata](evidence/final-checks.meta.json), [review vòng 2](evidence/review-round-2.md).
Các đoạn bên trên ghi theo từng lượt triển khai; kết quả cuối nằm ở mục này.

Bản `bint` trên máy đã được build/cài từ 1524 build-input khớp cây đã kiểm;
hook Codex đã thêm `local_shell` vào hai matcher. Lần kiểm metadata gần nhất còn
hai định nghĩa PreToolUse/PostToolUse `modified`, chờ người dùng duyệt `/hooks`.
Lệnh làm mới giữ nguyên khối `.gitignore` đã sửa ngoài Bint và không dùng `--force`;
`.bint/local.yml` được giữ nguyên. Việc triển khai hook không tự trust hoặc chứng minh
nguồn session Codex được chuyển tới MCP. Các mã NT chạy thật, Code Mode và chữ ký
nghiệm thu vẫn giữ mở theo `docs/acceptance.md`.

## Tích hợp trước khi xuất bản

Khi chuẩn bị PR, bản vá được gộp với `main` mới đã có `bint iso`. Xung đột shim
được giải bằng cách giữ cả Harness/SessionID và Env=daemonEnv(), cùng hai hàm
phục vụ attribution và môi trường thật. Các test MCP xuyên lớp, resolver,
daemonEnv trong profile, readonly, hookStatus, config và CLI được chọn đã qua
trên bản kết hợp; gofmt, go mod tidy -diff, go mod verify, vet/staticcheck hai
nền tảng cũng qua.

Test golden workflow ban đầu đỏ vì hai mục step_props còn thiếu rule git ls-files.
Golden được sinh lại qua BINT_UPDATE_GOLDEN=1; diff JSON chỉ đổi agent_args của
planner và reviewer. Chạy lại không bật biến này: TestFlowGoldenRunnerBehaviour
qua. Bộ CI đầy đủ Linux race/Windows/build/lint được dùng để xác minh commit
xuất bản; kết quả hiện hành được dẫn trong PR.

Hai test evolve lock trên máy phát triển vẫn tái hiện EPERM khi open tệp; đó là
blocker môi trường đã có bộ tái hiện Python độc lập, không được biến thành skip
hoặc ErrLocked để có kết quả xanh. CI sạch kiểm toàn bộ suite riêng.
