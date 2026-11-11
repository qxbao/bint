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
`agent` | `imported`, phiên/lượt chạy, đầu vào đã đọc, cờ `third_party`, `eval_case`), tác giả,
người duyệt, thời điểm; sau đó là nội dung Markdown. Tệp là nguồn chuẩn duy nhất.

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
đưa nhận định `valid`/`outdated`/`unsure` vào `knowledge_revalidations` — không tự đổi trạng thái. Mục `stale`
quá `knowledge.stale_deprecate_days` (30) được đề xuất `deprecated`.

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
