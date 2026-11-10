-- P7: tiến hoá skill (L2, mục 10.5). Đề xuất skill dùng cùng bảng đề xuất (level L2,
-- target_type skill). Thêm loại lần đối chiếu để phân biệt kiểm tra hồi quy khi đổi mô
-- hình (mục 12.6) với đối chiếu trước khi áp dụng: kết quả hồi quy không bao giờ được dùng
-- làm căn cứ áp dụng đề xuất.
ALTER TABLE evolution_evaluations ADD COLUMN kind TEXT NOT NULL DEFAULT 'probe' CHECK (kind IN ('probe','regression'));
