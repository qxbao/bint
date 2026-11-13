# V1 — Phiên chạy thật trên máy (một phần) — 2026-11-13

- Commit kiểm tra: bản clone dùng một lần của Bint tại `1c727f8` (+ 3 commit cấu hình cục bộ của bản clone:
  `mcp.services`, `grants` gồm `interactive` chỉ nhóm đọc, `cite_knowledge` của Codex). Binary build riêng từ bản clone.
- Môi trường: Linux amd64, Go 1.25.1; Claude Code 2.1.323 (phiên điều phối `sonnet`, `bypassPermissions`, do chủ dự án
  mở trong tmux); Codex CLI 0.167.0; codegraph 1.6.2 (npm), specmatic-mcp 1.2.0 (npm), deepwiki (HTTP).
- Ngân sách: 0 token đo. Token phiên: không đo đủ (nhiều bước hỏng giữa chừng vì lỗi đăng nhập 403 của Claude API; Bint ghi
  `cost_usd` rỗng cho các bước đó). Ước lượng dưới $5.
- Tác vụ validation/test do agent duyệt: không áp dụng.
- **Vòng dừng sớm** theo mục 4.4 ("một kiểm lộ lỗi mức cao: dừng vòng") sau khi `NT-ISO-G4` lộ một lần ghi lọt ra cây gốc,
  và vì Claude API trả 403 (Cloudflare, `authentication_failed`) lặp lại bốn lần làm hỏng ba lượt chạy.

## Kết quả

| Mã | Kết quả | Bằng chứng | Ghi chú |
|---|---|---|---|
| `NT-MCP-M1` | **đạt** | [m1-human-step1.log](evidence/m1-human-step1.log), [m1-check-daemon-env-explore-only.log](evidence/m1-check-daemon-env-explore-only.log) | `add`/`check`/`remove` ba service; `index codegraph` (719 tệp) thêm `.codegraph/` vào `.git/info/exclude`; `doctor` báo service dự án khai báo mà máy chưa đăng ký; daemon codegraph chạy env hẹp (thiếu `codegraph_status`) → `check` báo `degraded` kèm hướng dẫn. Lệnh chỉ dành cho người do chủ dự án chạy ở terminal riêng |
| `NT-ISO-BYPASS` | **đạt một phần** | [hook-probe-writes.tsv](evidence/hook-probe-writes.tsv), [guardrail-events.txt](evidence/guardrail-events.txt) | Dưới `bypassPermissions`, `deny` của hook áp cho `bint-implementer` cả tiền cảnh (E4) lẫn chạy nền (E5, G4 lần 2). Chưa kiểm vế "`Bash(<mẫu>)` có giới hạn lệnh của subagent chỉ đọc dưới bypass" |
| `NT-ISO-G4` | **không đạt** | [g4-subagent-A-tool-calls.txt](evidence/g4-subagent-A-tool-calls.txt), [hook-probe-writes.tsv](evidence/hook-probe-writes.tsv) | Lần 1: subagent `bint-implementer` (chạy nền, bypass) của phiếu A ghi được `V1_ROOT_PROBE.txt` ra cây gốc, không có `guardrail.triggered`. Lần 2 (phiếu D, cùng điều kiện, có ghi log hook): bị chặn đúng, `outcome: fired`. Hai phiếu song song chưa chạy tới `done` (403) |
| `NT-MCP-M0` | chưa kết luận | [taskC-run-summary.json](evidence/taskC-run-summary.json) | Phiếu C: b-feat Codex (`bint run`, process driver) xong, có `knowledge.cited` từ `hook:codex`; b-review Claude hỏng vì 403 |
| `NT-MCP-M2`, `NT-MCP-M5-WT` | chưa đo | [gateway-status-peak.json](evidence/gateway-status-peak.json), [pss-timeline.log](evidence/pss-timeline.log) | Gateway tạo phân vùng `worktree` riêng cho worktree phiếu C (codegraph, specmatic) nhưng khởi động lười; không bước nào gọi codegraph trong worktree nên không có hai tiến trình cùng sống để so PID/PSS. Phân vùng cây gốc: codegraph PSS ≈ 78 MB, specmatic ≈ 23 MB |
| `NT-MCP-M3`, `NT-MCP-S3`, `NT-ISO-MAXTURNS`, `NT-HOOK-COMPACT`, `NT-AGY-REAL`, `NT-TUI-REAL`, `NT-TUI-VHS` | chưa chạy | — | Dời sang lần sau |
| `NT-TUI-USER` | bỏ qua | — | Chủ dự án quyết định bỏ |

## Chẩn đoán lần ghi lọt của G4

Lần 1 (09:40:57 UTC): transcript subagent A cho thấy `Write` vào cây gốc trả "File created successfully"; spool không có
`guardrail.triggered`; không có `hook-errors.log`. Đã thử tái hiện, mọi lần đều bị chặn:

| Phép thử | Chế độ | Subagent | Kết quả |
|---|---|---|---|
| Phát lại payload của lần 1 vào `bint hook claude` | bypass | bint-implementer | `deny` (nhánh dự phòng, bước của A đã kết thúc) |
| Phát lại với session-step còn hạn và transcript có `<bint-task>` | bypass | bint-implementer | `deny`, `fired` (nhánh ánh xạ) |
| E1 (`claude -p`) | mặc định | general-purpose | cho qua — đúng thiết kế (guard chỉ cho `bint-implementer`) |
| E2, E3 (`claude -p`) | mặc định | bint-implementer, tiền cảnh / nền | `deny` |
| E4, E5 (phiên điều phối) | bypass | bint-implementer, tiền cảnh / nền | `deny` |
| G4 lần 2 (phiếu D, `bint run next` → Agent) | bypass | bint-implementer, nền | `deny`, `fired` |

Độ trễ hook đo lại 35–125 ms kể cả khi đang chạy `go test ./...` (không có dấu hiệu quá hạn 10 s). Nguyên nhân lần 1
chưa xác định. Hệ quả vẫn nghiêm trọng: khi hook của Bint không chạy hoặc không trả quyết định, Claude Code cho lệnh đi
qua (fail-open) và Bint không có dấu vết nào để phát hiện sau đó.

## Lỗi tìm được

1. **`NT-ISO-LEAK` (cao):** một lần ghi ra cây gốc của subagent không bị chặn, không tái hiện được, không để lại dấu vết.
   Đề xuất: lớp phát hiện độc lập với hook — `bint run complete` của bước worktree chụp cây gốc trước/sau bước (như
   `guardReadOnly`), thay đổi ở cây gốc mà không thuộc tác vụ nào thì chặn bước và báo; và wrapper log hook (như phiên này
   dùng) bật được bằng cấu hình để chẩn đoán.
2. **`NT-MCP-CAT` (trung bình):** danh mục MCP lệch upstream: `specmatic` gợi ý `uv tool install specmatic-mcp==1.2.0`
   nhưng PyPI không có (đúng là `npm i -g specmatic-mcp@1.2.0`); `deepwiki` đã đổi `ask_question` → `ask_wiki_question`
   (`check` báo `degraded`). Phiếu C (Codex) đã có bản sửa trong worktree bản clone, chưa review, chưa đưa về repo.
3. **`NT-MCP-PROBE` (thấp):** `mcp check` chỉ gọi thử `codegraph_status`; daemon chạy env hẹp mà vẫn có tool này
   (ví dụ chỉ `codegraph_status`) qua `check` dù nhóm `query` còn lại bị tắt
   ([m1-check-daemon-env-status-only.log](evidence/m1-check-daemon-env-status-only.log)).
4. **Ghi nhận:** Claude Code 2.1.323 chạy subagent bất đồng bộ ngay cả khi không đặt `run_in_background`.
5. **Ghi nhận:** phiên điều phối chép tay prompt của `bint run next` và làm lệch khối `<bint-context>` (đã tự báo). Gợi ý:
   skill b-do nên bảo ghi JSON ra tệp rồi đọc, hoặc `bint run next` in prompt ra tệp.
6. **Ghi nhận:** `bint init --mcp` báo khối `bint` trong `.codex/config.toml` "đã bị sửa ngoài Bint" ngay sau `init --hooks`
   trên bản clone mới; phải `--force`.

## Môi trường đã đổi trên máy (để chủ dự án biết)

- Cài toàn cục: `@colbymchenry/codegraph@1.6.2`, `specmatic-mcp@1.2.0` (npm), `vhs` (Go), `ttyd` 1.7.7 (`~/.local/bin`).
- `~/.config/bint/mcp.yml`: đăng ký codegraph, specmatic, deepwiki (cấp máy).
- `~/.codex/config.toml`: thêm trust cho đường dẫn bản clone (bản sao lưu ở thư mục làm việc của phiên).
- Bản clone, worktree và tiến trình gateway/codegraph nằm ngoài repo; gỡ khi đóng phiên.

## Đề xuất kết luận (chờ chủ dự án ký)

`NT-MCP-M1` đạt. `NT-ISO-BYPASS` đạt vế hook `deny`. `NT-ISO-G4` không đạt; mở `NT-ISO-LEAK` (cao) và sửa trước khi chạy
lại G4. Các mục còn lại của V1 chạy lại ở phiên sau, khi Claude API ổn định.
