# Workflow (P4) và Multi-Harness (P5)

Điều phối quy trình lập kế hoạch → thực thi → đánh giá thay đổi qua phiếu tác vụ và các adapter
đa harness (mục 5, 6, 7 của [đặc tả thiết kế](bint-plan.md); cấu hình chi tiết tại
[docs/configuration.md](configuration.md)).

## Workflow tối thiểu (P4)

Bốn skill gốc — `b-req` (phân tích, lập kế hoạch), `b-feat` (thực thi), `b-review` (đánh giá
thay đổi), `b-do` (điều phối) — gắn mọi đầu ra, kiểm chứng và vòng sửa bằng **task ID**.

`/b-do` là **orchestrator**: phiên đang chạy chỉ lập phiếu, hỏi người dùng và gọi `bint run`
(không gọi `bint run` lồng bên trong bước đang có `BINT_RUN_ID`). Mỗi bước do một tiến trình
agent riêng thực hiện, harness theo `roles.<role>`, mô hình theo `harnesses.<harness>.models.<role>`.
Mặc định của Claude: `planner` (`b-req`), `reviewer` (`b-review`), `evolver` và `reflector`
dùng `opus`; `implementer` (`b-feat`) dùng `sonnet`. Codex: `gpt-6-astra` / `gpt-6-luna`.
Antigravity: `gemini-4-argon-high` / `gemini-3.8-flash-medium`. Thứ tự ưu tiên chọn mô hình:
`--model` → `harnesses.<h>.models.<role>` → mô hình chung (`workflow.agent.model`,
`harnesses.<h>.model`) → mặc định của harness. Kết quả từng bước ghi `harness/model` đã dùng.

```sh
# Trong harness có hỗ trợ skill (xuất bản khi init/sync):
/b-do Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db

# Hoặc Bint Core điều phối theo chương trình (adapter cấp 3, giới hạn áp ở tầng chương trình):
bint run b-do "Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db"
bint run b-do --depth deep "Thiết kế lại luồng thanh toán trong internal/billing"
bint run b-do --no-plan "Sửa chính tả trong README.md"
bint run b-req --task <id>                      # chạy lẻ một bước (b-req | b-feat | b-review)
bint run status <task-id|run-id>                # tác vụ + các lượt chạy (cha b-do, con từng bước)
bint run resume <task-id>                       # tiếp tục tác vụ blocked / interrupted / failed

bint task new "<mô tả tự do>"                   # Classify: lập phiếu tác vụ (mục 6.7)
bint task list [--all]
bint task show <id>
bint task set <id> --verify-command "go test ./internal/db/..." \
  --criteria "Kiểm thử cạnh tranh SQLite không còn lỗi database is locked" \
  --allow "internal/db/**"
bint task transitions                           # bảng chuyển trạng thái
bint task write <id> requirement|plan|tasks -   # ghi tài liệu qua stdin
bint task diff <id> [--stat|--name-only]        # thay đổi của RIÊNG tác vụ (không lẫn tác vụ khác / thay đổi có sẵn)
bint task new --worktree|--shared "<mô tả>"     # chọn nơi chạy lúc lập phiếu (mặc định theo workflow.isolation)
bint task land <id> [--3way] [--keep]           # tác vụ worktree: đưa thay đổi về cây gốc (chưa commit); --3way chỉ NGƯỜI
bint task cancel <id> [--reason …] [--discard]  # huỷ; tác vụ worktree: gỡ worktree (--discard chỉ NGƯỜI nếu còn thay đổi chưa đưa về)
bint task lint <id> [--strict] [--json]         # kiểm ba tài liệu b-req theo hợp đồng v1 (AC↔TSK, câu hỏi mở)
bint task verify <id>                           # chạy lệnh kiểm chứng (chỉ khi người đã xác nhận)
bint task criteria <id> <n> met|unmet -e "…"    # đối chiếu tiêu chí chấp nhận
bint task error <id> --step b-feat "…"          # ghi lỗi / lần thử thất bại
bint task review <id> -                         # vòng review (JSON hoặc YAML) → review.json + review.md
bint task approve <id> [--path <tệp>]           # NGƯỜI: xác nhận lệnh kiểm chứng, duyệt đường dẫn bảo vệ
bint task status <id> done [--force]            # done cần bằng chứng; chỉ người --force được

bint skill list
bint skill show <tên> [--core]
bint skill overlay <tên>
bint sync                                       # ghép skill gốc + lớp phủ, xuất bản, cập nhật core.lock
bint knowledge add --from-review <task-id>.<n>  # phát hiện review → mục warning/lesson
```

**Tác vụ** nằm ở `<state>/runtime/tasks/<task-id>/` (ngoài repository): `task.yml` (phiếu: `goal`,
`verify.command`/`criteria`, `scope.allow`/`deny`, `constraints`, `autonomy`; sửa được bằng tay),
`state.json` (do Bint ghi qua khoá thư mục: trạng thái, lịch sử chuyển, kiểm chứng, tiêu chí, duyệt,
lỗi, lượt chạy), `requirement.md`, `plan.md`, `tasks.md`, `review.json`/`review.md`, `logs/`.
`--criteria`, `--allow`, `--deny`, `--constraint` trong `task set` nối thêm vào danh sách sẵn có;
dùng `--clear-criteria` và `--clear-scope` để xoá trước khi đặt lại. Đổi `verify.command` làm lần xác
nhận lệnh cũ hết hiệu lực.

**Classify** xác định được (heuristic, chưa gọi mô hình): mục tiêu = dòng đầu mô tả; `scope.allow` =
đường dẫn và mẫu glob nêu trong mô tả mà tồn tại; `verify.command` = `eval.test_command` đã xác nhận;
đề xuất bỏ `b-req` cho việc nhỏ/ngắn gọn. Tệp tài liệu (`.md`, `.txt`…) mà mô tả coi là kế hoạch/đặc tả
cần làm theo ("thực hiện plan docs/x.md") vào `references` thay vì `scope.allow` và kéo theo `b-req`.
`task new` có `--clear-scope` để bỏ phạm vi tự đoán, chỉ giữ `--allow`/`--deny`.

Heuristic chỉ là gợi ý: trong `/b-do`, orchestrator tự đánh giá yêu cầu (đọc cả tài liệu được nêu) và
truyền `--plan`/`--no-plan`/`--depth` tường minh. Khi phạm vi chạm đường dẫn bảo vệ
(`Assessment.Risk=high`), Bint tự bật `b-req`; chỉ **người** bỏ được bằng `--no-plan`, còn `--no-plan`
từ phiên agent bị bỏ qua và lý do ghi vào `classification.reason`.

**Hợp đồng artifact của `b-req` (v1).** Đầu ra của `b-req` có khuôn kiểm được bằng máy:
- `requirement.md`: dưới `## Tiêu chí chấp nhận` có các dòng `- [ ] AC-N: …` đánh số liên tục từ 1,
  khớp từng tiêu chí tương ứng trong `task.yml` (sau khi chuẩn hoá khoảng trắng).
- `tasks.md`: mỗi đầu việc dạng `- [ ] TSK-N [AC-N]: <việc> — xong khi: <điều kiện>`, ID duy nhất; mọi
  AC phải được ít nhất một TSK bao phủ.
- `plan.md`: không rỗng. Dòng AC/TSK không được chứa placeholder (`TODO`, `TBD`, `...`, `<...>`), và nội
  dung trong khối code fence không được tính là AC/TSK.
- **Câu hỏi mở:** dưới `## Câu hỏi mở` trong `requirement.md`, câu hỏi thực sự cản trở triển khai có
  dạng `- [ ] Q-1: <câu hỏi>` sẽ chặn `b-feat`. Sau khi có câu trả lời, cập nhật thành
  `- [x] Q-1: <câu hỏi> — Trả lời: <nội dung>` bằng `bint task write`.

Ví dụ ghi bộ tài liệu hợp lệ và kiểm tra bằng `task lint`:

```sh
bint task write "$task_id" requirement - <<'EOF_REQ'
# Yêu cầu
## Mục tiêu và phạm vi
Sửa cạnh tranh SQLite trong internal/db.
## Tiêu chí chấp nhận
- [ ] AC-1: Kiểm thử cạnh tranh SQLite không còn lỗi database is locked
## Câu hỏi mở
EOF_REQ
bint task write "$task_id" plan - <<'EOF_PLAN'
# Kế hoạch
Đọc đường mở kết nối và giao dịch trong internal/db; tái hiện qua ca kiểm thử cạnh tranh.
Sửa tại nguồn tạo khoá và chạy go test ./internal/db/...
Rủi ro: tăng thời gian chờ có thể che lỗi; rollback bằng cách hoàn tác phần thay đổi của tác vụ.
EOF_PLAN
bint task write "$task_id" tasks - <<'EOF_TASKS'
- [ ] TSK-1 [AC-1]: Sửa nguyên nhân cạnh tranh và bổ sung ca hồi quy — xong khi: go test ./internal/db/... qua, gồm ca cạnh tranh mới
EOF_TASKS
bint task lint "$task_id"
```

Tác vụ mới qua `b-req` mang `planning_contract_version: 1`. Khi sai khuôn hoặc còn câu hỏi mở:
- Trong `b-do` (hoặc tiền kiểm trước `b-feat`): tác vụ chuyển sang `blocked` (trạng thái trước là
  `planned`); sửa tài liệu bằng `bint task write`, kiểm tra bằng `bint task lint <id>`, rồi gọi
  `bint run resume <id>` để đi tiếp sang `b-feat` mà không chạy lại planner.
- Khi chạy lẻ `bint run b-req --task <id>`: tác vụ giữ ở `planned` và lệnh thoát mã 1; sau khi sửa bằng
  `task write` và `task lint`, chạy tiếp bằng `bint run b-feat --task <id>` hoặc `bint run b-do --task <id>`
  (không gọi `resume` trên tác vụ do `b-req` chạy lẻ để tránh gọi lại planner).

Tác vụ cũ và `--no-plan` giữ đường chạy tương thích; `bint task lint --strict` kiểm hợp đồng v1 trên
tác vụ cũ. Core chỉ kiểm cấu trúc, không đánh giá nội dung (việc của `b-review` và người dùng).

**Độ sâu của `b-req`.** `Classification.Plan` quyết định *có* chạy `b-req` không; `Classification.Depth`
(`standard` | `deep`, kèm `depth_reason`) quyết định *phân tích kỹ đến đâu*, vẫn cùng một skill và cùng
ba artifact. Với `--depth auto` (mặc định), Bint chọn **một lần** lúc tạo tác vụ: `deep` khi phạm vi chạm
`limits.protected_paths` (`protected_paths`) hoặc yêu cầu từ 5 dòng không trống / hơn 100 từ
(`complex_request`); còn lại `standard`. Mô tả dài **không** nâng `Assessment.Risk`. `deep` yêu cầu thêm
trong `plan.md` các mục giả định/mâu thuẫn, ít nhất hai phương án và lý do chọn, vùng ảnh hưởng,
rủi ro/rollback, chiến lược kiểm thử, checklist AC↔TSK. `--depth standard|deep` là ghi đè của người dùng:
hàm ý có `b-req`, lỗi nếu đi cùng `--no-plan` hoặc workflow `b-feat`/`b-review`, và chỉ nhận khi tác vụ
còn `created` (hoặc `bint run b-req --task <id> --depth …` để lập kế hoạch lại trên tác vụ `planned`).
`resume` luôn dùng độ sâu đã lưu; tác vụ cũ không có `depth` được hiểu là `standard`.

**Trạng thái:** `created → planned → in_progress → in_review → done`; `blocked`, `interrupted`,
`failed` nhớ trạng thái trước để tiếp tục; `cancelled` kết thúc. **done cần bằng chứng**: lệnh kiểm
chứng đã được người xác nhận và lần chạy cuối qua trên **đúng trạng thái mã hiện tại** (fingerprint
HEAD + diff + tệp chưa theo dõi), hoặc — khi không có lệnh kiểm chứng — mọi tiêu chí được đối chiếu là
đạt; không tiêu chí nào bị đánh dấu `unmet`; không có thay đổi chạm đường dẫn bảo vệ chưa duyệt.

**Chỉ người** (chặn khi phát hiện biến môi trường của phiên agent, như ở P2): xác nhận lệnh kiểm
chứng và duyệt thay đổi (`task approve`), đặt `autonomy: auto`, `done --force`. Lệnh kiểm chứng do
agent đề xuất không được chạy trước khi người xác nhận (`security.require_approval_for_commands`;
lệnh trùng với `eval.test_command` đã xác nhận thì được kế thừa).

**Review và vòng sửa.** `bint task review` nhận JSON hoặc YAML qua tệp/stdin (YAML thuận tiện khi shell
của harness chặn heredoc chứa dấu ngoặc nhọn/nháy kép), ghi `review.json` và `review.md`:

```sh
bint task review "$task_id" - <<'EOF_REVIEW'
summary: Đã đọc diff và đối chiếu tiêu chí; không phát hiện lỗi nghiêm trọng
findings: []
EOF_REVIEW
```

Mỗi phát hiện có `kind` (`bug`, `risk`, `suggestion`), `severity` (`critical`, `high`, `medium`, `low`),
`title`, `evidence` (và `path`, `lines`, `body`, `criterion` nếu có), được gán ID `<task-id>.<n>`. Trong
`b-do`, phát hiện `bug`/`risk` mức `critical`/`high` đưa luồng quay lại `b-feat` (tối đa
`workflow.max_review_rounds`, mặc định 2 vòng; quá số vòng → `blocked`). Khi chạy lại `b-feat`, đề bài
nhắc rõ các tiêu chí còn `unmet` để agent hoàn thành và đối chiếu lại.

**`bint run`** cấp `run_id` cho lượt `b-do` (cha) và mỗi bước (con, `parent_run_id`), ghi `run_steps`,
chi phí/token/mô hình; agent nhận `BINT_RUN_ID`, `BINT_TASK_ID`, `BINT_AGENT=1`, `BINT_RUNNER_PID` và
danh sách đường dẫn đã duyệt, nên lệnh `bint task` và hook gắn sự kiện vào đúng lượt con. Đề bài =
skill (gốc + lớp phủ) + khối `<bint-task>` (phiếu, tài liệu, phát hiện review cần sửa, phạm vi diff) +
gói ngữ cảnh của Context Engine. Giới hạn vận hành (mục 5):

| Giới hạn | Áp dụng |
|---|---|
| `limits.workflows.<wf>.max_cost_usd` (mặc định `limits.per_run`) | `b-do`: tổng các bước; mỗi bước `--max-budget-usd` = min(giới hạn bước, phần còn lại); hết → `blocked` (với harness không báo USD thì ghi chú chỉ dẫn) |
| `limits.workflows.<wf>.max_turns` | `--max-turns` cho bước trên Claude Code (`b-do` trừ số lượt đã dùng khỏi tổng) |
| `limits.protected_paths`, `scope.deny` | sau `b-feat`, đối chiếu tệp mà tác vụ đã sửa (`touched`; phiếu cũ: so với commit gốc); có terminal thì hỏi, không thì `blocked` chờ `task approve` |
| ngoài `scope.allow` | chỉ hỏi khi `autonomy: ask_always` (ngoài ra ghi chú) |
| `workflow.max_review_rounds` (2) | phát hiện `bug`/`risk` mức `critical`/`high` → quay lại `b-feat`; hết vòng → `blocked` |
| `workflow.timeout_minutes` (60), `verify_timeout_minutes` (15) | giới hạn thời gian mỗi bước agent / lệnh kiểm chứng |

**Dừng, duyệt đường dẫn bảo vệ và chạy tiếp (`resume`).** Ctrl-C trong `bint run` chuyển tác vụ sang
`interrupted`; timeout chuyển sang `failed`; tiến trình điều phối bị kill sẽ được phát hiện ở lần đọc
tác vụ tiếp theo và chuyển `interrupted`. Có hai thời điểm duyệt đường dẫn bảo vệ:
1. **Sau khi `b-feat` đã sửa tệp (hậu kiểm diff chờ duyệt):** người dùng kiểm tra `git diff`, chạy
   `bint task approve <id>`, rồi `bint run resume <id>` — luồng tiếp tục kiểm tra phạm vi → kiểm chứng →
   `b-review`, không chạy lại `b-feat`.
2. **Trước khi sửa tệp (hook `PreToolUse` của Codex trả `deny`, chưa có diff):** người dùng duyệt trước
   đường dẫn tệp cụ thể (không nhận glob) bằng `bint task approve <id> --path .github/workflows/ci.yml`,
   rồi chạy tiếp (`--path` đặt điểm tiếp tục về `in_progress` để chạy lại `b-feat`; nếu trước đó gọi lẻ
   `b-feat` khiến tác vụ đã sang `in_review`, gọi lại `bint run b-feat --task <id>`).

Lưu ý: `resume` dùng workflow và độ sâu đã lưu trong tác vụ nhưng **không lưu các cờ ghi đè trên dòng
lệnh** (`--harness`, `--role`, `--model`); nếu lượt trước có ghi đè trên dòng lệnh, truyền lại các cờ đó
khi gọi `bint run resume`.

**Tín hiệu kết quả mới** (mục 9.6): `verify` (lệnh kiểm chứng; mạnh nếu đã xác nhận — quyết định nhãn
như kiểm thử mạnh), `criteria` (agent tự đối chiếu, trung bình), `review` (phát hiện nghiêm trọng,
trung bình, tiêu cực). "Không phát hiện vấn đề" không tạo tín hiệu tích cực.

**Skill gốc và lớp phủ** (mục 6.6). Skill gốc khai báo điểm neo `<!-- bint:anchor <tên> -->` …
`<!-- bint:anchor-end <tên> -->` (`b-req`: `project-rules`, `plan-template`; `b-feat`: `project-rules`,
`test-strategy`; `b-review`: `review-checklist`; `b-do`: `routing`; `b-bootstrap`: `investigation-focus`).
Lớp phủ `.bint/skills/overlays/<skill>.md` (commit được) chỉ chứa khối
`<!-- bint:overlay <tên> append|prepend|replace -->` … `<!-- bint:overlay-end -->` ở cột đầu; văn bản
khác bị bỏ qua. `bint sync` (và `init`, `bootstrap`) ghép rồi xuất bản; `core.lock` ghi băm skill gốc,
lớp phủ và bản đã xuất bản. Nâng cấp Bint: skill gốc mới được áp lại lớp phủ; nếu lớp phủ dùng điểm neo
không còn → **xung đột**, bản đã xuất bản và mục `core.lock` cũ được giữ nguyên cho tới khi sửa lớp phủ
(`--force` không vượt qua được). Skill đã xuất bản bị sửa tay → xung đột như P2.

## Multi-Harness (P5)

Workflow (skill gốc + lớp phủ, phiếu tác vụ, `bint run`) và nguồn tri thức (`.bint/knowledge`,
`AGENTS.md`, Context Engine) tách khỏi harness thực thi. Mỗi harness có một **adapter** (mục 7)
khai báo bảng khả năng đã đối chiếu với một phiên bản cụ thể; khả năng không có được báo rõ, không
được mô phỏng.

```sh
bint harness list                 # harness trên máy, phiên bản, cấp hỗ trợ, bảng khả năng
bint harness show codex           # chi tiết từng khả năng (--json)
bint init --harness claude,codex,antigravity --hooks --mcp
bint run b-do --harness claude --role implementer=codex "…"   # b-feat bằng Codex, bước khác bằng Claude
bint run b-review --harness antigravity --task <id>
bint eval run --harness codex …   # đo trên harness khác (nhánh không hỗ trợ → skipped)
```

| Khả năng | Claude Code (2.x) | Codex CLI (0.167) | Antigravity CLI (agy 1.5.3) |
|---|---|---|---|
| Skill | `.claude/skills` | `.agents/skills` | `.agents/skills` (dùng chung với Codex) |
| Ẩn skill / chỉ gọi tường minh | ✓ (`disable-model-invocation`) | không ẩn khỏi người; `agents/openai.yaml` có `policy.allow_implicit_invocation: false` (Bint chưa sinh policy này) | ✗ |
| MCP server của Bint | `.mcp.json` | `.codex/config.toml` — cần dự án được Codex tin cậy; riêng `cite_knowledge` được đặt `approval_mode = "approve"` | ✗ (chỉ cấp người dùng; dùng CLI `bint`) |
| Hook ghi nhận | SessionStart/End, PreToolUse, PostToolUse(+Failure), UserPromptSubmit, Stop | `.codex/hooks.json`: SessionStart/End, PreToolUse, PostToolUse — mỗi hook cần được duyệt trong Codex | `.agents/hooks.json`: SessionStart, PreToolUse, PostToolUse — không có kết thúc phiên |
| Chèn ngữ cảnh theo đường dẫn | trước Read/Edit/Write (và `UserPromptSubmit`) | chỉ trước `apply_patch` | ✗ (`PreToolUse` chỉ trả quyết định) |
| Guardrail quy trình (`.bint/harness`, xem [harness-components.md](harness-components.md)) | `remind`, `ask`, `deny` trước lệnh/sửa; `Stop` chặn kết thúc (remind một lần, deny tối đa 3 lần) | `deny` và `remind` trước lệnh/sửa; `ask` hạ xuống `remind`; không có `Stop` | `ask`/`deny` trước lệnh/sửa; `remind` bị bỏ (không chèn được ngữ cảnh); không có `Stop` |
| Đường dẫn bảo vệ | hỏi (`ask`) | chặn (`deny`); khi chưa có diff: `task approve <id> --path <tệp>` rồi `run resume <id>` | hỏi (`ask`) |
| Thực thi (`bint run`) | `claude -p --output-format json` | `codex exec --json` (stdin) | `agy --print=… --output-format json` |
| Bước chỉ đọc (`b-req`, `b-review`) | allowlist + cấm công cụ sửa tệp, kèm hậu kiểm | sandbox `workspace-write`, kèm hậu kiểm | `--mode plan`, kèm hậu kiểm |
| Chi phí / giới hạn lượt | ✓ / ✓ | token / ✗ | token / ✗ |
| Sự kiện `skill.activated` | ✓ (công cụ Skill) | ✗ (không tổng hợp giả) | ✗ (không tổng hợp giả) |

Cấp hỗ trợ (mục 7): cả ba ở **cấp 3** (Skill + Hook + thực thi theo chương trình); cấp 4 (điều phối
nhiều agent song song) chưa có.

**Chọn harness** cho từng bước theo thứ tự (mục 5): `--role <role|step>=<harness>` → `--harness` →
`roles.planner|implementer|reviewer` → `harnesses.default` → `claude`. Harness phải nằm trong
`harnesses.enabled` — yêu cầu dòng lệnh không vượt qua cấu hình dự án. Tham số gọi Codex/Antigravity
ở `harnesses.codex|antigravity.{binary, model, extra_args}`; Bint không tự thêm cờ bỏ quyền
(`--dangerously-*`). Adapter không khả dụng (thiếu chương trình) → bước thất bại với lỗi nêu cách xử lý.

**Cô lập bước có quyền ghi và hậu kiểm bước chỉ đọc:**
- **Khoá theo worktree có hai chế độ** (`flock`/`LockFileEx` trong `<state>/runtime/locks/`, bất kể harness):
  *chia sẻ* cho bước chỉ đọc (`b-req`, `b-review`);
  *độc quyền* cho `b-feat` và cho lệnh kiểm chứng chạy ngay sau `b-feat` (lệnh kiểm chứng hay ghi tệp
  như coverage/snapshot/build, không được chạy song song với bước chỉ đọc của tác vụ khác). Nhiều bước chia sẻ chạy cùng lúc được; bước độc quyền loại trừ mọi bước khác.
  Khoá chia sẻ giữ suốt bước chỉ đọc nên `b-feat` của tác vụ khác không chen vào giữa hai ảnh chụp hậu kiểm.
- **Bước đến sau chờ khoá** (không còn `blocked` ngay): in "chờ khoá do <tác vụ> <bước> giữ…", chờ tối đa
  `workflow.timeout_minutes` rồi `blocked` ("chờ khoá quá N phút: <chủ>"); Ctrl-C khi chờ → `interrupted`.
  `--no-wait` (cả `bint run` và `bint run resume`) lấy khoá một lần, bận thì `blocked` như hành vi cũ. Khoá
  của tiến trình chết do hệ điều hành tự nhả; thông tin "ai đang giữ" chỉ để báo, không quyết định khoá.
  Tệp khoá không bị xoá khi trả (xoá khi còn người đọc khác sẽ cho hai chủ độc quyền cùng vào).
- **Mỗi tác vụ chạy đúng worktree của nó** (`State.Worktree`, ghi lúc lập phiếu): `run`, `run resume`,
  `task verify`, `task status done`, `task diff` dùng thư mục đó dù đang đứng ở thư mục khác (in "chạy
  trong <đường dẫn>"). Thư mục không còn hoặc không còn là gốc worktree Git → tác vụ `failed` với thông
  báo rõ, không lùi về thư mục đang đứng. Phiếu cũ (không có `Worktree`) dùng thư mục hiện tại.
- **Tập tệp của riêng từng tác vụ.** Trước `b-feat` đầu tiên Bint chụp cây làm việc (`base_tree`, giữ sống
  bằng `refs/bint/tasks/<id>/base`); sau mỗi lượt `b-feat` (kể cả lỗi/Ctrl-C) ghi các tệp đã đổi vào
  `touched`. Kiểm tra phạm vi, đường dẫn bảo vệ và `task status done` dùng `touched`, nên thay đổi chưa
  commit có sẵn trước khi lập phiếu và tệp của tác vụ khác **không** bị tính cho tác vụ. `bint task diff`
  hiển thị đúng phần này (b-review dùng lệnh này thay cho `git diff <base>`). Phiếu có `b-feat` từ trước khi
  có cơ chế này (hoặc làm tay) giữ cách cũ (toàn bộ thay đổi so với base) suốt đời; chụp lỗi thì cũng quay
  về cách cũ kèm ghi chú. Thay đổi **ngoài** `b-feat` của Runner
  (người hoặc phiên agent khác sửa tay, ở bất kỳ trạng thái nào của tác vụ, kể cả sau `bint task status <id>
  in_progress`) không có trong `touched`; chúng được tính thêm là tệp đổi giữa `base_tree` và cây hiện tại
  mà không được miễn: `touched` của chính tác vụ luôn được miễn; `touched` của tác vụ **khác** chỉ miễn khi tác
  vụ đó **chưa kết thúc** (không `done`/`cancelled`) **và** tệp không phải đường dẫn bảo vệ/deny của phiếu này.
  Nhờ vậy tệp bảo vệ/deny sửa tay vẫn bị chặn ở cổng `done` và hiện trong `bint task diff`, kể cả khi một tác vụ
  khác (đang chạy hay đã xong) từng sửa đúng tệp đó. Hệ quả: thay đổi tay của người dùng trên cùng worktree cũng bị
  quy cho tác vụ (chỉ dẫn tới yêu cầu duyệt, không mất dữ liệu). Cây gốc (ref) bị xoá khi tác vụ `done`/`cancelled`.
  Tập tệp phạm vi của `b-feat` được tính **ngay sau bước, khi còn giữ khoá độc quyền** (rồi mới hỏi duyệt, đã nhả
  khoá), nên `b-feat` của tác vụ khác vừa giành khoá không bị quy nhầm; lượt `resume` (b-feat đã chạy ở lượt trước)
  tính dưới khoá chia sẻ, tức đợi `b-feat` đang chạy xong.
- **Chồng lấn:** sau `b-feat`, tệp mà tác vụ chưa kết thúc khác trên cùng worktree cũng đã sửa được đưa vào
  danh sách cần duyệt ("tác vụ <id> cũng sửa: <tệp>"): có terminal thì hỏi, không thì `blocked` chờ
  `bint task approve` rồi `bint run resume`.
- **Kiểm chứng:** vẫn tính trên toàn cây (thay đổi của tác vụ khác làm kết quả cũ mất nghĩa thật), nhưng khi
  `task status done` bị chặn vì mã đã đổi, thông báo (chỉ ở lệnh chuyển trạng thái, không ở `bint task show`/TUI để tránh chụp cây mỗi lần xem) liệt kê tệp đổi **ngoài** `touched` kể từ lần kiểm
  chứng ("thay đổi không thuộc tác vụ: …; chạy lại bint task verify <id>").
- Bước chỉ đọc (`b-req`, `b-review`) được **hậu kiểm ở tầng chương trình**: Bint chụp cây làm việc bằng
  chỉ mục Git tạm trước và sau bước (gồm cả tệp theo dõi và tệp mới chưa bị Git ignore, **kể cả
  `.bint/`**). Ngoại lệ duy nhất được phép là thêm mục tri thức mới hợp lệ ở trạng thái `candidate`
  trong `.bint/knowledge/items/`; mọi thay đổi khác làm bước thất bại kèm danh sách tệp bị sửa (Bint
  không tự hoàn tác).
- **Thay đổi do Bint ghi thay người không tính là vi phạm** (docs/isolation-subagent-plan.md 4.5, H8).
  Người đổi cấu hình (`bint config set`/TUI), duyệt-từ chối-xoá tri thức, sinh lại `AGENTS.md`/skill, `init --update`…
  trong lúc bước chỉ đọc chạy đều đi qua tầng dịch vụ (`internal/app`, dùng chung cho CLI và TUI); mỗi tệp dịch vụ
  ghi được nối một dòng vào `<state>/runtime/writes.jsonl` (`{path, sha256, prev_sha256, at, actor, agent}`, nối bằng
  `O_APPEND` mỗi dòng một lần ghi; tệp bị xoá ghi `sha256` rỗng; `prev_sha256` là băm ngay trước khi Bint ghi, rỗng
  nếu tệp chưa có, còn `sha256` tính từ dữ liệu ghi; dòng cũ hơn `workflow.timeout_minutes` × 2 được dọn khi nối).
  Khi hậu kiểm, một đường dẫn đổi được **miễn** nếu các dòng nhật ký của nó trong khoảng bước chạy tạo thành một
  **chuỗi nối liền** từ nội dung ở ảnh chụp trước (blob đọc qua `git cat-file --filters`, nên đúng byte cây làm
  việc) tới nội dung hiện tại: mọi dòng `agent: false` (không có biến môi trường phiên agent), có `prev_sha256` bằng
  băm hiện hành rồi chuyển sang `sha256` của dòng; cuối chuỗi băm khớp nội dung hiện tại. Nhờ đó ghi đọc-sửa-ghi
  của người không hợp thức hoá sửa đổi agent đã làm trước đó trên cùng tệp (`prev_sha256` là bản agent đã sửa, chuỗi
  đứt). Kết quả bước liệt kê tệp
  được miễn (`steps[].human_writes` trong `bint run --json`, `Result.Notes`) và log in
  "bỏ qua thay đổi do người ghi qua Bint: …" để người xem lại. Vẫn là vi phạm: agent gọi lệnh `bint …` (dòng có
  `agent: true`), agent sửa tệp rồi người đọc-sửa-ghi (chuỗi đứt), người sửa rồi agent sửa tiếp cùng tệp (băm lệch),
  dòng thiếu `prev_sha256`, sửa tệp trực tiếp không qua Bint. Hook không
  ghi vào nhật ký. TUI hiện dòng "<bước> của <tác vụ> đang chạy trên cây này" ở thanh trên khi cây có bước đang
  chạy (đọc tệp chủ khoá, thăm dò 3 giây); thao tác ghi vẫn được phép.
- **Mỗi tác vụ một worktree (`workflow.isolation`, docs/isolation-subagent-plan.md G2).** Một tác vụ có thể
  chạy trong worktree Git riêng do Bint tạo để hai tác vụ trên cùng một repo chạy **song song thật** (khoá của
  chúng khác nhau, không chờ nhau) mà thay đổi vẫn về cây đã lập phiếu dưới dạng chưa commit:
  - *Chọn chế độ, một lần lúc lập phiếu* (`Classification.isolation` = `shared` | `worktree`, kèm
    `isolation_reason` = `concurrent_task` | `user` | `config` | `default` và `isolation_tasks`): cờ `--worktree` /
    `--shared` (loại trừ nhau; không dùng cùng `--task`) → `workflow.isolation`. Với `auto`: có tác vụ **khác** chưa
    `done`/`cancelled` mà `State.Worktree` là cây hiện tại và đang `shared` (phiếu cũ không ghi chế độ = `shared`) ⇒
    `worktree` (`concurrent_task`, kèm ID tác vụ đó; tác vụ bỏ dở cũng tính, huỷ chúng bằng `bint task cancel`);
    ngược lại `shared`. `task new --json` có `isolation`, `isolation_reason`, `isolation_tasks` ở cấp trên; `resume`
    không chọn lại.
  - *Tạo lười ở lượt `bint run` đầu tiên* (mọi workflow, kể cả `b-req`), sau khi nhận quyền điều phối: ảnh chụp cây
    gốc (gồm thay đổi chưa commit và tệp mới) dưới khoá chia sẻ → commit `bint base <id>` (cha `HEAD`) →
    `refs/bint/tasks/<id>/base` → `git worktree add -b bint/<id> <state>/worktrees/<id>` → chép tệp cục bộ do
    Bint quản lý và tệp khớp `workflow.worktree.copy` → `workflow.worktree.setup_command`. Nhờ ảnh chụp, agent thấy
    đúng nội dung đang sửa dở ở cây gốc. Lỗi ở bước nào thì dọn phần đã tạo, tác vụ `failed` (nêu đường dẫn nhật ký
    của `setup_command`), `resume` tạo lại. Thành công: `State.Origin` = cây gốc, `State.Worktree` = worktree,
    `State.Branch` = `bint/<id>`, `State.Base` = base commit; mọi bước, khoá, ảnh chụp, kiểm chứng sau đó chạy ở
    worktree (đề bài nêu "Thư mục làm việc"). Ref base dùng chung tên với `base_tree` của G1: ở `b-feat` đầu ref
    trỏ vào commit `base_tree` cha là base commit, nên base vẫn tới được qua chuỗi cha và qua nhánh `bint/<id>`.
  - *Đưa về* — `bint task land <id>`: bản vá nhị phân `git diff --binary` từ base tới ảnh chụp worktree, giới hạn
    trong tệp của tác vụ (`touched`; phiếu không theo dõi thì mọi thay đổi so với base), lấy **khoá độc quyền của
    cây gốc** (chờ như Runner), `git apply --check` rồi `git apply` **không `--index`**: HEAD và chỉ mục của bạn
    không đổi. Chỉ đưa về khi điều kiện `done` (trừ chính việc đưa về) đã đủ — kiểm ở worktree vì sau khi gỡ không
    còn chỗ chạy kiểm chứng — và tác vụ không đang được `bint run` khác điều phối. Không áp sạch ⇒ cây gốc **không
    bị đụng**, lỗi liệt kê tệp xung đột. `--3way` (**chỉ người**, `ErrAgentForbidden`): phần sạch vẫn áp, tệp xung
    đột trộn bằng `git merge-file` (cây gốc / base / worktree) và để lại dấu `<<<<<<<` để người gỡ; tệp nhị phân
    hoặc bị xoá/sửa ở một phía giữ nguyên (`Landed.Unmerged`) — lúc đó worktree **được giữ lại** dù không có `--keep`
    vì bản của tác vụ chỉ còn ở đó (lấy bằng `git -C <worktree> show <mã cây>:<tệp>`). Sau `--3way`, `done` bị chặn
    tới khi các tệp trong `Landed.Conflicts` hết dấu `<<<<<<<` (rồi `bint run resume`); tệp không trộn được thì
    người xử lý xong chạy `bint task status <id> done --force`. (Không dùng `git apply --3way` vì lệnh đó ngầm ghi
    chỉ mục; `git apply` chạy với `LC_ALL=C` để đọc được thông báo lỗi.)
    Thành công: `State.Landed` (thời điểm, người, tệp, tệp xung đột, fingerprint và mã cây lúc đưa về), gỡ worktree,
    nhánh `bint/<id>`, ref base, tệp khoá của worktree (`--keep` giữ worktree). Gỡ lỗi (Windows giữ tệp…) thì ghi chú
    "dọn tay: <lệnh git>" và `Landed` vẫn ghi. Tác vụ đã đưa về dùng bản lưu trong `Landed` cho cổng `done`,
    `task diff`, kiểm tra phạm vi (worktree có thể đã gỡ).
  - *Finalize của `b-do`*: kiểm điều kiện `done` (ở worktree) → `land` → `done`. Xung đột ⇒ `blocked` với lý do bắt đầu
    bằng "xung đột khi đưa về: <tệp>" kèm hướng dẫn; `State.awaiting_land` được đặt nên `resume` vào thẳng finalize
    (không chạy lại `b-review`). `bint task status <id> done` của tác vụ worktree bị chặn khi chưa đưa về và
    `--force` **không** bỏ qua điều này (done rồi thì không đưa về được nữa); chạy từng bước thì
    `bint task land <id>` rồi `done`; muốn bỏ thay đổi thì `bint task cancel <id> --discard`. Báo cáo ghi rõ kiểm chứng đã chạy trên cây cô lập và cây gốc có
    thể còn thay đổi khác chưa được kiểm cùng.
  - *Huỷ* — `bint task cancel <id> [--reason] [--discard]` (và `task status <id> cancelled`, cùng đường): gỡ
    worktree, nhánh, ref, tệp khoá. Worktree còn thay đổi chưa đưa về ⇒ cần `--discard` (**chỉ người**).
  - *Quan sát*: `bint run status <id>` và `bint task show <id>` hiện chế độ, đường dẫn worktree, nhánh, cây gốc,
    trạng thái đưa về (`--json`: `isolation`, `worktree`, `branch`, `origin`, `landed`); `bint doctor` báo
    **worktree mồ côi** (thư mục trong `<state>/worktrees` không ứng với tác vụ chưa kết thúc, worktree còn giữ bằng
    `--keep`, mục `prunable` của `git worktree list`) kèm lệnh git để dọn tay — doctor không tự xoá. TUI: phím `o`
    (đưa về) và `x` (huỷ) ở màn Tác vụ, cột chế độ cô lập (`wt`, `wt✓` đã đưa về), ô `--worktree`/`--shared` ở form
    `task new` và `run`. Quét commit của runlog (`gitscan`) bỏ qua `refs/bint/*`, nhánh `bint/*` và commit "bint base".
  - *Giới hạn đã biết*: kiểm chứng của tác vụ `shared` giữ fingerprint toàn cây nên khi tác vụ `worktree` đưa về trước
    lúc nó finalize thì kết quả cũ mất hiệu lực (cần verify lại); tệp tác vụ khác đã đưa về cây này không bị quy cho
    tác vụ `shared` chừng nào chưa bị sửa tiếp (blob khác thì bị tính). `WritableDirs` của agent là cả thư mục trạng
    thái nên về lý thuyết ghi được worktree của tác vụ khác (chưa thu hẹp). Tạo worktree chưa chạy thử trên Windows.
- Codex chạy mọi bước trong sandbox `workspace-write` kèm `--add-dir <thư mục trạng thái>` để lệnh
  `bint task …` ghi được; agent nhận `BINT_RUNNER_PID` để không nhầm tiến trình điều phối (bị sandbox
  che PID) là đã chết.

**Chuẩn hoá sự kiện và duyệt trust trên Codex** (mục 13.2):
- `bint hook codex` (`apply_patch` sửa nhiều tệp → mỗi tệp một `file.modified`) và
  `bint hook antigravity <sự kiện>` (payload camelCase `toolCall{name,args}`) chuẩn hoá về cùng mô hình
  lượt chạy. Lượt Antigravity không có `session.ended` được đóng `interrupted` theo thời gian.
- Hook Codex chỉ chạy sau khi người dùng duyệt trong `/hooks`; `bint doctor` gọi API `hooks/list` của
  Codex để kiểm tra mọi định nghĩa/băm hiện hành và cảnh báo nếu có hook mới, bị sửa hoặc bị tắt.
- Trên Codex, `PostToolUse` xuất hiện cả khi lệnh shell thất bại: Bint tra `exit_code` trong transcript
  theo `tool_use_id` (thiếu bằng chứng → `unknown`) và chỉ nâng lệnh kiểm thử trực tiếp đơn lẻ thành
  tín hiệu `tests` (loại trừ script nhiều lệnh, heredoc, lệnh chạy nền và mọi lời gọi CLI `bint`).

**Bằng chứng nghiệm thu thực tế:**
- [Codex 2026-11-10](evals/codex-flows-2026-11-10/README.md) và
  [Codex 2026-11-11](evals/codex-flows-2026-11-11/README.md): 30 phiên chạy thật kiểm chứng đủ 5 skill,
  trust hook, fail-open, hậu kiểm bước chỉ đọc, timeout, Ctrl-C/`resume` và đánh giá `strict` 3 nhánh.
- [Hỗn hợp Claude↔Codex 2026-11-11](evals/claude-codex-mixed-2026-11-11/README.md): đã chạy thật cả hai
  chiều phân vai (Claude lập kế hoạch/review ↔ Codex thực thi, và ngược lại) cùng luồng chặn đường dẫn
  bảo vệ → `task approve` → chạy tiếp.

Giới hạn đã biết của P4–P5:

- Classify của `bint task new` là heuristic từ khoá; phân loại bằng mô hình do orchestrator của `/b-do`
  đảm nhận (truyền `--plan`/`--no-plan`/`--depth`), `bint run` gọi trực tiếp thì vẫn theo heuristic.
- Chưa xuất tài liệu sang `docs/spec/<feature>/`.
- Trên worktree chung, người sửa đúng tệp mà `b-feat` đang sửa trong lúc nó chạy thì phần sửa bị tính cho
  tác vụ (không phân biệt được ai sửa). Người sửa tệp khác **bằng IDE hoặc công cụ ngoài Bint** trong lúc bước
  chỉ đọc chạy vẫn làm bước đó `failed` ("nếu chính bạn sửa trong lúc bước chạy: bint run resume"): chỉ thay đổi đi
  qua Bint mới được miễn (xem trên). Muốn vừa sửa vừa để agent chạy thì dùng worktree riêng (`--worktree` hoặc `workflow.isolation: worktree`).
- Nhật ký ghi `writes.jsonl` nằm trong thư mục trạng thái mà bước chỉ đọc được ghi, nên agent cố tình có thể
  chèn dòng giả (đúng `path`, `sha256` của nội dung nó vừa ghi, `agent: false`) để được miễn. Chấp nhận: mục
  đích là tránh chặn nhầm người, không chống agent cố ý; lớp chặn chính của bước chỉ đọc vẫn là
  `--disallowedTools`/sandbox của harness và kết quả bước liệt kê tệp được miễn. Phiên người dùng chạy trong
  phiên agent (có biến `CLAUDECODE`, `BINT_AGENT`…) cũng ghi `agent: true` nên không được miễn.
- Miễn H8 cần chuỗi `prev_sha256`: dòng nhật ký của đường ghi không biết trạng thái trước thì không được miễn (an
  toàn, có thể báo vi phạm với ghi hợp lệ của người): `InstallMCP` (`init --mcp`: đường dẫn chỉ biết sau khi ghi) và
  `evolve apply/rollback/cycle` (gói `evolve` tự ghi nhiều tệp). Dòng nhật ký cũ (trước khi có `prev_sha256`) còn
  trong hạn dọn cũng không được miễn: bước chỉ đọc đang chạy lúc nâng cấp có thể báo vi phạm với ghi của người
  (`bint run resume`). Bộ lọc Git (eol/LFS) lệch hay lỗi khi đọc blob cũng thành vi phạm.
- Tệp của tác vụ **đã kết thúc** (`done`/`cancelled`) còn khác `base_tree` của tác vụ đang xét thì bị quy cho tác vụ
  đang xét (kể cả tệp không bảo vệ, kể cả đã commit vì `base_tree` là cây chụp): tệp bảo vệ/deny đã được duyệt ở
  tác vụ kia phải duyệt lại ở đây. Tệp bảo vệ/deny của tác vụ **chưa kết thúc** khác cũng bị quy cho tác vụ đang xét
  (chỉ tệp thường mới được miễn). Hướng chính xác hơn (ghi cây sau mỗi `b-feat`, miễn khi blob hiện tại khớp) chưa làm.
- Hậu kiểm so nội dung bằng băm: người ghi qua Bint rồi agent ghi lại đúng nội dung đó thì không phát hiện được.
  Lỗi ghi nhật ký (đĩa đầy, không lấy được khoá) không làm hỏng thao tác của người; hậu quả là bước chỉ đọc báo
  vi phạm như trước (hướng an toàn). Nhật ký chưa chạy thử thật trên Windows (đổi tên tệp khi đang mở).
- Đói lượt khoá độc quyền: bước chỉ đọc nối tiếp nhau có thể giữ `b-feat` chờ lâu; hết `workflow.timeout_minutes`
  thì `blocked`.
- Tác vụ chưa kết thúc (kể cả `failed`/`blocked` bị bỏ quên) vẫn được tính khi dò chồng lấn: huỷ
  (`task status <id> cancelled`) tác vụ bỏ dở để hết cảnh báo.
- Khoá Windows (`LockFileEx` trên một byte ở offset lớn) mới kiểm biên dịch (`GOOS=windows go vet`, staticcheck),
  chưa chạy thật; trong lúc nâng cấp, binary cũ (khoá byte 0) và mới không loại trừ lẫn nhau trên Windows.
- Ghi đè `--harness`/`--role` chưa lưu vào `state.json` của tác vụ nên cần truyền lại khi `resume`.
- Hook `deny` của Codex chặn thao tác sửa tệp nhưng chưa tự chuyển tác vụ sang `blocked`.
- Antigravity: chưa có MCP cấp dự án, chưa chèn ngữ cảnh qua hook, đề bài `bint run` truyền qua một
  đối số dòng lệnh (giới hạn 120 KiB), và một số payload công cụ thực tế cần kiểm chứng thêm khi chạy live.
