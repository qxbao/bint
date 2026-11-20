# Đợt 7: A / B / B′ trên L1 (2026-11-20 → 21)

Bản đăng ký trước, các thay đổi giữa chừng và sự cố: `prereg-and-incidents.md`. Dữ liệu thô: `D:\bench\bench-v7-20261120\` (repo, log, state từng lượt);
bản chép kết quả: `results/`.

> **Đính chính 2026-11-21.** Bản đầu của README này có chín chỗ lệch dữ liệu thô và mã nguồn (đối chiếu đầy đủ: `reports/Đánh giá đợt 7 luồng Bint.md`,
> mục "Chín chỗ README đợt 7 lệch khỏi dữ liệu thô"). Bản dưới đã sửa theo tệp thô. Thay đổi lớn nhất về cách đọc: kiểm hẹp theo mốc và "lõi trước" của B′
> **chưa được thử đúng** ở đợt này (lỗi nối dây trong Bint và lỗi dựng mẫu), nên đợt 7 không phải phép đo về ý tưởng B′ ở hai cơ chế đó.

## Kết luận

**Ở bài L1, B và B′ cùng đạt 30/36 ô sạch, hơn A 8 ô (22/36, cả đợt 6 lẫn đợt 7). Nhưng không nhánh nào xong việc: B và B′ lọt khoá PEM ở cả sáu nơi
(A che được PEM ở 4/6 nơi), cả ba hỏng N2, B và B′ bị trần 180 phút cắt và chưa lần kiểm chứng nào xanh — một phần vì mẫu `tmpl-B` đỏ sẵn hai test ở
commit gốc. B′ ngang B ở test ẩn; hai trong bốn cơ chế của B′ (kiểm hẹp theo mốc, lõi trước) không chạy như thiết kế nên "B′ không hơn B" chưa phải kết
luận về ý tưởng B′.** Một lượt mỗi nhánh: đây là tín hiệu định hướng, không đủ để áp tiêu chí dừng đã đăng ký (cần CI 95 %), và A+ (đối chứng ngân sách) đã
bị bỏ theo yêu cầu, nên chưa tách được phần hơn của B/B′ là nhờ cấu trúc hay nhờ tiêu nhiều token hơn (5,5–7,7 lần A).

Toàn bộ chênh lệch B đợt 7 so với B đợt 6 (20/36 → 30/36) là 10 ô của hai loại AWS, trùng với commit `39a9b70` (sau đợt 6) thêm quy tắc "danh sách loại
không hẹp hơn … biến thể phổ biến ngoài đời" vào b-req/b-goal; test ẩn đợt 7 trùng từng byte với đợt 6. Quy tắc viết sau khi thấy kết quả test ẩn đợt 6,
nên phần lợi này có nguy cơ là học theo đề.

## Bảng chính

| | A đợt 6 | A-1 | B-1 (classic) | B′-1 lúc lượt mở rộng bị đánh failed (9100 s) | B′-1 cuối (đã bù) |
|---|---|---|---|---|---|
| Test ẩn (ô sạch / 36) | 22 | **22** | **30** | 30 | **30** |
| Theo nhóm S1–S6 | S3, S6 = 0/7 | S1,S2,S4 7/7; S3,S6 0/7; S5 1/1 | mỗi nhóm 6/7; S5 0/1 | như B-1 | như B-1 |
| Loại còn lọt | — | lọt hết ở S3, S6; `private_key` sạch 4/6 | `private_key` 0/6 | `private_key` 0/6 | `private_key` 0/6 |
| N1 (không che quá tay) | qua | qua | qua | qua | qua |
| N2 (≤ 3× bản gốc) | 20,1× hỏng | 9,62× hỏng¹ | 6,61× hỏng¹ | 11,5× hỏng | 15,7× hỏng |
| `go test ./...` cây cuối | đỏ | 1 đỏ (chập chờn) | 8 đỏ: 2 hồi quy có bằng chứng, 1 thước đo của chính nó, 2 lỗi mẫu, 3 chỉ đỏ ở lần chấm cuối dưới tải | — | 3 đỏ: 2 lỗi mẫu (đỏ sẵn ở `e41bb69`), 1 test nhạy thời gian |
| Diff (bỏ .bint/skill) | — | 32 tệp, +718/−130 | 76 tệp, +2983/−234 | 68 tệp | 87 tệp, +3299/−219 |
| Token vào / ra | 37,4M / 147k (5 tệp phiên) | 8,0M / 44k | 43,7M / 173k | 39,3M | 61,5M / 245k (≈ 45M sau khi trừ thăm dò chạy lại và lượt mở rộng chạy hai lần²) |
| Thời gian | ≈ 84 phút, tự báo xong | 30 phút, tự dừng | 180 phút, **trần cắt** khi đang kiểm chứng | 152 phút | 180 + 38 phút bù, **trần cắt** trong vòng sửa sau review |
| Tới b-review? | — | — | **không** | không | **có** (8 phát hiện high, 7 đã có bản sửa trong cây) |

¹ Số trong `results/*/n2.json` hiện còn; bản đầu README ghi 19,3× và 11,7× nhưng `grade.py` ghi đè `n2.json` mỗi lần chấm nên không còn nguồn. Các lần
đo chạy dưới tải máy khác nhau (A-1 và B-1 chấm song song; B′ chấm một mình khi máy rảnh: 9 s so với 0,57 s): mọi nhánh hỏng xa ngưỡng nên "không ai
sửa hiệu năng" đứng vững, nhưng mức chậm **không so được giữa các nhánh**.
² Ước lượng: lượt mở rộng thứ hai có làm thêm việc mới, nên con số thật nằm giữa ≈ 45M và 55M.

## Dòng thời gian (180 phút = 100 %)

| Mốc | B-1 | B′-1 |
|---|---|---|
| b-req | 6m26s (không câu hỏi, thẳng cổng kế hoạch lớn: 42 tệp, 4 lát) | 3m09s + 1 câu hỏi + 1m53s (kế hoạch nhỏ, chạy thẳng) |
| Dòng mã sản phẩm đầu tiên | phút 36,7 (**20 %**): b-goal 24m41s chạy tuần tự trước | phút 7,1 (**4 %**): lõi chạy song song với thăm dò |
| Kết quả kiểm chứng đầu tiên | phút 76,9 (**43 %**) | phút 103,4 (**57 %**); lần kiểm chứng trước đó bị sự cố dlv giết |
| Cơ cấu | 4 lát × (b-feat ~14 phút + kiểm chứng/goal ~17–20 phút): kiểm chứng + cổng mục tiêu ≈ 50 % ngân sách | lõi 29 phút → kiểm chứng → sửa lõi 6,5 phút → gộp thăm dò → mở rộng 28 phút (xong bình thường nhưng bị đánh failed) → mở rộng lần hai 17 phút → review 7 phút → vòng sửa 16 phút (bị cắt trước khi kiểm chứng lại) |
| `bint task check` (kiểm hẹp theo mốc của B′) | không áp dụng | **gọi 24 lần, cả 24 lần tự bỏ qua**: sandbox Codex chặn `git add` ghi `.git/objects`, lỗi chụp cây bị coi là phạm vi full, lệnh in "Bỏ qua" và không ghi gì; lượt lõi không nhận chỉ dẫn gọi lệnh này |

## Nhận xét

1. **Ô sạch: B ≈ B′ > A.** Cả hai luồng Bint phủ được S3 (mô tả tác vụ) và S6 (tài liệu tác vụ) mà A bỏ trọn cả hai lần. Đây là độ rộng của b-req, đúng như
   điểm cộng đã thấy ở đợt 6. B′ dùng b-req gọn vẫn giữ được độ rộng đó. Ngược lại, A che được PEM ở 4/6 nơi mà B/B′ không: hơn 8 ô = +12 ô nhờ độ rộng
   nơi lưu − 4 ô PEM.
2. **Điểm mù chung của thước đo tự viết vẫn còn.** Không test đóng băng nào (B: `credential_persistence_test.go`; B′: hai tệp `secret_storage_goal_test.go`
   do ngữ cảnh thăm dò hộp đen viết) chứa khoá PEM, và `private_key` lọt ở mọi nơi trong cả hai nhánh Bint. Thăm dò của B′ được tách ngữ cảnh (không thấy
   kế hoạch, không thấy mã) nhưng **đề bài của nó vẫn chứa nguyên 5 tiêu chí do b-req viết** và không có danh mục ngoài, nên phép thử "thước đo khác thông
   tin" chưa sạch. Lỗ PEM có từ đầu vào: prompt, câu trả lời của người dùng giả và cả hai requirement đều không nhắc khoá riêng.
3. **Đảo thứ tự có tác dụng ở chỗ dòng mã đầu tiên (20 % → 4 %), chưa có tác dụng ở chỗ kết quả kiểm chứng đầu tiên (43 % ở B, 57 % ở B′).** Lý do không
   phải agent bỏ chỉ dẫn: (a) lượt lõi nối tiếp phiên b-req và đề bài nối tiếp (`planHandoffPrompt`) thiếu khối "LÕI trước" lẫn khối kiểm hẹp, nên lượt
   lõi là một b-feat toàn phần chạm 59 tệp; (b) các lượt sau gọi `bint task check` 24 lần nhưng lệnh tự bỏ qua vì sandbox; (c) kể cả chạy được, "gói bị chạm"
   tính so với mốc tác vụ và lan theo phụ thuộc là 39 gói (~20 phút trên Windows). Kiểm nền chỉ chạy sau một kiểm hẹp xanh nên cũng không chạy.
4. **Kiểm chứng trên Windows chiếm ngân sách.** Kiểm chứng + cổng mục tiêu ăn ≈ 50 % ngân sách ở cả hai nhánh Bint (B: 90,9/179,9 phút; B′: 109,5/218 phút).
5. **Lát mỏng và test đỏ nền.** Bint báo "test cũ đỏ" ở cả 4 lát của B-1, nhưng hai trong các test đó đỏ sẵn ở commit gốc của mẫu (`e41bb69`, tệp skill
   9067 so với 9086 byte) và lát 1 không có hồi quy thật nào. "Người dùng giả" là bảng quyết định viết cứng trong `tools/run.py`: `--slice keep` là một nhánh
   của script, nên "người dùng không rành sẽ luôn chọn giữ" là giả định, không phải quan sát. Cây cuối của B có 2 hồi quy thật có bằng chứng
   (`TestRepoWritesGoThroughRepoWriter`, `TestMCPIndexIgnoresWrapperLauncher`). B còn bị chính `scope.allow` do b-req đặt chặn không cho sửa
   `internal/cli/task.go`, nên cổng mục tiêu của nó không thể xanh.
6. **Review của B′ tìm thêm lỗi thật ngoài ma trận** (8 high: `curl --user` lọt vào log kiểm chứng, chỉ mục tri thức lưu token, đồng bộ profile làm hỏng
   xác thực MCP, …; một là lỗi mẫu). Vòng sửa 15,9 phút đã đưa bản sửa cho 7 phát hiện vào cây (37 tệp, +822/−129) nhưng bị trần cắt trước khi kiểm chứng
   lại. Review không nhắc PEM hay hiệu năng.
7. **Chi phí.** Token vào so với A-1: B 5,5×, B′ 7,7× (≈ 5,7× sau khi trừ thăm dò chạy lại và lượt mở rộng chạy hai lần). Đều vượt ngưỡng "tốn kém" 2,5× của
   báo cáo. Ở mốc 9100 s B′ đã 30/36 với 39,3M, tức phần thêm của B′ sau đó không đổi điểm test ẩn mà chỉ giảm test repo đỏ và chạy review. 7,76M trong 8,0M
   token vào của A là cache; mọi tệp kết quả có `cost_usd: null`, nên so chi phí thật cần tính cache.
8. **Ghi chú của Bint sai.** Bint ghi "lõi xanh, đã gộp và đóng băng thước đo" khi kiểm chứng còn `passed: false`, và đánh `failed` một lượt b-feat đã
   `turn.completed` chỉ vì một sự kiện `error` "Reconnecting… 2/5" ở đầu log.

## Đối chiếu với tiêu chí dừng đã đăng ký

| Tiêu chí | Kết quả đợt này |
|---|---|
| 1. B không hơn A+ ≥ 10 điểm % → bỏ classic làm mặc định | **Không áp được**: không có A+. So với A: B hơn 22 điểm % (30 so với 22 ô), nhưng với token 5,5× và kiểm chứng chưa lần nào xanh. |
| 2. B′ hơn A+ ≥ 10 điểm % với token ≤ 2,5× A → giữ B′ cho việc cỡ L1 | **Không đạt về chi phí** (5,7–7,7× A) dù hơn A 22 điểm %; không có A+. |
| 3. B′ không hơn A+ → định vị lại Bint | Chưa kết luận được. |
| B′ so với B | Ngang test ẩn (30 = 30). So sánh số test repo đỏ (3 so với 8) **không dùng được**: phần lớn là lỗi mẫu và nhiễu tải. B′ tới được review. Hai cơ chế B′ chưa chạy đúng thiết kế. |

## Giới hạn

- Một lượt mỗi nhánh; A ổn định giữa hai lượt (22/36 cả hai), B/B′ chưa đo được phương sai.
- Không có A+, nên "B/B′ hơn A" có thể chỉ là do tiêu nhiều token hơn.
- Mẫu `tmpl-B` đỏ sẵn hai test đồng bộ skill ở commit gốc `e41bb69`: kiểm chứng của B/B′ không thể xanh dù agent làm gì.
- B′-1 bị một sự cố ngoài Bint: người điều phối giết tiến trình bằng dlv lúc 22:49 (mất lần kiểm chứng ~38 phút và lần thăm dò đầu ~6,6M token). Sự cố
  00:09 **không** phải lỗi mạng làm mất việc: lượt b-feat mở rộng đã `turn.completed`, Bint đánh `failed` nhầm vì một sự kiện "Reconnecting… 2/5", nên B′
  chạy hai lượt mở rộng thay vì một. Đã bù 38 phút và chạy tiếp; điểm test ẩn trước và sau phần bù như nhau (30/36).
- Tải máy không đều: 45 phút đầu có 5 lượt Bint chạy song song; A-1 và B-1 chấm cuối song song còn B′ chấm một mình.
- B và B′ dùng skill/binary tại `63a6114`, A dùng repo đã gỡ Bint; chênh lệch B đợt 6 → đợt 7 không tách được giữa thay đổi skill (`39a9b70`), cấu hình
  lát mỏng có hiệu lực, trần 180 phút một mạch và ngẫu nhiên.
- Test ẩn L1 chỉ đo sáu nhóm nơi lưu × bảy loại; B/B′ sửa rộng hơn nhiều (76–87 tệp) nên phần ngoài ma trận chỉ thấy qua review và test repo.

## Việc nên làm tiếp

Danh sách đầy đủ 18 việc xếp theo lợi ích trên chi phí: `reports/Đánh giá đợt 7 luồng Bint.md`. Các việc sản phẩm đã làm sau đợt này (chưa đo lại):
đề bài nối tiếp mang khối B′; kiểm hẹp có đường chỉ đọc khi sandbox chặn ghi `.git`, ghi cả lần bị bỏ qua, chỉ chạy gói đổi kể từ lần kiểm xanh trước,
và Bint tự chạy khi cây đứng yên; không đánh `failed` lượt Codex đã `turn.completed` sau khi kết nối lại; ghi chú/đề bài nói thật khi lõi còn đỏ; thăm dò
chỉ thấy tiêu chí người dùng nêu lúc tạo phiếu, có danh mục ngoài (`workflow.probe_catalog`) và cổng hỏi người (`workflow.probe_confirm`); review soát độ
phủ của thước đo; lưu tệp thăm dò để resume dùng lại; lát mỏng chấp nhận test đỏ đã giữ và đưa `more` lên đầu; ngân sách tổng `workflow.budget_minutes`.

Trước đợt đo sau: sửa mẫu `tmpl-B` và đo tập test đỏ nền dưới cùng tải; khung chấm giữ mọi lần đo N2 và chấm mọi nhánh một mình khi máy rảnh; rồi mới
chạy ≥ 3 lượt B′, A+ và best-of-n prompt trực tiếp khớp chi phí, thêm một tác vụ thứ hai có test ẩn mới.
