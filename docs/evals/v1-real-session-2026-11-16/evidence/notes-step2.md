# Ghi chép bước 2 (đang chạy)

- 11:23 lập phiếu A 01M7H9FPFY3NYKEB3W4MS20FM1 (--no-plan), B 01M7H9FZQ6JACTH163E6NSH985 (--plan); cả hai blocked:
  tác vụ shared 01M7H8YGHV5YRPAFG8ZMZBXDW5 (spike iso, phiên khác) giữ khoá ghi cây gốc (b-feat rồi verify).
  ensureWorktree chụp cây gốc dưới khoá chia sẻ (worktree.go:336) — đúng thiết kế; driver session trả blocked thay vì chờ.
- 11:40:28 khoá nhả; next --resume tạo hai worktree. Mỗi worktree .codegraph chỉ có .gitignore + codegraph.db (62 MB)
  (step2-seed-worktree-codegraph.txt) — chạy thật NT-MCP-SEED đạt.
- 11:40:28 gateway khởi động lại (PID 846096 → 981865): binary mới cài (go install 11:19), shim thay daemon chạy binary cũ (ExeModTime).
- Phân vùng codegraph cho worktree B (PID 992851) tạo khi planner gọi codegraph; worktree A không có phân vùng (implementer không gọi codegraph).
- Sau khi người mở thêm 2 Claude + 1 Codex: gateway 14 phiên; codegraph gốc 1 tiến trình (PID 1016554) cho 5 phiên.
  Shim: 846076, 858685, 1013985, 1023790 (Claude), 1028026 (Codex). (step2-gateway-5sessions.json)
- LỖI: dropped_events = 14, không có mcp.called nào từ 04:40Z. record.go:29 bỏ sự kiện khi thiếu RunID và thiếu cặp
  Harness+SessionID; app/mcp_shim.go:71 không bao giờ đặt Harness/SessionID cho shim → mọi lời gọi của phiên tương tác
  và subagent (driver session) bị bỏ. Nhánh "cặp harness + session" trong record() là mã chết.
- Mỗi phiên Claude còn tự chạy một specmatic-mcp (npm exec) từ cấu hình MCP người dùng, ngoài gateway; có tiến trình
  specmatic-mcp sống 2–5 ngày từ phiên cũ.
- Phiếu A verify-1 không qua: TestDocCommandsExist — docs/README.md:37 `bint flow simulate`. Base (23099a9, chụp 11:40)
  = 6e84247 + thay đổi chưa commit của cây gốc lúc đó: một phiên khác đang sửa docs/README.md (commit 78dba2c lúc 11:44,
  sau đó lại sửa tiếp chưa commit). Không do thay đổi của phiếu A: hệ quả của việc chụp cây gốc bẩn làm base.
- Lệch nhỏ: prompt b-feat phiếu A chép tay có một chỗ khác nguyên văn trong khối bint-context (thêm một đường dẫn bằng chứng).
- 11:55 người chạy `bint mcp gateway stop` ở terminal riêng (lần đầu gõ `! …` trong phiên agent bị từ chối vì CLAUDECODE=1 —
  đúng thiết kế, tài liệu 4.4 đã sửa). Gọi lại cg_status ở một phiên Claude: thành công; daemon mới PID 1145874 khởi động
  11:55:16 (gateway.log), codegraph gen 1 PID 1145901 (step2-gateway-after-stop.json). NT-MCP-M3 vế "shim khởi động lại" đạt.
- 12:02 phiếu A done: resume chạy lại kiểm chứng (lệnh thu hẹp `go test -count=1 ./internal/workflow/`, người set + approve ở
  terminal riêng) → qua; land 1 tệp (internal/workflow/codegraph_copy_test.go, +54) về cây gốc (taskA-finalize.json, taskA-run-status.txt).
  Chi phí: b-feat 0,12 + b-review 0,42 = 0,55 USD.
- Guardrail (step2-guardrail-events.txt): bint-worktree deny lệnh Bash của implementer B không bắt đầu bằng `cd <worktree>`
  (đúng). bint-readonly deny 5 lệnh CHỈ ĐỌC của planner/reviewer: `grep -n "a\|b" …`, `git ls-files …`, `grep -n "NT-…" docs/…`,
  `go test …` (đúng: reviewer không chạy test), `grep -n -E "…" <tệp ngoài worktree>`. Ít nhất 3 lần là chặn nhầm lệnh đọc
  (cần xem readonly_tools/phân tích lệnh). Không có missed_by_hook.
- 12:03 hai phân vùng codegraph cùng lúc: worktree B PID 1342969, cây gốc PID 1145901 (step2-gateway-two-partitions.json).
  PSS (step2-pss-two-worktrees.txt): daemon 14 MB; cây codegraph B 363 MB (5 tiến trình), gốc 67 MB (3); 6 shim 59 MB.
- 12:03 kill 1342969 rồi gọi cg_explore với projectPath worktree B: thành công; phân vùng B gen 2, restarts 1, PID 1350165
  (step2-gateway-after-kill.json, gateway.log "upstream đã đóng stdout"). NT-MCP-M2 đạt.
- Phiếu B: review 4 phát hiện (.1 medium: mcp check không còn trần tổng ~190 s; .2–.4 low). Finalize chặn (verify đỏ do README
  base; tiêu chí 5 tự viết đòi go test ./...). Người: thu hẹp verify (`go test -count=1 ./internal/app/ ./internal/mcpcat/...`),
  approve, đánh dấu tiêu chí 5 met kèm lý do. Lần đầu lệnh lưu có "\n" giữa chừng (dán bị ngắt dòng) → verify-2 mã 127;
  `bint task set --verify-command` không cảnh báo ký tự xuống dòng (gợi ý low). Lần hai qua.
- 12:32 phiếu B done, land 3 tệp (mcp_svc.go +38/-4, mcp_svc_test.go +49, probetest.go +6). Chi phí: b-req 1,35 + b-feat 0,43 +
  review 0,87 = 2,65 USD. Cả hai worktree đã gỡ.
- Cây gốc có đủ thay đổi của cả hai phiếu (git diff --stat: 4 tệp, +143/-4), lẫn với thay đổi chưa commit của phiên spike iso
  (internal/iso/**, config, state…) — khác tệp, không xung đột.
- Phiếu sửa từ phát hiện: 01M7HDFSQFGFCZYWXN891HVCE8 (B.1–.4, A.2), chưa chạy. `bint task new --from-findings` nhận danh sách
  cách bằng dấu phẩy; help ghi `<id>...` gây hiểu là nhiều đối số (gợi ý low).
- NT-MCP-S3, Claude Code: 12:07:47 kill shim `bint mcp serve` (PID 1013985) của phiên Claude #1. Lời gọi sau: Claude Code coi server
  MCP bint đã ngắt kết nối, gỡ toàn bộ tool mcp__bint__* khỏi phiên, KHÔNG tự khởi động lại stdio server; agent đề nghị người
  kết nối lại bằng /mcp. (Nguyên văn phản hồi do người dán.)
- Claude #1 sau /mcp → Reconnect: gọi cg_status lại thành công (người xác nhận).
- NT-MCP-S3, Codex: kill shim (PID 1028026) cùng lúc. Lời gọi sau: Codex không gọi được tool của server bint (người báo
  "không gọi được"), không có tiến trình `bint mcp serve` mới dưới Codex → Codex cũng không tự khởi động lại server stdio.
  Chưa đo: cách khôi phục trong Codex (khởi động lại phiên?), Codex có chuyển env tới server stdio không, approval_mode cho
  tool chuyển tiếp.
