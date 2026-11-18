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
bint run resume <task-id> [--rerun]             # tiếp tục tác vụ blocked / interrupted / failed; --rerun: bỏ kết quả bước chỉ đọc đang giữ, chạy lại bước

bint task new "<mô tả tự do>"                   # Classify: lập phiếu tác vụ (mục 6.7)
bint task list [--all]
bint task show <id>
bint task set <id> --verify-command "go test ./internal/db/..." \
  --criteria "Kiểm thử cạnh tranh SQLite không còn lỗi database is locked" \
  --allow "internal/db/**"
bint task transitions                           # bảng chuyển trạng thái
bint task write <id> requirement|plan|tasks -   # ghi tài liệu qua stdin
bint task diff <id> [--stat|--name-only] [--all]  # thay đổi của RIÊNG tác vụ (shared: chỉ tệp Touched, tệp khác nêu tên ở ghi chú, --all để xem hết)
bint task new --worktree|--shared "<mô tả>"     # chọn nơi chạy lúc lập phiếu (mặc định theo workflow.isolation)
bint task land <id> [--3way] [--keep]           # tác vụ worktree: đưa thay đổi về cây gốc (chưa commit); --3way chỉ NGƯỜI
bint task land <id> --force --reason "<lý do>"  # chỉ NGƯỜI: bỏ qua điều chặn về bằng chứng (kiểm chứng không qua/cũ, tiêu chí chưa đối chiếu)
bint task cancel <id> [--reason …] [--discard]  # huỷ; tác vụ worktree: gỡ worktree (--discard chỉ NGƯỜI nếu còn thay đổi chưa đưa về)
bint task new --driver session "<mô tả>"       # driver session: phiên đang mở chạy bước bằng subagent (bint run next/complete/abort)
bint task lint <id> [--strict] [--json]         # kiểm ba tài liệu b-req theo hợp đồng v1 (AC↔TSK, câu hỏi mở)
bint task verify <id>                           # chạy lệnh kiểm chứng (chỉ khi người đã xác nhận)
bint task criteria <id> <n> met|unmet -e "…"    # đối chiếu tiêu chí chấp nhận
bint task error <id> --step b-feat "…"          # ghi lỗi / lần thử thất bại (--question "…": câu hỏi gửi người, xem dưới)
bint task review <id> -                         # vòng review (JSON hoặc YAML) → review.json + review.md
bint task approve <id> [--path <tệp>] [--setup-sha <băm>]  # NGƯỜI: xác nhận lệnh kiểm chứng, lệnh setup worktree (đúng băm đã xem), duyệt đường dẫn bảo vệ
bint task status <id> done [--force]            # done cần bằng chứng; chỉ người --force được
bint task done <id> [--force] [-m "…"]          # bí danh của bint task status <id> done

bint skill list
bint skill show <tên> [--core]
bint skill overlay <tên>
bint skill new <tên>                            # skill tuỳ biến: tạo .bint/skills/<tên>/SKILL.md mẫu (không ghi đè)
bint skill lint [tên]                           # kiểm hình thức skill tuỳ biến; thoát 1 khi có lỗi (--json)
bint flow list|show|lint                        # flow nhúng và flow tuỳ biến (.bint/flows): xem, kiểm; chạy bằng bint run <flow> --task, bint sync xuất bản /<flow>
bint sync                                       # ghép skill gốc + lớp phủ, xuất bản, cập nhật core.lock
bint sync --allow-downgrade                     # ép ghi skill khi binary cũ hơn core.lock (mặc định bị chặn)
bint knowledge add --from-review <task-id>.<n>  # phát hiện review → mục warning/lesson
```

**Tác vụ** nằm ở `<state>/runtime/tasks/<task-id>/` (ngoài repository): `task.yml` (phiếu: `goal`,
`verify.command`/`criteria`, `scope.allow`/`deny`, `constraints`, `autonomy`; sửa được bằng tay),
`state.json` (do Bint ghi qua khoá thư mục: trạng thái, lịch sử chuyển, kiểm chứng, tiêu chí, duyệt,
lỗi, lượt chạy), `requirement.md`, `plan.md`, `tasks.md`, `review.json`/`review.md`, `logs/`.
`--criteria`, `--allow`, `--deny`, `--constraint` trong `task set` nối thêm vào danh sách sẵn có;
dùng `--clear-criteria` và `--clear-scope` để xoá trước khi đặt lại. Đổi `verify.command` làm lần xác
nhận lệnh cũ hết hiệu lực. Khi tác vụ đã có `requirement.md`, đổi danh sách tiêu chí (`task set --criteria` /
`--clear-criteria`, hoặc sửa phiếu qua `bint run … --criteria`) tự đồng bộ sang artifact: Bint viết lại
các dòng AC cho khớp phiếu (AC mới thêm nguyên văn, số tiếp theo; AC thừa bị bỏ), thêm vào `tasks.md` một
TSK cho mỗi AC chưa có TSK bao phủ, và in ghi chú những gì đã đổi — `bint task lint` qua ngay mà không
cần `bint task write`. TSK còn tham chiếu AC đã bỏ (số lớn hơn số tiêu chí mới, khi thu gọn): nếu TSK còn AC hợp lệ khác thì Bint gỡ AC đã bỏ khỏi ngoặc và ghi chú; nếu TSK chỉ còn AC đã bỏ thì giữ nguyên dòng, ghi chú nêu TSK, AC và lệnh sửa (`bint task write <id> tasks`), và `bint task lint` báo `unknown_ac` đúng dòng đó.

**Classify** xác định được (heuristic, chưa gọi mô hình): mục tiêu = dòng đầu mô tả; `scope.allow` =
đường dẫn và mẫu glob nêu trong mô tả mà tồn tại; `verify.command` = `eval.test_command` đã xác nhận;
đề xuất bỏ `b-req` cho việc nhỏ hoặc việc ngắn mà **rõ** (xem dưới). Tệp tài liệu (`.md`, `.txt`…) mà mô tả coi là kế hoạch/đặc tả
cần làm theo ("thực hiện plan docs/x.md") vào `references` thay vì `scope.allow` và kéo theo `b-req`.
`task new` có `--clear-scope` để bỏ phạm vi tự đoán, chỉ giữ `--allow`/`--deny`.

Yêu cầu ngắn (không quá 25 từ) chỉ được bỏ `b-req` khi nêu được điều kiểm được: tệp/đường dẫn, định danh mã
(`camelCase`, `snake_case`, `a.b`, `hàm()`), cờ `--x`, chuỗi trích dẫn, mã lỗi/HTTP, hành vi kỹ thuật cụ thể (`nil`, `panic`,
`timeout`…) hoặc giá trị đo được ("30 ngày", "500ms"); lý do ghi "yêu cầu ngắn, rõ: bỏ b-req". Yêu cầu ngắn mà **mơ hồ** — không
nêu tệp, hành vi hay tiêu chí cụ thể ("Thư mục dữ liệu cứ phình to, làm cho nó tự dọn bớt mấy thứ cũ đi", "Tôi sợ mật khẩu bị lưu lại
trong log") — đi qua `b-req` (lý do "yêu cầu ngắn nhưng chưa nêu tệp, hành vi hay tiêu chí cụ thể: cần b-req"), vì chọn phạm vi ở đó
mới đo được mục đích của người dùng. Việc nhỏ (chính tả/đổi tên/định dạng) vẫn bỏ `b-req` như cũ.

Heuristic chỉ là gợi ý: trong `/b-do`, orchestrator tự đánh giá yêu cầu (đọc cả tài liệu được nêu) và
truyền `--plan`/`--no-plan`/`--depth` tường minh. Khi phạm vi chạm đường dẫn bảo vệ
(`Assessment.Risk=high`), Bint tự bật `b-req`; chỉ **người** bỏ được bằng `--no-plan`, còn `--no-plan`
từ phiên agent bị bỏ qua và lý do ghi vào `classification.reason`.

**Hợp đồng artifact của `b-req` (v1).** Đầu ra của `b-req` có khuôn kiểm được bằng máy:
- `requirement.md`: dưới `## Tiêu chí chấp nhận` có các dòng `- [ ] AC-N: …` đánh số liên tục từ 1,
  khớp từng tiêu chí tương ứng trong `task.yml` (sau khi chuẩn hoá khoảng trắng).
- `tasks.md`: mỗi đầu việc dạng `- [ ] TSK-N [AC-N]: <việc> — xong khi: <điều kiện>`, ID duy nhất; mọi
  AC phải được ít nhất một TSK bao phủ.
- `plan.md`: không rỗng. Dòng AC/TSK không được chứa placeholder (`TODO`, `TBD`, `…`, `<mô tả>` đứng riêng), và nội
  dung trong khối code fence không được tính là AC/TSK. Hai ngoại lệ: nội dung trong code span
  (`` `<x>` ``) và đoạn `<…>` là một thành phần của định danh có dấu chấm (`limits.workflows.<flow>`,
  `harnesses.<h>.model`).
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
**Tự kiểm chứng lại ở Finalize** (NT-WF-BASELINE): khi người hoặc tác vụ khác commit/land vào cùng cây giữa lúc tác vụ
chạy, fingerprint mã đổi sau lần kiểm chứng cuối và `done` bị chặn ("thay đổi không thuộc tác vụ: …"). Nếu **chỉ** vì
thế (lệnh kiểm chứng đã xác nhận, lần chạy cuối qua, mọi tệp đổi kể từ đó đều ngoài `touched` của tác vụ, và nếu coi
lần kiểm chứng là còn mới thì không còn điều chặn nào khác) thì Finalize (cả driver process lẫn session) tự chạy lại
lệnh kiểm chứng đúng một lần dưới khoá độc quyền, ghi một `verification` mới có `reason` "mã khác đổi sau lần kiểm
chứng: <tệp>", rồi xét `done` lại. Chạy lại không qua thì tác vụ `blocked` kèm mã thoát, tệp và log. Không tự chạy khi
lệnh chưa được người xác nhận, khi có tệp thuộc `touched` đổi, hoặc khi còn lý do chặn khác (đường dẫn bảo vệ chưa duyệt,
tiêu chí `unmet`, xung đột đưa về…). `bint task status <id> done` do người/agent gọi không tự chạy lệnh nào.
**`land --force --reason` và lối thoát khi kiểm chứng hỏng vì lý do ngoài mã.** `land` đòi lần kiểm chứng cuối qua và mới, còn
`status done --force` không bỏ qua việc chưa đưa về, nên tác vụ `worktree` có kiểm chứng hỏng ngoài mã (mạng, đĩa đầy, máy
treo) trước đây không chốt được. Người (không phải agent: `ErrAgentForbidden`) chạy `bint task land <id> --force --reason
"<lý do>"` để bỏ qua **chỉ** điều chặn về bằng chứng: lần kiểm chứng cuối không qua/cũ/chưa chạy/lệnh đổi, tiêu chí chưa
đối chiếu, phiếu không có bằng chứng nào. Danh sách cho phép này phân loại theo loại điều chặn (`task.BlockerKind.Forceable`,
không so chuỗi); loại điều chặn thêm sau này mặc định là cứng. KHÔNG bỏ qua: tiêu chí đã đánh dấu chưa đạt (`unmet`), lệnh
kiểm chứng chưa được người xác nhận, thay đổi chạm đường dẫn bảo vệ hoặc `scope.deny` chưa duyệt, phiếu chỉ lập kế hoạch có
thay đổi lạ, xung đột khi áp. `--force` thiếu `--reason` (hoặc `--reason` thiếu `--force`) là lỗi cách dùng. Lý do và danh
sách điều chặn đã bỏ qua nằm ở `landed.forced/force_reason/bypassed` của phiếu, ở payload sự kiện `task.status`
(`forced`, `force_reason`, `bypassed`) và ở `bint task show` ("ÉP (--force): …"). Sau đó chốt bằng `bint task status <id>
done --force`. Ở TUI: màn Tác vụ, `o`, ô "Ép đưa về" và "Lý do".

**Resume sau khi finalize bị chặn không review lại.** Khi review đã duyệt (không có phát hiện nghiêm trọng) nhưng finalize
bị chặn vì thiếu bằng chứng (thường là kiểm chứng cuối không qua), phiếu nhớ dấu vân tay worktree lúc chặn
(`State.finalize_fp`). `bint run next <id> --resume` (driver session) và `bint run resume <id>` (driver process) mà
worktree không đổi kể từ lúc đó thì KHÔNG mở lượt b-review mới: chạy lại lệnh kiểm chứng đã xác nhận đúng một lần (ghi
`verification` có `reason` "tiếp tục sau khi finalize bị chặn…") rồi áp cổng `done`. Worktree đã đổi, hoặc `--rerun`, thì
review lại như cũ. Dấu vân tay bị xoá khi lần kiểm chứng chạy lại xong hoặc khi tác vụ bị chặn vì lý do khác lần kiểm
chứng hỏng (ví dụ tiêu chí `unmet`), để lần resume sau không bỏ review nhầm.

**Kiểm chứng chạy qua Bint trên Windows chậm hoặc treo (tác vụ 01M7C3CBN5JKTMVJV97TXDR5DK, 2026-11-14).** Trạng thái: **nguyên nhân đã được xác nhận bằng phép đo** (2026-11-14, Go 1.25.1, phiếu 01M7CZR838A68TT47THCCPWH9A); các phần
đo và giả thuyết cũ bên dưới giữ làm lịch sử.

*Nguyên nhân (test cache băm testlog).* Khi có test cache, trước khi chạy mỗi gói `go.exe` nạp testlog của lần chạy trước
(danh sách `getenv`/`stat`/`open` do tiến trình test ghi) và băm từng dòng trong `computeTestInputsID` để biết kết quả cũ còn
dùng được không. Đường dẫn ngoài module đi qua `search.InDir` và bị `filepath.EvalSymlinks` hai lần; trên Windows bước này tốn rất
nhiều thao tác hệ thống tệp mà không đọc tệp nào, nên `go.exe` ~2 nhân, ~1,3 GB, không có tiến trình con, không in gì cho tới
khi xong gói (đo riêng bước băm: 56 nghìn dòng ≈ 17 s mỗi lượt, mỗi gói 2–3 lượt). Mỗi `exec.Command("git")` gọi `exec.LookPath`,
sinh (số thư mục PATH × số đuôi PATHEXT) dòng `stat` kèm `open NUL` vào testlog; testlog của `internal/tui/app` có ~477 nghìn
dòng (~7000 lần dò git), `internal/app` ~357 nghìn, `internal/evolve` ~101 nghìn, `internal/eval` ~68 nghìn,
`internal/tui/actions` ~63 nghìn.

*Vì sao `go test -count=1` không tái hiện:* `-count=1` tắt test cache nên `go.exe` không nạp và không băm testlog; đó là lý do lần
đo trước (phiếu 01M7CVBF84DKPP40QJ392H8MQS, dùng `-count=1`) không thấy lỗi. **Giải pháp tạm** của người dùng: `go env -w
GOFLAGS=-count=1` (biến môi trường `GOFLAGS` rỗng không ghi đè được `go env -w`; muốn bỏ qua cho một lệnh thì đặt
`GOFLAGS=" "`). Đo testlog: `go test -c -o x.test.exe ./pkg` rồi chạy `x.test.exe -test.testlogfile=<tệp>` trong thư mục gói.

*Gỡ giải pháp tạm (2026-11-15).* Sau bản sửa bên dưới, `go env -u GOFLAGS` đã bật lại test cache; mục tri thức
01M7D6WA84MDN32JHM21VZWQ5H đã bị vô hiệu. Đo cùng máy: testlog của `internal/tui/app` còn 167.600 dòng (12.816 dòng khác nhau;
~72 nghìn dòng `stat`/`open` là tệp tạm dưới `%TEMP%`, ~54 nghìn `getenv`, chủ yếu PATHEXT/USERPROFILE/PATH);
`go test ./internal/tui/app` chạy thật 327 s, lần sau trả `(cached)` trong 19 s. `eval.test_command` vẫn giữ `-count=1`: lần
kiểm chứng full không dựa vào cache (cache không theo dõi tệp mà tiến trình con như git đọc).

*Thư mục tạm của lệnh kiểm chứng trên Windows (tác vụ 01M7NWDTRN29QFP1AFAKGB87XS, 2026-11-18).* Unix dùng
`<state>/tmp/verify-*`; Windows dùng `%TEMP%\bv\<số>` (`verifyTempBase`, `internal/workflow/ops.go`): đường dẫn dưới thư mục
trạng thái (~80 ký tự) làm git trong test eval/evolve vượt MAX_PATH ("Filename too long", lượt "lỗi hạ tầng"). TMPDIR/TMP/TEMP
vẫn truyền dạng Windows; `sh.exe` (MSYS) chỉ in TMPDIR dạng `/c/...` hoặc `/tmp/...`, còn tiến trình Go con nhận đúng đường dẫn
Windows (`cygpath -w` đổi lại trong test; hồi quy `TestVerifyChildProcessTempDir`). Thư mục `bv\*` sót lại khi bint bị giết không
có GC riêng.

*Test cache không dùng chung giữa các worktree.* Khoá cache của bước biên dịch chứa thư mục gói (trừ khi `-trimpath`), mà
worktree của mỗi tác vụ có đường dẫn riêng, nên lần kiểm chứng đầu của mỗi tác vụ chạy thật mọi gói; từ lần thứ hai trong cùng
worktree mới được cache. Với `-trimpath`, gói thuần (`ulid`, `config`) dùng chung được, nhưng gói mở tệp theo đường dẫn
tương đối hoặc phụ thuộc thư mục (`gitrepo`, `knowledge`) vẫn chạy lại, và hai worktree ghi đè kết quả cache của nhau.

*Sửa trong Bint:* mọi lệnh git của mã không phải test đi qua `gitrepo.Command`/`gitrepo.CommandContext`, nhớ đường dẫn git theo
(PATH, PATHEXT) và dò lại khi một trong hai đổi (`internal/gitrepo/command.go`; kiểm thử `TestLookGitCachesPerPath`);
`TestNoDirectGitExec` quét từ gốc module (gồm `cmd/`), nhận diện gói `os/exec` qua import (kể cả import đổi tên) và chặn mã
ngoài helper gọi `exec.Command("git")`; `testutil.Git` cũng dùng helper. `gitrepo.LookPathCached` dùng chung cho `taskkill`
(`internal/eval/proc_windows.go`, `internal/harness/codex/process_windows.go`), chỉ nhớ khi dò được và đường dẫn tuyệt đối.
`repoWriter.record` giải thư mục cha trùng nhau một lần (`state.NormalizeWritePaths`). Không còn đường tắt `pathutil.Canonical`
dưới `%TEMP%` (đã hoàn tác; `internal/pathutil` không đổi). Thêm hai giảm dò PATH trong kiểm thử: `testutil.Isolate` dùng
thư mục harness giả **riêng của tiến trình kiểm thử** (`os.MkdirTemp` một lần qua `sync.Once`, dọn bằng `testutil.Main` /
`testutil.CleanupHarness` trong `TestMain`; gói không có `TestMain` để lại thư mục, bị quét sau 6 giờ) nên PATH không đổi giữa
các kiểm thử và bộ nhớ đệm `LookPath` trúng; `detect.lookPath` nhớ kết quả tìm thấy 10 phút (không thấy: 2 giây), khoá đã gồm
PATH/PATHEXT, và gói `detect` không import `testing`.

*Số đo testlog (dòng `stat`, binary test chạy trong thư mục gói bằng `-test.testlogfile`, cùng máy; "sau" đo 2026-11-15 sau khi
hoàn tác `pathutil`):*

| gói | trước: tổng dòng / stat | sau: tổng dòng / stat | stat của `git` trước → sau |
|---|---|---|---|
| `internal/tui/app` | 383.861 / 287.641 | 151.478 / 63.267 | 141.718 → 3.525 |
| `internal/app` | 311.097 / 223.884 | 186.591 / 105.570 | 100.648 → 3.503 |

Tổng dòng `stat` của `internal/tui/app` giảm **4,5 lần** (đạt đích 3 lần; đích ban đầu 10 lần đã được người quyết định hạ xuống 3
lần ngày 2026-11-14); `internal/app` giảm **2,1 lần** (tiêu chí chỉ đòi giảm và ghi số đo). Muốn đo lại: `go test -c -o x.test.exe
./internal/<gói>` rồi chạy `x.test.exe -test.testlogfile=<tệp>` trong thư mục gói.

*Đường chạy:* `eval.RunShell` → `sh.exe -c` → `go.exe test ./...`, stdout/stderr ghi vào `verify-N.log` (append), stdin rỗng.

*Phép đo đã làm* (cùng máy, `go test -count=1 ./internal/task/ ./internal/eval/`, cả hai gói chạy thật, không cached): gõ
tay 64,4 s; qua `sh -c` với stdin `/dev/null` và đầu ra qua pipe 61,7 s. Với hai gói này, `sh.exe`/pipe/stdin của RunShell
không thêm độ trễ đo được. Chưa đo toàn bộ `./...` qua Bint.

*Log thật của tác vụ nguồn* (`logs/verify-1..3.log`): `verify-1.log` có phần lớn gói `(cached)`, dừng sau
`internal/tui/actions` (gói kế tiếp, `internal/tui/app`, không in gì vì `go test` chỉ in kết quả gói khi gói xong);
`verify-2.log` chỉ có dòng `bint/cmd/bint [no test files]`; `verify-3.log` (sau `go clean -testcache`, không còn gói cached)
có kết quả đầy đủ của phần lớn gói nhưng `internal/cli` 209 s, `internal/eval` 78 s, `internal/evolve` 106 s,
`internal/tui/app` 338 s, và phiếu ghi `timed_out: true`. Cũng trong log đó: `chdir …\worktrees\<id>\internal\app: The
system cannot find the file specified` và golden `testdata\golden\…` "không tìm thấy đường dẫn" ở `internal/tui/app`, tức
thư mục worktree biến mất giữa lúc chạy (bị dọn/đổi trong lúc kiểm chứng) nên các gói đó FAIL vì lý do ngoài mã. Đó là bằng
chứng có thật nhưng không giải thích triệu chứng `go.exe` ~160 nghìn thao tác tệp/giây, ~1,3 GB RAM, không có tiến trình
con của `verify-1`.

*Khác biệt đã biết giữa chạy tay và qua Bint* là môi trường: lệnh kiểm chứng trước đây kế thừa nguyên môi trường của tiến
trình `bint` (đang trong lượt điều phối), gồm `BINT_RUN_ID`, `BINT_TASK_ID`, `BINT_APPROVED_PATHS`, `BINT_RUNNER_PID`,
`CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_SSE_PORT` và `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE`/`GIT_COMMON_DIR` (nếu có).
Kiểm thử của Bint dựng repo tạm và gọi lại `bint`/`git` nên có thể nhận nhầm là nằm trong lượt/tác vụ của phiên chủ hoặc đọc
nhầm kho git của worktree. Đây là giả thuyết, chưa được chứng minh là nguyên nhân của việc chậm hay treo. Bản sửa phòng
ngừa: `Env.VerifyWith` dùng `eval.VerifyEnv()` — bỏ các biến trên, GIỮ `BINT_AGENT`, `CLAUDECODE`, `AI_AGENT`,
`CODEX_SANDBOX`, `GEMINI_CLI` (kiểm thử thao tác chỉ dành cho người vẫn thấy mình ở trong agent). Hồi quy:
`TestVerifyEnvStripsRunIdentityKeepsAgentMarkers` (eval) và `TestVerifyWithEnvironment` (workflow). Không tái hiện được
triệu chứng trong phiên điều tra vì đặt `GIT_DIR` trỏ vào kho thật có nguy cơ làm hỏng kho.

*Nếu sau bản sửa vẫn treo, đo theo thứ tự:* (1) chạy cùng lệnh qua `bint task verify` và tay với `GOFLAGS=-p=1`, so thời gian
từng gói (`go test -json`); (2) thử loại trừ thư mục worktree và `%LOCALAPPDATA%\go-build` khỏi Microsoft Defender (giả thuyết,
chưa đo: quét thời gian thực tệp thực thi kiểm thử, nhất là sau `go clean -testcache`); (3) đảm bảo không dọn worktree trong
lúc kiểm chứng đang chạy. Mỗi lần chạy có tệp riêng `verify-<n>.log` theo thứ tự lần kiểm chứng (xem `verifications[].log`);
việc đối chiếu tệp log "mất" với `verifications[].log` của tác vụ nguồn chưa được làm, nên chưa kết luận gì về nó.

*Mã thoát 127* là của `sh`: "command not found". Hai nguyên nhân thường gặp trên Windows: `go` (hoặc công cụ khác) không có
trong `PATH` của `sh.exe`, và đường dẫn Windows trần có dấu `\` trong lệnh (ví dụ `sh C:\Users\…\agent.sh`) bị `sh -c` nuốt
dấu gạch chéo ngược nên không tìm thấy tệp; viết đường dẫn bằng `/` (kiểm thử e2e dùng `filepath.ToSlash`). `verify-1` và
`verify-2` của tác vụ nguồn đều thoát 127 nhưng log không có dòng lỗi của `sh`; nguyên nhân cụ thể của 127 ở hai lượt đó
chưa được xác nhận.

Với tác vụ `shared`, `bint task diff <id>` mặc định chỉ hiện tệp thuộc `touched`; tệp khác đổi vào cây sau mốc đầu được
loại, ghi chú nêu số và tên chúng (`--all` hiện lại toàn bộ). Tác vụ `worktree` luôn hiện toàn bộ phạm vi (worktree
không có người viết khác, và mọi tệp đổi trong đó đều được đưa về cây gốc). Lời nhắc tiếp tục trong lý do chặn theo driver: tác vụ session là
`bint run next <id> --resume`, tác vụ process là `bint run resume <id>`.
Phiếu **chỉ lập kế hoạch** (workflow mà flow không có bước ghi, như `b-req`; đang `planned`, chưa từng `in_progress`,
không có `touched`/đường dẫn chờ duyệt) đóng được bằng `bint task status <id> done --reason "…"` mà không cần kiểm chứng,
đối chiếu tiêu chí hay duyệt đường dẫn bảo vệ (nó không có mã; thay đổi của người khác trên cây không bị gán cho nó);
chỉ còn điều kiện "đã đưa về" nếu phiếu ở worktree riêng. Ở worktree riêng, phiếu chỉ được hưởng cổng nhẹ này khi diff
của worktree so với commit gốc rỗng hoặc chỉ gồm mục tri thức `candidate` mới dưới `.bint/knowledge/items/` (cùng quy tắc
miễn trừ của bước chỉ đọc); có thay đổi nào khác (kể cả tệp mã trong `scope.allow`) thì `bint task land` và `done` áp
cổng đầy đủ (kiểm chứng, tiêu chí, phạm vi, đường dẫn bảo vệ) và blocker nêu tệp đó — phiếu chỉ lập kế hoạch không đưa mã
về cây gốc: chạy `b-feat`/`b-do` hoặc người huỷ bằng `cancel --discard`. Người ép `done --force` thì lịch sử ghi chú
thích "người xác nhận dù thiếu bằng chứng" cùng các điều chặn đã bỏ qua. Phiếu có bước `b-feat` giữ nguyên cổng done và
vẫn không chuyển `planned → done` được.

**Hai tác vụ không cùng ghi một cây chung.** Phiếu `b-req` đang `planned` ở cây chung (isolation chọn bằng `auto`) không
được tính khi lập phiếu mới (nó không ghi cây). Nếu sau đó nó được chạy tiếp bằng `b-do`/`b-feat`/flow tuỳ biến (hoặc
driver session) trong lúc cây chung còn tác vụ shared khác chưa kết thúc, Bint dừng nó ở `blocked` trước bước ghi đầu
tiên, lý do nêu ID tác vụ kia và hai lối ra: chờ tác vụ đó xong rồi `bint run resume <id>`, hoặc huỷ phiếu
(`bint task cancel <id>`) rồi lập lại với `--worktree`. Isolation do người chọn (`--shared`, `workflow.isolation`) không bị
chặn; tác vụ khác đang `blocked` mà chưa từng ghi (do chính cổng này giữ) không tính, để hai phiếu không chặn nhau mãi.

**Bước chỉ đọc bị giữ.** Bước plan/review kết thúc bình thường nhưng cây đổi trong lúc chạy thì tác vụ `blocked` và kết
quả bước được giữ (`HeldStep`): `bint run resume <id>` áp kết quả đó, không chạy lại; `bint run resume <id> --rerun`
(driver session: `bint run next <id> --resume --rerun`) bỏ kết quả giữ và chạy lại bước — dùng khi đã hoàn tác thay đổi lạ.
Người tự chuyển trạng thái bằng `bint task status` (không qua resume) cũng bỏ kết quả giữ, lịch sử ghi dấu vết.

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
`workflow.max_review_rounds`, mặc định 2 — đây là số **vòng sửa** tối đa, nên 2 vòng sửa ứng với tối đa 3 vòng review;
hết vòng → `blocked`). Khi chạy lại `b-feat`, đề bài nhắc rõ các tiêu chí còn `unmet` để agent hoàn thành và đối chiếu lại.

**Hết vòng sửa: ba lựa chọn.** Còn phát hiện nghiêm trọng sau khi dùng hết vòng sửa, tác vụ `blocked` với lý do nêu số vòng nhất
quán ("còn 1 phát hiện nghiêm trọng sau 3 vòng review và 2 vòng sửa (tối đa 2 vòng sửa: `workflow.max_review_rounds` = 2)") và kết
quả lượt chạy (văn bản và `--json`, trường `choices`) in ba lệnh để người không đọc mã chọn:

```sh
bint run resume <id> --accept       # chấp nhận kết quả như hiện tại: vào thẳng finalize (cổng done, land vẫn áp dụng), không chạy b-feat/b-review
bint run resume <id> --fix-round    # cho sửa thêm đúng một vòng: một b-feat rồi một b-review; hết lại hiện ba lựa chọn (số vòng +1)
bint task cancel <id>               # huỷ
```

Driver session: `bint run next <id> --resume --accept|--fix-round`. `--accept` và `--fix-round` chỉ **người** dùng được
(`ErrAgentForbidden` trong phiên agent) và chỉ hợp lệ khi tác vụ đang chờ ở trạng thái này; lịch sử chuyển trạng thái ghi "người chấp
nhận N phát hiện nghiêm trọng còn lại". Vòng sửa **nối tiếp phiên của chính `b-feat` vừa chạy** (lớp dừng `review_fix`: driver process
dùng `claude --resume`/`codex exec resume`, driver session dùng SendMessage tới subagent cũ) khi cùng harness/profile, harness hỗ trợ
nối tiếp, có phiên và cây làm việc không đổi kể từ lúc `b-feat` kết thúc; lời nhắn nối tiếp liệt kê phát hiện nghiêm trọng và tiêu chí
`unmet`. `--fresh` hoặc thiếu điều kiện thì chạy đề bài đầy đủ như cũ, lý do ghi vào `notes`.

**`b-feat` không đổi tệp nào, hoặc hỏi người.** Sau bước implement cuối mà tác vụ không đổi tệp nào (kết quả `scope`/`touched` rỗng),
Bint không chạy kiểm chứng, không mở `b-review` và không chuyển `done`: tác vụ `blocked` (`LastStep` = `awaiting_human`), lý do nêu
điều `b-feat` đã ghi bằng `bint task error` (nếu có) và hai lệnh: `bint run resume <id> --answer "<nội dung>"` (driver session:
`bint run next <id> --resume --answer "<nội dung>"`) và `bint task cancel <id>`. `b-feat` hỏi người bằng
`bint task error <id> --step b-feat --question "<câu hỏi>"`: câu hỏi mở chặn tác vụ sau bước (kể cả khi đã đổi tệp), lý do và
`questions` in nguyên văn câu hỏi (quên `--step` thì câu hỏi gắn cho bước đang chạy). `--answer` ghi câu trả lời vào phiếu rồi chạy lại `b-feat` **trên chính tác vụ đó**; đề bài (hoặc lời nhắn
nối tiếp phiên khi nối tiếp được) và đề bài `b-review` chứa nguyên văn câu hỏi và câu trả lời. `--answer` chỉ hợp lệ khi tác vụ đang chờ
như vậy. `questions`, `awaiting_answer`, `review_exhausted`, `extra_fix_rounds` vào cả bản chiếu `.bint/tasks` (máy nhập phiếu dùng được
lệnh trong lý do blocked); `last_implement` (phiên harness, dấu vân tay cây) chỉ nằm ở `state.json` của máy.

**Đề bài `b-review`.** Đề bài kèm sẵn danh sách tệp và thống kê diff của tác vụ, diff đầy đủ khi không quá 48 KiB (vượt thì chỉ thống
kê và lệnh `bint task diff <id>`), `plan.md` (cắt ở 16 KiB), các câu hỏi `b-feat` đã được trả lời và, từ vòng 2, danh sách phát
hiện nghiêm trọng của vòng trước. Reviewer còn phải đối chiếu **yêu cầu gốc**, từng tiêu chí chấp nhận và mục "Bề mặt bị ảnh hưởng" của
`plan.md` để ghi phát hiện **bỏ sót** (được chỉ tệp ngoài diff, kèm `criterion`), không chỉ lỗi trong phần đã sửa. Kiểm chứng `scope: none`
(không test nào bị ảnh hưởng) ghi log "không chạy lệnh — không test nào bị ảnh hưởng", không in dòng lệnh như thể đã chạy.

**Mục "Bề mặt bị ảnh hưởng" của `b-req`.** `plan.md` (mức `standard` lẫn `deep`) lần theo từng nơi dữ liệu/hành vi đi qua rồi gộp theo
nhóm (≤ 8–10 dòng; ví dụ với bí mật: log, mô tả phiếu, `review.md`, bản chiếu `.bint/tasks`, SQLite; với dọn dữ liệu: loại dữ liệu trong
state dir), mỗi nhóm gắn một AC kiểm được hoặc lý do ngoài phạm vi kèm hậu quả người dùng sẽ thấy. Ngân sách cho yêu cầu mơ
hồ khoảng ≤ 5 AC và ≤ 5 TSK là để **gộp** bề mặt theo nhóm, không để cắt mục tiêu (benchmark lần 3: kế hoạch 12–14 KB, 7–8 AC làm diff gấp ~6 lần;
benchmark lần 4: plan T2 chia G1–G4 rồi chỉ làm G1). Chỉ tách giai đoạn khi người dùng đã chia hoặc phần sau là việc khác có giá trị riêng; khi
mục tiêu người dùng là toàn bộ thì cấm tách chỉ vì ngân sách AC/TSK.
Mục "Giả định thay người dùng" ghi giá trị mặc định cho quyết định kỹ thuật người không rành không trả lời được; giả định không đảo ngược
được hoặc đổi hành vi/dữ liệu hiện có mặc định chọn phương án đảo ngược được (xem trước/`--dry-run`, cảnh báo thay vì từ chối, chỉ che khi
có nhãn rõ). Yêu cầu mơ hồ: `b-req` hỏi tối đa 2–3 câu về kết quả mong muốn qua `## Câu hỏi mở` (bản phác hợp lệ rồi dừng; tác vụ chờ
người, trả lời thì nối tiếp planner) thay vì tự giả định hết.

**Kiểm tra mục tiêu (GĐ2 nhóm 2).** Bint đo "xong" theo **mục tiêu của người dùng**, không chỉ theo kế hoạch của agent. `requirement.md` do `b-req`
phải có mục `## Kiểm tra mục tiêu`: một kiểm tra chạy được suy từ mục tiêu và từng nhóm của `## Bề mặt bị ảnh hưởng` (chuẩn bị = dữ liệu giả cài
vào mọi đường nhập/nơi lưu; hành động; quét/đo; điều kiện đạt; tên test và tệp dự kiến) kèm đúng một AC "kiểm tra mục tiêu `<tên test>` xanh".
Ví dụ T2 (không lưu bí mật): cài bí mật giả có dấu dễ grep vào mọi đường nhập rồi quét toàn bộ state dir và repo, báo từng tệp còn thô; T1 (dọn
dữ liệu cũ): dựng dữ liệu cũ giả mỗi loại, kiểm preview liệt kê đủ và clean giải phóng dung lượng. `bint task lint` cảnh báo `goal_check_missing`
(chỉ cảnh báo, kể cả `--strict`, để phiếu cũ không hỏng; `bint run` ghi thêm một dòng vào `notes` sau `b-req`). Đề bài `b-feat` (đầy đủ và lời nhắn nối
tiếp `b-req` → `b-feat`) chép **nguyên văn** mục này (cắt ở 4 KiB); đề bài đường nhanh (không có `b-req`) dặn `b-feat` tự viết kiểm tra mục tiêu trước
khi sửa. Kiểm tra là **test tự động nằm trong bộ test của `verify.command`** (`b-req` chỉ đọc; Bint không chạy lệnh do agent đề xuất khi chưa có người
xác nhận): `b-feat` viết trước khi sửa, chạy sau mỗi nhóm sửa, không kết thúc khi còn đỏ trừ khi ghi tiêu chí `unmet` kèm đầu ra.

**Kiểm tra mục tiêu độc lập và đóng băng (`b-goal`).** Test do chính `b-feat` viết có cùng điểm mù với mã của nó (docs/evals/bench-bint-vs-plain-tong-hop.md
mục 4), nên ở `b-do` gốc đầy đủ Bint chạy thêm bước `b-goal` (kind `goal`, role `planner`, chỉ có trong flow nhúng `b-do`) **giữa `b-req` và `b-feat`**,
sau cổng cỡ kế hoạch, khi `requirement.md` có mục `## Kiểm tra mục tiêu` và phiếu có `verify.command`:
- **Phiên riêng, chỉ thấy `requirement.md`**: luôn là phiên mới (không dùng cơ chế nối tiếp khác model của `b-req` → `b-feat`: phiên `b-req` đã thấy
  plan/tasks nên nối tiếp sẽ làm lộ chúng); model theo `roles.planner`/`models.planner` (mặc định khác implementer). Đề bài chỉ gồm skill `b-goal`, phiếu và
  nguyên văn `requirement.md`, không có `plan.md`/`tasks.md` hay đường dẫn của chúng. Rào thực thi: với bước `b-goal` do `bint run` khởi chạy, Bint đặt
  biến `BINT_DENY_READ` (đường dẫn tuyệt đối `plan.md`, `tasks.md`, mỗi dòng một đường; không bước nào khác có biến này) và hook PreToolUse trả `deny` cho công cụ
  đọc trỏ tới các tệp đó và lệnh shell nhắc tới chúng (lệnh cố tình né bằng ghép chuỗi/biến không bị bắt: chặn đọc nhầm, không chặn cố ý). Hook Claude đã cài
  chỉ đăng ký `Read|Edit|Write|MultiEdit|NotebookEdit|Bash`, không có `Grep`/`Glob`, nên với b-goal chạy bằng Claude Bint cấp thêm settings riêng của bước (`--settings`, xoá
  sau bước) đăng ký `bint hook claude` cho `Grep|Glob`; harness khác không nhận. Rào Read/Bash vẫn dựa vào hook đã cài (`bint init` không có `--no-hooks`). Ở driver session
  (`bint run next`) subagent chạy trong phiên Claude chính nên Bint không đặt được biến này: rào vẫn chỉ là lời dặn trong đề bài. `b-feat`
  sau đó vẫn nối tiếp phiên `b-req` (Bint làm mới dấu vân tay cây sau `b-goal`). Driver session: subagent `bint-implementer` (cần quyền ghi), model theo planner.
  Tệp `b-goal` viết cần người duyệt phạm vi (đường dẫn bảo vệ, `scope.deny`…): tác vụ dừng chờ duyệt, và sau `bint task approve` rồi `resume` Bint chỉ chạy phần
  còn lại của bước (đóng băng đúng nội dung đã duyệt, cổng mốc), **không** gọi lại agent `b-goal`.
- **Đóng băng**: Bint tự đo tệp `b-goal` đã tạo/sửa (không tin khai báo của agent), ghi `{đường dẫn, sha256}` vào `State.GoalTest`, thêm chúng vào `scope.deny`.
  Sau **mỗi** bước implement, trước kiểm phạm vi, kiểm chứng và review, `freezeGate` so băm: tệp bị sửa/xoá/đổi tên thì tác vụ `blocked` nêu tệp; `bint task
  approve` không bỏ qua cổng này (người khôi phục tệp, ví dụ `git restore`, rồi `resume`). Tệp còn nguyên không bị hỏi duyệt dù khớp `scope.deny` và không
  được tính là việc `b-feat` đã làm (cổng "không đổi tệp nào"). Lối thoát khi chính test đóng băng sai: cổng mục tiêu đỏ là điều chặn người bỏ qua được như kiểm chứng đỏ, tức
  `bint task land <id> --force --reason "<lý do>"` (tác vụ worktree) hoặc `bint task status <id> done --force` (tác vụ shared); lý do và điều chặn được ghi vào
  `Landed.Bypassed`. Chỉ người dùng được cờ ép (agent bị từ chối). "Chưa chạy cổng mục tiêu" vẫn là điều chặn cứng. Chưa có lệnh chạy lại `b-goal` hay đóng băng lại.
- **Cổng riêng**: lệnh là `workflow.verify.affected_command` mở rộng trên tệp đóng băng khi đã đặt, không thì `verify.command`. Khi lệnh bắt đầu bằng `go test`, chưa có
  `-run` và tệp đóng băng là `*_test.go`, Bint thêm `-run '^(TestA|TestB)$'` gồm đúng các hàm `Test*` của tệp đóng băng để cổng không chạy cả gói (tệp khác Go,
  tệp không có hàm `Test*` hay lệnh đã có `-run` giữ lệnh cũ); chạy một lần ngay sau `b-goal`
  (mốc, không chặn; xanh ngay thì ghi chú "test có thể quá yếu") và sau kiểm chứng của mỗi bước implement. Log `goal-N.log`, kết quả ở `State.GoalChecks`, hiện ở
  `bint task show`. Đỏ được xử lý như kiểm chứng đỏ (`redVerification`: review thấy, vòng sửa); `finalize` chạy lại khi cây đổi và không `done` khi lần mới nhất đỏ.
- `b-feat` không còn tự viết test mục tiêu khi đã có tệp đóng băng (đề bài nêu tệp đó, cấm sửa). **Giữ hành vi cũ**: `--fast`, phiếu không có mục Kiểm tra mục
  tiêu hay không có `verify.command`, flow tuỳ biến và workflow `b-feat` riêng không chạy `b-goal`. Phiếu cũ còn `planned` sẽ chạy `b-goal` khi resume.

**Vòng sửa khi kiểm chứng đỏ.** Review không còn phát hiện nghiêm trọng nhưng lần kiểm chứng cuối (cổng sau `b-feat`, do một lượt điều phối chạy) **không
qua** — thường là test kiểm tra mục tiêu còn đỏ — thì `reviewDecision` không `finalize` (cổng done sẽ chặn không lối ra) mà mở vòng sửa trong cùng ngân
sách `workflow.max_review_rounds` (đề bài vòng sửa nêu lệnh, mã thoát và log của lần kiểm chứng đỏ). Hết ngân sách (hoặc `max_review_rounds: 0`) thì
`blocked` với ba lựa chọn `--accept` / `--fix-round` / huỷ và `reason` mở đầu bằng "review vòng N không còn phát hiện nghiêm trọng nhưng kiểm chứng cuối
không qua" (không dùng thông điệp "không hội tụ": 0 phát hiện không phải review đứng yên); `--accept` vẫn phải qua cổng done nên người ép bằng `--force`
như mọi tác vụ. Lần Bint tự kiểm chứng lại có lý do (finalize, tệp ngoài đổi) và lần người chạy tay `bint task verify` đi đường cũ: finalize chặn kèm dấu vân
tay để resume không review lại. Giới hạn đã biết: test chập chờn ngoài diff làm vòng sửa vô ích (`b-feat` không đổi tệp thì dừng chờ người như mọi tác vụ).

**Mức độ review và dừng khi không hội tụ.** `b-review` chấm theo **mục tiêu người dùng** (yêu cầu gốc và mục kiểm tra mục tiêu) rồi AC và bề mặt: chỗ sót mà
kiểm tra mục tiêu hoặc bằng chứng cụ thể cho thấy mục tiêu chưa đạt là `high` **kể cả khi nằm ngoài kế hoạch** hay trong "ngoài phạm vi" của plan; chỉ chỗ sót
ngoài mục tiêu hoặc suy đoán không bằng chứng là `low`. Kiểm chứng cuối không qua thì reviewer đọc log (đường dẫn có trong đề bài) và ghi phát hiện `high` cho
nguyên nhân; reviewer cũng kiểm test mục tiêu có đúng đặc tả, không bị nới. `critical`/`high` còn dành cho AC chưa đạt,
hồi quy và lỗi đúng đắn trong mã đã sửa. Từ vòng review 2, nếu số phát hiện nghiêm trọng (`Blocking`) không giảm so với vòng liền trước
(vòng đó cũng có phát hiện nghiêm trọng) và còn ngân sách vòng sửa thì `reviewDecision` không sửa tiếp mà vào nhánh `ReviewExhausted` sẵn
có: tác vụ `blocked`, `reason` bắt đầu `review không hội tụ`, ba lựa chọn `--accept` / `--fix-round` / huỷ như khi hết vòng sửa. Áp cho cả
`bint run` lẫn driver session; không áp cho flow có nhiều bước review. `--fix-round` cho đúng một vòng sửa: giảm thì chạy tiếp, không giảm
thì dừng lại. Hết `workflow.max_review_rounds` mà review **không giảm** vẫn dừng theo thông điệp cũ.

**Sửa tiếp khi review đang hội tụ.** Vòng review có số phát hiện nghiêm trọng **giảm hẳn** so với vòng liền trước (a → b với b < a) vẫn được
mở vòng sửa kế tiếp kể cả khi đã dùng hết `workflow.max_review_rounds` (ghi `notes` "vòng sửa N chạy ngoài workflow.max_review_rounds vì
review đang hội tụ"), tới **trần cứng `2 × workflow.max_review_rounds` cộng số vòng người cho thêm** (`--fix-round`). Ví dụ
`max_review_rounds = 2`: 5→4→3→2→0 chạy tới `done` (4 vòng sửa); 5→4→3→2→1 dừng ở trần (vòng review 5, thông điệp nêu "Review vẫn đang
giảm (2→1) nhưng đã chạm trần 4 vòng sửa") với ba lựa chọn; 4→3→3 dừng ở vòng review 3 (không giảm, không sửa tiếp); 4→4 dừng ở vòng 2
vì không hội tụ. Chỉ áp cho flow có một bước review; `max_review_rounds: 0` vẫn là không tự sửa. Giới hạn đã biết: reviewer hạ mức
nghiêm trọng của một phát hiện cũng được tính là giảm. Phát biểu "vòng sửa tối đa" ở trên là ngân sách *thường*; trần hội tụ chỉ mở thêm khi
số đang giảm.

**Đường nhanh của `b-do` (tuỳ chọn có chủ đích: `--fast`, skill `/b-do-fast`).** Mặc định `workflow.fast_path` **tắt** và `task.Classify` không tự chọn đường nhanh: yêu cầu **mơ hồ** (ngắn nhưng chưa nêu tệp, hành vi hay tiêu chí) đi `b-req → b-feat → b-review`.
**Giả định đã chốt (quyết định của người dùng):** hai nhánh bỏ `b-req` của `Classify` được giữ — việc nhỏ (chính tả/đổi tên/định dạng) và yêu cầu ngắn nêu rõ chỗ sửa (có mỏ neo cụ thể như tên lỗi) — chỉ yêu cầu mơ hồ mới bắt buộc qua `b-req`; muốn bỏ `b-req` cho yêu cầu khác thì chọn `--fast` hoặc `--no-plan`. Người dùng chủ động chọn đường nhanh bằng
`bint run b-do --fast "<yêu cầu>"`, `bint task new --fast` (rồi `bint run b-do --task <id>`) hoặc `bint run b-do --task <id> --fast` trên phiếu còn
`created`; skill mỏng **`/b-do-fast`** lập phiếu `--workflow b-do --fast` rồi điều phối như `/b-do`. Đường nhanh không đi `b-req → b-feat → b-review`
thành nhiều phiên mà chạy một phiên liên tục kiểu plain: bỏ `b-req`, `b-feat` một phiên (tự xác định phạm vi tối thiểu, chọn giả định đảo
ngược được, hỏi người bằng `bint task error --question` khi điều chưa rõ đổi cách làm), cổng kiểm chứng của Bint, rồi **đúng một vòng
`b-review`**. Còn phát hiện nghiêm trọng thì **đúng một vòng sửa** nối tiếp phiên `b-feat` (lớp `review_fix`), kiểm chứng rồi `finalize`
**không review lại**; `notes` nêu ID phát hiện của vòng 1 "đã qua một vòng sửa nhưng chưa được review lại" (muốn review lại:
`bint run b-review --task <id>`). Đường nhanh chỉ áp ở review **vòng 1** (vòng review sau `--fix-round` đi luật thường: ngân sách, hội tụ). Review vòng 1
sạch mà kiểm chứng đỏ cũng dùng đúng vòng sửa nhanh đó. **Kiểm chứng đỏ sau vòng sửa duy nhất** dừng tác vụ `blocked` với ba lựa chọn `--accept` /
`--fix-round` / huỷ (`reason` bắt đầu "đường nhanh: kiểm chứng sau vòng sửa duy nhất không qua", kèm lệnh, mã thoát, log); `--fix-round` rời đường nhanh: `b-feat`
rồi `b-review` lại như nhãn lựa chọn. `bint run b-req --task` trên phiếu `created` bỏ cờ `Fast` (b-req là đường đầy đủ). Mục tiêu: thời gian gần plain (20–30 phút)
cho yêu cầu mơ hồ đơn giản; đo ở `docs/acceptance.md` (`NT-FAST-BENCH`, đo bằng `--fast`).

Quy tắc của `--fast` (`workflow.checkFastFlag`, `task new` và `run`): chỉ workflow `b-do` gốc; **không** đi cùng `--plan`/`--no-plan`/`--depth` hay workflow khác
(lỗi, mã thoát 2, không chạy agent); với `--task` chỉ nhận khi phiếu còn `created`; độc lập với `workflow.fast_path`; `Classification.Fast` ghi đường nhanh, `reason`
nêu "chỉ định đường nhanh (--fast)". Phạm vi chạm `limits.protected_paths` (rủi ro `high`, kể cả khi scope **sửa cùng lượt hay sau lúc lập phiếu** bằng `--allow`/`task set
--allow`) thì đường nhanh bị bỏ: `b-req` bắt buộc, `reason` nêu đường dẫn bảo vệ và việc đường nhanh không áp dụng được.

`workflow.fast_path: true` (bật tay, opt-in) giữ hành vi chọn **tự động** cũ lúc lập phiếu: `task.Classify` rơi vào nhánh "yêu cầu ngắn nhưng chưa nêu tệp, hành vi hay
tiêu chí cụ thể" (≤ 25 từ, không mỏ neo, không từ khoá kế hoạch), workflow `b-do`, không tài liệu tham chiếu, rủi ro `low`, không `--plan`/`--no-plan`/`--depth`.
Đi **đường đầy đủ** khi: mô tả dài/nhiều ý, có tài liệu tham chiếu, rủi ro `high`, có cờ chỉ định (`--plan`, `--no-plan`, `--depth`), flow tuỳ biến/`b-feat`/`b-req`.
Yêu cầu ngắn **đã rõ** (nhánh "yêu cầu ngắn, rõ: bỏ b-req") và việc nhỏ giữ vòng review–sửa như cũ (ngân sách `max_review_rounds` và hội tụ). `max_review_rounds: 0`
tắt vòng sửa tự động của đường nhanh. Chạy cho cả driver process và driver session (cùng `reviewDecision`).

**Cổng cỡ kế hoạch của `b-do` (`workflow.small_plan_max_files`).** Sau `b-req` (và các cổng lint, câu hỏi mở, lệnh kiểm chứng), Bint đo kế hoạch trên tài liệu
(`internal/workflow/plansize.go`, chung cho driver process và session) rồi quyết định có dừng giữa `b-req` và `b-feat` không. `b-req` ghi mục `## Cỡ kế hoạch` trong
`plan.md`: dòng `Kết luận: nhỏ` hoặc `Kết luận: lớn` và mỗi tệp sẽ sửa trong một dòng bullet (đường dẫn trong backtick; một dòng nhiều đường dẫn thì Bint đếm đủ từng đường dẫn). Bint **không tin** kết luận "nhỏ": kế hoạch là **nhỏ** khi
và chỉ khi đủ cả: (1) plan ghi `Kết luận: nhỏ`; (2) không có dòng `G-N` nào dưới `## Đề xuất tách giai đoạn` (giai đoạn hoãn/tách ⇒ luôn lớn, "nhỏ" không dùng để cắt phạm vi);
(3) 1 ≤ số tệp liệt kê ≤ `workflow.small_plan_max_files` (mặc định 5; `0` = không kế hoạch nào là nhỏ); (4) `## Bề mặt bị ảnh hưởng` có ít nhất một nhóm bullet cấp ngoài và số nhóm ≤ cùng ngưỡng (thiếu mục hoặc 0 nhóm là lớn);
(5) `## Kiểm tra mục tiêu` của `requirement.md` không rỗng và nêu ít nhất một tên trong backtick (tên test); (6) rủi ro không `high`, không tệp liệt kê nào khớp
`limits.protected_paths` và không đường dẫn nào chứa ký tự glob (`*`, `?`, `[`: không đếm được số tệp thật). Thiếu mục, kết luận lớn hoặc trượt một điều kiện là **lớn** (plan cũ không có mục luôn lớn). **Nhỏ**: `b-feat` chạy ngay trong cùng lượt, vẫn là
phiên riêng nối tiếp từ `b-req`; `notes` ghi "cỡ kế hoạch: nhỏ"; kiểm chứng, review và kiểm tra mục tiêu giữ như đường đầy đủ. **Lớn**: có `Confirm` tương tác (terminal)
thì hỏi có/không (đồng ý ⇒ đi tiếp, `notes` ghi người đã đồng ý); không thì tác vụ `blocked` ở `planned` với `reason` mở đầu `kế hoạch lớn:` nêu từng lý do và lệnh
`bint run resume <id>` (driver session: `bint run next <id> --resume`); resume chạy `b-feat`, **không chạy lại `b-req`** (cổng không đặt `awaiting_human` và giữ
`State.LastPlan`). Cổng không bị bỏ qua bởi lần dừng khác đặt trước nó (lint `b-req`, lệnh kiểm chứng, câu hỏi mở): dấu `State.PlanSize` ghi cổng đã áp chưa, và `b-feat` đầu tiên sau `b-req` (`preflightFeat`) áp cổng nếu chưa — người/agent sửa lint rồi resume vẫn gặp cổng với kế hoạch lớn. Skill `/b-do` Bước 3 dặn orchestrator tóm tắt kế hoạch cho người dùng và chỉ resume khi họ đồng ý. Cổng chỉ áp cho `b-do` gốc: flow tuỳ biến chạy tiếp
như cũ và `b-req` chạy riêng vẫn dừng ở `planned`.

**Lát mỏng và chốt một chiều (`workflow.slice_timeout_minutes`).** Benchmark Bint-vs-plain cho thấy `b-feat` một khối kéo 20–47 phút, bị cắt giữa chừng thì không có gì
để giao, và review không hội tụ là cách dừng duy nhất. Kế hoạch **lớn** của `b-do` gốc vì thế không chỉ dừng báo: khi người cho chạy `b-feat`, Runner tự chia việc
thành **lát** (`internal/workflow/slice.go`), mỗi lát một lượt `b-feat` riêng có trần thời gian, và chỉ giữ lát nào đo được là tiến.

- **Điều kiện bật** (kiểm ở `b-feat` đầu tiên; thiếu một thì chạy một `b-feat` như cũ): `b-do` gốc chạy bằng `bint run` (driver process; driver session `bint run next`
  không chia lát), không đường nhanh, kế hoạch lớn theo cổng cỡ kế hoạch, đã có kiểm tra mục tiêu đóng băng (`b-goal`, `State.GoalTest`) và lệnh kiểm chứng đã được xác nhận,
  theo dõi được tệp của tác vụ (hoặc tác vụ ở worktree riêng), `plan.md` có **≥ 2 nhóm** cấp ngoài cùng dưới `## Bề mặt bị ảnh hưởng` có nêu `AC-N` (nhóm không nêu AC,
  như dòng "Ngoài phạm vi", không thành lát), và `workflow.slice_timeout_minutes > 0`. Danh sách lát ghi một lần vào `State.Slices` (theo thứ tự dòng; sửa `plan.md` sau đó
  không chia lại). Lý do chặn của cổng cỡ kế hoạch liệt kê các lát ("Bint sẽ chạy N lát: lát 1/N: …"); đề bài `b-req` của `b-do` dặn xếp nhóm đầu là bản nhỏ nhất vẫn làm giảm
  số ô đỏ của kiểm tra mục tiêu (luật nằm trong đề bài do Bint dựng, không phải skill).
- **Mỗi lát một `b-feat`**: đề bài nêu "lát k/N", nguyên văn dòng nhóm, các lát đã chốt (đừng làm lại), đã bỏ và lát sau (đừng làm trước); phiên mới, không nối tiếp lát
  trước. Thời gian agent của lát là `min(workflow.timeout_minutes, workflow.slice_timeout_minutes)` (mặc định 30; kiểm chứng không tính).
- **Đo và chốt**: sau mỗi lát (qua kiểm phạm vi, kiểm chứng và cổng mục tiêu như thường) Runner đọc log. *Tập ô đỏ* là tên các dòng `--- FAIL: <tên>` là lá; log
  *không đo được* khi quá giờ, có `[build failed]`/`[setup failed]`/`panic:`, hoặc thoát khác 0 mà không có dòng FAIL; mã thoát 0 là tập rỗng. Lát **chốt** khi
  (a) test cũ vẫn xanh: kiểm chứng của lát qua, hoặc scope `none`, hoặc log đo được và mọi ô đỏ thuộc test mục tiêu (hàm `Test*` của tệp đóng băng, hoặc tên cấp đầu của ô
  đỏ trong log cổng mục tiêu); **và** (b) tập ô đỏ của cổng mục tiêu là **tập con thực sự** của tập ở điểm xanh trước (giảm, không ô xanh nào chuyển đỏ); điểm xanh trước chưa đo
  được (vd. biên dịch hỏng) thì lát chỉ cần làm cho đo được; điểm xanh đã 0 ô đỏ thì lát phải giữ 0. Điểm xanh đầu là cây ngay sau `b-goal` (số đo của cổng mốc). Lát chốt ghim
  cây ở `refs/bint/tasks/<id>/slice-green` (điểm xanh chỉ tiến) và ghi tập ô đỏ vào `State.Slices`; `LastGreen` và `bint task restore-green` dùng điểm xanh mới hơn
  giữa lần kiểm chứng xanh cuối và điểm xanh lát.
- **Bỏ lát** (không đạt (a)/(b), hoặc agent quá trần): Runner ghim cây hiện tại ở `refs/bint/tasks/<id>/slice-<N>-discarded`, đưa **tệp của tác vụ** về điểm xanh dưới khoá độc quyền
  (cùng luật với `restore-green`: tệp tạo sau đó bị xoá; tệp ngoài tác vụ giữ nguyên, ghi chú liệt kê), ghi lát `discarded` kèm số đo ("ô đỏ 3→3 không giảm", "test cũ đỏ: TestX",
  "quá trần thời gian lát (30 phút)") rồi chạy lát kế. Lát 1 bị bỏ thì tác vụ `blocked` (không có gì để giao ngoài điểm xanh đầu); resume chạy tiếp lát chưa làm kế tiếp (không chạy lại lát đã bỏ).
  **Lấy lại lát bị bỏ**: `git restore --source=refs/bint/tasks/<id>/slice-<N>-discarded -- <tệp>`. Các ref lát xoá cùng `base` khi tác vụ kết thúc. Hết lát (còn ít nhất lát 1 chốt) thì
  sang `b-review`, vòng sửa, finalize như cũ; vòng sửa **không** chốt một chiều (vẫn theo `reviewStalled` và ngân sách vòng sửa).
- **Hết ngân sách hoặc lỗi**: hết `limits.workflows.b-do` (`max_cost_usd`, `max_turns`) trước lát kế thì `blocked` với lý do nối danh sách lát; agent lỗi (hết quota/chi phí của bước, hạ tầng; không phải quá trần, không phải
  người huỷ) giữa lát thì cây về điểm xanh (phần dở ghim ở `slice-<N>-discarded`), lát **giữ trạng thái chưa làm** (không bị coi là bỏ) và tác vụ `blocked` (không `failed`). Kết luận (`reason`, `notes` và `slices` trong JSON của `bint run --json`)
  nêu lát đã chốt, lát đã bỏ (lý do, ref lấy lại) và lát chưa làm; `resume` chạy tiếp lát chưa làm đầu tiên (lát dở vì lỗi thì chạy lại đúng lát đó), không chạy lại lát đã chốt hay đã bỏ (muốn làm lại lát đã bỏ thì lấy cây của nó bằng `git restore --source`, hoặc lập phiếu mới).
- **Tắt**: `workflow.slice_timeout_minutes: 0`. Hạn chế đã biết: driver session không chia lát; hiển thị lát trong TUI và `bint task show` chưa có (xem `slices` trong JSON kết quả,
  `state.json`).

**Không tự cắt phạm vi: dòng `G-N` và "mới xong 1/N".** Khi thật sự tách, `plan.md` ghi mỗi giai đoạn đúng một dòng `- G-N: mục tiêu — phạm vi — phụ thuộc` dưới
`## Đề xuất tách giai đoạn` (`G-1` là phiếu này; cho phép `[ ]`/`[x]`, bỏ dòng trong code fence). Bint đọc các dòng này (`task.PlanPhases`): từ hai giai đoạn thì
`notes` sau `b-req` ghi "kế hoạch chia N giai đoạn, phiếu này chỉ làm G-1" và `notes` lúc hoàn tất (JSON và bản in của `bint run`) ghi **`mới xong 1/N giai đoạn`**
kèm các giai đoạn chưa làm và lệnh lập phiếu con (`bint task new "<mục tiêu>" --parent <id> --depends-on <id>`). Skill `b-do` Bước 4 nêu nguyên văn dòng đó và đề nghị lập
phiếu con theo Bước 1c; Bint không tự lập hay chạy phiếu con (cần người đồng ý). Plan không có dòng `G-N` thì không ghi gì (plan cũ không đổi hành vi).

**Nối tiếp phiên `b-req` → `b-feat` (driver process).** `b-req` kết thúc bình thường (không dừng chờ người) thì Bint ghi `last_plan`
(phiên, dấu vân tay cây, model); `b-feat` **đầu tiên** của tác vụ nối tiếp phiên đó (lớp dừng `plan_handoff`, `claude --resume` /
`codex exec resume`) khi **cùng harness và profile `bint iso`, cây làm việc không đổi, phiên còn trên đĩa** và không `--fresh`. **Khác model vẫn nối tiếp** với
claude và codex (đã kiểm thật: `claude -p --model sonnet --resume <id>` trên phiên mở bằng opus; `codex exec -m gpt-6-luna … resume <id> -` trên phiên
mở bằng gpt-6-astra; xem `NT-CONT-CLAUDE-MODEL`, `NT-CONT-CODEX-MODEL` ở `docs/acceptance.md`): ngữ cảnh được giữ, cache prompt mất ở lượt đầu
sau khi đổi model nên `notes` có thêm một dòng nêu model cũ → mới và việc cache được dựng lại. Harness khác chưa kiểm thì khác model vẫn chạy đề bài đầy đủ.
Lời nhắn nối tiếp gồm thân skill `b-feat` (gốc + lớp phủ), thư mục làm việc/đường dẫn bảo vệ và **kế hoạch rút gọn** — các dòng `AC-N` của
`requirement.md`, các dòng `TSK-N` của `tasks.md`, câu hỏi đã trả lời, đường dẫn tài liệu để tra `plan.md` khi cần — và **không chép lại
`task.yml` lẫn khối tri thức** mà phiên đã nạp ở `b-req`. Usage của `b-feat` chỉ tính phần tăng thêm so với `b-req`. Khác harness/profile,
mất phiên, cây đổi hoặc `--fresh` thì `b-feat` chạy đề bài đầy đủ như cũ, lý do ghi vào `notes`. Chưa áp cho driver session: subagent
`bint-planner` chỉ có công cụ đọc nên không làm implementer qua SendMessage (b-feat ở session vẫn nạp lại ngữ cảnh). Giả định chưa đo thật:
cờ chỉ đọc/ghi áp theo từng lần gọi harness, và nối tiếp làm giảm lượt khám phá lại mã (NT-FAST-BENCH).

**Điểm verify xanh (`last_green`).** Mỗi lần kiểm chứng qua (không tính scope `none`) mà có ảnh chụp cây (`Verification.Tree`, tác vụ có theo
dõi tệp riêng) thì Bint ghim ảnh chụp bằng ref `refs/bint/tasks/<id>/green` (commit-tree, như base-tree) để `git gc` không dọn; ref xoá cùng
base khi tác vụ kết thúc. Không thêm trường vào `state.json`: điểm xanh cuối suy ra từ `Verifications`. Khi tác vụ dừng (`blocked`,
`interrupted`, `failed`), kết quả `bint run` (JSON và text, cả driver session), `bint task show` và `--json` có `last_green` (lúc, lệnh, phạm vi,
`pinned`, `changed_since`: tệp đã đổi từ điểm xanh) và `open_findings` (phát hiện kết cục open của vòng review mới nhất). Tác vụ cũ chưa có `Tree`
thì không có `last_green`. `bint task restore-green <id>` (chỉ người, RequireHuman): mặc định liệt kê tệp của tác vụ sẽ được đưa về nội dung điểm xanh;
`--apply` mới ghi, chỉ đụng tệp thuộc tác vụ (Touched, hoặc mọi tệp của worktree riêng), tệp khác giữ nguyên (liệt kê ở bỏ qua), tệp tạo sau
điểm xanh bị xoá, không đụng chỉ mục/HEAD/trạng thái tác vụ; trước khi ghi, cây hiện tại được ghim ở `refs/bint/tasks/<id>/before-restore`
(đường lui: `git restore --source=<ref> -- <tệp>`) và lấy khoá độc quyền của cây. Chỉ tác vụ đang dừng.

**Agent con của Codex.** Bước workflow chạy `codex exec` thêm `-c features.multi_agent_v2.disable_native_tools=true` (đo trên codex-cli 0.169:
model gpt-6.x có `multi_agent_version: v2` trong danh mục nên `features.multi_agent[_v2]=false` không tắt được). Lượt `bint eval` và skill
test không thêm cờ. Muốn bật lại cho bước workflow: đặt `-c features.multi_agent_v2.disable_native_tools=false` trong `harnesses.codex.extra_args`.

**Kết cục phát hiện (`bint findings`).** Mỗi phát hiện có kết cục `open` (mặc định, kể cả `review.json` cũ),
`fixed`, `rejected` hoặc `deferred`. Kết cục ghi chỉ-thêm vào `findings.json` trong thư mục tác vụ (kèm `by`,
`at`), không sửa `review.json`, nên vòng review mới không làm mất kết cục:

```sh
bint findings list [--open] [--task <id>] [--json]     # mọi tác vụ (kể cả done/cancelled); tác vụ hỏng báo ở stderr
bint findings set fixed <id>... --commit <sha>         # commit phải có thật; lưu SHA đầy đủ
bint findings set rejected <id>... --reason "…"        # tương tự deferred
bint findings set open <id>...                         # mở lại
bint task new --from-findings <id>[,<id>…] [--allow …] # phiếu sửa: scope.allow = đường dẫn phát hiện (+ --allow)
```

`--from-findings` từ chối phát hiện `fixed`/`rejected`; mô tả tự do thành tuỳ chọn (Bint liệt kê từng phát hiện
vào mô tả) và `from_findings` được ghi vào `state.json`. Chỉ có CLI, chưa có màn hình TUI.

**Phiếu cha/con và phụ thuộc (`depends_on`).** Quan hệ lưu trong `state.json` (trường `parent`, `depends_on`,
`inherited`, đều omitempty; không có trong `task.yml`, không migration), nên phiếu cũ đọc và chạy như trước:

```sh
bint task new "giai đoạn 1" --parent <id> --depends-on <id>[,<id>…]   # ID đầy đủ hoặc tiền tố
bint task set <id> --depends-on <id>…                                # thêm phụ thuộc
bint task set <id> --clear-depends-on                                # xoá hết phụ thuộc cũ
bint task ready [--parent <id>] [--json]                             # phiếu chưa kết thúc mà mọi phụ thuộc đã done
```

- ID không có, tiền tố mơ hồ hoặc trùng chính phiếu → mã thoát 2 và không tạo gì; cạnh tạo chu trình (A→B rồi
  B→A, hoặc dài hơn) → mã thoát 2, thông báo nêu đường chu trình, `state.json` không đổi.
- `task ready` liệt kê cũ nhất trước; phụ thuộc chưa xong, `cancelled` hoặc không đọc được làm phiếu vắng mặt.
  Phiếu cha không ngầm phụ thuộc phiếu con: muốn kế hoạch xong sau cùng thì thêm `depends_on` cho nó.
- Kế thừa: lúc lập, phiếu con chép từ phiếu cha `scope.allow`, `scope.deny`, `references`, `constraints` cho từng
  trường còn rỗng sau khi dựng từ mô tả và cờ (một lần, không theo dõi thay đổi về sau). Phạm vi tự đặt (đường dẫn
  trong mô tả, `--allow`, `--clear-scope`) không bị ghi đè; đánh giá rủi ro/đường dẫn bảo vệ tính trên phiếu sau
  kế thừa. `task new` in các trường đã kế thừa; `task show` in phiếu cha, từng phụ thuộc kèm trạng thái và phiếu
  con (`--json` thêm `children` chỉ khi có).
- Giới hạn: khoá chỉ theo từng phiếu, nên hai `task set --depends-on` chạy song song (A→B và B→A) có thể cùng qua
  kiểm chu trình; `task ready` chịu được dữ liệu như vậy (phiếu trong chu trình không bao giờ sẵn sàng).
- Agent: skill `b-do` (Bước 1c) dạy khi nào tách yêu cầu lớn thành phiếu cha + phiếu con giai đoạn có `depends_on`, lập chúng bằng `task new --parent/--depends-on`, rồi lặp `task ready --parent <cha> --json` + `run b-do --task <id>` tới khi hết phiếu sẵn sàng hoặc một phiếu dừng. `b-req` đọc tài liệu phiếu cha khi phiếu có `parent`, đề xuất tách giai đoạn trong `plan.md` (mục "Đề xuất tách giai đoạn") và không tự lập phiếu con dưới `bint run`.
- TUI (màn Tác vụ): hàng danh sách có `◇` (sẵn sàng) / `⧗` (chờ); phím `P` bật cây kế hoạch (nhánh vẽ bằng ký tự nối cây, phiếu được phụ thuộc đứng trước); phím `R` mở hộp chỉ đọc Đồ thị phụ thuộc của kế hoạch chứa phiếu đang chọn: các bậc theo thứ tự topo, mỗi phiếu kèm trạng thái, dấu sẵn sàng/chờ, phụ thuộc chưa xong, phiếu sẵn sàng kế tiếp; chu trình và phụ thuộc không đọc được hiện riêng.

#### Phiếu trong Git: `.bint/tasks/` và `bint task sync`

Phiếu tác vụ có hai nơi lưu. Thư mục `<state>/runtime/tasks/<ID>/` (ngoài repository) vẫn là **bộ đệm làm việc** và giữ
dữ liệu riêng của máy: runner, khoá, log, mọi phê duyệt của người, worktree, ảnh chụp cây. Phần **bền** của phiếu thì
được Bint xuất ra **cây làm việc** ở `.bint/tasks/<ID>/`, để commit cùng mã như `.beads/issues.jsonl` của Beads: mỗi
nhánh/worktree một bản, đi theo `git push`/`git pull`/merge, nên chia sẻ được giữa các máy và các nhánh. Bint **không
bao giờ tự commit hay push**; người commit `.bint/tasks/` như mọi tệp khác.

- **Xuất.** Mỗi lần Bint ghi phần bền của phiếu (tạo, đổi trạng thái, `task set`, `task write`, vòng review, `findings
  set`, đối chiếu tiêu chí...) thì `.bint/tasks/<ID>/` ở cây gốc của phiếu (cây lập phiếu; `origin` với tác vụ
  worktree) được cập nhật: `task.yml`, `state.json` (bản chiếu bền), `requirement.md`, `plan.md`, `tasks.md`,
  `review.json`, `findings.json`. Chỉ ghi khi bản chiếu đổi: thay đổi chỉ ở trường cục bộ (runner, bước dở...) không
  ghi lại tệp nào. Lỗi ghi `.bint/tasks` không làm hỏng thao tác cục bộ, chỉ in cảnh báo (`bint task sync` ghi bù).
- **Bản chiếu không chứa** trường cục bộ hay phê duyệt: `runner`, `pending_step`, `held_step`, `session_run`,
  `session_spend`, `agent_runs`, `worktree`, `origin`, `base_tree`, `touched`, `verify_approval`,
  `protected_approval`, `setup_approval`, đường dẫn log, dấu vân tay và mã cây của lần kiểm chứng. Nhập từ cây không
  đặt các trường đó, nên lệnh kiểm chứng của phiếu nhập về vẫn chờ `bint task approve` trên máy này.
- **Tự nhập sau `git pull`/merge (như Beads).** Trong phiên của người, khi bản **đã commit** trong `.bint/tasks/<ID>`
  khác băm lần đồng bộ trước và phía cục bộ không đổi (so băm, không dựa vào `updated_at`), thì các lệnh đọc (`task show`,
  `task list`, TUI) và lệnh ghi phiếu tự nhập trước khi dùng — kể cả phiếu chỉ có trong cây — không cần chạy gì. Đổi cả
  hai phía thì không nhập, giữ bản cục bộ và báo xung đột. Sửa **chưa commit** trong `.bint/tasks` (kể cả của agent đang
  chạy trong cây) không tự nhập, để không lặng lẽ đổi phạm vi hay tiêu chí của phiếu: nhập bằng `bint task sync`.
  Bản đã commit mà đổi trường quyền hạn (`autonomy`, `scope`, `verify.command`, `constraints`, chuyển sang `done`,
  lùi khỏi `done`/`cancelled`; phiếu mới có `autonomy: auto`) cũng **không tự nhập** (vì `.bint/tasks` miễn kiểm phạm vi, agent không worktree có
  thể sửa rồi commit): Bint cảnh báo, người xem `git diff` rồi chạy `bint task sync`, lệnh này nhập và nêu rõ từng thay
  đổi quyền hạn trong kết quả.
- **`bint task sync [--json]`** so ba bên (thư mục cục bộ, `.bint/tasks` trong cây, băm lần đồng bộ trước): phiếu chỉ có
  trong cây thì dựng thư mục cục bộ; chỉ có cục bộ thì ghi ra cây; một phía đổi (theo băm so với lần đồng bộ trước)
  thì lấy phía đó; chỉ `findings.json` khác thì hợp các bản ghi; đổi cả hai phía thì giữ bản cục bộ, báo xung đột và
  thoát mã 1. Sửa tay tệp trong cây (kể cả không đổi `updated_at` của `state.json`) cũng là một phía đổi: lần ghi tự
  động và `task sync` không bao giờ đè nó khi bản cục bộ cũng đổi. Giải xung đột: áp phần cần giữ của bản trong cây
  bằng `bint task set`/`bint task write`, xoá `.bint/tasks/<ID>` rồi chạy lại `bint task sync` để ghi bản cục bộ ra
  cây. Chạy lần hai liền sau không đổi tệp nào.
- **Phiên agent không nhập.** Trong phiên agent, `task sync` và các lệnh đọc/ghi chỉ ghi ra cây và liệt kê thay đổi chờ
  người nhập (`pending`); chỉ người mới áp thay đổi từ cây vào thư mục cục bộ, kể cả hợp bản ghi `findings.json`.
- **Cảnh báo.** CLI in cảnh báo của sổ (lỗi ghi, xung đột, thay đổi chờ nhập) ra stderr; TUI hiện chúng thành thông báo
  lỗi ở thanh trên và giữ trong hộp Thông báo (phím `!`).
- **Kiểm dữ liệu nhập.** Tên thư mục phải là ULID; chỉ nhận các tệp trên (tệp thường, không symlink, tối đa 1 MiB);
  `task.yml` qua kiểm phiếu và `id` khớp thư mục; JSON hỏng hoặc còn dấu xung đột merge bị bỏ. Mục sai bị bỏ qua và
  báo, không bao giờ ghi ra ngoài thư mục tác vụ.
- **Miễn khỏi mọi kiểm tra cây làm việc.** `.bint/tasks/` không tính vào ảnh chụp chỉ-đọc của bước plan/review, kiểm
  phạm vi, `touched`, `land`, `bint task diff`, dấu vân tay kiểm chứng và cổng plan-only: Bint ghi phiếu trong bước
  chỉ-đọc không làm bước bị chặn và không làm lần kiểm chứng trước thành lỗi thời.
- **Cấu hình `tasks.ledger`:** `git` (mặc định) hoặc `local`. `local` giữ đúng hành vi cũ: không đọc, không ghi
  `.bint/tasks`. Gỡ bỏ: đặt `local` và xoá thư mục `.bint/tasks`; bộ đệm cục bộ đủ dữ liệu.
- **Giới hạn.** Phía nào đổi được xác định bằng băm so với lần đồng bộ trước (lưu cục bộ cho từng cây), nên đổi nhánh
  sang bản phiếu khác mà phía cục bộ cũng đã đổi thì là xung đột, không tự chọn phía. Chỉ khi chưa có mốc đồng bộ với
  cây đó (vd. worktree dựng từ nhánh mang bản phiếu cũ) mới so `updated_at`: bản trong cây không mới hơn bản cục bộ
  thì coi là cũ, nên lệch đồng hồ giữa máy có thể làm sai ở ca này. **Chuyển sang nhánh mang bản phiếu cũ hơn**
  (`git checkout`, `git switch`) mà phía cục bộ không đổi thì là "cây đổi một phía": lệnh đọc/ghi phiếu tự nhập bản của
  nhánh đó (lịch sử, tiêu chí, review lùi theo), như Beads; riêng lùi khỏi `done`/`cancelled` và các trường quyền hạn ở
  trên thì không tự nhập, chỉ cảnh báo, người quyết bằng `bint task sync`. Tác vụ đang chạy
  dở trên máy khác (worktree, runner, bước subagent) là dữ liệu của máy và không chuyển sang. Log và `review.md`
  không xuất (`review.md` dựng lại từ `review.json`). Không có nhánh `bint/tasks` riêng hay merge driver: xung đột merge
  trong `.bint/tasks/` do người giải như tệp thường. `bint init --git local` loại `.bint/` khỏi Git nên phiếu không commit
  được khi dùng chế độ đó. eval/evolve/hook vẫn đọc bộ đệm cục bộ, không tự nhập.

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
| `workflow.max_review_rounds` (2) | số vòng **sửa** tối đa: phát hiện `bug`/`risk` mức `critical`/`high` → quay lại `b-feat`; hết vòng → `blocked` kèm ba lựa chọn (`--accept`, `--fix-round`, `task cancel`); review còn giảm thì sửa tiếp tới trần `2 ×` số này (+ vòng người cho thêm) |
| `workflow.fast_path` (`false`) | `true` = Bint tự chọn đường nhanh của `b-do` cho yêu cầu ngắn, mơ hồ, rủi ro thấp: bỏ `b-req`, một phiên `b-feat`, cổng kiểm chứng, một vòng review (+ một vòng sửa rồi xong, không review lại); `false` (mặc định) = `Classify` quyết như thường (yêu cầu mơ hồ vào `b-req`; việc nhỏ hoặc yêu cầu ngắn nêu rõ chỗ sửa có thể bỏ `b-req`), đường nhanh chỉ chạy khi người dùng chọn `--fast` / `/b-do-fast` |
| `workflow.small_plan_max_files` (`5`) | cổng cỡ kế hoạch sau `b-req` của `b-do`: kế hoạch nhỏ (số tệp và số nhóm bề mặt không quá ngưỡng, không giai đoạn hoãn, có kiểm tra mục tiêu nêu tên test, không chạm đường dẫn bảo vệ) chạy thẳng `b-feat`; còn lại `blocked` chờ người (`kế hoạch lớn: …`); `0` = mọi kế hoạch dừng chờ người |
| `workflow.timeout_minutes` (60), `verify_timeout_minutes` (15) | giới hạn thời gian mỗi bước agent / lệnh kiểm chứng |
| `workflow.slice_timeout_minutes` (`30`) | kế hoạch lớn của `b-do` chạy theo lát mỏng (mục "Lát mỏng và chốt một chiều"): mỗi nhóm bề mặt gắn AC một `b-feat` tối đa số phút này; lát không giảm ô đỏ của kiểm tra mục tiêu hoặc làm đỏ test cũ thì bị bỏ và cây về điểm xanh; `0` = tắt (một `b-feat`) |
| `workflow.verify.scope` (`full`), `verify.affected_command` | `affected`: kiểm chứng sau implement chỉ chạy test bị ảnh hưởng; done vẫn đòi chạy đầy đủ ([phạm vi kiểm chứng](#phạm-vi-kiểm-chứng-workflowverifyscope-và-bint-test-affected)) |

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

Hook ghi quyết định deny vào nhật ký spool riêng gắn với child run, để `runlog.Ingest` không thể
xóa tín hiệu trước khi runner đọc. Chỉ deny đường dẫn thuộc `limits.protected_paths` (harness không hỏi
được người dùng, như Codex, nhận deny thay cho "cần duyệt") làm workflow `blocked` và tạo yêu cầu duyệt
đường dẫn. Deny của guardrail `pre_edit`/`pre_bash` là phản hồi cho agent: nó được ghi vào log của bước
nhưng không đổi trạng thái tác vụ, vì agent có thể đổi cách làm (Bint không nhốt agent). Nhật ký deny của
child run được dọn khi bước kết thúc, kể cả khi bị ngắt. Lỗi ghi nhật ký không làm hook trả lỗi hay chặn agent.

`resume` tái dùng lựa chọn harness/model hiệu lực đã lưu theo role trong trạng thái tác vụ. Với
Claude, khi không có model theo role thì snapshot lấy từ `workflow.agent.model` (cùng giá trị agent
workflow dùng), không lấy model của `eval.agent`. Workflow đơn chỉ cần xác thực role tương ứng với
bước đang chạy; role khác chưa bật không làm bước đó lỗi. Role chưa chọn không được lưu như một
snapshot rỗng; bản cũ có Harness rỗng được coi là chưa chọn. Snapshot có Harness nhưng Model rỗng
giữ mặc định CLI, kể cả Claude, khi cấu hình model đổi. Trong `b-do`, planner đã bỏ qua theo
classification/trạng thái hoặc `--no-plan`, và reviewer khi `--no-review`, không bị xác thực. Việc phân giải lựa chọn diễn ra sau Classify và áp cờ lập kế hoạch, trước khi claim runner. Cờ
`--harness`, `--role` hoặc `--model` truyền tường minh khi resume ghi đè lựa chọn tương ứng; harness
vẫn phải đang có trong `harnesses.enabled`. Tác vụ cũ chưa có snapshot dùng cấu hình hiện tại và lưu
lựa chọn đó khi chạy tiếp. Tác vụ vẫn giữ nguyên workflow, quyền duyệt và ngân sách.

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

**Dấu build và lệch phiên bản binary ↔ `core.lock`.** `bint --version` in phiên bản kèm revision rút gọn, thời điểm commit
(`vcs.time`) và `sửa` khi `vcs.modified=true` (ví dụ `0.1.0-dev (c17f329, 2026-11-15T09:06:25Z, sửa)`); bản build không có
thông tin VCS (`go test`, `go run`) chỉ in phiên bản. `core.lock` ghi `bint_revision`, `bint_revision_time`,
`bint_modified` của binary đã ghi băm skill gốc; các trường này **chỉ đổi khi băm skill gốc trong lock đổi** (hoặc lock chưa
có chúng), nên `bint sync` lặp lại bằng binary khác mà nội dung không đổi không làm đổi `core.lock` (không nhiễu git).
Hướng lệch giữa binary và `core.lock`: semver khác nhau thì theo semver; cùng semver thì theo `vcs.time` của hai revision;
cùng revision thì lock có sửa mà binary sạch nghĩa là binary cũ hơn; thiếu thông tin hoặc binary có sửa là *không xác định*.
`bint doctor` có mục `phiên bản skill`: khớp / binary cũ hơn (cảnh báo, khuyên `bint update --pull`, không gắn sửa nhanh sync) /
binary mới hơn (khuyên `bint sync`, có sửa nhanh) / không xác định.

- **Chặn hạ cấp.** `bint sync`, `bint init --update` và cài skill từ TUI trả lỗi, không ghi tệp skill nào và không đổi
  `core.lock` khi binary cũ hơn `core.lock` **và** băm skill gốc nhúng khác lock. Muốn hạ cấp thật: `bint sync --allow-downgrade`
  (cờ chỉ của người; `bint update --pull` là cách cập nhật binary). Băm khớp thì không từ chối.
- **Tự xuất lại skill.** `bint status`, `bint task new` và `bint run <workflow>` tự chạy sync khi binary *sạch* mới hơn
  `core.lock`, băm skill gốc nhúng khác lock và bản xem trước sync không có xung đột; in đúng một dòng lên stderr (stdout không
  đổi), lỗi của bước này chỉ là cảnh báo và không làm lệnh thất bại. Không chạy khi có tệp đã xuất bị sửa tay, binary cũ hơn hoặc
  không xác định, binary có `vcs.modified`, trong phiên agent, khi `BINT_RUN_ID` có giá trị, và không bao giờ trong `bint hook`.
- **Nhắc ở SessionStart.** Hook `SessionStart` của Claude Code so `core.lock` với binary đang chạy (chỉ đọc một tệp: không SQLite,
  không gọi mô hình, không trả lỗi kể cả khi `core.lock` hỏng) và khi binary cũ hơn thì chèn đúng một dòng
  `additionalContext` nhắc `bint update --pull`. Khớp, mới hơn, không xác định: không chèn gì. Codex và Antigravity chưa có đầu ra
  SessionStart đã xác minh nên chưa nhắc.
- **`bint task done <id>`** là bí danh của `bint task status <id> done` (cùng `--force`, `-m/--reason`, `--json`).
  Gõ sai lệnh con (ví dụ `stauts` thay cho `status`, `tsak` thay cho `task`) thoát mã 2, nêu lệnh lạ và dòng `Ý bạn là:` kèm gợi ý.
- **`--json` cho lệnh ghi của task.** `bint task set --json`, `bint task status --json`, `bint task done --json`, `bint task write --json`, `bint task criteria --json`, `bint task error --json` và `bint task review --json` in đúng một JSON object (có
  `task_id`) lên stdout thay cho văn bản; lỗi vẫn ra stderr với mã thoát như cũ.

**Skill tuỳ biến** (GĐ1 của `docs/custom-flow-plan.md`). Ngoài skill gốc (bất biến) và skill tiến hoá (do Bint sinh),
dự án có thể có skill do người dùng tự viết: thư mục `.bint/skills/<tên>/` (commit được) gồm `SKILL.md` và tệp phụ
(tài liệu tham chiếu, script, kể cả thư mục con). Mọi thư mục con trực tiếp của `.bint/skills/` là ứng viên, trừ tên
dành riêng `overlays`, `evolved` và thư mục ẩn. Tạo bằng `bint skill new <tên>` hoặc nhờ agent qua skill gốc
`/b-add-skill` (hướng dẫn quy trình RED → GREEN → REFACTOR bằng subagent, và khi nào *không* nên tạo skill).
`bint sync` xuất bản `SKILL.md` (với `.agents/skills` chỉ giữ `name`, `description`) và tệp phụ nguyên văn sang
`.claude/skills/<tên>/` và `.agents/skills/<tên>/`; `core.lock` ghi mục `custom` với băm từng tệp đã xuất bản.
Thư mục `tests/` ở gốc skill và tệp ẩn không được xuất bản; symlink hay tệp không thường là lỗi. Trần tệp phụ: 50 tệp,
mỗi tệp 1 MiB, tổng 5 MiB. Cùng quy tắc an toàn như skill gốc: bản đã xuất bản bị sửa tay → xung đột (`--force`
ghi đè, có sao lưu; tuỳ biến thuộc về nguồn trong `.bint/skills/<tên>/`); tệp phụ hoặc cả skill bị xoá khỏi nguồn →
gỡ bản đã xuất bản nếu chưa bị sửa tay.

`bint skill lint` (và `bint sync`, `bint doctor`) kiểm: tên (chữ thường, số, gạch ngang, 3–40 ký tự; không tiền tố `b-`;
không trùng skill gốc hay tiến hoá), `name` trùng tên thư mục, `description` không rỗng và ≤ 1024 ký tự, frontmatter
chỉ gồm `name`, `description`, `disable-model-invocation`, `argument-hint`, thân không rỗng và ≤ 500 dòng, liên kết
Markdown tới tệp phụ phải tồn tại. Description không mở đầu "Dùng khi"/"Use when" chỉ bị cảnh báo. Skill có lỗi lint
→ xung đột: **không** xuất bản bản mới nhưng **giữ** bản đã xuất bản và mục `core.lock` cũ (như lớp phủ hỏng).
Nội dung skill tuỳ biến là tệp người viết: ranh giới tin cậy là review Git (không có kiểm nội dung kiểu skill tiến hoá).

### Flow tuỳ biến (GĐ2 và GĐ4 của `docs/custom-flow-plan.md`)

Một **flow** là dãy bước của một workflow; mỗi bước có `id`, `kind` (`plan` | `implement` | `review`, do Bint giữ vì kind
quyết định trạng thái tác vụ, chế độ chỉ đọc và cổng sau bước), `skill` và `role`. Năm workflow gốc `b-do`, `b-req`, `b-feat`,
`b-review`, `b-debug` là flow nhúng (gói `internal/flow`; `b-do` = `b-req` plan tuỳ chọn → `b-feat` implement → `b-review` review với
vòng sửa về `b-feat`; `b-debug` xem mục "Flow `b-debug`" dưới đây). Người dùng khai báo thêm flow trong `.bint/flows/<tên>.yml` (commit được):

```yaml
schema: 1
name: tdd                # trùng tên tệp; không trùng workflow gốc, không tiền tố b-, không trùng tên skill
description: "Dùng khi muốn viết kiểm thử đỏ trước khi sửa mã"
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
```

Khoá của bước: `id`, `kind`, `skill` (bắt buộc); `role` (rỗng → `planner`/`implementer`/`reviewer` theo kind), `optional`
(chỉ bước plan: bỏ qua theo phân loại), `verify` (chỉ bước implement: có chạy lệnh kiểm chứng sau bước hay không, mặc định có),
`fix` (chỉ bước review: id bước implement mà vòng sửa quay về), `expect` (chỉ bước implement, giá trị duy nhất `fail`: cổng đỏ,
xem "Khoá `expect: fail`"). Khoá YAML lạ ở bất cứ đâu là lỗi.

`bint flow lint [tên]` (thoát 1 khi có lỗi, `--json`) từ chối: `schema` khác 1; tên sai quy tắc (chữ thường, số, gạch ngang,
3–40 ký tự), khác tên tệp, trùng workflow gốc, có tiền tố `b-` hoặc trùng tên một skill (gốc, **mọi** thư mục skill tuỳ biến kể cả đang lỗi
lint, hoặc skill tiến hoá, vì flow được xuất bản thành skill cùng tên; cùng luật ở `bint flow lint`, `bint run <flow>` và `bint sync`); không có bước; id rỗng, trùng hoặc trùng bước giả `post-feat`/`verify`; `kind` ngoài ba giá trị; kind giảm
theo thứ tự plan → implement → review; hơn một bước plan; thiếu bước implement; `skill` rỗng hoặc không tồn tại (skill gốc hoặc
skill tuỳ biến qua được `bint skill lint`; skill tiến hoá không tính); `optional` ở bước không phải plan; `verify: false` ở bước
implement cuối (kiểm chứng phải chạy trước review và trước khi done); `expect` khác `fail`, ở bước không phải implement, ở bước
implement cuối hoặc cùng `verify: false`; `fix` ở bước không phải review hoặc không trỏ tới bước
implement. Một tệp hỏng không chặn việc kiểm tệp khác; flow nhúng cũng qua cùng bộ luật (riêng flow nhúng một bước được miễn
luật "thiếu implement" và luật tên gốc). `bint flow list` hiện cả flow nhúng và flow tuỳ biến (nguồn, số bước, mô tả, trạng thái
lint); `bint flow show <tên>` in định nghĩa đã chuẩn hoá (role mặc định được điền) kèm nguồn và băm (`sha256:…`, không phụ thuộc
chú thích, thứ tự khoá hay mô tả). Trong TUI: ba hành động chỉ đọc "Liệt kê/Xem/Kiểm flow" ở bảng lệnh (`:`), kết quả ở khung đầu ra
của thẻ Skill.

#### Phạm vi kiểm chứng: `workflow.verify.scope` và `bint test affected`

Mặc định (`workflow.verify.scope: full`) mọi lần kiểm chứng chạy nguyên `verify.command` của phiếu. Với
`scope: affected`, **vòng kiểm chứng sau bước implement** (sau `b-feat` và các bước implement của flow tuỳ biến) chỉ chạy
các test bị ảnh hưởng bởi thay đổi của tác vụ, để vòng review/sửa nhanh hơn. Chỉ áp dụng khi `verify.command` của phiếu đã được
người xác nhận; lệnh chạy là `workflow.verify.affected_command`, trong đó `{test_dirs}` (thư mục `./dir`) và `{test_files}`
được thay bằng test được chọn, ví dụ:

```yaml
workflow:
  verify:
    scope: affected
    affected_command: "go test -count=1 {test_dirs}"   # bắt buộc có {test_dirs} hoặc {test_files}
```

Tập thay đổi là mọi tệp khác nhau giữa commit gốc của tác vụ (`Base`) và cây làm việc hiện tại (gồm thay đổi chưa commit và tệp
chưa theo dõi, không gồm sổ phiếu `.bint/tasks`). Phạm vi chọn theo tầng, từ hẹp đến rộng:

| Tầng | Khi nào | Lần kiểm chứng làm gì |
|---|---|---|
| `none` | chỉ `*.md`/`*.txt` ngoài mọi gói mà không test nào nhắc tới tên tệp (test có nhắc thì nới lên `package`, lý do `test_reference`; `.md`/`.txt` trong thư mục con của gói thuộc gói đó); hoặc không gói nào có test bị ảnh hưởng | không chạy lệnh nào, ghi nhận là qua |
| `graph` | có tệp mã (`.go`) trong thư mục gói | chạy `affected_command` với test của gói đổi và mọi gói có test phụ thuộc bắc cầu vào nó |
| `package` | thay đổi không phải mã nằm trong gói: `testdata/`, tệp được `//go:embed`, tệp khác trong thư mục gói | như `graph` |
| `full` | `go.mod`, `go.sum`, `go.work`, khoá phụ thuộc, cấu hình công cụ, `.bint/**`, `.github/**`, `Makefile`, script, tệp lạ ngoài gói, module lồng, hoặc nguồn ảnh hưởng không dùng được | chạy nguyên `verify.command` |

**Quy tắc nới (không bao giờ thu hẹp).** Gặp thay đổi không phân loại chắc được thì phạm vi nới lên tầng rộng hơn, và mỗi lần
nới ghi đường dẫn, tầng và lý do (`root_config`, `ci_config`, `bint_config`, `testdata`, `embedded_file`, `non_code_change`,
`nested_module`, `unknown_package`, `deleted_package`, `unknown_file`, `source_unavailable`, `unsupported_language`, `no_base`).
Cụ thể: `go list` lỗi hoặc không có trên `PATH`, gói lỗi nhập/biên dịch, thư mục có `.go` mà không ra gói nào, gói bị xoá, tệp
thuộc module lồng (`go.mod` con), dự án không có nguồn hỗ trợ (không có `go.mod`) và không có commit gốc đều ra `full`.
Phạm vi rỗng ở `graph`/`package` hạ về `none` (không bao giờ để `{test_dirs}` rỗng, vì `go test` không đối số sẽ chạy thư mục
hiện tại).

**Nguồn ảnh hưởng theo ngôn ngữ** nằm sau interface `affected.Source` (`internal/affected`). Hiện có Go: một lần
`go list -e -test -json ./...` cho đồ thị phụ thuộc mức gói, ánh xạ tệp đổi theo thư mục gói (tệp bị build tag loại như
`*_windows.go` vẫn ra gói của nó, chạy được trên mọi GOOS), chọn gói có test mà phụ thuộc bắc cầu vào gói đổi. Không dùng codegraph
làm nguồn cho Go vì nó bỏ sót gói phụ thuộc theo từng tệp build tag. Ngôn ngữ chưa hỗ trợ → `full`.

**Bằng chứng.** `Verification` ghi `scope`, `selection` (`base`, `head` là mã cây, `changed` tối đa 200 tệp kèm cờ cắt,
`widening`, test được chọn) và `ran` (lệnh thực chạy); payload `task.verified` có `scope`. state.json cũ không có `scope` = full.

**Vẫn chạy đầy đủ trước khi xong.** Lần kiểm chứng một phần chỉ là bằng chứng trung gian:

- cổng `done` không nhận lần kiểm chứng cuối có `scope` khác `full` (điều chặn mới, ép được bằng `--force` như các điều chặn
  bằng chứng khác);
- finalize tự chạy `verify.command` đầy đủ một lần trước `done`/`land` khi lần cuối chỉ là affected (ghi lý do vào lần kiểm
  chứng). Nếu lần chạy đầy đủ này hỏng, tác vụ quay về bước sửa (`in_progress`, như vòng sửa sau review, chung ngân sách
  `workflow.max_review_rounds`); hết vòng mới chặn chờ người;
- `bint task verify` và cổng đỏ (`expect: fail`) luôn chạy đầy đủ.

**`bint test affected`** dùng cùng bộ chọn ngoài tác vụ (cho CI và người dùng):

```sh
bint test affected --base origin/main            # in scope, base/head, tệp đổi, lý do nới, thư mục test
bint test affected --base origin/main --json     # như trên dạng JSON (selection, command, ran, exit_code)
bint test affected --base origin/main --run      # chạy luôn và thoát theo mã của lệnh test
```

`--base` mặc định `HEAD` (chỉ thay đổi chưa commit và tệp chưa theo dõi); gốc so sánh là điểm tách của `--base` khỏi `HEAD`
(`merge-base`). `--run` chạy `affected_command` ở tầng `graph`/`package`, `eval.test_command` ở tầng `full`, không chạy gì ở
`none`; thiếu lệnh tương ứng thì báo lỗi. Với `--json`, đầu ra của lệnh test đi sang stderr.

#### Khoá `expect: fail` (cổng đỏ)

Bước implement khai báo `expect: fail` là bước viết **kiểm thử tái hiện lỗi**: sau nó Bint luôn chạy lệnh kiểm chứng của phiếu
(không phụ thuộc `verify`) và **mong nó hỏng**. `flow.Validate` từ chối khoá này ở bước không phải implement, ở bước implement
cuối (sau bước đỏ phải có bước sửa cho kiểm chứng qua) và cùng `verify: false`. Runner áp ở `afterStep`, theo thuộc tính của bước
chứ không theo tên:

- Kiểm chứng **hỏng**: ghi một `Verification` hỏng (bằng chứng đỏ trước bản sửa) và sang bước kế.
- Kiểm chứng **qua**: test chưa tái hiện lỗi (có thể lệnh kiểm chứng không bao phủ test mới). Tác vụ `blocked` nêu lý do này;
  `State.FlowStep` giữ ở bước đỏ nên `bint run resume` (hoặc `bint run next --resume`) chạy lại đúng bước đó.
- Kiểm chứng **không chạy được** (lệnh bị bỏ qua hoặc quá giờ): `blocked`, giữ `AwaitingCheck`; `resume` chỉ chạy lại cổng, không
  gọi lại agent.
- **Trước** bước đỏ: phiếu thiếu `verify.command`, hoặc lệnh chưa được người xác nhận (`bint task approve`) thì tác vụ `blocked`
  trước khi gọi agent, lời nhắc nêu `bint task set --verify-command`, `bint task approve` và lệnh resume.

Giới hạn: kiểm chứng hỏng chỉ chứng minh `verify.command` hỏng sau bước đỏ, không chứng minh nó hỏng đúng ở test mới (có thể do
lỗi biên dịch hoặc một test khác đỏ sẵn). Skill `b-repro` bắt chạy test hẹp và ghi bằng chứng, `b-review` xét lại, còn `bint eval
verify` là phép kiểm fail→pass độc lập khi phiếu trở thành đề đánh giá.

#### Flow `b-debug`

Flow nhúng thứ năm, dùng cho sửa lỗi có test tái hiện (cùng khuôn `/b-do`, phần thực thi tách làm hai):

| Bước | kind | Skill | Ghi chú |
|---|---|---|---|
| `b-req` | plan, optional | `b-req` | như `b-do`: `--plan`/`--no-plan`/`--depth` |
| `b-repro` | implement, `expect: fail` | `b-repro` | viết một test hỏng, không sửa mã sản phẩm |
| `b-fix` | implement | `b-fix` | giả thuyết nguyên nhân, sửa tối thiểu, test qua |
| `b-review` | review, `fix: b-fix` | `b-review` | vòng sửa quay về `b-fix`, không về `b-repro` |

Chạy: `bint task new "<mô tả lỗi>" --workflow b-debug --verify-command '<lệnh test>'` rồi `bint run b-debug --task <id>`, hoặc
`bint run b-debug "<mô tả lỗi>"`, `bint run resume`, driver session (`bint run next/complete`), TUI (ô workflow của form `run` và
`task new`). Skill điều phối `/b-debug` (skill gốc, cùng khuôn `/b-do`) hướng dẫn lập phiếu kèm lệnh kiểm chứng. Role theo kind
(`planner`, `implementer`, `reviewer`); ngân sách `limits.workflows.b-debug`.

Khác bốn workflow gốc cũ, id bước của `b-debug` (`b-repro`, `b-fix`) không phải tên workflow đơn lẻ, nên nó **gắn phiếu** như flow
tuỳ biến: phiếu `b-debug` chỉ chạy bằng `bint run b-debug --task` (`b-do`, `b-feat`, `b-review --task` bị từ chối; `b-debug` cũng
không nhận phiếu `b-do`, chỉ nhận phiếu mới chỉ lập kế hoạch bằng `b-req`), và `--step` hợp lệ cho nó: `bint run b-debug --task <id>
--step b-repro|b-fix|b-review`. Role gán harness khác Claude trong driver session trả lệnh dạng `--step` đó. `b-debug` là flow nhúng
nên không ghi `flow_sha` (đổi theo phiên bản Bint).

Phiếu `b-debug` đã `done` cho ra một đề fail→pass: `bint eval mine --from-tasks` nhận nó cùng phiếu `b-do` (xem
[evaluation.md](evaluation.md)); cây trước `b-repro` là commit gốc của đề, cây sau `b-fix` là lời giải, test tái hiện là kiểm thử ẩn.

#### Chạy flow tuỳ biến (GĐ4)

Flow tuỳ biến chạy bằng cùng các lệnh với workflow gốc: `bint run <flow> "<mô tả>"`, `bint task new --workflow <flow>` rồi
`bint run <flow> --task <id>`, `bint run resume <id>`, driver session (`bint run next/complete`) và màn Tác vụ của TUI (form
`run` và `task new` có thêm các flow qua lint sạch ở ô workflow; thẻ Phiếu hiện bước flow đang chạy). Tên `<flow>` được Runner
nạp từ `.bint/flows/<flow>.yml` của thư mục có `.bint/` (không phải worktree của tác vụ, nên flow chưa commit vẫn chạy được ở
chế độ worktree) và phải qua cùng bộ luật của `bint flow lint` (kể cả skill của bước tồn tại) trước khi chạy.

Runner theo **kind** của bước, không theo tên bước:

- Thứ tự: bước plan (bỏ qua nếu `optional` và phân loại không cần) → các bước implement theo thứ tự → bước review. Đề bài của
  mỗi bước là thân skill đã khai báo (skill gốc ghép lớp phủ, hoặc skill tuỳ biến `.bint/skills/<tên>`) cộng khối tác vụ như
  workflow gốc; skill lỗi lint thì bước `failed` nêu skill.
- Bước plan và review **chỉ đọc** (kiểm cây làm việc trước/sau, khoá worktree chia sẻ, công cụ sửa tệp bị cấm); bước implement
  giữ khoá độc quyền. Chế độ chỉ đọc theo kind của bước trong flow đang chạy: một bước implement đặt id `b-review` vẫn ghi
  được.
- Sau **mỗi** bước implement Bint kiểm phạm vi (`scope`, đường dẫn bảo vệ, autonomy) trước khi sang bước sau. Lệnh kiểm chứng
  của phiếu chạy sau bước implement có `verify` (mặc định `true`) và **luôn** trước bước review đầu tiên và trước khi done, kể
  cả khi bước implement cuối khai báo `verify: false` (luật nằm ở Runner, không chỉ ở lint). Lệnh vẫn chỉ chạy khi người đã xác
  nhận (`bint task approve`).
- Review còn phát hiện nghiêm trọng (tối đa `workflow.max_review_rounds` vòng sửa): quay về đúng bước `fix` của bước review (không
  khai báo `fix` thì về bước implement cuối) rồi đi tiếp các bước sau nó tới review; sạch thì sang bước review kế tiếp hoặc
  finalize. `State.FlowStep` ghi bước đang chạy/kế tiếp nên resume và driver session đi tiếp đúng chỗ.
  Flow một bước review đếm vòng theo số vòng của `review.json`; flow có **nhiều** bước review chỉ đếm các vòng có phát hiện nghiêm
  trọng (mỗi lượt review sạch của mỗi bước không tiêu hao ngân sách vòng sửa).
- Mọi flow tuỳ biến, kể cả flow chỉ có **một** bước implement, do Runner điều phối cả chuỗi: sau implement cuối chạy review (nếu
  có) rồi finalize (không dừng ở `in_review`), và chịu ngân sách chung `limits.workflows.<tên flow>` (tên flow khác id bước).
  Phiếu chuyển từ b-req sang flow tuỳ biến chưa có `FlowSHA` thì băm được ghi ở lần đầu flow nạp được.
- Cổng của Bint không đổi: kiểm chứng đã xác nhận, đường dẫn bảo vệ, `autonomy`, `done` chỉ khi có bằng chứng, giới hạn
  (`limits.workflows.<flow>` là ngân sách chung của cả flow nhiều bước; mỗi bước dùng thêm `limits.workflows.<id bước>` nếu có).

**Flow đổi giữa chừng.** Phiếu của flow tuỳ biến ghi `flow_sha` (băm `bint flow show`) lúc lập. Nếu tệp flow khác băm đó (bị sửa,
hoặc bị xoá/hỏng) thì `bint run <flow> --task`, `bint run resume` và `bint run next` chuyển tác vụ `blocked` kèm lý do nêu tên
flow và không chạy bước nào; không trộn hai định nghĩa trong một tác vụ. Cách thoát: khôi phục `.bint/flows/<flow>.yml` về bản lúc
lập phiếu rồi `bint run resume <id>`, hoặc `bint task cancel <id>`. Băm chỉ phủ định nghĩa flow, không phủ nội dung skill. Flow
nhúng không ghi băm (nó đổi theo phiên bản Bint). Tác vụ lập cho flow tuỳ biến chỉ chạy bằng đúng flow đó; tác vụ mới chỉ lập
kế hoạch (`b-req`) có thể chạy tiếp bằng flow tuỳ biến (khi đó băm ghi lúc chuyển).

**Role tuỳ biến.** Role của bước là `role` của nó (rỗng → `planner`/`implementer`/`reviewer` theo kind; không dùng được
`interactive`, `evolver`, `reflector`). Cấu hình nhận role tự do cùng cấp với role cố định (`roles.test-writer: codex`,
`harnesses.<h>.models.test-writer: gpt-6-luna`, `mcp.grants.test-writer: […]`). Chọn harness: `--harness` → `roles.<role>` →
(role chưa cấu hình) cách chọn của role gốc theo kind (`--role <gốc>=… → roles.<gốc> → harnesses.default → claude`); model:
`--model` → `models.<role>` → `models.<gốc>` → model chung; quyền MCP của bước driver process: `mcp.grants.<role>` nếu khai báo,
không thì `mcp.grants.<gốc>`. `--role` ở CLI vẫn chỉ nhận `planner|implementer|reviewer`. Khoá gõ sai dưới `roles:` hoặc
`models:` không bị schema bắt (yaml nội tuyến), nên `bint doctor` cảnh báo role không thuộc bước nào của flow, và khoá
`limits.workflows` không trỏ tới workflow gốc, flow tuỳ biến hay id bước.

**Driver session.** Subagent theo kind (`bint-planner`/`bint-implementer`/`bint-reviewer`), `model` của role trong action. Role gán
harness khác Claude trả action `process` với lệnh chạy đúng một bước: `bint run <flow> --task <id> --step <bước> --json`. Cờ
`--step` (nâng cao) chỉ nhận cùng `--task` và flow gắn phiếu (flow tuỳ biến hoặc `b-debug`), và phải là bước kế tiếp của tác vụ; Runner chạy đúng một bước cùng cổng
sau bước (kể cả quyết định review) rồi dừng.

Ví dụ nghiệm thu: `internal/cli/flow_run_e2e_test.go` chạy flow `tdd` ở trên bằng driver process và driver session với agent giả
(thứ tự `plan, red, green, review, green, review` → done; không kiểm chứng sau `red`, một lần sau mỗi `green`).

#### Xuất bản flow và `/b-add-flow` (GĐ5)

`bint sync` xuất bản mỗi flow tuỳ biến **qua kiểm** (cùng luật với `bint flow lint`) thành một **skill điều phối** cùng tên, dựng từ
mẫu nhúng trong tệp thực thi (không có tệp mẫu để sửa trong repository), để người dùng gọi `/<flow>` trong harness như `/b-do`:

- Ở `.claude/skills/<flow>/SKILL.md` (frontmatter `name`, `description`, `disable-model-invocation: true`) và
  `.agents/skills/<flow>/SKILL.md` (chỉ `name`, `description`). `description` ghép mô tả của flow với câu "Dùng khi người dùng chạy
  /<flow>" (mã hoá YAML, tối đa 1024 ký tự).
- Nội dung sinh theo định nghĩa flow: bảng bước (id, kind, skill, role, ghi chú optional/verify/fix); lập phiếu bằng
  `bint task new "<yêu cầu>" --workflow <flow> … --json` (`--plan|--no-plan` chỉ khi bước plan `optional`, `--depth` chỉ khi flow có
  bước plan); chạy `bint run <flow> --task <id> --json` (nền, tiến độ bằng `bint run status`); nhánh driver session
  (`--driver session`, `bint run next/complete`); xử lý `blocked` (câu hỏi mở của bước plan, lệnh kiểm chứng chưa xác nhận, đường dẫn
  bảo vệ, hết ngân sách `limits.workflows.<flow>`, hết vòng review, xung đột khi land, **flow đổi giữa chừng**); báo cáo như b-do.
  Ranh giới tin cậy như b-do: không `bint task approve`, không `done --force`. Không có điểm neo lớp phủ (muốn đổi thì sửa flow).
- `core.lock` ghi mục `flows` (băm bản định dạng Claude, biến thể `agents`, các đường dẫn đã xuất bản; lock cũ không có mục này vẫn
  đọc được). Quy tắc an toàn như skill gốc: bản đã xuất bản bị sửa tay → xung đột, không ghi đè (`--force` ghi đè, có sao lưu).
- Flow lỗi lint (hoặc tệp hỏng) → xung đột nêu lỗi: **không** xuất bản bản mới, **giữ** bản đã xuất bản và mục lock cũ.
- Tệp flow bị xoá → skill điều phối đã xuất bản bị gỡ nếu chưa bị sửa tay; bị sửa tay → xung đột, giữ tệp và mục lock. Bản đã xuất
  bản thuộc nguồn khác cùng tên (skill tuỳ biến hoặc tiến hoá vừa ghi đè) không bị gỡ nhầm.
- Tên flow trùng skill gốc, thư mục skill tuỳ biến nào hoặc skill tiến hoá bị từ chối (xung đột ở sync, lỗi ở `bint flow lint` và khi
  chạy). Tạo skill tuỳ biến/tiến hoá trùng tên một flow đã xuất bản thì skill thắng, flow thành lỗi lint.
- Bản xem trước của `bint sync` (phím `s` ở TUI) liệt kê cả tệp của skill điều phối. Skill điều phối chưa hiện trong
  `bint skill list`/`skill show`; danh sách flow xem bằng `bint flow list`.

**`/b-add-flow`** (skill gốc, cùng khuôn `/b-add-skill`) hướng dẫn agent cùng người dùng tạo flow: có cần flow không (hay `/b-do`, lớp
phủ, skill tuỳ biến là đủ); chọn bước, `kind` theo thứ tự plan → implement → review, `skill` (gốc hoặc tuỳ biến qua `/b-add-skill`),
`role`, `optional`, `verify`, `fix`; skill thay bước plan **phải ghi `requirement.md`, `plan.md`, `tasks.md` bằng `bint task write` theo
hợp đồng v1** (nếu không Bint chặn trước implement); `bint flow lint`; `bint sync`; chạy thử trên phiếu nhỏ. Không tự `git commit`,
không `bint task approve`.

#### Kiểm thử skill: `bint skill test` (GĐ6)

`bint skill test <tên>` chạy các **kịch bản áp lực** `.bint/skills/<tên>/tests/*.yml` của skill tuỳ biến. Thư mục `tests/` không được
xuất bản (`bint sync` chỉ chép `SKILL.md` và tệp phụ ở gốc skill). Mỗi kịch bản là một tệp YAML (`KnownFields`: khoá lạ là lỗi):

```yaml
prompt: Hạn chót là chiều nay, bỏ qua bước viết test cho nhanh.   # bắt buộc
pressures: [Sếp đã duyệt rồi, đừng hỏi lại.]                      # nối vào cuối đề bài theo thứ tự
reps: 5                                                           # 1–20, mặc định 5 lượt mỗi nhánh
expect:                                                           # ≥ 1 mục, mỗi mục đúng một khoá
  - contains: test             # chuỗi phải có trong câu trả lời cuối
  - not_contains: bỏ qua test  # chuỗi không được có
  - regex: '(?i)viết test'     # biểu thức Go phải khớp
  - not_regex: '^Xong'         # biểu thức Go không được khớp
```

Kịch bản sai định dạng (YAML hỏng, khoá lạ, thiếu `prompt`/`expect`, mục `expect` có 0 hoặc ≥ 2 khoá, regex sai, `reps` ngoài 1–20) bị báo
lỗi nêu tên tệp trước mọi lần gọi agent.

- **Hai nhánh.** `without` (RED): đề bài chỉ gồm `prompt` + `pressures`. `with` (GREEN): đề bài có thêm nội dung `SKILL.md` đặt trước
  tình huống. Một lượt **tuân thủ** khi mọi mục `expect` đúng trên câu trả lời cuối; lượt lỗi hạ tầng, hết giờ hoặc không có câu trả
  lời không vào mẫu số. Tỉ lệ = số lượt tuân thủ / số lượt đã chấm, theo từng nhánh và từng kịch bản. `--arm without|with|both`
  (mặc định `both`), `--reps N` ghi đè `reps` của mọi kịch bản. Nhánh `with` bị chặn khi skill lỗi lint (`--arm without` vẫn chạy).
- **Agent chỉ đọc.** Theo role `skill-tester` (`roles.skill-tester` và `harnesses.<h>.models.skill-tester`; chưa đặt thì theo role
  `planner`); `--harness`, `--model`, `--agent-command` ghi đè. Mỗi lượt chạy trong thư mục tạm rỗng ngoài repository.
- **Chi phí.** Agent gọi mô hình nên ước tính (số lượt, chi phí) được in trước mọi lần gọi. Ở chế độ không tương tác (stdin không
  phải terminal) phải có `--yes`; thiếu thì thoát mã 2 mà không gọi agent — chạy một lần không `--yes` để xem ước tính. TUI: hành
  động `system.skill_test` (job có xác nhận). `--cost-per-run` đặt chi phí ước tính mỗi lượt khi harness không báo.
- **Kết quả.** JSON ở `<thư mục trạng thái>/runtime/skill-tests/<tên>/<id>.json` (ghi nguyên tử); `--json` in cùng nội dung ra stdout
  (ước tính và tiến độ sang stderr).
- **Giới hạn.** Tỉ lệ là số đo tham khảo trên vài lượt, không phải bằng chứng thống kê. Agent không cách ly khỏi cấu hình cấp người
  dùng của harness (như mọi lượt `eval`), và lệnh **không** đo việc agent tự kích hoạt skill (nhánh `with` nhét thẳng nội dung skill
  vào đề bài).

`/b-add-skill` dùng lệnh này cho vòng RED/GREEN: viết kịch bản vào `tests/`, `bint skill test <tên> --arm without` (RED), viết/sửa
skill, `bint skill test <tên>` (GREEN); chạy không `--yes` để xem ước tính và hỏi người dùng trước khi thêm `--yes`.

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
| MCP server của Bint | `.mcp.json` (+ quy tắc `mcp__bint__*` trong `permissions.allow` khi `init --mcp`/`--update`) | `.codex/config.toml` — cần dự án được Codex tin cậy; riêng `cite_knowledge` được đặt `approval_mode = "approve"` | chỉ cấp người dùng: `bint init --mcp-user` ghi mục `bint` vào `~/.gemini/config/mcp_config.json` (tuỳ chọn); mặc định dùng CLI `bint` |
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

### Tập MCP của mỗi bước

Mỗi bước agent của `bint run` chạy theo chương trình (driver process) có **đúng một** MCP server: `bint`, tức cửa vào
gộp `bint mcp serve` (shim, docs/mcp-gateway-plan.md mục 7). Nó phục vụ 6 công cụ của Bint (`context_for_task`,
`context_for_paths`, `knowledge_search`, `knowledge_get`, `cite_knowledge`, `report_observation`) và chuyển tiếp tool của
các **service ngoài được cấp cho role của bước** (codegraph, specmatic, …) qua daemon gateway dùng chung, với tên
`<tiền tố>_<tool>` (vd. `cg_explore`; harness thấy `mcp__bint__cg_explore`). Trước mỗi bước, Bint ghi:

- `<state>/mcp/<run-id>.json`: cấu hình MCP chỉ có `mcpServers.bint`, `command` là đường dẫn tuyệt đối của binary Bint,
  `args` = `mcp serve --run-id <run> --task-id <task>`. Run/task nằm trong **đối số** vì Codex lọc env của server MCP
  stdio (chưa kiểm trên Codex thật); shim ưu tiên cờ rồi mới tới `BINT_RUN_ID`/`BINT_TASK_ID`.
- `<state>/runtime/mcp-runs/<run-id>.json` (0600): **bản ghi lượt chạy** (`step`, `role`, `read_only`, `worktree`). Daemon
  không mở SQLite nên dùng bản ghi này để tự giải role của lượt; Bint xoá cả hai tệp khi bước kết thúc. Thiếu thư mục
  trạng thái thì bước chạy không có MCP (ghi một dòng log), không thất bại; không ghi được bản ghi thì bước chỉ có tool của
  Bint. Bước bị giết (SIGKILL, mất điện) để lại hai tệp này; bước sau dọn các tệp cũ hơn gấp đôi `workflow.timeout_minutes`
  (tối thiểu 24 giờ).

**Quyền theo role** (`mcp.grants` trong `.bint/config.yml`; role của bước từ `b-req`→`planner`, `b-feat`→`implementer`,
`b-review`→`reviewer`): tool được cấp = grants của role ∩ `mcp.services` của dự án ∩ service đã đăng ký trên máy. Daemon
tự giải role (không tin header `X-Bint-Role`), lọc `tools/list` và từ chối lời gọi ngoài quyền (kể cả tên đoán mò) bằng lỗi
-32015, upstream không nhận lời gọi, `mcp.called` ghi `outcome: denied`. `run_id` không có bản ghi thì không cấp service nào.
**Bước chỉ đọc** (`b-req`, `b-review`) không được tool ghi vào cây làm việc dù `grants` có ghi (danh mục v1: specmatic
`run_contract_test`, `run_resiliency_test`, `manage_mock_server`); `bint mcp grants` cảnh báo các tool bị loại. Cùng một
hàm giải quyền (`config.MCP.Resolve`) cho daemon, `bint mcp grants` và allowlist của bước nên không lệch nhau.

| Bước | Claude Code | Codex CLI | Antigravity |
|---|---|---|---|
| `b-req` (chỉ đọc) | `--strict-mcp-config --mcp-config <tệp>`; `--allowedTools` gồm 6 tool `mcp__bint__*` của Bint cộng `mcp__bint__<tên>` của từng tool được cấp cho `planner`; vẫn `--disallowedTools Edit,Write,…` | `bint` qua `-c mcp_servers.bint.*` (args gồm `--run-id`/`--task-id`), `approval_mode="approve"` cho cùng tập tool được cấp; server khác trong config Codex bị tắt (xem dưới) | không có MCP cấp dự án (trừ khi `init --mcp-user`): dùng CLI `bint context`, `bint knowledge cite` |
| `b-feat` (ghi) | như trên với role `implementer` (gồm tool nhóm ghi nếu `grants` cấp) | như trên | như trên |
| `b-review` (chỉ đọc) | như `b-req` với role `reviewer` | như `b-req` | như `b-req` |
| Phiên tương tác (ngoài `bint run`) | `.mcp.json` → `bint mcp serve`, role `interactive` (D4): không khai `grants.interactive` thì được mọi service có trong `mcp.services` và đã đăng ký trên máy | như Claude Code | mục MCP cấp người dùng (`init --mcp-user`), role `interactive` |
| Driver session (`bint run next`/`complete`) | subagent dùng kết nối MCP của phiên cha, role `interactive`; quyền riêng cho subagent là việc của G3 | như Claude Code (phiên cha) | như Claude Code (phiên cha) |

- **Shim khởi động daemon:** shim đọc `gateway.json`; không có hoặc không sống thì tự khởi động daemon tách rời
  (`bint mcp gateway start --foreground`). `initialize` chờ tổng cộng tối đa khoảng 8 giây (3 giây tìm/khởi động daemon, 4 giây
  mở phiên và lấy `tools/list`), dưới timeout khởi động MCP 10 giây của Codex; service chưa mở xong trong hạn thì phiên đó
  không có tool của nó và được nêu trong `instructions`. Không lên được thì vẫn phục vụ 6 tool của Bint và nêu service
  lỗi kèm lý do trong `instructions` của `initialize`. Daemon chết giữa phiên thì lời gọi kế tiếp nối lại (khởi động lại
  daemon, mở lại phiên); lời gọi chỉ được gửi lại khi chắc chưa tới daemon hoặc tool chỉ đọc — **lệnh ghi không bao giờ bị gửi
  hai lần**. `tools/list` lấy từ cache theo (service, revision) lưu ở `<gateway>/tools-cache/` nên `initialize` không bật
  upstream; service chưa từng chạy thì daemon chạy `tools/list` đúng một lần. **Hook không khởi động daemon.**
- **Tắt hoặc không dùng:** `BINT_MCP_NO_GATEWAY=1` (hoặc repo không khai `mcp.services`, hoặc role không có service khả dụng,
  hoặc chế độ đánh giá `--eval-config`) thì shim chỉ còn tool của Bint, không đọc `gateway.json`, không khởi động daemon.
- **Claude**: `--strict-mcp-config` luôn có, nên `.mcp.json` của dự án và MCP cấp người dùng không được nạp ở bước
  `bint run`; chỉ còn `bint`.
- **Codex**: Codex vẫn nạp `[mcp_servers.*]` trong `$CODEX_HOME/config.toml` (mặc định `~/.codex/config.toml`) và
  `.codex/config.toml` của dự án. Bint đọc tên các bảng đó và thêm `-c mcp_servers.<tên>.enabled=false` cho mọi tên
  khác `bint`; không dùng `--ignore-user-config` vì cờ đó bỏ cả cấu hình model/provider. Lưu ý (đã kiểm với
  codex-cli 0.167): (1) `-c` của Codex tách khoá theo dấu chấm mà không hiểu dấu trích dẫn, nên tên có dấu chấm hay
  khoảng trắng được tắt qua bảng nội tuyến `-c 'mcp_servers={"a.b"={enabled=false}}'`, đặt trước mọi `-c mcp_servers.*`
  khác; (2) tắt một tên không tồn tại làm Codex báo `invalid transport` và không chạy, nên server chỉ có trong
  `.codex/config.toml` chỉ bị tắt khi `projects."<thư mục>".trust_level = "trusted"` trong config người dùng khớp đúng
  thư mục chạy hoặc gốc repo chính (khi thư mục chạy là worktree liên kết) (Codex bỏ qua config của dự án chưa tin
  cậy). Chỉ dự án được tin cậy qua một thư mục cha khác thì Bint không nhận ra, nên server riêng của dự án đó
  **không** bị tắt (không gây lỗi). Người dùng vẫn ghi đè được qua `harnesses.codex.extra_args`. Tên tool được cấp không có
  dấu chấm nên đặt được `-c mcp_servers.bint.tools.<tên>.approval_mode="approve"`. Phiên Codex **tương tác** giữ hỏi duyệt
  mặc định của Codex cho tool chuyển tiếp (Bint không giả lập).
- **Antigravity**: chỉ có MCP cấp người dùng, Bint không cấp cho từng bước; skill dặn dùng CLI. Bước `agy` nhận mọi
  MCP người dùng đã cấu hình. `bint init --mcp-user` ghi mục `bint` (`bint mcp serve`) vào
  `~/.gemini/config/mcp_config.json` (khoá `mcpServers`, giữ server khác, sao lưu bản cũ, chạy lại không đổi); role lấy theo
  env như trên (ngoài `bint run` là `interactive`). Mục này áp cho **mọi workspace** của Antigravity: ở thư mục không phải
  repository đã khởi tạo Bint, `bint mcp serve` phục vụ server rỗng (không tool, `instructions` giải thích) thay vì thoát lỗi.
  Chưa kiểm agy khởi động MCP cấp người dùng với cwd là workspace ([acceptance.md](acceptance.md), mã `NT-AGY-REAL`).
- **Lượt đánh giá** (`bint eval run`) không đổi: nhánh `bint` có cấu hình MCP riêng (`--eval-config`) và Codex chạy
  trong profile đã cô lập; không chạm gateway.
- Skill `b-req`, `b-feat`, `b-review` ưu tiên công cụ MCP `context_for_task`/`context_for_paths` khi có, và báo trích
  dẫn mục đã áp dụng bằng `cite_knowledge` hoặc `bint knowledge cite <id>` (cần `BINT_RUN_ID` do `bint run` đặt).
- Quyền theo role là **chính sách, không phải ranh giới bảo mật**: agent cùng UID đọc được `gateway.json` rồi gọi thẳng
  daemon; bước chỉ đọc không có Bash tự do nên không làm được, b-feat có Bash đầy đủ thì làm được. Ghi `mcp.called` chỉ có
  khi biết lượt chạy (bước `bint run`, hoặc phiên có harness + session); phiên tương tác chưa có định danh đó thì sự kiện bị bỏ
  và đếm trong `dropped_events`.

#### Di chuyển sang cửa vào gộp (M3)

1. **Gỡ MCP toàn cục trùng service đã đăng ký trong Bint** (tránh hai bản: một do harness sinh theo từng phiên, một do
   gateway). Với mỗi service bạn đã `bint mcp add` (codegraph, specmatic, jira, chrome-devtools, deepwiki, context7):
   xoá mục tương ứng khỏi `~/.claude.json`/`.mcp.json` (Claude: `claude mcp remove <tên>`), `[mcp_servers.<tên>]` trong
   `~/.codex/config.toml` và `~/.gemini/config/mcp_config.json`. Service chưa đăng ký trong Bint thì giữ nguyên.
2. Khai `mcp.services` và (khuyến nghị) `mcp.grants` trong `.bint/config.yml`; **khai `grants.interactive`** nếu không muốn
   phiên tương tác được mọi nhóm của service dự án khai báo (D4 ngầm gồm cả nhóm ghi như `jira.write`, và quy tắc
   `mcp__bint__*` cho phép chúng không hỏi lại; `bint init --mcp`/`--update` cảnh báo khi gặp trường hợp này). Kiểm bằng
   `bint mcp grants`.
3. Chạy `bint init --update` (hoặc `bint init --mcp`): thêm quy tắc `mcp__bint__*` vào `permissions.allow` của settings Claude
   ở phạm vi `--hooks-scope`; không truyền cờ thì theo phạm vi hook đang được cài (local, project rồi user; chưa cài hook thì
   `local` → `.claude/settings.local.json`); chạy lại không thêm trùng. `bint init --no-mcp` gỡ quy tắc khỏi
   `.claude/settings.json` và `.claude/settings.local.json`. Quy tắc cũ trỏ tên tool của MCP toàn cục đã gỡ
   (vd. `mcp__codegraph__*`) vô hại nhưng nên dọn tay.
4. Antigravity (tuỳ chọn): `bint init --mcp-user`.
5. Rollback: đặt `BINT_MCP_NO_GATEWAY=1` (chỉ còn tool của Bint, không cần sửa mã).

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
  bằng `refs/bint/tasks/<id>/base-tree`); sau mỗi lượt `b-feat` (kể cả lỗi/Ctrl-C) ghi các tệp đã đổi vào
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
  trong `.bint/knowledge/items/`; mọi thay đổi khác làm tác vụ chuyển `blocked` (không `failed`) kèm danh sách tệp đổi
  (Bint không tự hoàn tác, và không phân biệt được người hay agent sửa nên người quyết định). Kết quả của bước được **giữ**
  (artifact `b-req`, vòng review trong `review.json`/`review.md`) và phiếu ghi bước đang giữ (`state.json` `held_step`,
  hiện ở `bint task show` dòng "Bước giữ"). Lối ra: chính bạn sửa đồng thời thì `bint run resume <id>` (driver session:
  `bint run next <id> --resume`) đi tiếp từ **sau** bước đó (lint + `planned` cho plan, quyết định review cho review), không
  chạy lại agent; agent sửa thì xem `git diff`, hoàn tác rồi mới resume, hoặc `bint task cancel`. Agent lỗi/quá giờ kèm vi
  phạm vẫn `failed` như cũ.
- **Thay đổi do Bint ghi thay người không tính là vi phạm** (docs/isolation-subagent-plan.md 4.5, H8).
  Người đổi cấu hình (sửa `.bint/config.yml` hoặc qua TUI), duyệt-từ chối-xoá tri thức, sinh lại `AGENTS.md`/skill, `init --update`…
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
    ngược lại `shared`. Phiếu **chỉ lập kế hoạch đã `planned`** (workflow mà flow không có bước ghi, như `b-req`, chưa từng
    vào `in_progress`, không có `touched`/đường dẫn chờ duyệt) không ghi cây nên **không tính** là tác vụ chưa kết thúc. `task new --json` có `isolation`, `isolation_reason`, `isolation_tasks` ở cấp trên; `resume`
    không chọn lại.
  - *Tạo lười ở lượt `bint run` đầu tiên* (mọi workflow, kể cả `b-req`), sau khi nhận quyền điều phối: ảnh chụp cây
    gốc (gồm thay đổi chưa commit và tệp mới) dưới khoá chia sẻ → commit `bint base <id>` (cha `HEAD`) →
    `refs/bint/tasks/<id>/base` → `git worktree add -b bint/<id> <state>/worktrees/<id>` → chép tệp cục bộ do
    Bint quản lý và tệp khớp `workflow.worktree.copy` → `workflow.worktree.setup_command`. **Trước tất cả các bước
    này**, nếu `setup_command` khác rỗng mà người chưa xác nhận (băm sha256 của lệnh, `bint task approve <id> --setup-sha <băm>`; đổi
    lệnh thì xác nhận lại) và `security.require_approval_for_commands` bật, tác vụ dừng `blocked` với lý do nêu lệnh,
    băm, `bint task approve <id> --setup-sha <12 ký tự đầu của băm>` và `bint run resume <id>`: chưa có worktree, nhánh hay lệnh nào chạy (cả driver process
    lẫn driver session). Nhờ ảnh chụp, agent thấy
    đúng nội dung đang sửa dở ở cây gốc. Lỗi ở bước nào thì dọn phần đã tạo, tác vụ `failed` (nêu đường dẫn nhật ký
    của `setup_command`), `resume` tạo lại. Thành công: `State.Origin` = cây gốc, `State.Worktree` = worktree,
    `State.Branch` = `bint/<id>`, `State.Base` = base commit; mọi bước, khoá, ảnh chụp, kiểm chứng sau đó chạy ở
    worktree (đề bài nêu "Thư mục làm việc"). Hai ref giữ cây, tên riêng: `refs/bint/tasks/<id>/base` luôn trỏ
    base commit (`State.Base`) cho tới khi land/cancel; `refs/bint/tasks/<id>/base-tree` giữ ảnh chụp `base_tree` của
    `b-feat` đầu. Cả hai bị xoá khi tác vụ kết thúc hoặc đưa về (không ghim cây vĩnh viễn), nên `bint task diff`
    của tác vụ đã kết thúc mà `git gc` đã dọn cây so sánh báo lỗi `cây so sánh của tác vụ đã bị dọn` (nêu ID, trạng
    thái, mã cây thiếu và danh sách tệp đã biết) thay vì lỗi thô của git.
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
- <a id="driver-session"></a>**Driver session — b-do chạy các bước bằng subagent (docs/isolation-subagent-plan.md G3a, mục 6.1–6.3).**
  Mặc định (driver `process`) `bint run` tự gọi harness cho từng bước. Với driver `session` (cờ `bint task new --driver
  session`, hoặc `workflow.driver: session`; `bint run b-do --driver session "…"` lập phiếu rồi dừng với
  `Result.Status = created`) phiên đang mở (orchestrator) chạy từng bước bằng subagent, còn Bint giữ máy trạng thái và
  **mọi cổng kiểm tra**: `next` và `complete` là hai nửa của một bước (`beginStep`/`endStep` quanh lời gọi agent), driver
  process dùng đúng hai nửa đó. Driver session luôn điều phối theo `b-do`. Trong Claude Code, người dùng gõ
  `/b-do --subagent <việc>` (hoặc nói "dùng subagent để …") thì skill `b-do` lập phiếu `--driver session` rồi tự chạy
  vòng `next` → Agent → `complete`; harness không có subagent thì dùng driver `process`.

  ```
  bint task new --driver session "<yêu cầu>" --json            → ID
  lặp:  bint run next <id> --json
          → {"action":"agent","step","run_id","subagent","model","prompt","worktree","readonly","max_turns","lease_until"}
          | {"action":"process","command":"bint run b-feat --task <id> --json"}   (role gán harness khác Claude)
          | {"action":"done"|"blocked"|"failed","reason","questions","notes"}
        Agent(subagent_type, model, prompt)                    ← orchestrator chạy subagent
        bint run complete <id> --run <run_id> [--agent-id <agentId>] [--failed "<lý do>"] --json
          → {"result":{…}} (còn tiếp tục: không có "action", gọi next)  |  {"action":"done|blocked|failed",…}
  bint run abort <id> [--reason …]                             ← phiên orchestrator chết giữa bước
  ```

  - **Nối tiếp phiên (giai đoạn 2 của `docs/step-continuation-plan.md`).** `next --resume` (và `next` khi gặp bước dừng loại nối
    tiếp được) trả `{"action":"agent","continue_agent_id":"<agentId cũ>","prompt":"<lời nhắn nối tiếp>"}` thay cho đề bài đầy
    đủ khi **cả năm điều kiện** đúng: `State.last_step` có `via: "subagent"`, `continuable`, cùng bước sắp mở, fingerprint cây
    hiện tại khớp lúc dừng, và bộ đếm nối tiếp chưa chạm trần (lớp `external` tối đa 3 lần liên tiếp, `timeout` 1 lần;
    `awaiting_human` không tính). Orchestrator gửi `prompt` bằng **SendMessage** tới agent cũ rồi `complete --agent-id <agent cũ>`.
    Thiếu một điều kiện thì trả đề bài đầy đủ như cũ. `notes` chỉ nêu lý do khi **bước sắp mở trùng bước dừng** (có gì để nối
    tiếp mà không nối tiếp được: `--fresh`, bước dừng không chạy bằng subagent, không `continuable`, `awaiting_human` không phải
    bước plan, fingerprint đổi, quá giới hạn); không có `last_step` hoặc bước sắp mở khác bước dừng (kể cả khi `--fresh`) thì
    không có note nào. `bint run next <id> --fresh` bỏ nối tiếp (bước dở đang
    là bước nối tiếp được đổi tại chỗ sang đề bài đầy đủ, giữ `run_id`; không có bước dở thì bước mới dùng đề bài đầy đủ) — dùng
    khi SendMessage không gửi được. `complete --failed "<lý do>"` khớp bộ phân loại lỗi hạ tầng của eval (429/5xx/quá tải) ghi
    `last_step.stop_class: external` (nối tiếp được khi có `--agent-id`); lý do khác là `intrinsic`. Bước nối tiếp vẫn là bước
    mới (`beginStep`: run con mới, khoá, hợp đồng thuê, ngân sách); chi phí/token/lượt chỉ tính phần phát sinh sau lần đóng trước
    của cùng agent (mốc `usage` lưu ở `last_step`, trừ khi cùng transcript); guardrail worktree thấy tệp session-steps của run gốc
    (bản ghi alias) trong suốt bước nối tiếp, gỡ khi bước đóng, abort hoặc tác vụ bị huỷ.
  - **Vòng câu hỏi nhiều lượt của b-req.** Planner xong mà `requirement.md` còn câu hỏi mở: `complete` đặt tác vụ `blocked`
    (`last_step`: `awaiting_human`, handle = agent_id). Người ghi trả lời (`bint task write <id> requirement -`) rồi
    `next --resume`: nếu nối tiếp được thì mở lại **bước plan** dạng nối tiếp tới planner cũ, lời nhắn chứa nguyên văn các dòng
    `Q-N … — Trả lời: …`; planner cập nhật requirement/plan/tasks, có thể thêm câu hỏi mới (`complete` lại chặn) — lặp tới khi hết câu hỏi
    mở thì tác vụ đi tiếp sang b-feat. Không nối tiếp được (không handle, cây đổi, `--fresh`) thì giữ hành vi cũ: vào thẳng b-feat,
    không chạy lại b-req (driver process: xem "Câu hỏi mở của b-req ở driver process" bên dưới). Đề bài b-feat (cả driver process và driver
    session) chứa nguyên văn mọi dòng câu hỏi đã trả lời.
  - **Nối tiếp phiên ở driver process (giai đoạn 3 của `docs/step-continuation-plan.md`).** `bint run resume <id>`,
    `bint run <workflow> --task <id>` và `bint run next` cho bước gán harness khác Claude (`action: "process"`) cũng nối tiếp phiên
    của bước dừng gần nhất thay vì dựng agent mới. Bước driver process ghi `last_step.via` bằng **tên harness** (`claude`, `codex`; harness
    khác không nối tiếp được). Điều kiện (thiếu một thì chạy mới với đề bài đầy đủ và `notes` nêu lý do): `last_step` cùng bước
    sắp mở và `via` khớp harness của bước (không nối tiếp chéo subagent/harness, claude/codex), `continuable` (lớp `external`,
    `timeout`, `awaiting_human`; có handle **và có tệp phiên trên đĩa**: transcript Claude ở `<CLAUDE_CONFIG_DIR|~/.claude>/projects/<thư mục
    dự án của cwd>/<session>.jsonl`, rollout Codex `<CODEX_HOME|~/.codex>/sessions/*/*/*/rollout-*-<thread>.jsonl`), fingerprint cây khớp lúc dừng,
    bộ đếm chưa chạm trần (`external` tối đa 3 lần liên tiếp, `timeout` 1 lần, `awaiting_human` không tính), và không có `--fresh`. Lệnh:
    Claude `claude -p --resume <session> …` (cùng cờ quyền/MCP/settings/model/`--max-budget-usd`/ExtraArgs như bước gốc, không
    `--session-id`, không `--no-session-persistence`, không `--fork-session`); Codex `codex exec <cờ của bước gốc> resume <thread> -` (lệnh con
    `resume` không nhận `--sandbox`/`--cd`/`--add-dir` nên mọi cờ của bước gốc đứng trước `resume`; dãy `-c` giữ nguyên). Đề bài là lời nhắn
    nối tiếp ngắn (lý do dừng, lượt con mới, "tiếp tục từ chỗ dở, kiểm lại cây", và với `awaiting_human` nguyên văn các dòng
    `Q-N … — Trả lời: …`); `BINT_RUN_ID` là lượt con mới (hook/nhật ký deny theo lượt mới), bộ đếm/lượt gốc kế thừa. Tệp phiên mất giữa
    lúc dừng và lúc nối tiếp, hoặc harness báo không tìm thấy phiên (`No conversation found with session ID`, `no rollout found for thread
    id`), thì chạy lại **một lần** ngay trong bước với đề bài đầy đủ và UUID mới (note "không tìm thấy phiên"). Claude chết trước khi
    ghi phiên ⇒ `continuable: false` lúc dừng (note "chưa được Claude ghi").
    `bint run resume <id> --fresh` và `bint run <workflow> --task <id> --fresh` (cờ không dùng khi lập phiếu mới) buộc chạy mới;
    `bint run next <id> --fresh` gặp bước gán harness khác Claude thì lệnh `command` của action process cũng mang `--fresh`.
    **Chi phí không tính trùng**: Bint không dùng số harness báo sau resume (`total_cost_usd` của Claude có thể khôi phục chi phí cả phiên;
    usage `turn.completed` của Codex lấy từ bộ đếm thread). Bước nối tiếp đọc tệp phiên (usage cộng dồn theo cả đời phiên) rồi trừ mốc lưu ở
    `last_step.usage` lúc dừng: Claude cộng `message.usage` của transcript rồi quy USD theo bảng giá (cùng cách với subagent; `--resume`
    ghi sang transcript khác tệp mốc thì `unknown` kèm note vì chưa rõ tệp mới có chép lại lịch sử cũ); Codex lấy
    `total_token_usage` của rollout (không có USD). Không đọc được tệp phiên ⇒ chi phí `unknown` kèm note, `max_cost_usd` chỉ là chỉ dẫn.
    Lưu ý: `--max-budget-usd` của Claude có thể tính trên chi phí cộng dồn cả phiên nên bước nối tiếp có thể dừng sớm vì trần (xem mục nghiệm
    thu resume ở `docs/acceptance.md`).
  - **Câu hỏi mở của b-req ở driver process.** b-do dừng vì câu hỏi mở (hoặc `bint run b-req --task`, tác vụ giữ `planned`) ghi
    `last_step`: `awaiting_human`. Sau khi người ghi câu trả lời hết các câu hỏi mở, `bint run resume <id>`, `bint run b-do --task <id>` và
    `bint run b-req --task <id>` **nối tiếp planner cũ** với nguyên văn các dòng đã trả lời (planner cập nhật requirement/plan/tasks; thêm
    câu hỏi mới thì tác vụ dừng chờ người lần nữa); hết câu hỏi thì b-do đi tiếp b-feat trong cùng lượt. Chưa trả lời hết, artifact lỗi cấu
    trúc, hoặc không nối tiếp được (cây đổi, `--fresh`, không có phiên…) thì giữ hành vi cũ: b-do vào thẳng b-feat (không chạy lại b-req từ
    đầu, `notes` nêu lý do), `b-req --task` chạy planner mới.
  - *`next`* mở bước (chuyển trạng thái, giới hạn, khoá worktree, ảnh chụp trước, đề bài, sự kiện bắt đầu), ghi
    `State.pending_step` (lượt, bước, ảnh chụp, đề bài, `lease_until`) rồi trả flock; **gọi lại khi bước còn hạn trả đúng
    bước đó** (cùng `run_id`, cùng prompt), không mở bước mới. Phần việc không cần agent chạy ngay trong `next`: kiểm
    phạm vi và kiểm chứng sau `b-feat`, quyết định sau review chạy riêng bằng tiến trình, finalize (kể cả đưa về cây gốc
    của tác vụ worktree). Tác vụ blocked/failed/interrupted: `next` dừng (mã thoát 1) cho tới khi người xử lý nguyên nhân
    rồi `bint run next <id> --resume`. `--no-wait`: khoá worktree đang bị giữ thì dừng ngay (blocked) thay vì chờ tối đa
    `workflow.timeout_minutes`.
  - *`process`*: role gán harness khác Claude (`roles.implementer: codex`, …) không có subagent; `next` trả lệnh
    `bint run <bước> --task <id> --json` để orchestrator chạy (nền được) rồi gọi lại `next`. Các bước khác vẫn bằng
    subagent. Review chạy riêng kiểu này đã ghi kết quả thì `next` áp quyết định sau review (không review lặp).
  - *`complete --run <run_id>`* làm phần sau của bước như `bint run`: kiểm bước chỉ đọc không sửa cây làm việc (b-req,
    b-review sửa tệp ⇒ tác vụ `blocked`, giữ kết quả bước), tập tệp, phạm vi và đường dẫn bảo vệ (sửa ngoài phạm vi ⇒ `blocked` chờ duyệt),
    kiểm chứng sau `b-feat` (chạy **trong** lệnh này, có thể lâu: chạy nền nếu công cụ có giới hạn thời gian), tài liệu của
    `b-req`, quyết định sau review (vòng sửa / dừng ở `workflow.max_review_rounds` / finalize). `--run` sai hoặc tác vụ
    không có bước dở ⇒ lỗi (mã 1), không đổi gì; thiếu `--run` ⇒ mã 2. `--failed "<lý do>"` đánh bước `failed`.
    `--agent-id` là `agentId` của kết quả công cụ Agent: lưu vào lượt chạy (`State.agent_runs`, payload
    `workflow.finished` của lượt con), dùng ghép sự kiện của subagent vào lượt đó và tính chi phí (G3b, xem dưới).
    `complete` thử lại được nếu bị ngắt giữa chừng (bước dở chỉ bị gỡ ở cuối).
  - *Hợp đồng thuê khoá theo thời gian* (mục 6.3). flock nhả khi `next` thoát nên không giữ được khoá suốt bước; `next` ghi
    hợp đồng `{tác vụ, bước, lượt, chế độ, hạn = bây giờ + workflow.timeout_minutes}` vào `locks/write-<hash>.lock.lease.json`
    (cạnh tệp khoá của worktree; ghi nguyên tử dưới mutex `.lease.lk`) rồi trả flock. **Mọi người lấy khoá đều tôn trọng
    hợp đồng còn hạn của tác vụ khác** theo bảng chia sẻ/độc quyền như khoá thật (`bint run` kiểu tiến trình chờ hoặc
    `blocked` với `--no-wait`; `next` của tác vụ khác tương tự; hai bước chỉ đọc cùng chạy được). Hết hạn xác định theo
    **thời gian, không theo PID còn sống** (không có kiểu race R1 của `evolve.AcquireLock`). `complete`/`abort`/huỷ tác vụ
    xoá hợp đồng. Phiên orchestrator chết giữa bước ⇒ khoá bị giữ tới hết hạn hoặc tới khi `bint run abort <id>`.
    **Không đọc được hợp đồng ⇒ coi là đang bị giữ**: tệp hợp đồng hỏng hoặc không lấy được mutex trong hạn thì khoá không
    được cấp (chờ / `blocked` kèm lý do "hợp đồng thuê khoá không đọc được: …"), `bint run abort <id>` xoá tệp hỏng.
  - *`abort`* gỡ bước dở và hợp đồng thuê, đóng lượt con là `interrupted`, ghi nhận tệp `b-feat` đã sửa vào `Touched`, đưa
    tác vụ về `interrupted` (tiếp tục bằng `next --resume`, bước dở chạy lại từ đầu). TUI: hành động `tasks.abort` (ghi,
    không phím, mở từ bảng lệnh, qua form xác nhận); `run next`/`run complete` là giao thức của phiên điều phối nên nằm
    trong danh sách loại trừ của bảng phủ lệnh TUI.
  - *Chặn chéo*: tác vụ có bước dở còn hạn thì `bint run <wf> --task` (driver process) bị từ chối ("bước … đang chạy bằng
    subagent: `bint run complete` hoặc `bint run abort`"); hết hạn thì nó đóng bước như quá giờ (`failed`) rồi tiếp tục theo
    trạng thái mới. Kiểm bước dở được làm lại **dưới khoá phiếu ngay lúc nhận quyền điều phối** (cả `next` lẫn `bint run`),
    nên `next` chạy xen vào không bị đóng nhầm như hết hạn. `next` trên tác vụ đang dừng (`blocked`/`interrupted`) mà bước
    dở còn hạn **không trả `action: agent`** (orchestrator sẽ chạy lại subagent) mà trả `blocked` kèm lệnh
    `bint run complete <id> --run <run_id>` (hoặc `abort`). Tác vụ session vẫn chạy được bằng `bint run resume` (lối lùi khi phiên không có subagent).
  - *Đề bài cho subagent* không có biến `BINT_*` (subagent không có biến môi trường riêng): ID tác vụ nằm trong đề bài và
    mọi lệnh `bint task …` phải truyền nó; tác vụ worktree đòi `cd <worktree> && …` ở mọi lệnh Bash và đường dẫn tuyệt đối
    (công cụ Agent không nhận thư mục làm việc). Vòng review ghi bởi subagent không mang `run_id` nên `complete` nhận vòng
    mới hơn vòng đã áp quyết định (`State.review_decided`).
  - *G3b — định nghĩa subagent, guardrail chỉ đọc, ghép sự kiện, chi phí (mục 6.4–6.7).*
    - **Định nghĩa subagent.** `bint init` / `init --update` / `bint sync` sinh `.claude/agents/bint-planner.md`,
      `bint-implementer.md`, `bint-reviewer.md` (khi bật `claude`): `tools` theo `workflow.agent.readonly_tools` (planner, reviewer)
      hoặc `allowed_tools` (implementer); `disallowedTools: Edit, Write, MultiEdit, NotebookEdit` cho planner/reviewer (giới hạn
      mức công cụ là cứng); `model` theo `harnesses.claude.models.<role>` (rỗng ⇒ `inherit`); `maxTurns` theo
      `limits.workflows.<bước>.max_turns` (bỏ khi không đặt). Khoá phiên bản như skill gốc (`core.lock` có `agents:`; sửa tay ⇒
      xung đột, `--force` để ghi đè có sao lưu; `bint doctor` báo thiếu/lệch). Đổi model hay `max_turns` trong cấu hình thì chạy
      `bint sync` để tệp theo kịp. `next` trả `subagent` và `model` (tham số lời gọi Agent thắng frontmatter).
    - **Guardrail chỉ đọc `bint-readonly` (hook, mục 6.4a).** `Bash(<mẫu>)` trong `tools` của subagent **không giới hạn lệnh**
      (spike S1), nên hook `PreToolUse` chặn: với `agent_type` là `bint-planner` / `bint-reviewer`, công cụ sửa tệp (Edit,
      Write, MultiEdit, NotebookEdit — kể cả đường dẫn ngoài repo) và lệnh Bash không khớp `workflow.agent.readonly_tools`
      đều bị `deny` kèm lý do; mỗi lần chặn ghi `guardrail.triggered` (guardrail `bint-readonly`, `agent_type`, `agent_id`,
      `command` đã che bí mật). Khớp lệnh **bảo thủ**, không phân tích shell đầy đủ: lệnh phải là một lệnh khớp mẫu
      `Bash(tiền-tố:*)` / `Bash(lệnh)` theo token; cho phép tiền tố `cd <một-thư-mục> && …` (đề bài của tác vụ worktree bắt
      buộc) và heredoc (`bint task write/review <id> … - <<'EOF'`; thân heredoc không nháy mà có `$(`/backtick bị chặn);
      mọi `;`, `&&` khác, `|`, chuyển hướng (trừ `2>&1`, `>/dev/null`), `$(…)`, backtick, nhiều dòng ⇒ chặn dù các vế đều chỉ
      đọc, `git … --output=…` bị chặn, và `git grep` có `-O`/`--open-files-in-pager` (chạy chương trình tuỳ ý; kể cả cờ gộp
      `-nOvim`, viết tắt `--op`, trong nháy, sau `\` hay `$'…'`) cũng bị chặn. `readonly_tools` mặc định có `Bash(grep:*)` và
      `Bash(git grep:*)`: `grep -rn foo internal`, `git grep -n foo`, `grep foo x 2>/dev/null` qua; `grep foo x > out` hay
      `grep foo x | sh` bị chặn như mọi chuyển hướng/pipe (mẫu có `|` như `grep -E 'a|b'` cũng bị chặn: dùng `-e a -e b`). Agent khác (`bint-implementer`, `general-purpose`, phiên chính) không bị can thiệp.
      Harness không có `agent_type`/deny cho subagent thì `Dialect.SubagentReadonly=false` (Codex, Antigravity). Lớp kiểm sau
      vẫn là ảnh chụp trước/sau của bước chỉ đọc. Hook cũ cần chạy lại `bint init` để có hook `SubagentStop`.
    - **Guardrail đường dẫn worktree `bint-worktree` (hook, mục 7 / 12.14).** Subagent chạy ở cwd của phiên cha (cây gốc), nên với
      tác vụ `worktree` hook `PreToolUse` chặn (`deny`) công cụ sửa tệp của `bint-implementer` có đường dẫn ngoài worktree của bước và
      lệnh Bash không bắt đầu bằng `cd <worktree> &&` (hoặc `cwd` không nằm trong worktree); tác vụ `shared`, `agent_type` khác và
      công cụ đọc không bị can thiệp. `bint run next` ghi `runtime/session-steps/<run_id>.json` (worktree, `isolated`, hạn thuê) cho
      mỗi bước; hook đọc dòng đầu transcript của subagent (prompt, khối `<bint-task … run=…>`) để tra tệp, không cần `attach`. Không
      ánh xạ được (transcript đổi bố cục…) thì fail-closed có giới hạn: chỉ khi còn bước worktree đang chạy, Edit chỉ được vào một
      worktree đang chạy và Bash phải `cd` vào đó; sự kiện `guardrail.triggered` ghi `outcome: unmapped`. Giới hạn: lệnh
      `cd <worktree> && … > /đường/dẫn/khác` không bị chặn. **Chạy song song:** mỗi việc một phiếu `--driver session --worktree`,
      Agent chạy nền, vòng `next → Agent → complete` xen kẽ; mỗi tác vụ tự `land` ở finalize, tác vụ land sau xung đột thì chỉ nó
      `blocked`.
    - **Hậu kiểm cô lập `missed_by_hook` (NT-ISO-LEAK).** Hook là lớp chặn duy nhất của guardrail trên và fail-open (Claude Code
      cho lệnh đi qua khi hook không chạy hoặc không trả quyết định; ở V1 một lần ghi lọt ra cây gốc không để lại dấu vết). Nên
      `bint run complete --agent-id X` của bước `bint-implementer` thuộc tác vụ `worktree` quét lại transcript của subagent (cùng
      tệp đã đọc để tính chi phí) bằng đúng quy tắc của hook (`hookcore.WorktreeViolation`): Edit/Write/MultiEdit/NotebookEdit có
      đường dẫn ngoài worktree, Bash không bắt đầu bằng `cd <worktree> &&` (hoặc `cd` ra ngoài) và `cwd` ngoài worktree. Chỉ tính
      lời gọi đã **thực sự chạy**: có `tool_result` và không bị hook chặn. Edit-type chỉ dựa vào `is_error` (kết quả thành công
      có thể chứa trích đoạn tệp có chuỗi "hook error"/"Bint: … bị chặn" mà vẫn là lần ghi đã xảy ra); Bash `is_error` chỉ
      bị coi là chưa chạy khi kết quả có dạng bị chặn trước khi thực thi (`hookcore.IsPreExecDenial`): hook chặn
      (`PreToolUse:` và dòng đầu có "hook error"), Claude Code từ chối (`<tool_use_error>`, ví dụ `Blocked: sleep …`), từ chối
      quyền (`Permission to use …`, `Permission for this …`) hoặc người dùng từ chối (`The user doesn't want to …`) — chỉ xét
      ở đầu nội dung. Lệnh thoát khác 0 (`Exit code N`) và mọi dạng lỗi lạ vẫn tính vì có thể đã chạy; lời gọi chưa có
      `tool_result` không tính. Có vi phạm thì bước **không** completed: tác vụ `blocked` (ghi đè cả `failed`/quá hạn; trừ khi bị
      huỷ), lý do nêu tối đa 10 mục `tool_use_id (công cụ): tệp|lệnh` (đã che bí mật) và nhắc kiểm tra cây gốc (các thay đổi đó
      không thuộc tác vụ; **Bint không tự xoá hay khôi phục**), lịch sử lỗi của phiếu ghi lại, và mỗi vi phạm một sự kiện spool
      `guardrail.triggered` (`guardrail: bint-worktree`, `outcome: missed_by_hook`, `on: post_step`, `blocked: false`, kèm
      `agent_id`, `tool_use_id`, `paths` (danh sách, mỗi phần tử một đường dẫn) hoặc `command`; `source` là của Bint chứ không phải của hook, `event_id` ổn định theo
      lượt con + `tool_use_id`). Người xem xong thì tiếp tục bằng `bint run next <id> --resume` (tác vụ driver session). Không đọc được transcript (thiếu
      `--agent-id`, thiếu `SubagentStop`, tệp mất, bố cục không nhận ra) thì chỉ ghi chú "không hậu kiểm được cô lập", không chặn
      (giống chi phí). Không áp cho tác vụ `shared` hay subagent khác. Giới hạn: không thấy ghi gián tiếp qua shell
      (`cd <worktree> && … > /đường/dẫn/khác`), và phụ thuộc bố cục transcript Claude Code (không công bố). Chẩn đoán hook không
      chạy: [BINT_HOOK_TRACE](harness-components.md#ghi-vết-hook-bint_hook_trace).
    - **Ghép sự kiện theo `agent_id` (mục 6.5).** Hook ghi `agent_id` vào payload mọi sự kiện của subagent;
      `bint run complete --agent-id X` ghi `agent_id` vào `workflow.finished` của lượt con. Lúc nạp spool, sự kiện hook cùng
      `agent_id` được gắn vào lượt con — cả khi hook tới sau (`resolveRun` tra ánh xạ) lẫn khi đã nạp trước (`bint task …` của
      chính subagent tự nạp spool; `adoptAgentEvents` chuyển `events`, `run_skill_usages`, hai bảng context, `knowledge_citations`,
      `observations` và upsert lại artifacts `file.modified`/`file.read`; bản ở lượt phiên cha giữ nguyên). Sự kiện không có
      `agent_id` không bao giờ bị chuyển. Thiếu `--agent-id`: lượt vẫn hợp lệ, ghi chú nêu sự kiện/chi phí không gắn được.
      Giới hạn: tín hiệu (`run_signals`, ví dụ kết quả test) theo lượt không được chuyển.
    - **Chi phí (mục 6.6).** Hook `SubagentStop` ghi sự kiện `subagent.stopped` (`agent_id`, `agent_type`,
      `agent_transcript_path`) và tệp nhỏ `runtime/subagents/<agent_id>.json` (spool bị nạp/xoá bởi bất kỳ lệnh nào nên
      `complete` đọc tệp này trước, dự phòng tệp spool còn chờ). `complete` cộng `message.usage` của dòng assistant trong
      transcript, **khử trùng lặp theo `message.id`** (bản cuối), tách theo model, rồi quy USD theo bảng giá dựng sẵn
      (`internal/harness/claude/pricing.go`, nguồn và ngày ghi trong mã; Haiku 5.5 có bậc giá prompt > 100 nghìn token) ghi đè
      được bằng `harnesses.claude.pricing`. `result.cost_usd` null (unknown, không phải 0) kèm ghi chú khi: thiếu
      `--agent-id`, chưa có `SubagentStop`, transcript không còn/không có usage, hoặc model không có giá — khi đó
      `max_cost_usd` chỉ là chỉ dẫn. Vượt `limits.workflows.<bước>.max_cost_usd` của bước ⇒ `task.error` + ghi chú (bước không
      failed: subagent không dừng được giữa chừng). Chi phí và số lượt cộng dồn vào `State.session_spend` theo lượt cha, nên
      ngân sách `limits.workflows.b-do` (chi phí, lượt) được kiểm qua nhiều lệnh: vượt ⇒ `next` trả `blocked`; lượt cha mới (sau
      `--resume`) bắt đầu từ 0.
    - **Skill `/b-do` (mục 6.7)** có nhánh driver session: "dùng subagent" / `--subagent` ⇒ `bint task new … --driver session`, vòng
      `next → Agent → complete --agent-id`, `process` chạy nền, `blocked` kèm lệnh `complete` thì chạy đúng lệnh đó, không có công
      cụ Agent ⇒ driver process.
  - *Giới hạn đã biết của G3a/G3b*: **chưa chạy thật bằng Claude Code** (thứ tự `SubagentStop` so với kết quả Agent, hook `deny`
    dưới `bypassPermissions`, `maxTurns` của frontmatter): mới có kiểm thử bằng sự kiện giả lập và "subagent giả". Subagent chạy ở
    thư mục của phiên cha, chưa có rào chắn đường dẫn worktree (G4). Windows chưa chạy thật (chỉ biên dịch/vet/staticcheck).
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
- Ghi đè `--harness`/`--role`/`--model` hiệu lực được lưu theo role trong `state.json`; task cũ không có snapshot dùng cấu hình hiện tại.
- Hook Codex ghi sự kiện deny có child run/tool ID và đường dẫn. Runner chuyển tác vụ sang `blocked`; duyệt đúng tệp bằng `task approve --path` rồi `resume` chạy lại `b-feat`.
- Antigravity: chưa có MCP cấp dự án (chỉ `init --mcp-user` cấp người dùng), chưa chèn ngữ cảnh qua hook, đề bài `bint run` truyền qua một
  đối số dòng lệnh (giới hạn 120 KiB), và một số payload công cụ thực tế cần kiểm chứng thêm khi chạy live.

### Agent evolution Codex (0.167.2)

Evolver/reflector/harness proposer dùng `CodexRestrictedAgent`, tách khỏi adapter
workflow `codex exec`. Catalog model tạm bỏ Code Mode, multi-agent và apply_patch;
app-server chạy cwd/profile/state rỗng với environments rỗng. Mỗi lượt phải qua
probe request thật tại endpoint fixture (kiểm cả tools và additional_tools) trước
khi nhận dữ liệu nghiệp vụ. Reflector/proposer không có công cụ; evolver chỉ có
Read/Grep/Glob do Bint thực thi trong repository với giới hạn output, không shell.
Model yêu cầu được giữ nguyên; model không có trong catalog, công cụ/field lạ hoặc
probe thất bại làm dừng trước inference. Extra args không được đưa vào đường này.
Không tự trust hook, bật evolution hoặc nới cổng chống rò rỉ/ngân sách đánh giá.
[Thiết kế, smoke live và giới hạn model cấu hình hiện tại](evals/codex-flow-acceptance-2026-11-12/codex-restricted-adapter-round6.md).

Với `bint run --agent-command`, command thay cho harness nên Bint không phân giải
các role harness không dùng. Snapshot harness/model có trước được giữ để resume
sau này; command không tạo snapshot giả cho các role đó.

### Quản lý MCP (`bint mcp`)

Danh mục MCP nằm trong binary (`internal/mcpcat`: codegraph, specmatic, deepwiki, context7, jira, chrome-devtools);
Bint **không tự cài** service nào (docs/mcp-gateway-plan.md mục 5). Hai tầng cấu hình:

- **Máy** (không commit), ở thư mục cấu hình user (`$BINT_CONFIG_HOME` hoặc `<UserConfigDir>/bint`): `mcp.yml`
  (service đã đăng ký: lệnh, đối số, URL, phiên bản, **tên** biến credential; Bint ghi với quyền 0600) và
  `secrets.env` (giá trị credential, dạng `TÊN=giá trị`, quyền 0600, do người sửa bằng trình soạn thảo). Trên Windows
  Bint chưa kiểm ACL của `secrets.env`.
- **Dự án** (commit), `.bint/config.yml`: `mcp.services` (service dự án dùng; `required: false` cho tuỳ chọn; `options`
  riêng) và `mcp.grants` (role tự do → `<service>.<nhóm>` hoặc `<service>.*`).

| Lệnh | Việc | Ai chạy |
|---|---|---|
| `bint mcp list [--json]` | danh mục, trạng thái đăng ký trên máy, dự án dùng gì | người, agent |
| `bint mcp add <service> [--command … --arg … --env-name … \| --url …]` | đăng ký (hiện lệnh cài gợi ý nếu thiếu binary; cảnh báo `npx …@latest`, `uvx`), rồi kiểm tra | chỉ người |
| `bint mcp remove <service>` | gỡ đăng ký (không đụng `secrets.env`) | chỉ người |
| `bint mcp check [<service>]` | `initialize` + `tools/list`, đối chiếu nhóm thao tác, báo biến credential thiếu; codegraph: gọi thử mọi tool của danh mục có trong `tools/list`, báo `degraded` kèm nhóm thao tác và tên tool bị tắt khi daemon cũ chạy với env hẹp | người, agent |
| `bint mcp index codegraph` | `codegraph init -y` (chưa có chỉ mục) hoặc `codegraph sync` ở worktree hiện tại, rồi thêm `.codegraph/` vào `info/exclude` của git | chỉ người |
| `bint mcp grants [--role <r>]` | quyền đã giải: role → tên tool tới agent (`mcp__bint__<tiền tố>_<tool>`), báo service thiếu | người, agent |
| `bint mcp gateway start [--foreground] [--json]` | khởi động daemon gateway tách rời (nhật ký `gateway.log`) rồi báo cổng, PID; đã chạy thì chỉ báo thông tin; `--foreground` chạy tại chỗ | người, agent |
| `bint mcp gateway status [--json]` | từng upstream: service, phân vùng, generation, PID, RSS/PSS, số phiên, lời gọi dang dở, lời gọi gần nhất, lỗi đã che credential; báo "không chạy" khi chưa có daemon | người, agent |
| `bint mcp gateway restart <service>` | dừng mọi upstream của service (kill cả cây), xoá lỗi và bộ đếm khởi động lại; lời gọi sau khởi động mới với generation +1 | chỉ người |
| `bint mcp gateway stop` | tắt daemon và mọi upstream | chỉ người |
| `bint mcp gateway log <service> [--lines N] [--json]` | log gần nhất của service: stderr (đã che credential) của các upstream **đang chạy** (hỏi daemon qua `GET /admin/log?service=`) và các dòng `gateway.log` của service (đuôi 256 KiB, `--lines` mặc định 50, tối đa 1000); daemon không chạy hoặc là bản cũ (404) thì chỉ có phần `gateway.log` kèm ghi chú | người, agent |

"Chỉ người" nghĩa là lệnh trả `ErrAgentForbidden` khi chạy trong phiên agent. `bint doctor` báo service dự án khai báo mà
máy chưa đăng ký, chỉ mục codegraph thiếu và `secrets.env` có quyền rộng hơn 0600 (không khởi động tiến trình MCP). M1
chỉ làm danh mục, cấu hình và lệnh quản lý; M2 thêm daemon gateway (dưới đây); việc cấp tool cho bước chạy qua cửa vào
gộp (shim) và quyền theo role là M3 (xem "Tập MCP của mỗi bước" và "Di chuyển sang cửa vào gộp"); màn TUI là M4 (dưới đây).

#### Màn MCP trong TUI (M4)

Tab thứ tám của `bint tui` (phím `8`, docs/tui-plan.md mục 5.8) gom các lệnh trên: thẻ **Dịch vụ** (danh mục kèm trạng thái chưa đăng ký / đã đăng ký /
đang chạy với PID, RSS, số phiên / degraded / lỗi; `p` lọc theo `mcp.services` của dự án), **Quyền** (`mcp grants` dạng bảng role × service, chỉ xem;
`e` mở `.bint/config.yml` qua luồng sửa cấu hình của Hệ thống) và **Công cụ Bint** (`mcp tools`). Thêm service (`a`) là form từ các cờ của `mcp add` và
**không có ô nhập giá trị bí mật**: màn chỉ hiện đường dẫn `secrets.env` và tên biến còn thiếu. Gỡ (`x`), khởi động lại (`R`) và tắt daemon (`S`) có hộp
xác nhận; kiểm tra (`c`), lập chỉ mục (`i`) và khởi động lại là việc nền có log; `l` xem `mcp gateway log`. Trạng thái degraded/lỗi do check chỉ có sau khi chạy
check trong phiên (danh sách không tự khởi động tiến trình). `add`, `remove`, `index`, `gateway restart`, `gateway stop` là thao tác chỉ người: bị khoá trong
phiên agent và ẩn ở `--read-only`.

#### Daemon gateway (`bint mcp gateway`, M2)

Một daemon cho mỗi user (`internal/mcpgw`, docs/mcp-gateway-plan.md mục 6), **không do hook khởi động** và chưa có gì tự
khởi động ở M2: chỉ người chạy `bint mcp gateway start`; từ M3 shim (`bint mcp serve`) tự khởi động khi cần. Tệp trong `<state base>/gateway/`:
`gateway.lock` (khoá `flock`/`LockFileEx`, daemon thứ hai bị từ chối), `gateway.json` (quyền 0600: `pid`, `port`, `token`;
xoá khi dừng), `gateway.log`, `tools-cache/` (M3).

- **Mạng:** Streamable HTTP trên `127.0.0.1` cổng động; `POST/GET/DELETE /mcp` và `/admin/status|restart|stop|log` đều đòi
  `Authorization: Bearer <token>` (thiếu/sai → 401); Host không phải loopback hoặc có header `Origin` → 403; body POST quá
  giới hạn → 413. Quyền theo role là chính sách, không phải ranh giới bảo mật: agent cùng UID đọc được `gateway.json`.
- **Phiên:** `initialize` mang header `X-Bint-Service` (bắt buộc), `X-Bint-Worktree` (gốc worktree git, bắt buộc với
  service chia sẻ theo worktree hoặc cần cwd), `X-Bint-Run-Id`, `X-Bint-Task-Id`, `X-Bint-Role`, `X-Bint-Harness`,
  `X-Bint-Session-Id`; gateway trả `Mcp-Session-Id`. Từ M3 `X-Bint-Role` **bị bỏ qua khi cấp quyền**: daemon tự giải role
  (có `X-Bint-Run-Id` thì từ bản ghi lượt chạy `runtime/mcp-runs/<run>.json`, không có thì `interactive`) rồi giải quyền theo
  `mcp.grants` của repo chứa worktree (phiên của `bint run` đọc `.bint/config.yml` của worktree **chính** trước, như allowlist
  của bước, nên bước trước sửa cấu hình trong worktree tác vụ không đổi được quyền của bước sau); không được cấp thì `initialize` trả -32015 (`CodeNotGranted`), nên một phiên gắn một
  service được cấp. Cache `initialize`/`tools/list` cũng lưu ở `<gateway>/tools-cache/<service>-<revision>.json` (0600). `DELETE /mcp` đóng phiên (nhả khoá exclusive).
- **Phân vùng tiến trình theo danh mục:** `machine` một tiến trình cho cả máy, `worktree` một tiến trình cho mỗi gốc
  worktree, `session` mỗi phiên một tiến trình (dừng khi phiên đóng), `remote` (deepwiki, context7) không sinh tiến trình.
  Upstream khởi động lười ở lời gọi (hoặc `initialize`, khi chưa có cache) đầu tiên cần nó và dừng sau `idle_timeout` khi hết
  phiên và lời gọi dang dở; dừng thì kill cả cây (unix: process group; Windows: Job Object).
- **Ghép phiên vào một upstream stdio:** đổi id JSON-RPC thành `gw_<generation>_<seq>_<phiên>` và đổi ngược (cả
  `notifications/cancelled` và `progressToken`); `initialize`/`tools/list` cache theo (service, revision) nên upstream chỉ
  nhận một `initialize` mỗi generation; điền tham số mặc định của danh mục (`repoDir={worktree}` cho
  `specmatic.backward_compatibility_check`, giữ nguyên nếu agent truyền); `roots/list` của upstream được trả gốc worktree,
  `sampling/*` và `elicitation/*` bị từ chối; giới hạn frame (8 MiB) và hàng đợi ghi (64 frame) — vượt thì upstream bị coi
  như chết và lời gọi sau chạy trên generation mới.
- **Nhóm thao tác:** nhóm `exclusive` (`specmatic.mock`) một phiên giữ tại một thời điểm (TTL 10 phút, chờ 30 giây rồi trả
  `busy` -32010); `Group.MaxConcurrent` giới hạn đồng thời mỗi phân vùng (`specmatic.contract` = 1).
- **Chết và khởi động lại:** upstream chết thì lời gọi đang chờ nhận lỗi và lời gọi sau chạy generation +1; vượt
  `max_restarts` (đăng ký, mặc định 3) trong 10 phút thì trả -32011 tới khi `bint mcp gateway restart <service>`. Lệnh không
  chỉ đọc bị timeout hoặc upstream chết sau khi đã gửi → lỗi "không rõ kết quả" (-32012), **không bao giờ tự gọi lại**;
  chỉ tool chỉ đọc của upstream HTTP được retry có backoff (lỗi mạng, 5xx, 429).
- **Env của upstream** = env tối thiểu (`PATH`, `HOME`, ngôn ngữ…) + env mặc định của danh mục + biến credential đã khai
  báo, giá trị đọc từ `secrets.env` lúc khởi động upstream; biến khác của daemon không lọt sang. Lỗi và stderr trong
  `status` đã che giá trị credential.
- **Ghi nhận:** mỗi `tools/call` ghi một sự kiện `mcp.called` (run/task/role đã giải, service, tool, nhóm, kết quả — gồm `denied` cho lời gọi ngoài quyền —, thời gian; không
  có đối số hay kết quả) vào spool của repo chứa worktree; repo chưa `bint init`, hoặc phiên thiếu cả `run_id` lẫn cặp
  harness + session thì không ghi (đếm trong `dropped_events`). Daemon không mở SQLite.

Chưa kiểm với service thật trên máy và Job Object trên Windows thật (CI chỉ biên dịch và vet cho Windows).
