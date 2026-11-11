# Bằng chứng chọn lọc

15 session Codex thật được liệt kê trong `sessions.json`. `codex debug prompt-input`, init/doctor và lỗi khởi động không gọi model nên không tính vào 15 session.

JSONL/stdin/stdout giữ field thật, chỉ thay đường dẫn riêng bằng `/probe`, `/state`, `/home/user`. Log agent bỏ cặp sự kiện đọc skill plugin cấp người dùng (nội dung không liên quan phép thử); không sửa kết quả tool/usage còn lại. Các tệp `*.stderr` giữ lỗi nguyên văn sau che đường dẫn. Bản init-first bỏ khoảng trắng căn cột ở cuối hai dòng; stdin/stdout JSON của hook không bị chuẩn hoá khoảng trắng. Không xuất auth/config/transcript người dùng đầy đủ. Các corpus tri thức trong repo dùng một lần là fixture tổng hợp, không phải tri thức đã được người duyệt của dự án thật.

`skill-policy-summary.json` và `context-catalogue.json` là kết quả phân tích prompt-input, không phải câu trả lời của model. `injected-developer-context.json` chọn đúng message developer từ transcript thật; không khẳng định model đã sử dụng nó. Fixture dùng trực tiếp trong Go test nằm tại `internal/harness/codex/testdata/codex-0.167/` và `internal/eval/testdata/codex-0.167/`.
