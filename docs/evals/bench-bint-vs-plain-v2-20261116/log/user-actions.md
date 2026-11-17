## B-T1
- 2026-11-16T10:20:00Z Lượt #0 kết thúc "✓ Hoàn thành" (done), diff rỗng. Người dùng thử dùng: không có gì để dùng.
- Chạy lệnh "Chi tiết" Bint in ra (nguyên văn): `bint task show 01M7HWZ3X8DB74YEDYSGN43Y3W` → thấy câu hỏi của b-feat trong mục "Lỗi / lần thử thất bại".
- Quyết định của chủ dự án (giữa lượt): cho người dùng hỏi tiếp qua kênh gần nhất Bint cho là `bint run b-do` mới.
- Tin nhắn người dùng #1 (qua `bint run b-do` mới): "Lúc nãy tôi nhờ bint làm cho thư mục dữ liệu của nó tự dọn bớt mấy thứ cũ, nó báo xong mà chẳng thay đổi gì, trong phần chi tiết thì hỏi tôi muốn chỉ dọn log quá 30 ngày hay dọn thêm gì. Tôi không rành, bạn tự quyết cho an toàn, giữ khoảng vài tháng thôi."

## A-T1
- 10:20Z Lượt #0 xong. Agent hướng dẫn: "Xem trước: `bint clean --dry-run`; Dọn ngay: `bint clean`"; có ghi "chưa cài lại binary trên máy".
- Thử dùng (nguyên văn, bint đang cài trên máy = binary đóng băng): `bint clean --dry-run` → exit 2 "lỗi: lệnh lạ \"clean\" cho \"bint\"".
- Tin nhắn người dùng #1: "Tôi chạy bint clean --dry-run thì nó báo:
lỗi: lệnh lạ "clean" cho "bint"
Xem `bint --help`
Chạy thế này thì nó báo vậy."

## A-T2
- 10:21Z Lượt #0 xong. Agent báo: đã che mật khẩu/token trước khi ghi log tiến trình, trace hook và codegraph; log cũ chưa được làm sạch; suite đầy đủ chưa xác minh được. Không hỏi gì, không đưa lệnh nào để dùng (tự động).
- Thử dùng: không có lệnh nào để chạy theo hướng dẫn. Lượt A-T2 kết thúc (0 tin nhắn người dùng).
- 10:22Z Lượt #1 xong. Agent bảo chạy nguyên văn: `cd …/A-T1` + `go build -o "$(command -v bint)" ./cmd/bint`, rồi `bint clean --dry-run` trong dự án đã init.
- Chạy nguyên văn trên "máy người dùng" giả (tmp/user-A-T1: bin riêng đầu PATH, dự án mẫu đã init, state riêng) → build exit 0; `bint clean --dry-run` exit 0: "Sẽ dọn 0 tệp (0 byte)."
- Lần thử dùng không lỗi → lượt A-T1 kết thúc. Tổng: 1 tin nhắn người dùng, 1 lệnh chạy nguyên văn, 0 thao tác kỹ thuật, 0 kẹt.
- 11:11Z Lượt #1 (tác vụ 01M7HXY1Y37AX1MTRMKH441BPV) done: b-req 6m17s, b-feat 26m46s, b-review 3m59s (1 phát hiện, 0 nghiêm trọng), kiểm chứng đầy đủ qua.
- Bint không in hướng dẫn dùng nào (chỉ dòng "Chi tiết: bint task show …"). Thử dùng: không có lệnh nào được bảo chạy → lượt B-T1 kết thúc (xử lý như A-T2). Tổng: 1 tin nhắn người dùng, 1 lệnh chạy theo dòng "Chi tiết", 0 thao tác kỹ thuật, 0 kẹt; 1 lần "done rỗng" ở lượt #0.

## B-T2
- 11:28:44Z Lượt #0 kết thúc exit=1, tác vụ blocked: "còn 1 phát hiện nghiêm trọng sau 3 vòng review (workflow.max_review_rounds = 2): cần người xem review.md". Đã chạy 85,5 phút (trần 90).
- Người dùng low-tech không đánh giá được review.md → 1 lần "kẹt". Bint chỉ in "Chi tiết: bint task show … · bint run status …", không in lệnh nào để tiếp tục (không có `bint run resume`); còn 4,5 phút trước trần. Theo mục 6.1 ("nếu Bint không cho cách nào thì lượt dừng ở đó") lượt B-T2 dừng tại đây. Tổng: 0 tin nhắn người dùng, 0 thao tác kỹ thuật, 1 kẹt.
