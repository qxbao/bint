# Bint

CLI hỗ trợ coding agent: ghi nhận lượt chạy, đo lường, và (ở các giai đoạn sau)
tri thức dự án có kiểm chứng. Kế hoạch đầy đủ: [docs/bint-plan.md](docs/bint-plan.md).

**Trạng thái: P0 — nền tảng CLI và ghi nhận lượt chạy.**

## Cài đặt

```sh
go install ./cmd/bint            # hoặc: go build -o bint ./cmd/bint
```

Go thuần, không cần CGO (SQLite qua `modernc.org/sqlite`, có FTS5), biên dịch chéo
được cho Linux/macOS/Windows. Cần git >= 2.31.

## Dùng

```sh
bint init --hooks        # khởi tạo; cài hook ghi nhận cho Claude Code
bint doctor              # kiểm tra môi trường
bint runs list           # lượt chạy đã ghi nhận (tự nạp spool trước)
bint runs show <id>      # chi tiết, chấp nhận tiền tố ID
bint status
bint events ingest       # nạp spool thủ công
```

`bint init` chạy lại bao nhiêu lần cũng an toàn:

- `.bint/config.yml` chỉ được tạo khi chưa có; không bao giờ bị ghi đè.
- Lệnh kiểm thử được phát hiện và ghi dưới dạng `suggested_test_command`; Bint không
  chạy gì cho tới khi bạn điền `eval.test_command`.
- `.gitignore`, `AGENTS.md`, `CLAUDE.md` chỉ bị chạm trong khối `bint:begin … bint:end`.
  Nếu bạn sửa tay khối đó, Bint báo xung đột thay vì ghi đè (`--force` để ghi đè, bản
  cũ được sao lưu). `AGENTS.md`/`CLAUDE.md` chỉ được tạo khi có `--agents-md`.
- Hook được thêm vào settings của Claude Code mà giữ nguyên các khoá và hook khác.
  `--hooks-scope local|project|user` (mặc định `local` = `.claude/settings.local.json`).
  `--no-hooks` gỡ hook của Bint.

### Mã thoát

| Mã | Ý nghĩa |
|---|---|
| 0 | thành công |
| 1 | lỗi, hoặc `init`/`doctor` có mục cần xử lý |
| 2 | sai cú pháp |
| 3 | lệnh dự kiến, chưa triển khai |
| 4 | không phải repository Git, hoặc chưa `bint init` |

## Kiến trúc P0

```text
cmd/bint/                 điểm vào
internal/cli/             cobra, định dạng đầu ra, mã thoát
internal/app/             init, doctor, hook wrapper, workspace
internal/config/          schema v1, mặc định, nạp theo lớp: toàn cục → dự án → .bint/local.yml
internal/state/           thư mục trạng thái ngoài repository, repo-id từ git-common-dir
internal/store/           SQLite (WAL) + migration có phiên bản (migrations/*.sql)
internal/spool/           hàng đợi sự kiện dạng tệp (mỗi lần hook = một tệp, ghi nguyên tử)
internal/runlog/          nạp spool → events/runs, loại trùng theo event_id; truy vấn
internal/harness/claude/  hook handler + cài/gỡ hook cho Claude Code
internal/fsafe/           ghi nguyên tử, sao lưu, khối đánh dấu, manifest băm
internal/detect/          phát hiện harness, lệnh test đề xuất, nhận diện lệnh test, che bí mật
```

**Luồng ghi nhận:** Claude Code gọi `bint hook claude` → hook xác định repository từ
`cwd`, ghi một tệp `<ulid>.jsonl` vào `spool/` rồi thoát 0 (không mở SQLite, không
gọi mô hình, lỗi chỉ được ghi vào `hook-errors.log`). Mỗi lệnh đọc (`runs`, `status`)
nạp spool vào SQLite trong giao dịch; `event_id` tất định (từ `session_id` +
`tool_use_id`) nên hook bị gọi trùng không tạo bản ghi trùng.

**Thư mục trạng thái** (`BINT_STATE_DIR` ghi đè gốc; `state.dir` trong `.bint/local.yml`
ghi đè cho một repository):

| Hệ điều hành | Mặc định |
|---|---|
| Linux | `${XDG_STATE_HOME:-~/.local/state}/bint/<repo-id>` |
| macOS | `~/Library/Application Support/bint/state/<repo-id>` (hoặc `$XDG_STATE_HOME/bint`) |
| Windows | `%LOCALAPPDATA%\bint\state\<repo-id>` |

Mọi worktree của một repository có cùng `<repo-id>` nên ghi về cùng một cơ sở dữ liệu.
Worktree phụ khi `.bint/` chưa được commit sẽ dùng cấu hình của worktree chính.

**Sự kiện được ghi ở P0:** `session.started` (nguồn, mô hình, HEAD, nhánh),
`session.ended` (lý do), `file.modified` (đường dẫn tương đối trong repository; tệp
ngoài repository bị bỏ qua), `test.completed` (lệnh đã che bí mật, ok/lỗi theo
`PostToolUse`/`PostToolUseFailure`). Lượt `running` không có sự kiện trong 12 giờ được
chuyển `interrupted`. Giá trị không đo được để `NULL`, không ghi 0.

## Phát triển

```sh
go test ./...                 # gồm kiểm thử đầu-cuối build tệp thực thi thật
go test -race ./...           # cần CGO/gcc
```
