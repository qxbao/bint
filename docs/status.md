# Trạng thái triển khai và giới hạn hiện tại

Tổng hợp trạng thái mã nguồn tại commit `183ac17` (`main`), kết quả từ các đợt thực nghiệm
đã lưu ([docs/evals/README.md](evals/README.md)) và các vấn đề còn mở so với
[đặc tả thiết kế](bint-plan.md).

## Tổng quan theo giai đoạn

| Giai đoạn | Phần đã có trong mã | Giới hạn hiện tại |
|---|---|---|
| **P0 — Nền tảng** | `init`/`doctor`/`update`, cấu hình 4 lớp, thư mục trạng thái ngoài repository dùng chung giữa các worktree, SQLite (WAL + FTS5) và hàng đợi spool | Hook chỉ ghi tệp spool (không mở SQLite, không gọi mô hình) |
| **P1 — Khung đánh giá** | `mine`/`verify`/`spec`/`approve`, cô lập bằng kho trần, chống rò rỉ `strict`/`economy`, các nhánh `none`/`minimal`/`bint`/`ideal`, biến thể `bint@…`, profile cô lập cho Codex | Mạng chưa bị chặn ở tầng hệ điều hành; cỡ mẫu và chất lượng đề bài phụ thuộc lịch sử commit của từng repository |
| **P2 — Tri thức** | Định dạng mục tri thức có bằng chứng + fingerprint, bảng chuyển trạng thái, kiểm tra xác định được, `b-bootstrap`, `b-knowledge`, xuất bản `AGENTS.md` | Chưa tự động phát hiện mâu thuẫn ngữ nghĩa giữa các mục; người duyệt chịu trách nhiệm |
| **P3 — Context Engine** | Chỉ mục FTS5 dẫn xuất, truy xuất theo tác vụ/đường dẫn, MCP server stdio (6 công cụ), hook chèn ngữ cảnh, tín hiệu kết quả và chẩn đoán cổng 2 | Fingerprint ngoài Go dùng heuristic; chưa có truy xuất ngữ nghĩa hay hook `post-merge` |
| **P4 — Workflow** | `b-req`/`b-feat`/`b-review`/`b-do`, hợp đồng artifact v1 (`task lint`, AC↔TSK, câu hỏi mở `Q-N`), độ sâu `standard`/`deep`, lớp phủ skill | Classify dùng heuristic; các cờ ghi đè `--harness`/`--role` trên dòng lệnh chưa lưu vào `state.json` khi `resume` |
| **P5 — Multi-Harness** | Adapter cấp 3 cho Claude Code, Codex CLI và Antigravity CLI; chọn harness và mô hình theo từng role; khoá ghi `flock`/`LockFileEx`; hậu kiểm bước chỉ đọc | Codex và Antigravity không báo chi phí USD / không có cờ giới hạn lượt; Antigravity chưa có MCP cấp dự án và chưa chèn ngữ cảnh qua hook |
| **P6/P7 — Tiến hoá L1/L2** | Quét cơ hội, đề xuất bất biến, đối chiếu hồi cứu/tiến cứu bằng Probe tuần tự (dừng sớm O'Brien–Fleming), áp dụng/quay lui, kiểm tra hồi quy khi đổi mô hình, chế độ `deferred` | Còn 5 phát hiện nghiệm thu `P6-01..P6-05` bên dưới; chưa có đề xuất L1/L2 chứng minh cải thiện có ý nghĩa thống kê trên tập `test` độc lập |
| **P8 — Cycle và bài học (E)** | Vòng `bint evolve cycle` có ngân sách, `bint experience digest`/`show`, cơ hội `failure_lesson`, reflector Claude (`--tools ""`), kiểm tra rò rỉ bài học và ngân sách token riêng | Mã và kiểm thử xác định đã xong; chưa chạy thực nghiệm reflector thật. Kế hoạch H (tiến hoá thành phần quy trình) và [TUI](tui-plan.md) vẫn là đề xuất |

## Kết quả thực nghiệm theo giai đoạn

- **P1 (Khung đánh giá):** đã chạy agent thật và chấm bằng kiểm thử ẩn trên cả [Codex](evals/pilot-codex-2026-11-10/README.md) lẫn [Claude Code](evals/p6-p7-claude-2026-11-10/README.md).
- **P2 / P3 (Cổng quyết định 1 và 2):** chưa nghiệm thu chính thức theo giao thức đã chốt tại [docs/gate-1.md](gate-1.md) và [docs/gate-2.md](gate-2.md) (cần đủ cỡ mẫu trên tối thiểu 2 repository và đầy đủ chỉ số chi phí/lượt).
- **P4 / P5 (Workflow và Multi-Harness):** đã kiểm chứng 30 phiên chạy thật trên Codex ([đợt 1](evals/codex-flows-2026-11-10/README.md), [đợt 2](evals/codex-flows-2026-11-11/README.md)) và cả hai chiều phối hợp [Claude Code ↔ Codex CLI](evals/claude-codex-mixed-2026-11-11/README.md). Các lỗi phát hiện trong tái nghiệm thu P0–P5 (`R1–R3`, `F1/F3/F7/F8/F11/F12`) đã được sửa và đóng tại [docs/bint-acceptance-open-findings.md](bint-acceptance-open-findings.md).
- **P6 / P7 / P8 (Tiến hoá):** trên 30 tác vụ trong [đợt đo Claude Code](evals/p6-p7-claude-2026-11-10/README.md), tỷ lệ đạt sau khi bổ sung đặc tả đề bài là 40 % (`bint`) so với 37 % (`none`, khoảng tin cậy ±15 điểm %, chênh lệch chi phí +2 %); chưa có đề xuất tự sinh nào vượt qua tập `test` độc lập. Hai hướng thử nghiệm tiếp theo được thiết kế tại [docs/p8-next-experiments.md](p8-next-experiments.md) và [docs/p8-harness-evolution-plan.md](p8-harness-evolution-plan.md).

## Phát hiện P6 còn mở trong mã nguồn

Năm phát hiện trong [biên bản nghiệm thu P6](bint-acceptance-p6-dff6570.md) vẫn còn tồn tại tại `183ac17`
(bộ tái hiện gốc lưu ở [`docs/evals/acceptance-p6-dff6570/`](evals/acceptance-p6-dff6570/)):

| Mã | Mức độ | Mô tả và hướng xử lý |
|---|---|---|
| `P6-01` | Cao | `evolve.AcquireLock` (`internal/evolve/apply.go`) vẫn dùng mẫu cũ `O_EXCL` + đọc PID + `os.Remove`, có thể xoá nhầm khoá của tiến trình mới khi thu hồi khoá chết. Cần chuyển sang `flock`/`LockFileEx` như `workflow.AcquireWriteLock`. |
| `P6-02` | Cao | Hậu kiểm sau khi chạy evolver (`internal/evolve/generate.go`) chỉ so chuỗi `git status --porcelain` trước/sau, nên không phát hiện được nếu evolver sửa thêm vào tệp vốn đã có thay đổi chưa commit. Cần chụp và so sánh nội dung bằng chỉ mục Git tạm như bước chỉ đọc của workflow. |
| `P6-03` | Cao | `Rollback` (`internal/evolve/apply.go`) trả lỗi ngay khi ghi tệp thất bại giữa chừng mà chưa khôi phục lại các tệp đã ghi trước đó trong cùng lần quay lui. |
| `P6-04` | Trung bình | Khi tạo đề xuất `update` (`internal/evolve/generate.go`), `DraftItem` giữ `Origin` của mục gốc nhưng chưa hợp nhất danh sách `Inputs` mới và chưa tính lại cờ `ThirdParty`. |
| `P6-05` | Trung bình | Đề xuất `merge` hai mục trùng (`internal/evolve/generate.go`) hợp nhất `scope` và `evidence` nhưng chỉ giữ nguyên phần thân của một mục rồi đánh dấu `deprecated` mục kia, có nguy cơ làm mất chi tiết chỉ có ở mục bị gộp. |

## Giới hạn vận hành cần lưu ý

- **Chi phí và giới hạn lượt trên Codex / Antigravity:** hai harness này chỉ báo số token, không báo USD và không có cờ chặn số lượt gọi mô hình. Khi chạy workflow hỗn hợp có bước không báo USD, tổng chi phí chỉ tính phần đã biết ở `cost_usd_partial` (`cost_usd` để `null`).
- **Tín hiệu kiểm thử trong phiên chạy:** trên Codex, Bint tra `exit_code` trong transcript và chỉ nhận một lệnh kiểm thử trực tiếp đơn lẻ; trên Claude Code, kết quả dựa vào `PostToolUse`/`PostToolUseFailure` của công cụ Bash nên một chuỗi lệnh ghép kết thúc bằng mã 0 vẫn có thể được tính là kiểm thử qua. Lệnh gọi CLI `bint` luôn bị loại khỏi bộ nhận diện lệnh kiểm thử.
- **Chạy tiếp (`resume`) sau khi bị chặn đường dẫn bảo vệ:** nếu hook `PreToolUse` của Codex trả `deny` trong một bước `b-feat` chạy lẻ, trạng thái tác vụ vẫn có thể chuyển sang `in_review` dù tệp chưa được sửa (trong `b-do` thì bước Finalize sẽ chặn lại theo tiêu chí/kiểm chứng).
- **Bài học từ thất bại (kế hoạch E):** hiện chỉ hỗ trợ reflector trên Claude Code (`--tools ""`); các giao diện đề xuất trong [thí nghiệm A](p8-next-experiments.md) (`bint eval snapshot import`, lọc theo `knowledge_origin`) chưa triển khai.
