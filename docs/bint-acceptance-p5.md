# Biên bản nghiệm thu P5 — Multi-Harness

> **Bản ghi lịch sử:** Biên bản dưới đây phản ánh trạng thái tại commit được kiểm tra. Tổng hợp hiện trạng và các phát hiện đã đóng / còn mở tại `HEAD` xem ở [docs/status.md](status.md) và [docs/evals/README.md](evals/README.md).

Ngày kiểm tra: 2026-11-10.

- HEAD được kiểm tra: `25202fb00945ddaf4e936d519e4203a12abc21f5`.
- Commit triển khai P5: `90f81f2543525f0fe2a75f2aef480eda5cfd2dde`.
- Chuẩn đối chiếu: `docs/bint-plan.md`, mục 7, P5 tại mục 16 và Adapter Contract Tests tại mục 17.
- Phạm vi: nghiệm thu và ghi nhận phát hiện; không sửa mã nguồn, không đánh dấu hoàn thành kế hoạch.

## Kết luận

**Chưa đủ điều kiện ký nghiệm thu P5.** Lớp adapter, xuất bản skill, chọn harness theo role, chuẩn hoá hook và mở rộng đánh giá đã được triển khai. Cùng một workflow và cùng nguồn tri thức hoạt động qua Codex/Antigravity trong phép thử dùng binary giả lập. Tuy nhiên, cơ chế cô lập có hai lỗi đã tái hiện, và trạng thái adapter có thể được báo sai khi cấu hình trỏ tới binary không tồn tại.

Không dùng việc toàn bộ suite hiện tại xanh để thay thế kiểm chứng các điều kiện nghiệm thu.

## Kiểm tra đã chạy

Môi trường: Linux amd64, Go 1.25.1, gcc. Phiên bản chương trình được xác nhận bằng CLI: Claude Code `2.1.302`, Codex CLI `0.167.0`, Antigravity `1.5.3`. Chỉ gọi version/help của chương trình thật; không giao tác vụ cho mô hình trả phí trong đợt nghiệm thu này.

| Kiểm tra | Kết quả |
|---|---|
| `CGO_ENABLED=0 go build -o /tmp/bint-acceptance-p5 ./cmd/bint` | PASS, exit 0 |
| `go vet ./...` | PASS, exit 0 |
| `go test -count=1 ./...` | PASS, exit 0 |
| `go test -race -count=1 ./...` | PASS, exit 0, không có báo cáo data race |
| `bint harness list`, đối chiếu help của Codex/Antigravity | Nhận diện được ba phiên bản; có bảng hỗ trợ đầy đủ/một phần/không hỗ trợ |
| b-do qua adapter Codex, binary giả lập | PASS: b-req → b-feat → verify → b-review → done |
| b-do qua adapter Antigravity, binary giả lập | PASS: cùng chuỗi và task ID/artifacts |
| Cùng repository/tri thức: toàn Codex, toàn Antigravity, role hỗn hợp | PASS cả ba; script yêu cầu đề bài chứa cùng một knowledge ID |
| Eval với CodexAgent/AgyAgent và binary giả lập trên fixture lịch sử | PASS nhánh none cho cả hai; nhánh bint của Antigravity skipped kèm lý do |
| Probe bước chỉ đọc sửa `.bint/config.yml` | FAIL assertion: bước vẫn được chấp nhận, xác nhận P5-02 |
| Probe khoá ghi đồng thời, chạy ba lần | FAIL assertion cả ba, xác nhận P5-01 |
| Cấu hình binary Codex không tồn tại rồi `harness show codex --json` | Báo `installed: true`, xác nhận P5-03 |

Test cài hook từng làm suite thất bại trong báo cáo P0–P4 hiện đã qua trên Linux. Đây là kết quả kiểm tra mới của HEAD nêu trên; báo cáo cũ vẫn mô tả đúng phiên bản cũ đã kiểm tra.

## Phát hiện

### P5-01 — Cao: khoá ghi không bảo đảm độc quyền khi tranh chấp đồng thời

Vị trí: `internal/workflow/isolation.go:49`, `:62`, `:72`, `:78`. Runner dùng khoá này tại `internal/workflow/run.go:417`.

`AcquireWriteLock` tạo tệp bằng `O_EXCL`, sau đó mới ghi PID. Giữa hai thao tác, một bên khác có thể thấy tệp rỗng, đọc PID thành 0 và xoá tệp như khoá bị bỏ lại. Bên đó lấy khoá mới trong khi chủ khoá đầu tiên vẫn hoạt động. Ngoài ra, `Release` chỉ xoá theo đường dẫn, không xác nhận quyền sở hữu; chủ cũ có thể xoá khoá của chủ mới.

Probe gọi hàm thật với cùng worktree, 48 goroutine, mỗi goroutine 80 lần thử. Sau khi lấy khoá thành công, tăng bộ đếm atomic, giữ vùng làm việc 200 microsecond rồi giảm bộ đếm và trả khoá. Nếu bộ đếm vượt 1 thì có nhiều chủ đang giữ khoá cùng lúc.

| Lần chạy | Số lượt lấy khoá thành công | Số lượt phát hiện đang có chủ khác |
|---|---:|---:|
| 1 | 2508 | 2101 |
| 2 | 2493 | 2016 |
| 3 | 2455 | 1922 |

Đây là tranh chấp trên giao thức tệp của khoá, không phải Go memory data race; việc suite `-race` xanh không phát hiện được lỗi này. Probe sử dụng goroutine để tạo tranh chấp trên chính đường dẫn khoá; chưa đo mức hỏng dữ liệu bằng nhiều agent thật.

Ảnh hưởng: không bảo đảm điều kiện “mỗi worktree chỉ một bước có quyền ghi tại một thời điểm”, bất kể harness. Các bài test hiện tại lấy khoá trước rồi mới thử Runner nên không bao phủ khoảng tạo tệp nhưng chưa ghi PID.

Điều kiện đóng: bảo đảm công bố/thay khoá nguyên tử, không thu hồi khoá chưa chứng minh được là bỏ lại, chỉ chủ khoá được trả khoá; bổ sung kiểm thử tranh chấp đồng thời và kiểm thử nhiều tiến trình.

### P5-02 — Cao: bước chỉ đọc có thể sửa cấu hình/chính sách trong `.bint/` mà không bị phát hiện

Vị trí: `internal/workflow/isolation.go:84`, `:110`; kiểm tra sau bước tại `internal/workflow/run.go:442`.

Snapshot loại toàn bộ `.bint/` với mục đích cho phép CLI ghi ứng viên tri thức. Phạm vi loại trừ thực tế còn gồm `.bint/config.yml`, lớp phủ skill và tập đánh giá. Do đó, kiểm tra chỉ đọc bỏ qua cả thay đổi chính sách/quy trình, không chỉ các ứng viên tri thức được phép ghi.

Probe trên Runner thật, dùng fake agent như cơ chế test hiện tại:

1. Tạo `.bint/config.yml` với `security.require_approval_for_commands: true`.
2. Chạy bước `b-req`.
3. Trong bước đó, agent sửa giá trị thành `false` rồi ghi đủ artifacts yêu cầu.
4. Kết quả thực tế: bước `completed`, tác vụ `planned`, không có lỗi “bước chỉ đọc”.

Việc cấu hình bị sửa không có nghĩa Runner đã nạp lại ngay chính sách mới trong cùng bước. Lỗi được chứng minh là cổng kiểm tra không phát hiện và chấp nhận thay đổi cấu hình; thay đổi đó tồn tại trên đĩa cho các lần nạp sau.

Ảnh hưởng: bảo đảm chỉ đọc ở tầng Bint yếu hơn bảng khả năng đã công bố. Codex chạy cả bước chỉ đọc trong `workspace-write`, nên cơ chế kiểm tra sau bước đặc biệt quan trọng. Claude/Antigravity có cơ chế riêng của harness nhưng không thay thế được kiểm tra chung như đã tuyên bố.

Điều kiện đóng: thu hẹp ngoại lệ tới đúng dữ liệu ứng viên được phép ghi, đồng thời bảo vệ/đối chiếu cấu hình, skill, tập đánh giá; bổ sung test bước chỉ đọc sửa các tệp này. Nếu chủ ý cho phép sửa một phần `.bint/`, phải mô tả chính xác trong hợp đồng thay vì loại cả thư mục.

### P5-03 — Trung bình: cấu hình binary không tồn tại vẫn được báo đã cài

Vị trí: `internal/harness/harness.go:215`, đặc biệt `:218`; đối chiếu `Available` tại `:385`.

`Describe` đặt `Present = true` ngay khi cấu hình có chuỗi binary, không kiểm tra tệp. Trong khi đó `NewAgent` gọi `Available` và từ chối nếu tệp không tồn tại. Hai đường xử lý cho trạng thái khác nhau.

Đã chạy CLI thật trên repository tạm, cấu hình:

```yaml
harnesses:
  codex:
    binary: /nonexistent/bint-p5-codex
```

`bint harness show codex --json` trả:

```json
{"name":"codex","installed":true,"binary":"/nonexistent/bint-p5-codex","level":3}
```

`level: 3` có thể mô tả cấp năng lực của loại adapter; lỗi ở đây là `installed: true` đối với chương trình không tồn tại, không phải riêng giá trị level.

Ảnh hưởng: chưa đáp ứng yêu cầu báo cáo chính xác adapter không khả dụng. Hành vi từ chối thực thi khi thiếu binary đã có test và hoạt động; phần hiển thị/phát hiện chưa nhất quán.

Điều kiện đóng: thống nhất kiểm tra binary hiệu dụng giữa Describe/Available/doctor, phân biệt có cấu hình với thực sự khả dụng; kiểm thử đường dẫn không tồn tại, thư mục và tệp không chạy được.

## Đối chiếu 10 hạng mục P5

| Hạng mục trong kế hoạch | Đánh giá | Bằng chứng và giới hạn |
|---|---|---|
| HarnessAdapter và capability matrix, gồm MCP/path hooks | Một phần | Có Adapter và các giao diện HookInstaller/MCPInstaller/Executor, 10 khả năng và contract tests; trạng thái cài đặt lỗi P5-03 |
| Tích hợp Claude Code | Có ở mức kiểm thử cục bộ | Giữ hook/MCP/skill và adapter thực thi; các test hồi quy qua. Không chạy model thật trong đợt này |
| Tích hợp Codex | Có ở mức hợp đồng và luồng giả lập | Hook/MCP/exec JSONL, shared-knowledge b-do và eval none qua; CLI thật xác nhận phiên bản/cờ. Chưa tái chạy agent thật/trust hooks |
| Tích hợp Antigravity | Một phần bằng chứng thực tế | Skill/hook/exec JSON và shared-knowledge b-do qua giả lập; công cụ thật có đúng các cờ được dùng. README:501–504 thừa nhận payload công cụ chưa bắt trực tiếp |
| Đồng bộ skill theo định dạng | Có | `.claude/skills` giữ metadata Claude; `.agents/skills` chỉ name/description, giữ thân và overlays; contract init/cài/gỡ giữ cấu hình người dùng qua |
| Role override | Có | `--role` → `--harness` → roles → default; từ chối harness chưa enabled. Đã chạy luồng planner/reviewer Antigravity, implementer Codex qua binary giả lập |
| Chuẩn hoá sự kiện khi có hỗ trợ | Có ở mức fixture | Hookcore chung; Codex apply_patch nhiều tệp, lifecycle/test; Antigravity camelCase/test, không bịa session.ended. Test ingest/statistics qua |
| Cô lập tác vụ có quyền ghi | Chưa đạt | Chỉ có khoá theo worktree và snapshot hậu kiểm, chưa có worktree riêng; lỗi độc quyền P5-01 và khoảng trống chỉ đọc P5-02 |
| Mở rộng eval nhiều harness | Có ở mức hợp đồng | Chọn adapter qua --harness, metadata batch/attempt, chặn resume sai harness, ArmChecker; probe Codex/Antigravity none qua, AGY bint skipped đúng lý do |
| Hành vi adapter không khả dụng | Một phần | Test NewAgentUnavailable/UnavailableAdapterFailsStep qua và nêu hướng xử lý; `harness show` vẫn có thể báo sai P5-03 |

Các phép thử cùng tri thức dùng một repository và một knowledge ID `01M71RHG38XZ0G6YC72P4RDNS8`. Binary giả lập kiểm tra ID này có trong đề bài từng bước, gọi CLI task để ghi artifacts/review/criteria, rồi trả kết quả theo định dạng riêng Codex/Antigravity. Cả ba cấu hình đều hoàn thành, không dùng `--agent-command`; vì vậy có đi qua lớp chọn harness và adapter thực tế của Bint. Phép thử xác nhận việc truyền dữ liệu/điều phối, không chứng minh chất lượng suy luận của mô hình.

## Giới hạn đã công bố và được giữ trong đánh giá

- Codex: MCP/hook dự án phụ thuộc trust; chèn theo đường dẫn chỉ trước apply_patch. Không xem thiếu chèn khi đọc qua shell là lỗi mới.
- Antigravity: không có MCP cấp dự án, không chèn ngữ cảnh qua hook, không có sự kiện kết thúc phiên. Đánh dấu các phần này không hỗ trợ/một phần là phù hợp.
- Codex/Antigravity không báo chi phí và không áp max_turns. Bảng khả năng đã nói rõ; không yêu cầu giả lập số liệu hoặc báo có giới hạn cứng.
- Nhánh eval bint trên Codex chỉ có MCP + AGENTS.md, không có path hook; trên Antigravity skipped. Không thể coi các nhánh này là phép đo đầy đủ giống Claude mà bỏ qua khác biệt cấu hình.
- README nêu có smoke test Codex thật trước đó; đợt nghiệm thu này không có log độc lập để xác nhận lại lời mô tả đó. Không có kết quả tác vụ thật Antigravity trong đợt này.
- Cả ba adapter khai cấp 3, chưa khai cấp 4; không đánh đồng cấp 3 với cô lập đầy đủ nhiều agent.

## Điều kiện nghiệm thu lại

1. Đóng P5-01 bằng kiểm thử độc quyền khi tranh chấp và thu hồi khoá bỏ lại, gồm nhiều tiến trình thực sự.
2. Đóng P5-02; xác định rõ ngoại lệ ghi tri thức của bước chỉ đọc, giữ cấu hình/chính sách/skill/evals trong phạm vi bảo vệ.
3. Đóng P5-03 và kiểm tra tính nhất quán giữa bảng trạng thái, doctor và việc tạo agent.
4. Giữ xanh bộ kiểm thử thường/race và các luồng cùng tri thức, role hỗn hợp, eval bỏ qua nhánh không hỗ trợ.
5. Lưu smoke logs của harness thật theo phiên bản đã công bố, đặc biệt payload công cụ và kết quả PreToolUse/PostToolUse của Antigravity, để hoàn thiện bằng chứng tích hợp thực tế.

Đầu ra phục vụ kiểm tra cục bộ (không phải artifact phát hành): `/tmp/bint-p5-probes-wfidum3s/` chứa hai probe workflow, probe eval và overlay Go; `/tmp/bint-p5-smoke-nkkp9zfx/` chứa repository/binary giả lập và `shared-results.json`, `missing-binary-result.json`. Các đường dẫn này có thể mất khi môi trường tạm được dọn.

## Cập nhật sau khi sửa (2026-11-10)

- P5-01, P5-02, P5-03 đã được sửa kèm test (tranh chấp đồng thời, nhiều tiến trình thật, thu hồi khoá bỏ lại, bước chỉ đọc sửa `.bint/config.yml`, nhất quán `Describe`/`Available`). `go vet ./...` và `go test -race -count=1 ./...` đều xanh.
- **Còn mở — điều kiện nghiệm thu lại số 5:** chưa lưu smoke log của harness thật (Claude Code, Codex, Antigravity) theo phiên bản đã công bố, đặc biệt payload công cụ và kết quả PreToolUse/PostToolUse của Antigravity. Việc này tốn chi phí gọi mô hình và cần CLI đã đăng nhập; chủ dự án sẽ làm sau. Cho tới lúc đó P5 chưa nên được coi là nghiệm thu đầy đủ.
