# Yêu cầu
## Mục tiêu và phạm vi
Bản phác: giảm nguy cơ mật khẩu/token lưu trong log Bint. Chưa chốt phạm vi dữ liệu cũ và các bản sao; chờ Q-1/Q-2 trước kế hoạch đầy đủ. Không sửa mã ở bước này.
## Yêu cầu chức năng
1. Che bí mật có nhãn rõ ở ranh giới ghi bản ghi đã chốt, giữ khả năng chẩn đoán và không đổi dữ liệu thực thi.
2. Không tự xoá hoặc ghi đè dữ liệu cũ khi chưa có lựa chọn của người dùng.
## Tiêu chí chấp nhận
- [ ] AC-1: Các bản ghi mới thuộc phạm vi đã chốt che mật khẩu và token có nhãn rõ trước khi ghi; nội dung thường và luồng thực thi vẫn hoạt động.
- [ ] AC-2: Kiểm tra mục tiêu TestBintSecretPersistenceGoal xanh.
## Kiểm tra mục tiêu
Chuẩn bị: test cô lập, cài mật khẩu/token giả có dấu nhận dạng vào stdout/stderr agent và lệnh kiểm chứng, lỗi hook, hook trace, payload spool; thêm goal/criteria/lệnh verify, requirement/review giả để đo bản sao task. Dựng bản ghi cũ riêng để kiểm chính sách sau khi trả lời Q-1.
Hành động: chạy workflow với agent giả, hook bật trace, nạp spool vào SQLite và đồng bộ task sang .bint/tasks; đọc lại qua giao diện xem/xuất hiện có.
Quét/đo: quét toàn bộ state dir và .bint/tasks trong repo thử nghiệm, gồm log, trace, spool còn lại, tài liệu task, SQLite và WAL; kiểm cả dữ liệu đọc logic và byte thô. Báo đường dẫn/bảng còn dấu bí mật, không in giá trị bí mật.
Điều kiện đạt: các nhóm đã chốt không còn bí mật thô trong bản ghi mới, dấu văn bản thường còn nguyên, lệnh thực thi nhận đúng đầu vào, hook không lỗi/chậm vượt ngân sách hiện có. Bản ghi cũ tuân thủ lựa chọn Q-1; nhóm ngoài phạm vi phải được báo rõ trong kết quả thay vì bỏ qua.
Tên test và tệp dự kiến: `TestBintSecretPersistenceGoal` tại `internal/cli/secret_persistence_e2e_test.go`, nằm trong lệnh verify hiện có `go test -count=1 -timeout 30m ./...`. Đây là đặc tả dự kiến, chưa viết/chạy test.
## Ngoài phạm vi
Không sửa log của nhà cung cấp agent, shell history, kho mật khẩu gốc hoặc lịch sử Git/backup ngoài Bint; bí mật ở đó vẫn tồn tại. Xử lý lịch sử lưu của Bint và bản sao task chưa bị loại khỏi mục tiêu, đang chờ Q-1.
## Câu hỏi mở
- [x] Q-1: Bạn muốn bảo vệ cả dữ liệu Bint đã lưu trước đây và các bản sao trong phiếu/bản xuất, hay chỉ log được tạo từ nay? — Trả lời: Bảo vệ hết, cả dữ liệu đã lưu trước đây lẫn các bản sao trong phiếu và bản xuất. Tôi không muốn bí mật nằm ở bất kỳ chỗ nào bint lưu.
- [x] Q-2: Bạn muốn tự động che ngay khi ghi, hay được xem trước và chủ động bật; thông tin nào cần giữ nguyên để công việc và việc chẩn đoán không bị ảnh hưởng? — Trả lời: Tự động che ngay khi ghi, mặc định bật, tôi không muốn phải bật tay. Che mấy loại hay gặp như mật khẩu, token, khoá API. Giữ lại gì thì bạn tự quyết, miễn bint không chạy hỏng hay chậm hẳn đi.
