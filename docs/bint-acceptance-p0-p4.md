# Biên bản nghiệm thu P0–P4

> **Bản ghi lịch sử:** Biên bản dưới đây phản ánh trạng thái tại commit được kiểm tra. Trạng thái nghiệm thu hiện tại và tiêu chí còn mở xem ở [docs/acceptance.md](acceptance.md); trạng thái triển khai ở [docs/status.md](status.md).

Ngày kiểm tra: 2026-11-10. Phiên bản được kiểm tra: `c52633419d8c39b2e650191f46dcbc6e5a4db281` (`P4: Workflow tối thiểu`).

Đối chiếu với `docs/bint-plan.md`, đặc biệt mục 16 và các mục được dẫn chiếu trong từng giai đoạn. Báo cáo này thay thế nhận định trên phiên bản P3 trước khi cập nhật main. Không thay đổi mã nguồn hoặc đánh dấu hoàn thành kế hoạch.

## Cập nhật sau khi sửa lỗi (2026-11-10)

A1, A2, A3 đã được sửa kèm kiểm thử; A4 không còn tái hiện ở HEAD (test đã có nhánh theo `runtime.GOOS`). Đã chạy lại: `CGO_ENABLED=0 go build`, `go vet ./...`, `go test -count=1 ./...` và `go test -race -count=1 ./...` đều PASS (123 test, 30 package).

| Mục | Trạng thái | Sửa đổi và kiểm thử |
|---|---|---|
| A1 | Đã đóng | `Env.UnapprovedChanges` (`internal/workflow/ops.go`) tính lại diff so với base tại điểm chuyển sang done; `TestDoneRechecksProtectedPathsFromDiff` |
| A2 | Đã đóng | `max_turns` của b-do là tổng ngân sách lượt, truyền phần còn lại xuống bước, hết thì `blocked` (`internal/workflow/run.go`); `TestBDoMaxTurnsIsTotalBudget`, `TestBDoMaxTurnsFromPerRun`. Số lượt đã dùng nằm trong bộ nhớ của lượt điều phối, `run resume` đếm lại từ 0 |
| A3 | Đã đóng | `EconomyFilter` loại mục thiếu `origin.commit`, commit không phải tổ tiên của base, hoặc `created_at` sau commit base; mở rộng `TestEconomyFilter` |
| A4 | Không còn tái hiện | `internal/harness/claude` PASS ở HEAD |

Chưa đổi: các điều kiện 3–5 ở cuối tài liệu (baseline P1, gate 1/2, quyết định fingerprint và tín hiệu còn thiếu) cần agent thật và repository thử nghiệm. P1–P3 vẫn chưa đủ bằng chứng; P4 chỉ còn thiếu dữ liệu thực tế, không còn lỗi A1/A2 đã biết. Phần dưới là kết quả của lần kiểm tra ban đầu.

## Kết luận

**Chưa đủ điều kiện ký nghiệm thu toàn bộ P0–P4.** P4 đã được triển khai trên main mới, nhưng còn lỗi thực thi giới hạn. P1 chưa có baseline thực tế; hai cổng quyết định của P2/P3 mới chốt ngưỡng, chưa chạy đo.

| Giai đoạn | Đánh giá chức năng | Kết luận nghiệm thu |
|---|---|---|
| P0 | Các luồng chính qua kiểm thử, gồm init lặp lại và nhiều worktree | Có điều kiện: cần xử lý test cài hook đang thất bại trên Linux |
| P1 | Có bộ khai thác/chạy/chấm/báo cáo; economy thiếu kiểm tra nguồn gốc theo thời gian | Chưa đạt: lỗi A3 và thiếu baseline trên repository thử nghiệm |
| P2 | Bootstrap tối giản và CLI tri thức đã có kiểm thử đầu-cuối | Chưa đủ bằng chứng: chưa qua cổng quyết định 1 |
| P3 | FTS5, truy xuất, MCP, hook, fingerprint, tín hiệu và chẩn đoán đã có | Chưa đạt đầy đủ: chưa qua cổng 2; còn sai khác và tín hiệu chưa triển khai |
| P4 | Chuỗi b-do, task ID, artifacts, review, resume, overlays đã có kiểm thử | Chưa đạt: lỗi A1 và A2 về đường dẫn bảo vệ và số lượt |

“Có chức năng” chỉ xác nhận mã và kiểm thử tương ứng tồn tại/hoạt động trong phạm vi đã chạy. Không thay thế bằng chứng hiệu quả trên tác vụ thực tế.

## Kiểm tra đã thực hiện

Môi trường: Linux amd64, Go 1.25.1, có gcc. Các lệnh dưới đây được chạy lại sau khi HEAD chuyển sang `c526334`.

| Lệnh | Kết quả |
|---|---|
| `CGO_ENABLED=0 go build -o /tmp/bint-acceptance-p0-p4 ./cmd/bint` | PASS, mã thoát 0 |
| `go vet ./...` | PASS, mã thoát 0 |
| `go test -count=1 ./...` | FAIL, mã thoát 1; chỉ package `internal/harness/claude` thất bại |
| `go test -race -count=1 ./...` | FAIL, mã thoát 1; cùng test thất bại, không thấy báo cáo data race |
| `go test -count=1 ./internal/harness/claude -run '^TestInstallPreservesAndIsIdempotent$'` | FAIL, tái hiện độc lập lỗi A4 |
| Probe CLI trên repository tạm: sửa migration rồi `task status … done` trong chế độ agent | Cho phép `done`, mã thoát 0; xác nhận lỗi A1 |
| Hai probe Go bổ sung, nạp qua `go test -overlay` từ `/tmp` | Hai assertion thất bại như mô tả A2/A3 |

Các package có test còn lại đều PASS trong cả lượt thường và race, bao gồm `internal/app`, `internal/cli`, `internal/eval`, `internal/skills`, `internal/task`, `internal/workflow`. Đặc biệt đã chạy các test CLI đầu-cuối cho workflow, đường dẫn bảo vệ, MCP và bootstrap.

Các kiểm thử agent hiện dùng fake agent hoặc script cục bộ. Không chạy mô hình trả phí, không chạy gate bằng agent thật, không kiểm chứng tương thích trên Windows/macOS. Các lỗi build của lượt đang chạy đúng lúc người dùng cập nhật main bị loại khỏi kết quả vì có thể đọc lẫn hai phiên bản mã.

## Phát hiện cần xử lý

### A1 — Cao: lệnh task trực tiếp có thể hoàn thành thay đổi ở đường dẫn bảo vệ mà chưa duyệt

Vị trí: `internal/workflow/ops.go:240`, `internal/task/status.go:112`.

`SetStatus(..., done)` gọi `DoneBlockers`, nhưng không tính lại danh sách tệp đã sửa và không gọi `CheckScope`. `DoneBlockers` chỉ kiểm tra `State.PendingPaths`, vốn được điền trong luồng `Runner.checkScope` sau b-feat. Khi dùng CLI task trực tiếp, danh sách này có thể luôn rỗng.

Đã tái hiện bằng binary build từ commit được kiểm tra, trong repository tạm có cấu hình mặc định bảo vệ `migrations/**`, không cài hook:

1. `bint task new 'Update app.txt' --criteria 'requested change applied' --json`.
2. Đặt `BINT_AGENT=1`; `bint task status <id> in_progress`.
3. Sửa `migrations/001.sql`.
4. `bint task criteria <id> 1 met --evidence 'agent claims acceptance'`.
5. `bint task status <id> done` trả **0**, trạng thái **done**, không `task approve`, không `--force`.

Phạm vi: lỗi ở cổng xác nhận hoàn thành của CLI task trực tiếp. Luồng `bint run` đã có kiểm tra sau b-feat và bài test tương ứng qua. Không có hook thì việc chặn sửa trước thao tác vốn là giới hạn đã công bố; lỗi này là tiếp tục xác nhận `done` mà không kiểm tra diff cần duyệt.

Điều kiện đóng: kiểm tra phạm vi/bảo vệ từ diff hiện tại tại điểm chuyển sang done; chứng minh cả CLI task trực tiếp và Runner đều từ chối khi chưa có phê duyệt phù hợp.

### A2 — Cao: `limits.workflows.b-do.max_turns` bị bỏ qua

Vị trí: `internal/workflow/run.go:376`, `internal/workflow/agent.go:38`.

Runner có lấy giới hạn của b-do vào `runState.limit`, nhưng chỉ dùng phần chi phí cho ngân sách tổng. Khi tạo agent, `maxTurns` chỉ lấy từ `ForWorkflow(step)` cho b-req/b-feat/b-review. Vì vậy cấu hình riêng cho b-do không truyền xuống bất kỳ bước nào; cũng không cộng/trừ `AgentResult.NumTurns` để quản lý tổng.

Probe đặt `limits.workflows.b-do.max_turns = 3`, giữ nguyên giới hạn từng bước ở mặc định, rồi chặn tại `AgentFactory` để ghi tham số. Cả ba bước nhận `maxTurns = nil`; workflow vẫn đi tới `done`.

Đây là lỗi ở Runner trước khi chọn adapter, không chỉ là hạn chế của `--agent-command`. CLI `run --help` còn mô tả giới hạn của b-do là tổng mọi bước.

Điều kiện đóng: xác định và thực thi giới hạn số lượt cho cả b-do, truyền ngân sách lượt còn lại xuống bước, lưu số lượt đã dùng và dừng khi hết; kiểm thử cả cấu hình riêng b-do và mặc định per_run.

### A3 — Trung bình: economy không loại mục có nguồn gốc sau commit gốc

Vị trí: `internal/eval/knowledge.go:169`, yêu cầu ở `docs/bint-plan.md:1195`.

`EconomyFilter` kiểm tra fingerprint tại base và ancestry của từng `Evidence.Commit`, nhưng không kiểm tra `Item.Origin.Commit`, `Origin.RunID` hay thời điểm tạo mục. Một kết luận sinh sau lời giải vẫn được giữ nếu dẫn bằng chứng tới vùng mã cũ không thay đổi.

Probe tạo base, sau đó tạo commit hậu duệ với README mới; mục tri thức có `Origin.Commit` trỏ hậu duệ, `CreatedAt` ở tương lai, còn bằng chứng trỏ hàm cũ tại base và không có `Evidence.Commit`. Kết quả: mục vẫn được giữ, assertion yêu cầu loại mục thất bại.

Chế độ economy đã được gắn nhãn chỉ để sàng lọc, nên phát hiện này không chứng minh strict bị lỗi. Tuy nhiên, nó không đáp ứng quy tắc loại mục tạo từ lượt chạy/commit sau base trong kế hoạch.

Điều kiện đóng: kiểm tra nguồn gốc tạo mục theo thời điểm/quan hệ commit; quy định rõ cách xử lý khi không chứng minh được thời điểm, bổ sung trường hợp bằng chứng cũ nhưng kết luận mới.

### A4 — Trung bình: bộ test chuẩn chưa xanh do giả định đường dẫn theo hệ điều hành

Vị trí: `internal/harness/claude/install_test.go:31`, `internal/harness/claude/install_test.go:49`, `internal/harness/claude/install.go:67`.

Test đưa đường dẫn `C:\Program Files\bint\bint.exe`, nhưng kỳ vọng JSON chứa `C:/Program Files/bint/bint.exe`. Hàm dùng `filepath.ToSlash`; trên Linux, dấu `\` không phải dấu phân cách bản địa nên không được đổi. Đầu ra thực tế vẫn có dấu ngoặc kép quanh đường dẫn.

Đây là thất bại về giả định chuẩn hoá đường dẫn trong test/contract đa nền tảng; chưa có bằng chứng rằng hook với đường dẫn Linux thật bị hỏng. Không nên kết luận thông báo “command not quoted” đồng nghĩa mất quoting trong sản phẩm.

Điều kiện đóng: làm rõ contract chuẩn hoá đường dẫn và sửa test hoặc hàm phù hợp; chạy lại đầy đủ suite thường và race.

## Đối chiếu từng hạng mục

Ký hiệu: **Có** = có triển khai và bằng chứng kiểm thử phù hợp; **Một phần** = có khoảng trống/sai khác; **Thiếu bằng chứng** = chưa có phép đo thực tế theo kế hoạch.

### P0 — Nền tảng CLI và ghi nhận lượt chạy

| Hạng mục kế hoạch | Đánh giá | Bằng chứng / khoảng trống |
|---|---|---|
| Go module, cấu trúc package | Có | Build Go thuần qua |
| CLI và quản lý lỗi | Có | `internal/cli/root.go`, `TestEndToEnd` |
| Config schema v1 | Có | `internal/config`, kiểm tra schema, khoá lạ, nạp cấu hình nhiều lớp |
| Trạng thái ngoài repo theo git-common-dir | Có | `internal/state`, `TestParallelWorktreesShareOneDatabase` |
| SQLite, migration, FTS5 không cần CGO | Có | `internal/store`, migration 0001–0004, `TestFTS5`, build CGO=0 |
| init và doctor | Có | `internal/app/app_test.go`, CLI E2E |
| Đồng bộ an toàn, backup, khối AGENTS.md | Có | `internal/fsafe`, `TestInitIdempotentAndPreservesUserEdits` |
| Spool, ingest, dedup event_id | Có | `internal/spool`, `internal/runlog`; test ingest đồng thời |
| Hook đầu tiên: phiên, tệp sửa, lệnh test | Một phần | Luồng chính qua; test cài hook chuẩn còn lỗi A4 |
| CLI trên nhiều trạng thái repo/worktree | Có | App/CLI E2E, repo chưa init, linked worktree |

Test đa worktree tạo 4 phiên, mỗi phiên 19 sự kiện hợp lệ, cố ý gửi hook trùng và nạp từ hai worktree song song; kiểm tra cùng DB, đủ 4 lượt, đúng số sự kiện và spool hết. P0 có bằng chứng tốt cho tiêu chí chức năng, nhưng chưa ký trọn vẹn khi suite chuẩn vẫn đỏ.

### P1 — Khung đánh giá

| Hạng mục kế hoạch | Đánh giá | Bằng chứng / khoảng trống |
|---|---|---|
| eval mine từ lịch sử Git | Có | `internal/eval/mine.go`, CLI eval E2E |
| Môi trường cô lập tại base | Có | `internal/eval/workdir.go`: clone tách biệt từ base, bỏ remote/lịch sử tương lai; khác phương án worktree nhưng giữ mục tiêu cô lập |
| Viết lại đề và duyệt tác vụ | Có | `DraftPrompt`, `CheckLeak`, approve và kiểm chứng fail→pass |
| Nhiều nhánh/lần lặp, một harness | Có | `TestMineVerifyRunReport`, `TestBintAndIdealArms`; harness thật chưa đo |
| Ước tính chi phí/cỡ mẫu | Có | `EstimateRun`, `PairedSampleSize`, CLI estimate |
| Báo cáo theo cặp và CI | Có | `BuildReport`, bootstrap CI, độ dao động trong từng tác vụ |
| strict và economy chống rò rỉ | Một phần | Strict có test dùng tri thức tại base; economy lỗi A3 |
| Baseline none trên repository thử nghiệm | Thiếu bằng chứng | README nêu chưa đo; fixture/script không phải benchmark thực tế |

Chưa đáp ứng nghiệm thu P1: chưa có tập tác vụ thực tế đủ lớn trên ít nhất một repository, baseline bằng agent thật và báo cáo dao động đi kèm.

### P2 — Bootstrap tối giản

| Hạng mục kế hoạch | Đánh giá | Bằng chứng / khoảng trống |
|---|---|---|
| Static discovery | Có | `internal/inspect`, `TestInspect` |
| Tệp tri thức, ULID, nguồn gốc, bằng chứng | Có | `internal/knowledge`, `internal/ulid` |
| b-bootstrap ưu tiên quy tắc bất thường/điểm dễ sai | Có | Skill nhúng, CLI bootstrap, snapshot bằng fake agent |
| Kiểm tra đường dẫn, ký hiệu, ví dụ | Có | `Verifier`, các test knowledge/fingerprint |
| approve và bảng chuyển trạng thái | Có | `Transition`, chặn agent tự duyệt rule, CLI E2E |
| add và review theo lô | Có | CLI knowledge E2E |
| Sinh khối AGENTS.md và nhập lại từ CLAUDE.md | Có | Render/publish và CLI knowledge E2E |
| Đo minimal so với none | Thiếu bằng chứng | `docs/gate-1.md:85`: repo thử nghiệm chưa có; kết quả chưa chạy |

Cổng 1 yêu cầu tối thiểu 2 repository khác hệ sinh thái, mỗi repo ít nhất 20 tác vụ test đã fail→pass, 3 lần lặp/nhánh, strict; CI95 minimal−none không thấp hơn −0,05 và chi phí hoặc số lượt giảm ít nhất 10% với CI phù hợp. Có ngưỡng đã chốt không đồng nghĩa đã qua cổng.

### P3 — Context Engine và vòng đời tri thức

| Hạng mục kế hoạch | Đánh giá | Bằng chứng / khoảng trống |
|---|---|---|
| FTS5 dẫn xuất và index rebuild | Có | `internal/kindex`, CLI context E2E |
| Truy xuất theo mô tả và đường dẫn | Có | `internal/ctxengine`, tests task/paths |
| MCP sáu công cụ | Có | `internal/mcpserver`, CLI JSON-RPC E2E |
| Hook theo đường dẫn, timeout | Có | `RunClaudeHook`, `TestPathHookMCPAndStats`; không lặp mục trong phiên |
| Fingerprint ký hiệu bằng tree-sitter và vùng dòng | Một phần | Go dùng go/parser; ngôn ngữ khác heuristic; có test vùng dòng dịch chuyển. Sai khác đã công bố ở gate-2, chưa thực hiện tree-sitter như kế hoạch |
| Xác thực lại theo rủi ro | Có | `RevalidateKnowledge`, `Transition`; loại rủi ro cao cần người duyệt lại, agent chỉ đưa nhận định |
| Ghi truy xuất và trích dẫn | Có | `context_usages`, `knowledge_citations`, test thống kê và MCP |
| Tín hiệu kết quả và quét hoàn tác | Một phần | Tests/rating/commit/revert có; P4 bổ sung verify/criteria/review. Tín hiệu người dùng sửa lại diff trong phiên vẫn chưa thu (README:330) |
| Nhánh ideal và chẩn đoán | Có | `internal/eval/arms.go`, `gate2.go`, unit/integration tests |
| Đo bint đầy đủ so với minimal | Thiếu bằng chứng | `docs/gate-2.md:87`: chưa repo thử nghiệm, kết quả chưa chạy |

Cổng 2 còn yêu cầu cổng 1 đã qua (hoặc áp dụng phương án thu hẹp được chỉ định), CI95 bint−minimal có cận dưới >0, chi phí tăng không quá 25%, và ít nhất 80% lượt bint có truy xuất ghi nhận. Chưa có dữ liệu để kết luận các điều kiện này.

### P4 — Workflow tối thiểu

| Hạng mục kế hoạch | Đánh giá | Bằng chứng / khoảng trống |
|---|---|---|
| Skill gốc bất biến, điểm neo, lớp phủ | Có | `internal/skills/overlay.go`, tests append/prepend/replace và xung đột |
| b-req, b-feat, b-review, b-do tối thiểu | Có | Bốn skill nhúng và `internal/workflow`; CLI workflow E2E |
| Task artifacts và state machine | Có | `internal/task`, tài liệu và run cha/con gắn task ID |
| Tiêu chí chấp nhận gắn tín hiệu kết quả | Một phần | Verify/criteria/review được ghi nhận, chặn thiếu bằng chứng; cổng done còn lỗi A1 |
| Lỗi và gián đoạn | Có | `TestInterruptAndFailure`, reconcile runner chết, resume |
| Nâng cấp core và áp lại overlays | Có | `TestInstallAppliesOverlayAndKeepsOldOnConflict`; kiểm thử mô phỏng mất neo và giữ bản cũ |
| Phiếu tác vụ và giới hạn vận hành | Một phần | Classify, scope, verify, cost có; lỗi A1/A2. Runner kiểm tra protected paths sau b-feat, không sandbox trước khi ghi |
| Review thành tri thức | Có | `knowledge add --from-review`, CLI E2E; mục do agent tạo vẫn candidate |

Luồng chức năng chính đã được kiểm chứng: Classify → b-req → b-feat → verify → b-review → done; 4 lượt cha/con gắn một task ID và các artifacts đúng thư mục trạng thái. Bài kiểm thử này qua, nhưng chưa đủ ký P4 khi hai giới hạn chương trình vẫn có thể bị bỏ qua.

## Điều kiện để nghiệm thu lại

1. Đóng A1/A2 và thêm kiểm thử giữ các giới hạn ở cả CLI task lẫn Runner.
2. Đóng A3 theo quy tắc nguồn gốc ở mục 12.3; xử lý A4 để suite thường và race cùng xanh trên Linux.
3. Chạy baseline P1 trên tập tác vụ thực tế, lưu manifest, phiên bản harness/model, lệnh chấm và báo cáo CI/dao động.
4. Chạy gate 1 rồi gate 2 theo ngưỡng đã commit; lưu báo cáo trên các repository đại diện. Nếu không đạt, thực hiện nhánh chẩn đoán/thu hẹp đã quy định, không chỉ cập nhật checkbox.
5. Ghi nhận quyết định về sai khác fingerprint và phạm vi tín hiệu kết quả còn thiếu trước khi ký P3.

Các probe thêm phục vụ cuộc nghiệm thu nằm ngoài repository: `/tmp/bint-audit-probes-caebagwp/` (hai test Go và overlay), `/tmp/bint-acceptance-t5x0cqei/` (repo/state tái hiện A1). Các đường dẫn này chỉ có giá trị trong môi trường kiểm tra hiện tại.
