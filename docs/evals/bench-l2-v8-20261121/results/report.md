# Kết quả bench v8

A, B và B′ mỗi nhánh một lượt. Với n = 1, chỉ đối chiếu mô tả và chưa áp dụng các tiêu chí suy luận ở §7.

Binary điều phối Bint là commit 0e35d20. A, B và B′ đều dùng Codex gpt-6.2-sol, effort high, cửa sổ ngữ cảnh 872.000 và provider codex-lb.

| Nhánh | Điểm | N1 | N2 | Phút | Token vào | Token cache | Token ra | Kết thúc |
|---|---:|---|---|---:|---:|---:|---:|---|
| A | 36/36 | Đạt | Đạt (1,05×) | 27,7 | 11.426.303 | 11.241.216 | 55.999 | Tự kết thúc |
| B | 36/36 | Đạt | Đạt (1,05×) | 180,0 | 86.625.815 | 85.435.904 | 188.253 | Chạm trần lượt |
| B′ | 36/36 | Đạt | Đạt (1,03×) | 134,4 | 78.401.440 | 77.308.032 | 175.041 | Vòng sửa sau review hết thời gian |

Token vào đã gồm token cache. Chi phí USD chưa tính vì thiếu bảng giá cho provider đã dùng.

Điểm cuối bằng nhau. B và B′ lần lượt dùng 7,58× và 6,86× token vào của A. B′ dùng ít hơn B khoảng 9,5% token vào và 25,4% thời gian tường. B′ đã đạt đủ điểm, qua N1 và N2 ngay ở ảnh phút 60. A đã kết thúc trước mốc đó.

| Nhánh | Phút 60 | Phút 120 | Phút 180 | Thay đổi đầu (giây) | Kiểm chứng đầu (giây) | Review (giây) | Kiểm hẹp |
|---|---:|---:|---:|---:|---:|---:|---:|
| A | — | — | — | 267,4 | 159,5 | — | 0 |
| B | 30/36 | 36/36 | 36/36 | 870,9 | 7.041,0 | 7.193,1 | 0 |
| B′ | 36/36 | 36/36 | — | 312,7 | 382,0 | 5.678,8 | 18 (8 tự động) |

Mốc thay đổi là lần đầu quan sát được diff qua Git, có thể gồm test hoặc tài liệu. Mốc kiểm chứng của A lấy từ lần đầu quan sát lệnh go test hoàn tất. Với B và B′, mốc này lấy từ bản ghi kiểm chứng, kiểm hẹp hoặc kiểm nền của Bint. Lần kiểm tra mục tiêu của B ghi kết quả ở giây 3.054. Các ảnh trung gian được chụp trong khi chạy và chấm sau khi cả ba lượt dừng.

Trong lượt B′, kết quả kiểm chứng đầu tiên xuất hiện ở phút 6,4, khoảng 3,7% ngân sách 170 phút. B′ vào review ở phút 94,6. Lượt sửa duy nhất sau review kéo dài 30 phút theo cấu hình lát. Thông báo kết thúc của Bint ghi trần chung 180 phút dù lượt chỉ chạy 134,4 phút.

## Go test

Cả ba cây build được. go test ./... thoát mã 1 ở cả ba cây.

- A đỏ ở TestServiceMessageCLIInventory do thiếu ánh xạ cho lệnh state relink, và ở subtest đo hiệu năng của TestGateEvidenceAndBudget. [Log A](A-1/gotest.txt)
- B đỏ ở TestDiffTaskWorktreeShowsFilesOutsideTouched và TestCleanupWorktreeReleasesGatewayBounded. [Log B](B-1/gotest.txt)
- B′ đỏ ở cùng subtest đo hiệu năng của TestGateEvidenceAndBudget như A. [Log B′](Bp-1/gotest.txt)

Các lỗi trên không xuất hiện trong bảng nền của mẫu tương ứng. Một số test phụ thuộc thời gian đã đỏ ở các lần dựng thử mẫu, nên chênh lệch với lần đo nền chưa đủ để quy mọi lỗi cho thay đổi của agent.

## Hiệu chuẩn

- Bản gốc 48dbfad trượt N1.5 và được miễn N1 ở bước hiệu chuẩn. Chấm N1 đầy đủ ở cả ba nhánh.
- Bản gốc đạt 9/36 trong ba lần. Bản chuyển trạng thái thủ công đạt 36/36 trong ba lần. N2 đạt ngưỡng thời gian ở cả ba lần cho bản gốc, với mã lỗi sau chuyển được ghi riêng.
- D8 trong ma trận đo đánh giá lượt chạy. N1 vẫn kiểm tra cả tri thức chờ duyệt.
- Mẫu B/B′ giữ skill nhúng của bài gốc. Hai test kiểm độ toàn vẹn của skill cài đặt đối chiếu với core.lock do binary điều phối phát hành. Tập đỏ nền của B/B′ kế thừa lần chạy đầy đủ trên cây cũ và chạy lại riêng các kiểm tra skill vừa đổi.

[Dữ liệu tổng hợp](summary.json) · [Hiệu chuẩn](../controls/l2-calibration.json) · [Nhật ký và ngoại lệ](../prereg-and-incidents.md)
