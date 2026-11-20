# Yêu cầu

## Mục tiêu và phạm vi

Chặn mật khẩu, token và khóa API lọt vào mọi bản ghi và bản sao chẩn đoán do Bint tạo, từ lần ghi đầu tiên đến nạp lại, sao chép, xem và xuất. Thực hiện nhiều lượt trên cùng phiếu, kiểm tra toàn bộ mục tiêu khi kết thúc.

## Hiện trạng

Theo `baseline.md`, thư mục trạng thái có 7 tệp `runtime/` (3043 byte), 3 tệp `spool/` (1308 byte), 1 tệp `state.sqlite` (327680 byte) và 3 tệp `backups/` (4224 byte). Đây là số đo dung lượng, chưa đo số giá trị bí mật có sẵn. Mã hiện có che một số lệnh hook và lỗi gateway, trong khi trace hook và log tiến trình có đường ghi trực tiếp.

## Yêu cầu chức năng

1. Che giá trị nhạy cảm theo ngữ cảnh và dạng token trước khi ghi hay cắt ngắn dữ liệu chẩn đoán. Áp dụng cả cho JSON lồng nhau, luồng stdout/stderr và dữ liệu cũ được đọc để tạo bản ghi hay bản xuất mới.
2. Giữ định dạng có thể đọc bằng máy, dữ liệu chẩn đoán hữu ích và đường xử lý thật của Bint. Tệp bắt buộc phải chứa đầu vào thực thi trong lúc chạy chỉ tồn tại ở khu vực vận hành riêng, dọn khi lượt chạy kết thúc và không thành hiện vật chẩn đoán.

## Tiêu chí chấp nhận

- [ ] AC-1: Spool, trace hook, nhật ký hook và lịch sử sự kiện SQLite có 0 giá trị mật khẩu hoặc token thô trong dữ liệu mới ghi, kể cả khi nạp lại sự kiện hoặc cách ly bản ghi hỏng.
- [ ] AC-2: Log workflow, agent, kiểm chứng và MCP có 0 giá trị mật khẩu hoặc token thô khi ghi tiêu đề lệnh, stdout, stderr hoặc đuôi log, kể cả khi giá trị nằm vắt qua các lần ghi; lệnh và mã thoát giữ đúng hành vi.
- [ ] AC-3: Nội dung mô tả, tiêu chí, báo lỗi, review và bản chiếu hoặc đồng bộ phiếu có 0 giá trị mật khẩu hoặc token thô trong dữ liệu chẩn đoán mới ghi hoặc xuất; phiếu vẫn đọc, lint và đồng bộ được.
- [ ] AC-4: Trace, tóm tắt, kết quả và dữ liệu chẩn đoán dẫn xuất từ eval, skill test và evolve có 0 giá trị mật khẩu hoặc token thô trong dữ liệu mới lưu hoặc xuất; kết quả chấm và trạng thái vẫn đúng.
- [ ] AC-5: Kiểm tra mục tiêu `TestBintDoesNotPersistCredentials` xanh.

## Kiểm tra mục tiêu

Loại: gán biến môi trường (trần, nháy đơn/kép), cờ mật khẩu/token/secret/API key/auth (dấu cách hoặc `=`), trường password/passwd/client_secret/access_token/refresh_token/API key trong JSON/YAML/log, header Authorization Bearer/Basic và X-API-Key, cookie phiên, URL/DSN có userinfo hoặc query bí mật, token nhận diện bằng định dạng (JWT, GitHub/GitLab PAT, OpenAI/Anthropic, Google OAuth/API key, Slack, AWS và Stripe), giá trị giả đã biết từ môi trường kiểm thử hoặc danh mục credential MCP. Nguồn gồm 4 nhóm mẫu của `internal/detect/detect.go`, tên credential trong `internal/iso/env.go` và `internal/mcpcat/catalog.go`, cùng các dạng xác thực phổ biến kể trên. Dùng mẫu riêng có nháy/escape, URL encode, giá trị ngắn có nhãn và dòng bị chia mảnh.

Nơi: (1) spool thường/denial/rejected, hook trace/lỗi, guard/subagent và SQLite lịch sử; (2) log stdout/stderr, tiêu đề/đuôi log của workflow, verify/setup/agent và gateway; (3) YAML/Markdown/JSON của phiếu, báo lỗi/review, bản chiếu Git và dữ liệu vừa nhập hoặc xuất; (4) log và dữ liệu dẫn xuất eval/skill test/evolve, trace ngữ cảnh, câu trả lời, báo cáo, tệp tạm và SQLite liên quan. Chạy mọi cặp loại × nhóm thành ô riêng, với dấu không trùng giữa các ô và đối chứng vô hại.

Chuẩn bị và hành động. Cô lập repo, worktree, state, thư mục gateway/profile và temp. Gọi các đường CLI/API hiện có với fixture cục bộ: (1) hook bật trace, payload lồng hợp lệ/lỗi, denial và spool phiên bản cũ, kiểm trước và sau nạp; (2) tiến trình giả in dữ liệu ở argv, stdout/stderr và đuôi, chia mẫu qua nhiều lần ghi, chạy nhánh thành công/lỗi/hủy và truy vấn log MCP; (3) tạo/sửa phiếu, ghi tài liệu/lỗi/review, sync hai chiều với fixture ledger cũ, xem JSON/văn bản; (4) chạy eval/skill test/evolve với agent giả, lấy báo cáo, ghi kết quả và dựng đầu ra dẫn xuất từ log cũ. Đặt nguồn thô cho ca tương thích ngoài thư mục đích, chụp cả lúc ghi và sau khi luồng hoàn tất. Đường gọi, dữ liệu đầu vào và điểm đo đều dùng giao diện có sẵn để test chạy được từ đầu.

Quét/đo. Quét đệ quy state, repo/worktree, ledger, bản xuất, vùng temp và gateway, gồm tệp tạm và bản sao chẩn đoán. Quét tên, byte thô, JSON/YAML đã giải mã, giá trị cột SQLite và DB/WAL khi còn mở. Tìm dấu ở dạng gốc, escape hoặc mã hóa tương ứng. Bằng chứng mỗi đích thực sự được tạo/đọc, nội dung đối chứng và ID/trạng thái/kết quả đọc được. Báo nhóm, loại và đường dẫn/bảng khi còn sót, không in dấu bí mật.

Điều kiện đạt. 0 ô có dấu bí mật trong dữ liệu chẩn đoán, mọi ô có điểm đo và đối chứng qua. `TestBintDoesNotPersistCredentials` tại `internal/cli/credential_persistence_test.go` nằm trong lệnh kiểm chứng của phiếu.

## Ngoài phạm vi

- Dọn hoặc ghi đè log, DB, thư mục sao lưu và lịch sử Git đã có thuộc phần mở rộng. Người dùng vẫn có thể thấy bản cũ nếu đọc trực tiếp trên đĩa. Bản ghi hay bản xuất mới sinh từ chúng phải được che.
- Kho credential vận hành (`secrets.env`, `auth.json`, token điều khiển trong `gateway.json`), cấu hình/lệnh nguồn và tệp nhập hay sao lưu nguyên văn là đầu vào có chủ đích. Các tệp đó và lịch sử gốc do harness ghi theo cơ chế riêng vẫn có thể chứa credential. Bất kỳ phần nội dung nào Bint trích ra để chẩn đoán phải được che.

## Câu hỏi mở

Không có.
