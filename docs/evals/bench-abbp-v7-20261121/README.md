# Đợt 7: A / B / B′ trên L1 (2026-11-20 → 21)

Bản đăng ký trước, các thay đổi giữa chừng và sự cố: `prereg-and-incidents.md`. Dữ liệu thô: `D:\bench\bench-v7-20261120\` (repo, log, state từng lượt);
bản chép kết quả: `results/`.

## Kết luận

**Ở bài L1, B và B′ cùng đạt 30/36 ô sạch, hơn A 8 điểm (22/36, cả đợt 6 lẫn đợt 7). Nhưng không nhánh nào xong việc: cả ba còn lọt khoá PEM ở mọi nơi,
cả ba hỏng N2 (verify log lớn chậm 11–19 lần), B và B′ bị trần 180 phút cắt và giao cây có test repo đỏ. B′ không hơn B về chất lượng; nó ngang B,
tốn token hơn (sau khi trừ phần do sự cố), ít test hồi quy hơn (3 so với 8) và là nhánh duy nhất tới được review.** Một lượt mỗi nhánh: đây là tín hiệu
định hướng, không đủ để áp tiêu chí dừng đã đăng ký (cần CI 95 %), và A+ (đối chứng ngân sách) đã bị bỏ theo yêu cầu, nên chưa tách được phần hơn của
B/B′ là nhờ cấu trúc hay nhờ tiêu nhiều token hơn (5,5–7 lần A).

Phần lớn chênh lệch B đợt 7 so với B đợt 6 (20/36 → 30/36) nằm ở đúng hai loại AWS, và trùng với commit `39a9b70` (sau đợt 6) thêm quy tắc
"danh sách loại không hẹp hơn … biến thể phổ biến ngoài đời" vào b-req/b-goal: quy tắc viết sau khi thấy kết quả test ẩn đợt 6, nên phần lợi này có
nguy cơ là học theo đề.

## Bảng chính

| | A đợt 6 | A-1 | B-1 (classic) | B′-1 lúc đứt mạng (9100 s) | B′-1 cuối (đã bù) |
|---|---|---|---|---|---|
| Test ẩn (ô sạch / 36) | 22 | **22** | **30** | 30 | **30** |
| Theo nhóm S1–S6 | S3, S6 = 0/7 | S1,S2,S4 7/7; S3,S6 0/7; S5 1/1 | mỗi nhóm 6/7; S5 0/1 | như B-1 | như B-1 |
| Loại còn lọt | — | lọt hết ở S3, S6 | `private_key` 0/6 | `private_key` 0/6 | `private_key` 0/6 |
| N1 (không che quá tay) | qua | qua | qua | qua | qua |
| N2 (≤ 3× bản gốc) | 20,1× hỏng | 19,3× hỏng | 11,7× hỏng | 11,5× hỏng | 15,7× hỏng |
| `go test ./...` cây cuối | đỏ | 1 đỏ (chập chờn) | **8 đỏ** (≥ 4 hồi quy + thước đo của chính nó) | — | **3 đỏ** (2 hồi quy đồng bộ skill + 1 cổng hiệu năng) |
| Diff (bỏ .bint/skill) | — | 32 tệp, +718/−130 | 76 tệp, +2983/−234 | 68 tệp | 87 tệp, +3299/−219 |
| Token vào / ra | 37,4M / 147k (3 phiên) | 8,0M / 44k | 43,7M / 173k | 39,3M | 61,5M / 245k (≈ 55M sau khi trừ thăm dò chạy lại) |
| Thời gian | 120 phút (cắt) | 30 phút, tự dừng | 180 phút, **trần cắt** khi đang kiểm chứng | 152 phút | 180 + 38 phút bù, **trần cắt** trong vòng sửa sau review |
| Tới b-review? | — | — | **không** | không | **có** (8 phát hiện high) |

Ghi chú: N2 đo thời gian tuyệt đối dưới tải máy khác nhau (B′ cuối đo khi máy rảnh: 9 s so với 0,57 s); mọi nhánh đều hỏng xa ngưỡng nên kết luận
"không ai sửa hiệu năng" không đổi.

## Dòng thời gian (180 phút = 100 %)

| Mốc | B-1 | B′-1 |
|---|---|---|
| b-req | 6m26s (không câu hỏi, thẳng cổng kế hoạch lớn: 42 tệp, 4 lát) | 3m09s + 1 câu hỏi + 1m53s (kế hoạch nhỏ, chạy thẳng) |
| Dòng mã đầu tiên | phút ~34 (**19 %**): b-goal 24m41s chạy tuần tự trước | phút ~5 (**3 %**): lõi chạy song song với thăm dò |
| Kết quả kiểm chứng đầu tiên | phút ~77 (**43 %**) | phút ~74 (≈ 41 %) theo cây gốc; thực tế bị sự cố dlv làm mất |
| Cơ cấu | 4 lát × (b-feat ~14 phút + kiểm chứng/goal ~17–20 phút): **kiểm chứng ăn hơn nửa mỗi lát** | lõi 29 phút → kiểm chứng → sửa lõi 6,5 phút → gộp thăm dò → mở rộng (đứt mạng 28 phút, chạy lại 17 phút) → review 7 phút → vòng sửa (bị cắt) |
| `bint task check` (kiểm hẹp theo mốc của B′) | không áp dụng | **0 lần**: agent bỏ qua chỉ dẫn, nên kiểm nền cũng không bao giờ chạy |

## Nhận xét

1. **Ô sạch: B ≈ B′ > A.** Cả hai luồng Bint phủ được S3 (mô tả tác vụ) và S6 (tài liệu tác vụ) mà A bỏ trọn cả hai lần. Đây là độ rộng của b-req, đúng như
   điểm cộng đã thấy ở đợt 6. B′ dùng b-req gọn vẫn giữ được độ rộng đó.
2. **Điểm mù chung của thước đo tự viết vẫn còn.** Không test đóng băng nào (B: `credential_persistence_test.go`; B′: hai tệp `secret_storage_goal_test.go`
   do ngữ cảnh thăm dò hộp đen viết) chứa khoá PEM, và `private_key` lọt ở mọi nơi trong cả hai nhánh. Thăm dò của B′ được tách ngữ cảnh (không thấy kế
   hoạch, không thấy mã) nhưng vẫn cùng model nên bỏ sót cùng loại; dự đoán "thước đo độc lập gỡ được ô lọt" của báo cáo **không** được xác nhận ở loại này.
3. **Đảo thứ tự có tác dụng ở chỗ dòng mã đầu tiên (19 % → 3 %), chưa có tác dụng ở chỗ kết quả kiểm chứng đầu tiên (~41–43 % ở cả hai).** Lý do: agent
   b-feat không gọi `bint task check` lần nào, nên kiểm hẹp và kiểm nền của giai đoạn 3 không chạy; kiểm chứng vẫn là một lần `go test` gói bị chạm
   (~20 phút trên Windows) sau mỗi b-feat. Muốn có tín hiệu sớm phải để Bint tự chạy kiểm hẹp, không trông vào agent.
4. **Kiểm chứng trên Windows chiếm ngân sách.** Ở B-1, mỗi lát ~14 phút b-feat nhưng ~17–20 phút kiểm chứng + cổng mục tiêu; 4 lát dùng hết 180 phút, không
   tới được review. B′ tới được review một phần nhờ chỉ có một lõi + một mở rộng.
5. **Lát mỏng không chặn hồi quy khi người dùng chọn "giữ".** Bint báo đúng "test cũ đỏ" ở cả 4 lát của B-1, nhưng người dùng giả (low-tech) chọn
   `--slice keep` cả 4 lần; cây cuối mang 4–5 hồi quy thật. Người dùng không rành sẽ luôn chọn giữ: lựa chọn này cần mặc định an toàn hơn hoặc lát sau phải
   sửa test cũ đỏ trước.
6. **Review của B′ tìm thêm lỗi thật ngoài ma trận** (8 high: `curl --user` lọt vào log kiểm chứng, chỉ mục tri thức lưu token, đồng bộ profile làm hỏng
   xác thực MCP, …) nhưng hết ngân sách trước khi sửa xong.
7. **Chi phí.** Token vào so với A-1: B 5,5×, B′ 7,7× (≈ 6,9× sau khi trừ thăm dò chạy lại). Đều vượt ngưỡng "tốn kém" 2,5× của báo cáo. Ở mốc 9100 s
   (trước phần bù) B′ đã 30/36 với 39,3M, tức phần thêm của B′ sau đó không đổi điểm test ẩn mà chỉ giảm test repo đỏ và chạy review.

## Đối chiếu với tiêu chí dừng đã đăng ký

| Tiêu chí | Kết quả đợt này |
|---|---|
| 1. B không hơn A+ ≥ 10 điểm % → bỏ classic làm mặc định | **Không áp được**: không có A+. So với A: B hơn 22 điểm % (30 so với 22 ô), nhưng với token 5,5× và test repo đỏ. |
| 2. B′ hơn A+ ≥ 10 điểm % với token ≤ 2,5× A → giữ B′ cho việc cỡ L1 | **Không đạt về chi phí** (6,9–7,7× A) dù hơn A 22 điểm %; không có A+. |
| 3. B′ không hơn A+ → định vị lại Bint | Chưa kết luận được. |
| B′ so với B | Ngang test ẩn (30 = 30), ít hồi quy hơn (3 so với 8), tốn token hơn, có review. Không có bằng chứng B′ tốt hơn B về chất lượng giải. |

## Giới hạn

- Một lượt mỗi nhánh; A ổn định giữa hai lượt (22/36 cả hai), B/B′ chưa đo được phương sai.
- Không có A+, nên "B/B′ hơn A" có thể chỉ là do tiêu nhiều token hơn.
- B′-1 bị hai sự cố ngoài Bint: người điều phối giết tiến trình bằng dlv lúc 22:49 (mất lần kiểm chứng ~38 phút và lần thăm dò đầu ~6,6M token), và proxy
  Codex cắt kết nối lúc 00:09 (mất một lượt b-feat 28 phút). Đã bù 38 phút và chạy tiếp; điểm test ẩn trước và sau phần bù như nhau (30/36).
- B và B′ dùng skill/binary tại `63a6114`, A dùng repo đã gỡ Bint; chênh lệch B đợt 6 → đợt 7 không tách được giữa thay đổi skill (`39a9b70`), cấu hình
  lát mỏng có hiệu lực, trần 180 phút một mạch và ngẫu nhiên.
- Test ẩn L1 chỉ đo sáu nhóm nơi lưu × bảy loại; B/B′ sửa rộng hơn nhiều (76–87 tệp) nên phần ngoài ma trận chỉ thấy qua review và test repo.

## Việc nên làm tiếp (theo thứ tự lợi/chi phí)

1. Cho Bint tự chạy kiểm hẹp (gói bị chạm) sau mỗi b-feat thay vì trông agent gọi `bint task check`; đo lại mốc "kết quả kiểm chứng đầu tiên".
2. Lát mỏng: khi có "test cũ đỏ", lát kế tiếp nhận yêu cầu sửa trước; hoặc đổi mặc định khi người dùng không chọn.
3. Thước đo: thêm vào b-goal/thăm dò một bước đối chiếu danh mục ngoài (ví dụ bộ mẫu bí mật phổ biến) thay vì chỉ "biến thể ngoài đời" do model tự nghĩ;
   PEM là dạng còn sót ở mọi nhánh.
4. Nếu cần kết luận theo tiêu chí dừng: ≥ 3 lượt B′ và A+ (ngân sách = token B′), cùng điều kiện máy.
