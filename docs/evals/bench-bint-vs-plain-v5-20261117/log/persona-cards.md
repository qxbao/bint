# Thẻ người dùng (agent không thấy; Opus chỉ trả lời từ đây)

## T1 — "Thư mục dữ liệu của bint trên máy tôi cứ phình to mãi, làm cho nó tự dọn bớt mấy thứ cũ đi."
- Mục tiêu: lấy lại dung lượng.
- Điều không muốn: mất việc đang làm dở, mất thứ còn cần.
- "Giữ bao lâu?": khoảng vài tháng.
- "Tự động hay chạy lệnh?": sao cũng được, miễn an toàn.
- "Có cần xem trước không?": có thì tốt.
- Mọi câu kỹ thuật khác: "tôi không rành, bạn tự quyết cho an toàn."

## T2 — "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi."
- Mục tiêu: không để bí mật nằm ở bất kỳ chỗ nào bint lưu.
- Điều không muốn: bint chạy hỏng hay chậm hẳn đi.
- "Có cần bật tay không?": không, nên mặc định bật.
- "Che những loại nào?": mấy loại hay gặp như mật khẩu, token, khoá API.
- Câu kỹ thuật khác: "bạn tự quyết."

# Giao thức (mục 6.1 của plan v2)
- Chỉ đọc tin nhắn cuối của agent / đầu ra của Bint; trả lời bằng lời thường từ thẻ.
- Chạy NGUYÊN VĂN lệnh được bảo chạy. Thử dùng một lần khi agent báo xong; lỗi thì dán nguyên đầu ra + "chạy thế này thì nó báo vậy".
- Không gợi ý kỹ thuật, không nêu tệp/khoá/hàm, không sửa tệp, không chạy test.
- Định dạng máy móc hộ = 1 "thao tác kỹ thuật". Không làm được kể cả có định dạng hộ = 1 "kẹt" + "tôi không biết làm cái này, bạn xử lý giúp".
- Trần: 4 tin nhắn người dùng / lượt (không tính lệnh chạy nguyên văn); 90 phút / lượt.
