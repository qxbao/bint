# Bằng chứng đợt tiếp theo

`/probe` thay repo/temp root của phép thử, `/home/user` thay đường dẫn cá nhân. Không copy auth.json, config.toml/provider values hoặc transcript người dùng đầy đủ. Các RPC chỉ initialize/hooks/list/skills/list/config state của profile tạm; không có model thread. Hash trust không phải credential.

`sessions.json` có đúng 15 thread ID thật, gồm 3 lượt không hoàn tất (Ctrl-C, timeout, auth 401). `r11-resume.stderr` là lỗi trước khi tạo session, không tính quota. Wrapper `codex-observed.py` trong phép thử eval chỉ đọc prompt-input/auth status rồi exec đúng Codex binary và đối số, không thay kết quả agent và không dùng `--agent-command`.

`profile-observed` là metadata trước từng lượt eval; một extractor đầu tiên bắt nhầm bullet tool thành skill, giữ field đó dưới tên `unscoped_bullet_names_initial_extractor` và không dùng để kết luận. `profile-catalogue-recheck.json` dùng CLI thật không chạy model với cùng constructor profile trên workdir giữ lại; null danh sách nghĩa là không có skill được liệt kê. Canary context/skill có đối chứng dương ở source profile, âm ở cả ba profile eval.

`hook-failure-payloads.json` giữ tool_use_id, stdout thật và exit_code đọc từ transcript khi hook chạy (khi có). `outcome-replay.json` được gắn nhãn replay; không sửa dữ liệu cũ đã ingest. `go-test-first-failure.txt` và `go-test-race-first-failure.txt` giữ lỗi timing test mới; tên không có `first-failure` là kết quả cuối sau sửa.

Hai tệp stderr metadata project trust được bỏ dòng trống cuối tệp để Git diff sạch; nội dung lỗi/cảnh báo không đổi.
