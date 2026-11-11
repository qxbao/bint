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
đề xuất bỏ `b-req` cho việc nhỏ/ngắn gọn (`--plan`/`--no-plan` ghi đè). Khi phạm vi chạm đường dẫn bảo
vệ (`Assessment.Risk=high`), tự động bật `b-req` trừ khi có `--no-plan`.

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
| `limits.protected_paths`, `scope.deny` | sau `b-feat`, đối chiếu tệp đã sửa với commit gốc; có terminal thì hỏi, không thì `blocked` chờ `task approve` |
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
- Mỗi worktree chỉ một bước có quyền ghi (`b-feat`) chạy tại một thời điểm, bất kể harness, thông qua
  khoá hệ điều hành (`flock`/`LockFileEx`) trong `<state>/runtime/locks/`; muốn chạy song song thì dùng
  worktree khác.
- Bước chỉ đọc (`b-req`, `b-review`) được **hậu kiểm ở tầng chương trình**: Bint chụp cây làm việc bằng
  chỉ mục Git tạm trước và sau bước (gồm cả tệp theo dõi và tệp mới chưa bị Git ignore, **kể cả
  `.bint/`**). Ngoại lệ duy nhất được phép là thêm mục tri thức mới hợp lệ ở trạng thái `candidate`
  trong `.bint/knowledge/items/`; mọi thay đổi khác làm bước thất bại kèm danh sách tệp bị sửa (Bint
  không tự hoàn tác).
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

- Classify bằng heuristic từ khoá; chưa có agent phân loại.
- Chưa tự động tạo worktree riêng cho từng bước ghi; chưa xuất tài liệu sang `docs/spec/<feature>/`.
- Ghi đè `--harness`/`--role` chưa lưu vào `state.json` của tác vụ nên cần truyền lại khi `resume`.
- Hook `deny` của Codex chặn thao tác sửa tệp nhưng chưa tự chuyển tác vụ sang `blocked`.
- Antigravity: chưa có MCP cấp dự án, chưa chèn ngữ cảnh qua hook, đề bài `bint run` truyền qua một
  đối số dòng lệnh (giới hạn 120 KiB), và một số payload công cụ thực tế cần kiểm chứng thêm khi chạy live.
