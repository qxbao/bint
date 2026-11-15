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

- **Khoá liên tiến trình: dùng fslock.Try (flock/LockFileEx), không tự thu hồi khoá theo PID chết** [k:01M7FGC944KVT6NY1TZE7MJ0P8]
  Khoá giữa các tiến trình dùng `fslock.Try(f, exclusive)` (gói `internal/fslock`, cặp `internal/fslock/fslock_unix.go`/`internal/fslock/fslock_windows.go`): hệ điều hành tự nhả khoá khi tiến trình chết, nên không có bước thu hồi.
  Đừng dựng lại mẫu O_EXCL + `task.ProcessAlive` + `os.Remove`: đọc khoá thấy PID chết rồi xoá có thể gỡ nhầm khoá của chủ mới đang sống (race R1, commit 755b544). Nội dung PID/thời điểm trong tệp khoá chỉ để báo lỗi, không dùng để quyết định khoá.
  Mẫu: `evolve.AcquireLock` (sửa ở V0, NT-P6-01), `workflow` (`internal/workflow/lock_flock.go`), `task`. Thay cho mục 01M746PTBYPSXDT0WJZS38KPFD đã lỗi thời.

Khi làm việc với `**/*_test.go`:

- **go test chạy trong phiên agent có CLAUDECODE=1: kiểm thử thao tác chỉ-dành-cho-người phải xoá biến agent** [k:01M746PHJXDZN6QMA3BEVEDNPP]
  `app.InAgentSession` coi tiến trình là agent khi có một biến trong `agentEnvVars` (BINT_AGENT, CLAUDECODE, AI_AGENT, CODEX_SANDBOX...); khi đó approve/apply/reject trả `ErrAgentForbidden`.
  `testutil.Isolate` chỉ xoá CLAUDE_CODE_SESSION_ID/BINT_RUN_ID/BINT_TASK_ID/BINT_APPROVED_PATHS, KHÔNG xoá các biến trên. Kiểm thử đóng vai người phải gọi `asHuman(t)` (hoặc đặt rỗng từng biến); nếu không, test qua trên CI nhưng hỏng khi agent chạy `go test`.
  Danh sách biến bị chép tay trong `internal/cli/knowledge_e2e_test.go` và `internal/evolve/evolve_test.go`: thêm biến vào `agentEnvVars` thì sửa cả hai.

Khi làm việc với `internal/**/*_test.go`:

- **go test ./... (có test cache, không -count=1) trên Windows treo hàng chục phút, go.exe không tiến trình con, không in g…** [k:01M7D6WA84MDN32JHM21VZWQ5H]
  go test ./... (có test cache, không -count=1) trên Windows treo hàng chục phút, go.exe không tiến trình con, không in gì: trước khi chạy mỗi gói, go.exe đọc testlog của lần trước và băm từng dòng stat/open; đường dẫn ngoài module bị filepath.EvalSymlinks hai lần (search.InDir). Mỗi exec.Command('git') gọi LookPath, sinh ~số thư mục PATH x số đuôi PATHEXT dòng stat, nên testlog của internal/tui/app ~477 nghìn dòng (~7000 lần LookPath git), internal/app ~357 nghìn; 56 nghìn dòng đã tốn ~17s mỗi lượt băm. Đo bằng -count=1 sẽ không tái hiện vì cờ này tắt test cache và testlog. Lệnh kiểm chứng nên dùng go test -count=1 ./..., hoặc giảm số lần LookPath git trong test.

Khi làm việc với `internal/**/*_test.go`, `internal/testutil/**`:

- **Git for Windows đặt core.autocrlf=true ở cấu hình hệ thống. testutil.Git chạy với GIT_CONFIG_NOSYSTEM=1 nhưng lệnh git …** [k:01M7B2GCBFCZEE8D2F84FDC8BR]
  Git for Windows đặt core.autocrlf=true ở cấu hình hệ thống. testutil.Git chạy với GIT_CONFIG_NOSYSTEM=1 nhưng lệnh git do Bint gọi thì không, nên repo test dựng với LF còn checkout/apply/cat-file --filters của Bint ra CRLF: test so nội dung chính xác hỏng trên Windows dù Linux xanh. Test cần môi trường git kín (NOSYSTEM cho cả tiến trình); muốn kiểm hành vi autocrlf thì đặt core.autocrlf ở cấu hình repo để chạy được trên mọi OS.
- **go test ./... trên Windows chậm (~10 phút) chủ yếu do tiến trình git: mỗi lần spawn git ~25 ms, test không dùng t.Paral…** [k:01M7CJ92ZB2DRYYHGFBQABYM9D]
  go test ./... trên Windows chậm (~10 phút) chủ yếu do tiến trình git: mỗi lần spawn git ~25 ms, test không dùng t.Parallel (Isolate dùng t.Setenv) và repo tạm nằm trong %TEMP% bị Defender quét thời gian thực. Gói workflow (~600 s) và tui/app (~300 s) là đường găng. Đo trước khi tối ưu: go test -json rồi tính thời gian từng test; profile bằng -test.cpuprofile, thời gian chờ git hiện thành runtime.cgocall/ReadFile. Đừng cache gitrepo.Discover chỉ để test nhanh (đổi hành vi sau khi xoá worktree).

Khi làm việc với `internal/app/**`, `internal/tui/actions/**`:

- **Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (in…** [k:01M78XP5NXWWRAZQHN5PSHNC13]
  Thêm lệnh CLI mới hoặc thông báo lỗi nêu cờ rời: TestServiceMessageCLIInventory đòi (1) hàng của lệnh trong cliRefs (internal/tui/actions/cliref.go) để Rewriter đổi lời nhắc 'bint <lệnh>' thành phím TUI, (2) cờ rời trong literal lỗi cách dùng phải vào litsrUsage của cliref_inventory_test.go. Quên cả hai thì test đỏ ở cli, không ở actions.

Khi làm việc với `internal/cli/*_test.go`, `internal/eval/shell.go`:

- **Trên Windows, lệnh chạy qua sh -c (eval.RunShell) mà chứa đường dẫn Windows trần có dấu gạch chéo ngược (sh C:\Users\..…** [k:01M7CX9V792957T6MGK6QWEJ7F]
  Trên Windows, lệnh chạy qua sh -c (eval.RunShell) mà chứa đường dẫn Windows trần có dấu gạch chéo ngược (sh C:\Users\...\agent.sh) bị sh nuốt dấu \ nên thoát mã 127 (command not found); viết đường dẫn bằng / (filepath.ToSlash). Cũng lưu ý lệnh kiểm chứng go test ./... trên Windows chậm/treo có thể do Defender quét tệp thực thi kiểm thử: xem docs/workflows.md mục 'Kiểm chứng chạy qua Bint trên Windows'.

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
<!-- bint:end -->
