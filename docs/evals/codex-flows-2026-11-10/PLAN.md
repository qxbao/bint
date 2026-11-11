# Kế hoạch kiểm chứng Codex thật — 2026-11-10

Đặc tả là yêu cầu F1–F7 của người dùng trong phiên này. Thực hiện trực tiếp trên `fix/codex-flows` từ `origin/main` (`2764e8f`); không merge/push nhánh sửa. Mỗi vấn đề một commit tiếng Việt. Giữ hành vi Claude, test và gate.

- [x] Nền: fetch/merge vào `duc`, kiểm tra Go, đọc adapter và pilot; phân biệt payload dựng với CLI thật.
- [x] F1: init local/project, idempotent, gỡ hook/MCP giữ cấu hình khác, doctor trước trust. User scope chưa thử runtime để tránh sửa home người dùng; không nâng nhãn từ source/test.
- [x] F2: bắt stdin/stdout/JSONL Codex thật trong Git repo tạm; shell, patch add/update/delete/move, deny và MCP.
- [x] Sửa timeout SessionEnd: fixture cảnh báo thật → test đỏ → sửa → test xanh; commit riêng.
- [x] F3: tái hiện cite bị policy never chặn; thêm quyền riêng cite_knowledge vào init và executor eval, doctor cảnh báo; CLI thật xác nhận citation.
- [x] Fixture replay: kiểm tra tên tool, đường dẫn và chuẩn hoá JSONL; giữ nguyên field payload, chỉ che đường dẫn/định danh riêng.
- [x] F4/F5 (trong ngân sách; các trường hợp chưa chạy ghi ở README): b-do thật (planner/implementer/reviewer), đối chiếu role, token, giới hạn và chỉ đọc; protected/approve/resume nếu ngân sách cho phép.
- [x] F6: 1 case × 1 repeat × 3 arm strict; không dùng agent-command, lưu log/cấu hình/đăng nhập và citation.
- [x] F7: đo kích thước AGENTS.md, kiểm tra ngữ cảnh đầu vào và tham chiếu riêng Claude trong skill gốc.
- [x] Kết thúc: nhãn bằng chứng từng kết luận, lỗi nguyên văn, gofmt/vet/test/race; cập nhật README/bảng khả năng nếu sai.

Ngân sách tối đa 15 session Codex thật. Các lỗi parse CLI trước khi tạo session không tính là lượt model. Dừng trước flow sau nếu không nạp được hook. Dùng sandbox workspace-write, approval never; bypass hook trust chỉ cho hook thử nghiệm đã đọc, từng invocation, không lưu bypass. Không yêu cầu thêm quyền hệ thống.

Điểm cần soi: hook bị bỏ qua do config không active; policy MCP chỉ cho phép citation; báo cost/turn thiếu phải là null; bước chỉ đọc phải được kiểm tra cây; `strict` không lấy lời giải/tri thức mới từ HEAD.

## Quyết định phát sinh

- Chỉ rõ binary Bint trong repo thử vì PATH của shell tìm một bản cũ, không sửa cài đặt người dùng.
- Duyệt trước path là lựa chọn `--path` tường minh của người; không tự suy đoán quyền từ stderr hay đánh dấu file chưa sửa thành đã sửa. Lựa chọn cũ không đổi.
- Không nhân đôi skill, không tự áp policy mới cho skill. Bảng khả năng phân biệt chính sách chỉ gọi tường minh với ẩn khỏi menu.
- Dừng gọi model sau session 15. Các trường hợp biên từ review dùng test cục bộ, không gọi là Codex runtime đã kiểm chứng.
- Giữ giới hạn nghiêm ngặt của kết luận strict: Git cô lập được nhưng user-level skill còn được nạp. Không nghiệm thu chống rò rỉ toàn môi trường.
