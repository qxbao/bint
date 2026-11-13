# Kế hoạch: custom skill và custom flow

> Chép từ artifact b-req của phiếu `01M79GY29G41X7J3AZCY30TTWB` (2026-11-13). Mỗi giai đoạn (GĐ) lập một phiếu b-do riêng.
> Tài liệu tham khảo: [superpowers-writing-skills.md](references/superpowers-writing-skills.md).

Trạng thái (2026-11-13): **GĐ1 đã làm** (commit `21171cb`: TSK-1..3), **GĐ2 đã làm** (commit `f77ae19`: TSK-4),
**GĐ3 đã làm** (commit `f124cd7`: TSK-5..6), **GĐ4 đã làm** (TSK-7..8: chạy flow tuỳ biến qua `bint run <flow>`, `task new
--workflow`, `resume`, driver session và TUI; xem [workflows.md](workflows.md); đang chờ commit); GĐ5–6 chưa làm. Lượt nghiệm thu thủ công sau GĐ5–6 được theo dõi ở [acceptance.md](acceptance.md)
(mã `NT-CF-MANUAL`).

## Yêu cầu

### Mục tiêu và phạm vi

Hiện Bint chỉ có một chuỗi cố định `b-do = Classify → b-req → b-feat → b-review → Finalize`, mã hoá cứng
trong `internal/workflow/run.go` (hằng `StepReq/StepFeat/StepReview/StepDo`, `nextStep`, `afterStep`,
`beginStep`, `prompt`) và rải ở `config.WorkflowNames`, `harness.StepRole`, `app.workflowRoles/roleStep`,
`harness.SubagentFiles`. Skill duy nhất người dùng tự đưa vào được là lớp phủ tại điểm neo của skill gốc
(`.bint/skills/overlays/`) và skill tiến hoá do `bint evolve` sinh (`.bint/skills/evolved/`).

Mục tiêu: người dùng (1) viết **skill tuỳ biến** trong `.bint/skills/`, được `bint sync` xuất bản sang
mọi harness đang bật như skill gốc; (2) khai báo **flow tuỳ biến** ghép các bước từ skill gốc và skill
tuỳ biến, chạy bằng `bint run` với đủ cổng an toàn của Bint; (3) có hai skill gốc hướng dẫn agent giúp
người dùng tạo skill/flow, áp phương pháp "viết skill như TDD" của tài liệu tham khảo
(docs/references/superpowers-writing-skills.md) kết hợp công cụ của Bint (lint, sync, đo bằng eval).

Phiếu này chỉ lập kế hoạch. Việc triển khai chia 6 giai đoạn, mỗi giai đoạn một phiếu b-do riêng
(xem plan.md và tasks.md).

### Yêu cầu chức năng

1. Skill tuỳ biến nằm ở `.bint/skills/{tên}/SKILL.md`, có thể kèm tệp phụ (tài liệu tham chiếu nặng, script)
   trong cùng thư mục; không gian tên phẳng chung với skill gốc và skill tiến hoá. Tên `overlays`, `evolved`
   và tiền tố `b-` được giữ cho Bint.
2. `bint sync` xuất bản skill tuỳ biến (cả tệp phụ) cho từng target harness cùng cơ chế với skill gốc: khoá
   phiên bản trong `core.lock` (mục mới `custom`), phát hiện bản bị sửa tay, sao lưu khi `--force`, gỡ bản đã
   xuất bản khi skill bị xoá. Với `.agents/skills` frontmatter rút còn `name`, `description` như hiện nay.
3. `bint skill new {tên}` sinh mẫu SKILL.md; `bint skill lint [tên]` kiểm phần máy kiểm được của quy tắc viết
   skill (tên, frontmatter, độ dài mô tả, khoá cho phép, liên kết tệp phụ, kích thước thân). `bint skill list`
   và `bint skill show` hiển thị cả skill tuỳ biến.
4. Flow tuỳ biến khai báo trong `.bint/flows/{tên}.yml`: danh sách bước, mỗi bước có `id`, `kind`
   (`plan` | `implement` | `review`), `skill`, `role`; tuỳ chọn: bước plan có thể bỏ qua theo phân loại, đích
   của vòng sửa sau review, có chạy kiểm chứng sau bước implement hay không. `bint flow list|show|lint` đọc và
   kiểm định nghĩa.
5. Bốn workflow gốc được biểu diễn bằng định nghĩa flow nhúng; Runner chọn bước, trạng thái tác vụ, chế độ chỉ
   đọc, cổng sau bước và đề bài theo `kind` của bước, không theo tên bước. Hành vi của b-do không đổi.
6. `bint run {flow}`, `bint task new --workflow {flow}`, `bint run resume`, driver session (`bint run next`,
   `complete`) và màn Tác vụ của TUI chạy được flow tuỳ biến; tên flow được lưu trong phiếu như workflow gốc.
7. Role của bước là tên tự do: harness lấy từ `roles.{role}`, model từ `harnesses.{h}.models.{role}`, quyền MCP
   từ `mcp.grants.{role}` (khoá này đã nhận tên tự do). Role chưa cấu hình thì dùng role gốc tương ứng với kind
   (plan → planner, implement → implementer, review → reviewer).
8. Mỗi flow tuỳ biến hợp lệ được xuất bản thành một skill điều phối cùng tên (giống b-do) để người dùng gọi
   `/{flow}` trong harness; skill này chỉ hướng dẫn chạy `bint run {flow}` và xử lý blocked.
9. Skill gốc `b-add-skill` hướng dẫn agent cùng người dùng tạo skill tuỳ biến: xác định có nên là skill hay
   không (quy ước dự án thì vào tri thức/lớp phủ), viết `description` chỉ nêu điều kiện kích hoạt, dùng
   `bint skill new/lint`, kịch bản áp lực RED/GREEN qua `bint skill test`, rồi `bint sync`.
10. Skill gốc `b-add-flow` hướng dẫn tạo flow: chọn bước và kind, role, lint, sync, chạy thử trên một phiếu nhỏ.
11. `bint skill test {tên}` chạy các kịch bản áp lực khai báo trong `.bint/skills/{tên}/tests/` ở hai nhánh có
    và không có skill, chấm theo tiêu chí khai báo trong kịch bản, ghi kết quả vào thư mục trạng thái.

### Tiêu chí chấp nhận

- [ ] AC-1: Có skill gốc b-add-skill: agent hướng dẫn người dùng tạo custom skill trong .bint/skills/ (đúng định dạng, kiểm tra, xuất bản sang harness)
- [ ] AC-2: Có skill gốc b-add-flow: agent hướng dẫn người dùng tạo custom flow (ghép các skill/bước, kiểm tra định nghĩa flow, xuất bản)
- [ ] AC-3: Skill tuỳ biến ở .bint/skills/{tên}/SKILL.md (kèm tệp phụ cùng thư mục) được bint sync xuất bản sang .claude/skills/{tên}/ và .agents/skills/{tên}/, ghi vào mục custom của core.lock; bản đã xuất bản bị sửa tay thì báo xung đột, không ghi đè
- [ ] AC-4: bint skill lint báo lỗi khi skill tuỳ biến có tên sai quy tắc, tiền tố b-, trùng skill gốc hoặc skill tiến hoá, name khác tên thư mục, description rỗng hoặc quá 1024 ký tự, khoá frontmatter ngoài danh sách cho phép, hoặc liên kết tới tệp phụ không tồn tại; mẫu do bint skill new {tên} sinh ra qua lint
- [ ] AC-5: Flow tuỳ biến khai báo trong .bint/flows/{tên}.yml (schema 1: bước có id, kind plan|implement|review, skill, role); bint flow lint từ chối flow tham chiếu skill không tồn tại, kind sai, sai thứ tự plan → implement → review, thiếu bước implement, đích vòng sửa không phải bước implement, hoặc tên trùng workflow gốc
- [ ] AC-6: b-do, b-req, b-feat, b-review được biểu diễn bằng định nghĩa flow nhúng và Runner chọn bước theo kind của định nghĩa; kiểm thử hiện có của internal/workflow, internal/app, internal/cli qua mà không đổi kỳ vọng
- [ ] AC-7: bint run {flow}, bint task new --workflow {flow}, bint run resume và driver session (bint run next/complete) chạy được flow tuỳ biến: mỗi bước dùng nội dung skill đã khai báo, bước plan/review chỉ đọc, kiểm phạm vi sau mỗi bước implement, lệnh kiểm chứng chạy trước bước review đầu tiên và trước khi done
- [ ] AC-8: Role tuỳ biến của flow lấy harness từ roles.{role}, model từ harnesses.{h}.models.{role} và quyền MCP từ mcp.grants.{role}; .bint/config.yml cũ không có khoá mới vẫn nạp được và TestRenderMatchesDefaults qua
- [ ] AC-9: Mỗi flow tuỳ biến hợp lệ được bint sync xuất bản thành skill điều phối cùng tên cho harness, hướng dẫn chạy bằng bint run {flow}; flow bị xoá thì skill điều phối đã xuất bản được gỡ nếu chưa bị sửa tay
- [ ] AC-10: bint skill test {tên} chạy các kịch bản áp lực trong .bint/skills/{tên}/tests/ hai nhánh có và không có skill qua harness đã cấu hình, ghi tỉ lệ tuân thủ của từng nhánh vào thư mục trạng thái, và không gọi agent khi thiếu --yes ở chế độ không tương tác
- [ ] AC-11: Sau mỗi giai đoạn: go test ./... và GOOS=windows go vet ./... đều qua

### Ngoài phạm vi

- Kind bước mới do người dùng tự định nghĩa (cổng, trạng thái tác vụ riêng). v1 chỉ có ba kind với cổng do Bint giữ.
- Bước chạy song song, rẽ nhánh có điều kiện, flow gọi flow khác.
- Flow bỏ qua cổng của Bint: lệnh kiểm chứng đã xác nhận, đường dẫn bảo vệ, autonomy, điều kiện done có bằng chứng,
  giới hạn chi phí/lượt. Flow chỉ chọn bước nào chạy, không nới cổng.
- Sửa trực tiếp skill gốc (vẫn qua lớp phủ, mục 6.6 của docs/bint-plan.md); skill tuỳ biến không được trùng tên skill gốc.
- Chợ/registry skill dùng chung giữa các dự án, cài skill từ URL.
- Skill tuỳ biến cấp người dùng (`~/.claude/skills`); chỉ cấp dự án.

### Câu hỏi mở

Không có câu hỏi cản trở. Các quyết định thiết kế cần người xác nhận khi duyệt kế hoạch (vị trí `.bint/flows/`,
ba kind cố định, danh sách khoá frontmatter cho phép) được ghi trong plan.md mục "Giả định và mâu thuẫn"; muốn
khác thì sửa trước khi lập phiếu của giai đoạn tương ứng.

## Kế hoạch: skill tuỳ biến và flow tuỳ biến

### 1. Hướng triển khai

Tách hai khái niệm đang dính nhau trong `internal/workflow/run.go`:

- **Kind của bước** (do Bint giữ): `plan` | `implement` | `review`. Kind quyết định trạng thái tác vụ, chế độ
  chỉ đọc, khoá worktree, cổng sau bước (hợp đồng v1 của tài liệu kế hoạch; kiểm phạm vi + kiểm chứng; quyết
  định review) và subagent của driver session. Hiện phần này mã hoá theo tên `b-req/b-feat/b-review`.
- **Skill của bước** (người dùng chọn): nội dung đề bài. Hiện `prompt()` lấy `skills.Get(step)`; sau thay đổi lấy
  skill khai báo trong flow (gốc + lớp phủ, hoặc skill tuỳ biến).

Flow = dãy bước (id, kind, skill, role). Bốn workflow gốc trở thành flow nhúng. Runner đọc định nghĩa flow thay
vì so tên bước. Flow tuỳ biến chỉ chọn **bước nào chạy với skill nào**; mọi cổng (kiểm chứng đã xác nhận, đường
dẫn bảo vệ, autonomy, done có bằng chứng, giới hạn) vẫn do Bint áp.

Skill tuỳ biến dùng lại đường xuất bản sẵn có (`skills.Install` → `publish`/`unpublish`, khoá phiên bản trong
`core.lock`, manifest phát hiện sửa tay) giống skill tiến hoá, thêm phần chép tệp phụ.

#### Định dạng

Skill tuỳ biến (bố cục phẳng như tài liệu tham khảo):

```text
.bint/skills/
  core.lock            # đã có
  overlays/            # đã có (tên dành riêng)
  evolved/             # đã có (tên dành riêng)
  write-failing-test/  # skill tuỳ biến
    SKILL.md           # name (= tên thư mục), description; tuỳ chọn disable-model-invocation, argument-hint
    reference.md       # tệp phụ, xuất bản cùng
    tests/*.yml        # kịch bản áp lực cho bint skill test (GĐ6), KHÔNG xuất bản
```

Flow tuỳ biến:

```yaml
# .bint/flows/tdd.yml
schema: 1
name: tdd                # = tên tệp; không trùng workflow gốc, không tiền tố b-
description: "Dùng khi muốn viết kiểm thử đỏ trước khi sửa mã"
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
```

Lint flow (AC-5): kind không giảm theo thứ tự plan → implement → review; 0–1 bước plan; ≥ 1 bước implement;
`optional` chỉ ở plan; `verify: false` không được ở bước implement cuối; `fix` trỏ tới bước implement; skill tồn
tại (gốc hoặc tuỳ biến hợp lệ); id duy nhất, không trùng bước giả `post-feat`/`verify`; role mặc định theo kind
(planner/implementer/reviewer). b-req, b-feat, b-review (một bước) vẫn là flow nhúng đặc biệt.

### 2. Giả định và mâu thuẫn

- **Vị trí flow**: yêu cầu chỉ nói skill ở `.bint/skills/`. Chọn `.bint/flows/{tên}.yml` để không trộn định
  nghĩa chạy với tài liệu xuất bản cho harness (mọi thư mục trong `.bint/skills/` trừ tên dành riêng là skill).
- **Ba kind cố định** đủ cho "custom flow ghép custom skill". Kind tuỳ ý cần cổng tuỳ ý, phá bất biến "done có
  bằng chứng" và máy trạng thái (created/planned/in_progress/in_review) mà TUI, `bint run next`, hookcore dựa vào.
- **Frontmatter cho phép** của skill tuỳ biến: `name`, `description`, `disable-model-invocation`, `argument-hint`.
  Không cho `allowed-tools`/`hooks`: nội dung đến từ repository nên không được tự cấp quyền công cụ (cùng tinh thần
  `CheckDraft` của skill tiến hoá và mục 7 "không mô phỏng khả năng"). `.agents/skills` vẫn chỉ giữ name/description.
- **Giới hạn mô tả**: tài liệu tham khảo dùng 1024 ký tự; skill tiến hoá đang dùng 20–400. Skill tuỳ biến theo
  1024 (AC-4); khuyến nghị mở đầu "Dùng khi…/Use when…" chỉ là cảnh báo.
- **Mâu thuẫn mã ↔ kế hoạch gateway**: docs/mcp-gateway-plan.md H6 ghi `config.Roles` là struct cố định không chứa
  được role tuỳ biến; `mcp.grants` đã nhận tên role tự do. Kế hoạch này giải H6 bằng map nội tuyến.
- **Ai viết skill/flow tuỳ biến**: là tệp repository, ranh giới là review Git như lớp phủ hiện nay; agent không
  duyệt ([k:01M746PJW8B5A8S1N5B3BEAPC0]). Agent (qua b-add-skill) soạn được, nội dung chỉ thành đề bài lượt sau.
  Tài liệu khuyến nghị thêm `.bint/skills/**`, `.bint/flows/**` vào `limits.protected_paths`; không đổi mặc định.
- Tri thức chưa duyệt [k:01M772E6Z8WN8SPECM8X8WQTPC] (snapshot role chưa chạy lưu model rỗng khi chuyển b-req →
  b-do) chạm `agentChoices`/`rolesForRun` sẽ sửa ở GĐ3–GĐ4: kiểm lại trước khi dựa vào, thêm hồi quy.

### 3. Lựa chọn và lý do

| Hướng | Ưu | Nhược | Chọn |
|---|---|---|---|
| A. Flow chỉ là skill điều phối (văn xuôi) trong harness | Rẻ | Không cổng chương trình, không `bint run`, không đo; trái mục 6.4 | Không |
| B. Runner riêng cho flow tuỳ biến, b-do giữ mã cứng | Không chạm b-do | Hai máy trạng thái lệch dần; sửa cổng phải làm hai lần | Không |
| C. Runner theo kind, b-do là flow nhúng | Một máy trạng thái, cổng dùng chung | Refactor rủi ro ở run.go/session.go | **Có** |
| D. Máy trạng thái do người dùng khai báo | Linh hoạt nhất | Phá bất biến done/verify, TUI, hook | Không |

C được chọn; refactor (GĐ3, không đổi hành vi) tách khỏi tính năng (GĐ4) để bộ test hiện có của
workflow/app/cli làm lưới an toàn. Skill tuỳ biến dùng lại `publish`/`unpublish`, không viết cơ chế xuất bản mới.

### 4. Giai đoạn (mỗi giai đoạn một phiếu b-do)

**GĐ1 — Skill tuỳ biến** (TSK-1..3). `internal/skills/custom.go` (mới): `LoadCustom(root)`, `CheckCustom`, tệp
phụ (chỉ tệp thường, không symlink, nằm trong thư mục skill, bỏ `tests/`, trần kích thước/số tệp); `Install`
thêm `installCustom` + `Lock.Custom`; `ValidName` thêm tên dành riêng `overlays`, `evolved` và kiểm trùng skill
tiến hoá. `internal/app` (skill svc, sync, doctor), `internal/cli/skill.go`: `skill new`, `skill lint`, list/show
có loại `custom`; hàng mới trong `internal/tui/actions/cliref.go` và `litsrUsage` ([k:01M78XP5NXWWRAZQHN5PSHNC13]).
Skill gốc `internal/skills/core/b-add-skill/SKILL.md` (`disable-model-invocation: true`, điểm neo `project-rules`):
khi nào nên/không nên tạo skill (quy ước dự án → tri thức/lớp phủ), description chỉ nêu điều kiện kích hoạt,
`bint skill new/lint`, RED/GREEN bằng subagent thủ công (GĐ6 thay bằng `bint skill test`), `bint sync`, không tự
commit/duyệt. Cập nhật `core.lock` của repo bằng `bint sync`.

**GĐ2 — Mô hình flow** (TSK-4). Gói mới `internal/flow`: `Def{Schema, Name, Description, Steps}`,
`Step{ID, Kind, Skill, Role, Optional, Verify *bool, Fix}`, `Load`, `Get`, `Builtin()` (nhúng b-do/b-req/b-feat/
b-review), `Validate(def, skillExists)`, `Hash()`; decode `KnownFields(true)`. CLI `bint flow list|show|lint`
(+ cliRefs). Chưa đổi Runner.

**GĐ3 — Runner theo kind, không đổi hành vi** (TSK-5..6). `workflow.RunOptions` mang `flow.Def`; `nextStep`,
`afterStep`, `beginStep`, `prompt`, `readonlySteps`, `stopOnPlanning`, `reviewDecision`, `session.go` (`next`,
`complete`, `PendingStep`) đổi `switch step` thành `switch kind`; `stepCheck` giữ. `app.workflowRoles`,
`roleStep`, `rolesForRun`, `harness.StepRole`, `harness.SubagentFiles` lấy từ flow nhúng. Id bước của flow nhúng
giữ đúng `b-req/b-feat/b-review` nên sự kiện spool, tên log, `limits.workflows.<bước>` không đổi. Đầu GĐ3 ghi
golden (trạng thái, phân loại, cờ → bước kế tiếp) từ mã cũ, sau refactor so khớp.

**GĐ4 — Chạy flow tuỳ biến** (TSK-7..8). Config: `Roles.Extra` và `RoleModels.Extra` kiểu
`map[string]string` thẻ `yaml:",inline"` (config cũ vẫn nạp; `For(role)` tra Extra); `Validate` kiểm tên role,
harness, cấm trùng trường cố định; `limits.workflows` nhận tên flow hợp lệ (tồn tại flow kiểm ở `bint doctor`);
mẫu `template.go` thêm dòng chú thích, nhớ `%%` ([k:01M746NPB0Y4D5S2D47EFTJ8T4]). Tác vụ: `State.Workflow` = tên
flow, thêm `State.FlowStep` (id bước hiện tại) và `State.FlowSHA` (băm định nghĩa lúc lập phiếu; flow đổi giữa
chừng ⇒ resume `blocked` kèm lý do). CLI/app: `run`, `task new --workflow`, `checkDepth` (`--depth` nhận flow có
bước plan), resume, `run next/complete`; subagent theo kind (`bint-planner/implementer/reviewer`, model của role
qua trường `model` của `next`); MCP grants theo role của bước. TUI màn Tác vụ chọn workflow từ danh sách flow.
Kiểm chứng: sau bước implement có `verify` (mặc định true) và luôn trước review đầu tiên/finalize.

**GĐ5 — Xuất bản flow + b-add-flow** (TSK-9..10). `skills.Install` nhận flow hợp lệ, sinh skill điều phối `{flow}`
từ mẫu nhúng (phần lập phiếu/chạy/xử lý blocked/báo cáo như b-do, gọi `bint task new --workflow {flow}` và
`bint run {flow} --task`), `disable-model-invocation: true`, mục `Lock.Flows`; flow xoá ⇒ `unpublish`. Skill gốc
`b-add-flow`: chọn bước/kind/role, skill thay bước plan phải ghi requirement/plan/tasks theo hợp đồng v1,
`bint flow lint`, `bint sync`, chạy thử trên phiếu nhỏ. Tài liệu: docs/bint-plan.md §6.4/§6.6/§7,
docs/workflows.md, README.

**GĐ6 — Kiểm thử skill** (TSK-11..12). `bint skill test {tên}`: kịch bản `tests/*.yml` (`prompt`, `pressures`,
`expect`: chuỗi/regex phải có hoặc không có trong câu trả lời, `reps` ≥ 5 theo "micro-test" của tài liệu tham
khảo); hai nhánh (có skill trong đề bài / không) chạy bằng `eval.Agent` theo role `skill-tester` (mặc định
planner), chỉ đọc, ước tính chi phí; không có terminal và không `--yes` thì dừng trước khi gọi agent. Kết quả JSON
vào `<state>/runtime/skill-tests/`. Cập nhật b-add-skill dùng lệnh này cho RED/GREEN.

### 5. Vùng ảnh hưởng

- Gói: `internal/skills`, `internal/flow` (mới), `internal/workflow` (run.go, session.go, ops.go, agent.go),
  `internal/app` (run_svc, task_svc, skill/sync/doctor svc, MCP theo role), `internal/cli` (skill, run, task,
  flow mới), `internal/harness` (StepRole, SubagentFiles), `internal/config` (Roles, RoleModels, limits,
  template), `internal/task` (State), `internal/tui` (màn Tác vụ, actions/cliref), `internal/eval` (GĐ6).
- Dữ liệu: `core.lock` thêm mục `custom`, `flows` (omitempty, lock cũ đọc được); state tác vụ thêm trường
  omitempty; config thêm khoá nội tuyến.
- Người dùng: không có `.bint/flows` và skill tuỳ biến thì hành vi không đổi. Skill gốc mới (`b-add-skill`,
  `b-add-flow`) xuất hiện trong harness sau `bint sync`.
- Hook: không thêm logic vào hook; hookcore chỉ biết tên subagent theo kind, không đổi ([k:01M746NPTW1B304MRQQPBCAFAM]).

### 6. Rủi ro và rollback

| Rủi ro | Phát hiện | Xử lý / rollback |
|---|---|---|
| Refactor GĐ3 đổi hành vi b-do (thứ tự bước, trạng thái, vòng review, session) | Bộ test workflow/app/cli + golden bước kế tiếp | GĐ3 là commit riêng không có tính năng mới: revert độc lập |
| Skill tuỳ biến trùng tên, ghi đè `.claude/skills/{tên}` của người dùng | Lint; `publish` báo xung đột khi tệp không do Bint ghi | Không ghi đè nếu thiếu `--force`; có sao lưu |
| Tệp phụ: symlink, đường dẫn thoát thư mục, tệp lớn | Kiểm khi nạp | Từ chối, báo xung đột, không chép |
| Skill/flow trong repo thành đề bài (tiêm chỉ dẫn lâu dài) | Review Git | Không cho khoá cấp quyền; khuyến nghị protected_paths; ranh giới tin cậy trong prompt giữ nguyên |
| Flow đổi khi tác vụ đang dở | `FlowSHA` khác lúc resume | `blocked` kèm lý do; người chọn chạy tiếp hay huỷ |
| Role nội tuyến va khoá cũ (`evolver`, `reflector`) | Test Decode/Validate | Validate từ chối tên trùng trường cố định |
| Windows: chép tệp, đường dẫn | `GOOS=windows go vet`, staticcheck ([k:01M746NASVKV9XMRHA4QNHJ3R8]) | Chỉ dùng `filepath`/`fsafe`, không syscall |
| Lệnh CLI mới thiếu cliRefs ⇒ test inventory đỏ ở cli | TestServiceMessageCLIInventory | Thêm hàng cùng commit |
| `bint skill test` tốn tiền ngoài ý | Ước tính + `--yes` | Không chạy khi không tương tác mà thiếu `--yes` |

### 7. Chiến lược kiểm thử

- GĐ1: unit `internal/skills` (nạp; mỗi lỗi của AC-4 một ca; xuất bản 2 target; tệp phụ; xung đột sửa tay; gỡ khi
  xoá; lock cũ không có `custom`); e2e cli `skill new` → `skill lint` sạch → `sync`; bảng skill gốc trong
  `skills_test.go` có `b-add-skill`.
- GĐ2: bảng lint flow (mỗi lỗi AC-5 một ca + ca hợp lệ); flow nhúng qua lint; decode từ chối khoá lạ.
- GĐ3: golden bước kế tiếp trước/sau; nguyên bộ test hiện có qua, không sửa kỳ vọng (AC-6).
- GĐ4: e2e với `--agent-command` giả (mẫu trong `workflow_e2e_test.go`): flow 4 bước như ví dụ, kiểm thứ tự bước,
  bước chỉ đọc bị chặn ghi, kiểm phạm vi sau `red`, verify không chạy sau `red` nhưng chạy sau `green`, vòng sửa
  về `green`, resume, đổi flow giữa chừng ⇒ blocked; session `next/complete`; config cũ nạp được +
  TestRenderMatchesDefaults. Test đóng vai người gọi `asHuman(t)` khi chạm thao tác chỉ-người.
- GĐ5: xuất bản/gỡ skill điều phối; nội dung có `bint run {flow}`.
- GĐ6: agent giả trả lời cố định cho hai nhánh, kiểm tỉ lệ; thiếu `--yes` và stdin không phải terminal ⇒ không gọi agent.
- Mỗi giai đoạn: `go test ./...`, `GOOS=windows go vet ./...`, staticcheck cả hai GOOS.
- Chưa kiểm được bằng test: chất lượng thật của b-add-skill/b-add-flow và skill điều phối với harness thật, chi
  phí thật của `bint skill test`; cần một lượt nghiệm thu thủ công sau GĐ5 và GĐ6.

### 8. Checklist nhất quán

- AC-1 → TSK-3, TSK-12; AC-2 → TSK-10; AC-3 → TSK-1; AC-4 → TSK-2; AC-5 → TSK-4; AC-6 → TSK-5, TSK-6;
  AC-7 → TSK-8; AC-8 → TSK-7; AC-9 → TSK-9; AC-10 → TSK-11; AC-11 → mọi TSK.
- Mỗi TSK có điều kiện "xong khi" đo bằng test hoặc lệnh CLI.
- `--allow` của phiếu phủ mọi tệp trong mục 4–5 (internal/skills, workflow, config, app, cli, harness, task, tui,
  eval, `.bint/skills/**`, `.bint/flows/**`, docs, README). Lập phiếu từng giai đoạn thì thu hẹp `--allow` theo mục 4
  (GĐ1 cần thêm `internal/skills/core/**` đã nằm trong `internal/skills/**`).

## Đầu việc

Mỗi giai đoạn (GĐ) lập một phiếu b-do riêng, tiêu chí của phiếu đó lấy từ các AC ghi trong ngoặc.

### GĐ1 — Skill tuỳ biến

- [x] TSK-1 [AC-3, AC-11]: internal/skills: nạp skill tuỳ biến từ .bint/skills/{tên}/ (bỏ overlays, evolved, core.lock), tệp phụ an toàn (không symlink, không thoát thư mục, bỏ tests/), installCustom xuất bản sang mọi target và gỡ khi xoá, mục custom trong core.lock — xong khi: test mới trong internal/skills chứng minh xuất bản 2 target kèm tệp phụ, xung đột khi tệp đã xuất bản bị sửa tay, gỡ khi skill bị xoá, đọc được core.lock cũ; go test ./... và GOOS=windows go vet ./... qua
- [x] TSK-2 [AC-4, AC-11]: bint skill new và bint skill lint (CheckCustom), skill list/show hiện loại custom, hàng cliRefs và litsrUsage cho lệnh mới — xong khi: bảng test có một ca cho mỗi lỗi nêu trong AC-4 và một ca hợp lệ, e2e skill new rồi skill lint thoát 0, TestServiceMessageCLIInventory qua
- [x] TSK-3 [AC-1, AC-11]: skill gốc internal/skills/core/b-add-skill/SKILL.md theo phương pháp của docs/references/superpowers-writing-skills.md (khi nào tạo skill, description chỉ nêu điều kiện kích hoạt, new/lint, RED/GREEN, sync, không tự duyệt), chạy bint sync cập nhật core.lock — xong khi: bint skill list có b-add-skill, bint sync xuất bản nó vào .claude/skills và .agents/skills, test bảng skill gốc trong skills_test.go qua

### GĐ2 — Mô hình flow

- [x] TSK-4 [AC-5, AC-11]: gói internal/flow (Def, Step, Load, Get, Builtin cho b-do/b-req/b-feat/b-review, Validate, Hash; decode KnownFields) và lệnh bint flow list/show/lint kèm cliRefs — xong khi: bảng test lint có một ca cho mỗi lỗi nêu trong AC-5 cộng ca hợp lệ, bốn flow nhúng qua Validate, bint flow lint trên .bint/flows/tdd.yml mẫu trong test thoát 0

### GĐ3 — Runner theo kind (không đổi hành vi)

- [ ] TSK-5 [AC-6, AC-11]: ghi golden bảng (trạng thái, phân loại, cờ) → bước kế tiếp từ mã cũ, rồi đổi nextStep/afterStep/beginStep/prompt/readonlySteps/stopOnPlanning/reviewDecision của driver process sang switch theo kind của flow.Def — xong khi: golden khớp, toàn bộ test internal/workflow qua mà không sửa kỳ vọng
- [ ] TSK-6 [AC-6, AC-11]: chuyển session.go (next/complete/PendingStep), app.workflowRoles/roleStep/rolesForRun, harness.StepRole và harness.SubagentFiles sang đọc flow nhúng; thêm hồi quy cho snapshot role khi chuyển b-req sang b-do — xong khi: test internal/app, internal/cli, internal/harness qua mà không sửa kỳ vọng và go test ./... qua

### GĐ4 — Chạy flow tuỳ biến

- [ ] TSK-7 [AC-8, AC-11]: config Roles.Extra và RoleModels.Extra (yaml inline), Validate tên role và harness, limits.workflows nhận tên flow, dòng chú thích trong template.go, doctor kiểm flow tồn tại; MCP grants tra theo role của bước — xong khi: test decode config cũ không có khoá mới qua, test role tuỳ biến lấy đúng harness/model/grants, TestRenderMatchesDefaults qua
- [ ] TSK-8 [AC-7, AC-11]: bint run {flow}, task new --workflow {flow}, resume, run next/complete, State.FlowStep và State.FlowSHA, verify theo cờ của bước, TUI màn Tác vụ chọn flow — xong khi: e2e với agent giả trên flow 4 bước (plan, red verify false, green, review fix green) xác nhận thứ tự bước, bước chỉ đọc bị chặn ghi, kiểm phạm vi sau red, kiểm chứng không chạy sau red mà chạy sau green, vòng sửa về green, flow đổi giữa chừng làm resume blocked, và cùng kịch bản qua driver session

### GĐ5 — Xuất bản flow và b-add-flow

- [ ] TSK-9 [AC-9, AC-11]: bint sync sinh skill điều phối cùng tên cho mỗi flow hợp lệ từ mẫu nhúng, mục flows trong core.lock, gỡ khi flow bị xoá — xong khi: test xuất bản có nội dung bint run tdd, test gỡ khi xoá flow và giữ bản bị sửa tay
- [ ] TSK-10 [AC-2, AC-11]: skill gốc internal/skills/core/b-add-flow/SKILL.md (chọn bước, kind, role, hợp đồng v1 cho bước plan, flow lint, sync, chạy thử), cập nhật docs/bint-plan.md mục 6.4, 6.6, 7, docs/workflows.md và README — xong khi: bint skill list có b-add-flow, bint sync xuất bản nó, test bảng skill gốc qua

### GĐ6 — Kiểm thử skill

- [ ] TSK-11 [AC-10, AC-11]: bint skill test {tên}: định dạng tests/*.yml, hai nhánh có và không có skill qua eval.Agent theo role skill-tester, ghi JSON kết quả vào thư mục trạng thái, ước tính chi phí, yêu cầu --yes khi không tương tác — xong khi: test với agent giả ghi đúng tỉ lệ tuân thủ của hai nhánh, test thiếu --yes khi stdin không phải terminal xác nhận agent không được gọi
- [ ] TSK-12 [AC-1, AC-11]: cập nhật b-add-skill dùng bint skill test cho bước RED/GREEN và nhắc kịch bản đặt trong tests/ — xong khi: nội dung b-add-skill nêu bint skill test, bint sync xuất bản bản mới, go test ./... và GOOS=windows go vet ./... qua
