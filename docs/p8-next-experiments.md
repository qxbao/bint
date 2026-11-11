# Kế hoạch thử nghiệm tiếp theo sau khi mở P8

Ngày: 2026-11-11. Bối cảnh: [số liệu P6–P7](evals/p6-p7-claude-2026-11-10/README.md), [trạng thái hiện tại](status.md) và [Kế hoạch E/H](p8-harness-evolution-plan.md).

## Hiện trạng

- **Tỷ lệ đạt:** trên 30 tác vụ, có Bint đạt 40%, không có Bint đạt 37%. Khoảng tin cậy rộng ±15 điểm %, nên chưa có khác biệt.
- **Chi phí:** ngang nhau (chênh lệch khoảng +2%, chưa có ý nghĩa thống kê).
- **Tri thức trong đợt đo:** do agent tự viết từ mã, chưa qua người duyệt. Agent giải đề tự đọc mã cũng biết những điều này, nên tri thức hầu như không thêm thông tin mới.
- **Giới hạn của cỡ mẫu:** với 30 tác vụ chỉ phát hiện được hiệu ứng khoảng 15 điểm % trở lên. Mức +3–5 điểm như các công bố về tối ưu harness thì cần hàng trăm tác vụ.

Có hai kế hoạch. Chúng độc lập với nhau và mỗi kế hoạch có cổng dừng riêng:

- **Kế hoạch A** kiểm tra giả thuyết cốt lõi: tri thức mà chỉ người biết có giúp agent không.
- **Kế hoạch B** kiểm tra hướng "model mạnh viết cải tiến cho model rẻ thực thi". Các công bố về tối ưu harness thu được nhiều nhất ở cấu hình này.

---

## Kế hoạch A — Tri thức do người viết

**Câu hỏi:** khi tri thức chứa điều không suy ra được từ mã, chẳng hạn quy ước, bẫy cũ hay lý do thiết kế, agent có giải đúng nhiều hơn không?

**Ràng buộc chống rò rỉ.** Người viết tri thức không được biết lời giải của tác vụ. Nếu viết ở HEAD hiện tại thì tri thức có thể đã chứa lời giải của các commit cũ. Ngoài ra, chế độ economy loại mọi mục tạo sau commit gốc, nên tri thức viết hôm nay sẽ không được dùng cho tác vụ cũ. Vì vậy:

1. Chọn một vùng mã có nhiều tác vụ, ví dụ một thư mục có ≥ 20 tác vụ validation/test có commit gốc nằm gần nhau.
2. Lấy commit gốc sớm nhất B0 của nhóm tác vụ đó. Người viết checkout B0 trong một worktree riêng và viết tri thức chỉ dựa trên mã và lịch sử tới B0. Người viết không mở danh sách tác vụ hay commit sửa lỗi.
3. Mỗi mục phải có bằng chứng (tệp:dòng) tồn tại ở B0. Khi dựng ảnh chụp cho từng tác vụ, Bint kiểm tra bằng chứng còn khớp tại commit gốc của tác vụ (giống điều kiện của chế độ economy).

**Mã cần viết cho A** (khoảng nửa ngày, không tốn token — hiện chưa triển khai):

- `bint eval snapshot import --base <commit> --from <thư mục items>`: nhận mục do người viết (origin human) làm ảnh chụp của commit gốc. Hiện ảnh chụp chỉ nhận mục do agent viết, thông qua `bint eval bootstrap`.
- `bint eval snapshot import --group <vùng>`: áp một bộ mục cho mọi tác vụ của vùng. Mục nào có bằng chứng không còn khớp ở commit gốc của tác vụ thì bị loại và được ghi lại.
- Thêm nhãn `knowledge_origin: human` vào đợt chạy và báo cáo, để không lẫn với kết quả dùng tri thức tự sinh.

**Việc của người dùng** (khoảng 30–60 phút):

- Viết 10–20 mục cho vùng đã chọn bằng `bint knowledge add` trong worktree ở B0.
- Ưu tiên điều mà đọc mã không thấy:
  - "phải gọi X trước Y vì …";
  - "test của module này kiểm …";
  - "đừng sửa Z, sửa ở W";
  - lỗi hay tái phát.
- Không viết lại điều mà mã đã nói rõ.

**Đo:**

| Bước | Cấu hình | Ước tính chi phí |
|---|---|---|
| A1 | `none` trên ~20 tác vụ của vùng, 2 lần lặp (dùng lại kết quả cũ nếu đã có) | $0–8 |
| A2 | `bint` với tri thức người viết, cùng tác vụ, 2 lần lặp, Sonnet | ~$8 |
| A3 (tuỳ chọn) | `bint` với tri thức tự sinh trên cùng tác vụ, để tách tác động của nguồn tri thức | ~$8 |

Trần ngân sách: $20.

**Cổng quyết định:**

- **Δ(bint − none) ≥ +15 điểm % và khoảng tin cậy không chứa 0:** giả thuyết được ủng hộ. Ưu tiên tính năng giúp người ghi tri thức dễ hơn, như gợi ý điều cần ghi từ lượt thất bại, rồi mới tới tối ưu tự động.
- **Δ dương nhưng khoảng tin cậy chứa 0:** chưa kết luận. Chỉ mở rộng thêm 20 tác vụ nếu người dùng duyệt thêm ngân sách.
- **Δ ≤ 0:** ghi kết luận âm tính. Với loại tác vụ này, đưa tri thức vào ngữ cảnh không cải thiện được agent mạnh. Dừng hướng tối ưu tri thức và giữ Bint làm công cụ quản lý tri thức và giám sát.

**Rủi ro:**

- 20 tác vụ trong một vùng có thể không đủ.
- Tác vụ có thể quá dễ, khi `none` đã đạt trên 70%, hoặc quá khó, khi dưới 10%; cả hai trường hợp đều khó thấy khác biệt. Kiểm tra tỷ lệ đạt của `none` trên vùng trước khi người dùng bắt đầu viết.

---

## Kế hoạch B — Model mạnh đề xuất, model rẻ thực thi

**Câu hỏi:** khi đề xuất do Opus viết, còn agent giải đề chạy model rẻ hơn (Haiku), vòng `bint evolve cycle` có tạo được cải tiến đạt trên tập test độc lập không? Nếu có thì đây là nghiệm thu P8.

**Khác biệt so với vòng P8 đầu tiên:**

1. Agent giải đề yếu hơn nên còn nhiều chỗ để cải thiện. Agent viết đề xuất mạnh hơn nên có thể mang thông tin mới.
2. Nguồn cơ hội là **lượt thất bại trên tập validation**, không chỉ lịch sử phiên làm việc. Reflector đọc bản tóm tắt của lượt trượt và viết mục `lesson` (đề xuất skill đi qua luồng L2 riêng).
3. Phạm vi thay đổi gồm bài học từ thất bại và quy trình (skill; chỉ dẫn công cụ thuộc [Kế hoạch H](p8-harness-evolution-plan.md)).

**Ràng buộc chống rò rỉ:**

- Chỉ đọc lượt thất bại trên **validation**. Không bao giờ đọc log, đề bài hay diff của tập **test**.
- Bài học rút ra từ một tác vụ không được dùng khi chấm lại chính tác vụ đó (mục 12.3 của kế hoạch Bint).
- Bài học không được chứa tên tệp, hàm hay đoạn mã của lời giải (`CheckLesson` kiểm tra xác định dòng mã, định danh mới và chuỗi literal dài so với diff lời giải của các tác vụ validation nguồn).

**Hạ tầng đã triển khai (qua [Kế hoạch E](p8-harness-evolution-plan.md), thay cho giao diện dự kiến ban đầu):**

- `bint experience digest <batch|run>` dựng bản tóm tắt thất bại xác định; `bint evolve scan` tự tìm cơ hội `failure_lesson` từ các lượt trượt trên validation và lượt thật có nhãn thất bại (theo ngưỡng `evolution.trigger.min_similar_runs` tác vụ phân biệt, không cần cờ `scan --from-eval`).
- Mô hình rút bài học tách riêng qua `roles.reflector: claude` và `harnesses.claude.models.reflector: opus` (hoặc ghi đè bằng `bint evolve propose --reflector-model opus`), nên `bint evolve cycle --model haiku --budget 25 --max 3` tự động dùng Opus cho reflector và Haiku cho thực thi/đối chiếu (không cần cờ `--propose-model`).
- Kiểm tra rò rỉ (`CheckLesson`), loại tác vụ nguồn khỏi tập đối chiếu và kiểm thử đầu-cuối với agent giả đã hoàn tất.

**Đo:**

| Bước | Việc | Ước tính chi phí |
|---|---|---|
| B0 | Đo `none` với Haiku trên validation (70 tác vụ, 1 lần lặp) để biết mức nền | ~$5 |
| — | **Cổng:** tỷ lệ đạt của Haiku trong khoảng 15–60%. Ngoài khoảng đó thì đổi executor (Sonnet) hoặc dừng | — |
| B1 | Đo `bint` với Haiku trên validation, cùng đợt với B0, để lấy log lượt trượt | ~$5 |
| B2 | Với `harnesses.claude.models.reflector: opus`: chạy `bint evolve cycle --model haiku --budget 25 --max 3` để rút tối đa 3 đề xuất từ lượt trượt và đối chiếu trên validation rồi test (có dừng sớm) | ≤ $25 |
| B3 | Nếu có đề xuất đạt trên test: chạy lại trên test với 2 lần lặp để xác nhận | ~$5–10 |

Trần ngân sách: $40.

**Cổng quyết định:**

- **Có đề xuất đạt độc lập trên test và B3 xác nhận:** nghiệm thu P8. Ghi báo cáo. Sau đó thử chuyển cải tiến sang Sonnet để xem có còn tác dụng với model mạnh hơn không.
- **Không đề xuất nào đạt sau 3 đề xuất:** dừng. Ghi kết luận rằng ngay cả cấu hình mạnh dạy yếu cũng không cải thiện được trên loại tác vụ này với cỡ mẫu này.
- **Bất kỳ lúc nào gần chạm trần:** dừng và hỏi người dùng.

**Rủi ro:**

- Haiku có thể quá yếu, đạt dưới 15%, khiến mọi đề xuất trông giống nhiễu. Cổng sau B0 xử lý trường hợp này.
- Bài học từ log thất bại dễ quá cụ thể, thành ra học thuộc lời giải. Kiểm tra rò rỉ và việc tập test độc lập xử lý trường hợp này.
- Cải tiến đạt với Haiku có thể không còn tác dụng với model mạnh hơn. Kết quả này vẫn có giá trị nếu người dùng chấp nhận chạy model rẻ.

---

## Thứ tự đề xuất

1. Mã cho B đã có qua Kế hoạch E; nếu chạy cả A thì viết thêm lệnh `bint eval snapshot import` (không tốn token).
2. Chạy B0 ($5). Đây là bước rẻ nhất và cho biết ngay hướng B có khả thi không.
3. Song song với bước 2, người dùng viết tri thức cho A tại commit gốc B0.
4. Chạy A và B2 theo cổng của từng kế hoạch.

Tổng trần cho cả hai kế hoạch: $60. Mỗi lần chạy có tốn tiền đều cần người dùng duyệt trước.

## Ràng buộc chung

- Không push lên remote.
- Báo cáo kết quả chỉ nêu số liệu benchmark, không nêu tên hay thông tin repository đo.
- Tác vụ validation/test hiện do agent duyệt (người dùng cho phép). Báo cáo phải ghi rõ điều này.
