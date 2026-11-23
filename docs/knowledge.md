# Tri thức (P2) và Context Engine (P3)

Khởi tạo tri thức dự án có bằng chứng, quản lý vòng đời chống lỗi thời và đưa ngữ cảnh đúng
lúc đến coding agent (mục 4, 8, 9 của [đặc tả thiết kế](bint-plan.md); cấu hình chi tiết tại
[docs/configuration.md](configuration.md)).

## Tri thức (P2)

Bootstrap sinh **một số ít** tri thức có căn cứ, tập trung vào những điều bất thường mà agent
không tự đoán được (mục 4.2) — không sinh tổng quan kiến trúc hay mô tả cấu trúc thư mục.

```sh
bint bootstrap                  # static discovery + cài/cập nhật skill b-bootstrap
/b-bootstrap                    # trong harness: agent điều tra, ghi mục candidate
/b-knowledge docs/guidelines/   # agent đọc tài liệu (MD, PDF) và ghi mục candidate gắn nhãn nguon:<lô>
bint knowledge review           # người duyệt từng mục: a/r/e/f/s/q
bint bootstrap publish          # xem trước rồi đưa quy tắc đã duyệt vào AGENTS.md

bint knowledge add --type warning --paths 'internal/db/**' \
  --evidence internal/db/db.go:40-52 "Mở SQLite bằng store.Open, không gọi sql.Open trực tiếp"
bint knowledge list
bint knowledge show <id>
bint knowledge verify           # chỉ kiểm tra, không đổi trạng thái (--apply để cập nhật)
bint knowledge approve <id>     # người dùng duyệt
bint knowledge reject <id> --reason "Không còn áp dụng"
bint knowledge transitions      # bảng chuyển trạng thái
bint inspect [--json]           # chỉ static discovery
bint bootstrap skill            # in nội dung skill cho harness chưa có adapter
```

**Định dạng.** Mỗi mục là `.bint/knowledge/items/<ulid>.md` (commit được): phần đầu YAML gồm
`type` (`rule`, `warning`, `pattern`, `fact`, `decision`, `lesson`), `status`, `scope` (glob đường dẫn, ký
hiệu, thẻ), `evidence` (`path`, `lines`, `symbol`, `commit`, `fingerprint`), `origin` (`human` |
`agent` | `imported`, phiên/lượt chạy, đầu vào đã đọc, cờ `third_party`, `eval_case`), `enforced_by` (tuỳ chọn:
danh sách `{kind: lint|test|hook|type|ci|script, path, note}` — cơ chế máy cưỡng chế quy tắc; `bint knowledge add --enforced-by
test:internal/x/y_test.go#TestZ`), tác giả, người duyệt, thời điểm; sau đó là nội dung Markdown. Tệp là nguồn chuẩn duy nhất.

**Trạng thái** `candidate → verified → stale → deprecated`:

| Chuyển | Ai / điều kiện |
|---|---|
| `candidate → verified` (`rule`, loại trong `require_human_approval_for`, mục gắn cờ bên thứ ba) | người: `approve` / `review` |
| `candidate → verified` (loại khác) | `verify --apply` khi qua mọi kiểm tra, hoặc người |
| `origin: human` | tạo ở `verified`, tác giả là người duyệt |
| `verified → stale` | `verify --apply` khi bằng chứng đổi/mất |
| `stale → verified` | người xem lại: `approve --refresh` |
| `* → deprecated` | người: `reject` / `deprecate` (cần lý do, giữ tệp để kiểm toán) |

Agent không bao giờ chuyển được trạng thái hiệu lực: `approve`, `review`, `reject`,
`deprecate`, `verify --apply` và `add --origin human` bị từ chối khi phát hiện biến môi trường
của phiên agent (`CLAUDECODE`, `AI_AGENT`, `CODEX_SANDBOX`, `CODEX_SANDBOX_NETWORK_DISABLED`,
`GEMINI_CLI`, `BINT_AGENT`). Đây là lớp phòng vệ bổ sung, không phải ranh giới bảo mật — agent
có shell vẫn xoá được biến môi trường; kiểm soát chính là review thay đổi `.bint/knowledge/` qua Git.

**Kiểm tra xác định được** (`bint knowledge verify`, chạy cả khi `add`; mục không qua thì không
được ghi): tệp và vùng dòng bằng chứng tồn tại; fingerprint vùng dòng khớp (bỏ qua CRLF và khoảng
trắng cuối dòng); ký hiệu có trong bằng chứng hoặc tệp thuộc phạm vi; mẫu phạm vi khớp ít nhất một
tệp; ví dụ mã khớp mã thật (` ```go path=… ` đối chiếu đúng tệp; khối shell/`text` bỏ qua);
đường dẫn nhắc tới trong nội dung tồn tại (cảnh báo); trùng lặp; đầu vào bên thứ ba
(`vendor`/`node_modules`/URL/issue) phải được gắn cờ; cảnh báo tổng quan kiến trúc và nội dung quá dài.

**AGENTS.md.** `bootstrap publish` (và `init --agents-md`) dựng khối `bint:begin … bint:end` từ
mục `rule` và `warning` ở trạng thái `verified` — quy tắc toàn cục trước, rồi theo phạm vi đường
dẫn; bỏ mục `stale` và mục bên thứ ba chưa duyệt; tối đa `knowledge.agents_md_max_items` (15). Luôn
in bản thay đổi; không có terminal thì chỉ xem trước (`--yes` để ghi). `CLAUDE.md` nhập lại
`@AGENTS.md`. Nội dung ngoài khối không bị chạm; khối bị sửa tay → xung đột.

**Nhập bộ nhớ của harness** (`bint knowledge import`, mục B3 của `next-directions.md`). Claude Code, Codex và
Cursor đã ghi sẵn ghi chú về dự án nhưng không kiểm chứng; lệnh này nhập chúng thành mục `candidate` (`origin:
imported`) để người duyệt như mọi mục khác. Xác định, không gọi mô hình, không dùng mạng.

```sh
bint knowledge import --from claude-memory --dry-run   # liệt kê từng ý và kết cục dự kiến, không ghi gì
bint knowledge import --from claude-memory              # ghi các ý đủ điều kiện thành mục candidate
bint knowledge import --from notes/lessons.md --type fact
```

| `--from` | Vị trí đọc |
|---|---|
| `claude-memory` | `$CLAUDE_CONFIG_DIR` hoặc `~/.claude`, rồi `projects/<tên>/memory/*.md`. `<tên>` là đường dẫn tuyệt đối của repository với mọi ký tự không phải chữ/số ASCII thành `-` (ví dụ `D--Projects-bint`); thử cả cây làm việc hiện tại lẫn cây chính khi đang ở worktree |
| `codex-memory` | `$CODEX_HOME` hoặc `~/.codex`, rồi `memories/**/*.md` (định dạng chưa đối chiếu tài liệu chính thức: đọc như Markdown thường) |
| `cursor-rules` | `.cursor/rules/**/*.mdc` và `.cursorrules` trong repository; `globs` của rule (kể cả mẫu không trích dẫn như `**/*.go`) thành `scope.paths` |
| `<tệp.md>` hoặc `file` + `--path` | một tệp Markdown cục bộ; không nhận URL |

`--path` chỉ thẳng thư mục hoặc tệp nguồn (khi tên dự án của Claude không khớp quy tắc đặt tên).

**Tách ý và bằng chứng.** Mỗi tệp bộ nhớ Claude là một ý (tiêu đề lấy `description`, rồi `name`); nguồn khác
tách theo gạch đầu dòng cấp đầu và đoạn văn dưới tiêu đề, khối mã rào không bị tách. Bằng chứng tìm bằng cách đối chiếu
mã nội tuyến (`` `Tên` ``) và đường dẫn trong ý với tệp của repository: định nghĩa ký hiệu (func/type/class/def...) được ưu
tiên, tên chung có nhiều định nghĩa bị bỏ, tối đa 3 bằng chứng mỗi ý, có fingerprint như `knowledge add`. Phạm vi mục lấy
từ `globs` (Cursor), nếu không thì từ các tệp bằng chứng, không để toàn repository. Mục mang `origin.source`,
`origin.inputs` (tham chiếu nguồn dạng `claude-memory:feedback_x.md`, không chứa đường dẫn thư mục home), độ tin cậy
`low`, nhãn `import` và nhãn lô `nguon:import-<nguồn>` để duyệt theo lô (`bint knowledge review`, Tổng quan).

Khử trùng theo tiêu đề/nội dung, không theo vị trí `#n` của nguồn: chèn hoặc đổi thứ tự ý không làm mất ý mới.

**Quy tắc bỏ qua** (mỗi ý bỏ qua có lý do trong báo cáo):

| Lý do | Ý nghĩa |
|---|---|
| `no_evidence` | không tìm được tệp hay ký hiệu nào của repository |
| `duplicate` | trùng tiêu đề hoặc nội dung của mục còn hiệu lực |
| `already_imported` | đã nhập trước đó, kể cả mục đã bị từ chối (`deprecated`): chạy lại không tạo mục mới và không đưa lại mục người đã loại |
| `work_state` | ghi chú tiến độ/TODO/WIP rõ ràng; ngày, mã ticket hoặc commit chỉ bị loại khi kèm ngữ cảnh công việc, không loại ví dụ trong quy tắc |
| `private` | bộ nhớ Claude `type: user` (thông tin riêng về người dùng; nội dung không được giữ lại trong kết quả) |
| `index_file` | `MEMORY.md` (chỉ mục các ghi chú khác) |
| `bint_derived` | nội dung do chính Bint sinh ra: khối `bint:begin … bint:end`, ý có tham chiếu `[k:…]` |
| `check_failed` | không qua `Verifier` (hoặc không đọc được tệp nguồn) |

**Riêng tư và quyền.** Bộ nhớ của harness nằm ngoài repository và có thể chứa thông tin riêng tư, nên nhập là thao tác
chỉ dành cho người (`RequireHuman`): cả chạy thật lẫn `--dry-run` bị từ chối trong phiên agent. Lệnh không bao giờ tự chạy
(hook, `bint init` và wizard chỉ gợi ý). Lỗi nguồn chỉ nêu tên tệp, không đưa đường dẫn home vào báo cáo. Onboarding chỉ gợi ý Claude/Cursor theo repository; bộ nhớ Codex global cần người chọn nguồn rõ ràng. TUI yêu cầu xem trước lại sau khi mở form sửa hoặc khi preview lỗi. Mỗi tệp nguồn bị chặn ở 256 KB, tối đa 500 tệp. Mục nhập là tệp `candidate` trong
Git nên reviewer PR thấy trước khi nó có hiệu lực; gỡ bằng `knowledge rm` hoặc từ chối theo lô.

**TUI và onboarding.** Màn Tri thức có thẻ phụ **Nhập**: form (nguồn, đường dẫn, loại) → xem trước từng ý → `w` mở hộp
xác nhận. Bước ghi chỉ chạy khi kế hoạch dựng lại lúc ghi có băm trùng bản đã xem (nguồn hay tri thức vừa đổi thì không ghi
và hiện kế hoạch mới, như Xuất bản); ghi xong danh sách tự lọc theo lô vừa nhập. Chế độ chỉ đọc không có hành động ghi.
`bint init` thêm bước tiếp theo nhắc `bint knowledge import` khi phát hiện nguồn (chỉ đếm tệp, không đọc nội dung); trong
trình hướng dẫn init của TUI dòng đó dẫn tới thẻ Nhập.

Giới hạn: ý văn xuôi không nhắc tên mã hay đường dẫn nào bị bỏ (`no_evidence`); quy tắc đặt tên thư mục của Claude Code và
định dạng Codex memories chưa có tài liệu chính thức để đối chiếu; chưa nhập CSDL của claude-mem; tách ý bằng mô hình
(`/b-import`) để sau.

Giới hạn đã biết của P2:

- Chưa phát hiện tự động mâu thuẫn giữa các mục, hay với tài liệu sẵn có; người duyệt chịu trách nhiệm.
- `knowledge review` đọc lựa chọn theo dòng (phím + Enter), chưa đọc một phím.
- Skill được xuất bản tự động cho Claude Code, Codex và Antigravity theo danh sách harness đã bật;
  với công cụ khác dùng `bint bootstrap skill`.

## Context Engine và vòng đời tri thức (P3)

```sh
bint context "thêm cột email vào bảng users"     # gói ngữ cảnh theo tác vụ (FTS5 + phạm vi + bằng chứng)
bint context --paths internal/db/db.go           # quy tắc/cảnh báo có phạm vi khớp tệp
bint init --hooks --mcp                          # cài hook và MCP cho harness đã bật
bint mcp serve                                   # MCP stdio: context_for_task, context_for_paths,
                                                 #   knowledge_search, knowledge_get, cite_knowledge, report_observation
                                                 #   (+ history_for_paths, run_get khi context.history.enabled)
bint knowledge search "migration"
bint knowledge stats
bint knowledge diff ORIG_HEAD..HEAD
bint knowledge revalidate --budget 1             # agent nhận định lại mục stale rủi ro cao
bint index rebuild                               # dựng lại chỉ mục (bình thường tự đồng bộ)
bint runs rate <id> good                         # hoặc bad; người dùng chấm lượt chạy
bint runs scan                                   # gắn commit với lượt, phát hiện hoàn tác
```

**Chỉ mục.** Bảng `knowledge_index` + FTS5 (`unicode61 remove_diacritics 2`: "tien te" khớp
"tiền tệ"; ký hiệu CamelCase được tách từ) trong SQLite là dữ liệu **dẫn xuất**: mọi lệnh truy xuất
so chữ ký thư mục `.bint/knowledge/items` và chỉ ghi lại mục đổi. Repository chưa có thư mục trạng
thái thì chỉ mục nằm trong bộ nhớ.

**Truy xuất** (mục 8.2). Theo tác vụ: từ khoá BM25 (giữ kết quả ≥ 30 % điểm tốt nhất), đường dẫn
nêu trong mô tả, miền/thẻ/ký hiệu → loại mục `deprecated`, mục bên thứ ba chưa duyệt, mục rút ra từ
chính tác vụ đánh giá → kiểm tra bằng chứng ngay lúc truy xuất → loại trùng → quy tắc trước → cắt
theo `context.max_tokens` (~3 ký tự/token). Theo đường dẫn: chỉ mục có phạm vi cụ thể khớp (mục toàn
cục đã ở `AGENTS.md`), ưu tiên phạm vi hẹp, tối đa `context.path_max_items`, không lặp mục đã chèn
trong phiên. Nhãn: **quy tắc bắt buộc** chỉ khi `rule` đã duyệt, không gắn cờ bên thứ ba và bằng
chứng còn khớp; mục chưa duyệt hoặc bằng chứng đã đổi thành **tham khảo** kèm cảnh báo. Mọi đầu ra
nằm trong khung `<bint-context>` "dữ liệu tham khảo", kèm `[k:<id>]` và nguồn gốc (mục 15.5).

**Bài học từ thất bại** (kế hoạch E, `bint experience`) nằm trong phần riêng của gói ngữ cảnh, với ngân
sách token riêng `context.lessons.max_tokens` (mặc định 600; 0 = tắt), không chiếm chỗ của tri thức. Theo
đề bài: luôn chèn tối đa `context.lessons.global_k` (mặc định 3) bài học **chung**, xếp theo số từ khoá
trùng với đề bài; bài học **riêng** chỉ khi phạm vi khớp đường dẫn hoặc miền/thẻ trong đề bài. Theo đường
dẫn (hook khi sửa tệp): chỉ bài học riêng khớp, không lặp mục đã chèn trong phiên. Hook không mở SQLite:
xếp hạng chỉ dựa trên từ khoá, nhãn và ID.

**Hook.** Trên Claude Code, `UserPromptSubmit` chèn gói ngữ cảnh theo đề bài ngay khi nhận yêu cầu, và
`PreToolUse` cho `Read|Edit|Write|MultiEdit|NotebookEdit` trả `hookSpecificOutput.additionalContext` theo
đường dẫn; quá `context.path_hook_timeout_ms` (1500 ms) thì bỏ qua, không chặn agent; tắt cả hai bằng
`context.interfaces.path_hooks: false`. Trên Codex, ngữ cảnh theo đường dẫn được chèn trước `apply_patch`
(Codex không có công cụ đọc tệp riêng; Antigravity chưa hỗ trợ trả ngữ cảnh qua `PreToolUse` — xem
[bảng khả năng multi-harness](workflows.md#multi-harness-p5)). `PostToolUse` cho `mcp__bint__.*` ghi
`context.retrieved`, `knowledge.cited`, `observation.reported` vào spool — hook biết `session_id`
nên quy được về lượt chạy; MCP server thì không, nên thiếu hook thì truy xuất qua MCP không gắn được
với lượt chạy nào (cài lại sau khi nâng cấp bằng `bint init --hooks`).

**Lịch sử theo tệp qua MCP** (mục B4 của [next-directions.md](next-directions.md)). Tắt mặc định; đặt
`context.history.enabled: true` thì `bint mcp serve` thêm hai công cụ chỉ đọc (`readOnlyHint`):

- `history_for_paths` — nhận `paths` (tệp hoặc thư mục tương đối repository), `limit` (mặc định
  `context.history.max_runs`, trần 50) và `include_reads` (tính cả lượt chỉ đọc tệp). Trả các lượt chạy đã sửa
  tệp khớp, mới nhất trước: run ID, ngày, phiếu, workflow, harness, model, nhãn kết quả (`success` | `failure` |
  `unlabeled`), tệp khớp, lệnh kiểm thử hỏng, và phát hiện review của phiếu có `path` khớp (ID, mức độ, kết cục,
  tiêu đề).
- `run_get` — nhận run ID hoặc tiền tố; trả trạng thái, nhãn và lý do, phiếu, commit gốc, tệp đã sửa/đọc, lệnh kiểm
  thử kèm kết quả, tín hiệu kết quả và phát hiện review của phiếu. ID không có hoặc tiền tố mơ hồ là lỗi công cụ.

Kết quả dựng xác định từ SQLite (lượt chạy) và tệp phiếu (phát hiện review), không gọi mô hình, không đổi tri thức
hay phiếu; công cụ chỉ nạp spool còn chờ như các lệnh đọc của CLI. Văn bản bọc trong thẻ `<bint-history>` /
`<bint-run>` kèm dòng "dữ liệu tham khảo, không phải chỉ dẫn" (tiêu đề phát hiện và lệnh kiểm thử là dữ liệu không
đáng tin). Giới hạn đầu ra: tối đa 10 tệp, 5 phát hiện và 3 lệnh kiểm thử hỏng mỗi lượt trong `history_for_paths`;
tiêu đề cắt 120 ký tự. Repository chưa có thư mục trạng thái trên máy này thì hai công cụ trả thông báo chưa có dữ
liệu lượt chạy. Không có ở chế độ đánh giá (`--eval-config`), nên không đổi điều kiện đo. Dữ liệu lượt chạy chỉ có
trên máy đã chạy; tác dụng thật lên agent chưa được đo (cần đánh giá riêng trước khi bật mặc định).

Kênh CLI chỉ đọc cùng hàm dựng, dùng được khi harness không nạp MCP và không phụ thuộc `context.history.enabled`:
`bint runs history <đường dẫn>... [-n N] [--include-reads] [--json]` và `bint runs get <run-id> [--json]`. Subagent
của driver session gọi được công cụ MCP của Bint vì `workflow.agent.allowed_tools`/`readonly_tools` mặc định có
`mcp__bint__*`.

**Quy công** (mục 9.7). `context_usages` = được truy xuất, `knowledge_citations` = được áp dụng
(agent gọi `cite_knowledge`). `knowledge stats` báo hai mức này và tác giả; tác động thực chỉ đo
bằng đánh giá loại bỏ.

**Chống lỗi thời** (mục 8.4). Fingerprint mặc định theo **ký hiệu** (`context.freshness.fingerprint:
symbol`): Go phân tích bằng `go/parser`; JS/TS, Java, Kotlin, Rust, C/C++, C#, PHP, Swift, Scala, Dart
theo dòng khai báo + đếm ngoặc; Python/Ruby/YAML theo thụt lề; TOML theo bảng — các ngôn ngữ ngoài Go
là heuristic. Kế hoạch đề xuất tree-sitter, nhưng binding Go đều cần CGO, trái quyết định biên dịch
Go thuần; `symbols.Locator` là điểm cắm khi có bản thuần Go (chi tiết tại
[quyết định fingerprint](fingerprint-outcome-decisions.md)). Không định vị được ký hiệu → vùng dòng →
cả tệp. Mục cũ được kiểm tra đúng theo mức fingerprint đã lưu. Với vùng dòng, nếu nội dung cũ còn
nguyên ở vị trí khác (khối thay đổi không chồng lên vùng) thì mục **không** lỗi thời; `verify --apply`
cập nhật `lines`. Xác thực lại: loại rủi ro thấp (`fact`, `pattern`, `decision`, `lesson`) qua mọi kiểm tra
trừ fingerprint → `verify --apply` làm mới fingerprint và đưa về `verified`; `rule`/`warning` cần người
(`approve --refresh`), `knowledge revalidate` cho agent (chỉ Read/Grep/Glob, theo lô, trong ngân sách)
đưa nhận định `valid`/`outdated`/`fix-evidence`/`unsure` vào `knowledge_revalidations` — không tự đổi trạng thái. Mục `stale`
quá `knowledge.stale_deprecate_days` (30) được đề xuất `deprecated`.

**Rà soát bằng agent qua skill** (không cần `--budget`): skill `b-knowledge-audit` (agent tự dùng khi được nhờ rà soát, hoặc gọi `/b-knowledge-audit`) hướng dẫn agent chạy
`bint knowledge audit` (liệt kê mục `stale` và mục `verified` có fingerprint lệch kèm lý do và mã hiện tại của
bằng chứng; mục chỉ dời dòng/chuyển tệp được tự áp như `verify --apply` nên không còn trong danh sách), đọc mã,
rồi lưu từng nhận định bằng `bint knowledge verdict <id> --verdict valid|outdated|fix-evidence|unsure --reason "…"`
(`fix-evidence` kèm `--path/--lines/--symbol` đề xuất). `verdict` chỉ ghi vào `knowledge_revalidations` cùng
agent/mô hình tạo ra nó, không đổi trạng thái nên agent gọi được. `knowledge revalidate --budget` dùng chung định
dạng nhận định (và prompt) và nhận cả mục `verified` có fingerprint lệch. Người xem nhận định bằng
`bint knowledge verdicts` hoặc thẻ **Nhận định** của màn Tri thức và áp theo lô (`verdicts --apply`, phím `w`/`V` ở
thẻ): `valid` → làm mới fingerprint, `outdated` → `deprecated`, `fix-evidence` → sửa bằng chứng rồi làm mới;
`unsure` và nhận định cũ (mã đổi tiếp sau nhận định) bị bỏ qua.

**Quét tổng quát** (`bint knowledge audit --all`, mặc định của skill): liệt kê mọi mục còn hiệu lực, kể cả `candidate`
chờ duyệt và mục bằng chứng vẫn khớp, kèm tín hiệu `problem` (stale/bằng chứng lệch), `never_used`/`unused` (không được
truy xuất, trích dẫn trong `--unused-days`, mặc định 90), `review_old`, `warning` (trùng lặp…), `unenforced` (rule/warning chưa có `enforced_by` còn tệp: ứng viên để `/retro` chuyển
thành check) và số lần dùng; mục có vấn
đề đứng đầu, rồi mục ít dùng. Agent đề xuất giữ (`valid`), loại (`outdated`: sai, thừa, trùng; ít dùng thôi không đủ)
hay sửa bằng chứng; người áp theo lô như trên. Với `candidate`, `valid` = duyệt, `outdated` = loại.

**Tín hiệu kết quả** (mục 9.6, bảng `outcome_signals`, không gộp thành một điểm): `tests` (mạnh nếu
lệnh khớp `eval.test_command` đã xác nhận, ngược lại trung bình), `rating` (người chấm, mạnh), `committed`
(commit chạm tệp lượt đã sửa trong 24 giờ sau lượt, yếu — gắn theo thời gian/tệp là gần đúng),
`reverted` (commit đó bị `git revert` trong 14 ngày, mạnh tiêu cực). Nhãn: hoàn tác hoặc chấm `bad` →
`failure`; chấm `good` → `success`; lần kiểm thử mạnh cuối cùng quyết định; còn lại `unlabeled`. Hiện ở
`runs list` và `runs show`.

**Chẩn đoán cổng 2** (mục 12.9). Đợt có `bint`/`ideal`: báo cáo thêm so sánh theo cặp với `minimal`
và xét theo thứ tự: đo lường → nội dung (`ideal` không hơn `minimal`) → truy xuất bỏ sót (độ phủ tập
đúng < 50 %) → không được áp dụng (mục đúng ít được trích dẫn ở lượt thất bại) → nhiễu. Chỉ là dữ liệu
cho người quyết định.

Giới hạn đã biết của P3:

- Đã chạy [pilot Codex](evals/pilot-codex-2026-11-10/README.md) và các đợt đo Claude, nhưng chưa nghiệm
  thu chính thức theo giao thức [cổng 2](gate-2.md).
- Phân loại loại công việc theo từ khoá thô; chưa có truy xuất ngữ nghĩa (mục 8.3: chỉ thêm khi FTS5
  không đạt).
- Gói ngữ cảnh chưa gồm skill có thể áp dụng (skill thuộc P4).
- Tín hiệu "người dùng sửa lại diff của agent trong phiên" chưa thu.
- Hook `post-merge` tuỳ chọn (mục 8.8) chưa có; dùng `bint knowledge diff` sau `git pull`.
- MCP server tự hiện thực phần tối thiểu của giao thức (`initialize`, `ping`, `tools/list`, `tools/call`).
