# Chuẩn bị lần 3 (2026-11-17)
- Gốc 8757340; binary đóng băng bin/bint sha256 35a8ebc3…0691 (chmod a-w).
- 4 repo (nhánh bench). A: gỡ Bint + bench: sửa TestDocCommandsExist (bỏ qua nguồn đã gỡ). B: bint init --hooks, codegraph index, trust project + hook trong ~/.codex/config.toml (khối bench-bint-v3-20261117; bản sao lưu log/codex-config.toml.before-bench).
- go test ./...: B-T1, B-T2 xanh; A-T1, A-T2 chỉ đỏ TestDocCommandsExist, sửa xong, gói cli xanh.
- Đối chứng âm: noop/naive/lockhog/T2-orig KHỚP lần 2 sau khi sửa fixture D5 (log/fixture-changes.md).
