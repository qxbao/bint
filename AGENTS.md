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

Khi làm việc với `internal/harness/**`, `internal/app/hook.go`:

- **Hook không bao giờ chặn agent: không trả lỗi, không mở SQLite, không gọi mô hình, không tự tổng hợp sự kiện** [k:01M746NPTW1B304MRQQPBCAFAM]
  Hook chạy ở mỗi lần agent dùng công cụ (giới hạn 5 giây). Lỗi của hook được ghi vào `hook-errors.log`, lệnh `bint hook ...` luôn trả `nil`; đừng thêm đường trả lỗi/mã thoát khác 0.
  Hook chỉ ghi vào spool, không mở SQLite và không gọi mô hình (việc nạp spool vào CSDL làm ở nơi khác).
  Sự kiện mà harness không cung cấp thì không được giả lập (docs/bint-plan.md mục 13.2). Khác biệt giữa harness đặt trong `hookcore.Dialect`, không rẽ nhánh theo tên harness trong hookcore.

### Điểm dễ sai

- **Khoá liên tiến trình: dùng flock/LockFileEx, đừng chép kiểu thu hồi theo PID chết của evolve.AcquireLock** [k:01M746PTBYPSXDT0WJZS38KPFD]
  Commit 755b544 (R1) sửa race của khoá ghi workflow: đọc khoá thấy PID chết rồi xoá/rename có thể gỡ nhầm khoá của chủ mới đang sống. Bản sửa dùng `tryFlock` (build tag unix/windows) để hệ điều hành tự nhả khoá khi tiến trình chết.
  `evolve.AcquireLock` (khoá `evolve.lock`) vẫn dùng mẫu cũ O_EXCL + `task.ProcessAlive` + `os.Remove`, nên cùng loại race. Khoá mới hãy theo `workflow.AcquireWriteLock`, không chép `evolve.AcquireLock`.

Khi làm việc với `**/*_test.go`:

- **go test chạy trong phiên agent có CLAUDECODE=1: kiểm thử thao tác chỉ-dành-cho-người phải xoá biến agent** [k:01M746PHJXDZN6QMA3BEVEDNPP]
  `app.InAgentSession` coi tiến trình là agent khi có một biến trong `agentEnvVars` (BINT_AGENT, CLAUDECODE, AI_AGENT, CODEX_SANDBOX...); khi đó approve/apply/reject trả `ErrAgentForbidden`.
  `testutil.Isolate` chỉ xoá CLAUDE_CODE_SESSION_ID/BINT_RUN_ID/BINT_TASK_ID/BINT_APPROVED_PATHS, KHÔNG xoá các biến trên. Kiểm thử đóng vai người phải gọi `asHuman(t)` (hoặc đặt rỗng từng biến); nếu không, test qua trên CI nhưng hỏng khi agent chạy `go test`.
  Danh sách biến bị chép tay trong `internal/cli/knowledge_e2e_test.go` và `internal/evolve/evolve_test.go`: thêm biến vào `agentEnvVars` thì sửa cả hai.
<!-- bint:end -->
