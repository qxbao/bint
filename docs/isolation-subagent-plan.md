# Kế hoạch: cô lập tác vụ chạy song song và điều phối b-do bằng subagent

Trạng thái: **đã chốt hướng** (2026-11-11; bổ sung mục 4.5 ngày 2026-11-12). **G1 mục 4.1–4.4 và 4.5 (H8) đã làm**
(2026-11-12, xem mục 12.1–12.5). **G2 (mục 5) đã làm** (2026-11-12, xem mục 12.6); G3–G4 chưa làm. Gồm bốn giai
đoạn: G1 (cô lập trên worktree chung), G2 (mỗi tác vụ một worktree, chế độ `auto`), G3 (driver session: b-do chạy
các bước bằng subagent), G4 (nhiều tác vụ song song bằng subagent).

## 1. Mục tiêu

1. Hai tác vụ không đụng nhau chạy song song được mà **không làm hỏng nhau**: bước của tác vụ này
   không bị tính lỗi, scope, verify hay review vì thay đổi của tác vụ kia.
2. Trường hợp thường gặp nhất (một tác vụ trên cây đang làm) **giữ nguyên trải nghiệm hiện nay**:
   agent sửa thẳng trên nhánh đang làm, người dùng thấy thay đổi ngay trong IDE.
3. Người dùng gõ `/b-do Dùng subagent để …` (hoặc `/b-do --subagent …`) thì phiên đang mở tự chạy
   b-req/b-feat/b-review bằng subagent, **mà Bint vẫn giữ mọi cổng kiểm tra**: khoá, kiểm bước chỉ đọc,
   scope, đường dẫn bảo vệ, verify, finalize, số vòng review.
4. Không nới quy tắc hook [k:01M746NPTW1B304MRQQPBCAFAM]: hook không trả lỗi, không mở SQLite, không gọi
   mô hình, không giả lập sự kiện harness không gửi.

Ngoài phạm vi: tự trộn thay đổi xung đột (mục 6.5 của `bint-plan.md`), driver session cho Codex làm
orchestrator, trần chi phí cứng bên trong một bước chạy bằng subagent.

## 2. Hiện trạng

`bint run` chạy mỗi bước bằng một tiến trình harness riêng (`eval.Agent`, `internal/eval/agent.go`)
với `Dir = r.Root`, tức worktree của thư mục đang đứng. Mọi kiểm tra tính trên **toàn cây làm việc**:

| # | Chỗ | Hậu quả khi hai tác vụ A, B cùng chạy trên một worktree |
|---|---|---|
| H1 | `internal/workflow/run.go:519-575`: bước chỉ đọc chụp toàn cây trước/sau (`WorktreeSnapshot`) | b-feat của B chạy trong lúc b-review của A ⇒ A `failed` "bước chỉ đọc đã sửa cây làm việc". Khoá ghi không loại trừ bước chỉ đọc. |
| H2 | `checkScope` (`run.go:837`) và `UnapprovedChanges` (`ops.go:401`) dùng `task.ChangedFiles(root, base)` | Tệp của B (và thay đổi chưa commit có từ trước khi lập phiếu) bị tính cho A: cảnh báo ngoài scope, chặn chờ duyệt đường dẫn bảo vệ. Skill b-review `git diff <base>` nên review A soi cả mã B. |
| H3 | `WorktreeFingerprint` (`ticket.go:315`, `ops.go:350`) | B sửa bất kỳ tệp nào ⇒ kết quả verify của A mất hiệu lực. |
| H4 | `AcquireWriteLock` (`isolation.go`) độc quyền theo worktree, không chờ | b-feat thứ hai bị `blocked` ngay. |
| H5 | `State.Worktree`/`State.Branch` được ghi lúc lập phiếu (`ops.go:160`) nhưng không nơi nào đọc | `bint run resume` gõ ở thư mục khác ⇒ chạy trên nhầm cây. |
| H6 | Skill b-do không nhắc worktree; Bint không có lệnh tạo worktree hay đưa thay đổi về | Kế hoạch gốc (`bint-plan.md:605`) yêu cầu worktree khi chạy song song nhưng không có công cụ. |
| H8 | Ảnh chụp của bước chỉ đọc không phân biệt người hay agent sửa | Người dùng sửa cấu hình/duyệt tri thức trong TUI lúc b-req chạy ⇒ bước `failed`. Ca thật: tác vụ 01M76RV9STQEA4A47AY1QC4V8T, 2026-11-12 09:29, "b-req … đã sửa cây làm việc: .bint/config.yml" do đổi `ui.language` trong TUI. |
| H7 | Skill b-do bắt mọi bước đi qua `bint run` | `/b-do Dùng subagent…` hoặc bị lờ đi, hoặc subagent sửa mã mà Bint không biết (bỏ qua mọi cổng). |

Những thứ đã có, dùng lại được:

- Trạng thái Bint nằm theo `git-common-dir`, dùng chung giữa các worktree (`internal/state`).
- `gitrepo.Repo.ConfigRoots` lùi về worktree chính khi `.bint/config.yml` chưa commit.
- `WorktreeSnapshot` dựng được mã cây (tree) cho toàn bộ cây làm việc, kể cả tệp chưa theo dõi.
- Hook đã đọc `agent_id`/`agent_type` của Claude Code (`hookcore/hook.go:882-883`) và ghi
  `agent_type` vào payload (`hook.go:214`).
- Guardrail `pre_edit`/`pre_bash` với `action: deny` đã là cơ chế hợp lệ của hook (trả quyết định, không
  trả lỗi), xem `harness-components.md`.
- `bint run <bước> --task <id>` chạy được từng bước riêng.

## 3. Quyết định đã chốt

| Vấn đề | Quyết định |
|---|---|
| Worktree có làm mặc định không | Không. `workflow.isolation: auto` (mặc định) \| `shared` \| `worktree`. `auto` = sửa thẳng trên cây đang làm, chỉ dùng worktree khi cây đó đang có tác vụ khác chưa kết thúc, hoặc người dùng yêu cầu song song / `--worktree`. |
| Lúc nào chọn chế độ | Một lần khi lập phiếu, ghi vào phiếu. `resume` không chọn lại. |
| Thay đổi trong worktree về đâu | Về **cây đã lập phiếu** (`State.Origin`), dưới dạng thay đổi chưa commit, không đụng index. Áp sạch thì tự đưa về ở finalize; xung đột thì `blocked` chờ người. |
| `done` của tác vụ worktree | Chỉ `done` sau khi đã đưa về thành công. |
| Driver | `workflow.driver: process` (mặc định, như hiện nay) \| `session`. `/b-do --subagent` hoặc câu tự nhiên tương đương ⇒ `session` cho phiếu đó. |
| Trần trong driver session | `maxTurns` của định nghĩa subagent là trần cứng mỗi bước; chi phí được tính lúc `complete` và chặn **giữa các bước**. Không thêm hook chặn theo chi phí. |
| Role gán harness khác Claude (vd `implementer: codex`) | Bước đó tự chạy kiểu tiến trình (`bint run <bước> --task`), các bước khác vẫn bằng subagent. |
| `isolation: worktree` có sẵn của Claude Code | Không dùng: worktree đó tách từ nhánh mặc định, không mang thay đổi chưa commit. Bint tự tạo worktree. |

## 4. G1 — Cô lập trên worktree chung

Mục tiêu: sửa H1–H5 ở mức không cần worktree mới. Đây cũng là lưới an toàn cho chế độ `shared`.

### 4.1. Gắn tác vụ với worktree của nó (H5)

- `run`, `resume`, `task verify`, `task done`, `task diff` (mục 4.3) lấy Root từ `t.State.Worktree`;
  phiếu cũ không có trường này thì dùng cwd như hiện nay.
- Cwd khác `State.Worktree`: chạy ở `State.Worktree` và in một dòng "chạy trong <đường dẫn>". Thư mục
  không còn tồn tại thì `failed` với thông báo rõ (không lùi về cwd).
- `Env.Root` hiện là trường chung; thêm hàm `Env.RootFor(t)` thay vì đổi ngữ nghĩa `Root` để các lệnh
  không gắn tác vụ (eval, knowledge…) không bị ảnh hưởng.

### 4.2. Khoá đọc/ghi và chờ khoá (H1, H4)

- `AcquireWriteLock` thành `AcquireLock(tasksDir, root, owner, mode)` với `mode` = `Shared` (b-req,
  b-review) | `Exclusive` (b-feat, `task land`). Unix: `flock(LOCK_SH|LOCK_NB)` / `LOCK_EX|LOCK_NB`;
  Windows: `LockFileEx` có/không `LOCKFILE_EXCLUSIVE_LOCK`. Giữ cặp `flock_unix.go`/`flock_windows.go`
  cùng chữ ký và kiểm `GOOS=windows go vet` + staticcheck [k:01M746NASVKV9XMRHA4QNHJ3R8].
- **Điểm dễ sai:** `Release` hiện xoá tệp khoá rồi mới đóng. Với khoá chia sẻ, xoá tệp khi người đọc
  khác còn giữ sẽ làm người đến sau mở tệp mới và lấy được khoá độc quyền song song. Bỏ việc xoá tệp
  khoá khi trả (tệp nhỏ, một tệp mỗi worktree); `sameFile` giữ lại cho tương thích. Tệp khoá của
  worktree tác vụ được dọn khi `task land`/huỷ. Không chép mẫu `evolve.AcquireLock` [k:01M746PTBYPSXDT0WJZS38KPFD].
- Chờ khoá: mặc định chờ (thăm dò có giãn cách, in "chờ khoá do <tác vụ> <bước> giữ"), tối đa
  `workflow.timeout_minutes`; Ctrl-C ⇒ `interrupted` như hiện nay. Cờ `--no-wait` để lỗi ngay
  (`blocked`, như hành vi cũ). Không thêm khoá cấu hình.
- Bước chỉ đọc giữ khoá chia sẻ suốt bước, nên trong `shared` chỉ còn **người dùng** sửa tệp mới làm
  thay đổi ảnh chụp. Sửa qua Bint (TUI/CLI) được miễn theo mục 4.5; sửa bằng IDE vẫn bị tính, thông báo
  vi phạm thêm: "nếu chính bạn sửa trong lúc bước chạy: bint run resume".

### 4.3. Tập tệp của từng tác vụ (H2)

- Bước ghi cũng chụp cây trước và sau. Thêm vào `task.State`:
  - `BaseTree string`: ảnh chụp trước lần b-feat đầu tiên (gồm cả thay đổi chưa commit có sẵn). Giữ
    sống bằng `git commit-tree` + `update-ref refs/bint/tasks/<id>/base` để `git gc` không dọn.
  - `Touched []string`: hợp các tệp đổi giữa ảnh trước/sau của mọi lượt b-feat (kể cả vòng sửa review).
- `checkScope` và `UnapprovedChanges` dùng `Touched`; phiếu cũ (`Touched == nil`) dùng `ChangedFiles` như
  cũ. Nhờ vậy thay đổi chưa commit có sẵn trước khi lập phiếu cũng thôi bị tính cho tác vụ.
- Lệnh mới `bint task diff <id> [--stat|--name-only]`: `git diff <BaseTree> -- <Touched>` so với cây
  làm việc (gồm tệp mới). Skill b-review chuyển từ `git diff <base>` sang lệnh này.
- Chồng lấn: sau b-feat trong `shared`, nếu `Touched` giao với `Touched` của tác vụ chưa kết thúc khác
  cùng worktree ⇒ `blocked` "tác vụ <id> cũng sửa: <tệp>", không tự xử lý.
- Giới hạn có chủ ý: người dùng sửa đúng tệp đó trong lúc b-feat chạy thì phần sửa bị tính cho tác vụ
  (không phân biệt được ai sửa). Ghi trong `workflows.md`.

### 4.4. Verify (H3)

Giữ fingerprint toàn cây: verify chạy cả bộ kiểm thử nên thay đổi của tác vụ khác làm kết quả cũ mất
nghĩa thật. Chỉ đổi thông báo của `DoneBlockers`: liệt kê tệp đã đổi **ngoài** `Touched` kể từ lần
verify ("thay đổi không thuộc tác vụ: …; chạy lại bint task verify <id>"). Cần lưu danh sách tệp
(hoặc tree) lúc verify bên cạnh fingerprint.

### 4.5. Thay đổi do Bint ghi thay người không tính là vi phạm (H8)

Phần lớn thay đổi của người trong lúc agent chạy đi qua chính Bint: TUI/CLI sửa cấu hình, duyệt/từ
chối/làm mới/áp dụng tri thức, sinh lại `AGENTS.md` và skill, `init --update`. Bint biết chính xác mình
vừa ghi gì nên tự loại các thay đổi đó khỏi kiểm tra bước chỉ đọc.

- **Nhật ký ghi.** Mọi thao tác ghi tệp trong repo của tầng dịch vụ (`internal/app`, dùng chung cho CLI
  và TUI) đi qua một hàm ghi chung, hàm này nối thêm một dòng vào
  `<state>/runtime/writes.jsonl`: `{path, sha256 sau khi ghi, at, actor, agent: InAgentSession()}`.
  Đặt ở tầng dịch vụ để TUI không thể ghi vòng qua nhật ký. Nối thêm bằng `O_APPEND`, một dòng mỗi lần
  ghi; dọn dòng cũ hơn `workflow.timeout_minutes` × 2 khi nối.
- **Đối chiếu.** `ReadonlyViolations` nhận thêm thời điểm bắt đầu bước. Một đường dẫn đổi giữa hai ảnh
  chụp **không** tính là vi phạm khi dòng nhật ký mới nhất của nó: ghi trong khoảng bước chạy, có
  `agent: false`, và hash khớp nội dung hiện tại của tệp. Tệp bị xoá: ghi dòng có `sha256: ""`.
- **Chốt an toàn.**
  - Agent tự gọi `bint config set`/`bint knowledge …` trong bước chỉ đọc ⇒ dòng nhật ký có
    `agent: true` ⇒ vẫn là vi phạm.
  - Agent sửa tiếp đúng tệp đó sau người ⇒ hash lệch ⇒ vẫn là vi phạm.
  - Tệp nhật ký nằm trong thư mục trạng thái mà bước chỉ đọc được ghi (`WritableDirs`), nên agent có thể
    tự chèn dòng giả. Chấp nhận được vì mục đích là chặn nhầm chứ không phải chống agent cố tình: lớp
    chặn chính của bước chỉ đọc vẫn là `--disallowedTools` của harness, và kết quả bước có ghi danh sách
    tệp được miễn để người xem lại. Ghi giới hạn này trong `workflows.md`.
- **Kết quả bước** liệt kê tệp được miễn (`StepResult.HumanWrites`) và in một dòng "bỏ qua thay đổi do
  người ghi qua Bint: …".
- **TUI báo trước.** Khi cây đang có bước của tác vụ chạy (đọc `bint run status`/khoá), TUI hiện dòng
  trạng thái "<bước> của <tác vụ> đang chạy trên cây này". Thao tác ghi vẫn được phép, không cần dặn
  người dùng tránh TUI.
- **Không chữa được:** người dùng sửa bằng IDE hoặc công cụ ngoài Bint trong lúc bước chỉ đọc chạy. Muốn
  vừa sửa vừa để agent chạy thì dùng `isolation: worktree` (G2).
- Hook không ghi vào nhật ký này (hook không ghi tệp trong repo).

### 4.6. Nghiệm thu G1

- Kiểm thử đồng thời (hai `Runner` với agent giả trong cùng thư mục tạm): b-review của A không fail khi
  B có b-feat chờ; B chờ tới lúc A trả khoá rồi chạy.
- Kiểm thử khoá: chia sẻ + chia sẻ cùng lúc được; chia sẻ chặn độc quyền và ngược lại; tiến trình giữ
  khoá chết ⇒ khoá tự nhả; không có tình huống hai người cùng giữ độc quyền khi trả khoá xen kẽ
  (chạy `-race -count=50`).
- `checkScope` không tính tệp chưa commit có sẵn trước khi lập phiếu, không tính tệp của tác vụ khác.
- `bint run resume` ở thư mục khác chạy đúng `State.Worktree`.
- Tái hiện ca H8: agent giả (bước chỉ đọc) chạy, trong lúc đó gọi dịch vụ sửa `ui.language` và duyệt một
  mục tri thức với vai người (`asHuman(t)`) ⇒ bước `completed`, kết quả liệt kê hai tệp được miễn. Cùng
  thao tác nhưng chạy với biến agent ⇒ vi phạm. Người sửa rồi agent giả sửa tiếp cùng tệp ⇒ vi phạm. Sửa
  tệp trực tiếp (không qua dịch vụ) ⇒ vi phạm.
- Kiểm thử liệt kê: mọi hàm ghi tệp repo trong `internal/app` đi qua hàm ghi chung (không còn
  `os.WriteFile`/`fsafe` gọi thẳng trên đường dẫn repo ngoài hàm đó).
- `GOOS=windows go vet ./...` và staticcheck qua.

## 5. G2 — Mỗi tác vụ một worktree

### 5.1. Chọn chế độ (lúc lập phiếu)

- Cờ `bint task new --worktree | --shared`; `bint run` khi tự tạo phiếu nhận cùng cờ.
- `task.Classification` thêm `Isolation` (`shared` | `worktree`) và `IsolationReason`
  (`concurrent_task` | `user` | `config` | `default`).
- Quy tắc `auto`: tồn tại tác vụ **chưa kết thúc** (khác `done`, `cancelled`) có `State.Worktree` bằng
  cây hiện tại và `Isolation = shared` ⇒ `worktree`, lý do `concurrent_task`, kèm ID tác vụ đó trong
  JSON. Ngược lại ⇒ `shared`. Tác vụ `failed`/`interrupted` cũng tính vì có thể còn thay đổi dở trên cây.
- JSON của `task new` có `isolation`, `isolation_reason` để orchestrator báo người dùng.

### 5.2. Tạo worktree (lười, ở bước đầu tiên của `bint run`)

Tạo ở lần chạy đầu thay vì lúc `task new` để phiếu lập rồi bỏ không để lại worktree mồ côi.

1. `State.Origin` = cây đã lập phiếu.
2. Ảnh chụp cây gốc (`WorktreeSnapshot`, có cả thay đổi chưa commit và tệp mới) ⇒
   `git commit-tree <tree> -p HEAD -m "bint base <id>"` ⇒ `refs/bint/tasks/<id>/base`.
3. `git worktree add -b bint/<id> <state>/worktrees/<id> <base-commit>`. Worktree nằm trong thư mục trạng
   thái (ngoài repo) để IDE/công cụ quét repo không thấy cây lồng nhau.
4. Chép các tệp khớp `workflow.worktree.copy` (glob, tệp bị gitignore như `.env`) từ cây gốc; chạy
   `workflow.worktree.setup_command` nếu có (vd `npm ci`), lỗi ⇒ `failed` kèm nhật ký.
5. `State.Worktree` = worktree mới, `State.Branch` = `bint/<id>`, `State.Base` = base commit, nên
   `ChangedFiles`, fingerprint, khoá, ảnh chụp… tự động chỉ còn phần của tác vụ.

Khoá cấu hình mới (`workflow.isolation`, `workflow.worktree.copy`, `workflow.worktree.setup_command`) phải
có đủ trường `yaml`, `Default()`, `Validate()` và dòng có chú thích trong `template.go`
[k:01M746NPB0Y4D5S2D47EFTJ8T4]; chú thích có `%` phải viết `%%`.

### 5.3. Đưa về: `bint task land <id>`

- Tính bản vá: `git diff --binary <Base> <ảnh chụp worktree tác vụ>` (gồm tệp mới; ứng viên tri thức mới
  trong `.bint/knowledge` đi theo bản vá).
- Lấy khoá độc quyền trên **cây gốc**, `git apply --check` rồi `git apply` (không `--index`, không đụng
  chỉ mục của người dùng). Thay đổi xuất hiện ở cây gốc dưới dạng chưa commit, như chế độ `shared`.
- Không áp sạch ⇒ không đụng gì ở cây gốc, `blocked` với lý do "xung đột khi đưa về: <tệp>" và hướng
  dẫn. `bint task land <id> --3way` (chỉ người, chặn bằng `InAgentSession`) để lại dấu xung đột cho người
  gỡ; `--3way` của git ngầm dùng `--index`, ghi rõ trong trợ giúp.
- Thành công: ghi `State.Landed{At, Files, By}`, gỡ worktree (`git worktree remove`), xoá nhánh
  `bint/<id>`, ref base và tệp khoá. `--keep` giữ worktree để xem lại.
- Finalize của b-do với tác vụ worktree: kiểm điều kiện done như hiện nay (verify chạy **trong worktree**)
  ⇒ land ⇒ `done`. Land lỗi ⇒ `blocked`; sau khi người gỡ xong và `land` thành công thì `resume` chuyển
  `done`.
- Báo cáo ghi rõ: verify đã chạy trên cây cô lập; cây gốc có thể còn thay đổi khác chưa được kiểm cùng.

### 5.4. Dọn dẹp

- `bint task cancel <id>` với tác vụ worktree: gỡ worktree. Worktree còn thay đổi chưa đưa về ⇒ cần
  `--discard` (chỉ người).
- `bint doctor` báo worktree mồ côi (thư mục trong `<state>/worktrees` không ứng với tác vụ chưa kết
  thúc, hoặc `git worktree list` có mục `prunable`) và gợi ý lệnh dọn.
- `bint run status <id>` hiện đường dẫn worktree, nhánh và trạng thái đưa về.

### 5.5. Skill (A3)

- **b-do:** bước "chế độ cô lập" sau Classify: đọc `isolation`/`isolation_reason` trong JSON; người
  dùng nói "song song" thì lập phiếu bằng `--worktree`; báo người dùng tác vụ đang chạy ở đâu. Bước báo
  cáo thêm: worktree, đã đưa về hay chưa, xung đột cần gỡ.
- **b-review:** phạm vi mặc định là `bint task diff <id>`.
- **b-feat:** không đổi cách làm; nhắc rằng cwd là worktree của tác vụ.
- Sửa ở `internal/skills/core/*`, đồng bộ `.claude/skills`, `.agents/skills`, `.bint/skills/core.lock`
  như commit 5136884.

### 5.6. Nghiệm thu G2

- E2E (agent giả bằng `--agent-command`): hai tác vụ lập liên tiếp trên một cây ⇒ tác vụ hai tự vào
  worktree; cả hai chạy đồng thời tới `done`; cây gốc nhận đủ thay đổi của cả hai, chỉ mục không đổi.
- Base có thay đổi chưa commit: agent trong worktree thấy đúng nội dung tệp đang sửa dở ở cây gốc.
- Xung đột: người dùng sửa cùng dòng ở cây gốc trong lúc tác vụ chạy ⇒ `blocked`, cây gốc không bị đụng;
  `land --3way` từ phiên agent bị từ chối.
- `cancel` gỡ worktree; `doctor` báo worktree mồ côi.
- Kiểm thử chạy dưới phiên agent phải xoá biến agent cho thao tác chỉ-người (`asHuman(t)`)
  [k:01M746PHJXDZN6QMA3BEVEDNPP].

## 6. G3 — Driver session (b-do chạy các bước bằng subagent)

### 6.1. Luồng

```
/b-do Dùng subagent để <yêu cầu>
  bint task new "<yêu cầu>" --driver session --json        → T1
  lặp:
    bint run next T1 --json
      → {action: "agent", step, run_id, subagent: "bint-implementer", model, prompt, worktree, readonly}
      | {action: "process", command: "bint run b-feat --task T1 --json"}   (role gán harness khác)
      | {action: "done" | "blocked" | "failed", reason, questions}
    Agent(subagent_type, model, prompt)                     ← chạy nền nếu được
    bint run complete T1 --run <run_id> [--failed "<lý do>"] --json
      → Bint làm phần sau bước như bint run: kiểm chỉ đọc, scope, artifact, vòng review…
```

`next`/`complete` là hai nửa của `runStep` hiện tại, máy trạng thái và mọi cổng nằm ở Bint. Orchestrator
chỉ chuyển prompt và kết quả.

### 6.2. Tách Runner

- Tách `runStep` (`run.go:451`) thành `beginStep` (chuyển trạng thái, giới hạn, khoá/ảnh chụp, prompt,
  `step.started`) và `endStep` (kiểm chỉ đọc, `StepResult`, `workflow.finished`, `AwaitingCheck`) quanh
  `agent.Run`. `Run` (driver process) gọi `beginStep → agent.Run → endStep → afterStep → nextStep` như cũ.
  Kiểm thử hiện có của `run_test.go` phải qua nguyên vẹn.
- Driver session lưu bước đang dở trong `State.PendingStep{RunID, Step, Snapshot, Model, MaxTurns,
  StartedAt, LeaseUntil}`. `next` khi đã có `PendingStep` chưa hết hạn ⇒ trả lại đúng bước đó (gọi lại an
  toàn), không mở bước mới.
- `complete` chấp nhận đúng `run_id` đang dở; sai ⇒ lỗi.

### 6.3. Khoá khi tiến trình `next` đã thoát

flock nhả khi `next` thoát, nên driver session không giữ được khoá hệ điều hành suốt bước. Dùng **hợp
đồng thuê theo thời gian** ghi trong tệp khoá của worktree:

- `next` lấy flock trong chốc lát, đọc hợp đồng thuê; còn hạn và khác chủ ⇒ chờ/`blocked` như mục 4.2;
  không thì ghi hợp đồng `{task, step, run_id, mode, until = now + timeout_minutes}` rồi trả flock.
- Người lấy khoá kiểu tiến trình (`bint run`) cũng phải tôn trọng hợp đồng thuê còn hạn.
- `complete` (hoặc `bint run abort <id>`) xoá hợp đồng thuê dưới flock.
- Hết hạn xác định theo **thời gian**, không theo PID còn sống, nên không có kiểu race R1 của
  `evolve.AcquireLock` [k:01M746PTBYPSXDT0WJZS38KPFD]. Phiên orchestrator chết giữa bước ⇒ khoá bị giữ tới
  hết hạn hoặc tới khi người chạy `bint run abort`.

### 6.4. Định nghĩa subagent do Bint cài

`bint init` / `init --update` sinh `.claude/agents/bint-planner.md`, `bint-implementer.md`,
`bint-reviewer.md` (cơ chế cài và khoá phiên bản như skill gốc):

- `tools`: theo `workflow.agent.allowed_tools` / `readonly_tools`; `disallowedTools: Edit, Write,
  NotebookEdit` cho planner và reviewer.
- `model`: theo `harnesses.claude.models.<role>` (rỗng ⇒ `inherit`). `next` cũng trả `model` để
  orchestrator truyền vào lời gọi Agent (tham số lời gọi được ưu tiên hơn frontmatter).
- `maxTurns`: theo `limits.workflows.<bước>.max_turns`. Đây là trần cứng mỗi bước. Phần còn lại của
  ngân sách lượt cả b-do được kiểm giữa các bước (không truyền động được vào frontmatter).
- Nội dung: vai trò ngắn, dẫn tới skill b-req/b-feat/b-review; prompt thật do `next` trả.

### 6.5. Gắn sự kiện vào lượt chạy (thay cho biến môi trường)

Subagent không có biến môi trường riêng (`BINT_RUN_ID`, `BINT_TASK_ID`, `BINT_APPROVED_PATHS`):

- Task ID nằm trong prompt; lệnh `bint task …` vốn nhận `<id>`.
- `BINT_APPROVED_PATHS`: đọc từ trạng thái phiếu.
- Thao tác chỉ-người vẫn bị chặn vì phiên cha có `CLAUDECODE=1`.
- Lệnh đầu tiên trong prompt: `bint run attach <run_id>`. Hook thấy lệnh này kèm `agent_id`/`agent_type`
  và chỉ ghi chúng vào spool như mọi sự kiện. Lúc nạp spool, Bint ghép `agent_id` ⇒ `run_id`, rồi gắn
  các sự kiện cùng `agent_id` vào lượt đó. Thiếu `attach` ⇒ lượt vẫn hợp lệ, sự kiện không gắn được và
  `complete` ghi chú việc này. Không suy đoán, không tạo sự kiện thay.

### 6.6. Chi phí

- `complete` tính token của bước từ transcript của subagent (nguồn cụ thể chốt ở S2, mục 8).
- Quy đổi USD cần bảng giá theo model. Hiện Bint chưa có bảng giá; thêm bảng dựng sẵn cho model Claude,
  ghi đè được bằng cấu hình. Model không có giá ⇒ chi phí `unknown`, dùng lại đường "không báo chi phí:
  max_cost_usd chỉ là chỉ dẫn" đã có.
- Vượt `limits.workflows.<bước>.max_cost_usd` ⇒ ghi lỗi; vượt ngân sách b-do ⇒ `next` trả `blocked`.

### 6.7. Skill b-do

- Thêm nhánh "driver session": khi người dùng nói dùng subagent (hoặc `--subagent`), lập phiếu với
  `--driver session`, chạy vòng `next → Agent → complete`, xử lý `action: process` bằng lệnh chạy nền,
  dừng khi `done`/`blocked`/`failed` và báo như bước 3–4 hiện tại.
- Không có công cụ subagent (harness khác Claude Code) ⇒ báo và dùng driver process.
- Ranh giới tin cậy giữ nguyên: orchestrator không tự sửa mã, không tự `approve`.

### 6.8. Nghiệm thu G3

- Kiểm thử đơn vị `next`/`complete` bằng "subagent giả" (kịch bản ghi tệp/gọi `bint task …`): đủ chuỗi
  b-req → b-feat → b-review → done; b-review sửa tệp ⇒ `complete` đánh `failed`; sửa ngoài scope ⇒
  `blocked` như driver process; vòng review dừng đúng `max_review_rounds`.
- `next` gọi hai lần liên tiếp trả cùng bước; `complete` với `run_id` sai bị từ chối.
- Hợp đồng thuê: `bint run` (process) chờ khi có hợp đồng còn hạn; hết hạn thì lấy được.
- Role `implementer: codex` ⇒ `next` trả `action: process` cho b-feat.
- Thử thật một lần bằng Claude Code: `/b-do --subagent <việc nhỏ>` tới `done`; spool có `agent_id` ghép
  được với `run_id`.

## 7. G4 — Song song nhiều tác vụ bằng subagent

- `/b-do Dùng subagent làm song song: (1) …, (2) …` ⇒ orchestrator lập mỗi việc một phiếu
  `--driver session --worktree`, chạy các vòng `next → Agent → complete` xen kẽ, các Agent chạy nền.
- **Cwd của subagent.** Công cụ Agent không nhận cwd: subagent chạy ở cwd của phiên cha (cây gốc). Prompt
  của `next` yêu cầu `cd <worktree>` ở mọi lệnh Bash và đường dẫn tuyệt đối cho Read/Edit. Rào chắn kèm
  theo là một guardrail dựng sẵn: hook (đã biết `agent_id` ⇒ worktree qua tệp ánh xạ trong
  `runtime/`, ghi khi thấy lệnh `attach`) trả `deny` cho Edit/Write có đường dẫn ngoài worktree của lượt.
  Hook chỉ đọc tệp và trả quyết định, không lỗi, không SQLite, đúng cơ chế guardrail hiện có. Khác biệt
  theo harness đặt trong `hookcore.Dialect`.
- Cùng guardrail cho reviewer/planner: `pre_bash` từ chối lệnh không khớp `readonly_tools` (lấp lỗ hổng
  khi phiên cha ở `bypassPermissions` làm giới hạn mẫu `Bash(...)` mất tác dụng, nếu S1 xác nhận lỗ hổng).
- Đưa về: mỗi tác vụ tự `land` ở finalize; tác vụ sau xung đột với tác vụ trước ⇒ `blocked` riêng tác
  vụ đó.
- Nghiệm thu: hai tác vụ song song bằng subagent tới `done`, cây gốc có đủ thay đổi; subagent cố sửa tệp
  ở cây gốc bị từ chối và sự kiện `guardrail.triggered` có trong spool.

## 8. Việc cần kiểm trước G3 (spike, không viết mã sản phẩm)

| # | Câu hỏi | Ảnh hưởng nếu câu trả lời là "không" |
|---|---|---|
| S1 | `tools: Bash(bint:*)` trong định nghĩa subagent có giới hạn lệnh khi phiên cha ở `bypassPermissions`/`acceptEdits` không? | Planner/reviewer bỏ Bash, dùng công cụ MCP mới (`task_show`, `task_diff`, `task_write`, `task_review`), hoặc làm guardrail `pre_bash` của G4 ngay ở G3. |
| S2 | Lấy token của subagent từ đâu: trường của `SubagentStop` (đường dẫn transcript?), hay tệp transcript con dưới phiên cha? | Chi phí của driver session là `unknown`; chỉ còn `maxTurns`. |
| S3 | Hook `PreToolUse` trả `deny` có áp cho công cụ do subagent gọi không? `agent_id` có mặt ở `PreToolUse` của subagent không? | G4 không có rào chắn đường dẫn; khi đó song song bằng subagent chỉ cho phép khi có cách cô lập khác. |

Ghi kết quả spike vào mục 12 của tài liệu này trước khi bắt đầu G3.

## 9. Cấu hình mới

| Khoá | Mặc định | Giai đoạn |
|---|---|---|
| `workflow.isolation` | `auto` | G2 |
| `workflow.worktree.copy` | `[]` | G2 |
| `workflow.worktree.setup_command` | `""` | G2 |
| `workflow.driver` | `process` | G3 |
| bảng giá model (tên khoá chốt ở G3) | dựng sẵn cho Claude | G3 |

Cập nhật `docs/configuration.md` cùng lúc.

## 10. Ảnh hưởng tới phần khác

- `docs/workflows.md`: mục cô lập, `task diff`, `task land`, driver session.
- Runlog/eval: sự kiện từ worktree tác vụ có `Worktree` là đường dẫn trong thư mục trạng thái. Kiểm lại
  `runlog/gitscan.go` và nơi nhóm theo worktree.
- TUI (`tui-plan.md`): bảng tác vụ cần cột chế độ cô lập và trạng thái đưa về; `task land`, `task diff`,
  `run next/complete/attach/abort` vào bảng phủ lệnh/cờ (kiểm thử phủ cờ của TUI sẽ bắt).
- MCP: chỉ thêm công cụ nếu S1 buộc phải thêm.

## 11. Thứ tự và kích thước ước lượng

1. **G1** (vừa): sửa ngay cảm giác "bị nhạy cảm với thay đổi" trên worktree chung, kể cả người dùng thao tác TUI lúc agent chạy (4.5).
2. **G2** (vừa): nền cho mọi việc song song.
3. Spike S1–S3 (nhỏ).
4. **G3** (vừa–lớn): phần lớn là tách `runStep` và hợp đồng thuê.
5. **G4** (vừa): guardrail dựng sẵn và hướng dẫn song song.

Mỗi giai đoạn một nhánh/PR, có review bằng b-review, tài liệu cập nhật cùng mã.

## 12. Ghi chép triển khai

### 12.1. G1 mục 4.1–4.4 (H1–H5), 2026-11-12, tác vụ 01M76XPE8PD17GNK77KDP40RDW

**Đã làm** (chưa commit khi viết ghi chép này; kiểm thử đã chạy xem cuối mục):

- **Khoá chia sẻ/độc quyền có chờ khoá (4.2).** `internal/workflow/isolation.go`: `LockMode` (`LockShared`,
  `LockExclusive`), `AcquireLock` (không chờ), `WaitLock` (thăm dò 50 ms giãn tới 2 s, `OnWait`, hết hạn → lỗi bọc
  `ErrLocked`, ctx huỷ → `ctx.Err()`), `tryFlock(f, mode)` ở cặp `flock_unix.go`/`flock_windows.go`. Giữ lại vỏ
  `AcquireWriteLock` (= độc quyền), `WriteLock` (= `Lock`), `ErrWriteLocked` (= `ErrLocked`) vì tri thức và tài
  liệu dẫn các tên này. `Release` không còn xoá tệp khoá. Runner (`run.go`): bước chỉ đọc lấy khoá chia sẻ
  **trước** ảnh chụp trước và giữ tới sau ảnh chụp sau; `b-feat` lấy độc quyền; kiểm chứng sau `b-feat` chạy dưới
  khoá chia sẻ (`lockStep`). Cờ `--no-wait` (`RunOptions.NoWait`, `RunWorkflowOptions`/`RunResumeOptions`, nhãn form
  vi/en). Hết hạn → `blocked` "chờ khoá quá N phút: <chủ>"; Ctrl-C → `interrupted`.
- **Gắn tác vụ với worktree (4.1).** `Env.RootFor(t)` + `ErrWorktreeGone` (`internal/workflow/touched.go`). `Runner.Run`
  chạy trên bản sao `Env` có `Root` của tác vụ (trả lại `Env` gốc khi xong), in "chạy trong <đường dẫn>"; thư
  mục mất → `failed` sau bước resume. `Verify`, `SetStatus(done)`, `UnapprovedChanges`, `app.ShowTask`,
  `app.DiffTask` dùng `RootFor`.
- **BaseTree/Touched (4.3).** `State.BaseTree`, `State.Touched`, `TracksTouched()`, `EverInProgress()`. Chụp trước/sau
  `b-feat` dưới khoá độc quyền (`beginTouched`/`endTouched`, sau cả khi agent lỗi/Ctrl-C), `DiffPaths` dùng
  `--no-renames -z` và trả lỗi. `checkScope`/`UnapprovedChanges` dùng `Touched` (phiếu cũ: `ChangedFiles`). Chồng
  lấn với tác vụ chưa kết thúc khác trên cùng worktree vào luồng duyệt sẵn có. **`bint task diff <id>
  [--stat|--name-only]`**: `app.DiffTask` (`internal/app/task_diff.go`), lệnh CLI, hành động TUI chỉ đọc
  `tasks.diff` (phím `D`). Skill b-review và đề bài `StepReview` của Runner dùng lệnh này; ba bản skill +
  `core.lock` đồng bộ bằng `bint sync`.
- **Verify (4.4).** `Verification.Tree`, `DoneInput.OutsideSinceVerify`, `Env.DoneInputFor` (dùng chung `SetStatus` và
  `ShowTask`); thông báo "thay đổi không thuộc tác vụ: …; chạy lại bint task verify <id>".
- Tài liệu: `docs/workflows.md` (đoạn cô lập, giới hạn đã biết). Chưa làm: 4.5 (H8, nhật ký ghi của Bint) — ở phiếu sau.

**Lệch so với kế hoạch ban đầu (và lý do):**

1. **Tên hàm `app.DiffTask`**, không phải `app.TaskDiff`: các dịch vụ tác vụ khác đặt động từ trước (`ShowTask`, `LintTask`).
2. **Windows khoá một byte ở offset lớn** (`OffsetHigh=0x7fffffff`), không phải byte 0 như bản cũ: khoá vùng trên
   Windows là bắt buộc, khoá chia sẻ ở byte 0 làm chính chủ không ghi được tệp, khoá độc quyền làm người khác không
   đọc được nội dung. Hệ quả: trong lúc nâng cấp, binary cũ (byte 0) và mới **không** loại trừ lẫn nhau trên
   Windows (trên Unix `flock` cùng tệp nên vẫn loại trừ). Tên tệp khoá `write-<hash16>.lock` giữ nguyên.
3. **Thông tin chủ khoá nằm ở tệp riêng mỗi chủ** (`locks/write-<hash>.lock.d/<pid>-<nonce>.json`), không ghi vào tệp
   khoá: nhiều chủ chia sẻ không ghi đè nhau và không đụng vùng khoá Windows. Mục của tiến trình chết bị ẩn/dọn khi
   hiển thị; **không dùng để quyết định khoá** (không chép mẫu PID chết của `evolve.AcquireLock`).
4. **Chồng lấn gỡ bằng `bint task approve`** (đưa tệp chung vào `PendingPaths`/`ProtectedApproval`), không thêm trạng
   thái hay cơ chế riêng. Hệ quả: duyệt một tệp chồng lấn cũng ghi tệp đó vào danh sách đã duyệt của tác vụ.
   `UnapprovedChanges` (done) **không** tính chồng lấn; chồng lấn chỉ chặn ở `checkScope` của Runner.
5. **Mốc `BaseTree` chỉ đặt ở lần `b-feat` đầu tiên của tác vụ chưa từng `in_progress`** (kể cả làm tay). Tác vụ đã có
   `b-feat` trước khi nâng cấp giữ chế độ cũ suốt đời: đặt muộn sẽ bỏ sót tệp đã sửa ở lượt trước, kể cả đường
   dẫn bảo vệ. Mốc "tiêu chí 3: phiếu cũ (Touched nil)" thực chất là `BaseTree` rỗng (`omitempty` làm nil và rỗng
   không phân biệt được sau khi lưu). Làm tay sau khi đã có `BaseTree`: bản sửa vòng 1 bỏ theo dõi khi `SetStatus` → `in_progress` không do
   Runner; **đã thay ở vòng sửa thêm (mục 12.2)** — không bỏ theo dõi nữa.
6. **Kiểm chứng sau `b-feat` chạy dưới khoá độc quyền** (kế hoạch không nói; vòng đầu là chia sẻ, đổi ở 12.2/.2). `bint task verify` gọi tay không lấy
   khoá: agent gọi nó trong `b-feat` sẽ không tự chờ chính Runner của mình.
7. **Bảng chuyển trạng thái không đổi**: "failed khi thư mục mất" đi qua bước resume (về trạng thái hoạt động) rồi
   `fail`, vì blocked/interrupted không chuyển thẳng sang failed.
8. `Cfg`/`ConfigRoot` vẫn lấy từ nơi gõ lệnh dù tác vụ chạy ở worktree khác (các worktree cùng repo thường cùng cấu hình).
9. Thêm `Verification.Tree` chỉ khi tác vụ theo dõi `Touched` (chụp lỗi thì để rỗng, không chặn).
10. Golden `help_*` của TUI đổi một dòng (thêm phím `D` ở màn Tác vụ); `TestWritesAudit` thêm `tasks.diff` vào danh
    sách chỉ đọc.

**Chưa kiểm được / rủi ro còn lại:**

- **Windows chạy thật**: mới kiểm biên dịch (`GOOS=windows go vet ./...`, staticcheck với `GOOS=windows`). Cần một lần
  chạy `go test ./internal/workflow/` và một phiên `bint run` song song trên máy Windows trước khi tin khoá chia sẻ.
- Hai binary **khác phiên bản** cùng giữ khoá chưa thử (kiến thức hiện có: Unix loại trừ nhờ cùng tệp + `sameFile`;
  Windows không, xem lệch 2).
- **Đói lượt khoá độc quyền** khi bước chỉ đọc nối tiếp nhau: chấp nhận ở G1; hết `workflow.timeout_minutes` thì
  `blocked` (kiểm thử khoá chia sẻ phải chấp nhận người đọc chồng nhau làm người ghi đói).
- Tác vụ bỏ dở (`failed`/`blocked` lâu) vẫn được dò chồng lấn: người phải huỷ nó (`cancelled`) để hết cảnh báo.
- Hiệu năng: thêm hai lần `git add -A` vào chỉ mục tạm mỗi `b-feat` và một lần mỗi verify/done/diff.

**Kiểm thử đã thêm:** `isolation_test.go` (ma trận S/X, tranh chấp hỗn hợp, đa tiến trình, kill tiến trình giữ khoá, chờ/hết
hạn/huỷ, tệp chủ khoá), `concurrent_test.go` (hai Runner; `b-review` của A không fail khi B có `b-feat` chờ; `--no-wait`; hết
hạn; Ctrl-C), `touched_test.go` (tệp bẩn có sẵn kể cả `migrations/**`, tác vụ khác, chồng lấn + approve + resume, phiếu
cũ, xoá/đổi tên tệp bảo vệ, agent lỗi, chụp lỗi → chế độ cũ, đề bài b-review), `rootfor_test.go`, e2e CLI
`isolation_e2e_test.go` (`task diff`, resume từ thư mục khác, thư mục mất), `internal/task` (`DoneBlockers` có/không tệp
ngoài), TUI (`tasks.diff` chỉ đọc, hộp diff), skill b-review.

### 12.2. Vòng sửa thêm sau review (phát hiện .2–.6), 2026-11-12, cùng tác vụ

Ngoài giới hạn `max_review_rounds` theo yêu cầu của người dùng.

- **.5 (medium) — sửa tay ngoài b-feat lọt cổng done (đã sửa).** Thay vì bỏ theo dõi khi `in_progress` tay (mất
  `BaseTree` nên mất cả lọc tệp của tác vụ khác, và hỏng đường `bint task status … in_progress` rồi `bint run
  b-feat` của skill b-do), `workflow.ScopeFiles` = `Touched` ∪ `outsideFeat`: tệp đổi giữa `BaseTree` và cây hiện
  tại mà không thuộc `Touched` của **bất kỳ** tác vụ nào trên cùng worktree (kể cả đã kết thúc). Dùng ở
  `checkScope`, `UnapprovedChanges` (cổng done, approve) và `app.DiffTask`. Không cần trường mới trong state nên
  chạy cả với phiếu đang dở (khác gợi ý `FeatTree` của review: `BaseTree` đã đủ vì phép trừ `Touched` loại phần của
  b-feat; một mốc "sau b-feat cuối" chỉ thêm trạng thái mà không bắt thêm được gì). Chụp cây lỗi → trả lỗi (không
  nuốt: cổng done đóng). Test: `TestManualEditOutsideFeatIsCaught` (in_review và in_progress; verify → done bị
  chặn → approve → done), `TestOutsideFeatExcludesOtherTaskTouched`. Đánh đổi: thay đổi tay của người dùng trên
  worktree chung bị quy cho tác vụ (chỉ dẫn tới yêu cầu duyệt); thay đổi của tác vụ chạy chế độ cũ (không `Touched`)
  trên cùng worktree cũng bị quy cho tác vụ có theo dõi.
- **.4 (đã sửa).** `SetStatus` tới `done`/`cancelled` xoá `refs/bint/tasks/<id>/base`. `bint task diff` của tác vụ
  đã đóng vẫn đọc được `BaseTree` tới khi `git gc` dọn đối tượng mồ côi (mặc định sau 2 tuần).
- **.3 (đã sửa).** `bint task diff` không đưa `Touched` vào dòng lệnh git: liệt kê toàn bộ rồi lọc trong Go; diff
  đầy đủ chia lô pathspec (≤16000 byte mỗi lô, `pathBatches`). `--stat` nhiều lô in theo lô, không có dòng tổng chung
  (có ghi chú). Test `TestPathBatches`. `DiffPaths` với pathspec vẫn chỉ dùng ở nơi pathspec ngắn.
- **.2 (đã sửa).** Kiểm chứng sau `b-feat` của Runner lấy khoá **độc quyền** (không còn chia sẻ): lệnh kiểm chứng
  hay ghi tệp nên không được song song với bước chỉ đọc của tác vụ khác. Đổi lại b-review của tác vụ khác phải chờ
  trong lúc chạy kiểm chứng (có thể tới `verify_timeout_minutes`). Kế hoạch lệch 6 sửa theo.
- **.6 (đã sửa).** `DoneInputFor` (dùng cho `ShowTask`/TUI) chỉ tính fingerprint; phần liệt kê tệp ngoài tác vụ
  (`DoneInputDetailed`, chụp cây + diff) chỉ chạy ở `SetStatus(done)`. Hệ quả: `bint task show` nêu thông báo chung
  "mã đã thay đổi sau lần kiểm chứng cuối", chi tiết tệp chỉ hiện khi thử `done`.
- Không sửa: không có phát hiện nào bị bỏ. Lỗi `TestEvalRunEscCancelsBatchAndShowsResumeCommand` (và đôi lúc
  `TestEvaluateCreateProposal`/`TestEvaluateSkillProposal`) chập chờn khi máy bận, không thuộc diff này.

### 12.3. Phát hiện còn mở khi đóng phiếu 01M76XPE8PD17GNK77KDP40RDW (review vòng 3)

Người dùng chọn đóng phiếu, để lại hai phát hiện cho phiếu sau (chi tiết trong `review.md` của tác vụ):

- **.7 (medium) — ĐÃ ĐÓNG ở mục 12.5** — `outsideFeat` (`internal/workflow/touched.go`) miễn trừ theo `Touched` của mọi tác vụ cùng
  worktree, kể cả `done`/`cancelled`: sửa tay một tệp bảo vệ mà tác vụ cũ từng sửa vẫn lọt cổng done. Hướng sửa: chỉ
  miễn theo tác vụ chưa kết thúc; không miễn đường dẫn bảo vệ/deny. Test: B sửa `migrations/001.sql` rồi `cancelled`;
  A `b-feat` sửa tệp khác; sửa tay `migrations/001.sql` ⇒ `UnapprovedChanges(A)` phải chứa nó.
- **.8 (low) — ĐÃ ĐÓNG ở mục 12.5** — `checkScope` chạy sau khi `runStep` đã nhả khoá độc quyền của `b-feat`: `b-feat` của tác vụ khác vừa
  giành khoá có thể bị quy vào tác vụ này; tệp do bước chỉ đọc/verify của tác vụ khác tạo cũng vào `ScopeFiles`. Hướng
  sửa: tính `ScopeFiles`/`checkScope` ngay sau `endTouched` khi còn giữ khoá.

### 12.4. G1 mục 4.5 (H8) — thay đổi do Bint ghi thay người không tính là vi phạm, 2026-11-12, tác vụ 01M772P7DKB3PBRBH7VPE614X6

**Đã làm** (chưa commit khi viết ghi chép này, cùng cây với 4.1–4.4; không sửa phát hiện .7/.8 ở 12.3):

- **Nhật ký ghi** `internal/workflow/writes.go`: `WriteRecord{path, sha256, at, actor, agent}`, `WritesLogPath`,
  `AppendWrites` (khoá `writes.jsonl.lock` bằng `tryFlock` chờ tối đa 2 s; mỗi bản ghi một lệnh `Write` một dòng trên tệp
  `O_APPEND`; dọn dòng hỏng và dòng cũ hơn retain bằng tệp tạm + rename, chỉ khi lấy được khoá), `ReadWrites(since)`,
  `HashFile`, `NormalizeWritePath` (giải symlink của thư mục tới tổ tiên còn tồn tại, nên tệp/thư mục đã xoá vẫn cho cùng
  đường dẫn; không giải symlink của chính tệp để khớp với đường dẫn git).
- **Đối chiếu** `internal/workflow/isolation.go`: `ReadonlyViolations(ctx, root, before, after, since, writesLog)` trả
  `ReadonlyCheck{Violations, HumanWrites}`. Thứ tự: ngoại lệ ứng viên tri thức mới → dòng nhật ký **mới nhất** của đường
  dẫn (cuối tệp) có `At >= since`, `agent == false`, `sha256 == HashFile(abs)` (tệp xoá: rỗng) → miễn; còn lại vi phạm.
  `ActiveSteps(tasksDir, root)` đọc tệp chủ khoá (chỉ đọc, bỏ qua tiến trình đã chết, không xoá mục).
- **Runner** `run.go`: `StepResult.HumanWrites` (`human_writes`), mốc `started := time.Now()` ngay trước ảnh chụp trước,
  log "bỏ qua thay đổi do người ghi qua Bint: …", dòng tương ứng trong `Result.Notes`, `human_writes` trong payload sự kiện
  kết thúc bước.
- **Hàm ghi chung** `internal/app/repowrite.go` (`repoWriter`, dựng bằng `newRepoWriter(paths, cfg, actor)`; retain =
  `workflow.timeout_minutes` × 2): `WriteFile`, `WriteNew`, `Remove`, `SyncBlock`, `RemoveBlock`, `Record`, và các hàm bọc
  gói thấp `SaveItem`/`RemoveItem` (knowledge.Store), `SaveComponent` (hcomp.Save), `InstallSkills` (skills.Install + `core.lock`
  nếu băm đổi). Đã chuyển: `config_svc.go` (`writeConfigLayer`, cả `SaveConfigEdit` và `SetUILanguage`), `init.go` (tạo/nâng cấu
  hình, `.gitignore`, skill, hook, MCP, `CLAUDE.md`, exclude), `publish.go` (`AGENTS.md`), `knowledge.go` (add, verify --apply,
  approve/reject/deprecate, remove), `skills_svc.go` (overlay), `bootstrap.go` (sync), `harness_components.go`,
  `evolvesvc` (`evolve.Apply/Rollback` → `recordFiles` → `KnowledgeWS.RecordRepoWrites`). `ComponentWS` có thêm `Paths`.
- **TUI**: `app.ActiveSteps(ctx, dir)`; `Options.ActivePoll` (3 s ở `tui.go`, 0 trong kiểm thử), `loadActive()` gọi trong
  `load()`, tin `activeStepsMsg`/`activeTickMsg`; cờ thanh trên `ui.step_running` ("b-review của 01M772P7 đang chạy trên cây
  này"), nhiều bước thì `ui.steps_running` (vi, en). Không đổi `gate()`: thao tác ghi vẫn `Available`.
- **Tài liệu**: `docs/workflows.md` (đoạn miễn trừ ghi qua Bint, ba mục "Giới hạn đã biết" mới).

**Lệch so với kế hoạch 4.5 và quyết định:**

1. **"Một hàm ghi chung" thành hai dạng** (tự ghi / ghi nhận ngay sau lời gọi gói thấp), vì phần lớn việc ghi nằm ở
   `knowledge`, `skills`, `fsafe`, `hcomp`, `evolve`, ngoài `--allow`. Độ phủ giữ bằng kiểm thử tĩnh + động (dưới).
2. **Chỉ ghi nhận khi Bint thật sự đổi tệp** (`Created/Appended/Updated/Removed`; `core.lock` chỉ khi băm đổi). Ghi nhận
   tệp không đổi sẽ che việc agent sửa tệp đó trước đó trong cùng bước.
3. **`started` dùng `time.Now()`**, không dùng `Env.Now` (có thể bị giả), cùng đồng hồ với `At` của dòng nhật ký.
4. **`ReadWrites(since)` lọc theo `At`** ngay khi đọc; tương đương với luật "dòng mới nhất nằm trong khoảng bước" vì dòng
   mới nhất cũ hơn `since` cũng cho kết quả "không có dòng" ⇒ vi phạm.
5. Dòng nhật ký ghi `path` **tuyệt đối đã chuẩn hoá** (thư mục trạng thái dùng chung giữa các worktree, `KnowledgeWS.Root`
   có thể là worktree chính). Tri thức ghi vào worktree chính không được miễn ở bước chạy trên worktree phụ — đúng.
6. **`init` lần đầu** tạo `.bint/config.yml` bằng writer chưa có nhật ký (chưa biết thư mục trạng thái); lần init đầu
   không thể có bước chỉ đọc nào đang chạy. Sau `state.Resolve` writer đầy đủ.
7. `.git/…` bị bỏ qua khi ghi nhận (exclude nằm ngoài cây làm việc).
8. `ui.language` mặc định ghi vào `.bint/local.yml` (bị `.gitignore`) nên không vào ảnh chụp; kiểm thử H8 dùng lớp project.
9. Dòng trạng thái TUI đọc **tệp chủ khoá**, không có `bint run status`; khoá khoá theo `Repo.Toplevel` như Runner.
10. `bint task show` vẫn cảnh báo "docs/isolation-subagent-plan.md không nằm trong phạm vi sửa" dù `--allow` có đủ: cảnh báo
    nhầm của Bint, không chặn, không sửa ở tác vụ này.

**Lỗi tìm thấy nhờ kiểm thử động và đã sửa:** `InstallMCP` trả đường dẫn **tương đối** với gốc repo; ghi nhận thẳng làm
nhật ký trỏ vào thư mục đang đứng (`.mcp.json`, `.codex/config.toml` đổi mà không có dòng khớp). Thêm `absUnder(root, p)`
cho MCP và đường dẫn hook.

**Kiểm thử đã thêm:**
- `internal/workflow/writes_test.go`: mỗi lần nối đúng một dòng; 8 writer × 25 lần không mất dòng (`-race`); dọn dòng cũ
  giữ dòng mới và bỏ dòng hỏng; `ReadWrites` bỏ dòng hỏng/trước mốc; chuẩn hoá đường dẫn ổn định khi tệp bị xoá và qua
  symlink; bảng `ReadonlyViolations` (người khớp băm, `agent: true`, băm lệch, dòng trước mốc, không có dòng, dòng của tệp
  khác, dòng mới nhất là agent/người, xoá `sha256: ""`, `sha256: ""` mà tệp còn); candidate mới; Runner giả với bước chỉ đọc
  (`completed`, `HumanWrites`, log, `Notes`); `ActiveSteps`.
- `internal/app/h8_test.go` (AC-1, AC-2, AC-4) qua `RunWorkflow` + agent giả của bước chỉ đọc: người đổi `ui.language` + duyệt
  mục tri thức ⇒ `completed`, `HumanWrites` = `.bint/config.yml` và tệp mục, log có dòng báo; biến agent ⇒ duyệt trả
  `ErrAgentForbidden`, `ui.language` thành vi phạm; người rồi agent sửa tiếp ⇒ vi phạm; sửa trực tiếp ⇒ vi phạm;
  `RemoveKnowledge` vai người ⇒ dòng `sha256: ""` và được miễn; hook (PreToolUse/PostToolUse/Stop/SessionStart) không tạo
  `writes.jsonl`. Kiểm chứng đột biến: làm `Record` thành no-op thì hai ca đầu đỏ. Chạy được cả khi `CLAUDECODE=1`.
- `internal/app/repowrite_audit_test.go` (AC-3): **tĩnh** (`go/parser`) mọi lời gọi `os.WriteFile/Create/OpenFile/Remove/RemoveAll/
  Rename/Truncate/CreateTemp/MkdirTemp`, `fsafe.WriteAtomic/SyncBlock/RemoveBlock`, `Store.Save/Remove`, `hcomp.Save`,
  `skills.Install` ngoài `repowrite.go` phải nằm trong bảng `directWriteAllowed` kèm lý do (thiếu hay thừa đều đỏ);
  `evolve.Apply/Rollback`, `InstallHooks/UninstallHooks/InstallMCP` phải kèm `Record/recordFiles/RecordRepoWrites` trong cùng
  hàm; `hook.go` không nhắc `repoWriter`/`AppendWrites`. Thêm tạm một `os.WriteFile` vào một hàm ⇒ test đỏ (đã thử).
  **Động**: 12 dịch vụ ghi (config set, knowledge add/approve/reject/remove/verify --apply, skill overlay, sync, bootstrap
  publish, harness component add + approve, `init --update`, `init --hooks --mcp`) chạy giữa hai ảnh chụp; mọi tệp đổi
  phải có dòng nhật ký không phải agent, băm khớp.
- `internal/tui/app/active_steps_test.go` (AC-5): khoá thật của workflow trên repo thật của fixture ⇒ "b-review của 01M772P7
  đang chạy trên cây này" (vi), bản en, dạng gộp "2 bước…", biến mất khi trả khoá, thăm dò tự phát hiện, hành động ghi
  (`system.language_save`) vẫn `Available`. Golden hiện có không đổi.

**Kết quả kiểm chứng (2026-11-12):** `go test -count=1 ./...` 1249 test qua (66 gói); `go vet ./...` và `GOOS=windows go vet ./...`
sạch; `staticcheck ./...` và `GOOS=windows staticcheck ./...` (binary đã cài) sạch; `go test -race` cho `internal/workflow`
(Writes|Readonly|ActiveSteps) và `internal/app` (H8, audit) qua; `-race -count=20` cho test TUI dòng trạng thái qua.

**Chưa kiểm được / rủi ro còn lại:**
- **Windows chạy thật**: nhật ký dùng rename để dọn dòng cũ (lỗi khi tệp đang mở thì bỏ dọn lần đó) và `tryFlock` — mới kiểm
  biên dịch.
- **Phiên thật**: chưa làm tay một lần đổi `ui.language` trong TUI trong lúc `bint run b-req` chạy với agent thật.
- **`evolve apply/rollback`** chỉ có kiểm thử tĩnh (buộc `recordFiles`), chưa có ca động vì dựng đề xuất đã đánh giá
  tốn công; `evolvesvc.Cycle` tự áp dụng/quay lui đi qua cùng `recordFiles`.
- Đường ghi **ngoài `internal/app`** (nếu sau này thêm ở `internal/cli`/`internal/tui`) không được kiểm thử tĩnh phủ; hiện
  CLI/TUI chỉ xoá tệp tạm.
- Lỗ đã biết (ghi ở `docs/workflows.md`): agent có thể chèn dòng giả vào nhật ký; sửa bằng IDE vẫn bị tính; agent ghi lại
  đúng nội dung người vừa ghi thì không phát hiện.
- Hiệu năng: mỗi thao tác ghi thêm một lần băm tệp và một lần mở nhật ký.

### 12.5. Sửa phát hiện .7/.8 (mục 12.3) và .1/.2/.3 của H8 (mục 12.4), 2026-11-12, tác vụ 01M774RD3A85K7HY9NRRQ73Z95

**Đã làm** (chưa commit khi viết ghi chép này):

- **.7 (medium) — `outsideFeat` chỉ miễn theo tác vụ chưa kết thúc, không miễn đường dẫn bảo vệ/deny**
  (`internal/workflow/touched.go`). `Touched` của chính tác vụ luôn được miễn; `Touched` của tác vụ khác chỉ miễn khi
  tác vụ đó chưa `done`/`cancelled` **và** `task.CheckScope([]string{f}, ticket, protected).NeedsApproval()` rỗng.
  `outsideFeat`/`ScopeFiles` nhận thêm `protected` (= `Cfg.Limits.ProtectedPaths`); nơi gọi: `Runner.checkScope`,
  `Env.UnapprovedChanges` (ops.go), `app.DiffTask` (task_diff.go).
- **.8 (low) — tính tập tệp khi còn giữ khoá độc quyền** (`internal/workflow/run.go`). Chỉ phần **tính tập tệp** dời vào
  trong khoá: `runStep` gọi `ScopeFiles` ngay sau `endTouched` (khi `step == b-feat` và ctx chưa huỷ) và lưu vào
  `runState.featScope*`; `checkScope` dùng tập đó. Không dời cả `checkScope`: nó có thể gọi `r.Confirm` (chờ người trả
  lời), giữ khoá độc quyền lúc đó sẽ chặn mọi bước của tác vụ khác. Đường `stepCheck` (resume, b-feat chạy ở lượt
  trước) lấy khoá **chia sẻ** (`lockStep(..., "scope", LockShared)`), tính rồi nhả trước khi hỏi người: khoá chia sẻ đợi
  `b-feat` đang chạy của tác vụ khác xong nên tệp dở dang của nó không bị tính. Seam kiểm thử: biến gói
  `afterStepUnlock` (nil ở sản phẩm), gọi ngay sau khi `runStep` nhả khoá.
- **.1 (medium, tác vụ 01M772P7…) — chuỗi nhật ký `prev_sha256`.** `WriteRecord.Prev *string` (`prev_sha256`,
  `omitempty`; con trỏ để phân biệt "không biết" với `""` = tệp chưa có), `workflow.HashBytes`. `repoWriter` băm tệp **ngay
  trước** lời gọi ghi (`hashBefore`) cho `WriteFile`/`WriteNew`/`Remove`/`SyncBlock`/`RemoveBlock`/`SaveItem`/`RemoveItem`/
  `SaveComponent`/`InstallSkills` (quét mọi tệp dưới thư mục skill của các target + `core.lock`) và hook của `init`
  (`InstallHooks`/`UninstallHooks`: đường dẫn biết trước); `sha256` tính từ dữ liệu ghi khi có (`WriteFile`, `WriteNew`,
  `SaveComponent`), không đọc lại đĩa. `Record(paths...)` giữ cho nơi không biết trạng thái trước (`Prev = nil`).
  `ReadonlyViolations` (isolation.go): với đường dẫn đổi, lấy chuỗi dòng nhật ký (thứ tự trong tệp, `At >= since`);
  `cur` = băm blob ở ảnh trước đọc bằng `git cat-file --filters` (tệp mới `A`: rỗng); dòng `Agent` hoặc `Prev == nil` ⇒
  vi phạm; `*Prev == cur` ⇒ `cur = SHA256`; `SHA256 == cur` (ghi giữa `started` và ảnh trước) ⇒ bỏ qua; còn lại ⇒
  vi phạm; cuối chuỗi `cur == HashFile(abs)` ⇒ miễn. Chữ ký hàm và ngoại lệ ứng viên tri thức mới giữ nguyên.
- **.3 (low) — mở workspace một lần.** `app.ResolveActiveSteps(ctx, dir) (ActiveStepsWatch, bool)` + `Steps()`;
  `ActiveSteps(ctx, dir)` thành vỏ. TUI (`internal/tui/app/app.go`) dựng watch trong `load()` (`loadActive(true)`, cả khi
  nạp lại), lượt thăm dò (`loadActive(false)`) chỉ gọi `Steps()`.
- **.2 (low) — sửa một phần**: xoá mục chết `skills.InstallEvolved` khỏi `lowLevelCalls` và `case` trong
  `repowrite_audit_test.go`. **Không** chuyển phép quét tĩnh sang `go/types`: go.mod không có `golang.org/x/tools`, tự
  dựng `types.Config` với importer "source" cho cả `internal/app` chậm và giòn; kiểm thử động (nay kiểm cả chuỗi
  `prev_sha256` qua `ReadonlyViolations`) bù độ phủ.

**Lệch so với kế hoạch của phiếu:** `InstallHooks`/`UninstallHooks` được băm trước (kế hoạch cho phép để `nil`) vì đường
dẫn biết trước lời gọi; vẫn để `Prev = nil`: `InstallMCP` (đường dẫn chỉ biết sau khi ghi) và `evolve apply/rollback/cycle`
(`recordFiles`). Test động liệt kê tường minh `.mcp.json`, `.codex/config.toml` của `init --mcp` trong `noPrev`.

**Kiểm thử đã thêm/sửa:**
- `internal/workflow/touched_test.go`: `TestOutsideFeatTerminalTaskDoesNotShield` (ca `cancelled` và `done`: B sửa
  `migrations/001.sql` + `docs/b.md`, sửa tay lại `migrations/001.sql` ⇒ `UnapprovedChanges(A)` chứa nó, `ScopeFiles(A)`
  chứa cả `docs/b.md`, done của A bị chặn); viết lại `TestOtherTaskFilesNotCountedAndDoneMessage` (B chưa kết thúc: tệp
  bảo vệ `migrations/002.sql` **bị** tính cho A, `docs/b.md` thì không; duyệt + verify lại ⇒ done qua); giữ
  `TestOutsideFeatExcludesOtherTaskTouched`; `TestScopeComputedUnderFeatLock` (seam ghi `migrations/003.sql` ngay sau khi A
  nhả khoá ⇒ A `in_review`, không có câu hỏi, không `PendingPaths`/`ProtectedApproval`); `TestScopeResumeTakesSharedLock`
  (resume chờ khoá chia sẻ khi b-feat khác giữ khoá độc quyền).
- `internal/workflow/writes_test.go`: bảng `TestReadonlyViolationsHumanWriteTable` có `prev_sha256` và thêm ca: dòng cũ không
  có khoá ⇒ vi phạm; hai lần ghi nối tiếp ⇒ miễn; đứt mắt xích (đầu/giữa chuỗi) ⇒ vi phạm; agent đầu/giữa chuỗi ⇒ vi
  phạm; dòng thiếu `prev_sha256` giữa chuỗi ⇒ vi phạm; ghi trước ảnh chụp trước rồi ghi tiếp ⇒ miễn; tệp mới (prev rỗng) ⇒
  miễn; xoá ⇒ miễn; `TestWriteRecordPrevJSON`; `TestReadonlyViolationsChainThroughEolFilter` (`.gitattributes eol=crlf`).
- `internal/app/h8_test.go`: `TestH8AgentEditThenHumanReadModifyWriteViolates` (agent sửa `.bint/config.yml` rồi người
  `SetUILanguage` ⇒ bước `failed`); `TestActiveStepsWatchSurvivesBrokenConfig`. Các `TestH8*` cũ giữ nguyên và qua.
- `internal/app/repowrite_audit_test.go`: `TestEveryServiceWriteIsLogged` đòi `workflow.ReadonlyViolations(...).Violations`
  đúng bằng danh sách `noPrev` của bước (chỉ đường ghi chưa biết trạng thái trước mới được lọt).
- `internal/tui/app/active_steps_test.go`: `TestActiveStepPollDoesNotReopenWorkspace`.
- **Kiểm đột biến (đều đỏ như mong đợi):** bỏ lọc `Terminal` ⇒ `TestOutsideFeatTerminalTaskDoesNotShield` đỏ (cả hai ca,
  nhờ `docs/b.md`; riêng tệp bảo vệ thì luật bảo vệ cũng bắt nên `TestOtherTaskFilesNotCountedAndDoneMessage` giữ nhánh
  đó); tính `ScopeFiles` sau khi nhả khoá ⇒ `TestScopeComputedUnderFeatLock` đỏ; bỏ khoá chia sẻ ở nhánh resume ⇒
  `TestScopeResumeTakesSharedLock` đỏ; bỏ kiểm `*Prev == cur` ⇒ `TestH8AgentEditThenHumanReadModifyWriteViolates` đỏ.

**Kết quả kiểm chứng (2026-11-12):** `go test -count=1 ./...` 1266 test qua (66 gói); `go vet ./...` và
`GOOS=windows go vet ./...` sạch; `staticcheck ./...` và `GOOS=windows staticcheck ./...` sạch; `go test -race` cho
`internal/workflow` (Readonly|Writes|Scope|Outside|Touched|Overlap), `internal/app` (H8|EveryService|ActiveSteps) và
`internal/tui/app` (Active, `-count=3`) qua.

**Rủi ro còn lại / chưa kiểm được:**
- **Quy nhầm nhiều hơn (đúng theo yêu cầu .7):** tệp của tác vụ B đã `done`/`cancelled` mà còn khác `BaseTree` của A (kể cả
  đã commit, vì `BaseTree` là cây chụp) vào `ScopeFiles(A)` và `bint task diff A`; tệp bảo vệ đã duyệt ở B phải duyệt lại
  ở A. Tệp bảo vệ/deny của tác vụ chưa kết thúc khác cũng bị quy cho A. Hướng chính xác hơn (ghi `FeatTree` sau mỗi
  `endTouched`, miễn khi blob hiện tại khớp `FeatTree` của tác vụ sở hữu) để lại.
- **Đường ghi có `Prev = nil`** (`InstallMCP`, `evolve apply/rollback/cycle`) không được miễn ở bước chỉ đọc đang chạy; dòng
  nhật ký cũ (trong 2 × `workflow.timeout_minutes` sau nâng cấp) cũng vậy. Hướng an toàn; ghi ở `docs/workflows.md`.
- `git cat-file --filters` chạy bộ lọc smudge (LFS) và có thể chậm hoặc lỗi ⇒ coi như không có chuỗi ⇒ vi phạm.
- Windows chạy thật vẫn chưa kiểm (chỉ biên dịch/vet/staticcheck); phiên `bint run` thật cùng TUI chưa thử tay.
- Lỗi chập chờn đã biết ở 12.2 (`TestEvalRunEscCancelsBatchAndShowsResumeCommand`…) không xuất hiện ở lượt chạy này.

### 12.6. G2 mục 5 — mỗi tác vụ một worktree, 2026-11-12, tác vụ 01M777NWWZA4J9F66Q1AKEAFFX

**Đã làm** (chưa commit khi viết ghi chép này; không làm G3/G4):

- **Cấu hình (mục 9).** `workflow.isolation` (`auto` | `shared` | `worktree`), `workflow.worktree.copy` (glob tương đối,
  không tuyệt đối/`..`), `workflow.worktree.setup_command`: trường `yaml` + hằng `config.Isolation*`, `Default()`,
  `Validate()`, dòng có chú thích trong `template.go`, `TestRenderMatchesDefaults` qua, `TestWorkflowIsolationDefaultsAndValidate`.
  `docs/configuration.md`, `docs/workflows.md` cập nhật. `state.Paths.Worktrees` (`<state>/worktrees`).
- **Mô hình dữ liệu.** `task.Classification`: `Isolation`, `IsolationReason`, `IsolationTasks` (+ `EffectiveIsolation()`);
  `task.State`: `Origin`, `Landed *Landing` (thời điểm, người, tệp, tệp xung đột, `ThreeWay`, `Fingerprint`, `Tree`, `Kept`,
  `Note`), `AwaitingLand`; `NeedsLand()`; `DoneBlockers` thêm "chưa đưa thay đổi về cây gốc … (bint task land)" trừ khi
  `DoneInput.IgnoreLanding`. Tất cả `omitempty`: phiếu cũ đọc được, coi là `shared`.
- **Chọn chế độ (5.1).** `internal/workflow/worktree.go`: `chooseIsolation` (cờ → cấu hình → quy tắc `auto`),
  `NormalizeIsolationFlag`, `NewTaskOptions.Isolation`, `RunOptions.Isolation`; `--worktree`/`--shared` ở `task new` và
  `run` (loại trừ nhau, `run --task` + cờ là lỗi dùng sai); `task new --json` có `isolation`, `isolation_reason`,
  `isolation_tasks`; `bint task show --json` và `run status --json` cũng có.
- **Tạo worktree lười (5.2).** `Runner.ensureWorktree` sau `ClaimRunner` và bước resume, trước mọi bước: ảnh chụp cây gốc
  dưới khoá chia sẻ → `commitTree` ("bint base <id>", cha HEAD) → `refs/bint/tasks/<id>/base` → `gitrepo.WorktreeAdd`
  (`bint/<id>`) → `copyIntoWorktree` (tệp cục bộ do Bint quản lý + `workflow.worktree.copy`, chỉ chép tệp chưa có ở
  worktree) → `setup_command` (`eval.RunShell`, `logs/worktree-setup.log`) → ghi `Origin/Worktree/Branch/Base`. Lỗi bước nào
  ⇒ `cleanupWorktree` rồi `failed` (lý do có đường dẫn nhật ký); huỷ ⇒ `interrupted`.
- **Đưa về (5.3).** `internal/workflow/land.go`: `Env.Land`, `LandOptions{ThreeWay, Keep, Orchestrator}`, `LandResult`,
  `LandConflictError`; `app.LandTask`, `bint task land`. Bản vá `git diff --binary --full-index --no-renames Base..ảnh chụp`
  giới hạn trong `ScopeFiles ∩ diff`, khoá độc quyền cây gốc (`WaitLock`), `git apply --check` rồi `git apply` (không
  `--index`). Finalize của `b-do` (`Runner.landStep`): `Land` (kiểm điều kiện done ở worktree) → `done`; xung đột ⇒ `blocked`
  "xung đột khi đưa về: <tệp> …", đặt `AwaitingLand` ⇒ `nextStep` ở `in_review` vào thẳng finalize khi `resume`.
  `SetStatus(done)` của tác vụ worktree chưa đưa về bị chặn. Cổng done/diff/phạm vi của tác vụ đã đưa về dùng bản lưu
  trong `Landed` (`UnapprovedChanges`, `DoneInputFor`, `app.DiffTask`); `RootFor` lùi về `Origin` khi worktree đã gỡ.
- **Huỷ/dọn (5.4).** `bint task cancel <id> [--reason] [--discard]` (`app.CancelTask`); `SetStatus(cancelled)` (cả
  `task status <id> cancelled`) gỡ worktree/nhánh/ref/tệp khoá; còn thay đổi chưa đưa về ⇒ `ErrDiscardRequired`, `--discard`
  chỉ người (`ErrAgent` ở workflow, `ErrAgentForbidden` ở app). `Env.OrphanWorktrees` + `bint doctor` ("worktree mồ côi",
  mức warn, in lệnh git dọn; gồm thư mục không ứng với tác vụ, tác vụ đã kết thúc, worktree `--keep`, mục `prunable`).
  `run status`/`task show` hiện chế độ, worktree, nhánh, cây gốc, trạng thái đưa về.
- **Skill (5.5).** b-do (bước 1b chế độ cô lập, "song song" ⇒ `--worktree`, báo nơi chạy, `land` trước `done` khi chạy từng
  bước, xử lý "xung đột khi đưa về", báo cáo đưa về), b-review (cwd là worktree, không `land`), b-feat (cwd là worktree,
  tác vụ worktree chạy qua `bint run`); đồng bộ `.claude/skills`, `.agents/skills`, `.bint/skills/core.lock` bằng
  `bint sync`; `TestCoreSkillsMentionWorktreeIsolation`.
- **Ảnh hưởng (mục 10).** Runlog: `gitscan` thêm `--exclude=refs/bint/* --exclude=refs/heads/bint/*` và loại commit "bint
  base". TUI: hành động `tasks.land` (phím `o`) và `tasks.cancel` (phím `x`) trong sổ + bảng ánh xạ lệnh CLI (`cliRefs`),
  ô form `--worktree`/`--shared`, cột chế độ cô lập (`wt`, `wt✓`) và dòng Cô lập/Worktree/Cây gốc/Đưa về ở thẻ Phiếu,
  nhãn vi/en, golden cập nhật.

**Lệch so với kế hoạch ban đầu (và lý do):**

1. **Ref base dùng chung tên với G1** (`plan.md` của phiếu, giả định 1): `refs/bint/tasks/<id>/base` ghi base commit lúc
   tạo worktree, `b-feat` đầu ghi đè bằng commit `base_tree` cha là base commit; base vẫn tới được qua chuỗi cha và nhánh
   `bint/<id>`. Có `TestBaseCommitSurvivesFirstFeatAndDropTouched`.
2. **`--3way` không dùng `git apply --3way`** (ngầm ghi chỉ mục): phần sạch đi bằng `git apply`, tệp xung đột trộn bằng `git
   merge-file`; tệp nhị phân hoặc xoá/sửa ở một phía giữ nguyên và được liệt kê trong `Landed.Conflicts`. Đã kiểm bằng test
   `TestLandThreeWayHumanOnlyLeavesMarkers` (chỉ mục không đổi). Chưa thử riêng ca "git apply --3way ghi chỉ mục" như
   dự định: bỏ hẳn nên không cần.
3. **Lệnh `bint task cancel` mới** (trước chỉ có `task status <id> cancelled`); hai đường dùng chung một chỗ dọn.
4. **Tệp cục bộ do Bint quản lý** (`.bint/local.yml`, `.claude/settings.local.json`, `.codex/hooks.json`, `.agents/hooks.json`) được chép
   sang worktree theo danh sách cố định trong `worktree.go` (chép khi có ở cây gốc và chưa có ở worktree), không lấy động
   từ khối `.gitignore` của `init` (gói `app` import `workflow` nên không gọi ngược được).
5. **`Runner.Run` làm việc trên bản sao `Env` ngay từ đầu** (trước chỉ khi đổi Root): `RunID`/`Source`/`Root` đổi trong lượt
   không còn rò sang `TaskWS` dùng chung (trước đây lượt thứ hai trên cùng `TaskWS` bị coi là "chạy lồng" khi tác vụ chạy
   ở chính cây gốc). Phát hiện khi viết test dịch vụ; không đổi hành vi CLI (mỗi tiến trình một lượt).
6. **Phím TUI**: kế hoạch không nêu; `L` trùng phím toàn cục đổi ngôn ngữ (test `TestTaskWriteActionsRefusedInReadOnly` bắt
   được vì đổi ngôn ngữ giữa chừng) nên dùng `o` (đưa về cây gốc/origin) và `x` (huỷ). Cả hai không có trong chân phím
   (Footer=false) để không đổi bố cục; hiện ở bảng lệnh và trợ giúp.
7. **Quy tắc `auto` tính cả tác vụ bỏ dở** (đúng 5.1): trên repo có phiếu cũ chưa đóng, mọi phiếu mới vào worktree.
   Test cũ tạo nhiều tác vụ trên một repo nay ghi rõ `Isolation: shared` (`newHarness` của workflow đặt
   `cfg.Workflow.Isolation = shared`).
8. **Tệp tác vụ khác đã đưa về cây này** không bị quy cho tác vụ `shared` đang chạy (`outsideFeat` miễn khi blob hiện tại
   bằng blob trong `Landed.Tree` và tệp không phải đường dẫn bảo vệ/deny của tác vụ đó); sửa tay sau đó vẫn bị tính
   (`TestLandedFilesNotAttributedToSharedTask`).

**Chưa kiểm được / rủi ro còn lại:**

- **Windows chạy thật**: `git worktree`, `LockFileEx` trên tệp khoá bị xoá khi land/cancel, đường dẫn dài, `git apply` với
  CRLF mới kiểm biên dịch/vet/staticcheck.
- **Land làm mất hiệu lực kiểm chứng của tác vụ `shared` đang chạy trên cây gốc** (G1 giữ fingerprint toàn cây, có chủ ý):
  tác vụ `shared` đã verify mà tác vụ worktree đưa về trước khi nó finalize thì `blocked` "mã đã thay đổi sau lần kiểm
  chứng", cần `bint task verify` lại. E2E `TestWorktreeParallelEndToEnd` sắp thứ tự bằng tệp tín hiệu để B đưa về sau khi A xong.
- **`setup_command` chạy lệnh trong cấu hình** với quyền người dùng (không qua cơ chế xác nhận lệnh): chỉ đặt khoá này
  từ nguồn tin cậy; ghi ở `docs/configuration.md`.
- `WritableDirs` của agent là cả thư mục trạng thái nên b-feat (Codex `--add-dir`) về lý thuyết ghi được worktree của tác vụ
  khác; chưa thu hẹp (ngoài phạm vi).
- Land chỉ đưa tệp thuộc `ScopeFiles` của tác vụ: tệp không bị gitignore do lệnh kiểm chứng sinh ra **sau** `b-feat` (ví dụ
  `coverage.out`) cũng nằm trong `outsideFeat` nên được đưa về, như chế độ `shared` vẫn tính chúng cho tác vụ.
- `git apply` dùng chỉ dấu cách `--whitespace=nowarn`; tệp có bộ lọc eol/LFS khác nhau giữa worktree và cây gốc chưa thử.
- Chưa chạy một phiên `bint run` thật với harness (Claude/Codex) trong worktree và TUI bằng tay; mới có agent giả.
- Doctor không tự dọn worktree mồ côi (đúng phạm vi: chỉ in lệnh git).

**Kiểm thử đã thêm:** `internal/workflow/worktree_test.go` (chọn chế độ, chạy → land → dọn dẹp, base bẩn + copy + setup,
setup lỗi + resume, xung đột + resume không review lại, `--3way` chỉ người, `--keep`, điều kiện done/chủ sở hữu, cancel +
`--discard`, mồ côi, ref base, tệp đã đưa về không quy cho tác vụ khác, glob copy, phân tích lỗi `git apply`),
`internal/cli/worktree_e2e_test.go` (AC-1 hai tiến trình `bint` song song với tác nhân giả bằng `--agent-command` và tệp
tín hiệu, từng bước land/cancel/doctor, xung đột + `--3way` + resume, lỗi dùng sai cờ), `internal/app/worktree_svc_test.go`
(`asHuman`/`asAgent` cho `--3way`/`--discard`, DiffTask/ShowTask sau land, doctor), `internal/config`, `internal/task`,
`internal/gitrepo`, `internal/runlog` (gitscan), `internal/skills`, TUI (`tasks_test.go`, `model_test.go`, golden, bảng phủ
lệnh/cờ, kiểm kê literal nhắc lệnh CLI). **Kiểm đột biến (đỏ như mong đợi, đã hoàn nguyên):** `git apply --index` ⇒
`TestWorktreeRunLandsAndCleansUp` đỏ; bỏ `awaitLand` ⇒ `TestWorktreeLandConflictBlocksAndResumes` đỏ; bỏ `--exclude`/`--invert-grep`
của gitscan ⇒ `TestScanGitIgnoresBintRefsAndBranches` đỏ.

**Kết quả kiểm chứng (2026-11-12):** `go test -count=1 ./...` 55 gói `ok`, 1297 test (kể cả subtest) qua, 0 lỗi; `go vet ./...` và
`GOOS=windows go vet ./...` sạch; `staticcheck ./...` và `GOOS=windows staticcheck ./...` sạch; `go test -race` cho
`internal/workflow` (Worktree|Land|Cancel|Orphan|Isolation|BaseCommit|Concurrent) và `internal/cli` (Worktree) qua.

### 12.7. Vòng sửa thêm sau review G2 (phát hiện .1–.7), 2026-11-12, tác vụ 01M777NWWZA4J9F66Q1AKEAFFX

Mỗi phát hiện medium có một test tái hiện (đã kiểm đột biến: tắt bản sửa ⇒ test đỏ).

- **.1 `done --force` bỏ qua land** — `SetStatus(done)` trả `NotDoneError` khi `State.NeedsLand()` kể cả có `--force`
  (`ops.go`); chỉ còn `land` hoặc `cancel --discard`. Doctor: `OrphanWorktree.Unlanded` (tác vụ `done` mà `Landed == nil`, ví
  dụ phiếu do bản cũ `--force`) không gợi ý `worktree remove --force`/`branch -D`, chỉ `git status`/`git diff HEAD` ở worktree.
  Test: `TestDoneForceCannotSkipLand`, `TestOrphanUnlandedDoneNotSuggestedForRemoval`.
- **.2 `--3way` gỡ worktree dù có tệp không trộn được** — `mergeOne` trả `mergeClean/mergeMarkers/mergeSkipped`;
  `Landing.Unmerged` (⊂ `Conflicts`) ghi tệp không trộn được; có chúng thì worktree được giữ (`Landed.Kept`, `Note`), nhánh
  `bint/<id>` còn, `LandResult.Notes` nêu cách lấy bản của tác vụ; doctor báo kèm lý do. Test: `TestLandThreeWayUnmergedKeepsWorktree`.
- **.3 resume → done khi còn dấu xung đột** — `DoneInput.LandConflicts` (tính bởi `landConflictsLeft` từ `Landed.Conflicts`: tệp
  còn dòng `<<<<<<<` ở cây gốc, và mọi tệp `Unmerged`) là blocker của `DoneBlockers`. Gỡ dấu xong thì `resume` ⇒ done; tệp không
  trộn được thì người xác nhận bằng `done --force` (Bint không biết khi nào người ghép xong). `TestLandThreeWayHumanOnlyLeavesMarkers`
  sửa lại: resume khi còn dấu ⇒ blocked, gỡ dấu ⇒ done.
- **.4 `conflictFiles` phụ thuộc ngôn ngữ của git** — `gitApply` đặt `LC_ALL=C`, `LANGUAGE=`. Test: `TestGitApplyForcesEnglishMessages`
  (chỉ khẳng định thông báo vẫn khớp khi môi trường đặt vi_VN; máy này git không có bản dịch nên chưa phân biệt được với trước khi sửa).
- **.7 `Paths.Worktrees` không dùng** — `state.WorktreesDirFor(tasksDir)` là nơi duy nhất suy thư mục worktree từ `Tasks.Dir`
  (`Env.worktreesDir` gọi nó; `state.At` dùng cùng hằng); `TestWorktreesDirMatchesPaths` bắt lệch bố cục.
- **.5 ref base bị `pinTree` ghi đè; `Landed.Tree`/`BaseTree` không có ref sau khi dọn** — **không sửa**. Nhánh `bint/<id>` giữ
  base commit tới lúc land nên chưa hỏng; sau khi tác vụ kết thúc mọi ref của nó bị xoá có chủ ý ở G1 (không ghim cây vĩnh viễn trong
  repo của người dùng), nên `task diff` của tác vụ đã xong sau `git gc` hỏng cũng đúng với tác vụ `shared` của G1. Đổi cần quyết định
  chung (giữ ref `landed` tới khi nào?), để cho vòng sau.
- **.6 `setup_command` chạy không cần xác nhận** — **chỉ ghi tài liệu** (`docs/configuration.md`). Bắt xác nhận kiểu băm lệnh như
  `verify.command` đòi thêm luồng duyệt/trạng thái chờ ở `bint run` đầu tiên; ngoài phạm vi vòng sửa này. Rủi ro còn: agent ở
  tác vụ `shared` sửa `.bint/config.yml` rồi tác vụ worktree sau chạy lệnh đó.
- **Test chập chờn đã sửa** — `TestWorktreeParallelEndToEnd` (cli) treo khi A (shared, giữ khoá độc quyền cây gốc suốt b-feat) vào trước
  B: B cần khoá chia sẻ để tạo worktree còn A chờ B bắt đầu. Test nay khởi động B và chờ `State.Origin` trước khi khởi động A
  (6/6 lần qua với `-count=6`; trước đó ~50% đỏ). Đây là tính chất đúng của thiết kế khoá, không phải lỗi sản phẩm.
- **Kết quả (2026-11-12):** `go test -count=1 ./...` 66 gói, 1302 test qua, 0 lỗi; `go vet` và `GOOS=windows go vet` sạch;
  `staticcheck` và `GOOS=windows staticcheck` sạch. `TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch` (chập chờn khi máy bận,
  ngoài mục tiêu) qua lần này.

### 12.8. Sửa phát hiện review vòng 2 của G2 (.8–.11), 2026-11-12, cùng tác vụ

Người dùng yêu cầu orchestrator sửa trực tiếp (không qua b-feat). Test ở `internal/workflow/land_review_test.go`.

- **.8 cancel gỡ worktree còn giữ tệp không trộn được** — `checkCancelWorktree`: tác vụ đã đưa về mà `Landed.Unmerged` khác rỗng
  thì trả `ErrDiscardRequired` liệt kê các tệp đó; chỉ người `--discard` mới gỡ được. Test: `TestCancelKeepsWorktreeWithUnmergedFiles`.
- **.9 Land không kiểm lại `Landed` dưới khoá** — sau khi lấy khoá độc quyền cây gốc, `Land` nạp lại phiếu; đã đưa về hoặc đã kết
  thúc thì trả lỗi "đã đưa về"/"đã <trạng thái>" thay vì áp lần hai (hoặc báo xung đột giả). Test: `TestLandRechecksLandedUnderLock`.
- **.10 dấu xung đột** — `landConflictsLeft` chỉ khớp dòng `<<<<<<< cây gốc` (nhãn do `mergeOne` ghi, hằng `mergeLabelOrigin`);
  dòng `<<<<<<< ` có sẵn trong tệp không chặn done. Test: `TestLandConflictsLeftMatchesOwnLabel`.
- **.11 doctor gợi ý xoá worktree đang chờ ghép** — `OrphanWorktree.AwaitingMerge` (giữ vì `Unmerged`, tác vụ chưa kết thúc):
  `CleanupCommands` và doctor chỉ in lệnh xem, như `Unlanded`. Test: `TestOrphanAwaitingMergeNotSuggestedForRemoval`.
