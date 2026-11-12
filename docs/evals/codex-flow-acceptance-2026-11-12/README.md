# Nghiệm thu Codex trên HEAD — 2026-11-12

## Phạm vi và kết luận

Đối chiếu mã tại HEAD `e9ce282` và thay đổi trong working tree của tác vụ
`01M76X7ZNGP537004YW0P5JP7N`. Codex CLI hiện có là `0.167.2`; bảng adapter được đối chiếu với
`codex-cli 0.167`. Bản vá sản phẩm không thay `.bint/config.yml`, `.bint/local.yml`, auth/profile người dùng,
trust hooks hoặc ngân sách. Không sửa hai báo cáo lịch sử 2026-11-11.

Nghiệm thu fixture xác nhận init/sync/doctor/harness show và hồi quy snapshot/deny.
Vòng sửa 6 bổ sung adapter evolution Codex thực thi qua app-server cô lập: reflector và
harness proposer không có công cụ, evolver chỉ có Read/Grep/Glob do Bint xử lý.
Probe CLI thật kiểm cả `tools` và `input.additional_tools` trước khi gửi dữ liệu thật.
Model live `gpt-6.1-sol` đã hoàn tất cả no-tools và reader trên fixture/profile mới;
[thiết kế và bằng chứng](codex-restricted-adapter-round6.md) phân biệt rõ probe endpoint
fixture với inference thật. Đã sửa thêm `.24`: command tùy chọn không bị harness không dùng chặn.

Cấu hình hiện hành đã được người dùng chọn: evolver/proposer `gpt-6-astra`,
reflector `gpt-6.1-sol`. Review vòng 6 đã chạy thật cả ba factory trên fixture và
profile cô lập ([log](evidence/factory-live-review-round6.txt)); probe endpoint trong
log là fixture, còn inference là model thật. Evolution máy vẫn tắt. Fixture workflow thật đã hoàn tất với hook do người dùng trust, duyệt đường dẫn và lệnh verify; [evidence đã lược thông tin máy](evidence/workflow-live-final.json).
Các lần thử bị chặn mạng ở vòng 5 được giữ như lịch sử, không còn là kết quả mới nhất.
Review độc lập cuối do bước `b-review` của Bint Core thực hiện; implementer không tự duyệt.
Phần `.codex/config.toml` network tạm do orchestrator chuẩn bị là runtime của phiên,
không phải bản vá sản phẩm; orchestrator đã khôi phục cấu hình gốc và xoá profile tạm.

## Ma trận bước

| Bước | Kết quả hiện tại | Bằng chứng và giới hạn |
|---|---|---|
| `init` | Fixture được khởi tạo với Codex, project hooks, MCP và skill; lệnh trả mã khác 0 vì kiểm tra trust hook không đọc được `hooks/list` (EOF). | [fixture init](evidence/fixture-init-track.txt). Repository fixture nằm dưới `/tmp`; state dùng `XDG_STATE_HOME` riêng. Không tự trust hook. |
| `sync` | Đạt, mã thoát 0; xuất bản sáu skill vào `.agents/skills`. | [fixture sync](evidence/fixture-sync-track.txt). |
| `doctor` | Lệnh mã thoát 0, báo project chưa trusted và hook chưa được Codex xác minh. Đây là cảnh báo đúng với fixture chưa được người dùng trust. | [fixture doctor](evidence/fixture-doctor-track.txt). Không thao tác `/hooks`. |
| Adapter/version/capabilities | CLI `0.167.2` được phát hiện; thực thi có, MCP/hook/path-context/readonly chỉ hỗ trợ một phần; không có giới hạn USD/lượt hay `skill.activated`. | [harness show](evidence/fixture-harness-show.txt), [version](evidence/codex-version.txt), [cờ exec](evidence/codex-exec-limits.txt). |
| Skills | `sync` tạo đủ skill trong fixture. Codex đã đọc/chạy skill thật trong đợt lịch sử 11/11; native activation event không có. | Fixture hiện tại: [sync](evidence/fixture-sync-track.txt). Live lịch sử: [Codex flows 11/11](../codex-flows-2026-11-11/README.md), mục F4. Không tuyên bố Codex luôn tự chọn đúng skill. |
| MCP | Fixture đăng ký `[mcp_servers.bint]`; doctor xác nhận cấu hình, nhưng Codex không được thử gọi MCP hiện tại vì fixture chưa trusted. | [init](evidence/fixture-init-track.txt), [doctor](evidence/fixture-doctor-track.txt). MCP thật đã gọi trong eval lịch sử 11/11, mục F3; đó không phải rerun trên thay đổi này. |
| Hook fail-open | Hồi quy hiện tại qua; lỗi xử lý/ghi spool không đổi kết quả hook. | `internal/app/guardrail_hook_test.go`, `internal/harness/hookcore` tests. Codex chạy thật với hook fail-open có trong [báo cáo 11/11](../codex-flows-2026-11-11/README.md), mục F2. Fail-open hiện tại có bằng chứng hồi quy; phép live mới xác nhận nhánh deny, không giả lập lỗi hook. |
| Hook deny đường dẫn / guardrail | Codex trả `permissionDecision=deny`; [live fixture](evidence/workflow-live-final.json) đã blocked trước diff và tiếp tục sau duyệt của người dùng; hook ghi `hook.tool_denied` vào nhật ký riêng theo child run, gắn tool ID, đường dẫn và lý do. Runner đọc đúng shard sau khi hook trả đầu ra deny, gộp mọi deny, rồi dọn shard; Ingest không tiêu thụ được tín hiệu và lần đọc không quét lịch sử các run khác. Chỉ protected paths tạo yêu cầu duyệt tệp. Bao gồm `pre_edit` và `pre_bash`. | `TestCodexProtectedPathDenyWritesChildRunEvent`, `TestCodexGuardrailDeniesWriteDurableWorkflowEvents` tại `internal/app/guardrail_hook_test.go`; `TestHookDenialIsBoundToChildRunAndCarriesProtectedPath`, `TestHookDenialSurvivesConcurrentSpoolIngest` tại `internal/workflow/run_test.go`. Lỗi ghi spool không làm hook trả lỗi/chặn agent theo hợp đồng fail-open. |
| `b-req` | Fixture/schema/test hiện có; live đã chạy trong đợt Codex 11/11. | Live lịch sử: [Codex flows 11/11](../codex-flows-2026-11-11/README.md), mục F4/r02 và F5/r12. Fixture mới chạy planner Codex thật với gpt-6-astra ([evidence](evidence/workflow-live-final.json)). |
| `b-feat` | Resume giữ lựa chọn đã lưu, gồm model rỗng theo mặc định CLI; lựa chọn không bị ghi khi runner đang bận. Claude snapshot lấy model workflow khi không có model role. Snapshot rỗng của Claude không lấy cấu hình mới; role chưa chọn (kể cả snapshot rỗng từ bản cũ) lấy model cấu hình khi chuyển b-req sang b-do. B-do bỏ xác thực planner bị bỏ qua và reviewer khi --no-review. Hook deny trước diff đưa protected path vào `blocked`, duyệt đúng path rồi chạy lại implementer. | Hồi quy `TestAgentChoicesResumeKeepsSavedHarnessAndModel`, `TestAgentChoicesResumeHonorsExplicitOverrides`, `TestAgentChoicesClaudeSnapshotsWorkflowModel`, `TestAgentChoicesSingleWorkflowIgnoresUnusedDisabledRole`, `TestAgentChoicesUnstartedRolesUseConfiguredModel`, `TestRunWorkflowSkipsDisabledRolesThatWillNotRun`, `TestRunWorkflowSnapshotsPlannerAfterPlanningOverrides`, `TestHarnessAgentsSavedEmptyClaudeModel`, `TestHarnessAgentsWithSavedChoicesUsesSavedCodexModel`, `TestHarnessAgentsWithSavedEmptyModelDoesNotPickUpNewConfig`, `TestBusyRunnerDoesNotPersistRejectedAgentChoices`, `TestCodexHookDenyBlocksBeforeDiffThenApprovalResumes`; live lịch sử có r03 và C3/C4 trong hai báo cáo 11/11. Hồi quy dùng fake agent/event không được tính live; [fixture mới](evidence/workflow-live-final.json) chạy implementer Codex thật. |
| `b-review` | Bước chỉ đọc được hậu kiểm theo chương trình; capability Codex là partial. | Source/test hiện tại: `internal/workflow/run.go`, `internal/workflow/readonly_knowledge_test.go`; live lịch sử Codex 11/11 mục F4/r04 và mixed flow A/B. Fixture mới chạy reviewer Codex thật gpt-6.1-sol ([evidence](evidence/workflow-live-final.json)). |
| Finalize/verify | Finalize giữ cổng xác nhận lệnh verify và duyệt; Codex không báo USD/max turns nên hai mức này chỉ là hướng dẫn, không phải enforcement. | `internal/workflow/run.go`, `docs/workflows.md`; live lịch sử có task done trong báo cáo mixed 11/11. Fixture mới được người dùng duyệt lệnh verify, chạy qua và finalize tới done ([evidence](evidence/workflow-live-final.json)); verify phiếu chính do Core chạy. |
| Blocked/interrupt/resume | Resume không còn bị chặn ở trạng thái planner `interrupted`; lựa chọn agent mới được lưu theo role. Deny path mới đưa task vào `blocked`, exact-path approval đặt điểm chạy lại implementer. | Regression hiện tại ở `internal/app/run_selection_test.go`, `internal/workflow/run_test.go`. Live Ctrl-C/resume thật của Codex có tại báo cáo 11/11 mục F5/r10-r12; deny → approve → continue thật có tại [mixed 11/11](../claude-codex-mixed-2026-11-11/README.md), mục C. Fixture mới đã SIGINT, đổi model cấu hình, resume không cờ giữ snapshot, deny trước diff, người dùng duyệt và resume tới done ([evidence](evidence/workflow-live-final.json)). |
| Eval/profile | Helper cô lập được adapter evolution tái sử dụng; auth sao chép 0600, profile và state mới, cleanup trên thành công/lỗi/timeout. Smoke model restricted hiện tại đã qua; chưa chạy lại eval strict 3-arm. | `internal/eval/codex_profile_test.go`, `TestRestrictedRPCProfileCleanupAndRejectsUnknownTools`; [live profile_removed](evidence/codex-restricted-live-round6.txt). Eval 3-arm thật nằm ở báo cáo lịch sử 11/11 mục F6. Profile riêng không thay sandbox OS. |
| Evolver | Factory dùng CodexRestrictedAgent với đúng model/binary role, chỉ đăng ký Read/Grep/Glob do Bint xử lý. Factory evolver chạy model live gpt-6-astra theo cấu hình hiện hành và đọc canary trong fixture. | `TestCodexEvolutionFactories`, `TestCodexReadersConfineFilesAndBoundOutput`; [live adapter](evidence/codex-restricted-live-round6.txt), [model cấu hình](evidence/codex-configured-evolver-round6.txt), [provider IDs](evidence/provider-model-ids-round6.json). [Factory live](evidence/factory-live-review-round6.txt); không bật evolution máy. |
| Reflector / harness proposer | Đã mở adapter Codex không công cụ với catalog tạm loại native tools, environments rỗng và probe mỗi lượt. Factory reflector gpt-6.1-sol và proposer gpt-6-astra đã chạy inference thật trong fixture/profile cô lập ([log](evidence/factory-live-review-round6.txt)). | `TestCodexEvolutionFactories`, `TestRestrictedCodexToolSurface`, `TestRestrictedRPCProfileCleanupAndRejectsUnknownTools`; [thiết kế vòng 6](codex-restricted-adapter-round6.md), [tool surfaces từ CLI thật](evidence/codex-tool-surface-round6.json), [model live](evidence/codex-restricted-live-round6.txt). Chưa chạy chu kỳ sinh/đánh giá đề xuất thật. |
| Propose/deferred | Mọi đường gọi trực tiếp và deferred truyền `Cases`/`RepoDir`; kiểm rò rỉ bài học dựng trước reflector. | `internal/app/evolvesvc/commands.go`, `internal/app/evolvesvc/deferred.go`, `internal/evolve/deferred.go`, test ở `internal/evolve/lesson_e2e_test.go`. Không chạy do evolution tắt trong cấu hình máy. |

### Quy tắc resume

`state.json.agent_choices` lưu harness/model hiệu lực theo role đã được chọn; role chưa chọn không có key. Key cũ có Harness rỗng được coi là chưa chọn, còn Harness có giá trị và Model rỗng là snapshot mặc định CLI hợp lệ trên cả Claude/Codex/Antigravity. Resume
không có cờ dùng snapshot đó; `--harness` ghi đè các role còn có thể chạy, `--role` ghi đè role chỉ định,
`--model` ghi đè model các role còn có thể chạy. Lựa chọn được phân giải sau Classify và áp cờ lập kế hoạch, trước khi claim runner; `--depth` hay yêu cầu protected-plan được phản ánh trong snapshot. Planner đã bỏ qua và reviewer khi `--no-review` không bị xác thực; snapshot cũ của chúng được giữ. Harness của role còn có thể chạy vẫn phải có trong `harnesses.enabled`; nếu bị tắt,
resume báo lỗi thay vì âm thầm đổi harness. Task cũ chưa có snapshot lấy lựa chọn từ cấu hình đang
có rồi lưu lại. Cổng người duyệt, workflow, scope và giới hạn chi phí không đổi.

## Cập nhật sau review vòng 8

Phát hiện .32 được tái hiện bằng phép hoán đổi thư mục và symlink đồng thời:
Glob đã lộ tên tệp trong `.bint/evals`. Reader nay duyệt bằng handle của từng
thư mục đã đối chiếu danh tính, đọc theo lô và kiểm context cùng giới hạn số mục;
`TestReviewGlobConcurrentDirectoryReplacement` kiểm lại phép tái hiện này.
Phép kiểm năm lần qua trên Linux; runtime Windows chưa được kiểm. Việc dùng handle
cũng áp dụng cho đường duyệt của Grep; việc đọc nội dung vẫn kiểm danh tính tệp.
Sau sửa, `GOCACHE=/tmp/bint-gocache go test ./...` qua; `go vet ./...`
và staticcheck v0.7.0 qua trên Linux và với `GOOS=windows` (staticcheck dùng
`XDG_CACHE_HOME=/tmp/bint-staticcheck-cache`). `git diff --check` qua.

Attempt-2 cũ tới `done` nhưng child chưa ghim binary nên không dùng chứng minh snapshot.
Fixture mới `01M77HBTHRB8B06TT2VXGGRH34` dùng binary ghim SHA-256
`a04e193e60d31c5756d68afdd8076eb4302598deafa31852c1a3272fc40a0708` cho parent,
child và hook; mã nguồn không đổi từ build. Sau SIGINT, cấu hình implementer fixture
đổi từ `gpt-6.1-terra` sang `gpt-6.1-sol`, nhưng resume không cờ vẫn dùng snapshot
`gpt-6.1-terra`. Native hook deny README trước diff tạo `blocked`; người dùng duyệt
README và lệnh verify, rồi resume qua verify, review và finalize tới `done`. Năm log
actor có hash binary và model yêu cầu trong [bản evidence đã lược](evidence/workflow-live-final.json).
Review vòng 9 không có critical/high; medium .34 về nhánh duyệt nay có bằng chứng trên.

## Kiểm chứng mã hiện tại

Sau review vòng 6, factory Codex chọn model theo thứ tự CLI, role, model chung
của harness, rồi mặc định profile. Reader Unix mở không chặn trước khi xác nhận
tệp thường, kể cả khi đường dẫn bị thay bằng FIFO đồng thời; reader kiểm tra
context khi duyệt và đọc. `TestCodexEvolutionModelPrecedence`,
`TestCodexReaderRejectsFIFOWithoutWriter`, `TestCodexReaderConcurrentFIFOReplacement`
và `TestCodexReaderHonorsCanceledContext` đã chạy hồi quy 5 lần.
Windows mới được kiểm tra tĩnh, chưa chạy runtime reader Windows.

Trên mã sau sửa review vòng 6, `GOCACHE=/tmp/bint-gocache go test ./...` qua ([log](evidence/go-test-round7.txt)); `go vet ./...` và staticcheck v0.7.0 đều qua trên Linux và `GOOS=windows`, với `GOCACHE=/tmp/bint-gocache` và staticcheck dùng `XDG_CACHE_HOME=/tmp/bint-staticcheck-cache`. `git diff --check` qua. Lần staticcheck đầu dùng cache mặc định chỉ đọc và thất bại; chạy lại với cache tạm đã qua.

Bằng chứng vòng sửa 6 trước đó: [manifest lệnh, exit code và SHA-256](evidence/verification-round6.json).
Lần chạy đầu đỏ vì `TestEvolveEndToEnd` vẫn yêu cầu Codex reflector bị từ chối; cập nhật
assert cho adapter đã hỗ trợ, không bỏ kiểm tra proposal/cổng chỉ người duyệt.
Lượt sau phát hiện exclusion inventory CLI của chuỗi từ chối cũ đã dư; chỉ xoá exclusion không còn khớp, giữ nguyên cổng kiểm ([lần đỏ](evidence/go-test-inventory-failure-round6.txt),
[hồi quy qua](evidence/inventory-regression-round6.txt)). Staticcheck lần đầu bắt ST1005 ở chuỗi lỗi mới; đã chuẩn hoá và chạy lại.
Log lần đầu được giữ tại [go-test-first-round6](evidence/go-test-first-round6.txt) và
[staticcheck-first-round6](evidence/staticcheck-first-round6.txt).
Hồi quy mới gồm factory/command/probe/reader/cleanup; [factory/reader/probe/cleanup 5 lần](evidence/restricted-regressions-round6.txt),
[factory/command 5 lần](evidence/command-regressions-round6.txt) và [CLI regression](evidence/cli-regressions-round6.txt). Hồi quy snapshot/TUI vòng 5
vẫn nằm trong full suite.

| Lệnh | Kết quả |
|---|---|
| `GOCACHE=/tmp/bint-gocache go test ./...` | Qua, exit 0. [log vòng 6](evidence/go-test-round6.txt) |
| `GOCACHE=/tmp/bint-gocache go vet ./...` | Qua, exit 0. [log Linux](evidence/vet-linux-round6.txt) |
| `GOOS=windows GOCACHE=/tmp/bint-gocache go vet ./...` | Qua, exit 0. [log Windows](evidence/vet-windows-round6.txt) |
| `GOCACHE=/tmp/bint-gocache XDG_CACHE_HOME=/tmp/bint-staticcheck-cache /tmp/bint-tools/staticcheck ./...` | Qua, exit 0. [log Linux, v0.7.0](evidence/staticcheck-linux-round6.txt) |
| `GOOS=windows GOCACHE=/tmp/bint-gocache XDG_CACHE_HOME=/tmp/bint-staticcheck-cache /tmp/bint-tools/staticcheck ./...` | Qua, exit 0. [log Windows, v0.7.0](evidence/staticcheck-windows-round6.txt) |

## Đối chiếu tiêu chí

Review độc lập vòng 9 kiểm toàn diff và mã chưa theo dõi, không phát hiện critical/high
hoặc bug mã mới. [Checks vòng 9](evidence/review-round9-checks.json) ghi full suite,
hồi quy reader/race 5 lần, vet và staticcheck Linux/Windows đều exit 0. Sau review chỉ
cập nhật tài liệu; Core sẽ verify và review lại theo workflow.

| AC | Tự đánh giá | Căn cứ |
|---|---|---|
| AC-1 | Đạt | Ma trận ghi mức bằng chứng và giới hạn cho từng bước. |
| AC-2 | Đạt | Hồi quy snapshot/deny, reader và ba factory live; hook giữ fail-open và cổng người duyệt. |
| AC-3 | Đạt | [Workflow live ghim binary](evidence/workflow-live-final.json) hoàn tất interrupt, deny, human approval, resume và finalize; factory live và hồi quy có nhãn riêng. |
| AC-4 | Đạt | [Checks vòng 9](evidence/review-round9-checks.json) qua, review độc lập không còn critical/high; báo cáo đã đồng bộ live cuối. Core tiếp tục verify/review thay đổi tài liệu. |

## Chưa kiểm chứng / còn mở

- Model cũ `gpt-6.1-astra` bị từ chối trước inference; người dùng chọn `gpt-6-astra` và factory đã chạy live.
- Trust hook do người dùng thực hiện trên fixture; không suy ra mọi môi trường trust được hoặc mọi thao tác UI đã được kiểm.
- Windows/macOS Codex runtime chưa chạy; Windows chỉ được kiểm bằng vet/staticcheck.
- Chưa chạy lại eval strict 3-arm hoặc chu kỳ sinh/đánh giá evolution thật; evolution máy vẫn tắt. Không tuyên bố proposal vượt test độc lập.

## Ghi chú ngoài các phát hiện review đã giao

Tác vụ bổ sung `01M77MTS8RPSP3MS0QCXNYW5YE` đã tái hiện lỗi agent gửi đồng thời
`--no-plan` và `--allow` thêm protected scope trên task có sẵn: bước đầu sai là `b-feat`.
Runner nay áp edit hợp lệ trước khi đánh giá routing trong cùng callback cập nhật; hồi quy
cho thấy `b-req` được chọn và snapshot planner lấy model cấu hình. Xem
[bằng chứng trước/sau](evidence/routing-protected-scope-2026-11-12.txt); đây là fixture,
không phải Codex live. Scope guard và quyền duyệt đường dẫn vẫn độc lập. Warning candidate
`01M779VDA0GZ4TMSMX8E0VDAZ5` đã được cập nhật, chưa tự duyệt. Review độc lập do Core thực hiện.
