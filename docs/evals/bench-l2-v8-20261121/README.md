# Đợt 8: A / B / B′ trên L2 "chuyển thư mục dự án không mất gì" (2026-11-21)

Kế hoạch đăng ký trước: `plan-v8.md`. Thay đổi giữa chừng và sự cố: `prereg-and-incidents.md`. Báo cáo gốc của người chạy: `results/report.md`.
Dữ liệu thô: `D:\bench\bench-v8-20261121\` (repo, state, log từng lượt). Binary điều phối: `0e35d20`. Test ẩn và bộ kiểm soát chép ở `hidden/`, `controls/`
sau khi đợt này chấm xong; **đợt sau không được dùng lại bài L2** (mục "Bài học về đề").

## Kết luận

**Ba nhánh cùng 36/36, cùng qua N1 và N2: ma trận chạm trần nên đợt 8 không phân biệt được chất lượng giải.** Lỗi nằm ở đề: các ô của ma trận không độc
lập, một cơ chế duy nhất (ánh xạ đường dẫn mới về trạng thái cũ) làm xanh cả 36 ô, như lời giải giả ở bước hiệu chuẩn đã cho thấy (36/36).

Thứ đo được rõ là **các cải tiến quy trình của B′ sau đợt 7 chạy đúng thiết kế** (n = 1):

| | B′ đợt 7 | B′ đợt 8 | B đợt 8 | A đợt 8 |
|---|---|---|---|---|
| Kết quả kiểm chứng đầu tiên | phút 103 (57 %) | **phút 6,4 (3,7 % của 170)** | phút 117 | (đo khác, không so được) |
| Kiểm hẹp chạy được | 0 / 24 lần gọi | **18 (8 do Bint tự chạy)** | 0 | — |
| Tới review | nhờ 38 phút bù | **phút 94,6** | phút 120 | — |
| Điểm ở ảnh phút 60 | — | **36/36** | 30/36 | (đã xong ở phút 27,7) |
| Test repo đỏ mới (bỏ test so thời gian) | — | **0** | **1 hồi quy thật** | **1 hồi quy thật** |
| Token vào so với A | 7,7× | 6,86× | 7,58× | 1× |

Chi phí vẫn trượt tiêu chí ≤ 2,5× A. A rẻ và nhanh nhất nhưng để lại hồi quy mà nó không biết; B cũng vậy; B′ là nhánh duy nhất giao cây không có
hồi quy thật và có review (7 phát hiện high ngoài ma trận).

## Test repo đỏ, phân loại lại

| Nhánh | Test đỏ | Loại | Bằng chứng |
|---|---|---|---|
| A | `TestServiceMessageCLIInventory` | **hồi quy thật**: A thêm lệnh `bint state relink` mà không thêm hàng `cliRefs` | `results/A-1/gotest.txt` |
| A, B′ | `TestGateEvidenceAndBudget/perf_gate/…` | test so thời gian, đỏ khi máy bận (cũng đỏ khi chạy song song ở cây Bint gốc) | `results/*/gotest.txt` |
| B | `TestDiffTaskWorktreeShowsFilesOutsideTouched` | **hồi quy thật**: chạy riêng trên cây cuối của B vẫn đỏ ("không nằm trong repository Git", `task_diff_outside_test.go:86`); B sửa `internal/state/state.go` (+81/−38); test không có trong tập đỏ nền | chạy lại 2026-11-21, `results/B-1/gotest.txt` |
| B | `TestCleanupWorktreeReleasesGatewayBounded` | test so thời gian ("nhả treo làm chặn dọn 5,7 s"), từng đỏ dưới tải ở cây Bint gốc | — |

## Token của B′ theo bước (từ `turn.completed` trong log từng lượt)

Bắt đầu 05:58:41; ảnh phút 60 ≈ 06:59.

| Bước | Kết thúc | Phút (từ đầu) | Token vào |
|---|---|---|---|
| b-req | 06:02 | 3 | 0,97M |
| b-goal (thăm dò, song song với lõi) | 06:14 | 15 | 4,13M |
| b-feat lõi | 06:45 | 46 | 32,75M |
| b-feat mở rộng | 07:12 | 73 | 12,49M |
| b-review | 07:42 | 103 | 3,92M |
| b-feat vòng sửa (bị cắt ở trần lát 30 phút, không có `turn.completed`) | 08:12 | 134 | ≈ 24,1M (suy ra: tổng 78,40M − 54,26M) |

Khoảng 52 % token vào (≈ 40,5M) tiêu sau khi lõi xong và thước đo đã đủ cho test ẩn. Phần đó không phí hoàn toàn: review tìm 7 lỗi high ngoài ma trận
(ví dụ "bản clone ở đường dẫn cũ nhận trạng thái của kho khác", hook dùng trạng thái chưa đăng ký sau nâng cấp) và vòng sửa xử lý chúng. Nhưng lượt mở rộng
(12,5M) chạy khi thước đo thăm dò đã xanh, tức không còn mục tiêu đo được.

## Lỗi của Bint lộ ra ở đợt này (đã sửa sau đợt, chưa đo lại)

1. **Vòng sửa sau review bị cắt ở trần lát 30 phút** dù `workflow.budget_minutes: 170` còn khoảng 35 phút: `budgetLeft` bỏ qua vòng sửa và
   `sliceTimeoutFor` giữ trần lát. Sửa: vòng sửa dùng phần còn lại của ngân sách tổng (`fixRoundLeft`, `internal/workflow/slice.go`).
2. **Lý do dừng sai**: "b-feat: quá workflow.timeout_minutes (180 phút)" cho lượt thật ra bị cắt ở 30 phút. Sửa: ghi trần thật đã giao (`stepRun.timeout`,
   `timeoutReason`).
3. **Ghi chú "kết quả kiểm chứng đầu tiên" chia cho 180 phút** thay vì ngân sách 170 phút. Sửa: dùng `workflow.budget_minutes` khi có.
4. **Không dừng sớm**: thước đo thăm dò xanh ngay trên lõi mà vẫn chạy lượt mở rộng. Sửa: bỏ lượt mở rộng, kiểm chứng lại một lần trên cây đã gộp thăm
   dò rồi vào review (`bprimeAfterCore`); review vẫn soát độ phủ của thước đo.

## Bài học về đề

- Cổng hiệu chuẩn của `plan-v8.md` chỉ đòi bản gốc ≤ 30 % ô xanh và mọi ô đạt được. Thiếu cổng **độc lập của ô**: một bản sửa đơn lẻ, rẻ nhất có thể
  (lời giải giả), không được làm xanh quá khoảng một nửa số ô. Ở L2 lời giải giả xanh 36/36, nên ma trận thực chất đo một bit.
- L1 (đợt 6–7) khác: mỗi loại bí mật cần nhận dạng riêng, nên ô độc lập hơn và điểm trải từ 20 tới 30.
- Đề đợt 9 phải qua cổng mới này ở bước hiệu chuẩn. Kế hoạch đợt 9 nằm ngoài repo (`D:\bench\bench-v9-20261121\plan-v9.md`) cho tới khi đợt đó chấm xong.

## Lệch khỏi kế hoạch (đã ghi trong `prereg-and-incidents.md`)

- Một lượt mỗi nhánh, bỏ A+ (theo yêu cầu người dùng); mọi so sánh là mô tả.
- Bản gốc trượt N1.5 (repo mới ở đường dẫn cũ đọc được tác vụ cũ, tức trộn dữ liệu) nên được miễn N1 ở cổng hiệu chuẩn; ba nhánh đều sửa được lỗi này.
- D8 đổi từ "tri thức chờ duyệt" (xanh sẵn vì đi theo cây repo) sang "đánh giá của người trên lượt chạy"; N1 vẫn kiểm tri thức chờ duyệt.
- Mẫu `tmpl-B`: chỉ đổi hai kiểm tra bản skill đã cài để kiểm băm theo `core.lock` của binary điều phối; mã, skill nhúng và golden giữ từ `e41bb69`.
- Cột "kiểm chứng đầu" của A lấy từ lần `go test` đầu quan sát được, khác cách đo của B/B′.
