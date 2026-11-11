# Tài liệu Bint

Mục lục các hướng dẫn sử dụng, tài liệu thiết kế và biên bản thực nghiệm của Bint.
Cài đặt và khởi tạo nhanh xem tại [README.md](../README.md).

## Hướng dẫn sử dụng

| Tài liệu | Nội dung |
|---|---|
| [Kiến trúc và dữ liệu](architecture.md) | Cây package, luồng ghi nhận qua spool, thư mục trạng thái, nguồn chuẩn và dữ liệu dẫn xuất |
| [Cấu hình](configuration.md) | Các lớp YAML, mặc định, chọn harness/mô hình theo role, giới hạn ngân sách và khoá dự phòng |
| [Tri thức và Context Engine](knowledge.md) | Bootstrap (P2), nhập tài liệu, vòng đời tri thức, kiểm tra xác định được, Context Engine, MCP và hook (P3) |
| [Workflow và multi-harness](workflows.md) | `b-req`/`b-feat`/`b-review`/`b-do` (P4), hợp đồng artifact v1, độ sâu lập kế hoạch, lớp phủ skill và adapter đa harness (P5) |
| [Khung đánh giá](evaluation.md) | Khai thác tác vụ từ Git, đặc tả đề bài, môi trường cô lập, chống rò rỉ, các nhánh đánh giá và biến thể `bint@…` (P1) |
| [Tiến hoá](evolution.md) | Tiến hoá tri thức L1 (P6), skill L2 (P7), bài học từ thất bại, Probe tuần tự, chế độ deferred và vòng `evolve cycle` (P8) |
| [Trạng thái và giới hạn](status.md) | Tổng hợp khả năng đã triển khai, kết quả thực nghiệm, phát hiện còn mở và giới hạn vận hành |
| [Phát triển và CI/CD](ci-cd.md) | Kiểm tra cục bộ trên Linux/Windows/macOS, lưu ý chạy test từ phiên agent và quy trình phát hành |

## Thiết kế và kế hoạch

| Tài liệu | Vai trò |
|---|---|
| [Đặc tả thiết kế](bint-plan.md) | Kiến trúc tổng thể, nguyên tắc thiết kế và điều kiện nghiệm thu P0–P8 (số mục được mã nguồn và tri thức tham chiếu) |
| [Cổng quyết định 1](gate-1.md) | Giao thức và ngưỡng nghiệm thu bootstrap tối giản (P2), chốt trước khi đo |
| [Cổng quyết định 2](gate-2.md) | Giao thức và ngưỡng nghiệm thu Context Engine (P3), chốt trước khi đo |
| [Quyết định fingerprint và outcome](fingerprint-outcome-decisions.md) | Quyết định về sai khác fingerprint đa ngôn ngữ và các tín hiệu kết quả trong đợt đo thực tế |
| [Tiến hoá quy trình và bài học từ thất bại](p8-harness-evolution-plan.md) | Kế hoạch E (bài học từ thất bại, đã có mã) và Kế hoạch H (tiến hoá thành phần quy trình, đang đề xuất) |
| [Kế hoạch thử nghiệm tiếp theo](p8-next-experiments.md) | Thiết kế thực nghiệm A (tri thức do người viết) và B (model mạnh rút bài học cho model yếu) |
| [Kế hoạch TUI](tui-plan.md) | Thiết kế giao diện terminal tương tác (chưa triển khai) |

## Nghiệm thu và thực nghiệm

Xem [mục lục thực nghiệm và nghiệm thu](evals/README.md) cho toàn bộ báo cáo chạy harness thật
(Claude Code, Codex, hỗn hợp Claude↔Codex) và các biên bản nghiệm thu theo commit (`P0–P4`, `P5`,
`P6`, tái nghiệm thu và biên bản đóng lỗi).

Khối `bint:begin … bint:end` trong `AGENTS.md` và `CLAUDE.md` ở gốc repository do Bint sinh từ
`.bint/knowledge/items/`; sửa mục nguồn rồi chạy `bint bootstrap publish`, không sửa tay khối đã sinh.
