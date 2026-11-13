# Trạng thái triển khai và giới hạn hiện tại

Tổng hợp trạng thái **triển khai** của mã nguồn tại HEAD `f77ae19` (2026-11-13); số liệu thực nghiệm lịch sử
giữ nguyên theo thời điểm chạy. Yêu cầu thiết kế xem ở [đặc tả thiết kế](bint-plan.md).

**Trạng thái nghiệm thu, tiêu chí còn mở và kế hoạch các vòng nghiệm thu nằm ở
[acceptance.md](acceptance.md)** (nguồn chuẩn duy nhất). Tài liệu này không ghi kết luận nghiệm thu; mã
`NT-…` bên dưới trỏ sang đó.

Các biên bản bổ sung được lưu theo baseline: [tái nghiệm thu `d788669`](reacceptance-2026-11-13/README.md)
và [phụ lục MCP/TUI `04214d8`](reacceptance-2026-11-13/update-04214d8/README.md).
Đây là bằng chứng lịch sử; trạng thái và tiêu chí còn mở hiện hành được đối chiếu ở `acceptance.md`.

## Tổng quan theo giai đoạn

| Giai đoạn | Phần đã có trong mã | Giới hạn hiện tại |
|---|---|---|
| **P0 — Nền tảng** | `init`/`doctor`/`update`, cấu hình 4 lớp, thư mục trạng thái ngoài repository dùng chung giữa các worktree, SQLite (WAL + FTS5) và hàng đợi spool | Hook chỉ ghi tệp spool (không mở SQLite, không gọi mô hình) |
| **P1 — Khung đánh giá** | `mine`/`verify`/`spec`/`approve`, cô lập bằng kho trần, chống rò rỉ `strict`/`economy`, các nhánh `none`/`minimal`/`bint`/`ideal`, biến thể `bint@…`, profile cô lập cho Codex | Mạng chưa bị chặn ở tầng hệ điều hành; cỡ mẫu và chất lượng đề bài phụ thuộc lịch sử commit của từng repository |
| **P2 — Tri thức** | Định dạng mục tri thức có bằng chứng + fingerprint, bảng chuyển trạng thái, kiểm tra xác định được, `b-bootstrap`, `b-knowledge`, xuất bản `AGENTS.md` | Chưa tự động phát hiện mâu thuẫn ngữ nghĩa giữa các mục; người duyệt chịu trách nhiệm |
| **P3 — Context Engine** | Chỉ mục FTS5 dẫn xuất, truy xuất theo tác vụ/đường dẫn, MCP server stdio (6 công cụ), hook chèn ngữ cảnh, tín hiệu kết quả và chẩn đoán cổng 2 | Fingerprint ngoài Go dùng heuristic; chưa có truy xuất ngữ nghĩa hay hook `post-merge` |
| **P4 — Workflow** | `b-req`/`b-feat`/`b-review`/`b-do`, hợp đồng artifact v1 (`task lint`, AC↔TSK, câu hỏi mở `Q-N`), độ sâu `standard`/`deep`, lớp phủ skill; harness/model lưu theo role để resume | Classify dùng heuristic; task cũ chưa có snapshot dùng cấu hình tại thời điểm resume |
| **P5 — Multi-Harness** | Adapter cấp 3 cho Claude Code, Codex CLI và Antigravity CLI; chọn harness và mô hình theo từng role; khoá ghi `flock`/`LockFileEx`; hậu kiểm bước chỉ đọc | Codex và Antigravity không báo chi phí USD / không có cờ giới hạn lượt; Antigravity chưa có MCP cấp dự án, chưa chèn ngữ cảnh qua hook, và mới chạy với binary giả lập |
| **P6/P7 — Tiến hoá L1/L2** | Quét cơ hội, đề xuất bất biến, đối chiếu hồi cứu/tiến cứu bằng Probe tuần tự (dừng sớm O'Brien–Fleming), áp dụng/quay lui, kiểm tra hồi quy khi đổi mô hình, chế độ `deferred` | Còn 5 phát hiện `NT-P6-01..05` (3 mức cao) trong `internal/evolve`; chưa có đề xuất L1/L2 đạt trên tập `test` độc lập |
| **P8 — Cycle, bài học (E), thành phần quy trình (H)** | Vòng `bint evolve cycle` có ngân sách, `bint experience digest`/`show`, cơ hội `failure_lesson`, reflector Claude (`--tools ""`) và adapter Codex cô lập có probe no-tools/reader, kiểm tra rò rỉ bài học và ngân sách token riêng. `.bint/harness` (guidance, guardrail, script, `strategy.yml`), `bint harness component`/`render`, guardrail thực thi qua hook (kể cả `Stop`), biến thể `bint@harness=…`, bảng `eval_guardrail_usage`, đề xuất `target_type: harness` ([hướng dẫn](harness-components.md)) | Phần đo của E và H chưa chạy; evolution máy đang tắt; reflector factory mới smoke bằng model thật |
| **Cô lập và driver session** ([kế hoạch](isolation-subagent-plan.md), G1–G4) | Khoá chia sẻ/độc quyền theo tác vụ, mỗi tác vụ một worktree (`workflow.isolation: auto`), `bint task diff`/`land`/`cancel`, nhật ký ghi `writes.jsonl`; driver session (`bint run next`/`complete`/`abort`, `/b-do --subagent`), guardrail dựng sẵn `bint-readonly` và `bint-worktree`, chi phí subagent qua `SubagentStop` | Hai tác vụ `--worktree` song song bằng subagent (G4) chưa chạy thật; hook `deny` dưới `bypassPermissions` chưa thử; Windows mới qua biên dịch và vet |
| **Gateway MCP** ([kế hoạch](mcp-gateway-plan.md), M0–M5) | Cấp MCP của Bint cho các bước `bint run`; danh mục service nhúng, cấu hình hai tầng, `bint mcp add`/`remove`/`check`/`index`/`grants`; daemon gateway (`bint mcp gateway …`), cửa vào gộp `bint mcp serve`, quyền role × service, sự kiện `mcp.called`; màn MCP trong TUI; đã đo bộ nhớ (6 phiên: 750 → 511 MB PSS) | Chưa chạy với service thật và nhiều phiên harness thật; `mcp.grants` mặc định để rỗng vì chưa đo chất lượng; chưa đo trên Windows và với nhiều worktree |
| **Giao diện terminal** ([kế hoạch](tui-plan.md), giai đoạn 0–6) | Tám tab (Tổng quan, Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá, Hệ thống, MCP), bảng lệnh, trợ giúp theo ngữ cảnh, vi/en, chuột, chế độ hẹp, `--read-only`, `NO_COLOR`, trình hướng dẫn `init`, nâng cấu hình và cập nhật trong TUI | Chưa thử với người dùng thật và terminal Windows; chưa có ảnh VHS; chuỗi của tầng dịch vụ vẫn là tiếng Việt khi giao diện `en` |
| **Custom skill/flow** ([kế hoạch](custom-flow-plan.md), GĐ1–6) | Skill tuỳ biến ở `.bint/skills/{tên}/` (`bint skill new`/`lint`/`test`), flow tuỳ biến ở `.bint/flows/{tên}.yml` (`bint flow list`/`show`/`lint`); Runner chọn bước theo kind (plan/implement/review) của flow, bốn workflow gốc là flow nhúng; `bint run {flow}` cho cả driver process và session, role tuỳ biến; `bint sync` xuất bản flow thành skill điều phối `/{flow}`; skill gốc `b-add-skill`, `b-add-flow` | Chưa nghiệm thu với harness thật (`NT-CF-MANUAL`); chỉ ba kind cố định, không có bước song song hay rẽ nhánh |

## Kết quả đo đã có

Kết luận nghiệm thu tương ứng: [acceptance.md, mục 2](acceptance.md#2-trạng-thái-nghiệm-thu).

- **P1:** đã chạy agent thật và chấm bằng kiểm thử ẩn trên cả [Codex](evals/pilot-codex-2026-11-10/README.md) lẫn [Claude Code](evals/p6-p7-claude-2026-11-10/README.md).
- **Cổng quyết định 1 và 2:** mới có pilot (một repository, hai tác vụ validation trên Codex; một repository trên Claude Code); chưa đủ điều kiện đo theo [gate-1.md](gate-1.md) và [gate-2.md](gate-2.md).
- **P4 / P5:** 30 phiên chạy thật trên Codex ([đợt 1](evals/codex-flows-2026-11-10/README.md), [đợt 2](evals/codex-flows-2026-11-11/README.md)) và cả hai chiều phối hợp [Claude Code ↔ Codex CLI](evals/claude-codex-mixed-2026-11-11/README.md).
- **Codex 2026-11-12:** [ma trận mới](evals/codex-flow-acceptance-2026-11-12/README.md) có hồi quy snapshot/deny và [adapter evolution vòng 6](evals/codex-flow-acceptance-2026-11-12/codex-restricted-adapter-round6.md). Model live `gpt-6.1-sol` qua no-tools và reader trên fixture/profile cô lập; request CLI thật được probe trước dữ liệu thật. Fixture live đã qua workflow, interrupt/resume giữ snapshot, hook deny trước diff, người dùng duyệt và resume tới finalize ([evidence](evals/codex-flow-acceptance-2026-11-12/evidence/workflow-live-final.json)). Model cũ `gpt-6.1-astra` bị từ chối trước inference; người dùng đã chọn `gpt-6-astra`, và cả ba factory đã chạy model thật theo cấu hình trong fixture cô lập ([log](evals/codex-flow-acceptance-2026-11-12/evidence/factory-live-review-round6.txt)). Không suy từ smoke thành tương thích toàn flow.
- **P6 / P7 / P8:** trên 30 tác vụ trong [đợt đo Claude Code](evals/p6-p7-claude-2026-11-10/README.md), tỷ lệ đạt sau khi bổ sung đặc tả đề bài là 40 % (`bint`) so với 37 % (`none`, khoảng tin cậy ±15 điểm %, chênh lệch chi phí +2 %); vòng `evolve cycle` đầu kết luận `not_supported`.
- **Driver session:** một phiếu `--driver session` chạy trọn trong Claude Code 2.1.322 ([mục 12.16](isolation-subagent-plan.md)).
- **Gateway MCP:** bộ nhớ đo với codegraph thật, một worktree ([mục 11](mcp-gateway-plan.md)).

## Phát hiện còn mở trong mã nguồn

`NT-P6-01..05` (khoá `evolve.AcquireLock`, hậu kiểm bằng `git status --porcelain`, `Rollback` giữa chừng, gộp
`Inputs`, merge nội dung): mô tả, điều kiện đóng và vòng sửa ở
[acceptance.md, mục 3.1](acceptance.md#31-sửa-mã-trước-khi-nghiệm-thu-không-token).

Phát hiện review mức low còn lại của từng giai đoạn được ghi trong mục tiến độ của kế hoạch tương ứng
([TUI mục 12](tui-plan.md), [cô lập mục 12](isolation-subagent-plan.md), [E/H](p8-harness-evolution-plan.md)).

## Giới hạn vận hành cần lưu ý

- **Routing protected scope:** hồi quy task có sẵn với `--no-plan` và scope mới đã được sửa; chi tiết và giới hạn trong [báo cáo Codex 2026-11-12](evals/codex-flow-acceptance-2026-11-12/README.md).
- **Chi phí và giới hạn lượt trên Codex / Antigravity:** hai harness này chỉ báo số token, không báo USD và không có cờ chặn số lượt gọi mô hình. Khi chạy workflow hỗn hợp có bước không báo USD, tổng chi phí chỉ tính phần đã biết ở `cost_usd_partial` (`cost_usd` để `null`).
- **Tín hiệu kiểm thử trong phiên chạy:** trên Codex, Bint tra `exit_code` trong transcript và chỉ nhận một lệnh kiểm thử trực tiếp đơn lẻ; trên Claude Code, kết quả dựa vào `PostToolUse`/`PostToolUseFailure` của công cụ Bash nên một chuỗi lệnh ghép kết thúc bằng mã 0 vẫn có thể được tính là kiểm thử qua. Lệnh gọi CLI `bint` luôn bị loại khỏi bộ nhận diện lệnh kiểm thử.
- **Chạy tiếp (`resume`) sau khi bị chặn đường dẫn bảo vệ:** hook Codex phát sự kiện deny gắn child run và đường dẫn; workflow chuyển `blocked`, người dùng duyệt đúng đường dẫn bằng `task approve --path`, rồi resume chạy lại `b-feat`.
- **Driver session:** repo cài hook từ bản cũ thiếu `SubagentStop` thì chi phí subagent là `unknown`; chạy lại `bint init --hooks` (`bint doctor` báo thiếu).
- **Gateway MCP:** `bint mcp add`/`remove`/`index` là thao tác chỉ dành cho người; thêm khoá vào `.bint/config.yml` thì cài lại binary, nếu không binary cũ (kể cả hook) không nạp được cấu hình.
- **Bài học từ thất bại / đề xuất quy trình (kế hoạch E/H):** Claude giữ `--tools ""`. Codex dùng catalog tạm tắt native tools, environments rỗng và probe request thật cho từng lượt; reflector/proposer không có công cụ, evolver chỉ có Read/Grep/Glob do Bint xử lý. Evolution máy vẫn tắt, chưa có vòng sinh/đánh giá proposal thật mới.
- **Cờ `security.*`:** `allow_external_agents` và `allow_network_in_evals` mới là khoá khai báo, chưa gắn với cơ chế chặn ở tầng hệ điều hành.
