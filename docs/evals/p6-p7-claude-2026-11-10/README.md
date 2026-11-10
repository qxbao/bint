# Số liệu P6–P7 (Claude Code, Sonnet) — chuẩn bị quyết định mở P8

**Ngày:** 2026-11-10. **Bint:** `dff6570` cộng bản sửa đặc tả đề bài (chưa commit lúc đo). **Harness:** Claude Code 2.1.322, mô hình `claude-sonnet-5-5`. **Ngân sách đã duyệt:** $25. **Đã tiêu:** ~$14,3 (USD do harness báo).

**Kết luận:** chưa đạt nghiệm thu P6 và P7; **chưa mở P8**. Có hai nút thắt. Thứ nhất, tỷ lệ đạt nền trên tác vụ thật gần 0 do đề bài thiếu đặc tả; đã có bản sửa và bản sửa cho thấy hiệu quả. Thứ hai, chi phí Probe ở cỡ mẫu cần thiết rất lớn.

## Repository thử nghiệm

| | Repository B | Repository C |
|---|---|---|
| Loại | Monorepo Python (pytest), ~2.000 commit không phải merge | Repository nhỏ, lịch sử tác vụ **tổng hợp** (22 tác vụ viết tay) |
| Ứng viên `eval mine` | 892 (711 tập trung ở một khu vực mã) | — |
| Tỷ lệ kiểm chứng fail→pass | 17/19 ứng viên Python ở mẫu ngẫu nhiên; 75/80 ở mẫu khu vực tập trung | 22/22 |
| Tác vụ đã duyệt | 92 (build 39 / validation 25 / test 28) | 22 (build 13 / validation 3 / test 6) |

Repository C chỉ dùng để đo chi phí từng pha và tìm lỗi cơ chế. Kết quả hiệu quả trên C không có giá trị đại diện.

## Chi phí theo pha (mục 12.8)

| Pha | Repository C | Repository B |
|---|---|---|
| Một lượt thực thi tác vụ | $0,07, ~16 s | **$0,30**, 1–2 phút (tối đa 11 phút) |
| Quét cơ hội (`evolve scan`) | $0, 0,05 s | $0, < 1 s |
| Tạo một đề xuất (agent evolver) | $0,06, 15–19 s | $0,15–0,17, 21–22 s |
| Một lượt đối chiếu (Probe) | $0,06 | $0,28–0,44 |
| Kiểm chứng fail→pass | 0 USD, ~5 s/tác vụ | 0 USD, ~15 s/tác vụ |

Ngoại suy ở cỡ mẫu Bint tự tính (cần ~197 tác vụ để phát hiện +10 điểm %; độ lệch chuẩn 0,5 là giả định thận trọng vì chưa đủ dữ liệu đo), với 2 lần lặp:

| | Một lần Probe | Một vòng Propose–Probe–Compose (~3 Probe) |
|---|---:|---:|
| Repository C ($0,06/lượt) | ~$47 | ~$140 |
| Repository B ($0,30/lượt) | **~$240** | **~$700** |

Chi phí tạo đề xuất chỉ chiếm dưới 0,1 % chi phí Probe; gần như toàn bộ nằm ở Probe. Nếu hạ mục tiêu xuống +20 điểm % thì chỉ cần ~50 tác vụ, và chi phí còn khoảng 1/4.

## Kết quả trên repository B

- **Lịch sử có nhãn:** 7 lượt phát lại tác vụ build. Nhãn được gán theo test ẩn qua `bint runs rate`, vì hook không tự gắn được nhãn mạnh (F5). Kết quả: **0/7 đạt**.
- **Cơ hội:** 2 cơ hội L1 (`exploration`, `failure_hotspot`) trên cùng một tệp điểm nóng. Không có cơ hội L2: `skill_candidate` cần ≥ 3 lượt thành công.
- **Phễu tác vụ đối chiếu hồi cứu:**

  | Đề xuất | Validation | Test |
  |---|---:|---:|
  | Đề xuất 1 (warning) | 6/25 | 5/28 |
  | Đề xuất 2 (pattern) | 1/25 | 1/28 |

  Mức này tốt hơn hẳn repository C, nơi các đề xuất có 0/6 tác vụ hồi cứu.
- **Probe:** dừng sớm để giữ ngân sách. Đã chạy 5 lượt: base 0/3, candidate 0/2.

### Vì sao tỷ lệ đạt bằng 0

Các lượt trượt đều qua phần lớn test (5/6, 22/23, 89/90, 84/87) và chỉ hỏng 1–3 test. Nguyên nhân nằm ở đề bài:

- Đề bài là tiêu đề commit, trung vị 124 ký tự.
- Ở **42 % tác vụ (39/92)**, test ẩn gọi hàm, lớp hoặc hằng mới không tồn tại tại commit gốc và không được nêu trong đề bài. Agent không thể đoán đúng tên.
- Phần còn lại trượt vì test so khớp nguyên văn câu chữ hoặc kiểu trả về.

Khi mọi nhánh cùng 0 %, không đề xuất tri thức hay skill nào đo được tác dụng.

### Bản sửa: đặc tả giao diện trong đề bài

`bint eval mine` và lệnh mới `bint eval spec` nối vào đề bài **chữ ký** (không có thân hàm, không có giá trị hằng) của những định danh vừa mới trong lời giải vừa được test dùng tới. Lệnh cũng cảnh báo `interface_gap`, `exact_text` và `short_prompt`. Trên 92 tác vụ: 42 tác vụ được nối chữ ký, 39 tác vụ không còn cảnh báo nào.

Kiểm tra nhanh trên 3 tác vụ có chữ ký và không còn cảnh báo (nhánh `none`, 2 lần lặp, $1,86): **2/6 lượt đạt**, trước đó 0/12 trên các tác vụ cùng loại. Tác vụ từng trượt vì `AttributeError` giờ chạy tới phần kiểm hành vi; nó vẫn trượt, nhưng là trượt thật về hành vi. Mẫu này rất nhỏ: nó chỉ cho thấy tác vụ đã trở nên giải được, chưa phải ước lượng tỷ lệ đạt nền.

## Đo lại tỷ lệ đạt nền sau bản sửa (bước 3)

Bint `04d6b2c`, gồm bản sửa đặc tả và các bản sửa F2/F4, F5, F6, F10, F13.
- **Cấu hình:** nhánh `none`, 1 lần lặp, Sonnet.
- **Tập đo:** 30 tác vụ build và validation của repository B. Trong đó 24 tác vụ không còn cảnh báo đặc tả và 6 tác vụ chỉ có cảnh báo `short_prompt`. Tập test không dùng tới.

| | Giá trị |
|---|---|
| Tỷ lệ đạt | **10/30 = 33 %** (Wilson CI95 ~19–51 %) |
| Chi phí | $6,05; **$0,20/lượt** |
| Thời gian | trung vị ~40 s/lượt; một lượt dài 17,7 phút (agent tự chạy bộ test lớn) |
| Lỗi hạ tầng | 0 |
| Bộ đệm `eval-work` sau đợt | 2,5 MB (trước bản sửa F10: 4,8 GB với 80 tác vụ) |

Phân loại 20 lượt trượt, đọc từ log test (không tốn thêm chi phí):
- **~11 lượt sai hành vi thật:** bỏ sót trường hợp biên hoặc sai logic. Đây là phần tri thức L1 và skill L2 có thể giúp.
- **~9 lượt trượt vì "hợp đồng ẩn" mà đặc tả chưa bắt được:**
  - chuỗi so khớp nguyên văn không có khoảng trắng (`exact_text` bỏ sót);
  - định dạng đối số dòng lệnh;
  - khoá trong tệp cấu hình YAML;
  - thuộc tính module do test thay thế (`interface_gap` bỏ sót vì định danh không nằm trong mã).

  Nếu loại nhóm này, tỷ lệ đạt trên tác vụ thực sự đủ đặc tả là khoảng 10/21, tức gần 50 %.

## Khả thi của P8 trên lý thuyết

**Mô hình:** đối chiếu ghép cặp theo tác vụ, R lần lặp mỗi nhánh.
- Phương sai của chênh lệch theo tác vụ: σ² = σ_b² + 2p(1−p)/R, với p ≈ 0,4 và σ_b là độ lệch giữa các tác vụ (giả định 0,15–0,25).
- Cỡ mẫu: n = (2,8·σ/δ)², ứng với α = 0,05 và power 0,8.
- Đơn giá: $0,20 mỗi lượt.

| Mức cải thiện δ | R | Tác vụ cần | Chi phí một Probe |
|---:|---:|---:|---:|
| +10 điểm % | 2 | 206–238 | $165–190 |
| +10 điểm % | 8 | 65–97 | $210–310 |
| +15 điểm % | 4 | 50–64 | $80–100 |
| +15 điểm % | 8 | 29–43 | $95–140 |
| +20 điểm % | 4 | 28–36 | $45–58 |
| +20 điểm % | 8 | 17–25 | $54–80 |

**Nút thắt là nguồn tác vụ, không phải tiền.** Ước tính nguồn tác vụ của repository B:
- 892 ứng viên × ~90 % kiểm chứng được × ~45 % đủ đặc tả ≈ 360 tác vụ dùng được.
- Tập validation chiếm 25 %, tức ≈ 90 tác vụ.
- Sau lọc phạm vi và bằng chứng của từng đề xuất (đo được 24 % trước bản sửa F2/F4; ước 40–50 % sau bản sửa), mỗi đề xuất còn **khoảng 20–45 tác vụ**.

Với nguồn tác vụ đó:
- **+10 điểm %:** không khả thi ở quy mô một repository, vì thiếu tác vụ ở bất kỳ R nào.
- **+15 đến +20 điểm %:** khả thi khi tăng số lần lặp (R = 4–8). Mỗi Probe tốn ~$50–140, mỗi vòng Propose–Probe–Compose (~3 Probe) ~$150–400.
- **Mức cải thiện thực tế:** một mục tri thức ít khi nâng +15 điểm % trên toàn tập. Nhưng Probe chỉ tính tác vụ trong phạm vi của đề xuất, nên trong phạm vi hẹp, mức đó có thể xảy ra.

**Kết luận:** P8 khả thi có điều kiện, nhưng **chưa sẵn sàng**. Cần:
1. Thiết kế Probe theo hướng ít tác vụ, nhiều lần lặp, cho phép dừng sớm theo phương pháp tuần tự. Mục tiêu nhắm δ ≥ 15 điểm % trong phạm vi đề xuất. Mức +10 điểm % chỉ dùng làm ngưỡng không kém (non-inferiority), không làm ngưỡng chấp nhận.
2. Đo σ_b thật bằng các lần lặp bổ sung trên cùng tập tác vụ. σ_b đang là giả định và quyết định toàn bộ bảng trên.
3. Mở rộng phát hiện hợp đồng ẩn (chuỗi ngắn, khoá cấu hình, thuộc tính module), rồi kiểm chứng và duyệt thêm tác vụ để mỗi đề xuất có ≥ 30 tác vụ.
4. Đặt ngân sách mỗi vòng khoảng $150–400 và chấp nhận rằng P8 chỉ phát hiện được cải thiện lớn.

## Phát hiện về Bint

| ID | Mức | Phát hiện |
|---|---|---|
| F1 | Cao | Cơ hội `exploration` và `skill_candidate` chỉ bật khi **cùng một tệp** bị chạm ở ≥ 3 lượt. Quy trình lặp lại trên nhiều tệp anh em không được phát hiện. |
| F2 | Cao | Bằng chứng gắn với ký hiệu cả lớp bị coi là "không khớp" khi lớp có thêm phương thức. Mức này khớp giả thuyết so fingerprint với cây hiện tại, chưa xác nhận bằng mã. |
| F3 | Cao | Quy tắc chống rò rỉ dùng cửa sổ 24 giờ, áp cả ở chế độ `--prospective`. Vòng tiến cứu không chạy được trong ngày. |
| F4 | Cao | Evolver neo bằng chứng vào dòng mã mới nhất. Trên tệp lớn hay đổi, nhiều tác vụ hồi cứu trong phạm vi bị loại vì bằng chứng không khớp tại commit gốc (repository B, đề xuất 1: 6 tác vụ validation và 7 tác vụ test; đề xuất 2: ≥ 10 tác vụ mỗi tập). Repository C: 0 tác vụ hồi cứu còn lại. |
| F5 | Trung bình | Hook chỉ gắn tín hiệu test **mạnh** khi lệnh chứa nguyên văn `eval.test_command`. Lệnh có `{test_files}` không bao giờ khớp, nên lịch sử thật luôn `unlabeled`. |
| F6 | Trung bình | Nhánh đối chiếu Claude không có công cụ `Skill` trong `allowedTools` và không có hook ghi `skill.activated`, nên Probe L2 không đo được skill có thực sự được dùng hay không. |
| F7 | Trung bình | Agent sửa tệp qua Bash thì hook không ghi `file.modified`/`file.read`. |
| F8 | Thấp | Lượt ghi qua hook không có chi phí USD, nên không tính được chi phí khám phá theo nhóm. |
| F10 | Trung bình | Bộ đệm `eval-work/bases` giữ **một kho Git trần cho mỗi commit gốc** (~60 MB/tác vụ trên repository B). 80 tác vụ chiếm 4,8 GB; ~200 tác vụ cần khoảng 12 GB. |
| F11 | Thấp | Kiểm chứng lại một ứng viên đã bị loại thì phần kiểm chứng thành `verified`, nhưng trạng thái vẫn là `rejected` kèm lý do cũ. |
| F12 | Thấp | Nhóm tác vụ theo hai cấp thư mục đầu, quá thô với monorepo: cả repository B dồn vào một nhóm. |
| F13 | Trung bình | Khi chưa có lịch sử chi phí, lượt đầu tiên vẫn chạy dù ngân sách chỉ $0,01 (đã tiêu $0,29). |

## Việc cần làm trước khi mở P8

1. Dùng bản sửa đặc tả, rồi đo lại tỷ lệ đạt nền trên tập validation đủ lớn. Chỉ chọn tác vụ không còn cảnh báo, hoặc viết lại đề bài cho những tác vụ có cảnh báo. Mục tiêu là tỷ lệ đạt nền khoảng 30–70 %.
2. Sửa F2/F4 (neo bằng chứng theo hàm, ưu tiên mã tồn tại tại commit cũ), F5, F6, F10, F13.
3. Đo lại chi phí Probe ở cỡ mẫu dùng được, rồi so với lợi ích kỳ vọng. Với mức $0,30 mỗi lượt, mục tiêu +10 điểm % đòi ~$700 mỗi vòng; cần xem xét hạ mục tiêu, hoặc dùng mô hình rẻ hơn cho Probe sàng lọc.
