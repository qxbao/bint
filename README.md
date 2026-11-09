# Bint

CLI hỗ trợ coding agent: ghi nhận lượt chạy, đo lường, và (ở các giai đoạn sau)
tri thức dự án có kiểm chứng. Kế hoạch đầy đủ: [docs/bint-plan.md](docs/bint-plan.md).

**Trạng thái: P0 (CLI, ghi nhận lượt chạy) + P1 (khung đánh giá).** Chưa có số liệu
đo mốc trên repository thử nghiệm — xem [Khung đánh giá](#khung-đánh-giá-p1).

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
internal/eval/            P1: khai thác tác vụ, kiểm tra rò rỉ, môi trường cô lập, nhánh, agent, chạy, thống kê, báo cáo
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

## Khung đánh giá (P1)

Đo xem một thay đổi (tri thức, skill, quy trình) có thực sự giúp agent trên **chính
repository này** hay không (mục 12 của kế hoạch). Người dùng cuối chỉ cần phần này khi
bật mức 3 (tiến hoá có kiểm chứng).

```sh
# 0. Xác nhận lệnh kiểm thử (Bint không bao giờ chạy lệnh đề xuất khi chưa được xác nhận)
#    .bint/config.yml hoặc .bint/local.yml:
#      eval:
#        test_command: "go test {test_dirs}"   # {test_dirs}/{test_files} = kiểm thử của tác vụ
#        setup_command: "go mod download"      # tuỳ chọn, chạy trước agent ở mỗi lượt
bint eval mine --verify          # khai thác commit có kiểm thử fail→pass làm ứng viên
bint eval show <id>              # đọc đề bài; sửa `prompt` trong tệp ứng viên nếu cần
bint eval approve <id>...        # duyệt vào .bint/evals/cases/ (commit được)
bint eval estimate --effect 0.1  # số lượt, chi phí, cỡ mẫu cần thiết
bint eval run --arms none --budget 20     # chạy; Ctrl-C an toàn, --resume <batch> để chạy tiếp
bint eval report [--cases] [--json]       # so sánh theo cặp + khoảng tin cậy 95%
```

**Khai thác (`mine`).** Chọn commit có một commit cha, thêm/sửa kiểm thử *và* sửa mã nguồn,
quy mô trong `eval.mine.*`, mô tả đủ dài. Đề bài được viết lại tự động từ thông điệp commit
(bỏ trailer, khối mã, diff, tham chiếu commit lời giải) rồi đối chiếu với lời giải:
`fail` nếu chứa nguyên dòng mã hoặc SHA của lời giải; `warn` nếu chứa chú thích của lời giải
hay định danh mới mà kiểm thử không dùng tới. `--verify` (hoặc `bint eval verify`) xác nhận
kiểm thử của commit **thất bại** ở commit cha và **qua** (2 lần, bắt kiểm thử chập chờn) với
lời giải thật; ứng viên không đạt bị loại. Ứng viên chứa thông điệp gốc nên chỉ nằm trong thư
mục trạng thái; `approve` từ chối đề bài `fail` (trừ `--force`) và tác vụ chưa kiểm chứng (trừ
`--unverified`), ghi người duyệt và gán tập `build/validation/test` (50/25/25, tất định theo ID).

**Cô lập (`run`).** Mỗi lượt là một repository Git **mới** chỉ chứa lịch sử tới commit gốc
(`git fetch <repo> <sha>` vào kho đệm, rồi clone cục bộ, gỡ remote) — không dùng
`git worktree` vì worktree dùng chung kho đối tượng và agent có thể `git show` commit lời giải.
Mọi tệp ngữ cảnh agent (`AGENTS.md`, `CLAUDE.md`, `.claude/`, `.bint/`, `.cursor/`…) bị gỡ và
commit. Agent làm xong → tệp kiểm thử của commit mới được áp vào → `test_command` quyết định
đạt/không. Thư mục làm việc bị xoá sau mỗi lượt (`--keep-workdirs` để giữ); log ở
`<state>/eval-work/logs/<batch>/`.

**Nhánh.** `none`: chỉ harness. `minimal`: thêm `AGENTS.md` từ `eval.minimal_context`
(mặc định `.bint/evals/minimal.md`, viết tay ở P1). `bint` cần Context Engine (P3) nên bị bỏ
khi lấy từ cấu hình, báo lỗi khi chỉ định bằng `--arms`.

**Chống rò rỉ.** `strict`: nhánh `minimal` chỉ dùng phiên bản tệp **tại commit gốc**; không có
thì lượt bị `skipped`. `economy`: dùng phiên bản hiện tại nếu không chứa mã của lời giải; báo
cáo đánh dấu kết quả chỉ để sàng lọc.

**Ngân sách.** Bắt buộc với Claude Code (`--budget` hoặc `eval.budget_usd`). Ước tính =
tác vụ × nhánh × lần lặp × chi phí trung bình mỗi lượt từ lịch sử (hoặc `--cost-per-run`);
vượt ngân sách thì từ chối chạy. Khi chạy, lượt mới không bắt đầu nếu đã tiêu + chi phí dự
kiến vượt ngân sách (trạng thái `budget_exhausted`, chạy tiếp bằng `--resume <batch> --budget
<tổng mới>`). `limits.per_run.max_cost_usd` → `claude --max-budget-usd` cho từng lượt.

**Báo cáo.** Tỷ lệ đạt mỗi nhánh (trung bình theo tác vụ, CI95 bootstrap 10 000 lần, hạt giống
cố định); so sánh theo cặp với mốc `none` trên cùng tác vụ cho tỷ lệ đạt, chi phí, thời gian,
số lượt agent; độ dao động giữa các lần lặp (độ lệch chuẩn trong tác vụ, số tác vụ không nhất
quán). Phần "độ tin cậy" chẩn đoán nhóm đầu của mục 12.9: CI quá rộng, mô hình/harness đổi
giữa chừng, lỗi hạ tầng > 10%, tác vụ bị sửa sau khi chạy, tác vụ chưa kiểm chứng, economy,
trần/sàn tỷ lệ đạt. `estimate` tính cỡ mẫu theo cặp (alpha 0.05, power 0.8) với độ lệch chuẩn
đo từ các đợt trước, hoặc 0.5 khi chưa có dữ liệu.

**Harness.** P1 hỗ trợ Claude Code: `claude -p --output-format json --no-session-persistence
--strict-mcp-config --permission-mode acceptEdits --allowedTools … --setting-sources
project,local` (cấu hình ở `eval.agent`), đọc chi phí, token, số lượt và mô hình từ JSON.
`--agent-command "<lệnh>"` dùng một tác nhân bất kỳ (đề bài qua stdin và
`$BINT_EVAL_PROMPT_FILE`); nếu stdout là JSON cùng định dạng thì số liệu được đọc.

Giới hạn đã biết của P1:

- Agent chạy với quyền của người dùng; mạng **không** bị chặn ở tầng hệ điều hành
  (`security.allow_network_in_evals` chỉ là chỉ dẫn). Mỗi đợt ghi lưu ý này vào báo cáo.
- Không có `--bare`, Claude Code vẫn có thể nạp `~/.claude/CLAUDE.md` của người dùng ở mọi
  nhánh (như nhau giữa các nhánh, nhưng không phải "không ngữ cảnh" tuyệt đối).
  `--bare` cô lập hoàn toàn nhưng cần `ANTHROPIC_API_KEY`.
- Agent mất lịch sử Git sau commit gốc (đúng ý) nhưng cũng không có remote/tag.
- Đề bài nháp lấy từ thông điệp commit; commit có mô tả sơ sài cho đề bài sơ sài — người
  duyệt nên viết lại trước khi `approve`.

Kết quả lưu trong SQLite (`eval_batches`, `evaluation_runs`, migration 0002); tác vụ đã
duyệt là nguồn chuẩn trong `.bint/evals/cases/*.yml` (fingerprint của đề bài + lệnh chấm
được ghi vào mỗi lượt để phát hiện tác vụ bị sửa).

## Phát triển

```sh
go test ./...                 # gồm kiểm thử đầu-cuối build tệp thực thi thật
go test -race ./...           # cần CGO/gcc
```
