# Nghiệm thu tiếp F1–F7 — 2026-11-11

Tiếp yêu cầu người dùng nghiệm thu toàn bộ, trên `fix/codex-flows` từ `9a688b3`; không merge/push. Đợt hai tối đa 15 session model Codex thật, gộp quan sát; truy vấn schema/app-server/debug không chạy model không tính quota. Claude thật cần đăng nhập, đã hỏi người dùng khi auth status báo false; không tự login hoặc giả mạo Claude.

- [x] F1: hooks/list/config state thật trong profile tạm, trust từng phần/đủ/modified/disabled; doctor tương ứng; scope user trong home Codex cô lập; hướng dẫn init đúng harness.
- [x] F2: payload shell/patch/MCP đã có; bổ sung lỗi hook/timeout/malformed fail-open và test thất bại.
- [x] F3/F4: skill discovery + gọi thật đủ b-do/b-req/b-feat/b-review/b-bootstrap, explicit/implicit, policy false; đo MCP tự chọn và sự kiện có thật.
- [x] F5: workflow đơn lẻ, deliberate readonly violation, timeout, Ctrl-C/interrupted/resume; hết auth trong profile rỗng; role/mixed thật nếu có Claude auth.
- [x] F6: profile/config/skill cô lập có giữ auth; strict none/minimal/bint không tải user skill; sửa rồi chạy lại ba nhánh.
- [x] Fixture thực, từng lỗi commit Việt riêng, không đổi Claude mặc định/gate; rà soát và gofmt/vet/test/race cuối.

Chỉ dùng repo Git tạm. Profile cấu hình tại `<repo-tạm>/.codex/profile`, đặt CODEX_HOME đúng chức năng cho tiến trình con, không đổi biến của phiên người dùng. Chỉ chép file auth vào profile tạm khi thực sự cần chạy; mode 0600, loại khỏi Git/artifact, xoá khi xong. Không sao chép config/plugins/AGENTS người dùng; nếu backend cần cấu hình provider, chỉ chuyển phần provider cần thiết, không xuất nội dung riêng vào báo cáo. Không biến fixture/agent giả thành bằng chứng runtime.

Kết thúc: Codex 15 thread thật; mixed ghi blocked do Claude auth theo quyết định người dùng. Không tự hỏi đăng nhập lại. Các giới hạn còn lại được công khai trong README, không suy đoán hỗ trợ runtime.
