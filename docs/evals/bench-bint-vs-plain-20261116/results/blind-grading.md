# Chấm mù (2 agent Opus mới, mỗi task một agent)

Đầu vào mỗi người chấm: đặc tả giai đoạn và câu yêu cầu, hai diff đã bỏ tệp của Bint và xoá ID phiếu (nhãn X/Y gán
ngẫu nhiên), kết quả test ẩn (bản v2), mã gốc tại `2b2f74d`. Bảng ghép nhãn: `blind-mapping.json`
(mid: X = B, Y = A; big: X = A, Y = B), chỉ mở sau khi cả hai người chấm đã nộp.

## Task trung (GĐ3): X = B (Bint), Y = A (không Bint)

| | Đúng đặc tả | Chất lượng | Test | Phạm vi | Tổng |
|---|---|---|---|---|---|
| X (B) | 4 | 4 | 5 | 4 | 17 |
| Y (A) | 4 | 3 | 4 | 3 | 14 |

Kết luận của người chấm: **X tốt hơn**. Lý do: test kỹ hơn rõ rệt (biên −1/=/+1/null, UTF-8, chứng minh dry-run/doctor
không ghi, so byte AGENTS.md), xử lý lỗi chặt hơn (doctor báo fail khi dựng khối lỗi), ít sửa ngoài phạm vi.

Nhận xét chính:

- **X (B)**:
  - Thiếu i18n vi/en; mã gốc của doctor và CLI cũng viết thẳng chuỗi như vậy.
  - `UnmarshalYAML` tự viết để chặn số thực bị cắt thành int; đúng nhưng hơi thừa.
  - Công thức token chép lại thay vì dùng chung với ctxengine.
  - `acceptance.md` chứa nhật ký chạy.
- **Y (A)**:
  - Có i18n và dùng chung công thức token với ctxengine.
  - Tên check doctor phụ thuộc ngôn ngữ, nên đầu ra JSON không ổn định giữa các máy.
  - Doctor nuốt lỗi dựng khối.
  - Tầng app nạp catalog ngôn ngữ theo biến môi trường.
  - Nới `TestNoUnusedKeys` sang quét cả `internal/`.
  - Thêm `docs/superpowers/plans/…` và sửa chính tệp kế hoạch.

## Task lớn (GĐ2a + GĐ2b): X = A (không Bint), Y = B (Bint)

| | Đúng đặc tả | Chất lượng | Test | Phạm vi | Tổng |
|---|---|---|---|---|---|
| X (A) | 4 | 4 | 4 | 3 | 15 |
| Y (B) | 3 | 3 | 4 | 3 | 13 |

Kết luận của người chấm: **X tốt hơn**. Lý do: X bám đặc tả GĐ2a, dựng lại được từ cặp `Verification.Tree/Worktree` của
dữ liệu lịch sử và ghi rõ đó là ước lượng; mã gọn hơn và dùng lại ảnh chụp cây có sẵn.

Phần offline của Y chỉ tính được `no_change` từ dữ liệu mà chế độ shadow ghi sau này, nên với phiếu lịch sử gần như luôn
trả "không biết". Người chấm coi đó là lệch đặc tả GĐ2a, không phải thận trọng hợp lý.

Y chặt hơn ở test Runner (khớp luồng giữa off và shadow trên hai driver) và ở việc đọc log an toàn (`os.OpenRoot`). Ngược
lại, Y có các điểm trừ sau:

- Đặt hằng khoá i18n ở `tui/actions` để bộ quét TUI coi là đã dùng (lách test, sai tầng).
- Đảo thứ tự trường `State`.
- Tệp `progress_read.go` dài 458 dòng với logic `extra_rounds` rối.
- Không thêm hàng `cliRefs`.

Cả hai bản đều từ chối `mode: stop`; người chấm không trừ điểm vì hành vi dừng thuộc GĐ2c.
