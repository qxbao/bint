# Bint

CLI hỗ trợ coding agent: ghi nhận lượt chạy, đo lường, quản lý tri thức dự án có bằng chứng,
điều phối workflow đa harness và tiến hoá có kiểm chứng. Kế hoạch thiết kế đầy đủ:
[docs/bint-plan.md](docs/bint-plan.md).

**Trạng thái: P0 (CLI, ghi nhận lượt chạy) + P1 (khung đánh giá) + P2 (bootstrap tối giản)
+ P3 (Context Engine và vòng đời tri thức) + P4 (workflow tối thiểu) + P5 (multi-harness:
Claude Code, Codex, Antigravity) + P6 (tiến hoá tri thức L1) + P7 (tiến hoá skill L2)
+ P8 (vòng `evolve cycle` và bài học từ thất bại).**
Đã có các đợt đo thực nghiệm trên Claude Code và Codex ([docs/evals/README.md](docs/evals/README.md));
cổng quyết định 1 và 2 chưa nghiệm thu chính thức — ngưỡng đã chốt ở
[docs/gate-1.md](docs/gate-1.md) và [docs/gate-2.md](docs/gate-2.md). Tổng hợp hiện trạng và
các giới hạn còn mở: [docs/status.md](docs/status.md).

## Cài đặt

```sh
go install ./cmd/bint            # hoặc: go build ./cmd/bint (bint trên Unix, bint.exe trên Windows)
```

Go thuần (>= 1.25.1 theo `go.mod`), không cần CGO (SQLite qua `modernc.org/sqlite`, có FTS5),
biên dịch chéo được cho Linux/macOS/Windows. Cần git >= 2.31 và CLI của harness muốn dùng.
Khi dùng `go install`, đảm bảo `$(go env GOBIN)` hoặc `$(go env GOPATH)/bin` nằm trong `PATH`.

## Dùng

```sh
bint init --hooks        # khởi tạo; cài hook ghi nhận + skill gốc cho các harness được bật
bint init --update       # repository init bằng bản cũ: thêm khoá cấu hình mới (giữ giá trị, có sao lưu)
bint init --git local    # dùng Bint riêng trên máy: tệp của Bint không vào Git (track = commit cho nhóm)
bint doctor              # kiểm tra môi trường
bint update [--pull]     # build lại + cài từ thư mục mã nguồn (go install ./cmd/bint); rồi bint init --update
bint runs list           # lượt chạy đã ghi nhận (tự nạp spool trước)
bint runs show <id>      # chi tiết, chấp nhận tiền tố ID
bint status
bint events ingest       # nạp spool thủ công
```

**Hiện diện trong Git** (`--git`, nhớ theo máy, lần init đầu thì hỏi): `track` (mặc định) để tệp
của Bint được commit cho cả nhóm, tệp riêng của máy vào khối bint của `.gitignore`. `local` dành cho
dùng riêng: tệp/thư mục chưa được Git theo dõi (`.bint/`, skill đã xuất bản, hook, `.mcp.json`…) vào
khối bint của `.git/info/exclude` (không commit, chung mọi worktree); tệp đã được theo dõi mà Bint sửa
(AGENTS.md, CLAUDE.md…) được đặt `git update-index --skip-worktree` (không dùng `--assume-unchanged`:
đó chỉ là gợi ý hiệu năng, Git có thể bỏ và ghi đè thay đổi cục bộ); `.gitignore` không bị đụng.
Upstream đổi một tệp skip-worktree thì `git pull` báo lỗi: chạy `bint init --git track` để gỡ cả hai.

`bint init` chạy lại bao nhiêu lần cũng an toàn:

- Skill gốc được xuất bản cho từng harness được bật (`.claude/skills/` cho Claude Code,
  `.agents/skills/` cho Codex và Antigravity; phiên bản ghi trong `.bint/skills/core.lock`);
  bản bị sửa tay không bị ghi đè (`--force`), `--no-skills` để bỏ.
- `.bint/config.yml` chỉ được tạo khi chưa có; `bint init --update` bổ sung khoá mới vào tệp đã có
  mà giữ nguyên giá trị/chú thích cũ và sao lưu trước khi ghi.
- Lệnh kiểm thử được phát hiện và ghi dưới dạng `suggested_test_command`; Bint không
  chạy gì cho tới khi bạn điền `eval.test_command`.
- `.gitignore`, `AGENTS.md`, `CLAUDE.md` chỉ bị chạm trong khối `bint:begin … bint:end`.
  Nếu bạn sửa tay khối đó, Bint báo xung đột thay vì ghi đè (`--force` để ghi đè, bản
  cũ được sao lưu). `AGENTS.md`/`CLAUDE.md` chỉ được tạo khi có `--agents-md`.
- Hook được thêm vào cấu hình của từng harness mà giữ nguyên các khoá và hook khác
  (Claude Code: `.claude/settings[.local].json`; Codex: `.codex/hooks.json`; Antigravity:
  `.agents/hooks.json`). `--hooks-scope local|project|user` (mặc định `local`: tệp trong
  repository và được thêm vào `.gitignore`; `user`: thư mục cấu hình người dùng).
  `--no-hooks` gỡ hook của Bint.

### Mã thoát

| Mã | Ý nghĩa |
|---|---|
| 0 | thành công |
| 1 | lỗi, hoặc `init`/`doctor` có mục cần xử lý |
| 2 | sai cú pháp |
| 3 | lệnh dự kiến, chưa triển khai |
| 4 | không phải repository Git, hoặc chưa `bint init` |

## Luồng làm việc chính

```sh
bint bootstrap                        # static discovery + xuất bản skill b-bootstrap
bint knowledge review                 # người dùng duyệt mục candidate → verified
bint bootstrap publish                # dựng khối quy tắc đã duyệt vào AGENTS.md
bint context "Mô tả tác vụ"            # truy xuất gói ngữ cảnh (FTS5 + phạm vi + bài học)
bint run b-do "Mô tả thay đổi"         # điều phối Classify → b-req → b-feat → b-review
bint run status <task-id>             # tiến độ tác vụ và các lượt chạy con
bint eval run --arms none --budget 20 # đánh giá trên môi trường Git cô lập
bint evolve cycle --budget 20         # vòng đề xuất → đối chiếu validation/test → áp dụng
```

Trong phiên tương tác của harness có hỗ trợ skill, gọi `/b-bootstrap`, `/b-knowledge`,
`/b-req`, `/b-feat`, `/b-review`, `/b-do`. Mọi mục tri thức do agent tạo đều ở trạng thái
`candidate` chờ người duyệt; với Codex cần duyệt hook trong `/hooks` (kiểm tra bằng `bint doctor`).

## Giao diện terminal (TUI)

Bint có giao diện terminal để xem và duyệt thay cho việc gõ lệnh: tri thức chờ duyệt, tác vụ, lượt chạy,
đánh giá, tiến hoá và cấu hình. Kế hoạch thiết kế và quyết định chi tiết: [docs/tui-plan.md](docs/tui-plan.md).

**Mở**

```sh
bint                     # không đối số: mở TUI nếu có terminal tương tác; ngược lại in trợ giúp như mọi lệnh CLI
bint tui                 # mở tường minh (lỗi nếu stdin/stdout không phải terminal)
bint tui --lang en       # ngôn ngữ giao diện vi | en
bint tui --read-only     # chỉ xem: ẩn mọi hành động ghi
```

`bint` không đối số in trợ giúp (thay vì mở TUI) khi stdin hoặc stdout không phải terminal (đường ống, CI,
chuyển hướng) hoặc khi chạy trong phiên agent; agent không điều khiển được TUI. Gõ `bint tui` trong phiên
agent vẫn mở được, nhưng các thao tác chỉ dành cho người (duyệt, áp dụng, loại bỏ) bị khoá kèm lý do.

**Bảy tab**: Tổng quan, Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá, Hệ thống. Mỗi tab có thẻ phụ riêng
(chọn bằng bàn phím hoặc chuột); phím của thẻ đang xem hiện ở viền dưới khung. Repository chưa `bint init`
thì TUI mở trình hướng dẫn khởi tạo.

**Phím toàn cục**

| Phím | Việc |
|---|---|
| `1`-`7` | chuyển thẳng tới tab (`1` Tổng quan … `7` Hệ thống) |
| `[` `]` | tab trước / sau |
| `:` hoặc `Ctrl+K` | bảng lệnh: tìm mờ trên mọi hành động của mọi màn, kể cả hành động không có phím |
| `?` | trợ giúp theo ngữ cảnh: giới thiệu thẻ đang xem, phím của thẻ, phím toàn cục, các màn khác (cuộn bằng `j`/`k`, `PgUp`/`PgDn`, bánh xe) |
| `!` | hộp Thông báo: nguyên văn thông báo hiện tại và 50 thông báo gần nhất (thông báo bị cắt bằng `…` luôn đọc đủ ở đây) |
| `L` | đổi ngôn ngữ trong phiên (vi ⇄ en) |
| `r` | làm mới ở Tổng quan (các màn khác: `Ctrl+R`) |
| `q` | thoát (còn việc nền thì hỏi: huỷ an toàn rồi thoát, hoặc ở lại) |

**Chuột** bật mặc định: bấm tab, thẻ phụ, hàng danh sách (bấm lại hàng đang chọn = `Enter`), phím ở chân phím
và nút của hộp thoại; bánh xe cuộn khung dưới con trỏ. Mọi thứ vẫn dùng được chỉ bằng bàn phím. Tắt/bật trong
phiên: bảng lệnh (`:`) → "Bật/tắt chuột".

**Chế độ chỉ đọc** (`--read-only`): mọi hành động ghi tệp, ghi cơ sở dữ liệu hoặc mở trình soạn thảo bị ẩn khỏi
chân phím, bảng lệnh và trợ giúp, và bị từ chối nếu vẫn được yêu cầu. Đây là chế độ tiện dụng để xem an toàn,
không phải ranh giới bảo mật (ranh giới là review Git và việc chặn thao tác chỉ dành cho người).

**Màu và NO_COLOR**: đặt `NO_COLOR` (bất kỳ giá trị khác rỗng) hoặc dùng terminal không màu thì TUI chuyển sang
chế độ đơn sắc: không dùng màu nào; khung đang chọn đảo màu tiêu đề, thông báo lỗi có dấu `✗`, hành động bị khoá
ghi rõ bằng chữ. Terminal 16 màu dùng bảng màu riêng; còn lại theo nền sáng/tối của terminal.

**Ngôn ngữ**: `--lang` > biến `BINT_LANG` > khoá `ui.language` trong cấu hình > `LC_ALL`/`LANG` > tiếng Việt.
Phím `L` đổi tạm trong phiên; Hệ thống › Ngôn ngữ lưu vào cấu hình (chọn lớp dự án hoặc riêng máy).

Thông điệp của tầng dịch vụ nhắc lệnh CLI (ví dụ "chạy `bint sync`") được TUI hiển thị bằng phím hoặc hành
động tương ứng của TUI; CLI giữ nguyên chữ.

## Tài liệu

Chi tiết từng thành phần được tách thành các hướng dẫn trong [docs/](docs/README.md):

| Chủ đề | Tài liệu |
|---|---|
| Mục lục toàn bộ tài liệu | [docs/README.md](docs/README.md) |
| Kiến trúc P0, luồng sự kiện và lưu trữ | [docs/architecture.md](docs/architecture.md) |
| Cấu hình YAML, harness/model theo role, ngân sách | [docs/configuration.md](docs/configuration.md) |
| Tri thức (P2), Context Engine và vòng đời (P3) | [docs/knowledge.md](docs/knowledge.md) |
| Workflow (P4), hợp đồng `b-req` và multi-harness (P5) | [docs/workflows.md](docs/workflows.md) |
| Khung đánh giá, chống rò rỉ và biến thể ngữ cảnh (P1) | [docs/evaluation.md](docs/evaluation.md) |
| Tiến hoá tri thức L1 (P6), skill L2 (P7), bài học và cycle (P8) | [docs/evolution.md](docs/evolution.md) |
| Thành phần quy trình của agent: guidance, guardrail, script, strategy (P8-H) | [docs/harness-components.md](docs/harness-components.md) |
| Trạng thái triển khai, kết quả thực nghiệm và lỗi còn mở | [docs/status.md](docs/status.md) |
| Phát triển, kiểm thử và CI/CD | [docs/ci-cd.md](docs/ci-cd.md) |

## Phát triển

```sh
go test ./...                 # gồm kiểm thử đầu-cuối build tệp thực thi thật
go test -race ./...           # cần CGO/gcc
GOOS=windows go vet ./...     # kiểm tra biên dịch chéo Windows
```

CI (GitHub Actions, `.github/workflows/ci.yml`) chạy lint (`gofmt`, `go mod tidy -diff`, `go vet`,
`staticcheck` cho Linux và Windows), `go test -race` và build chéo 6 nền tảng trên mỗi PR/push vào
`main`. Workflow release (`.github/workflows/release.yml`) đã viết sẵn nhưng đang khoá. Chi tiết
cách chạy trên máy cục bộ và kích hoạt CD: [docs/ci-cd.md](docs/ci-cd.md).
