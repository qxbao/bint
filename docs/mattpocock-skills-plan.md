# Kế hoạch: tích hợp bộ skill của Matt Pocock và pstack (Lauren Tan) vào Bint

> Nguồn: [mattpocock/skills](https://github.com/mattpocock/skills) @ `abeeaf9` (2026-11-19, plugin v1.3.2, giấy phép MIT) và
> [pstack của Lauren Tan (poteto)](https://github.com/cursor/plugins/tree/main/pstack) @ `7a58391` (2026-11-21, giấy phép MIT; mục 4b).
> Lập ngày 2026-11-23 từ phiên nghiên cứu; mỗi giai đoạn (GĐ) lập một phiếu b-do riêng.

Trạng thái (2026-11-23): **GĐ0–GĐ3, GĐ4b và TSK-5.1 đã viết mã** (làm thẳng trong một phiên, không qua phiếu b-do). Đã chốt: cách
hỏi-đáp A + B (mục 3.1), 0 agent con ở bước implement (mục 3.2), đưa thẳng vào skill gốc nhúng (mục 3.3), `GLOSSARY.md` ở gốc repo
(mục 3.4). Đã chạy TSK-1.4, TSK-5.2 (validation) và kiểm Codex (rủi ro 1).
Còn lại: TSK-5.3, eval trên tập test khi có thêm tác vụ, GĐ4 và GĐ6 (chỉ làm khi cần). Xem mục 9.

## 1. Mục tiêu và kết luận khả thi

Mục tiêu: đưa vào Bint các **kỷ luật kỹ thuật** của bộ skill Matt Pocock (phỏng vấn làm rõ, ngôn ngữ miền chung, TDD theo seam,
chẩn đoán lỗi có vòng phản hồi, thiết kế module sâu, review hai trục, retro môi trường) mà không phá các cổng của Bint (hợp đồng v1,
kiểm chứng đã xác nhận, phạm vi sửa, người duyệt, ngân sách).

Kết luận:

- **Kỹ thuật: khả thi cao.** Mỗi skill là `SKILL.md` thuần + tệp phụ; frontmatter chỉ dùng `disable-model-invocation` và
  `argument-hint`, đúng hai khoá `customKeys` cho phép (`internal/skills/custom.go`). GĐ1–GĐ2 không cần viết Go.
- **Triết lý: hợp khoảng 60–70 %.** Matt: "skill nhỏ, ghép được, không nắm quy trình, người cầm lái". Bint: nắm quy trình bằng cổng
  và bằng chứng. Vì vậy **nhập phần kỷ luật, bỏ phần điều phối** (spec → ticket → implement đã có b-req → `G-N` → b-feat).
- **Đưa thẳng vào skill gốc** (mục 3.3): Bint chưa có người dùng ngoài, nên không cần giai đoạn thử riêng trên một repo. Số đo ở GĐ5 dùng để
  giữ hay gỡ từng thành phần, không phải cổng trước khi vào skill gốc.

## 2. Triết lý của Matt và chỗ khớp với Bint

| Ý tưởng | Bint đã có | Phần mới cần lấy |
|---|---|---|
| **Grilling**: hỏi theo vòng, mỗi vòng hỏi trọn *frontier* (mọi câu đã đủ tiền đề), mỗi câu kèm đáp án đề xuất; tra sự thật là việc của agent, quyết định là của người | b-req hỏi ≤ 2–3 câu `Q-N` khi yêu cầu mơ hồ; nối tiếp phiên planner sau khi trả lời | Vòng hỏi có đề xuất ("có" = chấp nhận đề xuất), tính frontier |
| **Ngôn ngữ chung** (`GLOSSARY.md`, ADR thưa: chỉ khi khó đảo ngược + gây bất ngờ + có đánh đổi thật) | Tri thức loại `decision`; `inspect` nhận thư mục ADR | Bảng thuật ngữ miền và kỷ luật đặt tên |
| **TDD tại seam đã thống nhất**, lát dọc (tracer bullet), tránh test lặp lại logic của mã, refactor không nằm trong vòng đỏ → xanh | b-goal, cổng đỏ `expect: fail`, bgraph | Chốt seam trước, lát dọc, nhận diện test sai kiểu |
| **Chẩn đoán lỗi**: vòng phản hồi chặt → thu nhỏ repro → 3–5 giả thuyết bác bỏ được → đo một biến → sửa + test hồi quy → dọn | b-repro → b-fix | Thu nhỏ, giả thuyết có dự đoán, log debug gắn thẻ, "không có seam đúng" là một phát hiện |
| **Module sâu** (Ousterhout): module, interface, depth, seam, adapter, leverage, locality; deletion test | Chưa có | Mới hoàn toàn |
| **Review hai trục**: Standards và Spec tách riêng, không xếp hạng chung; nền code smell của Fowler | b-review chấm theo mục tiêu, AC, tri thức | Tách trục, smell ở mức `suggestion` |
| **Retro**: sửa *môi trường* (lỗi máy móc → check tự động, lỗi phán đoán → quy ước) | Tiến hoá, bài học từ lượt hỏng | Phiên retro do người khởi động |
| **Writing-for-agents**: context pointer, leading word, tránh phủ định, dọn câu không đổi hành vi | b-add-skill (TDD cho skill) | Tài liệu tham chiếu cho b-add-skill và tri thức |

## 3. Quyết định đã chốt

### 3.1. Hỏi-đáp với người: A + B (không dùng terminal multiplexer)

Bước của `bint run` chạy không tương tác (`claude -p`, `codex exec`), nên grilling nhiều lượt không chạy trực tiếp trong bước được.

- **A. Grilling trước khi lập phiếu**, trong phiên tương tác (phiên điều phối `/b-do` hoặc `/grill-with-docs`), rồi mới
  `bint task new` kèm tiêu chí. Không cần mã. Đúng luồng grill-with-docs → to-spec của Matt.
- **B. Vòng `Q-N` trong b-req**: b-req dừng với câu hỏi, người trả lời, `bint run resume --answer` **nối tiếp chính phiên planner**
  (`claude -p --resume <session_id>`, đã có trong `docs/step-continuation-plan.md`), không mất ngữ cảnh. Chỉ đổi skill: câu hỏi có
  `Đề xuất:` theo khuôn grilling.
- **Loại:** mở pane tương tác bằng zellij/tmux. zellij không phải đường chạy được trên Windows; tmux đã bị loại ở
  `docs/step-continuation-plan.md` (Ngoài phạm vi). Chỉ xét lại (dạng "tầng mở terminal" có nhiều backend: `wt`, zellij, tmux, Orca;
  tín hiệu xong qua hook và lint tài liệu, không đọc màn hình) nếu A + B không đủ khi dùng thật.

### 3.2. Agent con

Căn cứ: `docs/evals/bench-bint-vs-plain-v3-20261117/README.md` (dòng 43, 139) và commit `72aa43c`.

| Đo ở đợt 3 (Codex) | T1 | T2 |
|---|---|---|
| Phiên b-feat chính | 44,1M token vào | 25,6M |
| 3 agent con b-feat tự sinh (50–105 lượt gọi, ngữ cảnh 165–207k) | 27,3M (≈ 9,1M/con) | 32,0M (≈ 10,7M/con) |
| Phần cộng thêm so với phiên chính | +62 % | +125 % |
| Tỉ lệ trên tổng token nhánh Bint | ≈ 25 % | ≈ 25 % |

Chất lượng không tăng (T2 6C/7M so với nhánh không Bint 5C/2M, chạm trần 90 phút). Sau `72aa43c` chi phí 11,3× → 2,51×, chất lượng không
đổi (mức giảm gộp với hai sửa khác, phần chắc chắn do agent con là ≈ 25 %).

- **Bước implement (b-feat, b-fix, b-repro): 0 agent con.** Giữ nguyên quy tắc hiện tại.
- **Bước review/plan chỉ đọc: chưa có số đo.** Không bật mặc định; chỉ thử như nhánh thí nghiệm ở GĐ5 (≤ 2 agent con theo hai trục,
  trần khoảng 30 lượt gọi mỗi con — ước lượng, không từ eval; giữ lại khi token bước review tăng ≤ ~50 % và số phát hiện
  high/critical có repro hợp lệ tăng).
- **Ngoài `bint run`** (phiên tương tác): skill của Matt dùng agent con như bản gốc.

Skill được nhập vào bước `bint run` đều **không bắt buộc** agent con:

| Mức dùng | Skill | Cách xử lý |
|---|---|---|
| Không dùng | `tdd`, `diagnosing-bugs`, `domain-modeling`, `writing-for-agents` | Dùng nguyên |
| Phụ | `grilling` (tra sự thật, `grilling/SKILL.md:28`) | Trong `bint run`: tự tra ngay trong phiên |
| Phụ | `improve-codebase-architecture` (1 agent khảo sát), `codebase-design` → `DESIGN-IT-TWICE.md` (≥ 3 agent, nhánh tuỳ chọn) | Chỉ chạy ngoài `bint run` |
| Cốt lõi | `code-review` (2 agent song song), `implement-spec`, `research`, `wayfinder` | Không nhập (mục 4) |

### 3.3. Đưa vào skill gốc nhúng (Q-1, đã trả lời)

Bint chưa có người dùng ngoài, nên skill nhập và phần kỷ luật đi thẳng vào `internal/skills/core/` (mọi dự án chạy `bint init`/`bint sync`
đều nhận), và sửa thẳng `b-req`, `b-feat`, `b-review`, `b-repro`, `b-fix` thay vì dùng lớp phủ. Hệ quả:

- **Skill gốc hiện chỉ nhúng và xuất bản `SKILL.md`** (`internal/skills/skills.go:65`, `go:embed core`), trong khi skill của Matt cần tệp
  phụ (`tests.md`, `mocking.md`, `DEEPENING.md`, `agents/openai.yaml`, script). Phải thêm hỗ trợ tệp phụ cho skill gốc (GĐ1).
- Tên giữ như bản gốc, không thêm tiền tố `b-` (để câu "Call the Skill tool with grilling" trong các skill khác vẫn đúng). Tên skill gốc mới
  thành tên cấm cho flow và skill tuỳ biến (luật trùng tên sẵn có); kiểm lại `bint flow lint`, golden màn Skill, `core.lock`.
- Rủi ro "b-repro/b-fix không có điểm neo" không còn: sửa thẳng skill gốc.

### 3.4. `GLOSSARY.md` ở gốc repo (Q-2, đã trả lời)

Đặt ở gốc repo như bản gốc của Matt (`GLOSSARY-MAP.md` khi có nhiều ngữ cảnh), để skill nhập dùng nguyên không cần sửa.

## 4. Phân loại 27 skill

**Nhập gần nguyên văn (skill tự gọi), thêm mục "Khi chạy dưới Bint":** `grilling`, `domain-modeling`, `codebase-design`
(kèm `DEEPENING.md`, `DESIGN-IT-TWICE.md`), `tdd` (kèm `tests.md`, `mocking.md`), `diagnosing-bugs` (kèm `scripts/hitl-loop.template.sh`),
`writing-for-agents` (kèm `SKILL-MECHANICS.md`).

**Nhập và viết lại cho Bint (skill do người gọi):**

| Tên trong Bint | Gốc | Điều chỉnh |
|---|---|---|
| `grill-with-docs` | `grill-with-docs` + `to-spec` | Gọi `grilling` và `domain-modeling`; kết thúc bằng bản nháp phiếu (`bint task new` + `--criteria` + `--allow`, chưa chạy) hoặc chuyển `/b-do`; không đăng issue |
| `improve-arch` | `improve-codebase-architecture` | Khảo sát bằng MCP `cg_*` + `git log` tìm điểm nóng; báo cáo HTML ra `%TEMP%`/`$TMPDIR`; ứng viên được chọn → grilling → `grill-with-docs` |
| `retro` | `retro` | Đọc lượt chạy của Bint (`bint run status`, lỗi bước, `review.json`); đầu ra là đề xuất `bint knowledge add`/`report_observation`/hook; không tự duyệt |
| `handoff` | `handoff` | Trỏ tới phiếu, `requirement.md`/`plan.md`, commit; không chép lại; ghi ra thư mục tạm |
| (tuỳ chọn) `prototype`, `research` | cùng tên | Chỉ ngoài `bint run`; xét sau GĐ2 |

**Không nhập:**

| Skill | Lý do |
|---|---|
| `to-spec`, `to-tickets`, `implement`, `implement-spec`, `triage`, `setup-matt-pocock-skills` | Trùng b-req (`G-N`, `--parent`/`--depends-on`), b-feat, kho phiếu; nhập vào thì đi vòng qua cổng kiểm chứng, phạm vi và duyệt người. Chỉ lấy ý: seam cao nhất, lát dọc, expand–contract |
| `code-review` | Trùng tên `/code-review` có sẵn của Claude Code (Matt ghi ở `.out-of-scope/harness-name-collisions.md`); dựa vào 2 agent con. Lấy ý hai trục vào b-review |
| `wayfinder` | Cần issue tracker; có thể làm lại trên phiếu cha/con của Bint sau này |
| `ask-matt` | Bộ định tuyến riêng của repo Matt; thay bằng mục định tuyến trong `/b-do` (điểm neo `routing`) |
| `pr`, `wizard`, `teach`, `to-questionnaire`, `wait-what`, `grill-me` | Ngoài trọng tâm hoặc đã phủ bởi `grill-with-docs` |
| `misc/`, `in-progress/`, `deprecated/` | Matt không phát hành chính thức |

## 4b. Nguồn thứ hai: pstack (Lauren Tan)

pstack là bộ skill Lauren Tan (poteto, React core, Cursor) dùng hằng ngày: 61 `SKILL.md` gồm `poteto-mode` (bộ định tuyến 22 playbook),
24 skill nguyên tắc một dòng, và các skill như `correct`, `interrogate`, `blast-radius`, `create-verification-skill`. Viết cho Cursor trước,
có bộ chuyển cho Claude Code. Bài nói "All Code Is Slop (Except Mine)" chỉ đọc qua bản tóm tắt của bên thứ ba (superbash.ai 2026-10-28,
ai-redteam.com 2026-11-14), nên các ý dưới đây lấy từ mã nguồn pstack là chính.

### Triết lý

- **Đường cong tin cậy:** tự chủ chỉ tăng khi độ tin cậy tăng; ba đòn bẩy là kiểm chứng trên app đang chạy, skill có eval, kiến trúc
  được cưỡng chế. Trùng hướng với Bint (bằng chứng, đo lường, cổng).
- **Bỏ vai "người chuyển lời":** mỗi lần agent cần người giúp lại, hỏi "thiếu năng lực gì" rồi xây năng lực đó.
- **Biến lỗi lặp lại thành kiểm tra chạy được** (`correct`, `principle-encode-lessons-in-structure`): kiến trúc → kiểu → lint/CI có thông
  báo nêu cách sửa → test → tài liệu cuối cùng; chứng minh mỗi check đỏ trên một lỗi thật trong quá khứ; giữ bảng "quy tắc ↔ cái gì cưỡng chế".
- **Chứng minh trên thứ thật** (`principle-prove-it-works`, `create-verification-skill`): chạy tính năng, không tin proxy hay "biên dịch
  được"; mỗi dự án có skill `verify-<app>` (launch, doctor, drive, evidence, cleanup) và bản đồ tính năng.
- **Phát hiện chỉ sống khi có lần chạy chứng minh** (`interrogate`), và **thang chắc chắn 1–5** (`blast-radius`: tự nói → chỉ dòng →
  lập luận đường hỏng → chạy script/test → tái hiện trong app).
- **Tấn công tiền đề** (`principle-attack-the-premise`): hai lần sửa cùng một tiền đề hỏng cùng một cổng thì viết tiền đề ra và kiểm nó.

### Chỗ xung đột với Bint và Matt

| pstack | Xử lý |
|---|---|
| "Tôi không tin vào lập kế hoạch, spec tốt nhất là mã" | Giữ b-req và hợp đồng v1. Lấy phần tương thích: bản nhỏ nhất đạt mục tiêu, kế hoạch ngắn |
| `never-block-on-the-human`: làm rồi trình bày, chỉ hỏi khi không đảo ngược được | Tương thích với "Giả định thay người dùng" của b-req. Phân vai với grilling (A + B): **hỏi người về hướng sản phẩm, không hỏi về thực thi** |
| Đa mô hình, nhiều agent con song song (`arena`, `swarm`, `interrogate`, ủy quyền cho `poteto-agent`); tác giả tự nói làm ở nơi "token không giới hạn" | Không nhập (mục 3.2). Đa mô hình ở Bint là theo role từng bước (`roles.*`), không phải tỏa nhánh trong bước |
| Gắn với Cursor (`/loop`, automations, Bugbot, `cursor-team-kit`), GitHub, slug mô hình | Không nhập playbook vận hành (babysit, shipping, autopilot, overnight) |

### Phân loại

| Nhập (viết lại cho Bint) | Vào đâu |
|---|---|
| `correct` + `principle-encode-lessons-in-structure` | Gộp với `retro` của Matt thành một skill (GĐ2, TSK-2.3) và trường `enforced_by` cho tri thức (GĐ4b) |
| `create-verification-skill`, `maintain-verification-skill`, `principle-prove-it-works` | GĐ4b: skill gốc sinh `.bint/skills/verify-<app>/` |
| `blast-radius` (thang 1–5, "một sự thật làm thay đổi an toàn") | b-review (GĐ3, TSK-3.3) |
| `interrogate` (chỉ giữ phát hiện có lần chạy chứng minh) | b-review: mở rộng luật `repro` sẵn có, chạy tuần tự, không tỏa nhánh |
| `principle-attack-the-premise`, `principle-fix-root-causes` | b-fix và vòng sửa của b-feat (GĐ3, TSK-3.4, TSK-3.6) |
| Nguyên tắc chọn lọc: `laziness-protocol`, `subtract-before-you-add`, `model-the-domain`, `boundary-discipline`, `make-operations-idempotent`, `separate-before-serializing-shared-state`, `sequence-verifiable-units`, `test-behavior-not-implementation`, `explain-the-number`, `prove-it-works` | Một skill gốc `principles` (mục lục một dòng mỗi nguyên tắc + tệp lá là tệp phụ), không tạo 24 skill để khỏi tốn ngữ cảnh vì mô tả |
| `why` (trả lời "vì sao" từ commit/PR, đánh dấu suy luận) | Tuỳ chọn, GĐ2 |

**Không nhập:** `poteto-mode` (định tuyến đã có `/b-do`), `arena`, `swarm`, `interrogate` dạng tỏa nhánh, playbook vận hành, `tdd` (dùng bản
của Matt), `unslop`/`no-comments`/`technical-writing` (văn phong tiếng Anh), `formal-verification`/`statechart` (hẹp), `automate-me`
(Bint đã có tiến hoá skill từ lịch sử lượt chạy), `ketchup`, `bro`, `make-bot-ui`, `setup-pstack`.

## 5. Rủi ro và điểm vướng

1. **Codex/Antigravity bỏ `disable-model-invocation`** khi xuất bản ra `.agents/skills` (`internal/skills/skills.go:186`). Skill do người
   gọi phải giữ `agents/openai.yaml` (`policy.allow_implicit_invocation: false`), được xuất bản như tệp phụ. **Chưa kiểm chứng** Codex đọc
   tệp này trong `.agents/skills/<tên>/agents/`: TSK của GĐ1.
2. **`GLOSSARY.md` sửa trực tiếp**, không qua duyệt tri thức. Chấp nhận vì cùng mô hình tin cậy "review qua Git" như skill tuỳ biến và
   lớp phủ; ghi thành ADR ở GĐ0.
3. **Skill gốc chưa có tệp phụ** (mục 3.3): cần mã nhúng, xuất bản, băm `core.lock` và phát hiện sửa tay cho cả cây thư mục skill,
   không chỉ `SKILL.md`. Sửa skill gốc làm đổi golden (`flow_golden.json`, màn Skill) và các bản đã xuất bản trong repo.
4. **Script `.sh`** (`hitl-loop.template.sh`) cần bash: trên Windows chạy qua Git Bash, đường dẫn viết bằng `/` (tri thức
   `01M7CX9V792957T6MGK6QWEJ7F`).
5. **"Call the Skill tool"** là cách gọi của Claude Code; Codex kích hoạt skill theo mô tả. Kịch bản `bint skill test` đo cả hai harness.
6. **Lệch bản gốc theo thời gian.** GĐ6 ghi nguồn/sha/giấy phép; trước đó, sha `abeeaf9` ghi trong ADR và trong tệp `UPSTREAM.md` của từng skill.
7. **Phình ngữ cảnh.** Mỗi mô tả skill tự gọi nằm trong ngữ cảnh mọi lượt; 7 skill mới (6 của Matt + `principles`) cộng thêm mô tả vào
   mọi phiên. GĐ5 đo cả chi phí này, không chỉ tỉ lệ đạt.
8. **Hai nguồn trùng nhau** (TDD, test hành vi, lát dọc/đơn vị kiểm chứng được, retro/correct). Mỗi ý chỉ giữ một nơi (bản của Matt cho
   `tdd`, gộp `retro` + `correct`), các skill khác trỏ tới đó.
9. **Kiểm chứng trên app thật cho Bint** là CLI/TUI: tmux không chạy native trên Windows (mục 3.1), nên `verify-bint` cần cách lái PTY
   chạy được trên Windows hoặc dựa vào bộ test TUI sẵn có. Kiểm lại ở GĐ4b.

## 6. Kế hoạch theo giai đoạn

### GĐ0 — Quyết định và pháp lý (≈ 0,5 ngày, không mã)

- TSK-0.1: ADR `docs/adr/0001-mattpocock-skills.md` (hoặc mục tri thức `decision`): nhập kỷ luật, không nhập điều phối; nguồn `@abeeaf9`;
  A + B; 0 agent con ở implement; đưa thẳng vào skill gốc; `GLOSSARY.md` ở gốc repo, sửa trực tiếp. — xong khi: ADR có trong repo.
- TSK-0.2: Quy ước giấy phép: mỗi thư mục skill nhập có `LICENSE` (MIT, bản gốc) và `UPSTREAM.md` (repo, đường dẫn, sha, danh sách sửa đổi).
- TSK-0.3: Quy ước ngôn ngữ: thân skill giữ tiếng Anh; mục "Khi chạy dưới Bint" và lớp phủ viết tiếng Việt.

### GĐ1 — Tệp phụ cho skill gốc, rồi nhập skill kỷ luật (≈ 4–5 ngày, có Go)

- TSK-1.0: Skill gốc có tệp phụ: nhúng cả thư mục `internal/skills/core/<tên>/` (trừ `tests/`), `bint sync` xuất bản tệp phụ ra
  `.claude/skills` và `.agents/skills`, `core.lock` băm từng tệp, sửa tay tệp phụ → xung đột như `SKILL.md`, gỡ tệp phụ không còn trong bản
  mới. Giữ bit thực thi cho script (`*_unix.go`/`*_windows.go` nếu cần, tri thức `01M746NASVKV9XMRHA4QNHJ3R8`). — xong khi: test xuất bản
  một skill gốc có tệp phụ lồng thư mục, có ca sửa tay và ca gỡ tệp.
- TSK-1.1: Chép 6 skill tự gọi (mục 4) vào `internal/skills/core/<tên>/` kèm tệp phụ, `agents/openai.yaml`, `LICENSE`, `UPSTREAM.md`.
- TSK-1.2: Thêm mục **"Khi chạy dưới Bint"** vào từng skill:
  - mọi skill: trong `bint run` không sinh agent con; trích dẫn tri thức qua `cite_knowledge`; đọc `GLOSSARY.md` nếu có;
  - `grilling`: trong `bint run`, tự tra sự thật ngay trong phiên; câu hỏi cho người ghi thành `Q-N` có `Đề xuất:`;
  - `tdd`: seam lấy từ mục `## Seam kiểm thử` của `plan.md` (GĐ3), chạy test hẹp, không chạy cả bộ;
  - `diagnosing-bugs`: mỗi giả thuyết bị bác ghi `bint task error <id> --step <bước>`; vòng phản hồi chính là `verify.command` khi có.
- TSK-1.3: Viết kịch bản áp lực cho mỗi skill, ví dụ (`bint skill test` hiện chỉ chạy skill tuỳ biến: hoặc mở rộng nó cho skill gốc, hoặc
  chạy kịch bản trên bản chép tạm trong `.bint/skills/` rồi xoá):
  - `tdd`: "viết hết test trước cho nhanh" → kỳ vọng làm lát dọc, một test một lần;
  - `diagnosing-bugs`: "đoán nguyên nhân rồi sửa luôn" → kỳ vọng dựng lệnh tái hiện đỏ trước;
  - `domain-modeling`: dùng thuật ngữ mâu thuẫn glossary → kỳ vọng chỉ ra mâu thuẫn.
- TSK-1.4: `bint skill test --arm both` (xem ước tính, hỏi người trước khi `--yes`); skill có nhánh `with` không hơn nhánh `without` thì
  sửa lại hoặc bỏ.
- TSK-1.5: Cập nhật danh sách skill gốc (trợ giúp `bint skill`, golden màn Skill, `core.lock`), `bint sync`; kiểm `.claude/skills/<tên>` và
  `.agents/skills/<tên>`; thử Codex với một skill do người gọi xem có tự kích hoạt không (rủi ro 1).
- Tiêu chí: 6 skill được xuất bản kèm tệp phụ; mỗi skill có ≥ 1 kịch bản và kết quả RED/GREEN lưu lại; rủi ro 1 có kết luận;
  vet/staticcheck cả `GOOS=windows`.

### GĐ2 — Viết lại 4 skill do người gọi, thành skill gốc (≈ 2 ngày)

- TSK-2.1: `/grill-with-docs` (A): gọi `grilling` + `domain-modeling`; khi frontier rỗng và người xác nhận, in lệnh `bint task new … --criteria …
  --allow …` (người chạy, hoặc chuyển `/b-do` với yêu cầu đã chốt). Không tự `bint task approve`.
- TSK-2.2: `/improve-arch`: khảo sát bằng `cg_*` + điểm nóng `git log`; báo cáo HTML ở thư mục tạm; người chọn ứng viên → grilling.
- TSK-2.3: `/retro` (gộp `retro` của Matt + `correct` của pstack): nguồn là lượt chạy, phát hiện review, lỗi bước và phiếu của Bint;
  nhóm thành lớp lỗi (một lớp tính khi xảy ra ≥ 2 lần); sửa ở mức cao nhất làm được: kiến trúc → kiểu → lint/CI có thông báo nêu cách
  sửa → test → tri thức (`bint knowledge add`, chờ duyệt) cuối cùng; mỗi check mới phải được chứng minh đỏ trên một lỗi thật đã xảy ra;
  câu hỏi "ở đâu agent lại cần người giúp" là một nguồn lớp lỗi. Không tự duyệt.
- TSK-2.6 (tuỳ chọn): `/why`: trả lời "vì sao mã có dạng này" từ commit, PR và tri thức `decision`, đánh dấu suy luận.
- TSK-2.4: `/handoff`: tài liệu ra thư mục tạm, trỏ tới phiếu/artifact, có mục "skill nên dùng", che bí mật.
- TSK-2.5: Mục định tuyến trong skill gốc `/b-do`: khi nào dùng `/grill-with-docs`, `/improve-arch`, `/retro`.
- Tiêu chí: 4 skill được xuất bản và có kịch bản; chạy thử `/grill-with-docs` → `/b-do` trên một yêu cầu thật của repo.

### GĐ3 — Đưa kỷ luật vào skill gốc của workflow (≈ 2–3 ngày, rồi đo ở GĐ5)

Sửa thẳng `internal/skills/core/*` (mục 3.3); giữ nguyên các điểm neo hiện có để lớp phủ của dự án vẫn áp được.

- TSK-3.1 `b-req` (B + ý của to-spec/to-tickets):
  - câu hỏi `Q-N` có `Đề xuất:` và hỏi theo frontier (vẫn giữ trần câu hỏi cho yêu cầu mơ hồ); dùng từ vựng `GLOSSARY.md`;
  - `plan.md` thêm mục `## Seam kiểm thử` (seam cao nhất, càng ít càng tốt); `G-N` cắt theo lát dọc; thay đổi rộng theo expand–contract.
  - Nếu lint bắt buộc mục `## Seam kiểm thử` thì đó là đổi hợp đồng: thêm mã lỗi mới ở mức cảnh báo trước.
- TSK-3.2 `b-feat`: một test → một bản cài đặt tại seam đã chốt; refactor không nằm trong vòng; gọi skill `tdd`.
- TSK-3.3 `b-review`: báo cáo tách hai trục **Spec** (mục tiêu, AC, bề mặt) và **Standards** (tri thức, quy ước + nền code smell ở
  `suggestion/low`); không trộn thứ hạng; chạy tuần tự, không agent con. Định dạng `bint task review` không đổi (trục ghi trong `title`/`body`
  hoặc thêm trường tuỳ chọn `axis`). Thêm từ pstack: tìm **một sự thật làm thay đổi an toàn** và ghi nó đạt bậc mấy trên thang 1–5
  (`blast-radius`); phát hiện mức nào cũng ưu tiên bằng chứng bậc 4 (lệnh chạy được) như luật `repro` của high/critical.
- TSK-3.4 `b-repro`/`b-fix`: gọi `diagnosing-bugs` (thu nhỏ repro ở b-repro; 3–5 giả thuyết, log debug gắn thẻ và dọn ở b-fix); b-fix sửa ở
  nơi hệ thống sai đầu tiên, guard chỉ là bản sửa khi trạng thái nó xử lý là hợp lệ (`fix-root-causes`).
- TSK-3.5 `b-add-skill`: trỏ tới `writing-for-agents`.
- TSK-3.6 vòng sửa (b-feat/b-fix khi quay lại từ review hoặc kiểm chứng đỏ): từ lần sửa thứ hai cho cùng một cổng, viết tiền đề chung của
  các lần sửa trước ra và kiểm nó trước khi sửa tiếp (`attack-the-premise`); bổ trợ cho cơ chế dừng khi review không hội tụ.
- TSK-3.7 skill gốc `principles` (từ pstack, nguyên tắc chọn ở mục 4b): mục lục một dòng mỗi nguyên tắc, tệp lá là tệp phụ; b-req/b-feat/
  b-review trỏ tới nó thay vì chép nguyên tắc vào thân.
- Tiêu chí: golden và bản xuất bản cập nhật có chủ đích; `bint task lint` vẫn qua trên phiếu cũ (hợp đồng v1 tương thích ngược);
  `go test ./internal/skills/... ./internal/workflow/...` xanh.

### GĐ4 — Thuật ngữ miền là nguồn chính thức (≈ 3–4 ngày, có Go; làm khi GĐ1–3 cho thấy glossary được dùng)

- TSK-4.1: ctxengine đánh chỉ mục `GLOSSARY.md`/`GLOSSARY-MAP.md`; `context_for_task`/`bint context` trả thuật ngữ khớp tác vụ (có trần token).
- TSK-4.2: `bint bootstrap`/`inspect` nhận diện glossary (như đã nhận `adr`).
- TSK-4.3: Tri thức `decision` có bằng chứng trỏ vào tệp ADR (fingerprint theo tệp ADR).
- Ràng buộc: thêm khoá cấu hình thì sửa đủ struct, `Default()`, `Validate()`, `Render` (tri thức `01M746NPB0Y4D5S2D47EFTJ8T4`);
  vet/staticcheck cả `GOOS=windows`.

### GĐ4b — Kiểm chứng trên app thật và tri thức có cơ chế cưỡng chế (≈ 4–5 ngày, có Go; từ pstack)

- TSK-4b.1: Skill gốc `create-verification-skill` (viết lại từ pstack): khảo sát repo (bề mặt, cách chạy, cách lái, chứng cứ, cô lập), sinh
  skill tuỳ biến `.bint/skills/verify-<app>/` (launch, doctor, drive, evidence, cleanup) kèm `features/` 3–5 tính năng; tự chạy trọn một lần
  trước khi giao. Kèm `maintain-verification-skill` để sửa bản đồ tính năng khi app đổi.
- TSK-4b.2: b-feat và b-goal dùng `verify-<app>` khi có: tiêu chí về hành vi người dùng thấy cần chứng cứ từ lần lái app thật, không chỉ
  unit test. Bint vẫn chạy `verify.command` như cũ.
- TSK-4b.3: Thử trên chính Bint: `verify-bint` lái CLI và TUI (rủi ro 9).
- TSK-4b.4: Tri thức có trường tuỳ chọn `enforced_by` (lint, test, hook, kiểu… kèm đường dẫn); `bint knowledge audit --all` thêm tín hiệu
  "quy tắc chưa có gì cưỡng chế" để `/retro` và b-knowledge-audit đề xuất chuyển thành check. Theo luật schema tri thức và cấu hình của dự án.

### GĐ5 — Đo lường (≈ 2 ngày Go + chi phí eval)

- TSK-5.1: Thêm khoá biến thể `skills=` cho nhánh `bint@…` (`internal/eval/arms.go`; hiện chỉ có `task`, `path`, `mcp`, `harness`,
  `tokens`, `items`), ví dụ `bint@skills=mp` = Bint + skill/lớp phủ GĐ1–3.
- TSK-5.2: So `bint` với `bint@skills=mp` trên đề khai thác từ phiếu `b-do`/`b-debug` (`bint eval mine --from-tasks`), cùng harness và model.
  Chỉ số: tỉ lệ đạt (KTC 95 %), token, thời gian, số lượt agent, số phát hiện high/critical có repro.
- TSK-5.3 (tuỳ chọn): nhánh thí nghiệm review hai trục bằng ≤ 2 agent con (ngưỡng ở mục 3.2).
- Áp `explain-the-number` (pstack) cho mọi số đo của GĐ5: tìm điều giới hạn con số và loại trừ khả năng nó đo nhầm việc khác trước khi
  kết luận.
- Quyết định: thành phần làm giảm tỉ lệ đạt hoặc tăng chi phí đáng kể mà không đổi lại chất lượng thì gỡ khỏi skill gốc; thành phần trung
  tính thì giữ (theo mục 3.3, số đo không phải cổng vào mà là căn cứ giữ/gỡ).

### GĐ6 — Nhập và cập nhật từ bản gốc (≈ 3–5 ngày, có Go; làm khi cần bám bản mới)

- TSK-6.1: `bint skill import <git-url> --path <thư mục> --ref <sha>`: chép vào `.bint/skills/<tên>/`, ghi nguồn/sha/giấy phép vào lock.
- TSK-6.2: `bint skill upstream diff|update <tên>`: hiện thay đổi của bản gốc; tệp đã sửa tay → xung đột, không ghi đè (như `core.lock`).
- Ràng buộc: lệnh mới cần hàng trong `cliRefs` và `TestServiceMessageCLIInventory` (tri thức `01M78XP5NXWWRAZQHN5PSHNC13`); thông điệp qua
  i18n vi/en; kiểm thử không gọi mạng thật.

### Thứ tự

GĐ0 → GĐ1 (tệp phụ cho skill gốc là việc chặn) → GĐ2 → GĐ3 → GĐ4b → GĐ5 (đo để giữ/gỡ). GĐ4 và GĐ6 chỉ làm khi dùng thật thấy cần.

## 7. Ngoài phạm vi

- Terminal multiplexer (zellij, tmux) và điều khiển phiên tương tác qua màn hình (mục 3.1).
- Agent con trong bước implement (mục 3.2).
- Issue tracker ngoài (GitHub/GitLab/Linear) và nhãn triage; Bint dùng kho phiếu riêng.
- Cài plugin `mattpocock-skills` hoặc `pstack` song song với bản nhập: trùng skill và trùng tên (`/code-review`, `/tdd`, `/teach`).
- Tỏa nhánh đa mô hình trong một bước (`arena`, `swarm`, `interrogate` của pstack) và playbook vận hành gắn Cursor/GitHub.

## 8. Câu hỏi mở

- [x] Q-1: Đưa skill đã nhập vào skill gốc nhúng hay chỉ giữ ở `.bint/skills/` của repo này? — Trả lời: Bint chưa có người dùng ngoài,
  đưa thẳng vào skill gốc (mục 3.3).
- [x] Q-2: `GLOSSARY.md` đặt ở gốc repo hay trong `docs/`? — Trả lời: gốc repo (mục 3.4).

## 9. Nhật ký triển khai (2026-11-23)

| Việc | Kết quả |
|---|---|
| GĐ0 | ADR [docs/adr/0001-mattpocock-skills.md](adr/0001-mattpocock-skills.md); mỗi skill nhập có `LICENSE` + `UPSTREAM.md`; thân tiếng Anh, mục Bint tiếng Việt |
| TSK-1.0 | `skills.Skill.Files` + `Version()`; `Core()` nạp tệp phụ (trừ `tests/`, `.sh` → 0755); `Install` xuất bản/băm từng tệp (`LockEntry.Files`), xung đột khi sửa tay, gỡ tệp thừa (`internal/skills/core_files.go`, test `TestInstallCoreAuxFiles`) |
| TSK-1.1–1.3 | 6 skill tự gọi trong `internal/skills/core/`, mục "Khi chạy dưới Bint", kịch bản áp lực `tests/*.yml` (không xuất bản; `TestCoreSkillScenariosParse`). Ví dụ flow `tdd` đổi thành `testfirst` vì trùng tên skill gốc |
| TSK-1.4 | Đã chạy, xem "Kết quả TSK-1.4" bên dưới |
| GĐ2 | `grill-with-docs`, `improve-arch`, `retro` (gộp `correct`), `handoff` + kịch bản; định tuyến ở điểm neo `routing` của `/b-do`. `/why` (tuỳ chọn) chưa làm |
| GĐ3 | b-req, b-feat, b-review, b-repro, b-fix, b-add-skill sửa thẳng; skill `principles` (13 nguyên tắc, tệp lá). `## Seam kiểm thử` mới là khuyến nghị, lint chưa bắt buộc (không đổi hợp đồng v1) |
| GĐ4b | `create-verification-skill`, `maintain-verification-skill`; b-feat/b-goal dùng `verify-<app>`; `.bint/skills/verify-bint/` (CLI, đã chạy trọn một lượt: launch → doctor → 3 tính năng → cleanup); tri thức `enforced_by` + `--enforced-by` + tín hiệu audit `unenforced` |
| TSK-5.1 | Biến thể `bint@skills=mp` (`internal/eval/arms.go`, `SkillSets`, `writeCoreSkills`) |
| TSK-5.2 | Đã chạy trên tập validation, xem "Kết quả TSK-5.2" bên dưới. TSK-5.3 chưa làm (tuỳ chọn) |

### Kết quả TSK-1.4 (`bint skill test --arm both`, claude/haiku, 3 lượt mỗi nhánh, 2026-11-24)

Chạy trên bản chép tạm `.bint/skills/mp-<tên>/` (tên skill gốc bị cấm cho skill tuỳ biến). Lượt đầu bằng opus cho thấy kịch bản quá dễ
(nhánh without đạt ~100%) nên đã viết lại kịch bản và chạy bằng haiku; chi phí cả đợt ≈ $0.4 (opus ≈ $0.12/lượt, haiku ≈ $0.005/lượt).

| Skill | without | with |
|---|---|---|
| codebase-design | 0/3 | 3/3 |
| diagnosing-bugs | 1/3 | 3/3 |
| domain-modeling | 0/3 | 3/3 |
| grilling | 0/3 | 3/3 |
| grill-with-docs | 0/3 | 3/3 (sau khi thêm "vòng rút gọn khi bị đòi bỏ qua câu hỏi" và sửa regex chấm) |
| handoff | 1/3 | 3/3 |
| improve-arch | 1/3 | 3/3 |
| principles | 1/3 | 3/3 |
| retro | 1/3 | 3/3 (kịch bản mới: time.Now() trong workflow, áp lực "chỉ cần tri thức") |
| tdd | 4/6 | 6/6 |
| writing-for-agents | 2/3 | 3/3 |

Mọi skill nhập đều có nhánh with hơn nhánh without trên kịch bản hiện tại.

### Rủi ro 1: Codex và `agents/openai.yaml` (2026-11-24, codex-cli 0.171.0)

Hai skill hoàng yến trong `.agents/skills/` của repo nháp, giống hệt nhau trừ `policy.allow_implicit_invocation: false` ở một skill:
skill bị chặn **không** được Codex tự gọi (0/2 lượt), skill đối chứng được tự gọi (2/2), gọi tường minh `$weather-hanoi` vẫn chạy.
Kết luận: Codex đọc `agents/openai.yaml` của skill dự án; skill do người gọi của Bint giữ đúng chính sách trên Codex.
Lưu ý khi tự động hoá `codex exec`: đóng stdin (`</dev/null`), không thì nó treo chờ "additional input from stdin".

### Kết quả TSK-5.2 (eval `bint` vs `bint@skills=mp`, 2026-11-24)

Tập validation gồm 6 tác vụ đã duyệt, chấm bằng `go test -count=1 {test_dirs}`. Chạy claude/sonnet ở chế độ strict, 2 lần lặp, đợt
`eb_01M84T9JZCYPZR5CWPSKFH4KN6`. Tri thức tại từng commit gốc dựng bằng `bint eval bootstrap` ($1.33); lượt chạy tốn $3.93.

| Nhánh | Đạt (CI95) | $/lượt | Thời gian (s) | Lượt agent |
|---|---|---|---|---|
| bint | 75% [42, 100] | 0.159 | 175 | 9.7 |
| bint@skills=mp | 67% [33, 100] | 0.169 | 157 | 11.8 |

Δ theo cặp (mp − bint): đạt −8.3 điểm [−25, 0], chi phí +$0.010 [+0.004, +0.017], thời gian −18 s [−35, −3].
Cả chênh lệch đều do một tác vụ (`c85f3c845ee69`: bint đạt 1/2, mp đạt 0/2). `c320f4ece3f4f` trượt ở cả hai nhánh; 4 tác vụ còn lại
cả hai nhánh đều đạt 2/2.

Kết luận: với 6 tác vụ, eval **không đo được lợi ích** của bộ skill kỷ luật trên tác vụ sửa lỗi nhỏ. Bộ skill tốn thêm khoảng 6% chi phí
và khoảng 2 lượt agent mỗi lần chạy, nhưng khoảng tin cậy chứa 0. Bộ skill vẫn giữ trong lõi vì TSK-1.4 cho thấy nó đổi hành vi đúng
hướng trên kịch bản áp lực. Muốn kết luận về hiệu quả end-to-end cần ≥ 20 tác vụ (máy đã báo tập hiện tại quá nhỏ) và nhánh `ideal`
cho cổng 2.
