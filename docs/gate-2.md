# Cổng quyết định 2 — Context Engine

**Trạng thái: ĐÃ CHỐT (2026-11-10) — cổng chính thức chưa mở; đã có pilot Codex và đợt đo Claude Code để thăm dò kỹ thuật.**

> Tài liệu này là **phụ lục giao thức đã chốt** (ngưỡng, điều kiện, quy trình). Trạng thái hiện tại và kế hoạch
> chạy cổng nằm ở [acceptance.md](acceptance.md) (mã `NT-GATE-2`, vòng V6).

Theo mục 16 và 19 của kế hoạch, ngưỡng dưới đây được chốt và commit trước khi chạy
đo (SHA ở mục "Chốt" cuối tài liệu). Không điều chỉnh ngưỡng, thước đo, điều kiện hay
quy trình sau khi đã thấy kết quả; mọi thay đổi phải là một lần chốt mới, ghi rõ lý do,
và chỉ áp dụng cho các lần đo bắt đầu sau đó.

Điều kiện tiên quyết: cổng quyết định 1 ([gate-1.md](gate-1.md)) đã qua. Nếu cổng 1
không qua và hướng sinh tri thức tự động bị dừng, cổng 2 đo trên tri thức do người viết.

## Câu hỏi

Đưa tri thức tới agent qua Context Engine (truy xuất theo tác vụ qua MCP, chèn theo
đường dẫn qua hook) có tốt hơn chỉ đặt tri thức đó vào AGENTS.md ngắn không?

| Nhánh | Nội dung |
|---|---|
| `minimal` | AGENTS.md dựng từ tri thức tại commit gốc (như cổng 1) |
| `bint` | **cùng** AGENTS.md + MCP server `bint` + hook PreToolUse chèn theo đường dẫn, cùng tập tri thức |
| `ideal` | chỉ để chẩn đoán (mục 12.9): AGENTS.md + toàn bộ mục có phạm vi chồng lên tệp của lời giải thật. Cận trên, không bao giờ báo cáo như kết quả của Bint |

Hai nhánh `minimal` và `bint` dùng đúng một tập tri thức; khác biệt duy nhất là cách
đưa tri thức tới agent.

## Ngưỡng

Chỉ tính trên tác vụ tập **test**, chế độ `strict`, so theo cặp `bint − minimal`.

1. **Thước đo chính (chọn trước): tỷ lệ đạt.** Cận dưới CI95 của chênh lệch tỷ lệ đạt
   theo cặp > **0** (Bint đầy đủ phải *tốt hơn*, không chỉ không kém).
2. **Không đổi chi phí lấy chất lượng quá mức:** chênh lệch chi phí trung bình mỗi tác vụ
   ≤ **+25 %** so với `minimal`.

Cả 1 và 2 đạt → qua cổng.

> Đã cân nhắc và **không** chọn: lấy chi phí/số lượt làm thước đo chính (giảm ≥ 10 %,
> cận trên CI95 < 0) kèm điều kiện không kém về tỷ lệ đạt. Nếu tỷ lệ đạt của `minimal`
> gần trần khiến cổng không phân biệt được, phải chốt lại thành một lần chốt mới trước
> khi đo tiếp, không đổi thước đo sau khi đã thấy kết quả.

## Điều kiện để phép đo được tính

Giống cổng 1: ≥ 2 repository thử nghiệm khác hệ sinh thái, mỗi repository ≥ 20 tác vụ
test đã kiểm chứng fail→pass, 3 lần lặp, cùng harness/mô hình trong một đợt, phần
"Độ tin cậy của phép đo" không có cảnh báo chặn, lượt `bint` bị `skipped` ≤ 5 %.
Thêm: hook và MCP thực sự hoạt động — trên ≥ 80 % lượt `bint` có ít nhất một lần truy
xuất được ghi (bảng `eval_context_usage`).

## Quy trình

```sh
bint eval bootstrap --split test --budget <USD> --yes
bint eval run --split test --arms minimal,bint --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate2-<repo>.json
```

## Khi không qua (mục 12.9)

1. Chẩn đoán trên tập **validation** với cả ba nhánh:
   `bint eval run --split validation --arms minimal,bint,ideal ...`, rồi
   `bint eval report` — phần "Chẩn đoán cổng quyết định 2" xét theo thứ tự: đo lường →
   nội dung tri thức (`ideal` không hơn `minimal`) → truy xuất bỏ sót (độ phủ tập đúng
   < 50 %) → không được áp dụng (mục đúng ít được trích dẫn ở lượt thất bại) → nhiễu.
2. Lỗi truy xuất hoặc trình bày: sửa rồi đo lại trên tác vụ test **chưa dùng** để chẩn đoán.
3. Nguyên nhân là nội dung tri thức, hoặc sau **2 vòng** vẫn không qua: thu hẹp Context
   Engine về chế độ chèn theo đường dẫn và xem xét lại các giai đoạn tiến hoá.

## Giới hạn đã biết của phép đo ở P3

- Ngưỡng độ phủ (50 %) và tỷ lệ trích dẫn (50 %) trong chẩn đoán là ngưỡng định hướng,
  chưa hiệu chỉnh từ dữ liệu.
- Tập "đúng" xác định theo phạm vi đường dẫn chồng lên tệp nguồn của lời giải; mục
  toàn cục (đã nằm trong AGENTS.md ở mọi nhánh) không thuộc tập đúng.
- Trích dẫn phụ thuộc việc agent gọi `cite_knowledge`; agent không gọi thì "không được
  áp dụng" có thể bị ước lượng cao.
- Fingerprint theo ký hiệu dùng `go/parser` cho Go và heuristic cho ngôn ngữ khác
  (không dùng tree-sitter vì cần CGO).

## Chốt

| Mục | Giá trị |
|---|---|
| Ngưỡng được chốt bởi | chủ dự án Bint, 2026-11-10 |
| Thước đo chính đã chọn | tỷ lệ đạt (`bint − minimal`, cận dưới CI95 > 0); ràng buộc chi phí ≤ +25 % |
| Commit chốt | `9135dfa5528b2d42e62adbda75b318ede819a6ec` |
| Repository đáp ứng đầy đủ giao thức nghiệm thu | _(chưa có; các pilot ghi bên dưới)_ |

## Kết quả

Pilot Codex thật trên repository A (TypeScript) ngày 2026-11-10 được ghi tại
[báo cáo và artifact](evals/pilot-codex-2026-11-10/README.md).
Nhánh `bint` trong pilot dùng cùng snapshot strict với `minimal`, qua MCP + AGENTS.md;
đây là thăm dò kỹ thuật, chưa phải phép đo đủ điều kiện mở gate 2.
Đợt so sánh đã hoàn tất: bint và minimal cùng đạt 3/6 lượt (50%);
6/6 lượt bint có truy xuất, 4 lời gọi cite bị chặn quyền, số trích dẫn ghi nhận bằng 0.

**Quyết định hiện tại: chưa mở gate chính thức.** Gate 1 chưa được nghiệm thu;
pilot này chỉ có một repository và hai tác vụ validation, Codex không báo chi phí USD
để kiểm tra ràng buộc +25%, và tại thời điểm chạy pilot nhánh đánh giá Codex chưa có path hook
(bản sửa sau đó đã bổ sung path hook và quyền `cite_knowledge`, kiểm chứng tại
[đợt Codex 2026-11-11](evals/codex-flows-2026-11-11/README.md)).

Quyết định về sai khác fingerprint và tín hiệu còn thiếu:
[fingerprint-outcome-decisions.md](fingerprint-outcome-decisions.md).
