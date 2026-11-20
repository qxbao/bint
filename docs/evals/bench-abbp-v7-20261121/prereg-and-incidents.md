# Đợt 7: A / A+ / B / B′ trên L1 (đăng ký trước, 2026-11-20)

Nguồn thiết kế: `reports/Luồng core Bint có lệch hướng.md`, mục "Thử nghiệm phân định và tiêu chí dừng".

## Cố định
- Model: Codex `gpt-6.2-sol`, effort high, `model_context_window` 872000, provider codex-lb (cùng cấu hình đợt 6). Mọi bước Bint chạy Codex cùng model.
- Bint điều phối: binary build từ commit trong `log/bint-commit.txt` (63a6114, có luồng B′). Repo bài: bint ở 48dbfad (cùng gốc đợt 6).
  A/A+: repo đã gỡ Bint (4db2657, như đợt 6). B/B′: 48dbfad + skill/hook cập nhật theo binary điều phối (`tmpl-B`), `.bint/local.yml` như đợt 6.
- Bài: L1 (prompt nguyên văn đợt 6). Người dùng giả tự động: một câu trả lời cố định lấy từ thẻ người dùng cho mọi câu hỏi;
  cổng kế hoạch lớn → resume; lát giữ → `--slice keep`; hết vòng sửa / kiểm chứng cuối đỏ → lượt dừng với cây hiện tại; tối đa 6 thao tác.
- Mỗi lượt môi trường sạch riêng: bản sao repo, CODEX_HOME, thư mục trạng thái, TMP. Tác vụ B/B′ chạy `--shared` (cây chấm = cây repo).
- Trần 180 phút tường mỗi lượt (đợt 6: 120 phút cắt B). Ghi riêng tỷ lệ lượt bị trần cắt.
- A: một `codex exec` tới khi agent tự dừng. A+: như A, rồi lặp `codex exec resume` với lời "tự kiểm lại và sửa" tới khi token vào
  (gồm cache) ≥ **trung vị token vào của B** đo ở pha 1, hoặc hết trần.
- **Đổi lúc 22:20 theo yêu cầu người dùng: 1 lượt mỗi nhánh** (B-2, Bp-2, B-3 đang chạy bị huỷ, lưu ở aborted-2/). Pha 1: B, B′, A song song; pha 2: A+.
- **Đổi lúc 22:35 theo yêu cầu người dùng: bỏ A+**; B và B′ so với A đợt 6 (cùng repo gốc 4db2657, model, test ẩn) và A-1 của đợt này.

## Chấm (tất định)
- Chính: ma trận test ẩn `hidden_l1.py` (36 ô: 7 loại bí mật × 6 nhóm nơi lưu, S5 một ô), điểm = ô sạch / 36.
- Phụ: N1 (kiểm chứng thường vẫn chạy, không che quá tay), N2 (verify log lớn không chậm quá 3× bản gốc), build được,
  `go test ./...` của cây cuối, thời gian tường, token vào/ra, cách kết thúc (tự xong / trần / dừng chờ người).

## Tiêu chí dừng (đăng ký trước; chỉ áp cho tầng L1)
1. B không hơn A+ ít nhất 10 điểm % ô sạch (trung bình hiệu, khoảng tin cậy 95 % không chứa 0) → bằng chứng bỏ luồng classic làm mặc định ở tầng này.
2. B′ hơn A+ ≥ 10 điểm % (CI 95 % không chứa 0) với token ≤ 2,5× A → bằng chứng giữ B′ cho việc cỡ L1.
3. B′ không hơn A+ → ở tầng L1 không có lợi thế cấu trúc; Bint nên định vị là lớp kiểm chứng/lưu vết quanh một agent đơn.
Với n = 5 mỗi nhánh, CI tính bằng t (Welch) trên hiệu trung bình; nhánh không ghép cặp tự nhiên nên không dùng hiệu ghép cặp.

## Giới hạn biết trước
- Chỉ một tầng việc (L1). Tầng nhỏ/rõ (T1) chưa có test ẩn tất định trên máy này; tầng vượt một ngữ cảnh chưa có bài.
- Hai phép cắt bỏ rẻ (B không b-goal; B với thước đo độc lập) chưa nằm trong đợt này.
- Pha 1 và pha 2 chạy ở hai khoảng thời gian khác nhau (tải máy/proxy khác nhau) → thời gian tường so giữa pha kém tin cậy hơn ô sạch.
- n = 5 chỉ phát hiện được chênh lệch lớn (~ 1,5–2 độ lệch chuẩn).

## Sự cố trong lúc chạy
- 22:49: người điều phối gắn dlv vào tiến trình `bint run resume` của Bp-1 để xem nó có treo không (thực ra đang chạy kiểm chứng `go test`
  chậm, ~20 phút); thoát dlv đã giết tiến trình ở giây 4337. Script tự `bint run resume`; lần kiểm chứng đang chạy dở bị mất và chạy lại.
  Bp-1 mất khoảng 38 phút ngân sách 180 phút vì sự cố này (không do Bint; đính chính 2026-11-21: bản đầu ghi 20–25 phút, mâu thuẫn với dòng bù
  bên dưới; tệp thô ủng hộ 38,4 phút).
- 00:09: Bp-1 `failed` ở giây 9100. Đính chính 2026-11-21: đây **không** phải lượt bị mất vì lỗi mạng. Log
  `logs/b-feat-01M7WXAMJMX449FBSME4KHV9HM.log` có sự kiện `error` "Reconnecting... 2/5 (stream disconnected before completion: websocket closed
  by server before response.completed)" ở dòng 4 nhưng lượt vẫn `turn.completed` ở dòng 212; Bint (bản `63a6114`) coi sự kiện `error` đầu tiên là lỗi
  bước nên đánh `failed` một lượt đã xong, và B′ được chạy thêm một lượt mở rộng ngoài thiết kế. Cây lúc đó đã chấm: `runs/Bp-1/grade-failed-9100s.json` (30/36). Chạy tiếp bằng `tools/cont.py` với
  ngân sách = phần còn của trần 180 phút (1700 s) + bù sự cố dlv 22:49 (lần kiểm chứng bị giết chạy từ ~22:11, mất ~38 phút = 2280 s) = 3980 s.
