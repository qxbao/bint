# Cổng quyết định 1 — Bootstrap tối giản

**Trạng thái: ĐÃ CHỐT (2026-11-10) — chưa chạy đo.**
Theo mục 16 và 19 của kế hoạch, ngưỡng dưới đây được chốt và commit trước khi chạy
đo (SHA ở mục "Chốt" cuối tài liệu). Không điều chỉnh ngưỡng, điều kiện hay quy trình
sau khi đã thấy kết quả; mọi thay đổi phải là một lần chốt mới, ghi rõ lý do, và chỉ
áp dụng cho các lần đo bắt đầu sau đó.

## Câu hỏi

Tri thức do bootstrap tự động sinh (quy tắc bất thường, điểm dễ sai), đặt vào
AGENTS.md ngắn, có giúp agent hoàn thành đúng tác vụ với chi phí thấp hơn so với để
agent tự khám phá không?

| Nhánh | Nội dung |
|---|---|
| `none` | chỉ harness, mọi tệp ngữ cảnh agent bị gỡ |
| `minimal` | AGENTS.md dựng từ tri thức do `b-bootstrap` sinh **tại commit gốc của từng tác vụ** (`bint eval bootstrap`, chế độ `strict`) |

Nhánh `minimal` đo đầu ra **tự động** của bootstrap: các mục chưa qua người duyệt
(quy tắc chưa duyệt được trình bày như tham khảo). Không dùng tri thức hiện tại của
repository, vì nó đã chứa lời giải của các commit cũ (mục 12.3).

## Ngưỡng

Chỉ tính trên tác vụ thuộc tập **test** (`--split test`), chế độ `strict`.

1. **Không kém về tỷ lệ đạt** — cận dưới CI95 của chênh lệch tỷ lệ đạt theo cặp
   (`minimal − none`) ≥ **−0,05**.
2. **Giảm rõ rệt chi phí hoặc thao tác** — ít nhất một trong hai:
   - chi phí trung bình mỗi tác vụ: chênh lệch theo cặp ≤ **−10 %** so với `none`
     và cận trên CI95 < 0;
   - số lượt agent (`num_turns`): chênh lệch theo cặp ≤ **−10 %** và cận trên CI95 < 0.

Cả 1 và 2 đạt → qua cổng.

## Điều kiện để phép đo được tính

- Ít nhất **2 repository thử nghiệm** khác hệ sinh thái (mục 17 đề xuất Go backend,
  TypeScript backend, frontend, monorepo), mỗi repository ≥ **20 tác vụ test** đã kiểm
  chứng fail→pass; mỗi tác vụ × nhánh × **3 lần lặp**. Cỡ mẫu chốt theo
  `bint eval estimate --effect 0.1` với độ lệch chuẩn đo ở đợt `none` đầu tiên.
- Cùng harness, cùng phiên bản và mô hình cho cả hai nhánh trong một đợt; khuyến nghị
  `--bare` để không nạp bộ nhớ cấp người dùng.
- Phần "Độ tin cậy của phép đo" trong `bint eval report` không có cảnh báo chặn: lỗi
  hạ tầng ≤ 10 %, tác vụ không bị sửa giữa chừng, không có lượt `economy`.
- Tỷ lệ đạt của `none` nằm trong khoảng 0,2–0,8 (tránh trần/sàn).
- Lượt `minimal` bị `skipped` (thiếu ảnh chụp) không được vượt 5 %.

## Quy trình

```sh
# Trên mỗi repository thử nghiệm
bint init --no-hooks
# xác nhận eval.test_command (và setup_command) trong .bint/local.yml
bint eval mine --verify && bint eval approve <id>...       # duyệt tác vụ, gán split
bint eval bootstrap --split test --budget <USD> --yes      # tri thức tại commit gốc
bint eval estimate --split test --arms none,minimal --effect 0.1
bint eval run --split test --arms none,minimal --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate1-<repo>.json
```

## Khi không qua (mục 12.9)

1. Chẩn đoán chỉ trên tập **validation**, theo thứ tự của bảng 12.9.
2. Điều chỉnh (skill `b-bootstrap`, cách trình bày, giới hạn số mục), rồi đo lại trên
   tác vụ test **chưa dùng** để chẩn đoán.
3. Tối đa **2 vòng**. Sau 2 vòng vẫn không qua: dừng hướng sinh tri thức tự động, chỉ
   giữ tri thức do người viết (`bint knowledge add`).

## Giới hạn đã biết của phép đo ở P2

- "Thao tác khám phá" được xấp xỉ bằng `num_turns`; chưa đếm riêng số lần đọc/tìm tệp.
- Nhánh `minimal` dựng từ ảnh chụp bootstrap trình bày quy tắc chưa duyệt như tham
  khảo; nhánh có quy tắc được người duyệt (mức 2) không đo được trên tác vụ lịch sử
  với chi phí hợp lý.
- Mạng không bị chặn ở tầng hệ điều hành trong các lượt chạy (giới hạn của P1).

## Chốt

| Mục | Giá trị |
|---|---|
| Ngưỡng được chốt bởi | orca <dquanbao@gmail.com>, 2026-11-10 |
| Commit chốt | _(SHA của commit thêm tài liệu này ở trạng thái đã chốt — điền ở commit kế tiếp)_ |
| Repository thử nghiệm | _(chưa)_ |

## Kết quả

_(chưa chạy)_
