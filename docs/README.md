# Tài liệu Bint

Mục lục các hướng dẫn sử dụng, tài liệu thiết kế và biên bản thực nghiệm của Bint.
Cài đặt và khởi tạo nhanh xem tại [README.md](../README.md).

## Hướng dẫn sử dụng

| Tài liệu | Nội dung |
|---|---|
| [Sử dụng CLI và TUI](usage.md) | Cài đặt, `bint init` và hiện diện trong Git, mã thoát, luồng lệnh chính, mở và điều khiển giao diện terminal |
| [Kiến trúc và dữ liệu](architecture.md) | Cây package, luồng ghi nhận qua spool, thư mục trạng thái, nguồn chuẩn và dữ liệu dẫn xuất |
| [Cấu hình](configuration.md) | Các lớp YAML, mặc định, chọn harness/mô hình theo role, giới hạn ngân sách và khoá dự phòng |
| [Tri thức và Context Engine](knowledge.md) | Bootstrap (P2), nhập tài liệu, vòng đời tri thức, kiểm tra xác định được, Context Engine, MCP và hook (P3) |
| [Workflow và multi-harness](workflows.md) | `b-req`/`b-feat`/`b-review`/`b-do` (P4), hợp đồng artifact v1, độ sâu lập kế hoạch, lớp phủ skill và adapter đa harness (P5) |
| [Khung đánh giá](evaluation.md) | Khai thác tác vụ từ Git, đặc tả đề bài, môi trường cô lập, chống rò rỉ, các nhánh đánh giá và biến thể `bint@…` (P1) |
| [Tiến hoá](evolution.md) | Tiến hoá tri thức L1 (P6), skill L2 (P7), bài học từ thất bại, Probe tuần tự, chế độ deferred và vòng `evolve cycle` (P8) |
| [Thành phần quy trình](harness-components.md) | `.bint/harness` (chỉ dẫn, guardrail, script, chiến lược ngữ cảnh), lệnh `bint harness component`/`render`, thực thi qua hook, đo trong eval và đề xuất tiến hoá (P8, kế hoạch H) |
| [Trạng thái và giới hạn](status.md) | Tổng hợp khả năng đã triển khai, kết quả thực nghiệm, phát hiện còn mở và giới hạn vận hành |
| [Phát triển và CI/CD](ci-cd.md) | Kiểm tra cục bộ trên Linux/Windows/macOS, lưu ý chạy test từ phiên agent và quy trình phát hành |

## Thiết kế và kế hoạch

| Tài liệu | Vai trò |
|---|---|
| [Đặc tả thiết kế](bint-plan.md) | Kiến trúc tổng thể, nguyên tắc thiết kế và điều kiện nghiệm thu P0–P8 (số mục được mã nguồn và tri thức tham chiếu) |
| [Cổng quyết định 1](gate-1.md) | Giao thức và ngưỡng nghiệm thu bootstrap tối giản (P2), chốt trước khi đo |
| [Cổng quyết định 2](gate-2.md) | Giao thức và ngưỡng nghiệm thu Context Engine (P3), chốt trước khi đo |
| [Quyết định fingerprint và outcome](fingerprint-outcome-decisions.md) | Quyết định về sai khác fingerprint đa ngôn ngữ và các tín hiệu kết quả trong đợt đo thực tế |
| [Tiến hoá quy trình và bài học từ thất bại](p8-harness-evolution-plan.md) | Kế hoạch E (bài học từ thất bại) và Kế hoạch H (tiến hoá thành phần quy trình); mã E1–E5 và H1–H4 đã xong, phần đo chưa chạy |
| [Kế hoạch thử nghiệm tiếp theo](p8-next-experiments.md) | Thiết kế thực nghiệm A (tri thức do người viết) và B (model mạnh rút bài học cho model yếu) |
| [Kế hoạch TUI](tui-plan.md) | Thiết kế và lịch sử triển khai giao diện terminal tương tác (các giai đoạn 0–6b đã có mã; xem trạng thái tái nghiệm thu bên dưới) |
| [Kế hoạch gateway MCP](mcp-gateway-plan.md) | Gateway MCP dùng chung cho mọi harness, danh mục và quản lý MCP (API/CLI/TUI), cấp MCP theo role, cấp MCP cho các bước `bint run` (đã chốt hướng, chưa triển khai) |

## Nghiệm thu và thực nghiệm

Xem [mục lục thực nghiệm và nghiệm thu](evals/README.md) cho toàn bộ báo cáo chạy harness thật
(Claude Code, Codex, hỗn hợp Claude↔Codex) và các biên bản nghiệm thu theo commit (`P0–P4`, `P5`,
`P6`, tái nghiệm thu và biên bản đóng lỗi).

Khối `bint:begin … bint:end` trong `AGENTS.md` và `CLAUDE.md` ở gốc repository do Bint sinh từ
`.bint/knowledge/items/`; sửa mục nguồn rồi chạy `bint bootstrap publish`, không sửa tay khối đã sinh.

## Tái nghiệm thu tại HEAD ngày 2026-11-13

Xem [báo cáo tái nghiệm thu toàn dự án](reacceptance-2026-11-13/README.md) để biết trạng thái hiện tại
của P0–P8, E/H, TUI, isolation, MCP gateway, gate và các đợt thử nghiệm. Báo cáo tách kiểm tra
chức năng khỏi hiệu quả thực nghiệm; lỗi môi trường và yêu cầu Claude bị hoãn được ghi rõ, không tính là đạt.

Phụ lục kiểm toán phần MCP/TUI mới tại baseline `04214d8`: [update-04214d8](reacceptance-2026-11-13/update-04214d8/README.md). Kết luận lịch sử phía trên được giữ nguyên.
