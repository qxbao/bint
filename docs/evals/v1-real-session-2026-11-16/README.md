# V1 (chạy lại, buổi A) — 2026-11-16

- Commit kiểm tra: `6e84247` (binary `bint 0.1.0-dev (6e84247)`), chạy trên **repo chính**, không phải bản clone.
- Môi trường: Linux 7.0, Go 1.25.1, Claude Code 2.1.325 (subagent: planner/reviewer opus, implementer sonnet),
  codex-cli 0.167.0, codegraph 1.6.2.
- Cấu hình riêng cho vòng: `.bint/local.yml` (gitignore) thêm `specmatic` vào `mcp.services`; grants:
  planner/reviewer/interactive `specmatic.compat`, implementer `specmatic.*`.
- Ngân sách token đo: 0. Chi phí phiên agent cho hai phiếu: 3,20 USD (A 0,55; B 2,65), chưa tính phiên điều phối.
- Tác vụ validation/test do agent duyệt: không dùng (vòng này không chạy eval).
- Cùng lúc có một phiên khác chạy tác vụ `shared` trên cây gốc (spike iso, `01M7H8YGHV5YRPAFG8ZMZBXDW5`); điều này ảnh hưởng
  kết quả (xem "Lỗi tìm được" mục 3). Lần chạy tiếp theo dùng bản clone.

Ghi chép theo thời gian và mọi số liệu: [evidence/notes-step2.md](evidence/notes-step2.md).

## Kết quả

| Mã | Kết quả | Bằng chứng | Ghi chú |
|---|---|---|---|
| `NT-ISO-G4` | đạt (kèm điều kiện) | taskA-*, taskB-*, step2-guardrail-events.txt | Hai phiếu `--driver session --worktree` chạy song song bằng subagent tới `done`; cây gốc nhận đủ 4 tệp (+143/−4); `bint-worktree` deny lệnh Bash không `cd <worktree>` của implementer, có `guardrail.triggered`; hậu kiểm không có `missed_by_hook`. Điều kiện: cả hai phiếu phải thu hẹp lệnh kiểm chứng (người set + approve) vì base chứa lỗi có sẵn (mục 3). Chưa thử ca "yêu cầu subagent sửa tệp ở cây gốc" một cách chủ động. |
| `NT-MCP-SEED` (chạy thật) | đạt | step2-seed-worktree-codegraph.txt | Mỗi worktree chỉ nhận `.gitignore` + `codegraph.db`. |
| `NT-MCP-M2` | đạt | step2-gateway-two-partitions.json, step2-gateway-after-kill.json | Worktree B và cây gốc: hai tiến trình codegraph khác PID. Kill upstream của B rồi gọi lại: thành công, generation 2, restarts 1. |
| `NT-MCP-M3` | một phần | step2-gateway-5sessions.json, step2-gateway-after-stop.json | Đạt: một client codegraph cho mọi phiên cùng worktree (5 phiên → 1 tiến trình); `gateway stop` giữa phiên → shim khởi động lại daemon, lời gọi sau thành công. Không đạt: `mcp.called` không ghi cho phiên tương tác (mục 1). Chưa kiểm: `b-review` không thấy `spec_manage_mock_server`, lời gọi ngoài quyền bị từ chối (cần `bint run`, driver process); jira không cấu hình. |
| `NT-MCP-M5-WT` | đã đo | step2-pss-two-worktrees.txt | PSS: daemon 14 MB; codegraph worktree B 363 MB (5 tiến trình); cây gốc 67 MB (3); 6 shim 59 MB. |
| `NT-MCP-S3` | một phần | notes-step2.md | Kill shim: Claude Code gỡ mọi tool `mcp__bint__*`, không tự khởi động lại, `/mcp` → Reconnect thì dùng lại được. Codex cũng mất tool, không sinh shim mới. Chưa đo: env tới server stdio của Codex, `approval_mode` cho tool chuyển tiếp. |
| `NT-MCP-M1` (lặp lại) | đạt | step1-mcp-check.log, step1-doctor.log | `mcp check`: codegraph/specmatic/deepwiki ok. |
| Các mã khác của V1 | chưa chạy | — | `NT-ISO-BYPASS` (vế Bash), `NT-ISO-MAXTURNS`, `NT-MCP-M0`, `NT-AGY-REAL`, `NT-HOOK-COMPACT`, `NT-H1-STOP`, `NT-TUI-REAL`, `NT-TUI-VHS`, `NT-RESUME-*`, `NT-CF-MANUAL`. |

Hai phiếu đã đưa về cây gốc (chưa commit):

- A `01M7H9FPFY3NYKEB3W4MS20FM1`: `TestCopyCodegraphIndexSkipsDaemonFiles` (`internal/workflow/codegraph_copy_test.go`).
- B `01M7H9FZQ6JACTH163E6NSH985`: hạn riêng cho từng lời gọi thử tool trong `bint mcp check codegraph`
  (`internal/app/mcp_svc.go`, `mcp_svc_test.go`, `internal/mcpcat/probe/probetest/probetest.go`) — đóng low còn lại của
  `NT-MCP-PROBE`, nhưng review có 1 medium (mục 5).

## Lỗi tìm được

1. **(medium) `mcp.called` của phiên tương tác bị bỏ hết.** `internal/mcpgw/record.go:29` chỉ ghi khi có `RunID` hoặc cặp
   Harness + SessionID; `internal/app/mcp_shim.go:71` không bao giờ đặt Harness/SessionID. Mọi lời gọi từ phiên tương tác và
   subagent của driver session bị bỏ (`dropped_events` = 14 sau vài phút). Nhánh "cặp harness + session" là mã chết.
2. **(low/medium) `bint-readonly` chặn nhầm lệnh chỉ đọc** của planner/reviewer: `grep -n "a\|b" …`, `git ls-files …`,
   `grep -n "NT-…" docs/…`, `grep -E` trên tệp log của tác vụ. Agent vẫn xong việc nhưng tốn lượt.
3. **(thiết kế) Base của worktree chụp cả thay đổi chưa commit đang dở của cây gốc.** Lúc 11:40 một phiên khác đang sửa
   `docs/README.md` (thêm `bint flow simulate`); base của cả hai phiếu nhận dòng đó nên `TestDocCommandsExist` đỏ ở kiểm
   chứng của cả hai, dù diff của phiếu không liên quan. Cùng cơ chế: phiếu `--worktree` không tạo được worktree khi tác vụ
   `shared` khác đang giữ khoá ghi cây gốc (driver session trả `blocked` ngay, phải chờ ~17 phút).
4. **(low) Shim là điểm chết duy nhất của mỗi phiên**: cả Claude Code và Codex không tự khởi động lại server stdio khi shim chết
   (mục `NT-MCP-S3`).
5. **(medium, review phiếu B .1)** `bint mcp check codegraph` không còn trần thời gian tổng (xấu nhất ≈ 190 s). Cùng .2–.4 (low)
   và A.2 (low): đã lập phiếu sửa `01M7HDFSQFGFCZYWXN891HVCE8`, chưa chạy.
6. **(low)** `bint task set --verify-command` nhận lệnh có ký tự xuống dòng không cảnh báo (dán bị ngắt dòng → kiểm chứng mã 127).
   Help của `bint task new --from-findings` ghi `<id>...` nhưng chỉ nhận danh sách cách bằng dấu phẩy.
7. **(tài liệu, đã sửa)** Mục 4.4 ghi người chạy thao tác chỉ-người bằng `! <lệnh>` trong phiên; sai vì shell của phiên có
   `CLAUDECODE=1`. Đã sửa thành "terminal riêng".

Ngoài Bint: mỗi phiên Claude tự chạy một `specmatic-mcp` từ cấu hình MCP người dùng (ngoài gateway); có tiến trình
`specmatic-mcp` sống 2–5 ngày từ phiên cũ.

## Chưa kiểm được và lý do

- `NT-MCP-M3` vế role/`mcp.called`: driver session dùng role `interactive` của phiên cha và `mcp.called` bị bỏ (lỗi 1); cần
  `bint run` (driver process) sau khi sửa lỗi 1.
- Ca chủ động "subagent sửa tệp ở cây gốc bị deny" của G4: prompt bước phải giữ nguyên văn nên không chèn yêu cầu; V1 lần
  trước đã có vế deny bằng hook probe.
- Các bước 3–6 của V1: chuyển sang lần chạy tiếp theo trên bản clone.

## Sai lệch quy trình

- Chạy trên repo chính đồng thời với một tác vụ `shared` của phiên khác (gây mục 3).
- Lệnh kiểm chứng của cả hai phiếu được người thu hẹp về gói bị đụng; tiêu chí 5 của phiếu B (do planner viết, đòi
  `go test ./...` qua) được người đánh dấu `met` kèm lý do.
- Prompt b-feat của phiếu A do phiên điều phối chép tay có một chỗ khác nguyên văn trong khối `bint-context` (thêm một đường dẫn
  bằng chứng vào một mục tri thức); không chạm khối `bint-task`.

## Đề xuất kết luận (chờ chủ dự án ký)

- Đóng `NT-ISO-G4` (kèm ghi chú mục 3), `NT-MCP-M2`; ghi `NT-MCP-SEED` đã kiểm chạy thật; `NT-MCP-M5-WT` đã đo.
- `NT-MCP-M3`, `NT-MCP-S3` giữ mở (một phần).
- Lập phiếu sửa lỗi 1 (mcp.called) trước khi đo lại M3; xem xét lỗi 2 và 3.
