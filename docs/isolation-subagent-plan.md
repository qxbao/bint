# Kế hoạch: cô lập tác vụ chạy song song và điều phối b-do bằng subagent

Trạng thái: **đã chốt hướng** (2026-11-11), chưa triển khai. Gồm bốn giai đoạn: G1 (cô lập trên
worktree chung), G2 (mỗi tác vụ một worktree, chế độ `auto`), G3 (driver session: b-do chạy các bước
bằng subagent), G4 (nhiều tác vụ song song bằng subagent).

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
  thay đổi ảnh chụp. Thông báo vi phạm thêm: "nếu chính bạn sửa trong lúc bước chạy: bint run resume".

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

### 4.5. Nghiệm thu G1

- Kiểm thử đồng thời (hai `Runner` với agent giả trong cùng thư mục tạm): b-review của A không fail khi
  B có b-feat chờ; B chờ tới lúc A trả khoá rồi chạy.
- Kiểm thử khoá: chia sẻ + chia sẻ cùng lúc được; chia sẻ chặn độc quyền và ngược lại; tiến trình giữ
  khoá chết ⇒ khoá tự nhả; không có tình huống hai người cùng giữ độc quyền khi trả khoá xen kẽ
  (chạy `-race -count=50`).
- `checkScope` không tính tệp chưa commit có sẵn trước khi lập phiếu, không tính tệp của tác vụ khác.
- `bint run resume` ở thư mục khác chạy đúng `State.Worktree`.
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

1. **G1** (nhỏ–vừa): sửa ngay cảm giác "bị nhạy cảm với thay đổi" trên worktree chung.
2. **G2** (vừa): nền cho mọi việc song song.
3. Spike S1–S3 (nhỏ).
4. **G3** (vừa–lớn): phần lớn là tách `runStep` và hợp đồng thuê.
5. **G4** (vừa): guardrail dựng sẵn và hướng dẫn song song.

Mỗi giai đoạn một nhánh/PR, có review bằng b-review, tài liệu cập nhật cùng mã.

## 12. Ghi chép triển khai

(Chưa có.)
