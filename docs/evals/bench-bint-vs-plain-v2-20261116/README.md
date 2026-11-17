# Benchmark Bint so với không dùng Bint, lần 2: người dùng mơ hồ, task rủi ro (2026-11-16)

Plan: [../../benchmark-bint-vs-plain-v2-plan.md](../../benchmark-bint-vs-plain-v2-plan.md).
Lần 1: [../bench-bint-vs-plain-20261116/](../bench-bint-vs-plain-20261116/README.md).

## Kết luận

**Có lợi, đánh đổi** (theo bảng mục 8 của plan, chốt trước khi chạy):

- T1 hoà.
- T2 B tốt hơn, chỉ hơn 1 lỗi critical.
- B tốn khoảng 2,1–2,5 lần token vào và 3,5–4,7 lần thời gian tường.

Kết luận này yếu:

- Mỗi task chỉ chạy một lần.
- Chênh lệch quyết định ở T2 chỉ là một lỗi critical: tóm tắt kết quả của agent bị ghi rõ vào SQLite.
- Không bên nào hoàn thành mục tiêu của T2: cả hai còn 5–6 lỗi critical.

Plan mục 9 đề xuất chạy thêm một lần lặp khi kết quả rơi vào loại này.

| Task | A: Codex trực tiếp | B: Bint `b-do` | Kết luận task |
|---|---|---|---|
| T1 dọn dữ liệu cũ | 0 critical, 3 medium | 0 critical, 2 medium | Hoà: critical bằng nhau, medium chỉ chênh 1 (luật cần ≥ 2) |
| T2 không lưu bí mật | 6 critical, 3 medium | 5 critical, 5 medium | B tốt hơn: ít hơn 1 critical |

Phát hiện quan trọng nhất không nằm trong bảng. Với người dùng mơ hồ, luồng của Bint có ba lỗi thật (mục "Trải nghiệm"):

- Bỏ qua lập kế hoạch.
- Đóng "done" khi chưa làm gì.
- Kẹt mà không có lối đi tiếp.

Chính những chỗ này lẽ ra là lợi thế của Bint với kiểu người dùng này.

## Thiết lập

| | A | B |
|---|---|---|
| Repo | Clone `ff262a2` (nhánh `fix/bench-frictions`, đã sửa 4 điểm vướng của lần 1), gỡ Bint | Clone `ff262a2`, `bint init --hooks`, index codegraph, skill bản mới |
| Giao việc | `codex exec --sandbox workspace-write --model gpt-6.1-sol` | `bint run b-do --harness codex --model gpt-6.1-sol --autonomy auto` |
| Người dùng | Opus theo giao thức low-tech (thẻ người dùng: [log/persona-cards.md](log/persona-cards.md)) | Như A |

Prompt giống nhau cho hai bên:

- T1: "Thư mục dữ liệu của bint trên máy tôi cứ phình to mãi, làm cho nó tự dọn bớt mấy thứ cũ đi."
- T2: "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi."

Commit gốc `ff262a2` tự nó đã đỏ ở hai chỗ:

- `TestGoldenSystemScreens`: nhánh sửa đổi skill `b-feat` nhưng chưa sinh lại golden.
- `TestDocCommandsExist`: README nêu `bint flow simulate`, lệnh chưa tồn tại.

Cả bốn repo được thêm một commit `bench:` giống hệt nhau để baseline xanh ([log/baseline-fix.patch](log/baseline-fix.patch)). **Nhánh `fix/bench-frictions` vẫn còn lỗi golden này, cần sửa trước khi merge.**

## Kết quả chi tiết

### Nguồn lỗi và cách gộp

Lỗi được gộp từ ba nguồn rồi bỏ trùng:

1. **Test ẩn** ([hidden/hidden2.py](hidden/hidden2.py)): kịch bản D1–D9 cho T1, S1–S6, N1 và N2 cho T2, cùng kiểm repo C1–C6.
   - Đối chứng chạy trước agent ([results/controls/](results/controls/)):
     - binary gốc không dọn gì thì chỉ D6 và D8 hỏng;
     - bộ dọn ngây thơ thì D1 ×2, D2 và D3 hỏng (critical);
     - binary gốc ở T2 thì lộ bí mật ở S1–S6.
   - Có một giới hạn: đối chứng khoá SQLite 25 giây không làm D5 hỏng, vì Bint chịu được khoá này. Vì vậy chưa có bằng chứng D5 bắt được lỗi đua.
2. **Review đối kháng mù:** một agent Opus mới cho mỗi task, nhãn X/Y gán ngẫu nhiên. Kết quả: [review/T1-findings.json](review/T1-findings.json), [review/T2-findings.json](review/T2-findings.json).
3. **Xác minh mù:** một agent Opus khác đọc mã và chạy binary trong sandbox. Kết quả: [review/T1-verified.json](review/T1-verified.json), [review/T2-verified.json](review/T2-verified.json).

Bảng ghép nhãn ([results/blind-mapping.json](results/blind-mapping.json)) chỉ được mở sau khi mọi khâu chấm xong: T1-X = B, T1-Y = A, T2-X = B, T2-Y = A.

Lệnh kích hoạt T1 do một agent mù điền theo `--help` và tài liệu của từng bản ([review/T1-X.trigger.json](review/T1-X.trigger.json), [review/T1-Y.trigger.json](review/T1-Y.trigger.json)):

- A: `bint clean --dry-run` và `bint clean`, giữ 30 ngày.
- B: không có lệnh dọn tay. Việc dọn chạy tự động sau khi nạp spool (kích hoạt qua `bint status`), giữ 90 ngày.

### T1: dọn dữ liệu cũ

| | A | B |
|---|---|---|
| Critical | 0 | 0 |
| Medium | Tự xoá `backups/` sau 30 ngày, không hỏi, chạy trước khi kịp xem trước. Review chấm critical, xác minh hạ xuống medium | D6 + review X1: phạm vi quá hẹp. Chỉ xoá log revalidate/evolve đã ghi sổ hoàn tất và hook-trace; log phiếu, eval-work, backups, spool không đụng tới, nên trên fixture giải phóng 0 byte |
| | C4 + C5: staticcheck đỏ (SA4006 `internal/app/cleanup.go:233`). Một nguyên nhân nhưng tính 2 mục theo luật của plan | D8: không có cách xem trước. Xác minh coi là low vì thẻ ghi "có thì tốt", nhưng test ẩn đã chốt mức medium |
| Lỗi bị hạ xuống low, không tính | Mốc 30 ngày xoá log eval mà evolve đọc trong cửa sổ 90 ngày | — |
| Test ẩn | Qua cả D1–D9: xem trước không đổi gì; dọn 3,3 MB log phiếu cũ đã kết thúc; giữ phiếu chưa xong, worktree bẩn, đích symlink, phiếu mới; chạy đồng thời với một lượt Bint vẫn ổn | Chỉ D6 và D8 hỏng |

Nếu gộp C4 và C5 thành một lỗi thì A còn 2 medium, bằng B, và kết luận T1 vẫn là hoà.

### T2: không lưu bí mật

Cả hai bên chỉ che log: log kiểm chứng, log agent, trace hook. Cả hai bỏ sót mô tả phiếu, review và tài liệu phiếu, kể cả bản chiếu `.bint/tasks` trong repo. Cả hai bỏ sót khoá riêng PEM và mã AKIA ngay trong log.

| Lỗi | A | B |
|---|---|---|
| S1 log kiểm chứng (PEM, AKIA) | critical | critical |
| S2 log agent (PEM, AKIA) | critical | critical |
| S3 mô tả phiếu `task.yml`, bản chiếu ledger | critical | critical |
| S4 `review.json` / `review.md` | critical | critical |
| S5 sự kiện hook trong SQLite (A: ghp_ trần, AKIA, PEM; B: AKIA, PEM) | critical | critical |
| `result_summary` của agent vào spool và `runs` | **critical** (Y1) | che đúng |
| S6 requirement/plan/tasks | medium | medium |
| `state.json` và sự kiện `task.*` (evidence, reason, lệnh kiểm chứng) | medium | medium |
| Dạng bí mật bị sót | medium: ghp_/github_pat_/sk- đứng trần, token trong URL, `key = value` có dấu cách, `curl -u`, xoxb-, npm_ | medium: SECRET_KEY, sk_live_, `Authorization: token`, Cookie, `curl -u`, npm_ |
| Dòng bắt đầu bằng `[`/`{` mà ngoặc không cân làm mất phần còn lại của log, kể cả dòng `--- FAIL` | — | **medium** (X6) |
| N2 hiệu năng (verify 20 MB; gốc 0,96 s; ngưỡng 4,88 s; đo lại 3 lần khi máy rảnh) | qua sát ngưỡng (4,82 s) | **hỏng** (5,33 s) |
| N1 không che quá tay; C1–C6 build/vet/staticcheck/test | qua | qua |
| Dòng log > 64 KiB bị bỏ (cả hai bên) | low | low |

Tổng: A 6 critical, 3 medium; B 5 critical, 5 medium. N2 của hai bên thực chất như nhau: khoảng 0,2 giây mỗi MB, tăng tuyến tính, A nằm ngay dưới ngưỡng.

## Chi phí

Token đo từ rollout Codex, lọc theo `cwd`, bằng cùng công cụ với lần 1.

| Lượt | Token vào (cache / không cache) | Token ra | Thời gian tường | Tin nhắn người dùng | Thao tác kỹ thuật | Kẹt |
|---|---|---|---|---|---|---|
| A-T1 | 7,57M (7,05M / 0,51M) | 31k | 19 phút | 1 | 0 | 0 |
| A-T2 | 6,91M (6,72M / 0,19M) | 36k | 18 phút | 0 | 0 | 0 |
| B-T1 | 15,64M (15,04M / 0,60M) | 74k | 68 phút | 1 | 0 | 0 |
| B-T2 | 16,97M (15,22M / 1,75M) | 117k | 85,5 phút (blocked) | 0 | 0 | 1 |

Tỷ lệ B/A:

- Token vào: ×2,07 ở T1 và ×2,46 ở T2.
- Token ra: ×2,4 ở T1 và ×3,2 ở T2.
- Thời gian tường: ×3,5 ở T1 và ×4,7 ở T2.

## Trải nghiệm của người dùng low-tech

Nhật ký nguyên văn ở [log/user-actions.md](log/user-actions.md).

**A-T1:**

- Agent làm `bint clean` rồi bảo người dùng chạy `bint clean --dry-run`. Lệnh báo "lệnh lạ", vì agent sửa mã nhưng chưa cài binary mới.
- Tin nhắn #1 dán nguyên lỗi đó. Agent đưa lệnh `go build -o "$(command -v bint)" ./cmd/bint`; người dùng chạy nguyên văn, và lần thử sau đó qua.
- Lần thử chạy trên "máy người dùng" giả (bin riêng, dự án mẫu), không đụng binary đóng băng.

**A-T2:** agent báo xong, nói tính năng tự bật, không có lệnh nào để thử.

**B-T1:**

1. Bint xếp yêu cầu vào loại "**yêu cầu ngắn, rõ: bỏ b-req**".
2. b-feat thấy mơ hồ, dừng lại và hỏi người dùng qua `bint task error`. Bint không coi đó là câu hỏi: kiểm chứng xanh vì diff rỗng, review ghi "tính năng chưa được triển khai", vậy mà cổng done vẫn cho qua và in "✓ Hoàn thành".
3. Người dùng chỉ thấy câu hỏi khi chạy dòng "Chi tiết: bint task show …".
4. Theo quyết định của chủ dự án giữa lượt, người dùng gửi tin nhắn #1 bằng một `bint run b-do` mới (Bint không có cách trả lời vào phiếu đã done). Lần này Bint chạy đủ b-req, b-feat và b-review.

**B-T2:**

1. Bint cũng bỏ b-req.
2. Review vòng 1 tìm ra 5 lỗi rò bí mật thật, trong đó 2 high, và vòng sửa đã chữa một phần. Đây là điểm cộng rõ nhất của Bint trong lần đo này.
3. Sau 3 vòng review, Bint dừng ở trạng thái blocked: "còn 1 phát hiện nghiêm trọng… cần người xem review.md".
4. Người low-tech không làm được việc đó, và Bint không in lệnh nào để tiếp tục. Lượt này tính là kẹt và dừng ở 85,5 phút.

Kết quả T2 của B được đo ở đúng trạng thái đó, không có ai xử lý phát hiện còn lại.

### Lỗi của Bint lộ ra (đề xuất sửa)

1. **Classify coi câu mơ hồ ngắn là "rõ" và bỏ b-req.** Người dùng mơ hồ chính là trường hợp cần b-req nhất.
2. **Cổng done cho qua khi diff rỗng** dù review nói chưa triển khai và b-feat đã ghi vướng mắc. Câu hỏi b-feat ghi bằng `task error` không thành trạng thái chờ người.
3. **Trạng thái blocked "cần người xem review.md" không có lối ra** cho người không đọc mã: không có lệnh `bint run resume` hay lựa chọn "chấp nhận / sửa tiếp" in ra.
4. **Cả hai bên đều không nói cho người dùng biết phải cài lại binary.** Bên A ít ra còn đưa lệnh build khi được hỏi; Bint thì không in hướng dẫn dùng nào khi done.

## Sự cố môi trường

Chi tiết ở [log/env-incidents.md](log/env-incidents.md).

**Ổ `/` đầy 100% lúc khoảng 10:30–10:47Z.**

- Lần kiểm chứng `go test` đầu tiên của B-T2 đỏ vì "no space left on device", nên Bint chạy thêm một vòng sửa, tốn thêm khoảng 17 phút và token so với khi đĩa đủ.
- Tôi đã giải phóng dung lượng: xoá thư mục tạm của hai lượt A đã xong và cache staticcheck. Tôi không can thiệp vào lượt chạy.
- Không loại được ảnh hưởng: chi phí của B-T2 có thể bị đội lên.

**Sandbox Codex lại tạo `/tmp/.git`** dù mỗi lượt có `TMPDIR` riêng. Agent bên A có nhắc tới lỗi Git/VCS khi chạy toàn bộ test. Test ẩn chạy với `TMPDIR` riêng nên không bị ảnh hưởng.

## Giới hạn

- Mỗi task chạy một lần. Kết luận T2 phụ thuộc vào đúng một lỗi critical; T1 phụ thuộc vào cách đếm C4 và C5.
- Thước đo "lỗi còn lại" không thưởng cho phần đã làm được. B-T1 lần đầu không làm gì nên có 0 critical: nếu dừng ở đó, B vẫn "hoà" T1 về critical. Chủ dự án đã cho người dùng hỏi tiếp, nhưng điểm yếu của thước đo vẫn còn.
- Có hai quyết định giao thức phát sinh giữa lượt, cả hai được ghi nguyên văn trong nhật ký:
  - **Có cho người dùng hỏi tiếp ở B-T1 không:** chủ dự án quyết định có.
  - **Đo bước thử dùng trên "máy người dùng" giả.**
- Mục "phải bật tay" (T2) không cần xét: cả hai bên đều bật mặc định.
- Opus đóng vai người low-tech. Mọi tin nhắn được ghi nguyên văn để chủ dự án tự soát.
