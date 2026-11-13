<!-- bint:begin -->
<!-- Khối do Bint quản lý. Không sửa tay: sửa tệp .bint/knowledge/items/<id>.md
rồi chạy `bint bootstrap publish`. Mã [k:<id>] để trích dẫn mục tri thức. -->

## Quy ước của dự án (Bint)

Ghi lại điều bất thường vừa phát hiện (lệnh đặc biệt, chỗ dễ sai) bằng
`bint knowledge add --type warning --paths '<glob>' --evidence <tệp>:<dòng> "<nội dung>"`;
mục do agent ghi chờ người duyệt, không tự có hiệu lực.

### Quy tắc bắt buộc

- **Mã phải qua go vet và staticcheck với GOOS=windows; mã theo nền tảng tách cặp *_unix.go / *_windows.go** [k:01M746NASVKV9XMRHA4QNHJ3R8]
  CI chạy `go vet` và `staticcheck` cho cả linux lẫn `GOOS=windows`; `go test ./...` trên Linux không bắt được lỗi biên dịch Windows.
  Lệnh gọi `syscall` riêng của Unix (Flock, Kill, Statfs...) phải nằm trong tệp `*_unix.go` có `//go:build !windows`, kèm bản `*_windows.go` có `//go:build windows` cùng chữ ký.
  Kiểm tra cục bộ: `GOOS=windows go vet ./...`; staticcheck cho windows phải cài binary rồi đặt GOOS (không dùng `go run ...@v0.7.0` với GOOS khác).

Khi làm việc với `internal/config/**`:

- **Thêm khoá cấu hình: sửa cả struct, Default(), Validate() và mẫu Render** [k:01M746NPB0Y4D5S2D47EFTJ8T4]
  Một khoá mới trong `.bint/config.yml` cần: trường có thẻ `yaml` trong struct, giá trị mặc định trong `Default()`, kiểm tra trong `Validate()`, và một dòng có chú thích trong mẫu `internal/config/template.go` (`Render`).
  `TestRenderMatchesDefaults` buộc mẫu khớp đúng `Default()`; `Decode` dùng `KnownFields(true)` nên khoá chưa khai báo làm hỏng việc nạp cấu hình.
  Xem commit d124a58 (thêm `eval.prior_run_usd`) làm mẫu.

### Điểm dễ sai

- **Khoá liên tiến trình: dùng flock/LockFileEx, đừng chép kiểu thu hồi theo PID chết của evolve.AcquireLock** [k:01M746PTBYPSXDT0WJZS38KPFD]
  Commit 755b544 (R1) sửa race của khoá ghi workflow: đọc khoá thấy PID chết rồi xoá/rename có thể gỡ nhầm khoá của chủ mới đang sống. Bản sửa dùng `tryFlock` (build tag unix/windows) để hệ điều hành tự nhả khoá khi tiến trình chết.
  `evolve.AcquireLock` (khoá `evolve.lock`) vẫn dùng mẫu cũ O_EXCL + `task.ProcessAlive` + `os.Remove`, nên cùng loại race. Khoá mới hãy theo `workflow.AcquireWriteLock`, không chép `evolve.AcquireLock`.

Khi làm việc với `**/*_test.go`:

- **go test chạy trong phiên agent có CLAUDECODE=1: kiểm thử thao tác chỉ-dành-cho-người phải xoá biến agent** [k:01M746PHJXDZN6QMA3BEVEDNPP]
  `app.InAgentSession` coi tiến trình là agent khi có một biến trong `agentEnvVars` (BINT_AGENT, CLAUDECODE, AI_AGENT, CODEX_SANDBOX...); khi đó approve/apply/reject trả `ErrAgentForbidden`.
  `testutil.Isolate` chỉ xoá CLAUDE_CODE_SESSION_ID/BINT_RUN_ID/BINT_TASK_ID/BINT_APPROVED_PATHS, KHÔNG xoá các biến trên. Kiểm thử đóng vai người phải gọi `asHuman(t)` (hoặc đặt rỗng từng biến); nếu không, test qua trên CI nhưng hỏng khi agent chạy `go test`.
  Danh sách biến bị chép tay trong `internal/cli/knowledge_e2e_test.go` và `internal/evolve/evolve_test.go`: thêm biến vào `agentEnvVars` thì sửa cả hai.

Khi làm việc với `internal/app/**`, `internal/tui/actions/**`:

- **Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (in…** [k:01M78XP5NXWWRAZQHN5PSHNC13]
  Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (internal/tui/actions/cliref.go) để Rewriter đổi lời nhắc 'bint <lệnh>' thành phím TUI, (2) cờ rời trong literal lỗi cách dùng phải vào litsrUsage của cliref_inventory_test.go. Quên cả hai thì test đỏ ở cli, không ở actions.

Khi làm việc với `internal/config/**`:

- **yaml.v3 đặt lại map inline mỗi lần giải mã lại khối chứa nó: lớp cấu hình sau làm mất role tuỳ biến của lớp trước** [k:01M79Y7NRTR51P7JA4A50P8FPG]
  Roles.Extra và RoleModels.Extra dùng thẻ yaml:",inline". yaml.v3 đặt map inline về nil mỗi khi giải mã lại khối `roles:`/`models:`, nên Decode lớp sau (local.yml) xoá role tuỳ biến đã nạp từ lớp trước. mergeFile phải chụp (snapshotExtras) rồi phục hồi (restore) quanh Decode. Thêm map inline mới vào Config thì làm tương tự; test: TestMergeFileKeepsExtraRoles.

Khi làm việc với `internal/config/**`, `internal/eval/**`:

- **Cờ security không thay thế cô lập agent và mạng** [k:01M74PDT2ETJAYRWFV9ZPWVHMR]
  `security.allow_external_agents` và `security.allow_network_in_evals` hiện mới là khoá khai báo trong schema/mặc định, chưa gắn với cơ chế chặn agent ngoài hay cô lập mạng ở tầng hệ điều hành; lượt đánh giá vẫn chạy với quyền tiến trình của người dùng và truy cập mạng phụ thuộc vào cấu hình sandbox của từng harness.

Khi làm việc với `internal/config/template.go`:

- **Render ghi mẫu bằng w(format) = fmt.Fprintf: mọi ký tự % trong chú thích của mẫu phải viết %% (bản cũ sinh '%!k(MISSING…** [k:01M7487MQV2FFZPHJDWEFVZC7Z]
  Render ghi mẫu bằng w(format) = fmt.Fprintf: mọi ký tự % trong chú thích của mẫu phải viết %% (bản cũ sinh '%!k(MISSING)hông' vào config của người dùng; Upgrade có templateFixes để sửa). Thêm dòng chú thích có % thì kiểm bằng TestRenderMatchesDefaults không đủ vì chú thích không được so.

Khi làm việc với `internal/eval/**`:

- **Mục không có bằng chứng bị EconomyFilter loại; bài học từ thất bại được nới** [k:01M74D57FYH0PS3DKV7BHRTCF8]
  EconomyFilter và EvidenceHoldsAt loại mục không có tệp bằng chứng. Bài học failure_lesson (Origin.Provenance=failure_lesson, Origin.Runs khác rỗng) không có tệp nên được nới qua Item.LessonFromRuns(); mọi mục khác vẫn bị loại.
- **codex -c tách khoá theo dấu chấm, không hiểu dấu trích dẫn: -c 'mcp_servers."a.b".enabled=false' làm Codex lỗi (invalid…** [k:01M77JFNA33PQ99TVS9A386HFG]
  codex -c tách khoá theo dấu chấm, không hiểu dấu trích dẫn: -c 'mcp_servers."a.b".enabled=false' làm Codex lỗi (invalid transport). Tên server có dấu chấm tắt bằng bảng nội tuyến -c 'mcp_servers={"a.b"={enabled=false}}' đặt TRƯỚC mọi -c mcp_servers.* khác (bảng nội tuyến thay thế các khoá đặt trước). Tắt tên không tồn tại (vd server chỉ có trong .codex/config.toml của dự án chưa trust) cũng làm Codex lỗi.

Khi làm việc với `internal/eval/**`, `internal/cli/**`:

- **go test ./... cần ≥ ~1 GB trống ở đĩa chứa TMPDIR: test eval/cli dựng kho đệm commit gốc và dừng với 'không đủ dung lượ…** [k:01M768KJEYSVTCQEJ3JAX3E7S2]
  go test ./... cần ≥ ~1 GB trống ở đĩa chứa TMPDIR: test eval/cli dựng kho đệm commit gốc và dừng với 'không đủ dung lượng đĩa' khi còn dưới ngưỡng eval.min_free_mb (mặc định 1024). Đĩa đầy thì dọn bộ đệm build (go clean -cache) trước khi chạy; lỗi này không liên quan mã đang sửa.

Khi làm việc với `internal/eval/*_test.go`:

- **Fixture dùng chính binary test cần tính thời gian thoát của race detector** [k:01M78968T95MDQSK472NC5XQT9]
  TestRestrictedRPCProfileCleanupAndRejectsUnknownTools gọi lại binary kiểm thử để đọc catalog trước khi mở app-server. Với go test -race, tiến trình catalog mặc định chờ khoảng 1 giây khi thoát; hạn tổng 1 giây có thể hết trước khi fixture RPC khởi động. Dành đủ hạn cho cả khởi tạo và probe, đồng thời xác nhận đã tới lượt RPC bị chặn để không kiểm nhầm timeout ở giai đoạn khởi tạo.

Khi làm việc với `internal/eval/codex*`:

- **Codex 0.167.2: features.multi_agent=false và features.code_mode=false không đủ tắt công cụ vì metadata model tool_mode/…** [k:01M77BS1E11BFC8C6JSS20Z5VF]
  Codex 0.167.2: features.multi_agent=false và features.code_mode=false không đủ tắt công cụ vì metadata model tool_mode/multi_agent_version vẫn bật Code Mode/collaboration; apply_patch_tool_type còn mở apply_patch. Adapter evolution cần catalog tạm bỏ các metadata này, environments=[], profile cô lập và probe request thật kiểm cả tools lẫn input.additional_tools trước khi đưa dữ liệu eval. Không suy no-tools từ thread/start hoặc riêng tools cấp cao nhất rỗng.

Khi làm việc với `internal/eval/codex_read_tools.go`:

- **Kiểm Lstat trước rồi mở lại toàn đường dẫn có race symlink: os.Root chỉ chặn thoát root, không chặn chuyển hướng vào .b…** [k:01M77FRYBCKRMTENPFE1XRFHVW]
  Kiểm Lstat trước rồi mở lại toàn đường dẫn có race symlink: os.Root chỉ chặn thoát root, không chặn chuyển hướng vào .bint/evals. Phải kiểm danh tính từng đối tượng đã mở và dùng handle thư mục đã kiểm.

Khi làm việc với `internal/evolve/**`:

- **CheckLeak chỉ cảnh báo định danh mới; bài học dùng kiểm rò rỉ riêng (định danh mã, định danh mới ở vị trí mã, literal)** [k:01M74D57EZ2HZSQMTB9YP56F3D]
  CheckLeak xếp định danh mới của lời giải (NewIdents) vào mức warn, chỉ mục solution_code mới là fail. Bài học do reflector soạn không được dùng CheckLeak một mình; evolve.CheckLesson (lesson_check.go) chặn (LessonLeak) khi nội dung bài học chứa:
  - eval.SolutionIdentifiers: định danh dạng mã (snake/camelCase, có số) của phần thêm mới, trừ tên test;
  - eval.SolutionNewIdentifiers: MỌI định danh mới ở VỊ TRÍ MÃ (kể cả tên một từ như Reset/Flush), trừ tên test. Từ thường chỉ nằm trong chú thích hoặc nội dung chuỗi (server, cache, timeout...) không bị chặn, vì bài học hay nhắc chúng;
  - eval.SolutionLiterals: chuỗi literal ≥ 12 ký tự ("...", '...', `...` raw) của phần thêm mới, trừ chuỗi có trong kiểm thử.
  Thêm ca chặn mới thì sửa TestCheckLessonTable (failure_test.go), kể cả ca chặn nhầm.
<!-- bint:end -->
