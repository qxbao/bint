# Kế hoạch: benchmark Bint so với không dùng Bint, lần 2 (người dùng mơ hồ, task rủi ro)

Trạng thái: **đã chạy 2026-11-16**. Kết luận: *Có lợi, đánh đổi* (T1 hoà, T2 B hơn 1 critical; B tốn ~2,1–2,5× token vào, 3,5–4,7× thời gian). Biên bản: [evals/bench-bint-vs-plain-v2-20261116/](evals/bench-bint-vs-plain-v2-20261116/README.md).

Lần 1: [benchmark-bint-vs-plain-plan.md](benchmark-bint-vs-plain-plan.md), biên bản
[evals/bench-bint-vs-plain-20261116/](evals/bench-bint-vs-plain-20261116/README.md).

## 1. Vì sao có lần 2

Lần 1 cho bên A một prompt ngắn trỏ vào một đặc tả chi tiết (tên khoá, tên lệnh, mục kiểm thử, nghiệm thu). Như vậy bên A
được nhận sẵn phần việc mà b-req của Bint làm, và người điều phối đóng vai người dùng giỏi: tự chạy `go test`, biết câu nào
đỏ. Đó là kịch bản bất lợi nhất cho Bint và không đại diện cho người dùng thật.

Lần 2 đổi đúng hai biến, phần còn lại giữ như lần 1:

1. **Người dùng mơ hồ.** Prompt là một câu nói thường, không nêu tệp, không nêu tên khoá hay tên lệnh, không có đặc tả. Người
   dùng low-tech: không đọc mã, không chạy test, không sửa tệp.
2. **Task rủi ro.** Chọn việc dễ để lại lỗi critical hoặc medium (mất dữ liệu, lộ bí mật, đua tiến trình). Phép đo chính là
   **số lỗi critical/medium còn lại**, không phải số test chức năng qua.

Giữ như lần 1: Codex `gpt-6.1-sol` qua codex-lb cho mọi bước của cả hai bên; mỗi task một lần cho mỗi bên; bốn lượt chạy
song song; trần 90 phút mỗi lượt; Opus điều phối; test ẩn và chấm mù.

## 2. Thiết kế

| | Bên A (không Bint) | Bên B (Bint) |
|---|---|---|
| Repository | Clone ở commit gốc (mục 4), gỡ dấu vết Bint như lần 1 | Clone ở cùng commit, `bint init --hooks`, index codegraph |
| Giao việc | `codex exec --json --sandbox workspace-write --skip-git-repo-check --cd <repo> --model gpt-6.1-sol` | `bint run b-do --harness codex --model gpt-6.1-sol --autonomy auto` |
| Người dùng | Opus theo **giao thức người dùng low-tech** (mục 6.1) | Như A |
| Binary Bint | Không dùng | Đóng băng, build từ commit gốc, đặt đầu `PATH` |

Hai task nhân hai bên là 4 lượt, chạy song song.

## 3. Hai task

Cả hai đều là việc thật, chưa ai làm ở commit gốc, và nằm trong khu vực tri thức của Bint có liên quan. Đây là lợi thế hợp
lệ, được ghi vào biên bản.

### T1: Dọn dữ liệu cũ (rủi ro mất dữ liệu)

- **Hiện trạng ở commit gốc:** không có lệnh hay cơ chế dọn `runtime/tasks`, log, `worktrees`, `spool`; `memory.retention_days`
  chỉ dùng làm cửa sổ cho `bint evolve`.
- **Prompt (giống nhau cho hai bên):** "Thư mục dữ liệu của bint trên máy tôi cứ phình to mãi, làm cho nó tự dọn bớt mấy thứ
  cũ đi."
- **Thẻ người dùng** (người dùng chỉ trả lời khi được hỏi, bằng lời thường; agent không thấy thẻ):
  - Mục tiêu: lấy lại dung lượng.
  - Điều không muốn: mất việc đang làm dở, mất thứ còn cần.
  - "Giữ bao lâu?": khoảng vài tháng.
  - "Tự động hay chạy lệnh?": sao cũng được, miễn an toàn.
  - "Có cần xem trước không?": có thì tốt.
  - Mọi câu kỹ thuật khác: "tôi không rành, bạn tự quyết cho an toàn."
- **Rủi ro chính** (dùng cho test ẩn và review): xoá phiếu chưa kết thúc hoặc worktree còn thay đổi chưa đưa về; theo symlink
  xoá ra ngoài thư mục trạng thái; đua với lượt đang chạy (khoá `fslock`); SQLite lệch với tệp; dọn khi đang chạy trên Windows
  (tệp bị khoá); không có cách xem trước.

### T2: Không lưu bí mật (rủi ro lộ thông tin)

- **Hiện trạng ở commit gốc:** `detect.Redact` chỉ áp cho lệnh, câu truy vấn và ghi chú trong sự kiện hook
  (`internal/harness/hookcore/hook.go`). Log kiểm chứng, log lượt chạy, review, mô tả phiếu và tài liệu phiếu chưa được che.
- **Prompt:** "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi."
- **Thẻ người dùng:**
  - Mục tiêu: không để bí mật nằm ở bất kỳ chỗ nào bint lưu.
  - Điều không muốn: bint chạy hỏng hay chậm hẳn đi.
  - "Có cần bật tay không?": không, nên mặc định bật.
  - "Che những loại nào?": mấy loại hay gặp như mật khẩu, token, khoá API.
  - Câu kỹ thuật khác: "bạn tự quyết."
- **Rủi ro chính:** bỏ sót nơi lưu; che sau khi đã ghi, để lại bản chưa che; che quá tay làm hỏng hash, ID, chữ ký đầu ra mà
  Bint đang dùng; regex chậm thảm hoạ trên log lớn; bắt buộc người dùng tự bật.

## 4. Điều kiện trước khi chạy

**Sửa các điểm vướng lần 1, hay chạy nguyên trạng** (chờ chủ dự án quyết, mục 10). Lần 1 cho thấy chi phí thêm của B phần lớn
đến từ năm điểm vướng:

1. Kiểm chứng ngoài sandbox dùng chung `TMPDIR` với sandbox Codex.
2. b-feat ghi đè `tasks.md` làm hỏng hợp đồng artifact.
3. Tiêu chí "go test toàn bộ" do agent tự chấm trong sandbox.
4. Resume vẫn chạy vòng sửa dù lần kiểm chứng mới nhất đã xanh.
5. Planner hỏi điều kế hoạch đã trả lời.

Có hai lựa chọn:

- **(a) Khuyến nghị:** sửa 1–4 (điểm 5 là prompt, tuỳ chọn) trên nhánh riêng, merge, rồi lấy commit đó làm commit gốc cho cả
  hai bên. Đo Bint ở trạng thái đã sửa các lỗi đã biết.
- **(b)** Giữ commit gốc `2b2f74d` như lần 1, để so được trực tiếp với lần 1.

**Đã chọn (a), 2026-11-16.** Đã sửa điểm 1–4 trên nhánh `fix/bench-frictions` của repo chính: `767c570` (TMPDIR riêng khi kiểm chứng) và `ff262a2` (giữ hợp đồng `tasks.md`, hướng dẫn skill về tiêu chí trong sandbox, resume bỏ vòng sửa khi kiểm chứng mới nhất xanh), trên nền `339f134`. Hai commit này chưa push. Commit gốc của lần 2 là `ff262a2`. Clone hai bên từ repo chính tại commit đó, không từ bản clone v1.

Giới hạn còn lại của bản sửa:
- Điểm 3 chỉ sửa bằng hướng dẫn trong skill. Nếu agent vẫn ghi `unmet`, tác vụ vẫn cần người can thiệp.
- Điểm 5 (prompt của planner) chưa sửa.

Chuẩn bị chung (rút kinh nghiệm lần 1):

- Mỗi lượt chạy với `TMPDIR` riêng (`<bench>/tmp/<lượt>`) cho **cả hai bên** ngay từ đầu. Lần 1 cho thấy `TMPDIR` riêng
  không bị sandbox tạo `.git` giả. Kiểm và xoá `/tmp/.git` trước và sau đợt chạy.
- Trust project và hook trong `~/.codex/config.toml`, trong khối có đánh dấu, có bản sao lưu.
- Bên A gỡ dấu vết Bint như lần 1, kể cả sửa `TestDocCommandsExist`. `go test ./...` trên mọi bản phải xanh trước khi chạy.
- **Đối chứng âm:** chạy toàn bộ test ẩn trên binary gốc trước khi chạy agent. Mọi kịch bản rủi ro phải được fixture dựng
  thành công, và kết quả trên binary gốc phải đúng như dự kiến: T1 không dọn gì, T2 lộ ở các nơi chưa che.

## 5. Vì sao test ẩn khác lần 1

Prompt không chốt tên lệnh hay tên khoá, nên test không thể gọi cố định `bint <lệnh>`. Cách làm:

- **Fixture và phép kiểm do Opus viết trước khi chạy**, chủ dự án duyệt. Fixture dựng trạng thái (phiếu, worktree, log, bí
  mật). Phép kiểm chỉ nhìn tệp và CSDL sau cùng, ví dụ: tệp X còn không, chuỗi bí mật có xuất hiện ở đâu không.
- **Chỉ phần "cách kích hoạt" là thích nghi theo cài đặt.** Sau khi chạy, một agent đánh giá mới, mù nhãn, đọc `--help`, tài
  liệu và diff của từng bản để điền **một lệnh kích hoạt** cho T1 (ví dụ lệnh dọn và cờ xem trước, hoặc lệnh kích hoạt dọn tự
  động). Nó ghi lại lệnh đã dùng. Fixture và phép kiểm thì giữ nguyên cho hai bên.
- **T2 chạy với cấu hình mặc định**, vì người dùng nói "mặc định bật". Nếu cài đặt cần bật tay, kết quả với cấu hình mặc định
  là kết quả chính; kết quả khi đã bật chỉ ghi để tham khảo.

### Kịch bản T1

Fixture: repository nhỏ, sổ phiếu dựng qua CLI gốc, rồi lùi mốc thời gian để tạo dữ liệu "cũ". Mốc cũ là 200 ngày (khớp với
"vài tháng"), mốc mới là 5 ngày. Mọi tín hiệu tuổi đều được lùi đồng bộ: `state.json`, lịch sử, mtime của tệp, `runs` trong
SQLite.

| ID | Kịch bản | Mức nếu hỏng |
|---|---|---|
| D1 | Phiếu `blocked`/`interrupted` cũ (resume được) còn đủ tệp phiếu và log | critical |
| D2 | Phiếu worktree cũ chưa kết thúc, worktree có thay đổi chưa commit: worktree và nhánh còn nguyên | critical |
| D3 | Symlink trong thư mục log trỏ ra một tệp ngoài thư mục trạng thái: tệp đích còn nguyên | critical |
| D4 | Không tệp nào ngoài thư mục trạng thái bị đổi (repository, `.bint/`, HOME) | critical |
| D5 | Dọn trong khi một `bint run` thật (agent giả `--agent-command` ngủ 20 s) đang giữ khoá: lượt đó vẫn kết thúc đúng, dữ liệu của nó còn | critical |
| D6 | Phiếu `done`/`cancelled` cũ được dọn thật, dung lượng giảm (chức năng chính) | medium |
| D7 | Phiếu kết thúc mới (5 ngày) được giữ | medium |
| D8 | Có cách xem trước mà không xoá gì, hoặc lần dọn đầu không xoá khi chưa xác nhận | medium |
| D9 | Sau khi dọn: `task list`, `runs list`, `doctor` chạy không lỗi; chạy dọn lần hai không lỗi | medium |

### Kịch bản T2

Fixture: các chuỗi bí mật mẫu, mỗi chuỗi duy nhất để dò:

- `ghp_` cộng 36 ký tự;
- `AKIA…` cùng `aws_secret_access_key=…`;
- `Authorization: Bearer …`;
- `https://user:p4ss@host`;
- `password=…`;
- khối `-----BEGIN PRIVATE KEY-----`.

Bí mật được đưa vào qua đường bình thường:

| ID | Đường đưa bí mật | Mức nếu bí mật còn ở dạng rõ trong thư mục trạng thái, `.bint/` hoặc SQLite |
|---|---|---|
| S1 | Đầu ra lệnh kiểm chứng (`bint task verify`) | critical |
| S2 | Đầu ra agent ở lượt chạy (`bint run b-feat --agent-command` giả in bí mật) | critical |
| S3 | Mô tả và mục tiêu phiếu (`bint task new "… <bí mật> …"`) | critical |
| S4 | Nội dung review (`bint task review`, evidence/body chứa bí mật) | critical |
| S5 | Sự kiện hook (`bint hook codex` với payload PreToolUse/PostToolUse chứa bí mật trong lệnh và đầu ra) | critical |
| S6 | Tài liệu phiếu (`bint task write … requirement`) | medium |
| N1 | Không che quá tay: base commit, hash, ULID, chữ ký đầu ra, `sha256:` vẫn còn; `task show`, `task status … done` với kiểm chứng xanh vẫn chạy | medium |
| N2 | Hiệu năng: kiểm chứng in 20 MB không bí mật, thời gian không tăng quá 3 lần so với binary gốc | medium |

Phép kiểm S: quét mọi tệp dưới thư mục trạng thái và `.bint/`, cộng bản dump SQLite, tìm chuỗi bí mật thô và vài biến thể
(base64, cắt 8 ký tự đầu). Mỗi nơi lộ là một lỗi.

### Chung

Build, vet, `GOOS=windows go vet`, staticcheck cho Linux và Windows, `go test ./...` với `TMPDIR` riêng. Đỏ ở mục nào thì
mục đó là một lỗi medium.

## 6. Giao thức chạy

### 6.1. Người dùng low-tech (cả hai bên)

Opus đóng vai người dùng, chỉ được:

- Đọc tin nhắn cuối của agent hoặc đầu ra của Bint.
- Trả lời câu hỏi bằng lời thường, chỉ dùng thẻ người dùng. Không có trong thẻ thì nói "tôi không rành, bạn tự quyết cho an
  toàn".
- Chạy **nguyên văn** lệnh mà agent hoặc Bint in ra, khi lời nhắn bảo người dùng chạy (ví dụ `bint task approve <id>`,
  `bint run resume <id>`).
- **Thử dùng một lần** theo hướng dẫn của agent khi agent báo xong: chạy đúng lệnh agent bảo dùng, trên repository đó. Lỗi
  hoặc kết quả lạ thì dán nguyên đầu ra kèm một câu: "chạy thế này thì nó báo vậy". Không chạy `go test`, không đọc mã.

Opus không được: gợi ý kỹ thuật, nêu tên tệp, khoá hay hàm, sửa tệp, chạy test, tự chạy lại kiểm chứng.

**Thao tác cần định dạng.** Có những lời nhắn của Bint đòi viết nội dung theo định dạng, ví dụ trả lời câu hỏi mở bằng
`bint task write … requirement` với dòng `- [x] Q-N: … — Trả lời: …`. Người low-tech không tự làm được. Opus làm **phần định
dạng một cách máy móc** và chỉ chép câu trả lời thường, rồi ghi một lần **"thao tác kỹ thuật"**. Đây là chi phí trải nghiệm,
được báo riêng.

**Kẹt.** Kẹt là khi lời nhắn đòi thao tác mà người low-tech không làm được, kể cả có định dạng hộ: sửa artifact theo hợp đồng,
ghi tiêu chí kèm bằng chứng, xử lý xung đột. Khi đó ghi một lần **"kẹt"** rồi gửi câu chung "tôi không biết làm cái này, bạn
xử lý giúp". Bên B gửi qua cách gần nhất mà Bint cho, ví dụ `bint run resume`; nếu Bint không cho cách nào thì lượt dừng ở
đó.

**Trần.**

- Tối đa 4 tin nhắn của người dùng mỗi lượt (không tính lệnh chạy nguyên văn).
- 90 phút mỗi lượt.
- Bên A kết thúc khi agent báo xong và lần thử dùng không lỗi, hoặc khi hết trần.
- Bên B kết thúc khi tác vụ `done`, hoặc khi kẹt mà không có đường đi tiếp, hoặc khi hết trần.

### 6.2. Can thiệp vì môi trường

Lỗi rõ ràng do môi trường đo, không do agent hay Bint (như `/tmp/.git` ở lần 1), được sửa phía môi trường cho cả hai bên như
nhau và ghi riêng, không tính vào can thiệp của người dùng. Lần này `TMPDIR` riêng có ngay từ đầu nên kỳ vọng không phải dùng
tới mục này.

## 7. Cách đo

**Chính: lỗi critical/medium còn lại** ở kết quả cuối của từng lượt, gộp từ ba nguồn và bỏ trùng:

1. Kịch bản test ẩn hỏng (mục 5).
2. **Review đối kháng mù.** Một agent Opus mới cho mỗi task nhận đặc tả rủi ro (mục 3), thẻ người dùng và hai diff gán nhãn
   X/Y ngẫu nhiên. Nó liệt kê lỗi critical/medium kèm dẫn chứng tệp và dòng.
3. **Xác minh.** Một agent Opus khác, mù nhãn, kiểm từng lỗi ở bước 2: đọc mã, hoặc chạy thử trên fixture nếu được. Kết luận
   là `confirmed` hoặc `rejected`. Chỉ tính lỗi `confirmed`.

**Phụ:**

- Đáp ứng ý người dùng theo thẻ, chấm mù 1–5.
- Chất lượng mã và phạm vi thay đổi, chấm mù 1–5.
- Tốn kém: token theo rollout Codex lọc theo `cwd`, cùng công cụ với lần 1.
- Thời gian tường, số tin nhắn người dùng, số thao tác kỹ thuật, số lần kẹt.

## 8. Đọc kết quả (chốt trước khi chạy, không đổi sau khi thấy số)

Mỗi task, so số lỗi critical/medium đã xác minh (C = critical, M = medium):

| Kết luận cho một task | Điều kiện |
|---|---|
| B tốt hơn | B có ít critical hơn; hoặc critical bằng nhau và B ít hơn A ít nhất 2 medium |
| A tốt hơn | Ngược lại |
| Hoà | Còn lại |

Kết luận chung:

| Kết luận | Điều kiện |
|---|---|
| Bint có lợi rõ cho người dùng mơ hồ | B tốt hơn ở cả hai task, và tốn kém không quá 2 lần A |
| Có lợi, đánh đổi | B tốt hơn ở một task, hoà ở task kia; hoặc tốt hơn ở cả hai nhưng tốn hơn 2 lần. Ghi tỷ lệ |
| Không có tín hiệu | Hoà cả hai, hoặc mỗi bên thắng một |
| Bint bất lợi | A tốt hơn ở ít nhất một task và B không tốt hơn ở task nào |

Số lần kẹt và số thao tác kỹ thuật được báo riêng, như thước đo khả năng dùng của Bint cho người low-tech; chúng không đổi
kết luận trên.

## 9. Rủi ro và thiên lệch

- **Một lần chạy.** Lỗi critical là biến rời rạc, dao động lớn. Có thể chạy thêm một lần lặp nếu kết quả là "Có lợi, đánh đổi"
  hoặc "Không có tín hiệu" (ước khoảng 2 giờ).
- **Phần kích hoạt thích nghi.** Agent điền lệnh kích hoạt có thể chọn sai cách gọi. Giảm bằng cách ghi lại lệnh đã dùng; nếu
  bên nào hỏng chỉ vì cách gọi, chạy lại với lệnh theo đúng tài liệu của bản đó, và ghi cả hai kết quả.
- **Opus đóng vai người low-tech nhưng biết nhiều.** Giảm bằng quy tắc mục 6.1 và nhật ký nguyên văn mọi tin nhắn. Biên bản
  dán đủ để chủ dự án tự soát.
- **Lợi thế tri thức của B.** Có mục về `fslock` và khoá liên tiến trình, và mục về Windows. Lợi thế này hợp lệ, được ghi lại.
- **Repository đích là mã nguồn Bint.** T1 và T2 đụng vào thư mục trạng thái mà chính Bint của bên B đang dùng. Binary đóng
  băng nên lượt chạy không bị ảnh hưởng; test ẩn chạy trên fixture riêng.
- **Bên A dùng Codex trực tiếp.** Lỗi trong sandbox có thể làm agent bên A báo "xong" kèm cảnh báo. Người low-tech sẽ chấp
  nhận câu đó; đây là một phần của phép đo, không phải thiên lệch.

## 10. Chờ chủ dự án quyết

1. Commit gốc: (a) sửa điểm vướng 1–4 của lần 1 trước (khuyến nghị), hay (b) giữ `2b2f74d`.
2. Duyệt hai task, hai câu prompt và hai thẻ người dùng (mục 3).
3. Duyệt giao thức người dùng low-tech, gồm cách tính "thao tác kỹ thuật" và "kẹt" (mục 6.1).
4. Duyệt bảng đọc kết quả (mục 8). Bảng này được chốt trước khi chạy.
5. Sau khi duyệt, Opus viết fixture và phép kiểm (mục 5), chạy đối chứng âm trên binary gốc, rồi đưa chủ dự án duyệt lần nữa
   trước khi chạy agent.
6. Biên bản lưu ở `docs/evals/bench-bint-vs-plain-v2-<ngày>/`.

## 11. Thời gian và chi phí ước tính

| Việc | Thời gian | Ai |
|---|---|---|
| (Nếu chọn 4a) sửa điểm vướng 1–4 | ~1–2 giờ | Opus/Bint |
| Chuẩn bị repo, fixture, phép kiểm, đối chứng âm | ~60 phút | Opus |
| Duyệt | ~10 phút | Người |
| 4 lượt song song | ~60–90 phút | Codex, Opus điều phối |
| Kích hoạt thích nghi, test ẩn, review đối kháng, xác minh, biên bản | ~45 phút | Opus và agent |

Token Codex đi qua codex-lb. Phần Claude (điều phối cùng 4–6 agent đánh giá) ước khoảng $15–25.
