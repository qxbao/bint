# Hướng phát triển tiếp theo và bài học từ các dự án tương tự

Ngày: 2026-11-13. Bối cảnh: [trạng thái hiện tại](status.md), [nghiệm thu](acceptance.md), [đặc tả thiết kế](bint-plan.md),
[kế hoạch thử nghiệm sau P8](p8-next-experiments.md), [kế hoạch custom skill/flow](custom-flow-plan.md).

Tài liệu này là **bản khảo sát và đề xuất**, chưa phải kế hoạch đã chốt. Không mục nào ở đây có phiếu
tác vụ; mỗi mục được chọn thì lập phiếu `b-req` riêng. Tên lệnh và schema trong phần B là phác thảo.

Tài liệu gồm hai phần:

- **Phần A — hướng chiến lược:** việc cần làm để Bint có kết quả đo, có bản phát hành và có vị trí rõ.
- **Phần B — tính năng mới:** thứ có thể xây thêm, học từ các dự án khác.

## 1. Định vị hiện tại

Bint đứng ở chỗ các dự án tương tự chưa chiếm: **đo** xem tri thức, skill hay quy trình có giúp agent
trên chính repository đó không, và chỉ áp dụng thay đổi khi có bằng chứng từ tập đánh giá độc lập
(mục 1 của đặc tả, "Khác biệt so với năng lực sẵn có của harness").

Hai điểm yếu lớn nhất:

- **Chưa có kết quả dương.** Trên 30 tác vụ, nhánh `bint` đạt 40 % so với 37 % của `none`, khoảng tin
  cậy ±15 điểm %; chưa đề xuất tiến hoá nào qua được tập `test` độc lập. Cổng quyết định 1 và 2 chưa
  nghiệm thu.
- **Chưa có bản phát hành.** Chỉ cài được bằng `go install` từ mã nguồn; `release.yml` đang khoá; tài
  liệu chỉ có tiếng Việt.

## 2. So sánh với các dự án khác

### 2.1. Bảng tổng hợp

| Dự án | Trọng tâm | Ghi nhớ | Quy trình | Đo hiệu quả | Người duyệt | Lưu trạng thái |
|---|---|---|---|---|---|---|
| **Bint** | Đo lường + tri thức có bằng chứng | Mục tri thức có `tệp:dòng`, fingerprint | `b-req` → `b-feat` → `b-review`, cổng cưỡng chế bằng hook | Có: đề fail→pass từ Git, kiểm thử ẩn, khoảng tin cậy | Bắt buộc | SQLite ngoài repo + tệp trong Git |
| **claude-mem** | Bộ nhớ tự động giữa các phiên | Observation do mô hình phụ nén | Không | Không | Không | SQLite + Chroma ở `~/.claude-mem` |
| **superpowers** | Phương pháp làm việc bằng skill | Không | Brainstorm → worktree → plan → subagent + TDD → review | Chỉ kiểm thử hành vi skill | Duyệt thiết kế trong hội thoại | Không (tệp kế hoạch trong dự án) |
| **Spec Kit / OpenSpec / BMAD** | Phát triển theo đặc tả | Đặc tả là bộ nhớ | Đặc tả → kế hoạch → tác vụ | Không | Duyệt đặc tả | Tệp Markdown trong repo |
| **compound-engineering** | Vòng plan → work → review → ghi bài học | Tệp bài học | Có, nhiều reviewer chuyên biệt | Không | Không bắt buộc | Tệp trong repo |
| **Beads** | Quản lý việc cho agent | Đồ thị việc | Không | Không | Không | JSONL trong Git + SQLite đệm |
| **Entire CLI** | Ghi phiên agent gắn với commit | Transcript thô | Không | Không | Không | Nhánh Git riêng |
| **Tessl** | Registry và chấm điểm skill | Không | Không | Có: kịch bản có/không có ngữ cảnh | Không | Dịch vụ |
| **Bộ nhớ sẵn của harness** | Ghi chú do mô hình tự viết | Tệp ghi chú | Không | Không | Không | Thư mục người dùng |

### 2.2. Chi tiết từng dự án

**claude-mem** (Apache-2.0, bản 15.18.3, khoảng 117 nghìn sao)

- Hook của Claude Code (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop, SubagentStop,
  SessionEnd…) gửi hoạt động công cụ tới một worker cục bộ. Một mô hình quan sát riêng nén hoạt động
  thành observation và viết tóm tắt cuối phiên.
- Phiên mới nhận một chỉ mục rút gọn của các observation cũ; chi tiết chỉ nạp khi cần. Ba công cụ theo
  tầng: `search` (chỉ mục kèm ID), `timeline` (diễn biến theo thời gian), `get_observations` (bản đầy đủ).
- Có web UI, đồng bộ đám mây tuỳ chọn, hơn 15 harness. Nội dung trong thẻ `<private>` không được lưu.
- Không có bước duyệt trước khi lưu, không đối chiếu observation với mã, không có benchmark. README
  tự ghi tỷ lệ token chỉ mục/bản đầy đủ "không phải số đo recall hay tiết kiệm đầu-cuối", và mô hình
  quan sát có thể làm tăng tới 100 % lượng dùng gói.

**superpowers** (MIT, khoảng 317 nghìn sao, 16 harness)

- Chuỗi skill: `brainstorming` → `using-git-worktrees` → `writing-plans` (tác vụ 2–5 phút, có đường
  dẫn tệp và bước kiểm chứng) → `subagent-driven-development` (subagent mới cho mỗi tác vụ, review hai
  tầng: đúng đặc tả rồi mới tới chất lượng mã) → `test-driven-development` → `requesting-code-review` →
  `finishing-a-development-branch`.
- Skill khác: `systematic-debugging`, `verification-before-completion`, `dispatching-parallel-agents`,
  `writing-skills`.
- Không có CSDL, bộ nhớ hay ghi nhận lượt chạy. Cổng là lời dặn trong skill, không cưỡng chế.
- Kiểm thử hành vi skill bằng một harness riêng (`superpowers-evals`). Không nhận skill mới từ cộng
  đồng; thay đổi skill phải chạy được trên mọi harness hỗ trợ.

**Spec Kit / OpenSpec / BMAD-METHOD**

- Spec Kit: "constitution" của dự án rồi đặc tả theo từng tính năng; hợp dự án mới.
- OpenSpec: mỗi thay đổi một thư mục gồm đề xuất, thiết kế, danh sách tác vụ và **delta đặc tả**; xong
  thì gộp delta vào đặc tả chung. Hợp mã có sẵn.
- BMAD: mô phỏng cả nhóm agile (analyst, PM, architect, dev, QA); nặng nhất trong ba.

**compound-engineering** (Every, MIT)

- Vòng bốn bước; bước cuối ghi lại bài học để lần sau dùng. Nhiều agent reviewer chuyên biệt chạy
  song song. Có CLI chuyển plugin Claude Code sang OpenCode và Codex.

**Beads** (Steve Yegge, MIT, alpha)

- `.beads/issues.jsonl` commit vào Git là nguồn chuẩn; SQLite cục bộ là bộ đệm. ID là băm nội dung
  nên nhiều agent tạo việc cùng lúc không đụng nhau; có merge driver cho JSONL.
- Lệnh chính cho agent: `bd ready` (việc đã hết bị chặn), `bd dep tree` (cây phụ thuộc).

**Entire CLI** (MIT)

- Mỗi checkpoint ghép một thay đổi mã với phiên agent tạo ra nó. Transcript, prompt, lệnh gọi công cụ
  và token lưu ở nhánh `entire/checkpoints/v1`. Quét bí mật bằng mẫu gitleaks cộng entropy trước khi
  lưu. Có lệnh gắn bổ sung phiên mà hook bỏ sót.

**Tessl**

- Mỗi kịch bản chạy hai lần, có và không có tệp ngữ cảnh; chấm theo từng tiêu chí. Kịch bản có thể
  sinh từ commit của repository. Skill chưa có eval thì điểm "Impact" để trống.

**Bộ nhớ sẵn của harness**

- Claude Code: auto memory ở `~/.claude/projects/<dự án>/memory/`, có tệp chỉ mục `MEMORY.md`; có
  bước gộp ghi chú ngoại tuyến.
- Codex: memories ở `~/.codex/memories/`, mặc định tắt.

### 2.3. Nghiên cứu liên quan

- **ACE** (Agentic Context Engineering, ICLR 2026): giữ ngữ cảnh như một playbook, cập nhật **từng
  mẩu** qua ba vai Generator, Reflector, Curator thay vì viết lại toàn bộ, để tránh mất chi tiết sau
  nhiều vòng. Báo cáo +10,6 % trên bài toán agent.
- **Meta-Harness** (arXiv 2603.28052): cho một coding agent đọc mã harness cũ, điểm số và **trace thô**
  rồi tự sửa harness; vượt baseline viết tay trên TerminalBench-2. Khác GEPA ở chỗ dùng trace thô thay
  vì bản tóm tắt.
- **Evaluating AGENTS.md** (ETH Zurich, arXiv 2602.11988, đã dẫn ở mục 1 của đặc tả): tệp ngữ cảnh
  không tăng tỷ lệ đạt, chi phí tăng hơn 20 %, tổng quan kiến trúc không giúp tìm tệp nhanh hơn; agent
  tuân thủ tốt quy ước bất thường.

---

## Phần A — Hướng chiến lược

### A1. Chốt giả thuyết cốt lõi trước khi xây thêm

**Lý do.** Mọi lớp tính năng của Bint dựa trên giả thuyết "tri thức có cấu trúc giúp agent". Kết quả
dương hay âm đều là thứ không dự án nào ở mục 2 có, và nó quyết định các hướng còn lại.

**Việc cần làm** (thiết kế đã có ở [p8-next-experiments.md](p8-next-experiments.md)):

- Kế hoạch A: tri thức do người viết tại commit gốc, trần $20. Còn thiếu mã:
  `bint eval snapshot import --base <commit> --from <thư mục>` và nhãn `knowledge_origin: human`.
- Bước B0: đo `none` với model rẻ trên validation, khoảng $5, để biết hướng "model mạnh đề xuất, model
  rẻ thực thi" có khả thi không.
- H5 và E6: đo thành phần quy trình và bài học từ thất bại, đã có mã, chưa chạy.

**Cổng.** Giữ nguyên ngưỡng đã ghi trong tài liệu thử nghiệm; không đổi sau khi thấy kết quả.

**Cách chạy.** Các phép đo này đã được gom thành vòng V3–V5 ở [acceptance.md](acceptance.md) để dùng chung
nhánh và vòng cycle.

### A2. Thêm thước đo tỷ lệ tuân thủ quy tắc

**Lý do.** Với 30 tác vụ chỉ thấy được hiệu ứng từ 15 điểm % trở lên; mức +3–5 điểm cần hàng trăm tác
vụ, khó có trên một repository. Câu hỏi "có mục tri thức X thì agent có làm đúng X không" có hiệu ứng
lớn hơn nhiều nên cần ít mẫu. Hướng này khớp với kết luận của bài ETH (agent tuân thủ quy ước bất
thường) và với cách Tessl chấm theo từng tiêu chí.

**Việc cần làm.**

- Mỗi mục `rule`/`warning` đã duyệt có thể kèm một kịch bản kiểm tra: đề bài ngắn chạm vào vùng mã của
  mục, và tiêu chí chấm xác định (lệnh, mẫu grep trên diff).
- Chạy hai nhánh có/không có mục đó; báo tỷ lệ tuân thủ của từng nhánh.
- Dùng chung hạ tầng với `bint skill test` (GĐ6 của kế hoạch custom skill/flow).

**Giới hạn.** Tuân thủ là chỉ số thay thế, không phải tỷ lệ hoàn thành tác vụ. Báo cáo phải tách hai
chỉ số, và mục 19 của đặc tả vẫn lấy chênh lệch so với nhánh `minimal` làm chỉ số chính.

### A3. Phát hành v0.1

**Lý do.** claude-mem và superpowers lan rộng nhờ cài bằng một lệnh qua plugin marketplace.

**Việc cần làm.**

- Sửa ba phát hiện mức cao còn mở `NT-P6-01..03` (vòng V0 của [acceptance.md](acceptance.md)).
- Bật `release.yml` (hiện là bản nháp, bị khoá), phát hành nhị phân cho 6 nền tảng đã build chéo.
- Đóng gói hook + skill gốc + MCP server thành plugin Claude Code.
- README và hướng dẫn sử dụng bằng tiếng Anh.
- Chạy kiểm thử trên Windows thật (khoá `LockFileEx` mới qua biên dịch và vet).

### A4. Mở `bint eval` cho đối tượng bên ngoài

**Lý do.** Khung đánh giá là phần khó thay thế nhất của Bint. Nếu đo được plugin, skill hay `AGENTS.md`
bất kỳ ("superpowers so với none trên repository của tôi"), Bint thành công cụ đo cho cả hệ sinh thái
thay vì đối thủ của nó.

**Việc cần làm.**

- Một loại nhánh mới nhận cấu hình ngoài: tập tệp chép vào bản sao cô lập, thư mục plugin/skill, cờ
  dòng lệnh của harness.
- Kiểm tra rò rỉ áp cho nội dung ngoài như với tri thức của Bint.
- Báo cáo ghi rõ nguồn và phiên bản của thứ được đo.

### A5. Lấy tri thức từ ma sát thật, không từ bootstrap

**Lý do.** Đợt đo P6–P7 cho thấy tri thức agent tự viết từ mã hầu như không thêm thông tin mới, vì
agent giải đề tự đọc mã cũng biết.

**Hiện trạng.** Đã có: cơ hội `failure_lesson`, công cụ MCP `report_observation`, và rút mục tri thức từ
phát hiện review (`internal/app/knowledge.go`).

**Còn thiếu.**

- Phát hiện chỗ người sửa lại hoặc revert việc của agent sau khi lượt chạy kết thúc.
- Guardrail bị kích hoạt nhiều lần ở cùng vùng mã thành ứng viên tri thức.
- Duyệt hàng loạt trong TUI, gợi ý mục trùng hoặc mâu thuẫn (giới hạn P2 trong
  [status.md](status.md): chưa tự phát hiện mâu thuẫn ngữ nghĩa).

### A6. Custom skill/flow: GĐ1 và GĐ6 trước, hoãn GĐ2–5

**Lý do.** Skill tuỳ biến và `bint skill test` gắn với đo lường. Flow tuỳ biến là phần trùng nhiều nhất
với superpowers và Spec Kit, và đặc tả tự giới hạn workflow ở mức tối thiểu cần cho đo lường.

**Hiện trạng.** GĐ1–6 đã commit (`21171cb` … `25afb78`, sửa review đến `5ce48e2`); còn nghiệm thu thủ công `NT-CF-MANUAL`.

**Ngoại lệ.** Nếu làm mục B2 (flow `b-debug`) thì cần GĐ2–4 trước.

### A7. Đo guardrail (H5) và cô lập mạng thật cho eval

**Lý do.** Guardrail qua hook là cưỡng chế chứ không phải lời dặn, đúng loại can thiệp mà nghiên cứu
ủng hộ; superpowers và các bộ skill khác không làm được điều này. Hai cờ
`security.allow_external_agents` và `security.allow_network_in_evals` hiện mới là khoá khai báo, chưa
chặn gì ở tầng hệ điều hành.

### Việc không nên làm lúc này

- **Chạy đua bộ nhớ tự động kiểu claude-mem.** Harness đang tự bổ sung; mô hình quan sát ở mỗi lượt
  dùng công cụ trái quy tắc "hook không gọi mô hình".
- **Chạy đua bộ skill phương pháp kiểu superpowers.** Trùng năng lực harness, không đo được ưu thế.
- **Truy xuất ngữ nghĩa.** Chưa có số đo nào cho thấy FTS5 là điểm nghẽn (mục 8 của đặc tả đã hoãn).

---

## Phần B — Tính năng mới học từ các dự án khác

Mỗi mục ghi: nguồn, hiện trạng trong mã (đã kiểm bằng grep ngày 2026-11-13 trừ khi ghi khác), đề xuất,
và rủi ro.

### B1. Kế hoạch nhiều giai đoạn và hàng chờ phát hiện tồn

**Nguồn.** Beads: đồ thị phụ thuộc, `bd ready`, liên kết "phát sinh từ".

**Hiện trạng.**

- Phiếu tác vụ không có quan hệ phụ thuộc (không có `depends_on`/`blocked_by` trong `internal/`).
- Các kế hoạch lớn (G1–G4, M0–M5, GĐ1–6) được chia phiếu và xếp thứ tự bằng tay.
- `task.Finding` (`internal/task/review.go`) có `kind`, `severity`, bằng chứng nhưng **không có trường
  trạng thái**; phát hiện mức low còn mở sau mỗi giai đoạn chỉ được theo dõi ngoài Bint.

**Đề xuất.**

- Phiếu cha (kế hoạch) và phiếu con (giai đoạn) có `depends_on`; `bint task ready` liệt kê phiếu đã hết
  bị chặn; phiếu con kế thừa phạm vi và bối cảnh của phiếu cha.
- Thêm kết cục cho phát hiện: `open` | `fixed` | `rejected` | `deferred`, kèm commit hoặc lý do.
- `bint findings list --open` gom phát hiện còn mở qua mọi phiếu; `bint task new --from-findings <id…>`
  lập phiếu sửa với phạm vi suy từ đường dẫn của phát hiện.
- Màn Tác vụ của TUI hiển thị cây kế hoạch và số phát hiện tồn.

*Đã làm (phần 1, chỉ CLI):* sổ chỉ-thêm `findings.json` cạnh `review.json`, `bint findings list|set`,
`bint task new --from-findings`; mô tả ở `docs/workflows.md`. *Còn lại:* `depends_on`, `bint task ready`,
kế thừa phạm vi, màn TUI.

**Rủi ro.** Phiếu nằm ở thư mục trạng thái ngoài repository nên không chia sẻ giữa các máy. Beads giải
bằng JSONL trong Git; với Bint đây là câu hỏi mở (mục 6).

### B2. Phiếu đã xong thành đề đánh giá, và flow `b-debug`

**Nguồn.** `systematic-debugging` và TDD của superpowers; cách Tessl sinh kịch bản từ commit.

**Hiện trạng.** `eval.Mine` (`internal/eval/mine.go`) chỉ khai thác đề từ `git log`. Phiếu `b-do` hoàn
tất đã có yêu cầu, tiêu chí chấp nhận, lệnh kiểm chứng đã được người xác nhận, commit gốc và diff.

**Đề xuất.**

- `bint eval mine --from-tasks`: dựng ứng viên đề từ phiếu `done`, vẫn qua `verify` (fail→pass) và
  `approve` như đề khai thác từ Git.
- Flow `b-debug`: tái hiện bằng một test hỏng → giả thuyết → sửa → test qua. Mỗi lỗi sửa xong cho ra
  một đề fail→pass có sẵn kiểm thử.
- Tập đánh giá lớn dần theo việc thật, giải quyết dần giới hạn cỡ mẫu.

**Rủi ro.**

- Rò rỉ: yêu cầu của phiếu có thể chứa gợi ý lời giải, cần qua bước `spec` như đề thường. Tri thức và
  bài học rút từ một phiếu không được dùng khi chấm chính đề đó (mục 12.3 của đặc tả).
- Phân tập: đề từ phiếu phải được gán `train`/`validation`/`test` theo cùng quy tắc với đề từ Git.

### B3. Nhập bộ nhớ của harness thành mục `candidate`

**Nguồn.** Claude auto memory, Codex memories, `.cursor/rules`, CSDL của claude-mem.

**Hiện trạng.** `bint knowledge` có `add`, `list`, `show`, `verify`, `approve`, `rm`, `review`, `cite`;
không có lệnh nhập từ nguồn ngoài.

**Đề xuất.**

- `bint knowledge import --from claude-memory|codex-memory|cursor-rules|<tệp>`: đọc nguồn, tách từng ý,
  tìm bằng chứng `tệp:dòng`, tạo mục `candidate` có `origin` ghi nguồn.
- Bỏ qua ý không tìm được bằng chứng, ý trùng mục đã có, và ghi chú trạng thái công việc (tiến độ,
  commit) vốn không phải tri thức về mã.
- Người duyệt như mọi mục `candidate` khác.

**Giá trị.** Các công cụ kia ghi nhận rất rẻ nhưng không kiểm chứng; Bint kiểm chứng và xuất bản cho
mọi harness. Khi đó chúng thành nguồn cấp thay vì đối thủ.

**Rủi ro.** Bộ nhớ của harness nằm ngoài repository và có thể chứa thông tin riêng tư; lệnh chỉ chạy
khi người gọi, không tự động, và là thao tác chỉ dành cho người.

### B4. Lịch sử theo tệp cho agent

**Nguồn.** Ba tầng `search` → `timeline` → `get_observations` của claude-mem.

**Hiện trạng.** MCP server có sáu công cụ (`context_for_task`, `context_for_paths`,
`knowledge_search`, `knowledge_get`, `cite_knowledge`, `report_observation`), đều về tri thức. Lượt
chạy đã lưu kèm tệp đọc/sửa, kết quả kiểm thử và nhãn kết quả, nhưng agent không truy vấn được.

**Đề xuất.**

- Công cụ MCP `history_for_paths`: trả chỉ mục ngắn các lượt chạy đã chạm vào đường dẫn (ngày, phiếu,
  kết quả, kiểm thử hỏng, phát hiện review liên quan).
- Công cụ `run_get`: chi tiết một lượt theo ID.
- Dựng xác định từ SQLite, không gọi mô hình. Chạy trong MCP server, không trong hook, nên không vi
  phạm quy tắc "hook không mở SQLite".

**Giá trị.** Có ích ngay từ ngày đầu mà không cần ai duyệt gì (xem mục 3, bài học thứ nhất).

**Rủi ro.** Tăng token ngữ cảnh; cần giới hạn số dòng và đo bằng biến thể `bint@…` trước khi bật mặc
định.

### B5. Bàn giao giữa phiên và giữa harness

**Nguồn.** Tóm tắt phiên của claude-mem (mặc định chỉ trong cùng một harness).

**Hiện trạng.** Không có khái niệm bàn giao trong `internal/`. Phiếu lưu harness và mô hình theo role
để `resume`, nhưng phiên mới bắt đầu lại từ artifact của phiếu.

**Đề xuất.**

- Một bản bàn giao xác định gắn với phiếu: đã làm gì, tệp đã sửa, lệnh kiểm thử cuối và kết quả, tiêu
  chí chấp nhận nào đã đạt, câu hỏi còn mở, lỗi đã gặp.
- Sinh khi một bước kết thúc hoặc bị ngắt; chèn vào đề bài của bước kế tiếp, kể cả khi bước đó chạy
  trên harness khác.
- Dùng lại hạ tầng `bint experience digest`.

**Giá trị.** Bint điều phối Claude lập kế hoạch rồi Codex viết mã, nên bàn giao xuyên harness là chỗ
Bint làm được mà claude-mem không làm.

### B6. Đặc tả sống theo delta

**Nguồn.** OpenSpec.

**Hiện trạng.** `requirement.md`, `plan.md`, `tasks.md` nằm ở `<state>/runtime/tasks/<task-id>/`, ngoài
repository, và dừng ở phiếu.

**Đề xuất.**

- Khi phiếu `done`, các yêu cầu chức năng được gộp vào một thư mục đặc tả trong repository, nhóm theo
  vùng mã.
- Mỗi câu đặc tả có bằng chứng `tệp:dòng` và fingerprint như mục tri thức, nên tự chuyển `stale` khi
  mã đổi.
- `b-req` của phiếu sau đọc đặc tả hiện hành của vùng liên quan.

**Rủi ro.** Trùng một phần với tri thức loại `rule`; cần ranh giới rõ (đặc tả mô tả hành vi mong muốn,
tri thức mô tả điều bất thường khi làm việc với mã). Bài ETH cho thấy tổng quan dài không giúp agent,
nên đặc tả chỉ được truy xuất theo đường dẫn, không chèn toàn bộ.

### B7. Review nhiều lăng kính kèm bảng điểm reviewer

**Nguồn.** Các reviewer chuyên biệt của compound-engineering; review hai tầng của superpowers (đúng
đặc tả trước, chất lượng mã sau).

**Hiện trạng.** `b-review` một reviewer, một lượt; phát hiện không có kết cục (xem B1).

**Đề xuất.**

- Lăng kính review khai báo theo vùng mã (ví dụ bảo mật cho `internal/harness/**`, tương thích Windows
  cho tệp có build tag), mỗi lăng kính là một lượt reviewer riêng.
- Sau khi B1 thêm kết cục cho phát hiện: bảng tỷ lệ phát hiện được sửa so với bị bác, theo lăng kính,
  harness và mô hình.
- Dùng bảng này để chọn reviewer theo role và để loại lăng kính nhiều nhiễu.

**Phụ thuộc.** Cần kết cục phát hiện của B1. Lăng kính là bước review thêm, nên hợp với flow tuỳ biến
(GĐ2–4).

### B8. `bint why <tệp>:<dòng>`

**Nguồn.** Checkpoint của Entire CLI.

**Hiện trạng.** Lượt chạy lưu commit gốc và được gắn commit sau khi quét Git; trích dẫn tri thức lưu ở
bảng `knowledge_citations`.

**Đề xuất.** Lệnh trả lời cho một dòng mã: commit nào, lượt chạy và phiếu nào tạo ra, harness và mô
hình, review nói gì, mục tri thức nào được trích dẫn trong lượt đó. Có màn tương ứng trong TUI.

**Giá trị.** Nối dài hướng "nguồn gốc và khả năng kiểm toán" ở mục 1 của đặc tả.

**Rủi ro.** Dữ liệu lượt chạy chỉ có trên máy đã chạy; trên máy khác lệnh chỉ trả được phần có trong
Git.

### Ý nhỏ hơn

- **Bộ đếm có ích/có hại cho từng mục** (ACE): đếm số lần mục được trích dẫn ở lượt đạt so với lượt
  trượt; dùng để xếp hạng truy xuất và làm tín hiệu cho cơ hội `unused`.
- **Tác vụ 2–5 phút, subagent mới cho mỗi tác vụ** (superpowers): driver session hiện giao một
  subagent cho mỗi bước; có thể giao theo từng `TSK-N` của `tasks.md`.
- **Quét bí mật trước khi lưu transcript** (Entire) và **thẻ loại nội dung khỏi ghi nhận** (claude-mem):
  `hookcore` đã có che dữ liệu; cần đối chiếu phạm vi che với hai cách làm này.
- **Gắn bổ sung phiên mà hook bỏ sót** (Entire): nạp một transcript có sẵn thành lượt chạy.
- **Điểm tác động trên từng skill và mục tri thức** (Tessl): hiển thị kết quả đo gần nhất, hoặc "chưa
  đo", ngay trong danh sách của TUI.

## 3. Bài học về cách làm

1. **Giá trị xuất hiện ngay, không cần người làm gì trước.** claude-mem có ích từ phiên thứ hai. Bint
   đòi duyệt tri thức rồi mới có tác dụng. Mục B4 và B8 cho giá trị ngay từ dữ liệu đã ghi nhận.
2. **Cập nhật từng mẩu, không viết lại.** ACE cho thấy viết lại toàn bộ ngữ cảnh qua nhiều vòng làm
   mất chi tiết. Phát hiện `P6-05` (gộp hai mục chỉ giữ thân của một mục) là đúng loại lỗi này.
3. **Mô tả skill chỉ nêu điều kiện kích hoạt.** Quy tắc của superpowers, đã có trong kế hoạch custom
   skill; nên áp cho cả skill gốc `b-*`.
4. **Đổi skill phải chạy được trên mọi harness.** superpowers lấy đây làm điều kiện nhận thay đổi.
   Bint có sẵn eval đa harness để kiểm điều này cho skill gốc.
5. **Người đề xuất đọc trace thô thì mạnh hơn đọc bản tóm tắt** (Meta-Harness). Bint chọn reflector
   không công cụ để chống rò rỉ. Có thể thử cho đọc trace thô của riêng tập validation, giữ nguyên
   kiểm rò rỉ ở đầu ra.

## 4. Thứ tự đề xuất

Công sức là ước lượng thô: S dưới một ngày, M vài ngày, L hơn một tuần.

| Thứ tự | Mục | Công sức | Tốn token đo | Phụ thuộc |
|---|---|---|---|---|
| 1 | A1 — chốt giả thuyết (Kế hoạch A, B0) | S (mã) | Có, trần $25 | Người viết tri thức |
| 2 | A3 — sửa `P6-01..03`, phát hành v0.1 | M | Không | — |
| 3 | B1 — kế hoạch nhiều giai đoạn, kết cục phát hiện | M | Không | — |
| 4 | B2 — phiếu thành đề đánh giá | S–M | Chỉ khi verify | — |
| 5 | B4 — lịch sử theo tệp qua MCP | S–M | Chỉ khi đo | — |
| 6 | B3 — nhập bộ nhớ harness | M | Không | — |
| 7 | A2 — thước đo tuân thủ | M | Có | GĐ6 custom skill |
| 8 | A6 — custom skill GĐ1, GĐ6 | M | Có (GĐ6) | — |
| 9 | A4 — eval cho đối tượng ngoài | M | Có | A3 |
| 10 | B5 — bàn giao | M | Chỉ khi đo | — |
| 11 | B7 — lăng kính review, bảng điểm | M–L | Có | B1, custom flow GĐ2–4 |
| 12 | A5, A7, B6, B8 | M mỗi mục | Tuỳ mục | Kết quả A1 |

Ba mục B đầu bảng (B1, B2, B4) được xếp trước vì không phụ thuộc kết quả đo, không tốn token, và khớp
với cách Bint đang được dùng để phát triển chính nó.

## 5. Lưu ý khi triển khai

Các quy tắc hiện hành của dự án áp cho mọi mục ở trên:

- Hook không mở SQLite, không gọi mô hình, không trả lỗi; B4 và B5 phải nằm ở MCP server hoặc bước
  workflow.
- Mục do agent tạo luôn ở `candidate`; duyệt, áp dụng, nhập (B3) là thao tác chỉ dành cho người.
- Lệnh CLI mới cần hàng trong `cliRefs` và kiểm `TestServiceMessageCLIInventory`.
- Khoá cấu hình mới cần đủ struct, `Default()`, `Validate()` và mẫu `Render`.
- Mọi thay đổi tốn token đo cần người duyệt ngân sách trước.

## 6. Câu hỏi mở

1. Phiếu tác vụ và phát hiện review có nên xuất được vào Git (như Beads) để dùng chung giữa các máy
   không, hay giữ ở thư mục trạng thái cục bộ?
2. Đề đánh giá sinh từ phiếu (B2) vào tập nào theo mặc định, và ai duyệt?
3. Đặc tả sống (B6) có đáng một loại đối tượng riêng, hay là một `type` mới của mục tri thức?
4. Plugin Claude Code (A3) là kênh phân phối chính hay phụ so với tệp nhị phân?

## 7. Độ tin cậy của nguồn

- Thông tin về claude-mem và superpowers lấy từ README trên GitHub ngày 2026-11-13.
- Thông tin về Spec Kit, OpenSpec, BMAD, compound-engineering, Beads, Entire, Tessl và bộ nhớ sẵn của
  harness lấy từ kết quả tìm kiếm và bài tổng hợp của bên thứ ba, chưa đối chiếu README hay tài liệu
  chính thức. Số agent, số lệnh và phiên bản của các dự án này có thể đã cũ.
- Số liệu của RELAI-VCL so với GEPA và Meta-Harness do chính nhóm tác giả tự báo cáo, không dùng làm
  căn cứ.
- Hiện trạng của Bint ở phần B được kiểm bằng grep trên cây làm việc, chưa đọc kỹ từng gói; trước khi
  lập phiếu cần `b-req` xác nhận lại.

## 8. Nguồn

- claude-mem: https://github.com/thedotmack/claude-mem
- superpowers: https://github.com/obra/superpowers
- So sánh Spec Kit, BMAD, OpenSpec: https://dev.to/willtorber/spec-kit-vs-bmad-vs-openspec-choosing-an-sdd-framework-in-2026-d3j
- Công cụ phát triển theo đặc tả 2026: https://codemyspec.com/blog/best-spec-driven-development-tools
- Compound Engineering: https://every.to/guides/compound-engineering
- Beads: https://github.com/steveyegge/beads
- Entire CLI: https://docs.entire.io/cli/overview
- Tessl eval setup: https://tessl.io/registry/experiments/eval-setup/skills
- Bộ nhớ agent 2026: https://dev.to/max_quimby/ai-agent-memory-in-2026-auto-dream-context-files-and-what-actually-works-39m8
- Bộ nhớ sẵn của Codex: https://codex.danielvaughan.com/2026/04/18/codex-built-in-memory-system-deep-dive/
- ACE: https://arxiv.org/abs/2510.04618
- Meta-Harness: https://arxiv.org/abs/2603.28052
- Evaluating AGENTS.md: https://arxiv.org/abs/2602.11988
