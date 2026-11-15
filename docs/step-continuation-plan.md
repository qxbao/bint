# Kế hoạch: nối tiếp phiên của bước (step continuation)

Trạng thái: đã thống nhất với người dùng (2026-11-16). Tài liệu đầu vào cho các phiếu b-do theo giai đoạn.

## Vấn đề

1. **Resume luôn chạy lại bước từ đầu.** Bước agent dừng vì nguyên nhân ngoại cảnh (hết hạn mức/429, API 5xx/quá tải,
   mất mạng, tiến trình harness chết, Ctrl-C, máy ngủ, bước hết hạn lease) thì `bint run resume` / `bint run next --resume`
   dựng một agent mới: đọc lại mã, đọc lại tài liệu, tốn chi phí như lần đầu, dù phiên cũ còn nguyên ngữ cảnh.
   `endStep` (`internal/workflow/run.go`) chỉ phân biệt huỷ / quá giờ / lỗi; bộ phân loại lỗi hạ tầng
   `classifyInfra` (`internal/eval/agent.go`) có sẵn nhưng workflow không dùng (`res.Infra`, `res.QuotaHit`).
2. **Câu hỏi của b-req chỉ có một vòng.** `stopOnQuestions` chặn tác vụ; người ghi `— Trả lời:` rồi resume thì tác vụ vào
   thẳng b-feat, không quay lại planner. Câu trả lời ABC làm nảy sinh câu hỏi BCD thì không có đường hỏi tiếp;
   `plan.md`/`tasks.md` không được cập nhật theo câu trả lời; prompt b-feat chỉ liệt kê đường dẫn `requirement.md`.
3. Bint không lưu định danh phiên harness của từng bước (driver process), nên không thể nối tiếp kể cả khi muốn.

## Mục tiêu

Một cơ chế chung **"nối tiếp phiên của bước"** cho **mọi bước** (plan, implement, review, bước tuỳ biến của flow):
bước dừng vì nguyên nhân ngoại cảnh hoặc vì chờ người thì lần chạy sau **gửi tiếp vào đúng phiên agent cũ** (giữ ngữ
cảnh) thay vì dựng agent mới. Bước dừng vì nguyên nhân nội tại vẫn đi như hiện nay.

## Phân loại lý do dừng (`stop_class`)

| Lớp | Ví dụ | Lần chạy sau |
|---|---|---|
| `external` | 429/hạn mức, API 5xx/overloaded/api_error, lỗi mạng, harness chết/thoát bất thường không có kết quả, Ctrl-C (`interrupted`), lease hết hạn | nối tiếp phiên cũ |
| `awaiting_human` | câu hỏi mở của b-req, đường dẫn bảo vệ cần duyệt, `autonomy: ask_always` | nối tiếp phiên cũ, kèm câu trả lời/quyết định của người |
| `intrinsic` | kiểm chứng không qua, phát hiện review, vi phạm phạm vi/chỉ đọc, guardrail deny, rò cô lập, agent tự báo thất bại | như hiện nay (chạy mới / vòng sửa) |
| `timeout` | quá `workflow.timeout_minutes` | nối tiếp **tối đa một lần**; quá giờ lần nữa thì coi là `intrinsic` |

Phân loại là hàm thuần, xác định được, có kiểm thử bảng. Không chắc thì chọn `intrinsic` (an toàn: chạy mới như cũ).

## Điều kiện được nối tiếp (thiếu một điều kiện → chạy mới như cũ, ghi chú lý do vào `notes`)

1. Có handle phiên của bước dừng gần nhất, cùng bước, cùng harness, cùng thư mục làm việc (root/worktree).
2. Cây làm việc **không đổi** kể từ lúc bước dừng (so fingerprint/snapshot đã lưu lúc dừng; phần trạng thái Bint
   như `.bint/` không tính). Riêng `awaiting_human` cho phép artifact tác vụ (requirement.md…) đổi vì đó là nơi ghi câu trả lời.
3. Số lần nối tiếp liên tiếp của bước < giới hạn (mặc định 3; `timeout` chỉ 1).
4. Còn ngân sách: chi phí và số lượt cộng dồn theo bước/lượt như bình thường, không reset khi nối tiếp.
5. Người không yêu cầu chạy mới (`--rerun`/cờ tương đương buộc chạy mới).

## Lời nhắn nối tiếp

Ngắn, do Bint dựng (i18n như các thông điệp khác): lý do bước dừng, run ID con mới (qua biến môi trường như mọi bước),
lời dặn "tiếp tục từ chỗ dở, đừng làm lại phần đã xong; kiểm lại trạng thái cây trước khi sửa", và với `awaiting_human`:
nguyên văn các dòng câu hỏi + câu trả lời / quyết định duyệt.

## Giai đoạn

### Giai đoạn 1 — Phân loại lý do dừng và lưu handle (chỉ ghi nhận, chưa đổi hành vi resume)

**Đã làm (2026-11-16).** `workflow.ClassifyStop` (`internal/workflow/stopclass.go`) phân loại ở `endStep`; kết quả ghi vào
`StepResult` (`stop_class`, `continuable`, `session_id`), payload `workflow.finished` của lượt con và `task.State.LastStep`
(`task.StepStop`; xoá ở `beginStep`; không chạm SQLite). `awaiting_human` ghi ở cổng sau bước (`markAwaitingHuman`) bằng
handle của bước vừa kết thúc **trong cùng lượt**; resume không có bước nào chạy thì giữ `LastStep` cũ. `bint run status` và
`bint task show` (văn bản, `--json`) hiển thị `last_step`. Giả định/hạn chế đã biết:

- Bước workflow của Claude chạy với `--session-id <uuid>` và **bỏ `--no-session-persistence`** (không lưu phiên thì không
  `--resume` được); lượt `bint eval` giữ nguyên cờ. Hệ quả: transcript bước workflow nằm trong `~/.claude/projects`.
- `continuable` = lớp cho phép (external/awaiting_human/timeout) **và** có handle **và** chụp được fingerprint.
- Driver session: câu hỏi mở phát hiện ở lệnh `next` kế tiếp (không cùng lệnh với `complete`) chưa có handle trong bộ nhớ,
  nên chưa ghi `awaiting_human`; giai đoạn 2 cần lưu handle bền cho trường hợp này.
- Chỉ fingerprint cây tại lúc ghi; việc so sánh và điều kiện nối tiếp thuộc giai đoạn 2/3.
- **Đổi hành vi chấm eval của Codex (có chủ đích, giữ lại).** `CodexAgent.Run` gán `Infra`/`QuotaHit` từ `codexInfra` trên lỗi
  `turn.failed`/`error`; regex `codexServerRe`/`codexRateRe` đã thu hẹp (`unexpected status 5xx/429`, `overloaded`...). Lỗi
  Codex 429/5xx/quá tải là **lượt hạ tầng**: `runner.go` coi `Infra` khác rỗng là lượt `error`, không chấm; `QuotaHit` (429,
  usage limit) dừng cả đợt — đồng nhất với Claude. Khoá bằng `TestCodexRunClassifiesInfraFromTurnFailed` (cấp `CodexAgent.Run`)
  và `TestCodexInfra`.

- Phân loại `stop_class` ở `endStep` (dùng lại `classifyInfra`/`res.Infra`/`res.QuotaHit`, mã thoát, `TimedOut`,
  `ctx.Err()`, deny của hook, vi phạm), ghi vào kết quả bước (`StepResult`, sự kiện `step.finished`/runlog) và vào phiếu
  (bước dừng gần nhất) để giai đoạn sau đọc.
- Lưu handle phiên của bước:
  - Claude (`claude -p`): Bint **tự cấp UUID** bằng `--session-id <uuid>` lúc khởi chạy (bền kể cả khi tiến trình chết
    trước khi in JSON), đối chiếu `session_id` trong kết quả JSON nếu có.
  - Codex (`codex exec --json`): lấy thread/session id từ sự kiện đầu (`thread.started` hoặc tương đương của bản
    đang cài — kiểm bằng fixture/testdata theo phiên bản như các testdata hiện có).
  - Harness khác: không có handle → `continuable=false`.
  - Driver session: `agent_id` (đã có `--agent-id` ở `bint run complete`) là handle.
- Lưu fingerprint cây làm việc lúc bước dừng.
- `bint run status`/`bint task show` (văn bản và `--json`) hiển thị lớp dừng và "có thể nối tiếp".
- Không đổi hành vi resume. Trường mới trong state phải tương thích ngược (state cũ thiếu trường vẫn nạp được).

### Giai đoạn 2 — Nối tiếp cho driver session + vòng câu hỏi nhiều lượt

- `bint run next --resume` (và `next` thường khi gặp bước dừng loại nối tiếp được): trả `action: "agent"` kèm
  `continue_agent_id` (agent ID cũ) và `prompt` = lời nhắn nối tiếp; orchestrator dùng **SendMessage** tới agent đó thay
  vì gọi Agent mới. Không gửi được (agent không còn) thì orchestrator báo lại bằng cờ/lệnh để Bint cấp prompt đầy đủ
  và chạy mới.
- **Vòng câu hỏi nhiều lượt của b-req trong cùng bước**: skill b-do hướng dẫn: subagent planner trả về mà
  `bint task lint` còn câu hỏi mở → orchestrator hỏi người dùng → SendMessage câu trả lời cho đúng planner → planner cập
  nhật cả requirement/plan/tasks, có thể thêm câu hỏi mới → lặp; hết câu hỏi mới `bint run complete`. Cần cơ chế để
  thời gian người suy nghĩ không làm hết lease (gia hạn lease cho bước, ví dụ `bint run extend <id> --run <run_id>`,
  hoặc trạng thái chờ trả lời); chọn cách đơn giản, có kiểm thử.
- **Resume sau câu hỏi đã trả lời quay về planner**: tác vụ b-do bị chặn vì câu hỏi mở, sau khi người ghi câu trả lời
  thì resume **nối tiếp bước plan** (gửi câu trả lời cho planner cũ để nó cập nhật plan/tasks và hỏi tiếp nếu cần) khi
  nối tiếp được; không nối tiếp được thì giữ hành vi hiện tại (vào thẳng b-feat) — không chạy lại b-req mới từ đầu.
- Prompt b-feat: khi `requirement.md` có câu hỏi đã trả lời, chèn nguyên văn các dòng `Q-N … — Trả lời: …` vào prompt.
- Skill b-do: thêm quy tắc định tuyến yêu cầu mơ hồ (yêu cầu chung chung/thiếu tiêu chí → hỏi người dùng trước khi lập
  phiếu, hoặc `--plan`), và mô tả vòng SendMessage ở Bước 2b/Bước 3. Sửa nguồn skill trong `internal/skills/**`
  (bản cài `.claude/skills/**` sinh ra từ đó).

### Giai đoạn 3 — Nối tiếp cho driver process (Claude, rồi Codex)

- `bint run resume` / `bint run b-do --task` gặp bước dừng loại nối tiếp được: gọi harness ở chế độ resume với lời nhắn
  nối tiếp:
  - Claude: `claude -p --resume <session_id> …` (cùng cờ quyền/MCP/max-turns/model như bước gốc; kiểm `claude --help`
    bản đang cài; không dùng `--fork-session` trừ khi cần).
  - Codex: `codex exec resume <SESSION_ID> <PROMPT>` (codex-cli 0.169.0 có lệnh này; giữ nguyên các `-c` của bước gốc,
    lưu ý điểm dễ sai về `-c mcp_servers` trong tri thức dự án).
- Câu hỏi mở của b-req ở driver process: sau khi người trả lời, resume nối tiếp phiên planner (như giai đoạn 2).
- **Tính chi phí**: xác định với từng harness chi phí/tokens báo về sau khi resume là cộng dồn cả phiên hay chỉ phần mới;
  không được tính trùng. Ghi rõ trong mã và test bằng fixture.
- Hook/guardrail: phiên nối tiếp nhận `BINT_RUN_ID` con mới; nhật ký deny đọc theo run mới.
- Kiểm thử bằng agent giả/fixture (không gọi Claude/Codex thật trong `go test`).

## Ngoài phạm vi

- tmux / điều khiển phiên tương tác qua màn hình (đã cân nhắc và loại: không chạy native trên Windows, đọc màn hình dễ vỡ,
  mất dữ liệu có cấu trúc).
- Harness ngoài Claude/Codex.

## Ràng buộc chung

- Mã qua `go vet`/`staticcheck` với cả linux và `GOOS=windows`.
- Trường mới trong state/sự kiện tương thích ngược; golden test (`internal/workflow/testdata/flow_golden.json`) cập nhật có chủ đích.
- Lệnh/cờ CLI mới: cập nhật `cliRefs` (`internal/tui/actions/cliref.go`) và inventory test.
- Thông điệp người dùng đi qua i18n (vi/en) như phần còn lại.
