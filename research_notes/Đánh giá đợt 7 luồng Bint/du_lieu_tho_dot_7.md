# Dữ liệu thô đợt 7 (A / B / B′ trên L1) đối chiếu với README

Quy ước: **[Đ]** đã kiểm trên tệp thô (có đường dẫn); **[B]** lấy từ README/prereg, chưa kiểm được; **[S]** suy luận của người ghi chú.
Viết tắt đường dẫn:
- `R7` = `D:\bench\bench-v7-20261120`
- `DOC` = `D:\Projects\bint\docs\evals\bench-abbp-v7-20261121`
- `TB` = `R7\runs\B-1\state\repo-c74be3073719\runtime\tasks\01M7WP58SM0M3942QZRGHFH6YS`
- `TP` = `R7\runs\Bp-1\state\repo-a253372825da\runtime\tasks\01M7WP81F0XSXRJXQVQR9KCMZE`
- `R6` = `D:\bench\bench-bint-v6-20261120`
- Giờ trong `state.json` là UTC (Z); giờ địa phương = UTC+7. B-1 bắt đầu 14:35:53Z (21:35:53), Bp-1 14:37:24Z (21:37:24), A-1 15:22:23Z (22:22:23).

Tóm tắt các chỗ README sai hoặc thiếu (chi tiết ở từng mục):
1. "Cả ba còn lọt khoá PEM ở mọi nơi" sai với A: A-1 sạch `private_key` 4/6 ô.
2. "`bint task check` 0 lần, agent bỏ qua chỉ dẫn" sai: agent gọi 24 lần, cả 24 lần Bint tự bỏ qua vì sandbox Codex chặn ghi `.git/objects`; riêng lượt lõi thì đề bài không hề chứa chỉ dẫn đó.
3. "Proxy cắt kết nối làm mất lượt b-feat 28 phút": lượt đó thực ra đã `turn.completed` bình thường; Bint coi một dòng "Reconnecting 2/5" ở đầu lượt là lỗi.
4. Hai test "đồng bộ skill" đỏ ở B và B′ là lỗi dựng mẫu `tmpl-B` (đỏ ngay ở commit gốc), không phải hồi quy; vì thế kiểm chứng của B/B′ không thể xanh từ đầu.
5. N2 của A-1 (19,3×) và B-1 (11,7×) không khớp tệp còn lại (9,62× và 6,61×).
6. "8 đỏ (≥ 4 hồi quy)" của B: chỉ chứng minh được 2 hồi quy thật.
7. "Hết ngân sách trước khi sửa xong review": 7/8 phát hiện đã có bản sửa trong cây cuối.
8. Cột "A đợt 6: 120 phút (cắt)" sai so với đợt 6 (A tự báo xong sau ≈ 84 phút).
9. Thăm dò hộp đen của B′ không thấy kế hoạch/mã nhưng có nhận 5 tiêu chí do b-req viết.

## Ma trận test ẩn có đúng A 22/36, B 30/36, B′ 30/36 không? Ô nào hỏng? Phép tính "mỗi nhóm 6/7; S5 0/1"

### Takeaway
Ba điểm số đúng với tệp thô và phép cộng 5 × 6 + 0 = 30 khớp. B và B′ hỏng đúng cùng 6 ô (`private_key` ở S1–S6); nhưng câu "cả ba còn lọt khoá PEM ở mọi nơi" của README sai với A: A-1 che được PEM ở S1, S2, S4, S5.

### Cited Findings
- [Đ] A-1: 22/36; S1 7/7, S2 7/7, S3 0/7, S4 7/7, S5 1/1, S6 0/7; theo loại: `private_key` 4/6, sáu loại còn lại 3/5 — `R7\runs\A-1\grade.json` (`hidden_per_group`, `hidden_per_type`), `R7\runs\A-1\hidden.json.matrix.json`.
- [Đ] B-1: 30/36; S1–S4 và S6 đều 6/7, S5 0/1; `private_key` 0/6, sáu loại còn lại 5/5 — `R7\runs\B-1\grade.json`.
- [Đ] Bp-1 cuối: 30/36, cùng hình dạng từng nhóm và từng loại với B-1 — `R7\runs\Bp-1\grade.json`; lúc 9100 s cũng 30/36 cùng hình dạng — `R7\runs\Bp-1\grade-failed-9100s.json`.
- [Đ] Dòng `hidden_tail` của B-1 và Bp-1 giống nhau: mỗi nhóm S1…S6 chỉ lộ `['private_key']`; của A-1: S1, S2, S4, S5 lộ `[]`, S3 và S6 lộ đủ 7 loại — trường `hidden_tail` trong ba `grade.json` trên.
- [Đ] 36 ô = 7 loại × 5 nhóm (S1–S4, S6) + 1 ô S5 (chỉ `private_key`); tức ma trận chỉ đếm ô đã lộ ở bản gốc — `R7\runs\A-1\hidden.json.matrix.json` (khoá `cells`), đối chứng `R7\controls\l1-base.json`.
- [Đ] Test ẩn đợt 7 trùng từng byte với đợt 6 (`diff -q` không ra gì) — `R7\hidden\hidden_l1.py` và `R6\hidden\hidden_l1.py`. Loại `private_key` được cài dạng `-----BEGIN PRIVATE KEY-----\n…\n-----END PRIVATE KEY-----` — `R7\hidden\hidden_l1.py` hàm `secrets_for` (khoảng dòng 26–47).
- [Đ] README dòng 8–9 viết "cả ba còn lọt khoá PEM ở mọi nơi"; bảng README dòng 24 lại ghi A-1 "lọt hết ở S3, S6" — `DOC\README.md:8-9,24`. Hai câu mâu thuẫn; tệp thô ủng hộ dòng 24.
- [Đ] Mã của A-1 có mẫu PEM và trạng thái `inPEM` trong bộ ghi log; B-1 và Bp-1 không có mẫu `BEGIN`/`PRIVATE` nào (grep = 0) — `R7\runs\A-1\repo\internal\redact\redact.go:29`, `R7\runs\A-1\repo\internal\redact\writer.go` (trường `inPEM`), `R7\runs\B-1\repo\internal\detect\credential\redactor.go`, `R7\runs\Bp-1\repo\internal\redact\redact.go`.
- [Đ] A đợt 6 cuối: 22/36 với đúng hình dạng của A-1 đợt 7 (S3, S6 = 0/7; `private_key` 4/6) — `R6\results\A-L1\seg3.hidden.json.matrix.json`.
- [Đ] B đợt 6 cuối: 20/36, `aws_key_id` 0/5, `aws_secret` 0/5, `private_key` 0/6 — `R6\results\B-L1\seg4.hidden.json.matrix.json`. Phần tăng 20 → 30 ở đợt 7 đúng là 10 ô AWS, khớp README dòng 14.

### Inferences
- [S] Về riêng loại PEM, prompt trực tiếp (A) tốt hơn cả hai luồng Bint: A che PEM ở mọi nơi nó có che; B/B′ không che PEM ở đâu cả. Điểm hơn 8 ô của B/B′ đến hoàn toàn từ độ rộng nơi lưu (S3, S6: +12 ô không tính PEM) trừ đi 4 ô PEM mà A có còn B/B′ không (12 − 4 = 8).
- [S] Nếu A phủ thêm S3 và S6 với cùng bộ lọc, A sẽ đạt 36/36; nếu B/B′ thêm một mẫu PEM, cũng 36/36. Hai nhánh hỏng ở hai trục trực giao (nơi lưu so với loại).

### Gaps
- Không có lượt lặp nên không biết hình dạng "B/B′ sót PEM, A sót S3/S6" ổn định tới đâu; với A có 2 lượt (đợt 6, đợt 7) cùng hình dạng, với B/B′ chỉ 1 lượt mỗi nhánh.

## Test đóng băng của B và B′ cài loại bí mật nào? Ngữ cảnh thăm dò của B′ có thật không thấy kế hoạch/mã? Lỗ PEM sinh ra ở đâu?

### Takeaway
Không tệp đóng băng nào có PEM (grep `BEGIN`, `PRIVATE KEY`, `PEM` = 0). Thăm dò B′ chạy trong worktree tạm từ commit gốc và không đọc requirement/plan/mã của b-feat, nhưng đề bài của nó chứa nguyên 5 tiêu chí do b-req viết ("mật khẩu hoặc token", "mật khẩu, token và khoá API"). Lỗ PEM có từ đầu vào: prompt, thẻ người dùng và cả hai requirement đều không nhắc khoá riêng.

### Cited Findings
- [Đ] B: `internal/cli/credential_persistence_test.go` (1329 dòng): `BEGIN` 0, `PRIVATE KEY` 0, `PEM|pem` 0, `AKIA` 1, `AWS_SECRET` 1, `aws_secret_access_key` 0, `ghp_` 1, `github_pat` 1 — `R7\runs\B-1\repo\internal\cli\credential_persistence_test.go` (ca theo định dạng ở dòng 102–114; bốn nhóm nơi lưu ở dòng 465–468).
- [Đ] B′: `internal/cli/secret_storage_goal_test.go` (539 dòng): `BEGIN` 0, `PRIVATE KEY` 0, `AKIA` 0, `aws_secret_access_key` 1; dòng 1 khai báo "Loại đầu vào: env-assignment, password-flag, passwd-flag, token-flag, secret-flag, api-key-flag, auth-flag, bearer, basic, url-userinfo, url-query-token, json-password, json-token, github-token, openai-key, aws-secret-key, slack-token, jwt" — `R7\runs\Bp-1\repo\internal\cli\secret_storage_goal_test.go:1`.
- [Đ] B′: `internal/mcpgw/secret_storage_goal_test.go` (126 dòng) chỉ có "bearer, json-token, github-token, url-query-token" — `R7\runs\Bp-1\repo\internal\mcpgw\secret_storage_goal_test.go:1`.
- [Đ] Thăm dò B′ không cài `AKIA` nhưng B′ vẫn sạch `aws_key_id` 5/5, tức ô đó đến từ mã (mẫu `AKIA` có trong `R7\runs\Bp-1\repo\internal\redact\redact.go`), không từ thước đo — `R7\runs\Bp-1\grade.json`.
- [Đ] Đề bài thăm dò (21 932 ký tự): cwd là `R7\runs\Bp-1\tmp\bv\bp1086439672`, "worktree TẠM dựng từ mốc của tác vụ (không có mã mà b-feat đang viết)"; "Bạn không có tài liệu kế hoạch nào… không được đọc thư mục tác vụ (hook chặn)"; nhưng có khối `task.yml` gồm `goal` nguyên văn và 5 `criteria` — phiên `R7\runs\Bp-1\codex-home\sessions\2026\11\20\rollout-2026-11-20T21-42-32-01a1f968-b995-7400-afab-8bf4bb5292cd.jsonl` (thông điệp user lúc 14:42:34.074Z).
- [Đ] Luật 1 của đề thăm dò: "Liệt kê các LOẠI đầu vào… (không hẹp hơn lời người dùng; thêm bộ quy tắc/danh mục sẵn có của dự án… và các biến thể phổ biến ngoài đời)" — cùng tệp phiên trên.
- [Đ] 5 tiêu chí trong đề thăm dò trùng từng chữ AC-1…AC-5 của requirement do b-req viết — `TP\requirement.md:9-13`, `TP\task.yml`.
- [Đ] Không lệnh nào trong hai log thăm dò chạm `requirement.md`, `plan.md`, `tasks.md`, thư mục `runtime\tasks`, hay repo chính `runs\Bp-1\repo` (lọc lệnh và lời gọi MCP theo các chuỗi đó: 0 kết quả) — `TP\logs\b-goal-01M7WPHE0V1BTP4FAFG7PTE9EW.log`, `TP\logs\b-goal-01M7WW5F8AX2CV92XTM2NXJVBK.log`.
- [Đ] Sandbox của thăm dò vẫn cho ghi vào thư mục trạng thái (`writable_roots` gồm `…\state\repo-a253372825da`); việc không đọc thư mục tác vụ dựa vào hook và chỉ dẫn — `turn_context` trong tệp phiên thăm dò nêu trên.
- [Đ] requirement của B liệt kê loại theo định dạng: "JWT, GitHub/GitLab PAT, OpenAI/Anthropic, Google OAuth/API key, Slack, AWS và Stripe"; không có PEM/khoá riêng/SSH — `TB\requirement.md:26`.
- [Đ] requirement của B′ chỉ nói "mật khẩu, token và khoá API phổ biến"; không có danh sách loại, không PEM — `TP\requirement.md:5,13`.
- [Đ] Prompt người dùng chỉ nói "mật khẩu với token"; thẻ người dùng: "Che những loại nào?: mấy loại hay gặp như mật khẩu, token, khoá API" — `R7\tools\run.py` (hằng `PROMPT`, `ANSWER`), `R7\log\persona-card.md`.
- [Đ] Commit `39a9b70` (sau đợt 6, 2026-11-20 11:36) thêm vào b-req/b-goal câu "danh sách loại không hẹp hơn lời người dùng, bộ quy tắc sẵn có của dự án và biến thể phổ biến ngoài đời" và sửa `internal/cli/bdo_lean_goal_test.go`, `breq_baseline_goal_test.go` — `git show --stat 39a9b70` trong `D:\Projects\bint`.

### Inferences
- [S] Lỗ PEM không sinh ở bước cài đặt: nó có ở cả ba tầng trên (lời người dùng, câu trả lời của người dùng giả, tiêu chí của b-req) và được thăm dò giữ nguyên. Thăm dò "độc lập" của B′ thực tế bị neo bởi chữ của b-req (tiêu chí trong phiếu) và cùng model, nên phép thử "thước đo do ngữ cảnh độc lập viết" của báo cáo trước chưa được thực hiện đúng nghĩa: ngữ cảnh khác, nhưng thông tin đầu vào có phần của b-req.
- [S] A che PEM mà không cần ai nhắc, nên "model không nghĩ tới PEM" không phải giải thích đủ; khả năng là danh sách loại tường minh (B) hoặc tiêu chí "mật khẩu/token/khoá API" (B′) đã thu hẹp sự chú ý. Đây là giả thuyết, n = 1 mỗi nhánh.
- [S] Nhận xét của README dòng 14–16 (phần lợi AWS có nguy cơ học theo đề) được dữ liệu ủng hộ: requirement B đợt 7 có chữ "AWS" (`TB\requirement.md:26`), đợt 6 thì không cài AWS.

### Gaps
- Thăm dò lần 1 (tệp `credential_storage_goal_test.go` trong `tmp\bv\bp1086439672`) đã mất; không so được danh sách loại của hai lần thăm dò.
- Chưa đọc toàn văn đề bài b-review để kiểm reviewer có thấy kế hoạch hay không.

## Dòng thời gian từng nhánh có khớp bảng README không?

### Takeaway
Các mốc của B khớp (dòng mã đầu ~phút 33–37, kết quả kiểm chứng đầu phút 76,9 = 42,7 %). Với B′, "phút ~74 (41 %)" là con số giả định; thực tế kết quả kiểm chứng đầu tiên về ở phút 103,4 (57 %). Kiểm chứng và cổng mục tiêu ăn khoảng 50 % ngân sách ở cả hai nhánh Bint.

### Cited Findings
- [Đ] B-1, mốc theo `history`/`verifications`/`goal_checks`: b-req xong 14:42:20Z (6m27s); thước đo đóng băng 15:07:03Z (b-goal 24m39s); goal-1 15:08:59Z; b-feat bắt đầu 15:09:00Z (phút 33,1 = 18,4 %); verify-1 có kết quả 15:52:48Z (phút 76,9 = 42,7 %) — `TB\state.json`, `R7\runs\B-1\log\01-b-do.out`, `02-resume.out`.
- [Đ] B-1, lần sửa mã sản phẩm đầu tiên: 15:12:35Z, `internal/detect/credentials.go` (phút 36,7 = 20,4 %) — sự kiện `file_change` trong `R7\runs\B-1\codex-home\sessions\2026\11\20\rollout-2026-11-20T21-35-55-01a1f962-….jsonl`.
- [Đ] B-1 theo lát (b-feat; kiểm chứng; cổng mục tiêu, phút): lát 1: 13,4; 30,4; 1,2. Lát 2: 14,0; 19,2; 2,1. Lát 3: 15,2; 14,2; 0,8. Lát 4: 15,2; 15,9; 2,4. Sau đó 2,8 phút kiểm chứng cuối bị trần cắt lúc 17:36:00Z — tính từ `TB\state.json` (`history`, `verifications[].at`, `goal_checks[].at`) và thời điểm cuối từng tệp phiên trong `R7\runs\B-1\codex-home\sessions\`.
- [Đ] B-1 cộng dồn: b-req 6,5 phút (3,6 %); b-goal viết thước đo 24,7 phút (13,7 %); b-feat 57,8 phút (32 %); kiểm chứng 79,7 + cổng mục tiêu 8,4 + kiểm chứng cuối dở 2,8 = 90,9 phút (50,5 %); tổng 179,9 phút — cùng nguồn.
- [Đ] B-1: cả 4 lần kiểm chứng và 5 lần cổng mục tiêu đều `passed: false`; `review_rounds: 0` — `TB\state.json`.
- [Đ] Bp-1: b-req lần 1 xong 14:40:33Z (3m09s), trả lời câu hỏi 14:40:36Z, b-feat bắt đầu 14:42:30Z (phút 5,1 = 2,8 %); sửa mã sản phẩm đầu tiên 14:44:29Z `internal/redact/redact.go` (phút 7,1 = 3,9 %) — `TP\state.json`, phiên `rollout-2026-11-20T21-37-25-01a1f964-….jsonl`.
- [Đ] Bp-1: b-feat lõi kết thúc 15:11:20Z (28,8 phút); thăm dò lần 1 chạy song song 14:42:32Z → 14:55:29Z (13 phút); tiến trình bị giết 15:49:42Z ("tiến trình bint run (pid 57532) kết thúc bất thường") sau 38,4 phút kiểm chứng; chạy lại, verify-1 có kết quả 16:20:45Z (31,0 phút; phút 103,4 = 57,4 % của 180) — `TP\state.json` (`history[5]`, `verifications[0]`), tệp phiên trong `R7\runs\Bp-1\codex-home\sessions\`.
- [Đ] Bp-1 tiếp: b-feat sửa lõi 16:20:53Z → 16:27:20Z (6,5 phút); thăm dò lần 2 song song 16:20:53Z → 16:33:11Z; verify-2 16:40:48Z (13,5 phút); thước đo đóng băng 16:40:49Z; goal-1 16:41:09Z đỏ (2 ô: `url-query-token/trace`, `openai-key/profile-sync`); b-feat mở rộng 16:41:10Z → 17:09:02Z (27,9 phút, bị đánh `failed`); resume 17:10:22Z; b-feat mở rộng lần 2 → 17:27:38Z (17,3 phút); verify-3 17:52:56Z (25,3 phút, đỏ); goal-2 17:53:57Z xanh; b-review 17:54:05Z → 18:00:51Z (6,8 phút); b-feat sửa theo review → cắt 18:16:44Z (15,9 phút) — `TP\state.json`, `TP\logs\goal-1.log`, `R7\runs\Bp-1\log\03-resume.out`, `04-resume.out`.
- [Đ] Bp-1 cộng dồn trên 218 phút thực: b-req 5,1; b-feat 96,4 (44 %); kiểm chứng 108,2 (38,4 bị giết + 31,0 + 13,5 + 25,3) + cổng 1,3 = 109,5 (50 %); review 6,8; khoảng trống giữa failed và resume 1,3 — cùng nguồn; tổng thời gian tường 13 082,5 s — `R7\runs\Bp-1\run.json`.
- [Đ] Lần kiểm chứng bị giết đã kịp ghi kết quả đỏ của ba gói vào `verify-1.log` trước khi chết: `internal/app` FAIL sau 338,9 s, `internal/cli` FAIL sau 1109 s, `internal/tui/actions` FAIL sau 363,9 s; tệp có hai khối `$ sh.exe …` (dòng 1 và dòng 76) — `TP\logs\verify-1.log:1-63,76-152`.
- [Đ] A-1: một phiên 1804,8 s; sửa mã đầu tiên 15:25:53Z (phút 3,5); tự chạy `go test` 13 lần; kết thúc `agent_exit` — `R7\runs\A-1\run.json`, `R7\runs\A-1\log\01-codex.out`, phiên `rollout-2026-11-20T22-22-24-….jsonl`.
- [B] README dòng 42 ghi kết quả kiểm chứng đầu của B′ "phút ~74 (≈ 41 %) theo cây gốc" — `DOC\README.md:42`; không có tệp thô nào cho con số 74.
- [Đ] README dòng 43 ghi mỗi lát B "kiểm chứng/goal ~17–20 phút"; thực đo 31,6 / 21,3 / 15,0 / 18,3 phút — `TB\state.json`.

### Inferences
- [S] Nếu không có sự cố dlv và lần kiểm chứng đầu của B′ dài như lần chạy lại (31 phút), kết quả đầu tiên về ở khoảng phút 65 (36 %); nếu dài hơn do máy đang chạy 5 lượt song song thì có thể tới phút 72+. "Phút ~74" của README là ước lượng hợp lý nhưng không phải số đo.
- [S] Đảo thứ tự của B′ rút được dòng mã đầu từ 20 % xuống 4 %, nhưng tín hiệu kiểm chứng đầu tiên Bint thực sự dùng lại đến muộn hơn B (57 % so với 43 %) vì sự cố; không có sự cố thì hai nhánh ngang nhau (~36–43 %).
- [S] Tín hiệu đỏ đã nằm trên đĩa từ phút ~40 của B′ (gói `app` FAIL sau 339 s) nhưng Bint chỉ dùng kết quả khi cả lệnh kết thúc: kiểm chứng theo gói, báo sớm từng gói, sẽ cho tín hiệu ở ~22 % ngân sách.
- [S] Trong 45 phút đầu, B-1 và Bp-1 chạy cùng 3 lượt khác (B-2, Bp-2, B-3 tới 22:21, `R7\log\orchestrate.log`), nên verify-1 của B (30,4 phút) dài gần gấp đôi các lần sau (14–19 phút).

### Gaps
- Không có điểm test ẩn theo mốc trung gian (sau lõi của B′, sau từng lát của B); chỉ có hai điểm chấm của B′ (9100 s và cuối) và một của B.

## Vì sao agent B′ không chạy được `bint task check`? Nó nhận chỉ dẫn gì và làm gì thay thế?

### Takeaway
README sai ở điểm này. Agent đã gọi `bint task check` 24 lần trong bốn lượt b-feat sau; cả 24 lần lệnh tự bỏ qua vì sandbox Codex không cho `git add` ghi vào `.git/objects`. Riêng lượt lõi 29 phút thì đề bài (dạng nối tiếp phiên b-req) không chứa chỉ dẫn "LÕI trước" lẫn "kiểm hẹp theo mốc". Agent có tự chạy `go test` hẹp ở mọi lượt.

### Cited Findings
- [Đ] Đầu ra của mỗi lần gọi: "Bỏ qua: thay đổi buộc chạy toàn bộ bộ test (phạm vi full) vì source_unavailable không chụp được cây làm việc: git add -A -- .: error: insufficient permission for adding an object to repository database .git/objects … kiểm hẹp không chạy, để kiểm nền lo / kiểm nền: chưa có kết quả" — `TP\logs\b-feat-01M7WW5CEENB1BD1TTRECV8Q93.log` (2 lần), `b-feat-01M7WXAMJMX449FBSME4KHV9HM.log` (10 lần), `b-feat-01M7WZ0559937XS3ESERWMDBAN.log` (9 lần), `b-feat-01M7X1WK3T6AF6YXXYPYRFN579.log` (3 lần); đếm chuỗi "kiểm hẹp không chạy".
- [Đ] Agent tự ghi lỗi vào phiếu lúc 16:25:59Z: "Kiem hep bi bo qua vi sandbox chan git add ghi snapshot vao .git/objects…" — `TP\state.json` (`errors[4]`).
- [Đ] Lời agent: "`bint task check` vẫn bỏ qua kiểm tra hẹp vì sandbox chặn ghi snapshot vào `.git/objects`, nên tôi đang chạy trực tiếp các kiểm thử liên quan" — `TP\logs\b-feat-01M7WW5CEENB1BD1TTRECV8Q93.log`.
- [Đ] Đề bài lượt lõi (11 751 ký tự, 14:42:34.128Z, mở đầu "b-req … đã xong… Giờ bạn làm bước b-feat… NGAY TRONG phiên này") không chứa các chuỗi "LÕI", "Luồng B′", "bint task check", "kiểm hẹp" — phiên `R7\runs\Bp-1\codex-home\sessions\2026\11\20\rollout-2026-11-20T21-37-25-01a1f964-0ae2-7d90-9a8b-a56b929df6ec.jsonl`.
- [Đ] Đề bài các lượt b-feat phiên mới (28 909 và 29 648 ký tự) có đủ: "Luồng B′ — LÕI trước…", "Luồng B′ — kiểm hẹp theo mốc: sau MỖI nhóm sửa (mỗi TSK) chạy `bint task check …`… Đừng tự chạy toàn bộ bộ test hay verify" — phiên `rollout-2026-11-20T23-20-52-01a1f9c2-bf55-….jsonl`, `rollout-2026-11-20T23-41-09-01a1f9d5-….jsonl`, `rollout-2026-11-21T00-10-23-01a1f9f0-….jsonl`.
- [Đ] Lượt lõi không gọi `bint task check` lần nào nhưng tự chạy 13 lệnh `go test` (2 lệnh có `./...`) — cùng tệp phiên `…21-37-25-01a1f964…`; log `TP\logs\b-feat-01M7WPHCVBE9N39JM56B7PZN5P.log` có 0 dòng chứa "task check".
- [Đ] Lượt lõi chạm 59 tệp — `TP\state.json` (`bprime_core_touched`, 59 mục).
- [Đ] Ghi chú của Bint "luồng B′: b-feat không chạy kiểm hẹp theo mốc nên chưa có kết quả kiểm chứng sớm" xuất hiện sau mọi lượt — `R7\runs\Bp-1\log\03-resume.out`, `04-resume.out` (`notes`).
- [Đ] README: "`bint task check`… **0 lần**: agent bỏ qua chỉ dẫn" và "agent b-feat không gọi `bint task check` lần nào" — `DOC\README.md:44,53-55`.

### Inferences
- [S] Có hai lỗi của Bint/khung chạy chứ không phải agent không tuân thủ: (1) đường "nối tiếp phiên b-req, kế hoạch rút gọn" làm rơi chỉ dẫn riêng của B′ ở đúng lượt quan trọng nhất; (2) `bint task check` cần ghi object Git, mà sandbox `workspace-write` của Codex để `.git` chỉ đọc. Giai đoạn 3 của B′ (kiểm hẹp theo mốc, kiểm nền) vì vậy chưa hề được thử ở đợt 7.
- [S] Vì lượt lõi không nhận chỉ dẫn "LÕI trước, đừng tự mở rộng", "lõi" thực tế là một b-feat toàn phần (59 tệp); giai đoạn 4 (lõi trước, mở rộng sau) cũng không được thử đúng thiết kế. Kết luận "B′ không hơn B" của README do đó là kết luận về một B′ chạy hỏng hai trong bốn cơ chế.
- [S] Đề xuất 1 của README (Bint tự chạy kiểm hẹp) vẫn đúng hướng, nhưng lý do thật là sandbox, không phải agent; nếu Bint chạy ngoài sandbox thì chụp cây được.

### Gaps
- Chưa đọc mã `bint task check` ở `63a6114` để xác nhận nó bắt buộc phải `git add` vào kho chính (có thể dùng index/object tạm).
- Không rõ vì sao `.agents/skills/b-add-flow/SKILL.md` là tệp đầu tiên bị từ chối (tệp chưa theo dõi hay đã đổi).

## N2 (chậm 11–19 lần): nguyên nhân trong diff từng nhánh? Có ai nhắc tới hiệu năng?

### Takeaway
Cả ba nhánh cùng một kiểu sửa: chèn bộ ghi che-theo-dòng (7–10 biểu thức chính quy mỗi dòng) vào luồng stdout/stderr của lệnh kiểm chứng; bài N2 đẩy 250 000 dòng qua đó. Chỉ B′ có nhắc hiệu năng (nhờ câu trả lời của người dùng giả) và có đo vi mô 11–14 µs/bản ghi, nhưng không đo log lớn. Hai số N2 của README cho A-1 và B-1 không khớp tệp còn lại.

### Cited Findings
- [Đ] N2 = `bint task verify` với lệnh in 250 000 dòng `line %d key=value%d sha256:%064x path=/a/b/c/%d.go ok`; tỷ số = trung vị 3 lần ứng viên / trung vị 3 lần bản gốc — `R7\hidden\hidden_l1.py:263-276,293-295`.
- [Đ] A-1: ứng viên 19,93 / 20,92 / 22,52 s; gốc 1,19 / 3,54 / 2,17 s; `ratio` 9,62 — `R7\runs\A-1\n2.json`, `R7\runs\A-1\grade.json`, bản chép `DOC\results\A-1\n2.json`.
- [Đ] B-1: ứng viên 14,45 / 16,39 / 16,63 s; gốc 1,27 / 2,89 / 2,48 s; `ratio` 6,61 — `R7\runs\B-1\n2.json`, `DOC\results\B-1\n2.json`.
- [Đ] Bp-1 cuối: 9,73 / 8,86 / 8,91 s so với 0,57 / 0,55 / 0,60 s; `ratio` 15,74. Lúc 9100 s: 8,0 / 8,59 / 8,16 so với 0,68 / 0,79 / 0,71; `ratio` 11,52 — `R7\runs\Bp-1\n2.json`, `R7\runs\Bp-1\grade-failed-9100s.json`.
- [Đ] README ghi A-1 19,3× và B-1 11,7× — `DOC\README.md:26`; không tệp nào trong `R7\log`, `R7\runs\*\n2.json`, `grade.json` chứa hai số này. `grade.py` ghi đè `n2.json` mỗi lần chấm; A-1 và B-1 được chấm lại lúc 00:37–01:01 khi hai lượt `go test ./...` chạy song song (thời gian bản gốc lên 1,2–3,5 s) — `R7\tools\grade.py`, `R7\log\orchestrate.log` (dòng 00:37:44–00:37:46), mtime `n2.json` 00:40.
- [Đ] A đợt 6: 20,11× (17–18 s so với 0,82–0,92 s, máy rảnh) — `R6\results\A-L1\n2.json`; B đợt 6: 5,5× — `R6\results\B-L1\n2.json`.
- [Đ] Cả ba diff thay `cmd.Stdout/Stderr = combined` bằng bộ ghi che: A `scrub.Writer(combined)`, B `redactor.LogWriter(combined)`, B′ `mask.Writer(combined)` — `git diff bench refs/bench/final -- internal/eval/shell.go` trong `R7\runs\A-1\repo`, `R7\runs\B-1\repo`, `R7\runs\Bp-1\repo`.
- [Đ] Số `regexp.MustCompile` trong bộ lọc: A 10 (`internal/redact/redact.go`), B 7 (`internal/detect/credential/redactor.go`), B′ 9 (`internal/redact/redact.go`); bộ ghi của A gom từng dòng rồi gọi `scrubber.Document(line)` — `R7\runs\A-1\repo\internal\redact\writer.go`.
- [Đ] B: `requirement.md`, `tasks.md`, `baseline.md` không có từ nào về hiệu năng/chậm/benchmark (grep = 0); `plan.md` chỉ nhắc `perf` như một nơi ghi log — `TB\requirement.md`, `TB\plan.md:11,54`.
- [Đ] B′: mục tiêu có "tránh làm chậm đáng kể luồng chạy thường dùng"; AC-5 "không quét lại toàn bộ lịch sử trong mỗi lệnh hoặc hook"; TSK-5 đòi "đo chi phí ở luồng thường dùng" bằng `BenchmarkSecretRedaction` — `TP\requirement.md:5,13`, `TP\tasks.md:7`.
- [Đ] B′ ghi bằng chứng "BenchmarkSecretRedaction: 14069 ns/op" rồi "11122 ns/op", "11301 ns/op" — `TP\state.json` (`criteria[4]`, `[9]`, `[14]`).
- [Đ] Nguồn của ràng buộc hiệu năng ở B′ là câu trả lời cố định "Tôi không muốn bint chạy hỏng hay chậm hẳn đi"; B không hỏi câu nào nên không nhận câu này — `TP\requirement.md:17`, `TB\requirement.md:41-43` ("Câu hỏi mở: Không có").
- [Đ] Ba tệp test đóng băng không có ca hiệu năng/log lớn (grep `benchmark|chậm|slow|perf|large` = 0) — ba tệp test nêu ở mục trước.
- [Đ] Review của B′ không có phát hiện nào về hiệu năng (grep = 0) — `TP\review.md`.
- [Đ] Lời kết của A-1 không nhắc hiệu năng — thông điệp cuối trong `R7\runs\A-1\log\01-codex.out`.

### Inferences
- [S] Nguyên nhân chung là chi phí regex theo dòng trên luồng log; không nhánh nào có đường tắt (bỏ qua dòng không có ký tự gợi ý, gộp mẫu). Chưa profile nên đây là đọc mã, không phải đo.
- [S] 250 000 dòng × ~11 µs ≈ 2,8 s, trong khi B′ chậm thêm ~8,3 s: benchmark vi mô của B′ đo một "bản ghi mẫu" khác dòng log N2, hoặc chi phí nằm ở chỗ khác (khoá, ghi từng dòng). B′ là nhánh duy nhất "biết" yêu cầu không chậm nhưng thước đo tự chọn không bắt được ca xấu.
- [S] Không so được mức chậm giữa các nhánh bằng tỷ số: A-1 và B-1 đo dưới tải (gốc 1,2–3,5 s), Bp-1 đo lúc máy rảnh (gốc 0,57 s). Theo thời gian tuyệt đối dưới điều kiện tương tự chỉ có A đợt 6 (17–18 s) và Bp-1 (8,9–9,7 s) là máy rảnh. Kết luận "cả ba hỏng N2" vững; khoảng "11–19 lần" thì không.

### Gaps
- Nguồn của 19,3× và 11,7× (có thể là lần chấm đầu đã bị ghi đè) không còn.
- Chưa có số N2 của ba cây đo trong cùng điều kiện máy.

## Test repo đỏ ở cây cuối: B (8) và B′ (3) là gì?

### Takeaway
B-1 có 8 test đỏ ở lần chấm cuối, nhưng chỉ 2 là hồi quy thật có bằng chứng; 1 là thước đo của chính nó; 2 là lỗi dựng mẫu benchmark; 3 chỉ đỏ ở lần chấm cuối chạy dưới tải. B′ có 3 test đỏ: 2 là cùng lỗi dựng mẫu, 1 là test thời gian nhạy tải. Không có hồi quy thật nào được chứng minh ở cây cuối của B′.

### Cited Findings
- [Đ] B-1, 8 test cấp trên đỏ: `TestRepoWritesGoThroughRepoWriter`, `TestMCPIndexIgnoresWrapperLauncher` (gói `app`); `TestBintDoesNotPersistCredentials`, `TestGoalBDoLeanSlicesAndGates`, `TestGoalBReqBaselineAndSkillGates` (gói `cli`); `TestExclusiveGroup` (`mcpgw`); `TestCLIRefTasksScreen` (`tui/app`); `TestCleanupWorktreeReleasesGatewayBounded` (`workflow`) — `R7\runs\B-1\gotest.txt`, `DOC\results\B-1\gotest-summary.txt`.
- [Đ] Hồi quy thật 1: `repowrite_audit_test.go:180: … mcp_gateway_svc.go:MCPGatewayStart gọi os.OpenFile thẳng` — `R7\runs\B-1\gotest.txt`; đỏ ổn định từ verify-2 tới verify-5 — `TB\logs\verify-2.log` … `verify-5.log`.
- [Đ] Hồi quy thật 2 (che quá tay): `TestMCPIndexIgnoresWrapperLauncher` — cảnh báo in "(phiên bản ***)" thay cho số phiên bản — `R7\runs\B-1\gotest.txt`; đỏ từ verify-2. B′ gặp đúng lỗi này ở lượt lõi và tự sửa — `TP\state.json` (`errors[2]`).
- [Đ] Thước đo của chính nó: `TestBintDoesNotPersistCredentials` còn đỏ ở nhóm `task-documents-ledger` (86 ô, "credential persisted in task criteria decoded stdout, task criteria stdout"); agent ghi nguyên nhân: `internal/cli/task.go:715` "ngoài scope.allow" — `R7\runs\B-1\gotest.txt`, `TB\state.json` (`criteria[9]`, `errors[3]`), `TB\task.yml` (danh sách `scope.allow` không có `internal/cli/task.go`).
- [Đ] Lỗi dựng mẫu: `TestGoalBDoLeanSlicesAndGates/skills_package_tests_synced` và `TestGoalBReqBaselineAndSkillGates/skill_published_copies` báo `.claude/skills/b-feat/SKILL.md` 9067 byte so với bản sinh từ `internal/skills/core` 9086 byte. Ở chính commit gốc `bench` (e41bb69 "bench: cập nhật skill/hook theo Bint HEAD (bên B/B′)") hai tệp đã là 9067 và 9086 byte; diff của B-1 và Bp-1 không chạm `internal/skills`, `.claude/skills`, `.agents/skills` — `git show bench:.claude/skills/b-feat/SKILL.md | wc -c`, `git show bench:internal/skills/core/b-feat/SKILL.md | wc -c`, `git diff --stat bench refs/bench/final -- internal/skills .claude/skills .agents/skills` trong `R7\runs\B-1\repo` và `R7\runs\Bp-1\repo`.
- [Đ] Hai test này đỏ trong mọi lần kiểm chứng của cả hai nhánh: B verify-1…4, B′ verify-1…3 — `TB\logs\verify-*.log`, `TP\logs\verify-*.log`. Ở B′, verify-2 và verify-3 chỉ đỏ đúng hai test này — `TP\logs\verify-2.log`, `verify-3.log`.
- [Đ] Review B′ phát hiện .1 xác nhận: "Các tệp skill và lock nằm ngoài diff của tác vụ" — `TP\review.md` (mục `.1`).
- [Đ] Ba test chỉ đỏ ở lần chấm cuối của B-1, không đỏ trong verify-1…4 của Bint: `TestExclusiveGroup` ("b đang giữ khoá"), `TestCLIRefTasksScreen`, `TestCleanupWorktreeReleasesGatewayBounded` — so `R7\runs\B-1\gotest.txt` với `TB\logs\verify-1.log` … `verify-4.log`.
- [Đ] `TestCleanupWorktreeReleasesGatewayBounded` cũng là test đỏ duy nhất của A-1 ("nhả treo làm chặn dọn 5.5075309s") và đỏ ở A đợt 6; chính agent A-1 đã chạy riêng test này với `-p 1` và qua (mã 0) — `R7\runs\A-1\gotest.txt`, `R6\results\A-L1\gotest.txt:81`, lệnh `go test` cuối trong `R7\runs\A-1\log\01-codex.out`.
- [Đ] Lần chấm cuối của A-1 và B-1 chạy song song (00:37 → 01:01); Bp-1 chấm một mình (01:17 → 01:31), thời gian các gói chỉ bằng khoảng một nửa (ví dụ `internal/cli` 686 s so với 1044 s) — `R7\log\orchestrate.log`, mtime `grade.json`, ba tệp `gotest.txt`.
- [Đ] B′, 3 test đỏ: hai test đồng bộ skill nêu trên và `TestGateEvidenceAndBudget/perf_gate/chậm_đỏ_rồi_vòng_sửa_xanh` ("0.227 giây… so với mốc 0.114 giây = ×1.99, trần… 1.50 và sàn nhiễu 0.1 giây") — `R7\runs\Bp-1\gotest.txt`.
- [Đ] Test `perf_gate` này cũng đỏ ở A đợt 6 và ở verify-1 của B-1 (rồi tự xanh ở các lần sau và ở lần chấm cuối của A-1, B-1) — `R6\results\A-L1\gotest.txt:83-85`, `TB\logs\verify-1.log`, `R7\runs\B-1\gotest.txt`.
- [Đ] `TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch` đỏ ở cả 4 lần kiểm chứng của B và verify-1 của B′ nhưng xanh ở cả ba lần chấm cuối; tri thức dự án đã ghi test này "thất bại ngẫu nhiên" (mục k:01M779C1VVVJMQH2Q2T9XBCTPQ, in trong đề thăm dò) — `TB\logs\verify-1.log` … `verify-4.log`, `TP\logs\verify-1.log`, tệp phiên thăm dò B′.
- [Đ] README: B "8 đỏ (≥ 4 hồi quy + thước đo của chính nó)", B′ "3 đỏ (2 hồi quy đồng bộ skill + 1 cổng hiệu năng)", và "cây cuối mang 4–5 hồi quy thật" — `DOC\README.md:27,59`.

### Inferences
- [S] Phân loại B-1: hồi quy thật 2; thước đo riêng 1; lỗi dựng mẫu 2; nghi chập chờn do tải 3 (không loại trừ được `TestExclusiveGroup` là hồi quy vì B có sửa `internal/mcpgw`, nhưng nó xanh trong 4 lần kiểm chứng của Bint). Phân loại Bp-1: lỗi dựng mẫu 2; nhạy thời gian 1; hồi quy thật 0.
- [S] Vì hai test đồng bộ skill đỏ sẵn trong `tmpl-B`, lệnh kiểm chứng `go test ./...` của B và B′ không thể xanh dù agent làm gì: B bị chặn "test cũ đỏ" ở cả 4 lát (lát 1 không có hồi quy thật nào, chỉ 2 lỗi mẫu + 2 test chập chờn), B′ bị ép một lượt "sửa lõi" và kiểm chứng cuối luôn đỏ. Đây là nhiễu dựng thí nghiệm lớn nhất mà README không nêu; nó làm B tốn 4 lần dừng chờ người và làm cả hai không thể kết thúc `done`.
- [S] So sánh "3 so với 8 test đỏ" của README không công bằng: hai cây được chấm dưới tải khác nhau, và 5 trong 8 (B) cùng 3 trong 3 (B′) không phải hồi quy do agent.

### Gaps
- Không có lượt `go test ./...` trên cây gốc `tmpl-B` dưới cùng tải để xác nhận trực tiếp bộ test đỏ nền.
- Chưa chạy lại riêng `TestExclusiveGroup`, `TestCLIRefTasksScreen` trên cây B-1 (bị cấm chạy bộ test dài).

## Token từng bước và phép "trừ phần do sự cố"

### Takeaway
Tổng token khớp README (A 8,0M; B 43,7M; B′ 61,5M; B′ lúc 9100 s 39,3M). Phép trừ của README chỉ trừ lần thăm dò đầu (6,6M → ≈ 55M); nếu trừ cả lượt mở rộng bị chạy hai lần vì Bint đánh `failed` nhầm (9,6M) thì B′ còn ≈ 45,3M, gần bằng B; phần trước review của B′ chỉ ≈ 32,7M.

### Cited Findings
- [Đ] Cách đếm: cộng `total_token_usage` cuối của mỗi tệp phiên trong `CODEX_HOME\sessions`; token vào gồm cả phần cache — `R7\tools\run.py` (hàm `tokens`).
- [Đ] A-1: 1 phiên, vào 8 005 135 (cache 7 760 256), ra 43 729 — `R7\runs\A-1\run.json`.
- [Đ] B-1: 5 phiên, vào 43 714 636, ra 173 230 — `R7\runs\B-1\run.json`. Theo bước (từ `turn.completed` trong log bước; lát 1 nối phiên b-req nên lấy hiệu): b-req 1 706 988; b-goal 11 384 848 (26,0 %); b-feat lát 1: 8 207 054; lát 2: 6 332 576; lát 3: 8 540 331; lát 4: 7 542 839 (b-feat cộng 30,62M = 70 %) — `TB\logs\b-req-….log`, `b-goal-….log`, bốn `b-feat-….log`.
- [Đ] Bp-1: 7 phiên, vào 61 548 389, ra 244 795 — `R7\runs\Bp-1\run.json`. Theo bước: b-req lần 1: 708 544; b-req lần 2: 595 795; b-feat lõi: 14 826 828; thăm dò lần 1: 6 642 618; b-feat sửa lõi: 1 608 843; thăm dò lần 2: 5 338 223; b-feat mở rộng lần 1 (bị đánh `failed`): 9 627 092; b-feat mở rộng lần 2: 9 628 760; b-review: 2 060 648; b-feat sửa theo review (bị cắt): 10 511 038 — `TP\logs\*.log` (dòng `turn.completed`), `TP\state.json` (`last_step.usage`: phiên `01a1f9f0` tổng 20 139 798).
- [Đ] Bp-1 lúc 9100 s: 5 phiên, 39 347 943 = 16 131 167 + 6 642 618 + 5 338 223 + 1 608 843 + 9 627 092 — `R7\log\Bp-1.driver.txt` (dòng END), `R7\runs\Bp-1\run-failed-9100s.json`.
- [Đ] Tỷ số so với A-1: B 5,46×; B′ 7,69×; B′ trừ thăm dò lần 1: 54,9M = 6,86× — tính từ các số trên; khớp `DOC\README.md:29,63`.
- [Đ] A đợt 6: 37 368 790 trên 5 tệp phiên (README đợt 7 ghi "3 phiên") — `R6\results\tokens.json`.

### Inferences
- [S] Trừ cả lượt mở rộng trùng: 61,55 − 6,64 − 9,63 = 45,28M (5,66× A), chênh B chỉ 3,6 %. Đây là cận dưới vì lượt mở rộng lần 2 có làm thêm việc trên cây đã mở rộng, không thuần lặp.
- [S] B′ trước review (bỏ b-review 2,06M và vòng sửa 10,51M, bỏ hai phần trùng): ≈ 32,7M (4,1× A), thấp hơn B (43,7M, chưa tới review) khoảng 25 %. Câu "B′ tốn token hơn B" của README chỉ đúng khi tính cả review + vòng sửa và cả phần lặp do sự cố.
- [S] b-goal của B tốn 11,4M (26 %) cho một tệp test 1329 dòng; hai lần thăm dò của B′ cộng 12,0M: chi phí viết thước đo ngang nhau, không rẻ hơn ở B′.
- [S] Mọi phương án đều vượt ngưỡng 2,5× A của tiêu chí 2; kết luận "không đạt về chi phí" không đổi.

### Gaps
- Không có giá USD (`cost_usd: null` trong mọi tệp `.out`).

## Hai sự cố (dlv 22:49, proxy 00:09) và 38 phút bù: thiên lệch cho B′ theo hướng nào?

### Takeaway
Con số 38 phút khớp thô (kiểm chứng chạy 38,4 phút thì bị giết). Sự cố thứ hai không phải mất việc do mạng: lượt b-feat đã hoàn tất, Bint đánh `failed` vì một dòng "Reconnecting… 2/5" ở dòng 4 của log. Hệ quả là B′ được chạy hai lượt mở rộng thay vì một như thiết kế; phần bù không đổi điểm test ẩn nhưng nghiêng nhẹ về phía có lợi cho B′ ở các chỉ số phụ.

### Cited Findings
- [Đ] `history[5]`: 15:49:42Z "tiến trình bint run (pid 57532) kết thúc bất thường"; b-feat lõi kết thúc 15:11:20Z → kiểm chứng đã chạy 38,4 phút; mã thoát của lệnh resume là 3221226324 — `TP\state.json`, `R7\runs\Bp-1\run.json` (sự kiện t = 4337,7).
- [Đ] `prereg-and-incidents.md` tự mâu thuẫn: dòng 38 "mất khoảng 20–25 phút", dòng 41 "mất ~38 phút = 2280 s" — `DOC\prereg-and-incidents.md:38,41`.
- [Đ] Ngân sách chạy tiếp 3980 s = 1700 + 2280; lệnh cuối chạy 3981,9 s rồi bị cắt — `R7\runs\Bp-1\run.json` (`continuations`, sự kiện t = 13082,5), `R7\tools\cont.py`.
- [Đ] Log lượt b-feat "hỏng": dòng 4 là `{"type":"error","message":"Reconnecting... 2/5 (stream disconnected…)"}`; dòng 212 (cuối) là `turn.completed` với 9 627 092 token vào; có thông điệp kết thúc đầy đủ ("Cả 10 kiểm tra mục tiêu đóng băng… đều qua… Đã ghi bằng chứng cho 5/5 tiêu chí") — `TP\logs\b-feat-01M7WXAMJMX449FBSME4KHV9HM.log:4,212`.
- [Đ] Bint ghi bước này `status: failed`, `stop_class: intrinsic`, 27m53s; lần resume ghi "không nối tiếp phiên cũ: bước b-feat dừng với lớp intrinsic" và mở phiên b-feat mới 17m15s với cùng đề "lượt MỞ RỘNG (duy nhất)… Không có lượt mở rộng thứ hai" — `R7\runs\Bp-1\log\03-resume.out`, `04-resume.out`, phiên `rollout-2026-11-21T00-10-23-01a1f9f0-….jsonl`.
- [Đ] Cây lúc 9100 s (sau lượt mở rộng lần 1 đã hoàn tất) chấm 30/36, 68 tệp, +2532/−145; cây cuối 30/36, 87 tệp, +3299/−219 — `R7\runs\Bp-1\grade-failed-9100s.json`, `R7\runs\Bp-1\grade.json`.
- [Đ] Thăm dò lần 2 chạy 16:20:53Z → 16:33:11Z, song song với sửa lõi và verify-2 (xong 16:40:48Z); nó kết thúc trước verify-2 nên không cộng thêm thời gian tường — tệp phiên `…23-20-52-01a1f9c2-bf54-….jsonl`, `TP\state.json`.
- [Đ] `run.json` cuối của Bp-1 còn giữ `finish: 2026-11-21T00:09:04` (mốc cũ) dù `wall_s` 13 082,5; driver chết vì lỗi mã hoá cp1252 khi in dòng END — `R7\runs\Bp-1\run.json`, `R7\log\Bp-1.cont.txt`.
- [B] "Người điều phối gắn dlv… thoát dlv đã giết tiến trình" — `DOC\prereg-and-incidents.md:36-38`; tệp thô chỉ xác nhận tiến trình chết bất thường đúng thời điểm.

### Inferences
- [S] Về thời gian tường, bù 38 phút là sòng phẳng nếu lần kiểm chứng gốc dài bằng lần chạy lại; không hào phóng quá.
- [S] Thiên lệch lên (có lợi cho B′): (1) lượt mở rộng thứ hai 17 phút, ngữ cảnh mới, mà thiết kế B′ không có; (2) phần đầu lần kiểm chứng bị giết chạy lúc máy 5 lượt song song, phần bù chạy sau 00:36 khi B-1 đã xong. Thiên lệch xuống: mất ngữ cảnh phiên (resume bằng phiên mới), và token đội thêm ~16M.
- [S] Tác động lên chỉ số chính bằng 0 (30/36 trước và sau). Tác động lên chỉ số phụ đáng kể: "tới được review" và "3 test đỏ" đều thuộc 66 phút sau mốc 9100 s. Không có sự cố dlv thì mốc 9100 s rơi vào khoảng phút 114, còn ~66 phút cho kiểm chứng + review + vòng sửa, tức B′ nhiều khả năng vẫn tới review trong 180 phút.
- [S] Sự cố "proxy" nên được ghi là lỗi của Bint (coi sự kiện `error` tạm thời là thất bại của bước dù lượt hoàn tất), không phải sự cố ngoài Bint như README dòng 79–80 viết.

### Gaps
- Không có log proxy để biết kết nối có đứt lần nào khác trong lượt đó (trong phiên có một thông điệp `environment_context` lúc 17:00:02Z, chưa rõ nghĩa).

## Người dùng giả: trả lời gì cho câu hỏi của B′, vì sao chọn `--slice keep` bốn lần ở B?

### Takeaway
Không có model đóng vai người dùng: đó là một chuỗi trả lời cố định và một bảng quyết định viết cứng trong `run.py`. `--slice keep` là nhánh `elif "--slice" in cmds` của script, không phải lựa chọn theo thẻ người dùng; thẻ người dùng nếu đọc theo nghĩa đen ("bạn tự quyết cho an toàn") còn không ủng hộ "giữ".

### Cited Findings
- [Đ] Câu hỏi của B′: "Q-1: Bạn muốn Bint cũng làm sạch log và lịch sử đã lưu từ trước, hay chỉ ngăn mật khẩu và token lọt vào những lần ghi mới?" — `R7\runs\Bp-1\log\01-b-do.out`.
- [Đ] Câu trả lời (chuỗi `ANSWER`, dùng cho mọi câu hỏi): "Mục tiêu của tôi: không để bí mật nằm ở bất kỳ chỗ nào bint lưu. Tôi không muốn bint chạy hỏng hay chậm hẳn đi. Không cần bật tay, nên mặc định bật. Che mấy loại hay gặp như mật khẩu, token, khoá API. Còn câu kỹ thuật khác thì tôi không rành, bạn tự quyết cho an toàn." — `R7\tools\run.py` (hằng `ANSWER`), ghi vào `TP\requirement.md:17`.
- [Đ] Câu trả lời không trả lời thẳng câu hỏi (dọn dữ liệu cũ hay không); b-req tự suy ra "kể cả dữ liệu cũ" và AC-5 "tự làm sạch dữ liệu vận hành đã lưu" — `TP\requirement.md:5,13`.
- [Đ] Bảng quyết định trong `run_b`: lý do bắt đầu bằng "kế hoạch lớn" → resume; `"--slice" in cmds` → `--slice keep`; `"--accept" in cmds` → dừng lượt; có `questions` → điền `ANSWER`; trần `MAX_USER_ACTIONS = 6` — `R7\tools\run.py` (hàm `run_b`); đăng ký trước ghi "lát giữ → `--slice keep`" — `DOC\prereg-and-incidents.md:10`.
- [Đ] B-1 có 5 thao tác: `approve_large_plan` (t = 388,1) và 4 × `slice_keep` (t = 4687,6; 6808,1; 8624,5; 10636,9); mỗi lần Bint đưa 4 lựa chọn: keep / discard / more / cancel — `R7\runs\B-1\run.json` (`user_actions`, `choices`).
- [Đ] Thẻ người dùng: "Hỏi về chia việc/làm tiếp: cứ làm tiếp chỗ đang dở…"; "Câu kỹ thuật khác: tôi không rành, bạn tự quyết cho an toàn" — `R7\log\persona-card.md`.
- [Đ] Lý do chặn ở lát 1: 4 test "cũ đỏ" gồm `TestEvalRunJobPlan…`, `TestGateEvidenceAndBudget/perf_gate/…`, và 2 test đồng bộ skill; từ lát 2 thêm `TestMCPIndexIgnoresWrapperLauncher`, `TestRepoWritesGoThroughRepoWriter` — `R7\runs\B-1\run.json` (trường `reason` của từng `status`).
- [Đ] Các lượt bị huỷ cho thấy b-req dao động ít: B-2 "46 tệp, 4 lát", B-3 "41 tệp, 4 lát", B-1 "42 tệp, 6 nhóm bề mặt, 4 lát", không lượt classic nào hỏi; B′ luôn hỏi (Bp-1 lần đầu 1 câu, Bp-2 2 câu, Bp-1 chính thức 1 câu) — `R7\aborted-2\B-2.driver.txt`, `B-3.driver.txt`, `Bp-2.driver.txt`, `R7\aborted-1\Bp-1.driver.txt`.

### Inferences
- [S] Nhận xét 5 của README ("người dùng không rành sẽ luôn chọn giữ") là giả định của người viết script, không phải quan sát hành vi: lựa chọn được đăng ký trước và viết cứng. Dữ liệu chỉ cho thấy "nếu luôn giữ thì hồi quy của lát 2 đi tới cuối".
- [S] Ở lát 1, danh sách "test cũ đỏ" không có hồi quy thật nào, nên "giữ" là quyết định đúng; chọn "discard" sẽ vứt việc tốt vì nhiễu nền. Cổng lát mỏng cần một mốc nền (test đỏ sẵn trước khi sửa) trước khi bàn mặc định an toàn hơn.
- [S] Lợi ích đo được của câu hỏi b-req gọn ở B′: mang ràng buộc "không chậm" vào requirement (B không có). Nó không cứu được N2.

### Gaps
- Không có lượt nào dùng người dùng giả là model, nên chưa biết một người dùng low-tech "thật" chọn gì ở cổng lát.

## 8 phát hiện high của review B′: có thật không, có trùng lỗi test ẩn không?

### Takeaway
Không phát hiện nào trùng PEM hay N2. Một trong tám (.1) là lỗi dựng mẫu benchmark, không phải lỗi sản phẩm. Bảy phát hiện còn lại dựa trên đọc mã (review tự ghi vậy), và trái với README, agent đã đưa bản sửa cho cả bảy vào cây trước khi bị cắt.

### Cited Findings
- [Đ] Tám tiêu đề: (.1) bản xuất skill và core.lock lệch nguồn; (.2) `curl --user`/`-u` lọt vào log kiểm chứng/eval; (.3) trình sửa cấu hình tạo bản sao tạm nguyên văn; (.4) trace hook giữ bí mật lặp lại ở trường mô tả; (.5) chỉ mục tri thức lưu token trong tags/domains; (.6) đồng bộ profile làm hỏng xác thực MCP qua `--header`; (.7) backup cài hook của iso sync bỏ qua bí mật trong profile; (.8) dọn dữ liệu cũ thay lệnh kiểm chứng nhưng giữ hiệu lực phê duyệt — `TP\review.md`, bản chép `DOC\results\Bp-1\review.md`, `R7\runs\Bp-1\log\04-resume.out` (`open_findings`).
- [Đ] Review tự nêu: "Các phát hiện về đường ghi dựa trên mã nguồn"; trường Repro của .2–.8 là vị trí tệp:dòng, không phải lệnh chạy — `TP\review.md`.
- [Đ] `TP\review.md` không chứa "PEM", "private", "khoá riêng", cũng không có từ nào về hiệu năng/log lớn (grep = 0).
- [Đ] Kiểm điểm (.2): ở cây 9100 s (commit `627ad02`), `internal/redact/redact.go` có 0 lần `userOption`; cây cuối có 3 — `git show 627ad02:internal/redact/redact.go`, `git show refs/bench/final:internal/redact/redact.go` trong `R7\runs\Bp-1\repo`. Phát hiện đúng với cây được review và đã được sửa.
- [Đ] Kiểm điểm (.5): cây cuối đã che `tags`, `domains` bằng `maskedTerms` trước khi ghi `knowledge_index` — `R7\runs\Bp-1\repo\internal\kindex\kindex.go:150-165` (bản `refs/bench/final`).
- [Đ] Lời agent trong vòng sửa: "Các sửa đổi cho bảy phát hiện về mã đã vào cây làm việc… Các bài kiểm thử hồi quy và 10 ca mục tiêu đóng băng đã qua. Hai ca đóng gói skill cũng qua trong checkout tạm… Việc áp dụng vẫn cần quyền ghi `.agents`" — `TP\logs\b-feat-01M7X1WK3T6AF6YXXYPYRFN579.log`, `TP\state.json` (`errors[8]`).
- [Đ] Diff 9100 s → cuối: 37 tệp, +822/−129, chạm đúng các tệp review nêu (`config_svc.go`, `iso_sync.go`, `kindex.go`, `iso/config_redaction.go`, `workflow/ops.go`, `redact.go`, `harness/claude/install.go`) — `git diff --stat 627ad02 refs/bench/final` trong `R7\runs\Bp-1\repo`.
- [Đ] README: "8 high… nhưng hết ngân sách trước khi sửa xong" — `DOC\README.md:61-62`.

### Inferences
- [S] Review một lượt ngữ cảnh sạch cho ra 7 phát hiện có vẻ thật ngoài ma trận, và một vòng sửa 16 phút xử lý được cả 7: đây là bằng chứng tốt nhất của đợt 7 cho phần "giữ" trong báo cáo trước (một lượt review, một vòng sửa có trần). Nhưng review không bắt được hai lỗi mà test ẩn đo (PEM, N2), tức nó cùng điểm mù với người viết về loại đầu vào và hiệu năng.
- [S] Hai phát hiện (.6, .8) là lỗi che quá tay/đổi hành vi, cùng họ với ba lỗi critical Y1–Y3 của B đợt 6: rủi ro "sửa rộng làm hỏng luồng" vẫn còn ở B′ và chỉ review mới thấy.

### Gaps
- Chỉ kiểm điểm 2/7 phát hiện mã; chưa tái hiện phát hiện nào bằng chạy thật. Không có review mù độc lập như đợt 6 để đối chiếu.
- Vòng sửa bị cắt trước kiểm chứng lại nên không biết 37 tệp sửa thêm có gây hồi quy không (lần chấm cuối: chỉ 3 test đỏ nêu trên).

## A-1 dừng ở 30 phút với 8,0M token so với A đợt 6 (37,4M, cùng 22/36): các phiên thêm ở đợt 6 mua được gì? Hàm ý cho A+

### Takeaway
Ở đợt 6, phiên đầu (14,8M) cho 15/36; 9,0M tiếp theo mua 7 ô (S4); 13,5M cuối không mua ô nào. A-1 đợt 7 đạt cùng 22/36 trong một phiên 8,0M. Hai lượt A đều dừng ở cùng trần 22/36 và không tự tìm ra S3/S6, nên A+ chỉ hơn A nếu lời nhắc "tự kiểm lại" khiến nó phát hiện thêm nơi lưu.

### Cited Findings
- [Đ] A đợt 6 theo tệp phiên: 25 491 (phiên chết ngay); 14 818 499 (21:11 → 21:41Z); 5 781 724 (15 phút); 3 195 569 (15 phút); 13 547 507 (24 phút); tổng 37 368 790 — `R6\codex-home-A\sessions\…`, `R6\results\tokens.json`.
- [Đ] Điểm A đợt 6 theo mốc: seg1 15/36 (S1, S2, S5 sạch; S4 0/7); seg2cut 15/36; seg2 22/36 (+S4); seg3 22/36 — `R6\results\A-L1\seg1.hidden.json.matrix.json`, `seg2cut.…`, `seg2.…`, `seg3.…`.
- [Đ] README đợt 6: A "báo xong sau 3 phiên (≈ 84 phút agent)", trần 120 phút cắt B — `D:\Projects\bint\docs\evals\bench-bint-vs-plain-v6-20261120\README.md` (bảng mục 1). README đợt 7 ghi cột A đợt 6 "120 phút (cắt)" — `DOC\README.md:30`; hai chỗ mâu thuẫn, tệp phiên ủng hộ ≈ 84 phút.
- [Đ] A-1 đợt 7: 1804,8 s, `end: agent_exit`, 8 005 135 token vào; lời kết tự nêu giới hạn: "Dữ liệu cũ chưa được sửa tại chỗ. Bộ lọc vẫn có thể bỏ sót… Bộ kiểm thử CLI đầy đủ chưa xác nhận được vì hết thời gian trên Windows" — `R7\runs\A-1\run.json`, `R7\runs\A-1\log\01-codex.out`.
- [Đ] A-1: 32 tệp, +718/−130; build được; N1 qua — `R7\runs\A-1\grade.json`.
- [Đ] Thiết kế A+ đã đăng ký: lặp `codex exec resume` với lời `SELF_CHECK` ("Rà mọi chỗ bint lưu dữ liệu xem còn chỗ nào bí mật có thể lọt, chạy test liên quan…") tới khi token vào ≥ trung vị của B; mục tiêu đã tính: 43 714 636 — `R7\tools\run.py` (hằng `SELF_CHECK`, hàm `run_ap`), `R7\log\ap-target.json`, `R7\log\orchestrate.log`.
- [Đ] A+ bị bỏ lúc 22:35 theo yêu cầu người dùng; có thư mục thử `R7\smoke\Ap-0` — `DOC\prereg-and-incidents.md:16`.

### Inferences
- [S] Hiệu suất token của A giảm nhanh: đợt 6, 22,5M token sau khi đạt 22/36 (phần còn lại của phiên 2 và phiên 3) không thêm ô nào. Nếu A+ hành xử như A đợt 6, đốt thêm ~35M token để bằng ngân sách B sẽ không qua được 22/36, và B/B′ hơn A+ 22 điểm %; khi đó tiêu chí 1 (B không hơn A+ ≥ 10 điểm) không kích hoạt.
- [S] Nhưng lời `SELF_CHECK` của A+ chỉ thẳng vào "mọi chỗ bint lưu", sát hơn lời "làm tiếp" của đợt 6; S3/S6 (mô tả phiếu, tài liệu phiếu) là thứ một lượt rà theo nơi lưu có thể tìm ra. Một lượt A+ tìm ra S3 và S6 với bộ lọc sẵn có (đã có PEM) sẽ đạt 36/36, vượt cả B và B′. Kết quả A+ vì thế hai đỉnh (22 hoặc ~36) và là phép đo đáng làm nhất còn thiếu.
- [S] A-1 kết thúc ở 1804,8 s, sát 30 phút; đợt 6 phiên 1 bị cắt cứng ở 30 phút. Khung chạy đợt 7 không đặt trần 30 phút cho A (`cap_s` 10800), nên có thể là trùng hợp; chưa loại trừ được một giới hạn phía Codex.
- [S] A là nhánh duy nhất có lời kết trung thực về phần chưa làm và không để lại hồi quy thật; chi phí 1/5,5 của B.

### Gaps
- Không có lượt A+ nào; `R7\smoke\Ap-0` chưa được đọc (lượt thử, không chấm).
- Phương sai của A mới có 2 điểm (22, 22) ở hai giao thức khác nhau (3 phiên cắt cứng so với 1 phiên tự dừng).

## Điều dữ liệu thô nói khác README hoặc README bỏ sót (tổng hợp)

### Takeaway
Ba kết luận chính của README đứng vững (22/30/30; không nhánh nào qua N2; B và B′ bị trần cắt, kiểm chứng chưa từng xanh). Nhưng phần giải thích về B′ dựa trên hai chẩn đoán sai (agent bỏ qua `bint task check`; mất lượt do mạng) và README bỏ sót một lỗi dựng mẫu khiến kiểm chứng của B/B′ không thể xanh. Đợt 7 chưa thử được giai đoạn 3 và 4 của B′ đúng thiết kế.

### Cited Findings
- [Đ] Đúng: điểm ma trận, diffstat (A 32 tệp +718/−130; B 76 tệp +2983/−234; B′ 68 tệp lúc 9100 s, 87 tệp +3299/−219 cuối), token, thời gian (B 10 808,5 s `cap`; B′ 13 082,5 s `cap`; A 1805,1 s `agent_exit`), B không tới review, B′ có review 8 high — `R7\runs\*\grade.json`, `R7\runs\*\run.json`.
- [Đ] Sai/thiếu 1: PEM ở A (xem mục ma trận) — `R7\runs\A-1\grade.json` so với `DOC\README.md:8-9`.
- [Đ] Sai/thiếu 2: `bint task check` 24 lần bị sandbox chặn; lượt lõi không nhận chỉ dẫn B′ — `TP\logs\b-feat-*.log`, tệp phiên `…21-37-25-01a1f964…` so với `DOC\README.md:44,53-55`.
- [Đ] Sai/thiếu 3: lượt b-feat "đứt mạng" đã `turn.completed` — `TP\logs\b-feat-01M7WXAMJMX449FBSME4KHV9HM.log:4,212` so với `DOC\README.md:79-80`.
- [Đ] Sai/thiếu 4: hai test đồng bộ skill đỏ sẵn ở commit gốc `e41bb69` của `tmpl-B` — `R7\runs\B-1\repo` (`git show bench:…`), so với `DOC\README.md:27` ("2 hồi quy đồng bộ skill").
- [Đ] Sai/thiếu 5: N2 của A-1 và B-1 — `R7\runs\A-1\n2.json` (9,62), `R7\runs\B-1\n2.json` (6,61) so với `DOC\README.md:26` (19,3; 11,7). Bản chép trong `DOC\results\` cũng là 9,62 và 6,61, tức README mâu thuẫn với chính thư mục kết quả của nó.
- [Đ] Sai/thiếu 6: số hồi quy thật của B (2 có bằng chứng) so với "≥ 4" và "4–5" — `R7\runs\B-1\gotest.txt`, `TB\logs\verify-*.log` so với `DOC\README.md:27,59`.
- [Đ] Sai/thiếu 7: 7/8 phát hiện review đã có bản sửa trong cây — `TP\logs\b-feat-01M7X1WK3T6AF6YXXYPYRFN579.log` so với `DOC\README.md:61-62`.
- [Đ] Sai/thiếu 8: "A đợt 6: 120 phút (cắt)", "(3 phiên)" — `R6\results\tokens.json` (5 tệp phiên), README đợt 6 (≈ 84 phút, agent báo xong) so với `DOC\README.md:29-30`.
- [Đ] Bỏ sót: B bị `scope.allow` do chính b-req đặt chặn không cho sửa `internal/cli/task.go:715`, nên cổng mục tiêu tự viết không thể xanh (86 ô `task-documents-ledger` đỏ tới cuối) — `TB\task.yml`, `TB\state.json` (`criteria[9]`, `errors[3]`). B′ có `scope.allow: []` — `TP\task.yml`.
- [Đ] Bỏ sót: Bint ghi "luồng B′: lõi xanh, đã gộp và đóng băng thước đo" trong khi verify-2 `passed: false` (chỉ còn 2 test lỗi mẫu đỏ) — `R7\runs\Bp-1\log\03-resume.out` (`notes`), `TP\state.json` (`verifications[1]`).
- [Đ] Bỏ sót: lượt B-1 đầu tiên (21:29) kết thúc `no_task` sau 354 s vì driver không đọc được JSON, phải dựng lại; B-1 chính thức bắt đầu 21:35:53 — `R7\aborted-1\B-1.driver.txt`, `R7\aborted-1\orchestrate.log`.
- [Đ] Bỏ sót: tải máy không đều: 21:35–22:21 có 5 lượt Bint song song (B-1, Bp-1, B-2, Bp-2, B-3); A-1 chạy 22:22–22:52 cùng 2 lượt; chấm cuối A-1 và B-1 song song, Bp-1 một mình — `R7\log\orchestrate.log`.
- [Đ] Bỏ sót: B-1 dùng 5/6 thao tác người dùng cho phép; thêm một lần dừng nữa là lượt kết thúc `user_actions_exhausted` — `R7\runs\B-1\run.json`, `R7\tools\run.py` (`MAX_USER_ACTIONS`).
- [Đ] Thư mục `DOC` chưa được commit (`?? docs/evals/bench-abbp-v7-20261121/`) — `git status` trong `D:\Projects\bint`.

### Inferences
- [S] Điều đợt 7 thực sự đo được về B′: b-req gọn (giữ được độ rộng nơi lưu, hỏi 1 câu, 5 phút thay vì 6,5 phút + cổng kế hoạch lớn), thăm dò song song (không làm chậm dòng mã đầu), một lượt review + một vòng sửa (hiệu quả). Điều chưa đo: kiểm hẹp theo mốc/kiểm nền (bị sandbox chặn 24/24 lần) và "lõi trước" (đề bài lượt lõi thiếu chỉ dẫn). So sánh B′ với B ở "kết quả kiểm chứng đầu tiên" vì vậy chưa có giá trị.
- [S] Dự đoán của báo cáo trước "thước đo do ngữ cảnh độc lập viết gỡ được ô lọt" chưa bị bác bỏ một cách sạch: thăm dò nhận tiêu chí của b-req và lỗ PEM nằm ngay trong lời người dùng giả. Muốn thử đúng, thăm dò chỉ nên nhận yêu cầu gốc, hoặc dùng danh mục loại bí mật từ ngoài (đề xuất 3 của README).
- [S] Với hai test nền đỏ sẵn, không lượt B/B′ nào trong đợt 7 có thể kết thúc `done`; "tỷ lệ bị trần cắt 2/2" của B/B′ một phần là hệ quả dựng mẫu chứ không chỉ do Windows chậm. Bất kỳ đợt lặp nào cũng phải sửa `tmpl-B` (chạy `bint sync` rồi commit) trước.
- [S] Thứ tự độ tin của các chỉ số đợt 7: ma trận test ẩn (chắc, tất định) > token (chắc, nhưng B′ lẫn phần lặp do sự cố) > thời gian tường (nhiễu tải và sự cố) > số test repo đỏ (nhiễu nền + tải, gần như không so được) > N2 (hỏng chắc chắn, độ lớn không so được).

### Gaps
- Chưa đọc mã Bint ở `63a6114` để xác nhận hai lỗi nghi ngờ (đề nối tiếp phiên làm rơi chỉ dẫn B′; coi sự kiện `error` là thất bại bước); kết luận dựa trên log và tệp phiên.
- Chưa đọc `R7\aborted-2\*` (ba lượt chạy ~40 phút rồi bị huỷ) ngoài dòng driver; có thể chấm cây dở của chúng để có thêm điểm phương sai cho b-req/b-goal.
- Không chạy lại test, N2 hay bộ `go test ./...` nào (theo ràng buộc), nên mọi phân loại "chập chờn" dựa trên đối chiếu chéo giữa các log có sẵn.
