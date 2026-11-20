# Benchmark Bint so với không dùng Bint — tổng hợp năm đợt chạy (2026-11-16 → 2026-11-17)

Tài liệu này gom kết quả và so sánh của cả năm đợt. Chi tiết từng đợt ở biên bản riêng (liên kết bên dưới); các thay đổi mã của Bint giữa
các đợt ở [bench-bint-vs-plain-thay-doi.md](bench-bint-vs-plain-thay-doi.md). Quy tắc đọc kết quả là bảng chốt trong kế hoạch của từng đợt
(chốt trước khi chạy, không đổi sau khi thấy số).

| Đợt | Biên bản | Kế hoạch |
|---|---|---|
| 1 (2026-11-16) | [bench-bint-vs-plain-20261116](bench-bint-vs-plain-20261116/README.md) | [plan](../benchmark-bint-vs-plain-plan.md) |
| 2 (2026-11-16) | [bench-bint-vs-plain-v2-20261116](bench-bint-vs-plain-v2-20261116/README.md) | [plan v2](../benchmark-bint-vs-plain-v2-plan.md) |
| 3 (2026-11-17) | [bench-bint-vs-plain-v3-20261117](bench-bint-vs-plain-v3-20261117/README.md) | [plan v3](../benchmark-bint-vs-plain-v3-plan.md) |
| 4 (2026-11-17) | [bench-bint-vs-plain-v4-20261117](bench-bint-vs-plain-v4-20261117/README.md) | plan v3 (chỉ chạy lại B) |
| 5 (2026-11-17) | [bench-bint-vs-plain-v5-20261117](bench-bint-vs-plain-v5-20261117/README.md) | plan v3 (chỉ chạy lại B) |

A là Codex (`gpt-6.1-sol`) làm trực tiếp từ prompt của người dùng; B là cùng model chạy qua `bint run b-do` (b-req → b-feat → b-review).
Đợt 1 dùng hai task khác (trung/lớn, người dùng rành kỹ thuật). Từ đợt 2 dùng người dùng mơ hồ, hai task: **T1** "dọn dữ liệu cũ của bint",
**T2** "không để mật khẩu/token bị lưu trong log". Đợt 4 và 5 chỉ chạy lại B; A giữ nguyên diff của đợt 3 (commit gốc `8757340`).

## 1. Kết luận từng đợt

| Đợt | Kết luận theo bảng chốt | T1: A → B (critical/medium) | T2: A → B (critical/medium) | Token vào B/A | Thời gian |
|---|---|---|---|---|---|
| 1 | Không có tín hiệu | (task khác: test ẩn 12/12 vs 12/12; 11/12 vs 10/12) | — | +51 % và +48 % | B +90 % và +55 % |
| 2 | Có lợi, đánh đổi | 0C/3M → 0C/2M (Hoà) | 6C/3M → 5C/5M (**B hơn**, 1 critical) | 2,1–2,5× | B 3,5–4,7× |
| 3 | **Bint bất lợi** | 0C/1M → 0C/3M (A hơn) | 5C/2M → 6C/7M (A hơn) | **11,3×** | cả hai lượt B chạm trần 90 phút |
| 4 | **Bint bất lợi** (sát ngưỡng) | 0C/2M → 0C/4M (A hơn) | 7C/1M → 8C/2M (A hơn) | **2,51×** | B-T1 88,5 phút xong; B-T2 chạm trần |
| 5 | **Bint bất lợi** | 0C/3M → 0C/2M (**Hoà**) | 7C/1M → 10C/6M (A hơn) | **4,94×** | B-T1 77 phút xong; B-T2 chạm trần |

Ngưỡng chi phí "tốn kém": 2× (đợt 2), 2,5× (đợt 3–5). Từ đợt 3, B cần kết quả tốt hơn A (ít critical hơn, hoặc bằng critical và ít hơn ≥ 2 medium) mới được tính là hơn.

## 2. Điều đáng tin và điều không

**Đáng tin (không phụ thuộc cách đếm):**
- Test ẩn của B và A **giống hệt nhau** ở cả T1 (cùng hụt D6 và D8i) lẫn T2 (cùng S1–S5 critical, S6 medium) ở đợt 4 và 5. Không đợt nào cho thấy B che được nhiều nơi lưu hơn A ở mức đo bằng test ẩn.
- B tốn nhiều token và thời gian hơn A trong mọi đợt: 2,1× → 11,3× → 2,51× → 4,94×. Giá trị này dao động mạnh theo kế hoạch của b-req (kế hoạch gọn: rẻ; kế hoạch phủ hết 8 nhóm bề mặt: đắt).
- Cả hai bên không đợt nào đạt mục tiêu của T2 (không để bí mật ở bất kỳ nơi lưu nào).

**Không đáng tin:**
- Mỗi ô một lượt. Chênh 1 critical hoặc 1 medium nằm trong dao động của phép đo.
- **Cùng một diff của A được ba reviewer chấm khác nhau**: A-T1 là 0C/1M (đợt 3), 0C/2M (đợt 4), 0C/3M (đợt 5); A-T2 là 5C (đợt 3), 7C (đợt 4), 7C (đợt 5). Độ hạt đếm cũng đổi (từ đợt 4 mỗi nơi lưu lộ tính một phát hiện),
  nên số tuyệt đối không so được giữa các đợt; chỉ so được A với B trong cùng một lần chấm.
- B-T2 bị cắt bởi trần 90 phút ở đợt 3, 4 và 5, giữa vòng sửa. Chưa từng đo B với thời gian dài hơn.

## 3. So sánh B qua các đợt

| | Đợt 2 | Đợt 3 | Đợt 4 | Đợt 5 |
|---|---|---|---|---|
| B-T1 (critical/medium) | 0/2 | 0/3 | 0/4 | 0/2 |
| B-T2 (critical/medium) | 5/5 | 6/7 | 8/2 | 10/6 |
| Token vào (cả hai task) | ~2,1–2,5× A | 165,7M (11,3×) | 36,95M (2,51×) | 72,60M (4,94×) |
| Luồng của b-req | plan nhỏ | plan phình ("mỗi bề mặt một dòng") | plan gọn (11 KB), chỉ làm G1 ở T2 | plan lớn 15/34 tệp, phủ hết bề mặt |
| N2 hiệu năng (T2, log 20 MB) | — | 13,4 s (gốc ~1 s) | 1,1–1,5 s | **14,4 s** (gốc 0,9 s) |
| Kết thúc | — | cả hai bị cắt vòng sửa 3 | T1 xong; T2 cắt (tự dừng "không hội tụ" rồi hết giờ) | T1 xong; T2 cắt, chưa từng xanh |
| Người dùng giả chạm | — | — | T1: 1 tin + `--fix-round`; T2: 1 tin + `--fix-round` | T1: 1 tin + cổng kế hoạch lớn + `--fix-round`; T2: 3 tin + cổng kế hoạch lớn |

Nhận xét:
- Đợt 4 giảm chi phí chủ yếu nhờ b-req gọn và không còn agent con tự sinh, nhưng T2 chỉ làm giai đoạn G1 (plan tự chia G1–G4).
- Đợt 5 sửa đúng lỗi đó (kế hoạch phủ cả 8 nhóm bề mặt, không giai đoạn hoãn) nhưng **kết quả xấu hơn**: một b-feat 47 phút trên 34 tệp, kiểm chứng đỏ, 6 phát hiện high ở review vòng 1,
  rồi hết giờ. Mã của B phủ nhiều nơi lưu hơn A nhưng vẫn lộ 7 nơi và gây thêm 3 lỗi hỏng dữ liệu/việc chạy (mục tri thức mất thân, lệnh kiểm chứng `TOKEN=$CI_TOKEN` thành `TOKEN=***`, ghi đè credential trong profile iso).
- T1: B an toàn hơn A ở hầu hết đợt (xem trước mặc định, không đụng phiếu dở), nhưng gần như không lấy lại được dung lượng người dùng muốn; cả hai bên cùng bỏ sót log phiếu đã xong.

## 4. Vì sao Bint vẫn thua một prompt đơn giản (giả thuyết từ số liệu)

1. **Cùng một model viết mã.** Các bước thêm của Bint (b-req, cổng, review, vòng sửa) chỉ có ích nếu bắt được lỗi trước hạn; không làm model viết đúng hơn.
2. **Phạm vi rộng hơn → nhiều chỗ sai hơn.** Ở T2 số chỗ còn lộ bí mật của A và B bằng nhau (7); B thua vì gây thêm lỗi hỏng dữ liệu khi che rộng.
3. **Kiểm tra mục tiêu do chính agent viết** nên cùng điểm mù với mã (PEM, `aws_secret_access_key`, `private_key` vẫn lọt). Chưa kiểm trực tiếp trên transcript.
4. **Review thấy lỗi nhưng không đủ giờ sửa.** Trần 90 phút cắt trước khi kiểm chứng lại.
5. **Cổng kế hoạch lớn chỉ báo, không chia nhỏ việc**; b-feat vẫn là một phiên khổng lồ.
6. **Phép đo chỉ chấm kết quả cuối**: lợi ích của quy trình (plan, tiêu chí chấp nhận, review, resume) không được tính.

## 5. Chưa đo / việc còn mở
- Đường nhanh opt-in (`bint run b-do --fast`, `/b-do-fast`) chưa được đo (đợt 5 chạy với `fast_path` tắt).
- Nối tiếp phiên b-req → b-feat khi khác model (Claude và Codex) mới kiểm bằng tay; benchmark chạy cùng model.
- Chạy ≥ 2 lượt mỗi ô, và một cấu hình không trần 90 phút để biết B-T2 có tự xanh được không.
- Đề xuất cải tiến: chia b-feat theo nhóm bề mặt kèm test riêng; Bint tự chạy kiểm tra mục tiêu dạng hộp đen (cài bí mật thật vào mọi đường nhập rồi quét) thay vì dựa vào test do agent viết;
  đặt trần token cho b-feat đầu; làm phiếu riêng sửa `detect.Redact` mới (các lỗi cụ thể ở `bench-bint-vs-plain-v5-20261117/review/T2-verified.json`).

Đợt 6 (2026-11-20, Windows, `gpt-6.2-sol`, task dài L1 qua tối đa 4 phiên × 30 phút có cắt cứng; [plan v6](../benchmark-bint-vs-plain-v6-plan.md),
[biên bản](bench-bint-vs-plain-v6-20261120/README.md)): **Hoà** theo bảng chốt. Ma trận loại × nơi lưu A 22/36, B 20/36; cả hai trượt N2
(A 20,1×, B 5,5×); B thêm 3 critical do che quá rộng và `go test` đỏ thật; B/A = 2,41× token vào. Lượt đầu bị huỷ vì repo còn tài liệu
benchmark cũ (agent đọc làm đáp án).

## 6. Ghi chú về dữ liệu trong thư mục này
Các biên bản đã bỏ: mã test ẩn (`hidden/`), công cụ chạy (`tools/`), bản ghi thô của agent (`*.jsonl`) và tệp > 400 KB. Các chuỗi giống token/khoá trong kết quả
là mẫu giả của test ẩn và đã được thay bằng `<FIXTURE>`. Đường dẫn máy (`/home/…`) giữ nguyên như biên bản gốc.
