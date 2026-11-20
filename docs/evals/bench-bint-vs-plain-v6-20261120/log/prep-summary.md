# Chuẩn bị đợt 6 (2026-11-20, Windows 11, D:\bench\bench-bint-v6-20261120)
- Commit gốc 17812a0; binary đóng băng bin/bint.exe (sha256 trong bint.sha256).
- A-L1: commit "bench: gỡ Bint (bên A)" = gỡ .bint/.agents/.claude/.codex/.mcp.json/AGENTS.md/CLAUDE.md + khối bint trong .gitignore
  + bỏ qua 4 test đọc tệp Bint của repo (log/baseline-fix-A.patch).
- B-L1: bint init --hooks (BINT_STATE_DIR=state-B, BINT_CONFIG_HOME=config-B chỉ có codegraph), codegraph index,
  .bint/local.yml: roles=codex, mọi model gpt-6.2-sol, workflow.slice_timeout_minutes=30 (repo đặt 0; mẫu mặc định 30).
- CODEX_HOME riêng mỗi bên: gpt-6.2-sol, effort high, model_context_window 872000 (theo quyết định), provider codex-lb, windows sandbox elevated.
- Baseline go test ./...: lần 1 (2 repo song song, timeout 10m mặc định) đỏ vì quá giờ ở cli/workflow + test đọc tệp Bint ở A + 2 test thời gian
  (TestCLIRefTasksScreen, TestCleanupWorktreeReleasesGatewayBounded). Chạy lại các gói đỏ, -timeout 40m, tuần tự: B xanh; A xanh sau bản sửa test.
  Kết luận: đỏ còn lại là quá giờ/chập chờn của Windows, không do mã gốc.
- Test ẩn viết lại cho Windows: hidden/hidden_l1.py. Đối chứng âm binary gốc: ma trận 36 ô (S1–S4, S6: 7 loại; S5: private_key), 0/36 sạch; N1 qua.
- Baseline sau gỡ tài liệu benchmark (48dbfad): B go test ./... -timeout 40m xanh; A internal/cli + internal/app xanh.
- Sự cố môi trường (04:00–04:11): hai CODEX_HOME khởi động cùng lúc, mỗi bên đặt lại mật khẩu tài khoản sandbox Windows
  CodexSandboxOffline → bên kia đăng nhập sai → Windows khoá 10 phút (CreateProcessWithLogonW 1909). Lượt 2 huỷ (aborted-2/), không tệp nào đổi.
  Sửa: chép .sandbox-secrets/sandbox_users.json của codex-home-A sang codex-home-B và ~/.codex (bản cũ sao lưu ở tmp/sbx-backup/);
  smoke cả hai bên chạy lệnh shell được, không đặt lại mật khẩu.
