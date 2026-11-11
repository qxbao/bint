# Số liệu P6–P7 (Claude Code, Sonnet) — chuẩn bị quyết định mở P8

> **Bản ghi lịch sử:** Báo cáo dưới đây phản ánh số liệu và trạng thái nhánh tại thời điểm chạy đợt đo. Tổng hợp hiện trạng tại `HEAD` xem ở [docs/status.md](../../status.md) và [docs/evals/README.md](../README.md).

**Ngày:** 2026-11-10. **Bint:** `dff6570` (đợt đầu), `04d6b2c`–`2764e8f` (đo lại, đo phương sai, kế hoạch Probe), `20cc037`–`aa66d8c` (bootstrap theo vùng mã, đo lại nhánh bint), `f2fd149`–`276500d` (kênh tri thức, mở P8). **Harness:** Claude Code 2.1.322, mô hình `claude-sonnet-5-5`. **Ngân sách đã duyệt:** $25. **Đã tiêu:** ~$87 (trần nâng từ $25) (USD do harness báo).

**Kết luận:** chưa đạt nghiệm thu P6 và P7. Về lý thuyết P8 **khả thi có điều kiện**. Tỷ lệ đạt nền sau bản sửa đặc tả là 33–40 %. Với thiết kế Probe mới (ít tác vụ, lặp theo kế hoạch, dừng sớm), mức cải thiện +15–20 điểm % phát hiện được với ~30 tác vụ mỗi đề xuất, tốn ≤ $25–60 mỗi lần đối chiếu. Nút thắt còn lại là số tác vụ đã duyệt, cùng một lần Probe thật để đo σ_b (mục cuối).

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

## Đo phương sai (lặp lại trên cùng 30 tác vụ)

Đợt thứ hai (nhánh `none`, cùng 30 tác vụ, $5,60) đạt **12/30**, tương đương đợt đầu (10/30).

Ghép hai lượt theo tác vụ:
- 16 tác vụ trượt cả hai lượt, 8 tác vụ đạt cả hai, 6 tác vụ một đạt một trượt.
- **80 % tác vụ cho cùng kết quả** ở hai lượt.
- Phương sai trong tác vụ đo được: **W = 0,20**. Mức giả định cho biến nhị phân với p ≈ 0,37 là 2p(1−p) = 0,46.

Hệ quả:
1. Tác vụ gần tất định, nên lặp nhiều lần mỗi tác vụ chỉ giảm được phần nhiễu nhỏ.
2. Một đề xuất có tác dụng sẽ chủ yếu "lật" hẳn một phần tác vụ từ trượt sang đạt, nên phương sai giữa tác vụ xấp xỉ δ(1−δ). Giá trị σ_b = 0,15 giả định trước đó là quá lạc quan.

Bộ lập kế hoạch nay lấy sàn σ_b = √(δ(1−δ))·(1 − W/0,5) khi σ_b chưa đo được từ dữ liệu ghép cặp.

## Nguồn tác vụ sau bản mở rộng đặc tả

`bint eval spec` bản mới bắt thêm bốn loại hợp đồng ẩn: chuỗi ngắn đặc thù, cờ dòng lệnh, khoá cấu hình/khoá dữ liệu, và tên bị monkeypatch.

Trên 27 tác vụ đã chạy:
- **Precision 4/4:** không tác vụ nào trong 19 tác vụ trượt vì hành vi hoặc đã đạt bị cảnh báo nhầm.
- **Recall 4/8** với nhóm trượt vì hợp đồng ẩn. Bốn ca còn sót gồm cặp cờ + giá trị đã có ở base, chuỗi nằm ngoài tệp lời giải, và hành vi đệm.

Trên toàn bộ 892 ứng viên:
- **293 tác vụ (33 %) đủ đặc tả**, không còn cảnh báo.
- 92 tác vụ đã duyệt chia theo tập: build 15 sạch + 3 chỉ có `short_prompt`; validation 8 + 3; test 10 + 4.

Ước tính nguồn tác vụ:
- Tối đa: 892 × ~90 % kiểm chứng được × 33 % ≈ 265 tác vụ dùng được, tức ≈ 66 tác vụ mỗi tập validation/test.
- Sau lọc phạm vi và bằng chứng theo đề xuất: **≈ 16–33 tác vụ mỗi đề xuất**.

## So sánh có Bint và không có Bint (30 tác vụ)

**Cấu hình:**
- Tri thức dựng theo chế độ strict: `bint eval bootstrap` tại commit gốc của từng tác vụ. 30 ảnh chụp, mỗi ảnh 4–7 mục chưa qua người duyệt, tốn $6,57.
- Nhánh `minimal` (AGENTS.md dựng từ tri thức) và nhánh `bint` (AGENTS.md + Context Engine), mỗi nhánh 1 lần lặp, tốn $13,16.
- Nhánh `none` dùng lại 2 lần lặp đã đo ở trên.
- Ghép cặp theo tác vụ; CI95 bootstrap theo tác vụ.

| Nhánh | Tỷ lệ đạt | Chi phí TB/lượt | Số lượt hội thoại TB |
|---|---:|---:|---:|
| none | 22/60 = 37 % | $0,194 | 13,3 |
| minimal | 11/30 = 37 % | $0,214 | 12,5 |
| bint | 12/30 = 40 % | $0,225 | 13,9 |

| So sánh | Δ tỷ lệ đạt | Δ chi phí/lượt | Δ lượt hội thoại | Tác vụ tốt hơn / kém hơn |
|---|---|---|---|---|
| minimal − none | +0,0 điểm % [−15, +15] | +$0,020 [−0,006, +0,046] | −0,8 [−3,3, +1,8] | 5 / 5 |
| bint − none | +3,3 điểm % [−12, +18] | **+$0,031 [+0,009, +0,054]** (+16 %) | +0,6 [−1,1, +2,6] | 6 / 4 |

**Đọc kết quả:**
- **Tỷ lệ đạt:** không thấy khác biệt. Khoảng tin cậy ±15 điểm % không loại trừ mức tăng hay giảm nhỏ.
- **Chi phí:** nhánh `bint` đắt hơn khoảng 16 % mỗi lượt, và chênh lệch có ý nghĩa thống kê. Nhánh `minimal` đắt hơn khoảng 10 %, chưa có ý nghĩa.
- **Số lượt hội thoại:** không giảm.
- **Truy xuất:** log truy xuất chỉ ghi hook theo đường dẫn, mỗi lượt tối đa 1 lần chèn (25/30 lượt); không thấy lượt nào gọi công cụ MCP.

**Kết luận:** với tri thức tự sinh chưa qua duyệt, Bint **chưa hiệu quả**. Tỷ lệ đạt ngang chạy thường, còn chi phí cao hơn khoảng 16 %. Hiện chưa thấy bằng chứng Bint làm hỏng việc, nhưng nó đang tốn thêm mà chưa đem lại lợi ích đo được.

### Đo lại sau khi sửa nội dung tri thức

**Chẩn đoán:** tri thức bootstrap ban đầu chủ yếu nói về CI, build và phát hành, không liên quan tới các tác vụ sửa mã runtime. Hướng dẫn trong AGENTS.md cũng không nêu tên đầy đủ của công cụ MCP; Claude Code ẩn công cụ MCP sau ToolSearch, nên agent không gọi.

**Bản sửa (`20cc037`):**
- Bootstrap ưu tiên 6 thư mục bị sửa nhiều nhất trong 300 commit tới commit gốc. Lịch sử này agent vốn đọc được, nên không lộ lời giải.
- Hướng dẫn nêu tên đầy đủ của công cụ và cách nạp qua ToolSearch.

Dựng lại 30 ảnh chụp (4–8 mục mỗi ảnh, $9,21), rồi chạy lại nhánh `bint` 1 lần lặp ($5,93). Một đợt chạy trước đó bị loại vì tài khoản hết hạn mức giữa chừng (xem F14).

| So sánh với none | Tỷ lệ đạt | Δ tỷ lệ đạt | Δ chi phí/lượt | Δ lượt hội thoại | Tác vụ đạt/trượt ngược với none |
|---|---:|---|---|---|---|
| bint (tri thức cũ) | 40 % | +3,3 điểm % [−12, +18] | +$0,031 [+0,009, +0,054] | +0,6 [−1,1, +2,6] | 6 / 4 |
| **bint (tri thức theo vùng mã)** | **40 %** | **+3,3 điểm % [−13, +20]** | **+$0,004 [−0,022, +0,030]** | **−1,9 [−4,1, +0,4]** | 2 / 2 |

(Cột cuối của dòng mới chỉ đếm tác vụ mà none cho cùng kết quả ở cả 2 lần lặp.)

**Đọc kết quả:**
- **Chi phí phụ trội biến mất:** từ +16 % còn +2 %, không còn ý nghĩa thống kê. Số lượt hội thoại có xu hướng giảm ~14 %, nhưng chưa có ý nghĩa thống kê.
- **Tỷ lệ đạt vẫn không đổi.**
- **Kênh truy xuất:** hook theo đường dẫn chèn tri thức ở 22/30 lượt. **Không lượt nào gọi công cụ MCP**, kể cả khi hướng dẫn đã nêu rõ cách nạp, và không có trích dẫn nào. Context Engine qua MCP hiện không đóng góp gì; toàn bộ tác động đến từ hook.

**Kết luận:** sau khi sửa, Bint **không còn tốn thêm**, nhưng cũng **chưa làm tăng tỷ lệ đạt đo được**. Với 30 tác vụ và 1 lần lặp, phép đo chỉ thấy được mức tăng ≥ ~18 điểm %, nên mức tăng nhỏ hơn vẫn có thể tồn tại. Hướng cải thiện tiếp theo là đẩy tri thức qua hook (kênh duy nhất đang hoạt động) thay vì trông vào việc agent tự gọi MCP, và dùng tri thức đã qua người duyệt.

## Khả thi của P8 trên lý thuyết

**Mô hình:**
- σ²(R) = σ_b² + W/R, với W = 0,20 đo được và σ_b lấy theo sàn mô hình lật.
- Cỡ mẫu: n = (2,8·σ/δ)², ứng với α = 0,05 và power 0,8.
- Đơn giá: $0,20 mỗi lượt.
- Bộ lập kế hoạch chọn R rẻ nhất mà n vừa với số tác vụ sẵn có.

Chi phí trong bảng là **chi phí tối đa** khi chạy đủ R. Đối chiếu tuần tự dừng sớm khi kết quả đã rõ, nên chi phí thực tế thường thấp hơn.

| δ cần phát hiện | 16 tác vụ/đề xuất | 33 tác vụ/đề xuất | 66 tác vụ/đề xuất |
|---|---|---|---|
| +10 điểm % | không khả thi | không khả thi | n = 65, R = 4, ≤ $104 |
| +15 điểm % | không khả thi | n = 30, R = 5, ≤ $60 | n = 51, R = 2, ≤ $41 |
| +20 điểm % | không khả thi | n = 31, R = 2, ≤ $25 | n = 51, R = 1, ≤ $20 |
| +25 điểm % | n = 15, R = 4, ≤ $24 | n = 22, R = 2, ≤ $18 | n = 34, R = 1, ≤ $14 |

**Kết luận: P8 khả thi về lý thuyết, có điều kiện.**
- **Mức +15 đến +20 điểm %:** phát hiện được với ~30 tác vụ mỗi đề xuất. Mỗi lần đối chiếu tốn ≤ $25–60, mỗi vòng Propose–Probe–Compose (~3 lần đối chiếu) ≤ $75–180.
- **Mức +10 điểm %:** chỉ khả thi khi mỗi đề xuất có ≥ 65 tác vụ, gần như toàn bộ tập validation của một repository cỡ này.
- **Nút thắt là nguồn tác vụ đã duyệt, không phải tiền.** Hiện chỉ có 92 tác vụ đã duyệt; mỗi đề xuất cần ~30 tác vụ sạch trong phạm vi. Việc duyệt tác vụ (`bint eval approve`) là việc của người.

**Điều kiện mở P8:**
1. Duyệt thêm tác vụ sạch cho tập validation và test, mục tiêu ≥ 60 tác vụ mỗi tập.
2. Chạy một lần Probe thật bằng thiết kế mới (L1 trên validation), dự kiến ≤ $60. Lần chạy này cho số đo σ_b đầu tiên từ dữ liệu ghép cặp, thay cho mức sàn giả định.
3. Chốt ngưỡng chấp nhận δ ≥ 15 điểm % trong phạm vi đề xuất, và biên không kém 10 điểm %.

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
| F14 | Cao (đã sửa, `aa66d8c`) | Agent hết hạn mức sử dụng (429) giữa đợt vẫn bị chấm là **trượt**, làm sai số liệu (một đợt ghi 4/30). Nay ghi là lỗi hạ tầng, đợt dừng ở `interrupted`, `--resume` chạy lại các lượt đó. |
| F15 | Cao | Agent không gọi công cụ MCP của Context Engine (0/30 lượt). Cấu hình đúng: thử trực tiếp thì agent thấy và gọi được công cụ. Agent chỉ coi lời gợi ý trong AGENTS.md là tuỳ chọn và tự đọc mã. Chỉ hook theo đường dẫn có tác dụng. |
| F16 | Cao (đã sửa, `f2fd149`) | `context_for_task`/`context_for_paths` chỉ trả ID trong `structuredContent`, mà Claude Code chỉ đưa trường này cho mô hình. Trước bản sửa, kể cả khi được gọi, MCP cũng không đem tri thức tới agent. |
| F17 | Cao (đã sửa, `c1794a6`) | Ký hiệu dạng `Lớp.phương_thức` trong bằng chứng luôn bị báo "không có trong mã" (định nghĩa chỉ ghi tên cuối), nên mọi đề xuất của evolver neo theo phương thức bị loại. |
| F18 | Trung bình (đã sửa, `7e9c358`) | Evolver hay ghi lệch vùng dòng vài dòng, hoặc đưa vào phạm vi một tệp test không tồn tại; cả đề xuất bị loại. Nay vùng dòng được đặt lại theo ký hiệu, đường dẫn phạm vi không tồn tại bị bỏ. |

## Mở P8 (2026-11-11)

**Kênh đưa tri thức (sửa F15):** hook `UserPromptSubmit` chèn gói ngữ cảnh theo đề bài ngay khi agent nhận yêu cầu (`340459e`), không chờ agent tự gọi MCP. Kiểm tra bằng Haiku (13 lượt, ~$1,1, dừng sớm vì đã đủ để kết luận): **13/13 lượt nhận tri thức qua kênh `hook:task`**.

**Tác vụ:** quét lại đặc tả 787 ứng viên ($0) → 168 không cảnh báo đặc tả/rò rỉ → kiểm chứng fail→pass 83 (76 đạt) → bỏ 4 tác vụ thay đổi ≥ 299 dòng → duyệt 90, chia ngẫu nhiên validation/test. **Validation 70, test 73.** Các tác vụ này do agent duyệt (người dùng cho phép vượt chặn); kết quả trên chúng kém tin cậy hơn một bậc so với tác vụ người duyệt.

**Vòng đối chiếu đầu tiên (vòng hiệu chỉnh)** — đề xuất tạo một mục tri thức cho vùng mã hay bị đọc lại:

| | Giá trị |
|---|---|
| Kế hoạch | 34 tác vụ × R = 4 × 2 nhánh = tối đa 272 lượt, trần $60, phát hiện δ = 15 điểm % |
| Thực tế | **Dừng ở điểm nhìn 1/4 sau 70 lượt, $14,58** (futility) |
| Δ tỷ lệ đạt | +0,0 điểm % [−8,8, +8,8] (26 % cả hai nhánh) |
| Δ chi phí/lượt | +$0,010 [−0,009, +0,030] |
| Kết luận | not_supported: cận trên CI95 +8,6 điểm % < 15 điểm % |

Thiết kế tuần tự làm đúng vai trò: loại đề xuất không hiệu quả sau 1/4 chi phí tối đa. σ_b vẫn chưa đo được vì dừng trước khi có lần lặp thứ hai.

**Mã P8 đã có:**
- `bint evolve cycle --budget X` — một vòng Propose–Probe–Compose: kiểm tra hồi quy đề xuất đã áp dụng → tạo đề xuất → đối chiếu validation rồi test → tự áp dụng tối đa một đề xuất mỗi vòng; chỉ tính kết quả đối chiếu sau lần áp dụng gần nhất, nên tác động cộng dồn luôn được đo lại. Chạy thử trên repository B: không có cơ hội mới, $0.
- Tự áp dụng rủi ro thấp (`evolution.adoption.auto_low_risk`, mặc định tắt): chỉ đề xuất tạo mục mới (vào candidate), rủi ro thấp, supported độc lập trên test; tác nhân `bint-auto`.
- Hồi quy: đề xuất do `bint-auto` áp dụng thì tự quay lui; do người áp dụng thì đề xuất quay lui.
- Tự bác bỏ: đề xuất mà lần đối chiếu trên nền hiện tại kết luận `not_supported` bị vòng bác bỏ (lý do ghi `bint-auto: không đạt trên <split>`; `--keep-failed` để giữ). `inconclusive` thì giữ. Trước đây đề xuất thua nằm chờ mãi và chặn cơ hội gốc; giờ cơ hội mở lại khi có lượt mới làm bằng chứng.
- Thay đổi workflow: đề xuất skill (tạo/sửa/gộp/bỏ skill) đi cùng đường đối chiếu validation → test như tri thức, nhưng không bao giờ được tự áp dụng; đạt thì vào danh sách chờ người duyệt.
- Thử nghiệm chiến lược chọn ngữ cảnh: nhánh `bint@task=off+path=off+mcp=off+tokens=N+items=N`, so sánh ghép cặp trong cùng đợt.
- Theo dõi chi phí: mỗi vòng ghi `<runtime>/evolve/cycles/<id>.json`; `bint evolve history` cộng tổng.
- Thay đổi workflow: đi qua đề xuất skill (L2), được vòng đối chiếu nhưng luôn chờ người duyệt.

**Thử nghiệm chiến lược ngữ cảnh đầu tiên** — hook chèn theo đề bài bật (`bint`) so với tắt (`bint@task=off`), 30 tác vụ có ảnh chụp tri thức strict, 1 lần lặp, $13,37:

| | bint | bint@task=off | Δ (tắt − bật) |
|---|---:|---:|---|
| Tỷ lệ đạt | 40 % | 50 % | +10,0 điểm % [−6,7, +26,7] |
| Chi phí/lượt | $0,218 | $0,228 | +$0,010 [−0,016, +0,039] |
| Thời gian | 40 s | 70 s | **+30 s [+2, +76]** |

Hook làm lượt chạy nhanh hơn có ý nghĩa thống kê, nhưng không tăng tỷ lệ đạt; điểm ước lượng còn nghiêng về phía tắt hook (chưa có ý nghĩa). Tri thức đang chèn là mục tự sinh chưa qua duyệt; chèn sớm loại tri thức này có thể dẫn hướng sai. Giữ mặc định hiện tại; cần lặp lại với tri thức đã duyệt và nhiều lần lặp trước khi đổi.

**Chưa đạt nghiệm thu P8:** chưa có đề xuất nào cải thiện được tỷ lệ đạt trên tập test độc lập. Việc tiếp theo: chạy `bint evolve cycle` định kỳ khi có lịch sử phiên mới (cơ hội mới), và đo biến thể chiến lược ngữ cảnh `bint` so với `bint@task=off` để định lượng tác động của hook mới.

## Việc cần làm trước khi mở P8 (đã xử lý, xem mục "Mở P8")

1. Dùng bản sửa đặc tả, rồi đo lại tỷ lệ đạt nền trên tập validation đủ lớn. Chỉ chọn tác vụ không còn cảnh báo, hoặc viết lại đề bài cho những tác vụ có cảnh báo. Mục tiêu là tỷ lệ đạt nền khoảng 30–70 %.
2. Sửa F2/F4 (neo bằng chứng theo hàm, ưu tiên mã tồn tại tại commit cũ), F5, F6, F10, F13.
3. Đo lại chi phí Probe ở cỡ mẫu dùng được, rồi so với lợi ích kỳ vọng. Với mức $0,30 mỗi lượt, mục tiêu +10 điểm % đòi ~$700 mỗi vòng; cần xem xét hạ mục tiêu, hoặc dùng mô hình rẻ hơn cho Probe sàng lọc.
