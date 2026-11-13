# Nghiệm thu Bint — trạng thái, kế hoạch và hướng dẫn

Cập nhật: 2026-11-13, sau vòng V0 (commit sửa mã `f1467a0`) và V1 một phần.

**Đây là nguồn chuẩn duy nhất về nghiệm thu.** Tài liệu này giữ:

- trạng thái nghiệm thu của mọi giai đoạn và kế hoạch;
- danh mục tiêu chí còn mở, mỗi tiêu chí một mã `NT-…`;
- kế hoạch các **vòng nghiệm thu**, đã gom tiêu chí để mỗi vòng đóng được nhiều mục nhất;
- hướng dẫn chạy từng vòng và bằng chứng cần lưu.

Các tài liệu khác chỉ trỏ về đây bằng mã `NT-…` hoặc tên vòng, không ghi lại trạng thái nghiệm thu.

| Loại tài liệu | Vai trò với nghiệm thu |
|---|---|
| Tài liệu này | Trạng thái, kế hoạch, hướng dẫn. Cập nhật sau mỗi vòng. |
| [gate-1.md](gate-1.md), [gate-2.md](gate-2.md) | Phụ lục giao thức **đã chốt** của hai cổng quyết định (ngưỡng, điều kiện, commit chốt). Không sửa ngưỡng; trạng thái và kế hoạch chạy ghi ở đây. |
| [status.md](status.md) | Trạng thái **triển khai** và giới hạn vận hành, không ghi trạng thái nghiệm thu. |
| Các kế hoạch (`*-plan.md`, [p8-next-experiments.md](p8-next-experiments.md)) | Thiết kế và nhật ký triển khai. Tiêu chí nghiệm thu gốc nằm ở đó; việc còn mở được chép về đây. |
| Biên bản lịch sử (`bint-acceptance-*.md`, `bint-reacceptance-*.md`, [evals/](evals/README.md)) | Bản ghi bất biến của một lần kiểm tra tại một commit. Không cập nhật. |

## 1. Quy tắc chung

1. **Ngưỡng chốt trước khi đo.** Không đổi ngưỡng, thước đo hay điều kiện sau khi thấy kết quả (mục 19 của
   [đặc tả](bint-plan.md)). Thay đổi là một lần chốt mới, có commit, chỉ áp cho lần đo sau đó.
2. **Mọi lần chạy tốn token cần chủ dự án duyệt ngân sách trước.** Gần chạm trần thì dừng và hỏi.
3. **Tập `test` không bao giờ là đầu vào** của agent sinh đề xuất hay bài học. Chẩn đoán chỉ trên `validation`.
4. **Bằng chứng** của mỗi vòng lưu ở `docs/evals/<tên-vòng>-<ngày>/` (README + `evidence/`, đã ẩn thông tin
   nhạy cảm). Báo cáo chỉ nêu số liệu, không nêu tên hay thông tin repository đo.
5. **Chỉ chủ dự án ký nghiệm thu.** Agent chạy vòng, ghi bằng chứng và đề xuất kết luận; cột "Ký" ở mục 2 do
   người điền.
6. Tác vụ `validation`/`test` hiện do agent duyệt (chủ dự án cho phép); mọi báo cáo phải ghi rõ điều này.
7. Sau mỗi vòng: cập nhật mục 2 và mục 3 của tài liệu này trong cùng commit với biên bản.

## 2. Trạng thái nghiệm thu

Ký hiệu: **Đủ điều kiện** = mọi phát hiện đã đóng, có bằng chứng, chờ ký · **Một phần** = còn tiêu chí mở ·
**Chưa đo** = mã xong, chưa chạy phép đo · **Chưa đạt** = đã đo, không qua.

### 2.1. Giai đoạn sản phẩm (mục 16 của đặc tả)

| Giai đoạn | Điều kiện nghiệm thu | Trạng thái | Bằng chứng | Còn mở | Ký |
|---|---|---|---|---|---|
| P0 Nền tảng | Khởi tạo nhiều lần không mất dữ liệu; nhiều worktree ghi về một CSDL không trùng, không mất | Đủ điều kiện (Linux) | [P0–P4](bint-acceptance-p0-p4.md), [tái nghiệm thu](bint-reacceptance-p0-p5-137dbf9.md), [đóng R1–R3](bint-acceptance-open-findings.md) | `NT-WIN-*` | |
| P1 Khung đánh giá | Khai thác tập tác vụ đủ lớn, chạy nhánh `none`, báo dao động giữa các lần lặp | Đủ điều kiện | [pilot Codex](evals/pilot-codex-2026-11-10/README.md), [Claude 30 tác vụ](evals/p6-p7-claude-2026-11-10/README.md) | — | |
| P2 Bootstrap | **Cổng quyết định 1** | Chưa đạt điều kiện đo | [gate-1.md](gate-1.md) (chỉ có pilot) | `NT-GATE-1` | |
| P3 Context Engine | **Cổng quyết định 2** | Chưa mở | [gate-2.md](gate-2.md) (chỉ có pilot) | `NT-GATE-2` | |
| P4 Workflow | Một yêu cầu đi qua `b-do` với đầu ra liên kết bằng task ID; nâng cấp không mất lớp phủ | Đủ điều kiện | [P0–P4](bint-acceptance-p0-p4.md), [Codex đợt 1](evals/codex-flows-2026-11-10/README.md), [đợt 2](evals/codex-flows-2026-11-11/README.md) | — | |
| P5 Multi-Harness | Cùng workflow và tri thức dùng được trên nhiều harness, giới hạn báo đúng | Một phần | [P5](bint-acceptance-p5.md), [hỗn hợp Claude↔Codex](evals/claude-codex-mixed-2026-11-11/README.md), [Codex 2026-11-12](evals/codex-flow-acceptance-2026-11-12/README.md) | `NT-AGY-REAL`, `NT-WIN-CODEX` | |
| P6 Tiến hoá L1 | Đề xuất được chấp nhận có lợi trên tập `test` độc lập | Một phần | [P6](bint-acceptance-p6-dff6570.md), [V0](evals/v0-p6-fixes-2026-11-13/README.md) | `NT-EVO-TEST`, `NT-P6-06` | |
| P7 Tiến hoá L2 | Skill đề xuất từ lịch sử chứng minh hữu ích trên tập đối chiếu | Chưa đo | — | `NT-EVO-TEST` | |
| P8 Tiến hoá tự động | Cải thiện trên tập tác vụ mới, giữ điều kiện chất lượng và an toàn | Chưa đạt (vòng đầu `not_supported`) | [Claude 2026-11-10, mục "Mở P8"](evals/p6-p7-claude-2026-11-10/README.md) | `NT-EVO-TEST` | |

### 2.2. Kế hoạch bổ sung

| Kế hoạch | Mã đã xong | Trạng thái nghiệm thu | Còn mở |
|---|---|---|---|
| [E — bài học từ thất bại](p8-harness-evolution-plan.md) | E1–E5, kiểm thử xác định của E6 | Chưa đo | `NT-E6` |
| [H — thành phần quy trình](p8-harness-evolution-plan.md) | H1–H4 | Chưa đo | `NT-H5` |
| [Thử nghiệm A, B](p8-next-experiments.md) | B có đủ mã; A thiếu `eval snapshot import` | Chưa đo | `NT-EXP-A`, `NT-EXP-B` |
| [Cô lập và driver session](isolation-subagent-plan.md) | G1–G4 và việc tồn | G1–G3 đạt (G3 chạy thật 2026-11-12, mục 12.16); G4 chạy thật ở [V1](evals/v1-real-session-2026-11-13/README.md): không đạt (một lần ghi lọt ra cây gốc) | `NT-ISO-LEAK`, `NT-ISO-G4`, `NT-ISO-BYPASS` (vế Bash), `NT-ISO-MAXTURNS`, `NT-WIN-*` |
| [Gateway MCP](mcp-gateway-plan.md) | M0–M4; M5 đã đo bộ nhớ | Test tự động đạt cho M0–M4; M1 đạt với service thật ([V1](evals/v1-real-session-2026-11-13/README.md)) | `NT-MCP-PROBE`, `NT-MCP-M0`, `NT-MCP-M2`, `NT-MCP-M3`, `NT-MCP-S3`, `NT-MCP-M5-*` |
| [TUI](tui-plan.md) | Giai đoạn 0–6, màn MCP | Golden và test phủ lệnh đạt; chưa thử với người và terminal thật | `NT-TUI-*` |
| [Custom skill/flow](custom-flow-plan.md) | GĐ1 (`21171cb`), GĐ2 (`f77ae19`) | Tiêu chí bằng test của GĐ1–2 đạt; GĐ3–6 chưa commit | `NT-CF-MANUAL` (sau GĐ5–6) |

## 3. Danh mục tiêu chí còn mở

Cột "Vòng" trỏ tới mục 4. "Token" là token đo (eval/evolve), không tính token của phiên agent chạy tay.

### 3.1. Sửa mã trước khi nghiệm thu (không token)

| Mã | Mức | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-ISO-LEAK` | Cao | Ở V1, subagent `bint-implementer` (driver session, worktree, chạy nền) một lần ghi được tệp ra cây gốc mà hook không chặn và không để lại `guardrail.triggered`; không tái hiện được. Điều kiện đóng: lớp phát hiện độc lập với hook — `bint run complete` của bước `bint-implementer` (tác vụ worktree) quét transcript subagent, lời gọi ghi/Bash đã chạy ra ngoài worktree thì chặn bước và ghi `guardrail.triggered` (`outcome: missed_by_hook`) — có test; và `BINT_HOOK_TRACE=1` ghi vết mỗi lần hook chạy để chẩn đoán. (Không chụp cây gốc trước/sau: cây gốc bị người và tác vụ khác sửa song song nên báo nhầm.) | Phiếu riêng, trước khi chạy lại `NT-ISO-G4` |
| `NT-MCP-PROBE` | Thấp | `bint mcp check` phát hiện daemon codegraph env hẹp kể cả khi env vẫn chứa `codegraph_status` (gọi thử một tool mỗi nhóm được cấp, hoặc so danh sách) | Phiếu riêng |
| `NT-WF-BASELINE` | Trung bình | Tác vụ `shared`: commit của người hoặc thay đổi do tác vụ khác land vào cây trong lúc tác vụ chạy không bị `bint task diff` và Finalize tính là thay đổi của tác vụ (hiện bị chặn "thay đổi không thuộc tác vụ", chỉ gỡ được bằng `--force`); test tái hiện: commit lên HEAD giữa b-feat và Finalize | Phiếu riêng |
| `NT-P6-06` | Thấp | Các phát hiện low còn lại sau V0 (`mergeBodies` không idempotent với khối mã chưa đóng, nối trùng câu khác dạng, test `third_party` chưa chặt, `repoToplevel` gọi hai lần, submodule, nhánh merge của skill); danh sách ở [biên bản V0](evals/v0-p6-fixes-2026-11-13/README.md) | Phiếu riêng |

`NT-P6-01..05` đóng ở V0 (2026-11-13), chờ ký. `NT-MCP-CAT` đóng 2026-11-13 (tác vụ `01M7A80F2X6A803CFR491X14GR`; còn gợi ý low: thêm test giữ cố định `Install` của specmatic và tool `ask_wiki_question`). Mục tri thức `k:01M746PTBYPSXDT0WJZS38KPFD` đã lỗi thời sau V0,
chờ người sửa hoặc revalidate.

### 3.2. Chạy thật trên máy (cần người, không token đo)

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-ISO-G4` | isolation §7, 12.16 | Hai tác vụ `--driver session --worktree` chạy song song bằng subagent tới `done`; cây gốc có đủ thay đổi của cả hai; subagent cố sửa tệp ở cây gốc bị `deny` và spool có `guardrail.triggered` (`bint-worktree`) | V1 |
| `NT-ISO-BYPASS` | isolation 12.9 (S1, S3) | Vế hook `deny` đã đạt ở V1. Còn: ghi nhận `Bash(<mẫu>)` trong frontmatter subagent chỉ đọc có giới hạn lệnh dưới `bypassPermissions` hay không | V1 (lần sau) |
| `NT-ISO-MAXTURNS` | isolation 12.13 | `maxTurns` trong frontmatter subagent được Claude Code áp (bước dừng đúng trần) | V1 |
| `NT-MCP-M0` | mcp §4 | Một lượt `bint run b-do` thật ghi ≥ 1 dòng `knowledge_citations` từ bước con trên Claude **và** trên Codex (`codex exec` thật); bước Codex không thấy server MCP toàn cục | V1 |
| `NT-MCP-M2` | mcp §6 | Với service thật: hai worktree không dùng chung tiến trình loại `worktree`; kill upstream thì lời gọi sau chạy với generation mới | V1 |
| `NT-MCP-M3` | mcp §7 | 4 phiên Claude + 1 Codex cùng repo: một client codegraph và một specmatic cho mỗi worktree, một jira cho cả máy; `b-review` không thấy `spec_manage_mock_server`; lời gọi ngoài quyền bị từ chối; spool có `mcp.called` đúng run/role; daemon chết giữa phiên thì shim khởi động lại | V1 |
| `NT-MCP-S3` | mcp §10 | Ghi nhận Claude và Codex làm gì khi chính shim chết; Codex có chuyển env tới server MCP stdio không; Codex có nhận `approval_mode` cho tool chuyển tiếp không | V1 |
| `NT-MCP-M5-WT` | mcp §11 | Đo PSS với ≥ 2 worktree (mỗi worktree một phân vùng codegraph) | V1 |
| `NT-AGY-REAL` | P5, workflows | Antigravity thật: một lượt `b-do`, payload hook đúng dạng adapter chờ, MCP cấp người dùng khởi động với cwd là workspace | V1 |
| `NT-HOOK-COMPACT` | tri thức `k:01M74VDJXBG8BF6J1DWB56KW06` | Sau một phiên dài thật có nén ngữ cảnh: spool có `session.started` với `source=compact`, và có dấu vết việc đặt lại bộ nhớ | V1 |
| `NT-TUI-REAL` | tui 12.10 | Terminal thật: `NO_COLOR=1 bint tui`, `bint tui --read-only`, phím `?` ở từng thẻ phụ; màn MCP với service thật | V1 |
| `NT-TUI-VHS` | tui 12.10 | Ảnh VHS các màn chính | V1 |
| `NT-CF-MANUAL` | custom-flow §7 | Sau GĐ5–6: `b-add-skill`, `b-add-flow` và skill điều phối chạy với harness thật; ghi chi phí thật của `bint skill test` | V1 (lần sau) |

`NT-MCP-M0`: theo ghi chú phiên làm việc 2026-11-12, Claude đã đạt và Codex mới kiểm ở mức `codex mcp list`;
chưa có biên bản, nên vẫn mở cho cả hai. V1 (2026-11-13): bước Codex qua `bint run` có `knowledge.cited`, bước Claude
hỏng vì lỗi 403 của API, chưa kết luận.

`NT-MCP-M1` đóng ở V1 (2026-11-13). `NT-TUI-USER` bỏ qua theo quyết định của chủ dự án (2026-11-13).
`NT-MCP-M3` không đo được bằng driver session: subagent dùng role `interactive` của phiên cha (giới hạn đã biết của M3),
nên kiểm role/`mcp.called` phải dùng `bint run` (process driver).

### 3.3. Windows thật (cần máy Windows, không token đo)

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-WIN-TEST` | open-findings (R1), isolation | `go test ./...` qua trên Windows thật | V2 |
| `NT-WIN-LOCK` | isolation 12.1 | Hai `bint run` song song trên cùng repo: khoá chia sẻ/độc quyền `LockFileEx` đúng; hai binary khác phiên bản cùng giữ khoá | V2 |
| `NT-WIN-WT` | isolation 12.6 | `git worktree`, `land`/`cancel` khi tệp khoá bị xoá, đường dẫn dài, `git apply`, đọc hợp đồng thuê trong lúc `os.Rename` | V2 |
| `NT-WIN-MCP` | mcp §6–8 | Gateway: Job Object/detach, RSS trong `gateway status`, shim khởi động lại daemon | V2 |
| `NT-WIN-TUI` | tui 12.10 | TUI trong Windows Terminal: màu, chuột, chế độ hẹp | V2 |
| `NT-WIN-CODEX` | Codex 2026-11-12 | Runtime Codex trên Windows (reader, profile cô lập) | V2 |

### 3.4. Phép đo tốn token

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-EXP-B` | experiments B | B0: tỷ lệ đạt của `none` với model rẻ trên `validation` nằm trong 15–60 %; nếu ngoài khoảng thì đổi executor hoặc dừng hướng B | V3 |
| `NT-E6` | plan E, E6 | Có bài học từ thất bại đạt trên `test`; hoặc kết luận âm tính sau 3 đề xuất | V3 (lấy lượt trượt) + V4 |
| `NT-H5` | plan H, H5 | Bước 1: biết guardrail `test-before-stop` có tác động không (`bint` so với `bint@harness=all`); bước 2: một vòng cycle với cơ hội quy trình | V3 (bước 1) + V4 (bước 2) |
| `NT-EVO-TEST` | P6, P7, P8 | Ít nhất một đề xuất tự sinh đạt `supported` trên `test` độc lập và được xác nhận lại với 2 lần lặp; hoặc kết luận âm tính có ghi chép | V4 |
| `NT-MCP-M5-QUAL` | mcp §11 | Nhánh eval có/không `grants` cho `b-req`/`b-review`; chỉ đưa `grants` vào mẫu mặc định khi có cải thiện | V3 nếu có biến thể nhánh, nếu không thì hoãn |
| `NT-EXP-A` | experiments A | Δ(`bint` − `none`) với tri thức do người viết, theo cổng của Kế hoạch A | V5 |
| `NT-GATE-1` | [gate-1.md](gate-1.md) | Ngưỡng và điều kiện ở gate-1.md, trên ≥ 2 repository | V6 |
| `NT-GATE-2` | [gate-2.md](gate-2.md) | Ngưỡng và điều kiện ở gate-2.md | V6 |

## 4. Kế hoạch các vòng nghiệm thu

### 4.1. Tổng quan

| Vòng | Nội dung | Đóng các mã | Token đo (trần đề xuất) | Cần người | Điều kiện trước |
|---|---|---|---|---|---|
| **V0** | Sửa mã P6 và hồi quy tự động — **xong 2026-11-13** | `NT-P6-01..05` | 0 | Duyệt phiếu | — |
| **V1** | Một phiên chạy thật trên máy: G4 + MCP + TUI + Antigravity — **một phần 2026-11-13, dừng sớm** | `NT-ISO-*`, `NT-MCP-M0..M3`, `NT-MCP-S3`, `NT-MCP-M5-WT`, `NT-AGY-REAL`, `NT-HOOK-COMPACT`, `NT-TUI-*` | 0 (chỉ token phiên agent) | Có, suốt vòng | Service MCP cài trên máy |
| **V2** | Một lượt trên máy Windows | `NT-WIN-*` | 0 | Có | Máy Windows |
| **V3** | Một đợt eval trên `validation` với model rẻ, ba nhánh | `NT-EXP-B` (B0, B1), bước 1 của `NT-E6`, bước 1 của `NT-H5` | ~$15 | Duyệt ngân sách | Guardrail `test-before-stop` ở `candidate` |
| **V4** | Một vòng `evolve cycle` rồi xác nhận trên `test` | `NT-EVO-TEST`, `NT-E6`, `NT-H5` (bước 2), B2–B3 | ≤ $35 | Duyệt ngân sách | V0, V3 qua cổng B0 |
| **V5** | Kế hoạch A: tri thức do người viết | `NT-EXP-A` | ≤ $20 | Viết 10–20 mục tri thức | Lệnh `eval snapshot import` |
| **V6** | Cổng quyết định 1 rồi 2 trên ≥ 2 repository | `NT-GATE-1`, `NT-GATE-2` | Theo `bint eval estimate` | Duyệt ngân sách, chọn repository | Repository thứ hai có ≥ 20 tác vụ `test` |

Thứ tự đề xuất: V0 → V1 → V3 → V4 → V5 → V6; V2 chạy bất cứ lúc nào có máy Windows.

### 4.2. Vì sao gom như vậy

- **V3 dùng chung nhánh `bint`.** Ba kế hoạch đều cần một đợt trên `validation`: B0 cần `none`, B1 và bước 1
  của E6 cần `bint` (để lấy lượt trượt), bước 1 của H5 cần `bint` so với `bint@harness=all`. Chạy riêng là 5
  nhánh-đợt; gom lại còn 3 nhánh trong một đợt.
- **V4 dùng chung một vòng cycle.** Bước 2–3 của E6 (trần $30), bước 2 của H5 (trần $25) và B2 (trần $25) đều
  là `bint evolve cycle` đối chiếu trên `validation` rồi `test`. Một vòng với `--max 3` nhận cả cơ hội
  `failure_lesson` lẫn cơ hội quy trình, nên một lần chạy trả lời cả ba, và cũng là phép thử cho nghiệm thu
  P6/P7/P8. Trần gộp ≤ $35 (cycle ≤ $25, xác nhận B3 ≤ $10) thay cho tổng trần riêng lẻ khoảng $80.
- **V1 dùng hai tác vụ song song của G4 làm tải cho gateway MCP.** Hai phiếu `--worktree` chạy bằng subagent
  tạo đúng tình huống M2/M3 cần: nhiều phiên, hai worktree, lời gọi theo role. Cùng phiên đó quan sát bằng
  TUI và để phiên đủ dài cho việc nén ngữ cảnh.
- **V2 gom mọi mục Windows** vì chi phí chính là dựng máy, không phải từng phép kiểm.
- **V6 không gom hai cổng vào một đợt.** Chung nhánh `minimal` tiết kiệm một nhánh, nhưng nếu cổng 1 không
  qua thì nhánh `bint` bỏ đi và cổng 2 phải đo lại trên tri thức do người viết. Số nhánh-đợt kỳ vọng: chạy
  tuần tự là 2 + 2p, chạy gộp là 3, với p là xác suất qua cổng 1. Gộp chỉ rẻ hơn khi p > 0,5; số liệu hiện
  có (chênh chi phí +2 %, trong khi cổng 1 đòi giảm ≥ 10 %) cho thấy p thấp. Phần dùng chung thật sự là
  `bint eval bootstrap`, chạy một lần cho cả hai cổng.

### 4.3. V0 — Sửa mã P6 và hồi quy tự động

**Chuẩn bị.** Lập một phiếu `b-do` cho `NT-P6-01..03`, một phiếu cho `NT-P6-04..05`; phạm vi `internal/evolve/**`.

**Kiểm.**

```sh
go test -count=1 ./...
go test -race -count=1 ./internal/evolve/... ./internal/workflow/...
GOOS=windows go vet ./...          # staticcheck cho windows: binary đã cài, đặt GOOS
```

- `NT-P6-01`: theo mẫu `workflow.AcquireWriteLock` (cặp `*_unix.go`/`*_windows.go`), không chép kiểu thu hồi
  theo PID chết. Test tranh chấp chạy với `-count=5`.
- Mỗi bản sửa phải có test **hỏng** khi hoàn nguyên bản sửa.
- Chạy lại bộ tái hiện ở `docs/evals/acceptance-p6-dff6570/`.

**Bằng chứng.** Commit sửa, tên test, kết quả lệnh trên. Ghi vào mục 5.

### 4.4. V1 — Phiên chạy thật trên máy

Một buổi, một repository thử (bản sao của Bint là đủ), Claude Code và Codex đã đăng nhập. Agent không làm
được các bước đánh dấu **(người)** vì `bint mcp add|remove|index` là thao tác chỉ dành cho người.

**Bước 1 — Chuẩn bị (người).**

```sh
go install ./cmd/bint && bint init --hooks && bint doctor     # hook phải có SubagentStop và Stop
bint mcp add codegraph && bint mcp add specmatic && bint mcp add deepwiki
bint mcp index codegraph
bint mcp check                     # ghi kết quả từng service
bint doctor                        # gỡ thử một service rồi chạy lại: doctor phải báo thiếu
```

Đóng `NT-MCP-M1`. Thử thêm: chạy daemon codegraph với env hẹp rồi `bint mcp check` phải báo `degraded`.

**Bước 2 — Hai tác vụ song song bằng subagent.** Trong một phiên Claude Code, cấp `grants` cho role (ví dụ
`planner`, `reviewer`: `codegraph.explore`, `specmatic` nhóm chỉ đọc), rồi lập hai việc nhỏ không đụng tệp nhau:

```sh
bint task new --driver session --worktree "<việc 1>"
bint task new --driver session --worktree "<việc 2>"
```

Chạy cả hai bằng `/b-do --subagent …` cùng lúc. Mở thêm 2 phiên Claude và 1 phiên Codex cùng repo (gọi một
công cụ `mcp__bint__*` mỗi phiên) để đủ 4 Claude + 1 Codex.

Trong lúc chạy, ở terminal khác:

```sh
bint mcp gateway status --json     # mỗi worktree một phân vùng codegraph/specmatic; số phiên đúng
bint tui                           # màn Tác vụ và màn MCP cập nhật đúng (NT-TUI-REAL)
```

Kiểm và ghi:

| Kiểm | Đóng |
|---|---|
| Cả hai phiếu tới `done`, cây gốc có đủ thay đổi | `NT-ISO-G4` |
| Yêu cầu một subagent sửa tệp ở cây gốc: bị `deny`, `bint runs show <run>` có `guardrail.triggered` | `NT-ISO-G4` |
| `gateway status`: hai phân vùng `worktree` khác PID | `NT-MCP-M2` |
| `kill <PID upstream codegraph>` rồi gọi lại: generation tăng, lời gọi thành công | `NT-MCP-M2` |
| Bước `b-review` không liệt kê `spec_manage_mock_server`; gọi thẳng thì bị từ chối, `mcp.called` có `outcome: denied` | `NT-MCP-M3` |
| `mcp.called` trong spool đúng run/role cho từng bước | `NT-MCP-M3` |
| `bint mcp gateway stop` giữa phiên: lời gọi sau vẫn thành công (shim khởi động lại daemon) | `NT-MCP-M3` |
| Kill tiến trình shim của một phiên Claude và một phiên Codex: ghi lại harness làm gì | `NT-MCP-S3` |
| Bước con của Claude và của Codex (`roles.implementer: codex` cho một phiếu) có dòng `knowledge_citations` | `NT-MCP-M0` |
| Đo PSS mọi tiến trình MCP (`/proc/<pid>/smaps_rollup`) với hai worktree | `NT-MCP-M5-WT` |
| Một bước có `maxTurns` thấp dừng đúng trần | `NT-ISO-MAXTURNS` |
| Phiên đủ dài để nén ngữ cảnh: spool có `session.started` với `source=compact` | `NT-HOOK-COMPACT` |

**Bước 3 — `bypassPermissions` (người).** Mở một phiên Claude Code ở chế độ `bypassPermissions`, chạy một
phiếu `--driver session` nhỏ, yêu cầu planner sửa tệp và chạy lệnh ngoài `readonly_tools`. Ghi hook `deny`
còn áp không, kể cả với subagent chạy nền. Đóng `NT-ISO-BYPASS`.

**Bước 4 — Antigravity (người).** `bint init --mcp-user`, mở `agy` trong workspace, gọi một công cụ Bint, chạy
một `b-do` nhỏ. Lưu một payload hook thật để so với adapter. Đóng `NT-AGY-REAL`.

**Bước 5 — TUI với người dùng (người).** Hai đến ba người không quen CLI làm ba kịch bản của `NT-TUI-USER`
trên repo mới; ghi thời gian và chỗ họ kẹt. Chạy `NO_COLOR=1 bint tui`, `bint tui --read-only`, phím `?` ở
từng thẻ phụ. Chụp VHS các màn chính.

**Bằng chứng.** `docs/evals/v1-real-session-<ngày>/`: `gateway status --json`, trích spool cho từng kiểm,
`runs show`, bảng PSS, ghi chú S3, ảnh VHS, phiếu quan sát người dùng.

**Dừng khi.** Một kiểm lộ lỗi mức cao: dừng vòng, lập phiếu sửa, chạy lại riêng kiểm đó.

### 4.5. V2 — Windows thật

```powershell
go test -count=1 ./...
go test -count=1 ./internal/workflow/ ./internal/mcpgw/ ./internal/tui/...
```

Rồi chạy tay: hai `bint run` song song trên cùng repo (`NT-WIN-LOCK`); một phiếu `--worktree` tới `land`, một
phiếu `cancel`, một repo có đường dẫn dài (`NT-WIN-WT`); `bint mcp gateway start`, `status`, kill daemon rồi
gọi lại (`NT-WIN-MCP`); `bint tui` trong Windows Terminal (`NT-WIN-TUI`); một `bint run b-feat` với Codex
(`NT-WIN-CODEX`).

**Bằng chứng.** `docs/evals/v2-windows-<ngày>/`: phiên bản Windows, Go, git; đầu ra test; ghi chú từng kiểm.

### 4.6. V3 — Một đợt eval trên `validation`

**Chuẩn bị.**

- (người) Thêm guardrail mẫu ở `candidate`: `bint harness component add --template test-before-stop`.
- Chốt model thực thi rẻ (theo Kế hoạch B: Haiku) và ghi vào biên bản **trước khi chạy**.
- `bint eval estimate --split validation --arms none,bint,bint@harness=all`.

**Chạy.**

```sh
bint eval run --split validation --arms 'none,bint,bint@harness=all' \
  --repeats 1 --model <model rẻ> --budget 15 --yes
bint eval report <batch> --json > v3.json
```

**Đọc kết quả.**

| Câu hỏi | Lấy từ | Quyết định |
|---|---|---|
| B0: tỷ lệ đạt của `none` trong 15–60 %? | nhánh `none` | Ngoài khoảng: đổi executor (model mạnh hơn một bậc) hoặc dừng hướng B; không chạy V4 với model này |
| B1 và bước 1 của E6: có lượt trượt để rút bài học? | nhánh `bint`; `bint experience digest <batch>` | Thiếu nhóm thất bại (dưới `evolution.trigger.min_similar_runs` tác vụ phân biệt): ghi nhận, V4 chỉ còn cơ hội quy trình |
| Bước 1 của H5: guardrail có tác động? | `bint` so với `bint@harness=all`, bảng `eval_guardrail_usage` | Agent đã tự chạy test ở hầu hết lượt: guardrail này vô tác dụng, đổi dấu hiệu khác trước V4 |

`NT-MCP-M5-QUAL`: hiện chưa có biến thể nhánh bật/tắt `grants`. Nếu thêm được trước V3 thì chạy thành nhánh
thứ tư của cùng đợt; nếu không thì hoãn, không chạy đợt riêng chỉ cho mục này.

**Bằng chứng.** `docs/evals/v3-validation-<ngày>/`: `v3.json`, bản tóm tắt thất bại, bảng guardrail.

### 4.7. V4 — Một vòng `evolve cycle` và xác nhận trên `test`

**Điều kiện.** V0 xong (khoá và hậu kiểm của evolve đúng); V3 qua cổng B0.

**Chuẩn bị.** `roles.reflector: claude`, `harnesses.claude.models.reflector: opus`; evolution máy đang tắt nên
bật theo [evolution.md](evolution.md) cho riêng vòng này và ghi lại.

**Chạy.**

```sh
bint evolve scan                                  # liệt kê cơ hội: failure_lesson, harness, L1/L2
bint evolve cycle --model <model rẻ> --budget 25 --max 3 --json > v4-cycle.json
```

Vòng tự đối chiếu trên `validation` rồi `test` với Probe tuần tự (dừng sớm). Nếu có đề xuất `supported`
trên `test`, xác nhận lại:

```sh
bint evolve evaluate <proposal> --split test --model <model rẻ> --budget 10    # 2 lần lặp
```

**Kết luận.**

| Kết quả | Ghi |
|---|---|
| Có đề xuất `supported` trên `test` và xác nhận lại đạt | `NT-EVO-TEST` đạt. Loại đề xuất quyết định mục đi kèm: `failure_lesson` → `NT-E6`; `harness` → `NT-H5`; tri thức L1 → P6; skill L2 → P7. P8 đạt khi vòng tự động cho ra đề xuất đó. |
| Không đề xuất nào đạt sau 3 đề xuất | Kết luận âm tính cho cấu hình này. `NT-E6`, `NT-H5`, B đóng với kết quả âm; P6/P7/P8 giữ "Chưa đạt". Không chạy lại cùng cấu hình. |
| Gần chạm trần | Dừng, hỏi chủ dự án. |

**Bằng chứng.** `docs/evals/v4-cycle-<ngày>/`: `v4-cycle.json`, đề xuất và kết quả đối chiếu, chi phí từng phần.

### 4.8. V5 — Kế hoạch A: tri thức do người viết

Thiết kế và ràng buộc chống rò rỉ: [p8-next-experiments.md, Kế hoạch A](p8-next-experiments.md).

1. Viết mã `bint eval snapshot import --base <commit> --from <thư mục>` và nhãn `knowledge_origin: human`
   (không token).
2. Chọn vùng mã có ≥ 20 tác vụ `validation`/`test` với commit gốc gần nhau. Xem tỷ lệ đạt của `none` trên vùng
   đó từ kết quả V3: dưới 10 % hoặc trên 70 % thì chọn vùng khác **trước khi** người viết tri thức.
3. (người) Checkout commit gốc sớm nhất trong một worktree riêng, viết 10–20 mục, không mở danh sách tác vụ
   hay commit sửa lỗi.
4. Chạy `bint` với tri thức người viết trên các tác vụ của vùng, 2 lần lặp. Nhánh `none` dùng lại từ V3 nếu
   cùng model và cùng tác vụ; chủ dự án chốt việc dùng lại này trước khi chạy (Kế hoạch A gốc ghi Sonnet).
5. Cổng: theo Kế hoạch A (Δ ≥ +15 điểm % và khoảng tin cậy không chứa 0 → ủng hộ; Δ ≤ 0 → kết luận âm).

### 4.9. V6 — Cổng quyết định 1 và 2

Ngưỡng và điều kiện: [gate-1.md](gate-1.md), [gate-2.md](gate-2.md). Không chép lại ở đây để tránh hai nguồn.

**Điều kiện để phép đo được tính** (tóm tắt, bản đầy đủ ở phụ lục): ≥ 2 repository khác hệ sinh thái, mỗi
repository ≥ 20 tác vụ `test` đã kiểm chứng fail→pass, 3 lần lặp, cùng harness và model trong một đợt.

**Harness.** Cổng 1 cần chi phí USD hoặc `num_turns`; Codex và Antigravity không báo hai số này, nên chạy trên
Claude Code.

**Trình tự trên mỗi repository.**

```sh
bint init --no-hooks
bint eval mine --verify && bint eval approve <id>...
bint eval bootstrap --split test --budget <USD> --yes            # một lần, dùng cho cả hai cổng
bint eval estimate --split test --arms none,minimal --effect 0.1
bint eval run --split test --arms none,minimal --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate1-<repo>.json
```

Cổng 1 qua trên cả hai repository thì mới chạy cổng 2:

```sh
bint eval run --split test --arms minimal,bint --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate2-<repo>.json
```

Cổng 1 không qua: theo mục "Khi không qua" của gate-1.md (chẩn đoán trên `validation`, tối đa 2 vòng); sau đó
cổng 2 đo trên tri thức do người viết, dùng lại kết quả V5 làm điểm xuất phát.

## 5. Nhật ký nghiệm thu

Thêm một dòng sau mỗi vòng. Kết luận do chủ dự án ký.

| Ngày | Vòng | Commit | Mã đã đóng | Mã còn mở sau vòng | Biên bản | Chi phí | Ký |
|---|---|---|---|---|---|---|---|
| 2026-11-10 | (trước khi có tài liệu này) | `8b5cc9f` | R1–R3, F1, F3, F7, F8 (một phần), F11, F12 | — | [open-findings](bint-acceptance-open-findings.md) | 0 | |
| 2026-11-12 | (trước khi có tài liệu này) | `2d1d901` | G3 chạy thật | G4, bypassPermissions, Windows | [isolation 12.16](isolation-subagent-plan.md) | — | |
| 2026-11-13 | (trước khi có tài liệu này) | `b958512` | M5 bộ nhớ (một worktree) | M5 chất lượng, nhiều worktree | [mcp §11](mcp-gateway-plan.md) | 0 | |
| 2026-11-13 | V1 (một phần, dừng sớm) | clone `1c727f8` | `NT-MCP-M1`; `NT-ISO-BYPASS` (vế deny) | `NT-ISO-LEAK`, `NT-MCP-CAT`, `NT-MCP-PROBE` (mới); `NT-ISO-G4` không đạt; còn lại của V1 chưa chạy | [v1-real-session](evals/v1-real-session-2026-11-13/README.md) | 0 token đo; phiên agent < $5 (ước) | |
| 2026-11-13 | V0 | `f1467a0` | `NT-P6-01..05` | `NT-P6-06`, `NT-WF-BASELINE` (mới), `NT-EVO-TEST` | [v0-p6-fixes](evals/v0-p6-fixes-2026-11-13/README.md) | 0 token đo; $10,06 phiên agent | |

## 6. Mẫu biên bản một vòng

```markdown
# <Vòng> — <ngày>

- Commit kiểm tra: <sha>. Môi trường: <OS, Go, phiên bản harness, model>.
- Ngân sách được duyệt: <USD>, người duyệt: <tên>. Chi phí thực: <USD>.
- Tác vụ validation/test do agent duyệt: có/không.

| Mã | Kết quả | Bằng chứng | Ghi chú |
|---|---|---|---|
| NT-… | đạt / không đạt / không kết luận | evidence/<tệp> | |

## Lỗi tìm được
## Chưa kiểm được và lý do
## Đề xuất kết luận (chờ chủ dự án ký)
```

## 7. Biên bản lịch sử

Bản ghi bất biến; hiện trạng lấy ở mục 2 và 3.

| Biên bản | Commit | Kết luận tại thời điểm đó |
|---|---|---|
| [P0–P4](bint-acceptance-p0-p4.md) | `c526334` | A1–A4, đã đóng sau đó |
| [P5](bint-acceptance-p5.md) | `25202fb` | Chưa đủ điều kiện; P5-01..03 đã sửa ở `8ff7416` |
| [Tái nghiệm thu P0–P5](bint-reacceptance-p0-p5-137dbf9.md) | `137dbf9` | Chưa đủ điều kiện; R1–R3 |
| [Đóng lỗi còn mở](bint-acceptance-open-findings.md) | `8b5cc9f` | Đóng R1–R3 và F1, F3, F7, F8 (một phần), F11, F12 |
| [P6](bint-acceptance-p6-dff6570.md) | `dff6570` | Chưa đủ điều kiện; P6-01..05 |
| [Các đợt chạy harness thật](evals/README.md) | theo đợt | Pilot Codex, Claude 30 tác vụ, flow Codex, hỗn hợp, nghiệm thu Codex 2026-11-12 |
