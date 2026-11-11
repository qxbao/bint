# Biên bản nghiệm thu và thực nghiệm

Mục lục các đợt đo trên harness thật và các biên bản nghiệm thu theo commit. Mỗi báo cáo phản ánh
đúng phiên bản mã nguồn và cấu hình tại thời điểm chạy; tổng hợp hiện trạng tại `HEAD` xem ở
[docs/status.md](../status.md).

## Các đợt chạy harness thật

| Báo cáo | Phạm vi và kết quả chính |
|---|---|
| [Pilot Codex — 2026-11-10](pilot-codex-2026-11-10/README.md) | 2 tác vụ validation × 3 nhánh × 3 lần lặp (18 lượt) kiểm chứng hạ tầng đánh giá `strict` trên Codex CLI |
| [Số liệu P6–P7 và mở P8 (Claude Code) — 2026-11-10](p6-p7-claude-2026-11-10/README.md) | Đo nền trên 30 tác vụ, bổ sung đặc tả giao diện đề bài, kiểm chứng Probe tuần tự và chạy vòng `evolve cycle` đầu tiên |
| [Kiểm chứng các flow Codex — 2026-11-10](codex-flows-2026-11-10/README.md) | 15 phiên chạy thật kiểm tra nhóm luồng F1–F7 (`b-do`, bảo vệ đường dẫn, duyệt và đánh giá `strict`) |
| [Nghiệm thu tiếp các flow Codex — 2026-11-11](codex-flows-2026-11-11/README.md) | Thêm 15 phiên chạy thật hoàn tất kiểm chứng 5 skill, fail-open hook, hậu kiểm bước chỉ đọc, timeout và Ctrl-C/`resume` |
| [Nghiệm thu flow hỗn hợp Claude ↔ Codex — 2026-11-11](claude-codex-mixed-2026-11-11/README.md) | Chạy thật cả hai hướng phân vai Claude ↔ Codex và chuỗi chặn đường dẫn bảo vệ → `task approve` → chạy tiếp (5 bản sửa đã hợp nhất vào `main`) |

## Biên bản nghiệm thu theo commit

| Biên bản | Nội dung |
|---|---|
| [Biên bản nghiệm thu P0–P4](../bint-acceptance-p0-p4.md) | Kiểm tra độc lập tại commit `c526334` (P0–P4) |
| [Biên bản nghiệm thu P5](../bint-acceptance-p5.md) | Kiểm tra adapter đa harness tại commit `25202fb` |
| [Tái nghiệm thu P0–P5 (`137dbf9`)](../bint-reacceptance-p0-p5-137dbf9.md) | Kiểm tra lại P0–P5 kèm [artifact đối chứng](reacceptance-p0-p5-137dbf9/), phát hiện `R1–R3` |
| [Nghiệm thu các lỗi còn mở sau P0–P5 và P6–P7](../bint-acceptance-open-findings.md) | Xác nhận đóng `R1–R3` và `F1/F3/F7/F8/F11/F12` tại `8b5cc9f` |
| [Nghiệm thu P6 (`dff6570`)](../bint-acceptance-p6-dff6570.md) | Kiểm tra tiến hoá tri thức L1 kèm [artifact tái hiện](acceptance-p6-dff6570/), ghi nhận `P6-01..P6-05` |

Các thư mục `evidence/`, tệp log và ảnh chụp tác vụ đi kèm báo cáo là dữ liệu đối chứng lịch sử
(đã ẩn thông tin nhạy cảm), giữ nguyên để phục vụ kiểm toán và tái hiện. Giao thức nghiệm thu
chính thức cho cổng 1 và cổng 2 xem tại [docs/gate-1.md](../gate-1.md) và [docs/gate-2.md](../gate-2.md).
