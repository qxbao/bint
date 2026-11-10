# Bint

CLI hỗ trợ coding agent: ghi nhận lượt chạy, đo lường, và (ở các giai đoạn sau)
tri thức dự án có kiểm chứng. Kế hoạch đầy đủ: [docs/bint-plan.md](docs/bint-plan.md).

**Trạng thái: P0 (CLI, ghi nhận lượt chạy) + P1 (khung đánh giá) + P2 (bootstrap tối giản)
+ P3 (Context Engine và vòng đời tri thức) + P4 (workflow tối thiểu) + P5 (multi-harness:
Claude Code, Codex, Antigravity) + P6 (tiến hoá tri thức L1, chế độ propose) + P7 (tiến hoá skill L2).**
Chưa có số liệu đo trên repository thử nghiệm; cổng quyết định 1 và 2 chưa chạy — ngưỡng
đã chốt ở [docs/gate-1.md](docs/gate-1.md) và [docs/gate-2.md](docs/gate-2.md).

## Cài đặt

```sh
go install ./cmd/bint            # hoặc: go build -o bint ./cmd/bint
```

Go thuần, không cần CGO (SQLite qua `modernc.org/sqlite`, có FTS5), biên dịch chéo
được cho Linux/macOS/Windows. Cần git >= 2.31.

## Dùng

```sh
bint init --hooks        # khởi tạo; cài hook ghi nhận + skill gốc cho các harness được bật
bint doctor              # kiểm tra môi trường
bint runs list           # lượt chạy đã ghi nhận (tự nạp spool trước)
bint runs show <id>      # chi tiết, chấp nhận tiền tố ID
bint status
bint events ingest       # nạp spool thủ công
```

`bint init` chạy lại bao nhiêu lần cũng an toàn:

- Skill gốc được xuất bản cho từng harness được bật (`.claude/skills/` cho Claude Code,
  `.agents/skills/` cho Codex và Antigravity; phiên bản ghi trong `.bint/skills/core.lock`);
  bản bị sửa tay không bị ghi đè (`--force`), `--no-skills` để bỏ.

- `.bint/config.yml` chỉ được tạo khi chưa có; không bao giờ bị ghi đè.
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
internal/harness/         P5: HarnessAdapter, bảng khả năng, chọn harness theo role
internal/harness/hookcore/ P5: xử lý hook dùng chung (dialect theo harness)
internal/harness/claude/  adapter Claude Code: hook, .mcp.json
internal/harness/codex/   P5: adapter Codex: .codex/hooks.json, .codex/config.toml, apply_patch
internal/harness/antigravity/ P5: adapter Antigravity CLI: .agents/hooks.json, payload camelCase
internal/jsondoc/         sửa tệp JSON của harness, giữ thứ tự khoá
internal/fsafe/           ghi nguyên tử, sao lưu, khối đánh dấu, manifest băm
internal/detect/          phát hiện harness, lệnh test đề xuất, nhận diện lệnh test, che bí mật
internal/eval/            P1: khai thác tác vụ, kiểm tra rò rỉ, môi trường cô lập, nhánh, agent, chạy, thống kê, báo cáo
internal/knowledge/       P2: định dạng mục tri thức, bảng chuyển trạng thái, kiểm tra xác định được, dựng AGENTS.md
internal/inspect/         P2: static discovery (ngôn ngữ, manifest, lệnh, công cụ, tài liệu, mã sinh, điểm nóng Git)
internal/skills/          P2: skill gốc nhúng trong tệp thực thi (b-bootstrap), xuất bản + core.lock
internal/pathglob/        mẫu glob dùng chung
internal/symbols/         P3: vùng dòng của ký hiệu (go/parser cho Go, heuristic cho ngôn ngữ khác)
internal/kindex/          P3: chỉ mục FTS5 dẫn xuất từ tệp tri thức
internal/ctxengine/       P3: Context Engine — truy xuất theo tác vụ/đường dẫn, gói ngữ cảnh, cấu hình nhánh bint
internal/mcpserver/       P3: MCP server stdio và sáu công cụ
internal/task/            P4: phiếu tác vụ, bảng chuyển trạng thái, tài liệu, kiểm chứng, tiêu chí, review
internal/workflow/        P4: thao tác tác vụ dùng chung + orchestrator `bint run` (giới hạn vận hành)
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
bint eval bootstrap --budget 5   # nhánh minimal (strict): dựng tri thức tại commit gốc từng tác vụ
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

**Đặc tả đề bài (`mine`, `bint eval spec`).** Đề bài từ tiêu đề commit thường không nói tên
hàm/lớp/hằng mới mà kiểm thử ẩn gọi tới; agent không đoán được nên tác vụ không giải được ở mọi
nhánh. `mine` nối vào cuối đề bài khối *"Interface the hidden tests will use"*: đường dẫn và **chữ
ký** (không thân hàm, không giá trị hằng) của định danh vừa mới trong lời giải vừa được kiểm thử
dùng tới; kiểm tra rò rỉ bỏ qua đúng khối này. Cảnh báo thiếu đặc tả (cột ĐẶC TẢ, `show`,
`approve`): `interface_gap` — định danh mới với cả repository mà không trích được chữ ký (khoá
chuỗi, thuộc tính…); `exact_text` — kiểm thử so khớp nguyên văn chuỗi mới mà đề bài không nêu;
`short_prompt` — đề bài < 100 ký tự. `bint eval spec [id…|--all]` áp lại cho ứng viên cũ
(`--approved` cho tác vụ đã duyệt: đổi fingerprint).

**Cô lập (`run`).** Mỗi lượt là một repository Git **mới** chỉ chứa lịch sử tới commit gốc
(`git fetch <repo> <sha>` vào kho đệm, rồi clone cục bộ, gỡ remote) — không dùng
`git worktree` vì worktree dùng chung kho đối tượng và agent có thể `git show` commit lời giải.
Mọi tệp ngữ cảnh agent (`AGENTS.md`, `CLAUDE.md`, `.claude/`, `.bint/`, `.cursor/`…) bị gỡ và
commit. Agent làm xong → tệp kiểm thử của commit mới được áp vào → `test_command` quyết định
đạt/không. Thư mục làm việc bị xoá sau mỗi lượt (`--keep-workdirs` để giữ); log ở
`<state>/eval-work/logs/<batch>/`.

**Nhánh.** `none`: chỉ harness. `minimal`: thêm `AGENTS.md` dựng từ tri thức
(`eval.minimal_source: knowledge`, mặc định từ P2) hoặc từ tệp viết tay `eval.minimal_context`
(`minimal_source: file`). `bint` (P3): cùng AGENTS.md và **cùng tập tri thức** với `minimal`, thêm
MCP server `bint` (`--mcp-config`) và hook PreToolUse chèn theo đường dẫn (`--settings`), chạy
ngoài thư mục làm việc trên bản sao tri thức; truy xuất/trích dẫn ghi vào `eval_context_usage`.
`ideal` (chỉ để chẩn đoán, mục 12.9): AGENTS.md + mọi mục có phạm vi chồng lên tệp của lời giải
thật — cận trên, không phải kết quả của Bint. `bint`/`ideal` cần `minimal_source: knowledge`.

**Chống rò rỉ.** `strict`: nhánh `minimal` chỉ dùng tri thức dựng **tại commit gốc** — ảnh chụp do
`bint eval bootstrap` sinh (chạy `b-bootstrap` bằng agent trong repository cô lập tại commit gốc,
lưu ở `<state>/eval-work/knowledge/<sha>/`), hoặc `.bint/knowledge/items` đã commit tại commit
gốc (với `file`: phiên bản tệp tại commit gốc); không có thì lượt bị `skipped`. `economy`: tri thức
hiện tại, chỉ giữ mục có mọi bằng chứng tồn tại và khớp fingerprint tại commit gốc, commit bằng
chứng là tổ tiên của commit gốc, không rút ra từ chính tác vụ, và nội dung không chứa mã của lời
giải; báo cáo đánh dấu kết quả chỉ để sàng lọc.

**Kho đệm commit gốc.** Mỗi base được nạp vào một kho trần riêng (`<state>/eval-work/bases/<sha>.git`,
chỉ chứa lịch sử tới commit gốc, không có alternates) rồi clone cho từng lượt. Kho được tạo lười,
dọn ngay khi lượt cuối dùng base xong (`eval.keep_bases: true` để giữ, kèm `eval.base_cache_max_mb`
để giới hạn theo LRU), và `bint eval clean` xoá phần còn sót. Trước khi nạp, Bint kiểm tra đĩa còn
ít nhất `eval.min_free_mb` (mặc định 1024) và dừng có thông báo nếu thiếu. `eval.base_depth: N` nạp
nông; mặc định 0 = đầy đủ lịch sử, để số liệu so sánh được với các đợt cũ.

**Ngân sách.** Bắt buộc với Claude Code (`--budget` hoặc `eval.budget_usd`). Ước tính =
tác vụ × nhánh × lần lặp × chi phí trung bình mỗi lượt từ lịch sử (hoặc `--cost-per-run`);
vượt ngân sách thì từ chối chạy. Khi chạy, lượt mới không bắt đầu nếu đã tiêu + chi phí dự
kiến vượt ngân sách; khi chưa lượt nào đo được chi phí thì dùng giá trị tiên nghiệm
`eval.prior_run_usd` (mặc định $0,50; 0 = tắt), từ lượt thật đầu tiên dùng chi phí đo được
và thông báo nêu rõ lý do dừng (trạng thái `budget_exhausted`, chạy tiếp bằng `--resume <batch> --budget
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

## Tri thức (P2)

Bootstrap sinh **một số ít** tri thức có căn cứ, tập trung vào những điều bất thường mà agent
không tự đoán được (mục 4.2) — không sinh tổng quan kiến trúc hay mô tả cấu trúc thư mục.

```sh
bint bootstrap                  # static discovery + cài/cập nhật skill b-bootstrap
/b-bootstrap                    # trong Claude Code: agent điều tra, ghi mục candidate
bint knowledge review           # người duyệt từng mục: a/r/e/f/s/q
bint bootstrap publish          # xem trước rồi đưa quy tắc đã duyệt vào AGENTS.md

bint knowledge add --type warning --paths 'internal/db/**'   --evidence internal/db/db.go:40-52 "Mở SQLite bằng store.Open, không gọi sql.Open trực tiếp"
bint knowledge list | show <id> | verify [--apply] | approve <id> | reject <id> --reason …
bint knowledge transitions      # bảng chuyển trạng thái
bint inspect [--json]           # chỉ static discovery
bint bootstrap skill            # in nội dung skill cho harness chưa có adapter
```

**Định dạng.** Mỗi mục là `.bint/knowledge/items/<ulid>.md` (commit được): phần đầu YAML gồm
`type` (rule, warning, pattern, fact, decision, lesson), `status`, `scope` (glob đường dẫn, ký
hiệu, thẻ), `evidence` (`path`, `lines`, `symbol`, `commit`, `fingerprint`), `origin` (human |
agent | imported, phiên/lượt chạy, đầu vào đã đọc, cờ `third_party`, `eval_case`), tác giả,
người duyệt, thời điểm; sau đó là nội dung Markdown. Tệp là nguồn chuẩn duy nhất.

**Trạng thái** `candidate → verified → stale → deprecated`:

| Chuyển | Ai / điều kiện |
|---|---|
| candidate → verified (`rule`, loại trong `require_human_approval_for`, mục gắn cờ bên thứ ba) | người: `approve` / `review` |
| candidate → verified (loại khác) | `verify --apply` khi qua mọi kiểm tra, hoặc người |
| `origin: human` | tạo ở verified, tác giả là người duyệt |
| verified → stale | `verify --apply` khi bằng chứng đổi/mất |
| stale → verified | người xem lại: `approve --refresh` |
| * → deprecated | người: `reject` / `deprecate` (cần lý do, giữ tệp để kiểm toán) |

Agent không bao giờ chuyển được trạng thái hiệu lực: `approve`, `review`, `reject`,
`deprecate`, `verify --apply` và `add --origin human` bị từ chối khi phát hiện biến môi trường
của phiên agent (`CLAUDECODE`, `AI_AGENT`, `CODEX_SANDBOX`, `GEMINI_CLI`, `BINT_AGENT`). Đây là
lớp phòng vệ bổ sung, không phải ranh giới bảo mật — agent có shell vẫn xoá được biến môi
trường; kiểm soát chính là review thay đổi `.bint/knowledge/` qua Git.

**Kiểm tra xác định được** (`bint knowledge verify`, chạy cả khi `add`; mục không qua thì không
được ghi): tệp và vùng dòng bằng chứng tồn tại; fingerprint vùng dòng khớp (bỏ qua CRLF và khoảng
trắng cuối dòng); ký hiệu có trong bằng chứng hoặc tệp thuộc phạm vi; mẫu phạm vi khớp ít nhất một
tệp; ví dụ mã khớp mã thật (` ```go path=… ` đối chiếu đúng tệp; khối shell/`text` bỏ qua);
đường dẫn nhắc tới trong nội dung tồn tại (cảnh báo); trùng lặp; đầu vào bên thứ ba
(vendor/node_modules/URL/issue) phải được gắn cờ; cảnh báo tổng quan kiến trúc và nội dung quá dài.

**AGENTS.md.** `bootstrap publish` (và `init --agents-md`) dựng khối `bint:begin … bint:end` từ
mục `rule` và `warning` ở trạng thái verified — quy tắc toàn cục trước, rồi theo phạm vi đường
dẫn; bỏ mục stale và mục bên thứ ba chưa duyệt; tối đa `knowledge.agents_md_max_items` (15). Luôn
in bản thay đổi; không có terminal thì chỉ xem trước (`--yes` để ghi). CLAUDE.md nhập lại
`@AGENTS.md`. Nội dung ngoài khối không bị chạm; khối bị sửa tay → xung đột.

Giới hạn đã biết của P2:

- (Đã xử lý ở P3: fingerprint theo ký hiệu, định vị lại vùng dòng.) Ở P2: chèn thêm dòng phía
  trên vùng bằng chứng làm mục thành stale dù nội dung không đổi.
- Chưa phát hiện tự động mâu thuẫn giữa các mục, hay với tài liệu sẵn có; người duyệt chịu trách nhiệm.
- `knowledge review` đọc lựa chọn theo dòng (phím + Enter), chưa đọc một phím.
- Skill chỉ được xuất bản cho Claude Code; harness khác dùng `bint bootstrap skill` (adapter ở P5).

## Context Engine và vòng đời tri thức (P3)

```sh
bint context "thêm cột email vào bảng users"     # gói ngữ cảnh theo tác vụ (FTS5 + phạm vi + bằng chứng)
bint context --paths internal/db/db.go           # quy tắc/cảnh báo có phạm vi khớp tệp
bint init --hooks --mcp                          # hook PreToolUse chèn theo đường dẫn + .mcp.json
bint mcp serve                                   # MCP stdio: context_for_task, context_for_paths,
                                                 #   knowledge_search, knowledge_get, cite_knowledge, report_observation
bint knowledge search "migration" | stats | diff [ORIG_HEAD..HEAD]
bint knowledge revalidate --budget 1             # agent nhận định lại mục stale rủi ro cao
bint index rebuild                               # dựng lại chỉ mục (bình thường tự đồng bộ)
bint runs rate <id> good|bad                     # chấm lượt chạy (tín hiệu mạnh)
bint runs scan                                   # gắn commit với lượt, phát hiện hoàn tác
```

**Chỉ mục.** Bảng `knowledge_index` + FTS5 (`unicode61 remove_diacritics 2`: "tien te" khớp
"tiền tệ"; ký hiệu CamelCase được tách từ) trong SQLite là dữ liệu **dẫn xuất**: mọi lệnh truy xuất
so chữ ký thư mục `.bint/knowledge/items` và chỉ ghi lại mục đổi. Repository chưa có thư mục trạng
thái thì chỉ mục nằm trong bộ nhớ.

**Truy xuất** (mục 8.2). Theo tác vụ: từ khoá BM25 (giữ kết quả ≥ 30 % điểm tốt nhất), đường dẫn
nêu trong mô tả, miền/thẻ/ký hiệu → loại mục deprecated, mục bên thứ ba chưa duyệt, mục rút ra từ
chính tác vụ đánh giá → kiểm tra bằng chứng ngay lúc truy xuất → loại trùng → quy tắc trước → cắt
theo `context.max_tokens` (~3 ký tự/token). Theo đường dẫn: chỉ mục có phạm vi cụ thể khớp (mục toàn
cục đã ở AGENTS.md), ưu tiên phạm vi hẹp, tối đa `context.path_max_items`, không lặp mục đã chèn
trong phiên. Nhãn: **quy tắc bắt buộc** chỉ khi `rule` đã duyệt, không gắn cờ bên thứ ba và bằng
chứng còn khớp; mục chưa duyệt hoặc bằng chứng đã đổi thành **tham khảo** kèm cảnh báo. Mọi đầu ra
nằm trong khung `<bint-context>` "dữ liệu tham khảo", kèm `[k:<id>]` và nguồn gốc (mục 15.5).

**Hook** (Claude Code). `PreToolUse` cho `Read|Edit|Write|MultiEdit|NotebookEdit`: trả
`hookSpecificOutput.additionalContext`; quá `context.path_hook_timeout_ms` (1500) thì bỏ qua, không
chặn agent; tắt bằng `context.interfaces.path_hooks: false`. `PostToolUse` cho `mcp__bint__.*` ghi
`context.retrieved`, `knowledge.cited`, `observation.reported` vào spool — hook biết `session_id`
nên quy được về lượt chạy; MCP server thì không, nên không có hook thì truy xuất qua MCP không được
quy về lượt nào. Cài lại hook sau khi nâng cấp: `bint init --hooks`.

**Quy công** (mục 9.7). `context_usages` = được truy xuất, `knowledge_citations` = được áp dụng
(agent gọi `cite_knowledge`). `knowledge stats` báo hai mức này và tác giả; tác động thực chỉ đo
bằng đánh giá loại bỏ.

**Chống lỗi thời** (mục 8.4). Fingerprint mặc định theo **ký hiệu** (`context.freshness.fingerprint:
symbol`): Go phân tích bằng `go/parser`; JS/TS, Java, Kotlin, Rust, C/C++, C#, PHP, Swift, Scala, Dart
theo dòng khai báo + đếm ngoặc; Python/Ruby/YAML theo thụt lề; TOML theo bảng — các ngôn ngữ ngoài Go
là heuristic. Kế hoạch đề xuất tree-sitter, nhưng binding Go đều cần CGO, trái quyết định biên dịch
Go thuần; `symbols.Locator` là điểm cắm khi có bản thuần Go. Không định vị được ký hiệu → vùng dòng →
cả tệp. Mục cũ được kiểm tra đúng theo mức fingerprint đã lưu. Với vùng dòng, nếu nội dung cũ còn
nguyên ở vị trí khác (khối thay đổi không chồng lên vùng) thì mục **không** lỗi thời; `verify --apply`
cập nhật `lines`. Xác thực lại: loại rủi ro thấp (fact, pattern, decision, lesson) qua mọi kiểm tra
trừ fingerprint → `verify --apply` làm mới fingerprint và đưa về verified; `rule`/`warning` cần người
(`approve --refresh`), `knowledge revalidate` cho agent (chỉ Read/Grep/Glob, theo lô, trong ngân sách)
đưa nhận định valid/outdated/unsure vào `knowledge_revalidations` — không tự đổi trạng thái. Mục stale
quá `knowledge.stale_deprecate_days` (30) được đề xuất deprecated.

**Tín hiệu kết quả** (mục 9.6, bảng `outcome_signals`, không gộp thành một điểm): `tests` (mạnh nếu
lệnh khớp `eval.test_command` đã xác nhận, ngược lại trung bình), `rating` (người chấm, mạnh), `committed`
(commit chạm tệp lượt đã sửa trong 24 giờ sau lượt, yếu — gắn theo thời gian/tệp là gần đúng),
`reverted` (commit đó bị `git revert` trong 14 ngày, mạnh tiêu cực). Nhãn: hoàn tác hoặc chấm bad →
failure; chấm good → success; lần kiểm thử mạnh cuối cùng quyết định; còn lại `unlabeled`. Hiện ở
`runs list` và `runs show`.

**Chẩn đoán cổng 2** (mục 12.9). Đợt có `bint`/`ideal`: báo cáo thêm so sánh theo cặp với `minimal`
và xét theo thứ tự: đo lường → nội dung (`ideal` không hơn `minimal`) → truy xuất bỏ sót (độ phủ tập
đúng < 50 %) → không được áp dụng (mục đúng ít được trích dẫn ở lượt thất bại) → nhiễu. Chỉ là dữ liệu
cho người quyết định.

Giới hạn đã biết của P3:

- Chưa đo trên repository thử nghiệm (ngưỡng cổng 2 đã chốt, chưa chạy).
- Phân loại loại công việc theo từ khoá thô; chưa có truy xuất ngữ nghĩa (mục 8.3: chỉ thêm khi FTS5
  không đạt).
- Gói ngữ cảnh chưa gồm skill có thể áp dụng (skill thuộc P4).
- Tín hiệu "người dùng sửa lại diff của agent trong phiên" chưa thu.
- Hook `post-merge` tuỳ chọn (mục 8.8) chưa có; dùng `bint knowledge diff` sau `git pull`.
- MCP server tự hiện thực phần tối thiểu của giao thức (initialize, ping, tools/list, tools/call).

## Workflow tối thiểu (P4)

Bốn skill gốc mới — `b-req` (phân tích, lập kế hoạch), `b-feat` (thực thi), `b-review` (đánh giá
thay đổi), `b-do` (điều phối) — chỉ ở mức tối thiểu cần cho ghi nhận và điều phối (mục 6). Mọi đầu
ra liên kết bằng **task ID**.

```sh
# Trong Claude Code (skill được xuất bản khi init/sync):
/b-do Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db

# Hoặc Bint Core điều phối theo chương trình (adapter cấp 3, giới hạn áp ở tầng chương trình):
bint run b-do "Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db"
bint run b-req|b-feat|b-review --task <id>      # một bước trên tác vụ có sẵn
bint run status <task-id|run-id>                # tác vụ + các lượt chạy (cha b-do, con từng bước)
bint run resume <task-id>                       # tiếp tục tác vụ blocked / interrupted / failed

bint task new "<mô tả tự do>"                   # Classify: lập phiếu tác vụ (mục 6.7)
bint task list | show <id> | set <id> … | transitions
bint task write <id> requirement|plan|tasks -   # tài liệu (agent ghi qua stdin)
bint task verify <id>                           # chạy lệnh kiểm chứng (chỉ khi người đã xác nhận)
bint task criteria <id> <n> met|unmet -e "…"    # đối chiếu tiêu chí chấp nhận
bint task error <id> --step b-feat "…"          # ghi lỗi / lần thử thất bại
bint task review <id> -                         # vòng review (JSON) → review.json + review.md
bint task approve <id>                          # NGƯỜI: xác nhận lệnh kiểm chứng, duyệt đường dẫn bảo vệ
bint task status <id> done [--force]            # done cần bằng chứng; chỉ người --force được

bint skill list | show <tên> [--core] | overlay <tên>
bint sync                                       # ghép skill gốc + lớp phủ, xuất bản, cập nhật core.lock
bint knowledge add --from-review <task-id>.<n>  # phát hiện review → mục warning/lesson
```

**Tác vụ** nằm ở `<state>/runtime/tasks/<task-id>/` (ngoài repository): `task.yml` (phiếu: `goal`,
`verify.command`/`criteria`, `scope.allow`/`deny`, `constraints`, `autonomy`; sửa được bằng tay),
`state.json` (do Bint ghi qua khoá thư mục: trạng thái, lịch sử chuyển, kiểm chứng, tiêu chí, duyệt,
lỗi, lượt chạy), `requirement.md`, `plan.md`, `tasks.md`, `review.json`/`review.md`, `logs/`.
Classify xác định được: mục tiêu = dòng đầu mô tả; `scope.allow` = đường dẫn nêu trong mô tả mà có
thật; `verify.command` = `eval.test_command` đã xác nhận; đề xuất bỏ b-req cho việc nhỏ (heuristic,
`--plan`/`--no-plan` ghi đè). Chỉ hỏi lại khi tác vụ chạm đường dẫn bảo vệ mà không có cách kiểm chứng.

**Trạng thái:** `created → planned → in_progress → in_review → done`; `blocked`, `interrupted`,
`failed` nhớ trạng thái trước để tiếp tục; `cancelled` kết thúc. **done cần bằng chứng**: lệnh kiểm
chứng đã được người xác nhận và lần chạy cuối qua trên **đúng trạng thái mã hiện tại** (fingerprint
HEAD + diff + tệp chưa theo dõi), hoặc — khi không có lệnh kiểm chứng — mọi tiêu chí được đối chiếu là
đạt; không tiêu chí nào bị đánh dấu chưa đạt; không có thay đổi chạm đường dẫn bảo vệ chưa duyệt.

**Chỉ người** (chặn khi phát hiện biến môi trường của phiên agent, như ở P2): xác nhận lệnh kiểm
chứng và duyệt thay đổi (`task approve`), đặt `autonomy: auto`, `done --force`. Lệnh kiểm chứng do
agent đề xuất không được chạy trước khi người xác nhận (`security.require_approval_for_commands`).

**`bint run`** cấp `run_id` cho lượt b-do (cha) và mỗi bước (con, `parent_run_id`), ghi `run_steps`,
chi phí/token/mô hình; agent nhận `BINT_RUN_ID`, `BINT_TASK_ID`, `BINT_AGENT=1`, nên lệnh `bint task`
và hook của Claude Code gắn sự kiện vào đúng lượt con. Đề bài = skill (gốc + lớp phủ) + khối
`<bint-task>` (phiếu, tài liệu, phát hiện review cần sửa, phạm vi diff) + gói ngữ cảnh của Context
Engine. b-req/b-review chạy với `workflow.agent.readonly_tools` và `--disallowedTools Edit,Write,…`.
Giới hạn vận hành (mục 5):

| Giới hạn | Áp dụng |
|---|---|
| `limits.workflows.<wf>.max_cost_usd` (mặc định `limits.per_run`) | b-do: tổng các bước; mỗi bước `--max-budget-usd` = min(giới hạn bước, phần còn lại); hết → blocked |
| `limits.workflows.<wf>.max_turns` | `--max-turns` cho bước |
| `limits.protected_paths`, `scope.deny` | sau b-feat, đối chiếu tệp đã sửa với commit gốc; có terminal thì hỏi, không thì blocked chờ `task approve` |
| ngoài `scope.allow` | chỉ hỏi khi `autonomy: ask_always` (ngoài ra ghi chú) |
| `workflow.max_review_rounds` (2) | phát hiện `bug`/`risk` mức critical/high → quay lại b-feat; hết vòng → blocked |
| `workflow.timeout_minutes`, `verify_timeout_minutes` | mỗi bước agent / lệnh kiểm chứng |

Trong phiên Claude Code (không qua `bint run`), hook `PreToolUse` trả `permissionDecision: "ask"`
khi agent sửa tệp thuộc `limits.protected_paths` (cần cài lại hook: `bint init --hooks`); không có
hook thì giới hạn chỉ là chỉ dẫn trong skill. Ctrl-C trong `bint run` → bước bị dừng, tác vụ
`interrupted`; tiến trình bị kill → lần đọc tác vụ sau phát hiện tiến trình đã chết và chuyển
`interrupted`. Sau khi người duyệt, `resume` không chạy lại b-feat mà tiếp tục kiểm tra → kiểm chứng →
review.

**Tín hiệu kết quả mới** (mục 9.6): `verify` (lệnh kiểm chứng; mạnh nếu đã xác nhận — quyết định nhãn
như kiểm thử mạnh), `criteria` (agent tự đối chiếu, trung bình), `review` (phát hiện nghiêm trọng,
trung bình, tiêu cực). "Không phát hiện vấn đề" không tạo tín hiệu tích cực.

**Skill gốc và lớp phủ** (mục 6.6). Skill gốc khai báo điểm neo `<!-- bint:anchor <tên> -->` …
`<!-- bint:anchor-end <tên> -->` (b-req: `project-rules`, `plan-template`; b-feat: `project-rules`,
`test-strategy`; b-review: `review-checklist`; b-do: `routing`; b-bootstrap: `investigation-focus`).
Lớp phủ `.bint/skills/overlays/<skill>.md` (commit được) chỉ chứa khối
`<!-- bint:overlay <tên> append|prepend|replace -->` … `<!-- bint:overlay-end -->` ở cột đầu; văn bản
khác bị bỏ qua. `bint sync` (và `init`, `bootstrap`) ghép rồi xuất bản; `core.lock` ghi băm skill gốc,
lớp phủ và bản đã xuất bản. Nâng cấp Bint: skill gốc mới được áp lại lớp phủ; nếu lớp phủ dùng điểm neo
không còn → **xung đột**, bản đã xuất bản và mục `core.lock` cũ được giữ nguyên cho tới khi sửa lớp phủ
(`--force` không vượt qua được). Skill đã xuất bản bị sửa tay → xung đột như P2.

Cấu hình mới: khối `workflow:` (`max_review_rounds`, `timeout_minutes`, `verify_timeout_minutes`,
`agent.{model, permission_mode, allowed_tools, readonly_tools, setting_sources, extra_args}`) và
`limits.workflows`. Migration 0004 chỉ thêm chỉ mục `runs(task_id)`, `runs(parent_run_id)`.

Giới hạn đã biết của P4:

- Classify bằng heuristic từ khoá; chưa có agent phân loại.
- Agent chạy trên cây làm việc hiện tại (không worktree cô lập); P5 thêm khoá ghi theo worktree.
- Kiểm tra đường dẫn bảo vệ của `bint run` là **sau** b-feat (đối chiếu diff), không chặn trước; trong
  phiên tương tác thì hook hỏi trước khi sửa.
- Agent không báo chi phí (`--agent-command`): giới hạn chi phí chỉ là chỉ dẫn (ghi chú trong kết quả).
- Tiếp tục từ `in_review` chạy lại b-review (không dùng lại vòng review cũ, vì mã có thể đã đổi).
- Chưa xuất tài liệu sang `docs/spec/<feature>/`.

## Multi-Harness (P5)

Workflow (skill gốc + lớp phủ, phiếu tác vụ, `bint run`) và nguồn tri thức (`.bint/knowledge`,
AGENTS.md, Context Engine) giờ tách khỏi harness thực thi. Mỗi harness có một **adapter** (mục 7)
khai báo bảng khả năng đã đối chiếu với một phiên bản cụ thể; khả năng không có được báo, không
được mô phỏng.

```sh
bint harness list                 # harness trên máy, phiên bản, cấp hỗ trợ, bảng khả năng
bint harness show codex           # chi tiết từng khả năng (--json)
bint init --harness claude,codex,antigravity --hooks --mcp
bint run b-do --role implementer=codex "…"        # b-feat bằng Codex, các bước khác theo roles
bint run b-review --harness antigravity --task <id>
bint eval run --harness codex …                   # đo trên harness khác (nhánh không hỗ trợ → skipped)
```

| Khả năng | Claude Code | Codex CLI (0.167) | Antigravity CLI (agy 1.5.3) |
|---|---|---|---|
| Skill | `.claude/skills` | `.agents/skills` | `.agents/skills` (dùng chung với Codex) |
| Ẩn skill (`disable-model-invocation`) | ✓ | ✗ (khoá bị bỏ khi xuất bản) | ✗ |
| MCP server của Bint | `.mcp.json` | `.codex/config.toml` — cần dự án được Codex tin cậy | ✗ (chỉ cấp người dùng; dùng CLI `bint`) |
| Hook ghi nhận | SessionStart/End, PostToolUse(+Failure) | `.codex/hooks.json`: SessionStart/End, PreToolUse, PostToolUse — mỗi hook cần được duyệt trong Codex | `.agents/hooks.json`: SessionStart, PreToolUse, PostToolUse — không có kết thúc phiên |
| Chèn ngữ cảnh theo đường dẫn | trước Read/Edit/Write | chỉ trước `apply_patch` | ✗ (PreToolUse chỉ trả quyết định) |
| Đường dẫn bảo vệ | hỏi (`ask`) | chặn (`deny`) kèm lý do | hỏi (`ask`) |
| Thực thi (`bint run`) | `claude -p` | `codex exec --json` (stdin) | `agy --print=… --output-format json` |
| Bước chỉ đọc | cấm công cụ sửa tệp | sandbox `workspace-write` | `--mode plan` |
| Chi phí / giới hạn lượt | ✓ / ✓ | token / ✗ | token / ✗ |

Cấp hỗ trợ (mục 7): cả ba ở **cấp 3** (Skill + Hook + thực thi theo chương trình); cấp 4 chưa có.

**Chọn harness** cho từng bước theo thứ tự (mục 5): `--role role=harness` → `--harness` →
`roles.planner|implementer|reviewer` → `harnesses.default`. Harness phải nằm trong
`harnesses.enabled` — yêu cầu dòng lệnh không vượt qua cấu hình dự án. Tham số gọi Codex/Antigravity
ở `harnesses.codex|antigravity.{binary, model, extra_args}`; Bint không tự thêm cờ bỏ quyền
(`--dangerously-*`). Adapter không khả dụng (thiếu chương trình) → bước thất bại với lỗi nêu cách xử lý.

**Cô lập bước có quyền ghi:** mỗi worktree chỉ một bước có quyền ghi (b-feat) chạy tại một thời điểm,
bất kể harness (khoá trong `<state>/runtime/locks/`, khoá của tiến trình đã chết bị thay); muốn chạy
song song thì dùng worktree khác. Bước chỉ đọc (b-req, b-review) được **kiểm tra lại ở tầng chương
trình**: ảnh chụp cây làm việc (chỉ mục Git tạm, trừ `.bint/`) trước và sau bước phải trùng nhau,
không thì bước thất bại kèm danh sách tệp bị sửa (Bint không tự hoàn tác). Codex chạy mọi bước trong
sandbox `workspace-write` kèm `--add-dir <thư mục trạng thái>` để lệnh `bint task …` ghi được; agent
nhận `BINT_RUNNER_PID` để không nhầm tiến trình điều phối (bị sandbox che PID) là đã chết.

**Chuẩn hoá sự kiện** (mục 13.2): `bint hook codex` (khuôn payload như Claude Code; `apply_patch`
sửa nhiều tệp → mỗi tệp một `file.modified`) và `bint hook antigravity <sự kiện>` (payload camelCase
`toolCall{name,args}`, không chứa tên sự kiện) chuẩn hoá về cùng mô hình lượt chạy. Sự kiện harness
không có thì không tổng hợp giả: lượt Antigravity không có `session.ended` và sẽ được đóng
`interrupted` theo thời gian như mọi lượt không có sự kiện kết thúc.

**Đánh giá trên nhiều harness:** `eval.harness` / `bint eval run --harness` nhận `claude | codex |
antigravity`; harness ghi trong đợt đánh giá nên không trộn kết quả. Nhánh `bint` trên Codex = MCP
(`-c mcp_servers.bint…`) + AGENTS.md, không chèn theo đường dẫn (hook Codex cần duyệt trust); trên
Antigravity nhánh `bint` bị bỏ qua (`skipped`, kèm lý do). Codex/Antigravity không báo chi phí: ngân
sách chỉ là chỉ dẫn.

Đã kiểm chứng trên máy phát triển: payload hook và đầu ra `codex exec --json` của codex-cli 0.167
(gồm việc agent nhận `additionalContext` của PreToolUse); chuỗi `bint run b-req → b-feat → b-review
--harness codex` chạy thật trên repository nháp. Antigravity: định dạng hook/skill theo tài liệu đi
kèm agy 1.5.3 và payload SessionStart/Stop bắt được; payload công cụ (tên `write_to_file`,
`run_command`, khoá `TargetFile`/`CommandLine`) chưa bắt được trực tiếp (hết quota) — bảng khả năng
ghi rõ điều này.

Giới hạn đã biết của P5:

- Hook Codex chỉ chạy sau khi người dùng duyệt trong Codex (`/hooks`); `bint doctor` báo khi chưa duyệt.
- Codex không có công cụ đọc tệp riêng nên chèn ngữ cảnh theo đường dẫn chỉ xảy ra trước khi sửa.
- Antigravity: không có MCP cấp dự án, không chèn ngữ cảnh qua hook; đề bài `bint run` đi qua một đối
  số dòng lệnh (tối đa 120 KiB).
- Chưa cô lập bằng worktree riêng cho từng bước ghi; chưa có cấp 4 (điều phối nhiều agent song song).

## Tiến hoá tri thức — L1 (P6)

Evolution Engine cấp L1 (mục 10–11 của kế hoạch), chế độ **propose**: Bint phát hiện cơ hội,
soạn đề xuất có bằng chứng, đối chiếu bằng khung đánh giá; **người duyệt** quyết định áp dụng.
Không thay đổi nào được tự áp dụng (`evolution.mode: auto` thuộc P8).

```sh
bint evolve scan                         # cơ hội + điều kiện tiên quyết (không gọi mô hình)
bint evolve propose --no-agent           # đề xuất xác định: loại bỏ, gộp mục trùng
bint evolve propose op-1a2b3c4d --budget 1   # cơ hội tạo/sửa tri thức: agent evolver soạn
bint evolve show ep_…                    # thay đổi, phiên bản gốc, căn cứ, kế hoạch, kết quả
bint evolve evaluate ep_… --split validation --budget 5 --yes
bint evolve evaluate ep_… --split test --budget 10 --yes
bint evolve apply ep_…                   # cần kết quả supported, độc lập trên test
bint evolve apply ep_… --unverified      # hoặc: chấp nhận rõ là chưa kiểm chứng (được ghi lại)
bint evolve rollback ep_…
bint evolve history                      # kiểm chứng, đối chiếu mô hình, theo dõi sau áp dụng, chi phí
```

**Dữ liệu** (chỉ đọc, đã lọc): lượt chạy đã kết thúc trong cửa sổ `memory.retention_days`, các
bước workflow gộp vào lượt gốc; nhãn kết quả tính từ tín hiệu (mục 9.6) sau khi quét Git để cập
nhật commit/hoàn tác đến muộn — **lượt `unlabeled` không bao giờ là bằng chứng**. Hook Claude Code
giờ ghi thêm `file.read` (tệp agent đọc) để đo khám phá lặp lại; Codex/Antigravity không có hook
trước công cụ đọc nên chỉ có tệp được sửa.

**Nhóm tác vụ** (MVP, mục 10.3): theo khu vực mã chính của lượt (hai cấp thư mục đầu của tệp được
sửa). **Cơ hội** (mục 10.2):

| Loại | Điều kiện | Hành động | Tạo bởi |
|---|---|---|---|
| `exploration` | tệp (hoặc ≥ 2 tệp cùng mẫu tên trong một thư mục: `handlers/*_handler.py`, `test_*.py`, `*_test.go`) được chạm lại ở ≥ `min_similar_runs` lượt có nhãn, không mục nào có phạm vi khớp | create | agent |
| `failure_hotspot` | tệp bị sửa ở ≥ `min_new_evidence` lượt thất bại, chưa có warning/rule | create | agent |
| `stale_used` | mục stale vẫn được truy xuất, hoặc `revalidate` nhận định outdated | update | agent |
| `stale_expired` | stale quá `knowledge.stale_deprecate_days` | deprecate | xác định |
| `unused` | truy xuất ở ≥ `min_similar_runs` lượt có nhãn, chưa từng được trích dẫn | deprecate | xác định |
| `duplicate` | cùng loại, phạm vi chồng, tiêu đề gần trùng | merge | xác định |

Cơ hội dựa trên lượt chạy cần đủ lượt có nhãn **và** ≥ `min_new_evidence` lượt chưa dùng cho đề
xuất trước cùng đối tượng; cơ hội đã có đề xuất đang chờ thì không đề xuất lại.

**Điều kiện tiên quyết** (mục 10.6) được kiểm tra ở mỗi lần quét: lượt có nhãn, tác vụ đánh giá đã
kiểm chứng ở tập validation/test đủ cỡ mẫu cho `evolution.evaluation.min_effect` (độ lệch chuẩn đo từ
các đợt trước), ước tính chi phí một lần đối chiếu nằm trong `evolution.evaluation.budget_usd`. Không
đạt → đề xuất vẫn được tạo nhưng ghi rõ **chưa được kiểm chứng định lượng**.

**Đề xuất** (mục 10.4) lưu trong cơ sở dữ liệu trạng thái cục bộ (bảng `evolution_proposals`, schema
có phiên bản) và bất biến: hành động, **phiên bản gốc** của mỗi mục bị sửa (phiên bản + băm tệp), lượt
làm căn cứ kèm nhãn lúc đó, thay đổi (chỉ nội dung: loại, tiêu đề, thân, phạm vi, bằng chứng — không
đặt được trạng thái, người duyệt, nguồn gốc hay cờ bên thứ ba), **rủi ro do Bint tính** (sửa/loại bỏ
rule/warning đã duyệt = cao), **kế hoạch đánh giá chốt trước** (`superiority` trên
`evolution.evaluation.metric`, hoặc `non_inferiority` với `non_inferiority_margin` cho loại bỏ/gộp).
Agent evolver (`roles.evolver`, chỉ Read/Grep/Glob, dữ liệu đóng khung là dữ liệu không đáng tin) trả
JSON; mỗi ứng viên phải qua kiểm tra xác định được như `bint knowledge add` (bằng chứng tồn tại, không
trùng, độ dài, cờ bên thứ ba tự gắn) — không qua thì bị loại kèm lý do. Agent sửa cây làm việc → bỏ kết quả.

**Đối chiếu** (`evaluate`, mục 11.2) dùng khung đánh giá với hai nhánh mới `base` và `candidate`: cùng
tập tri thức của tác vụ (lọc chống rò rỉ theo `evolution.evaluation.leakage_mode`), cùng cách đưa tới
agent (`delivery: bint | minimal`); `candidate` = sau khi áp đề xuất. Đề xuất deprecate chính là
**đánh giá loại bỏ** (mục 9.7, 12.4). Chỉ chạy tác vụ đã duyệt, đã kiểm chứng fail→pass, mà đề xuất thực
sự đổi tập tri thức và mục đổi có phạm vi liên quan tới tệp của tác vụ.

Nội dung mới của đề xuất luôn được viết *sau* các tác vụ trong lịch sử, nên không qua được điều kiện nguồn
gốc của economy (A3: mục tạo sau commit gốc bị loại). Thay vào đó, đối chiếu **hồi cứu** dùng ba lớp chặn:
(1) bằng chứng của nội dung phải đã khớp tại commit gốc; (2) nội dung không chứa mã của lời giải;
(3) loại tác vụ mà lời giải có thể đến từ chính các lượt làm căn cứ của đề xuất — commit lời giải được quét
Git gắn với lượt đó, hoặc lượt đó sửa tệp của lời giải và commit nằm trong cửa sổ 24 giờ sau lượt. Rủi ro rò
rỉ gián tiếp còn lại được ghi trong kết luận. `--prospective` chỉ dùng tác vụ sinh sau đề xuất (**tiến cứu**)
để loại hẳn rủi ro này. `show`, `apply` và `history` ghi rõ lần đối chiếu làm căn cứ là tiến cứu hay hồi cứu
(đề xuất chỉ loại bỏ tri thức không có nội dung mới nên không phân loại). Kết luận
`supported | not_supported | inconclusive` theo kế hoạch đã chốt (cảnh báo đo lường chặn → inconclusive).
Trên tập **test**, nếu tác vụ đã dùng để đối chiếu một đề xuất khác cùng đối tượng thì kết quả bị đánh
dấu **không độc lập** (mục 12.2).

**Áp dụng** (`apply`, chỉ người; khoá tiến hoá trong `<state>/runtime/locks/evolve.lock`):
- Phiên bản gốc phải còn khớp — mục bị sửa sau khi đề xuất (người khác áp dụng, `git pull`, sửa tay)
  làm đề xuất **không hợp lệ** (`list` hiện `outdated`); bác bỏ và đề xuất lại.
- Cần kết quả `supported`, độc lập, trên test, với đúng phiên bản gốc hiện tại — hoặc `--unverified`.
- Rủi ro cao cần `--confirm-risk`. Mọi mục được kiểm tra xác định được lại trước khi ghi.
- Mục mới do agent soạn vào `candidate` (duyệt bằng `bint knowledge review`); mục được sửa nhận người
  áp dụng làm người duyệt; loại bỏ = `deprecated` (`delete` chỉ cho mục candidate/deprecated).
- Bản ghi kiểm toán (`evolution_adoptions`) giữ nội dung trước/sau từng tệp: `rollback` khôi phục, từ
  chối nếu tệp đã bị sửa sau khi áp dụng (trừ `--force`).

`history` gắn mỗi lần áp dụng với harness/mô hình của lần đối chiếu làm căn cứ và báo **chưa xác thực
với mô hình hiện tại** khi mô hình đổi (`evolution.adoption.bind_to_model_version`, mục 12.6); theo dõi
số lần truy xuất/trích dẫn sau áp dụng (chỉ là gợi ý, không phải tác động); chi phí tạo ứng viên và
chi phí đối chiếu tính riêng (mục 12.8). `show` báo khi nhãn của lượt làm căn cứ đã đổi do tín hiệu đến
muộn. `bint status` nhắc khi có đề xuất chờ duyệt.

Giới hạn đã biết của P6:

- Chế độ Manual và Deferred (xem dưới); chưa có Managed (tiến trình nền, mục 13.4) — không giả định mọi harness cho phép tác vụ nền bền vững.
- Nhóm tác vụ theo khu vực mã, chưa dùng mô tả tác vụ hay biểu diễn ngữ nghĩa.
- `evaluate` cho đề xuất đang chờ; với đề xuất đã áp dụng (tri thức hoặc skill), đổi mô hình được báo ở
  `bint evolve history` / `bint skill regress` và kiểm tra lại bằng `evaluate --regression`.
- Ở chế độ economy, tri thức hiện có tạo sau commit gốc của tác vụ bị loại (A3), nên đánh giá loại bỏ
  chỉ đo được trên tác vụ cũ hơn mục bị loại bỏ; tác vụ không chịu ảnh hưởng được báo kèm lý do.
- Chưa có số liệu thật: tiêu chí nghiệm thu P6 (lợi ích trên tập kiểm tra độc lập) cần chạy trên
  repository thử nghiệm có đủ lượt có nhãn và tác vụ đánh giá.

## Tiến hoá skill — L2 (P7)

Cùng vòng đề xuất → đối chiếu → áp dụng → quay lui của L1, đối tượng là **skill chuyên biệt** trong
`.bint/skills/evolved/<tên>/`. Skill gốc (`b-*`) bất biến: không bao giờ là đối tượng của đề xuất,
tuỳ biến chúng chỉ qua lớp phủ do người viết (mục 6.6).

```sh
bint evolve scan                         # thêm cơ hội L2: skill_candidate, skill_merge, skill_stale
bint evolve propose op-… --budget 1      # tạo skill: agent evolver soạn; gộp/loại bỏ: xác định, không gọi mô hình
bint skill check ep_…                    # kiểm tra điều kiện kích hoạt TRƯỚC khi đối chiếu
bint evolve show ep_…                    # đọc SKILL.md / bản thay đổi: đây là chỉ dẫn sẽ nằm trong ngữ cảnh agent
bint evolve evaluate ep_… --split validation --budget 5 --yes   # base = tập skill hiện có, candidate = sau đề xuất
bint evolve evaluate ep_… --split test --budget 10 --yes
bint evolve apply ep_…                   # xuất bản cho harness (bint sync) ngay sau khi ghi
bint evolve rollback ep_…                # khôi phục tệp và gỡ bản đã xuất bản
bint skill list | show | versions | diff # skill gốc và tiến hoá, lịch sử phiên bản, bản thay đổi
bint skill regress                       # skill nào chưa xác thực với mô hình hiện dùng
bint evolve evaluate ep_… --regression --model <mô hình> --yes  # chạy lại tập nhỏ đại diện
```

**Kích hoạt thực tế**: hook Claude Code ghi `skill.activated` khi công cụ Skill chạy; bảng `run_skill_usages`
(migration 0007) liên kết skill với lượt chạy. `bint skill check` báo số lượt đủ điều kiện mà skill thực sự
được kích hoạt; `skill_ignored` (action update, agent viết lại mô tả, giữ tên) khi < 20% với ≥ `min_similar_runs`
lượt đủ điều kiện. Hook cũ cần `bint init --hooks` để thêm `Skill` vào matcher.

**Cơ hội** (từ lượt chạy có nhãn, mục 10.2): `skill_candidate` — khu vực mã có quy trình được xác nhận
hiệu quả ở ≥ `evolution.trigger.min_similar_runs` lượt thành công mà chưa có skill bao phủ (cần
`min_new_evidence` lượt mới so với đề xuất trước); `skill_merge` — hai skill mô tả tương đồng ≥ 50% và
chồng đường dẫn kích hoạt; `skill_stale` — mọi mẫu `triggers.paths` không còn khớp tệp nào. Không có
cơ hội "skill bị bỏ qua": Bint chưa nhận được sự kiện `skill.activated` từ harness nào và không tổng hợp
giả sự kiện (mục 13.2).

**Kiểm tra xác định được** (không gọi mô hình) trước khi đề xuất thành hình, và lặp lại khi áp dụng /
`bint sync` / `bint doctor`: tên hợp lệ (không `b-*`, không trùng skill gốc); mô tả một dòng 20–400 ký
tự (harness chọn skill theo mô tả); frontmatter chỉ `name` và `description` (không tự xin quyền hay hook);
không ghi đè quy tắc/tri thức/skill gốc, không nhắc `.bint/evals`, `.bint/config.yml` hay dữ liệu Bint
quản lý (đề xuất không tự sửa tiêu chí chấm điểm); bằng chứng, ký hiệu, ví dụ mã, đường dẫn khớp mã thật
(cùng bộ kiểm tra của tri thức); điều kiện kích hoạt khớp tệp thật; không trùng chức năng với skill khác;
chứa lệnh thực thi → rủi ro cao (`--confirm-risk`). Đề xuất tạo/sửa skill tối thiểu rủi ro trung bình.

**Điều kiện kích hoạt** (`bint skill check`): mẫu đường dẫn còn khớp tệp; kích hoạt được ở ≥ 50% lượt
thành công làm căn cứ (không quá hẹp); không kích hoạt ở > 35% lượt có nhãn không liên quan (không quá
rộng, cần ≥ 3 lượt để kết luận); mô tả không trùng skill khác. Đây là kiểm tra theo đường dẫn — harness
quyết định kích hoạt thật theo mô tả; kích hoạt thực tế chỉ được đo gián tiếp qua đối chiếu.

**Đối chiếu**: tác vụ chịu ảnh hưởng là tác vụ thoả điều kiện kích hoạt của skill bị thay đổi (tệp hoặc
từ khoá trong đề bài). Skill được đặt vào thư mục làm việc của cả hai nhánh (`.claude/skills` và
`.agents/skills`, đúng định dạng từng nơi), cùng một tập tri thức. Mọi skill đặt vào phải áp dụng được tại
commit gốc: bằng chứng khớp và nội dung không chứa mã lời giải (skill của đề xuất không thoả → tác vụ bị
loại; skill khác không thoả → bỏ khỏi cả hai nhánh). Tạo/sửa: tốt hơn trên thước đo chọn trước;
gộp/loại bỏ: không kém về tỷ lệ đạt. Nguồn ngữ cảnh ghi `skills[tên@băm]` để kiểm toán.

**Hợp nhất** giữ skill có phiên bản cao hơn, hợp điều kiện kích hoạt và bằng chứng, thêm vào cuối mọi
dòng của skill kia chưa có — không mất kiến thức; skill bị gộp thành `deprecated` và ghi `merged_from`.

**Phiên bản gắn mô hình** (mục 12.6): `skill.yaml` ghi từng phiên bản kèm băm, đề xuất, harness, mô
hình, kiểm chứng hay chưa, lần đối chiếu và người duyệt; `versions/N.md` giữ ảnh chụp để `bint skill
diff` xem bản thay đổi. Khi mô hình hiện dùng khác mô hình đã kiểm chứng, `bint skill regress` đánh dấu
"chưa xác thực với mô hình hiện tại" (kết quả cũ không là mốc cho mô hình mới) và
`evolve evaluate --regression` chạy lại tối đa 5 tác vụ đại diện (chính các tác vụ đã làm căn cứ, base =
tập skill trước đề xuất dựng lại, candidate = hiện tại). Lần chạy này ghi `kind = regression`, không bao
giờ dùng làm căn cứ áp dụng; kết quả: `revalid` (vẫn có lợi), `regressed` (không còn bằng chứng có lợi),
`inconclusive`. Bint chỉ báo cáo; không tự gỡ skill.

**Làm việc nhóm** (mục 8.7): skill và `skill.yaml` được commit; đề xuất ghi băm SKILL.md + skill.yaml làm
phiên bản gốc và mất hiệu lực nếu đổi. Hai thay đổi cùng sửa một skill rồi merge (nội dung không khớp
phiên bản ghi trong `skill.yaml`, dấu xung đột, thiếu ảnh chụp) → `bint doctor` báo lỗi để xử lý thủ
công; Bint không tự hợp nhất. `bint sync` xuất bản skill còn hiệu lực cho harness và gỡ bản của skill bị
loại bỏ, xoá hoặc lỗi thời (bản đã bị sửa tay không bao giờ bị xoá; ghi vào `core.lock`, mục `evolved`).

Giới hạn đã biết của P7:

- Skill mới/sửa có hiệu lực ngay khi người áp dụng (người áp dụng là người duyệt): hãy đọc SKILL.md ở
  `evolve show` — skill là chỉ dẫn hành vi do agent soạn từ dữ liệu không đáng tin.
- Sự kiện `skill.activated` chỉ có ở Claude Code (hook PostToolUse của công cụ Skill). Codex và
  Antigravity chọn skill ngầm, không có công cụ gọi skill đi qua hook: không đo được, và Bint không tổng
  hợp giả (khả năng `skill_events` trong bảng khả năng ghi rõ). Cơ hội `skill_ignored` chỉ dựa vào lượt
  của Claude Code.
- Chưa có số liệu thật: tiêu chí nghiệm thu P7 (skill đề xuất từ lịch sử, chứng minh hữu ích trên tập
  tác vụ đối chiếu trước khi áp dụng) mới được kiểm chứng bằng kiểm thử với agent giả
  (`TestEvaluateSkillProposal`), chưa chạy trên repository thật.

## Chế độ thực thi tiến hoá: Deferred (mục 13.3–13.4)

`evolution.execution: deferred` — khi đủ bằng chứng mới Bint **đánh dấu yêu cầu tiến hoá** (tệp trong
`<state>/runtime/evolve/requests`) và xử lý ở **lần gọi CLI phù hợp tiếp theo** (`bint status` khi có sự kiện
mới hoặc còn yêu cầu chờ; hoặc `bint evolve process`). Hook không bao giờ chạy vòng gọi mô hình.

```sh
bint evolve queue [--all]                 # yêu cầu đã đánh dấu: pending | done | skipped
bint evolve process [--budget 2] [--max N]   # đánh dấu + xử lý ngay (người dùng)
```

Mỗi lần xử lý: kiểm tra khoá tiến hoá (đang bị giữ → không làm gì, yêu cầu vẫn chờ) và ngân sách
(`evolution.deferred.budget_usd`; null = chỉ yêu cầu không cần mô hình: loại bỏ, gộp, skill lỗi thời; yêu cầu cần
agent evolver chờ ngân sách), tối đa `evolution.deferred.max_per_run` yêu cầu, rồi tạo **đề xuất** qua cùng đường
với `bint evolve propose`. Không áp dụng gì (chế độ propose). Cơ hội không còn sẵn sàng lúc xử lý (đã có đề xuất,
điều kiện đổi) → `skipped` kèm lý do. Không xử lý trong phiên agent (đề xuất tiến hoá chỉ dành cho người dùng).
Managed (tiến trình nền được cài đặt riêng) chưa có.

## Phát triển

```sh
go test ./...                 # gồm kiểm thử đầu-cuối build tệp thực thi thật
go test -race ./...           # cần CGO/gcc
```

CI (GitHub Actions, `.github/workflows/ci.yml`) chạy lint (gofmt, `go mod tidy -diff`, `go vet`,
staticcheck), `go test -race` và build chéo 6 nền tảng trên mỗi PR/push vào `main`. Workflow
release (`.github/workflows/release.yml`) đã viết sẵn nhưng đang bị khoá. Chi tiết, cách chạy
lại trên local và cách kích hoạt CD: [docs/ci-cd.md](docs/ci-cd.md).
